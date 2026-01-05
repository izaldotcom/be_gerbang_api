package services

import (
	"context"
	"errors"
	"fmt"

	"gerbangapi/prisma/db"
)

// Definisikan struct di luar fungsi
type MixingItem struct {
	SupplierProductID string
	Quantity          int
}

type OrderService struct {
	client *db.PrismaClient
}

func NewOrderService(client *db.PrismaClient) *OrderService {
	return &OrderService{client: client}
}

// 1) Build Supplier Items
func (s *OrderService) BuildSupplierItems(ctx context.Context, productId string, orderQty int) ([]MixingItem, error) {
	recipes, err := s.client.ProductRecipe.
		FindMany(
			db.ProductRecipe.ProductID.Equals(productId),
		).Exec(ctx)

	if err != nil {
		return nil, err
	}

	if len(recipes) == 0 {
		return nil, errors.New("recipe not found")
	}

	var items []MixingItem
	for _, r := range recipes {
		items = append(items, MixingItem{
			SupplierProductID: r.SupplierProductID,
			Quantity:          r.Quantity * orderQty,
		})
	}

	return items, nil
}

// 2) Create Supplier Order (UPDATED: Menerima targetSupplierID)
func (s *OrderService) CreateSupplierOrderFromInternal(
	ctx context.Context, 
	internalOrder db.InternalOrderModel,
	targetSupplierID string, // <-- PARAMETER BARU
) (*db.SupplierOrderModel, error) {

	// A. Hitung bahan
	items, err := s.BuildSupplierItems(ctx, internalOrder.ProductID, internalOrder.Quantity)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("mixing generated 0 items")
	}

	// B. Validasi Supplier ID (Cek apakah ada di DB)
	supplier, err := s.client.Supplier.FindUnique(
		db.Supplier.ID.Equals(targetSupplierID),
	).Exec(ctx)

	if err != nil || supplier == nil {
		return nil, errors.New("supplier_id tidak valid atau tidak ditemukan")
	}

	// C. Buat Header Supplier Order
	order, err := s.client.SupplierOrder.CreateOne(
		// 1. Link ke InternalOrder
		db.SupplierOrder.InternalOrder.Link(
			db.InternalOrder.ID.Equals(internalOrder.ID),
		),
		
		// 2. Link ke Supplier (SESUAI PILIHAN USER)
		db.SupplierOrder.Supplier.Link(
			db.Supplier.ID.Equals(targetSupplierID),
		),

		// 3. Scalar Fields
		db.SupplierOrder.Status.Set("pending"),
	).Exec(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to create supplier_order: %w", err)
	}

	// D. Buat Detail Item
	for _, it := range items {
		_, err := s.client.SupplierOrderItem.CreateOne(
			// 1. Quantity (Scalar Wajib)
			db.SupplierOrderItem.Quantity.Set(it.Quantity),

			// 2. Link ke Header (SupplierOrder)
			db.SupplierOrderItem.SupplierOrder.Link(
				db.SupplierOrder.ID.Equals(order.ID),
			),

			// 3. Link ke Produk Supplier
			db.SupplierOrderItem.SupplierProduct.Link(
				db.SupplierProduct.ID.Equals(it.SupplierProductID),
			),
		).Exec(ctx)

		if err != nil {
			return nil, fmt.Errorf("failed to create item: %w", err)
		}
	}

	return order, nil
}

// 3) Public API (UPDATED)
func (s *OrderService) ProcessInternalOrder(
	ctx context.Context, 
	internalOrderID string, 
	supplierID string, // <-- PARAMETER BARU
) (*db.SupplierOrderModel, error) {
	
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

	// Oper supplierID ke fungsi creation
	return s.CreateSupplierOrderFromInternal(ctx, *internalOrder, supplierID)
}

// 4) GET HISTORY BY USER ID (BARU)
func (s *OrderService) GetOrderHistory(ctx context.Context, userID string) ([]db.InternalOrderModel, error) {
	// Ambil Internal Order milik User tersebut
	orders, err := s.client.InternalOrder.FindMany(
		db.InternalOrder.UserID.Equals(userID),
	).With(
		db.InternalOrder.Product.Fetch(),
		db.InternalOrder.SupplierOrders.Fetch(),
	).Exec(ctx) // [FIX] OrderBy dihapus untuk mencegah panic jika generate belum update

	if err != nil {
		return nil, err
	}

	return orders, nil
}