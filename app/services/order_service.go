package services

import (
	"context"
	"errors"

	"gerbangapi/prisma/db"
)

type OrderService struct {
	client *db.PrismaClient
}

func NewOrderService(client *db.PrismaClient) *OrderService {
	return &OrderService{client: client}
}

// ---------------------------------------------------------
// 1) Build Supplier Items
// ---------------------------------------------------------
func (s *OrderService) BuildSupplierItems(
	ctx context.Context,
	productId string,
	orderQty int,
) ([]struct {
	SupplierProductID string
	Quantity          int
}, error) {

	recipes, err := s.client.ProductRecipe.
		FindMany(
			db.ProductRecipe.ProductID.Equals(productId),
		).Exec(ctx)

	if err != nil {
		return nil, err
	}

	if len(recipes) == 0 {
		return nil, errors.New("recipe not found (pastikan seeder dijalankan)")
	}

	items := []struct {
		SupplierProductID string
		Quantity          int
	}{}

	for _, r := range recipes {
		items = append(items, struct {
			SupplierProductID string
			Quantity          int
		}{
			SupplierProductID: r.SupplierProductID,
			Quantity:          r.Quantity * orderQty,
		})
	}

	return items, nil
}

// ---------------------------------------------------------
// 2) Create Supplier Order (FIXED: Tanpa Transaction .Tx)
// ---------------------------------------------------------
func (s *OrderService) CreateSupplierOrderFromInternal(
	ctx context.Context,
	internalOrder db.InternalOrderModel,
) (*db.SupplierOrderModel, error) {

	// A. Hitung kebutuhan bahan (Mixing)
	items, err := s.BuildSupplierItems(ctx, internalOrder.ProductID, internalOrder.Quantity)
	if err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return nil, errors.New("mixing generated 0 items")
	}

	// B. Buat Header Supplier Order (Sequential)
	// Kita lakukan CreateOne biasa tanpa .Tx untuk menghindari panic
	order, err := s.client.SupplierOrder.CreateOne(
		db.SupplierOrder.InternalOrderID.Set(internalOrder.ID),
		db.SupplierOrder.SupplierID.Set("mitra-higgs"),
		db.SupplierOrder.Status.Set("pending"),
	).Exec(ctx)

	if err != nil {
		return nil, errors.New("failed to create supplier_order: " + err.Error())
	}

	// C. Buat Detail Item (Looping)
	for _, it := range items {
		_, err := s.client.SupplierOrderItem.CreateOne(
			// Pastikan urutan parameter sesuai atau gunakan setter
			// Link/Relation ID biasanya wajib diisi
			db.SupplierOrderItem.SupplierOrderID.Set(order.ID),
			db.SupplierOrderItem.SupplierProductID.Set(it.SupplierProductID),
			db.SupplierOrderItem.Quantity.Set(it.Quantity),
		).Exec(ctx)

		if err != nil {
			// Karena tidak pakai Tx, jika error disini, data header sudah terlanjur masuk.
			// Untuk tahap development ini OK. Nanti bisa ditambahkan logic delete manual.
			return nil, errors.New("failed to create item: " + err.Error())
		}
	}

	return order, nil
}

// ---------------------------------------------------------
// 3) Public API
// ---------------------------------------------------------
func (s *OrderService) ProcessInternalOrder(
	ctx context.Context,
	internalOrderID string,
) (*db.SupplierOrderModel, error) {

	// Validasi Context tidak boleh nil
	if ctx == nil {
		return nil, errors.New("context cannot be nil")
	}

	internalOrder, err := s.client.InternalOrder.
		FindUnique(
			db.InternalOrder.ID.Equals(internalOrderID),
		).Exec(ctx)

	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, errors.New("internal order not found")
		}
		return nil, err
	}

	return s.CreateSupplierOrderFromInternal(ctx, *internalOrder)
}