package services

import (
	"context"
	"errors"

	// Pastikan import ini benar merujuk ke file db_gen.go Anda
	"gerbangapi/prisma/db"
)

type OrderService struct {
	client *db.PrismaClient
}

func NewOrderService(client *db.PrismaClient) *OrderService {
	return &OrderService{client: client}
}

/*
---------------------------------------------------------
 1) Build Supplier Items
---------------------------------------------------------
*/

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
		return nil, errors.New("recipe not found")
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

/*
---------------------------------------------------------
 2) Create Supplier Order + Items (FIXED: Transaction & Syntax)
---------------------------------------------------------
*/

func (s *OrderService) CreateSupplierOrderFromInternal(
	ctx context.Context,
	internalOrder db.InternalOrderModel,
) (*db.SupplierOrderModel, error) {

	items, err := s.BuildSupplierItems(ctx, internalOrder.ProductID, internalOrder.Quantity)
	if err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return nil, errors.New("mixing generated 0 items")
	}

	var createdOrder *db.SupplierOrderModel

	// PERBAIKAN: Menggunakan Functional Transaction (s.client.Tx)
	// Ini adalah cara yang paling modern dan aman, serta menangani Commit/Rollback otomatis
	err = s.client.Tx(ctx, func(tx *db.PrismaClient) error {
		
		// CREATE SUPPLIER ORDER
		order, err := tx.SupplierOrder.CreateOne(
			// PERBAIKAN SINTAKSIS: Menggunakan setter function yang tersedia di generated client
			db.SupplierOrder.InternalOrderID.Set(internalOrder.ID),
			db.SupplierOrder.SupplierID.Set("mitra-higgs"),
			db.SupplierOrder.Status.Set("pending"),
		).Exec(ctx)

		if err != nil {
			return err
		}
		
		createdOrder = order

		// CREATE SUPPLIER ORDER ITEMS
		for _, it := range items {
			// Variabel 'it' sekarang jelas digunakan, mengatasi error 'declared and not used'
			_, err := tx.SupplierOrderItem.CreateOne(
				// PERBAIKAN SINTAKSIS
				db.SupplierOrderItem.SupplierOrderID.Set(order.ID),
				db.SupplierOrderItem.SupplierProductID.Set(it.SupplierProductID),
				db.SupplierOrderItem.Quantity.Set(it.Quantity),
			).Exec(ctx)

			if err != nil {
				return err
			}
		}
		
		return nil // Commit (jika tidak ada error)
	})

	if err != nil {
		return nil, err
	}

	return createdOrder, nil
}

/*
---------------------------------------------------------
 3) Public API
---------------------------------------------------------
*/

func (s *OrderService) ProcessInternalOrder(
	ctx context.Context,
	internalOrderID string,
) (*db.SupplierOrderModel, error) {

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