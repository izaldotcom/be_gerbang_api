package handlers

import (
	"log"
	"net/http"
	"os"

	"gerbangapi/app/services"
	"gerbangapi/app/services/scraper"
	"gerbangapi/prisma/db"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type SellerHandler struct {
	DB           *db.PrismaClient
	OrderService *services.OrderService
}

func NewSellerHandler(dbClient *db.PrismaClient, orderService *services.OrderService) *SellerHandler {
	return &SellerHandler{
		DB:           dbClient,
		OrderService: orderService,
	}
}

func (h *SellerHandler) SellerProducts(c echo.Context) error {
	products, err := h.DB.Product.FindMany().Exec(c.Request().Context())
	if err != nil {
		return c.JSON(500, echo.Map{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, echo.Map{
		"message": "List produk internal",
		"data":    products,
	})
}

func (h *SellerHandler) SellerOrder(c echo.Context) error {

	type Req struct {
		ProductID   string `json:"product_id"`
		Destination string `json:"destination"`
		RefID       string `json:"ref_id"`
		SupplierID  string `json:"supplier_id"` // <-- FIELD BARU
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "Invalid request"})
	}

	// Validasi Input
	if req.ProductID == "" || req.Destination == "" || req.SupplierID == "" {
		return c.JSON(400, echo.Map{"error": "product_id, destination, dan supplier_id required"})
	}

	ctx := c.Request().Context()

	// ======================================================
	// 1) VALIDASI PRODUCT ID (UUID)
	// ======================================================
	var product *db.ProductModel
	var err error

	// A. Cek UUID valid
	product, err = h.DB.Product.FindUnique(
		db.Product.ID.Equals(req.ProductID),
	).Exec(ctx)

	// B. Fallback cari by Nama
	if err != nil {
		log.Printf("⚠️ Input '%s' bukan UUID valid, mencari berdasarkan Nama...", req.ProductID)
		product, err = h.DB.Product.FindFirst(
			db.Product.Name.Contains(req.ProductID), 
		).Exec(ctx)

		if err != nil {
			return c.JSON(404, echo.Map{"error": "Product tidak ditemukan."})
		}
	}

	realProductUUID := product.ID
	finalLoopCount := product.Qty

	log.Printf("✅ Order: %s | Supplier: %s | QtyLoop: %d", product.Name, req.SupplierID, finalLoopCount)

	// ======================================================
	// 2) INSERT INTERNAL ORDER
	// ======================================================
	internalOrderID := uuid.New().String()

	_, err = h.DB.Prisma.ExecuteRaw(
		`INSERT INTO internal_order 
			(id, product_id, buyer_uid, quantity, status, created_at, updated_at) 
			VALUES (?, ?, ?, ?, 'pending', NOW(), NOW())`,
		internalOrderID, realProductUUID, req.Destination, 1,
	).Exec(ctx)

	if err != nil {
		return c.JSON(500, echo.Map{"error": "Database error: " + err.Error()})
	}

	// ======================================================
	// 3) LOGIKA ORDER SERVICE (MIXING)
	// ======================================================
	
	// Kirim req.SupplierID ke Service
	supplierOrder, mixErr := h.OrderService.ProcessInternalOrder(ctx, internalOrderID, req.SupplierID)
	
	if mixErr != nil {
		h.DB.Prisma.ExecuteRaw("UPDATE internal_order SET status='failed' WHERE id=?", internalOrderID).Exec(ctx)
		return c.JSON(400, echo.Map{"error": "Mixing failed: " + mixErr.Error()})
	}

	// ======================================================
	// 4) EKSEKUSI SCRAPER (MITRA HIGGS ONLY)
	// ======================================================
	// TODO: Nanti logic ini bisa dibuat dinamis berdasarkan Tipe Supplier
	// Untuk sekarang kita asumsikan ID supplier yang dikirim adalah tipe Mitra Higgs
	
	svc, err := scraper.NewMitraHiggsService(false) // False = Headless ON
	if err != nil {
		return c.JSON(500, echo.Map{"error": "Browser init failed: " + err.Error()})
	}
	defer svc.Close()

	if err := svc.Login(os.Getenv("MH_USERNAME"), os.Getenv("MH_PASSWORD")); err != nil {
		return c.JSON(502, echo.Map{"error": "Provider login failed: " + err.Error()})
	}

	// Ambil ID Tombol HTML
	var items []map[string]interface{}
	queryExec := h.DB.Prisma.QueryRaw(
		`SELECT sp.supplier_product_id 
		 FROM supplier_product sp
		 JOIN supplier_order_item soi ON soi.supplier_product_id = sp.id
		 WHERE soi.supplier_order_id = ? 
		 LIMIT 1`,
		supplierOrder.ID,
	)
	if err := queryExec.Exec(ctx, &items); err != nil || len(items) == 0 {
		return c.JSON(500, echo.Map{"error": "No items found for scraping"})
	}
	nominalID := items[0]["supplier_product_id"].(string)

	// Eksekusi Robot
	trxID, err := svc.PlaceOrder(req.Destination, nominalID, finalLoopCount)
	if err != nil {
		h.DB.Prisma.ExecuteRaw("UPDATE supplier_order SET status='failed', last_error=? WHERE id=?", err.Error(), supplierOrder.ID).Exec(ctx)
		h.DB.Prisma.ExecuteRaw("UPDATE internal_order SET status='failed' WHERE id=?", internalOrderID).Exec(ctx)
		return c.JSON(502, echo.Map{"error": "Provider failed: " + err.Error()})
	}

	// Sukses
	h.DB.Prisma.ExecuteRaw("UPDATE supplier_order SET status='success' WHERE id=?", supplierOrder.ID).Exec(ctx)
	h.DB.Prisma.ExecuteRaw("UPDATE internal_order SET status='success' WHERE id=?", internalOrderID).Exec(ctx)

	return c.JSON(200, echo.Map{
		"status":          "success",
		"message":         "Order Processed",
		"product":         product.Name,
		"supplier_id":     req.SupplierID,
		"trx_id":          trxID,
	})
}