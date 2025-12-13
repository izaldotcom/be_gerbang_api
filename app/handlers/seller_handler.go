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
	return c.JSON(http.StatusOK, echo.Map{
		"message": "List produk internal",
		"data": []echo.Map{
			{"product_code": "1M", "price": 1000, "status": "active"},
			{"product_code": "5M", "price": 7500, "status": "active"},
			{"product_code": "100M", "price": 140000, "status": "active"},
		},
	})
}

func (h *SellerHandler) SellerOrder(c echo.Context) error {

	type Req struct {
		ProductID   string `json:"product_id"`
		Destination string `json:"destination"`
		RefID       string `json:"ref_id"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "Invalid request"})
	}

	if req.ProductID == "" || req.Destination == "" {
		return c.JSON(400, echo.Map{"error": "product_id & destination required"})
	}

	// ======================================================
	// 1) INSERT internal_order
	// ======================================================
	internalOrderID := uuid.New().String()

	// Kita insert default 1 dulu, nanti quantity loop diambil dari tabel product
	_, err := h.DB.Prisma.ExecuteRaw(
		`INSERT INTO internal_order 
			(id, product_id, buyer_uid, quantity, status, created_at, updated_at) 
			VALUES (?, ?, ?, ?, 'pending', NOW(), NOW())`,
		internalOrderID, req.ProductID, req.Destination, 1,
	).Exec(c.Request().Context())

	if err != nil {
		return c.JSON(500, echo.Map{"error": "Failed inserting internal_order: " + err.Error()})
	}

	// ======================================================
	// 2) MIXING (OrderService)
	// ======================================================
	supplierOrder, mixErr := h.OrderService.ProcessInternalOrder(c.Request().Context(), internalOrderID)
	if mixErr != nil {
		return c.JSON(500, echo.Map{"error": mixErr.Error()})
	}

	// ======================================================
	// 3) SCRAPER Login
	// ======================================================
	svc, err := scraper.NewMitraHiggsService()
	if err != nil {
		return c.JSON(500, echo.Map{"error": "Browser init failed: " + err.Error()})
	}
	defer svc.Close()

	if err := svc.Login(os.Getenv("MH_USERNAME"), os.Getenv("MH_PASSWORD")); err != nil {
		return c.JSON(502, echo.Map{"error": "Provider login failed: " + err.Error()})
	}

	// ======================================================
	// 4) GET QUANTITY DARI TABEL PRODUCT (SESUAI REQUEST)
	// ======================================================
	
	// A. Ambil Resep Loop (Qty) dari Internal Product
	var internalProduct []map[string]interface{}
	
	// Query langsung ke tabel product berdasarkan req.ProductID (Misal "5M")
	errProduct := h.DB.Prisma.QueryRaw(
		"SELECT qty FROM product WHERE id = ? LIMIT 1", 
		req.ProductID,
	).Exec(c.Request().Context(), &internalProduct)

	if errProduct != nil || len(internalProduct) == 0 {
		return c.JSON(500, echo.Map{"error": "Product definition not found in DB"})
	}

	// Convert Qty DB ke Integer
	finalLoopCount := 1
	if qtyFloat, ok := internalProduct[0]["qty"].(float64); ok {
		finalLoopCount = int(qtyFloat)
	} else if qtyInt, ok := internalProduct[0]["qty"].(int64); ok {
		finalLoopCount = int(qtyInt)
	}

	log.Printf("🔥 PRODUCT ID: %s | QTY (LOOP): %d", req.ProductID, finalLoopCount)


	// B. Ambil ID Tombol HTML (NominalID) dari Supplier Product
	var items []map[string]interface{}
	queryExec := h.DB.Prisma.QueryRaw(
		`SELECT sp.supplier_product_id 
		 FROM supplier_product sp
		 JOIN supplier_order_item soi ON soi.supplier_product_id = sp.id
		 WHERE soi.supplier_order_id = ? 
		 LIMIT 1`,
		supplierOrder.ID,
	)
	if err := queryExec.Exec(c.Request().Context(), &items); err != nil || len(items) == 0 {
		return c.JSON(500, echo.Map{"error": "No supplier_order_item found"})
	}

	nominalID := items[0]["supplier_product_id"].(string)

	// ======================================================
	// 5) Scraper PlaceOrder (GUNAKAN LOOP DARI TABEL PRODUCT)
	// ======================================================
	
	// Masukkan finalLoopCount ke fungsi PlaceOrder
	trxID, err := svc.PlaceOrder(req.Destination, nominalID, finalLoopCount)
	
	if err != nil {
		h.DB.Prisma.ExecuteRaw(
			"UPDATE supplier_order SET status='failed', last_error=? WHERE id=?",
			err.Error(),
			supplierOrder.ID,
		).Exec(c.Request().Context())

		h.DB.Prisma.ExecuteRaw(
			"UPDATE internal_order SET status='failed' WHERE id=?",
			internalOrderID,
		).Exec(c.Request().Context())

		return c.JSON(502, echo.Map{"error": "Provider failed: " + err.Error()})
	}

	// ======================================================
	// 6) Mark SUCCESS
	// ======================================================
	h.DB.Prisma.ExecuteRaw("UPDATE supplier_order SET status='success' WHERE id=?", supplierOrder.ID).Exec(c.Request().Context())
	h.DB.Prisma.ExecuteRaw("UPDATE internal_order SET status='success' WHERE id=?", internalOrderID).Exec(c.Request().Context())

	return c.JSON(200, echo.Map{
		"message":         "Order Success",
		"internal_order":  internalOrderID,
		"supplier_order":  supplierOrder.ID,
		"provider_trx_id": trxID,
		"destination":     req.Destination,
		"quantity_loop":   finalLoopCount, // Mengembalikan jumlah loop yang terbaca
		"status":          "success",
	})
}