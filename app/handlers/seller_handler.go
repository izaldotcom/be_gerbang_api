package handlers

import (
	"log"
	"net/http"
	"os"
	"strings"

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
	// [FIX] Tambahkan .With(db.Product.Supplier.Fetch())
	products, err := h.DB.Product.FindMany().With(
		db.Product.Supplier.Fetch(), 
	).Exec(c.Request().Context())
	
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
		SupplierID  string `json:"supplier_id"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "Invalid request"})
	}

	if req.ProductID == "" || req.Destination == "" || req.SupplierID == "" {
		return c.JSON(400, echo.Map{"error": "product_id, destination, dan supplier_id required"})
	}

	ctx := c.Request().Context()

	// 1) VALIDASI PRODUCT (UUID / Name Fallback)
	var product *db.ProductModel
	var err error

	product, err = h.DB.Product.FindUnique(
		db.Product.ID.Equals(req.ProductID),
	).Exec(ctx)

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
	// Note: Untuk produk MIX, qty di tabel product biasanya 1.
	// Jumlah looping/klik sebenarnya ada di tabel supplier_order_item nanti.

	log.Printf("✅ Order: %s | Dest: %s | Supplier: %s", product.Name, req.Destination, req.SupplierID)

	// 2) INSERT INTERNAL ORDER
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

	// 3) MIXING PROCESS (Order Service)
	supplierOrder, mixErr := h.OrderService.ProcessInternalOrder(ctx, internalOrderID, req.SupplierID)
	
	if mixErr != nil {
		h.DB.Prisma.ExecuteRaw("UPDATE internal_order SET status='failed' WHERE id=?", internalOrderID).Exec(ctx)
		return c.JSON(400, echo.Map{"error": "Mixing failed: " + mixErr.Error()})
	}

	// ======================================================
	// 4) PERSIAPAN SCRAPER
	// ======================================================
	
	// A. Query Items (AMBIL SEMUA, JANGAN LIMIT 1)
	// Kita ambil HTML ID (supplier_product_id) dan Quantity (berapa kali klik)
	type ItemToScrape struct {
		HtmlID   string `json:"supplier_product_id"`
		Quantity int    `json:"quantity"`
	}
	var items []ItemToScrape

	// Query Join: supplier_order_item -> supplier_product
	queryExec := h.DB.Prisma.QueryRaw(
		`SELECT sp.supplier_product_id, soi.quantity 
		 FROM supplier_order_item soi
		 JOIN supplier_product sp ON soi.supplier_product_id = sp.id
		 WHERE soi.supplier_order_id = ?`,
		supplierOrder.ID,
	)
	
	if err := queryExec.Exec(ctx, &items); err != nil {
		return c.JSON(500, echo.Map{"error": "Failed to fetch order items"})
	}

	if len(items) == 0 {
		return c.JSON(500, echo.Map{"error": "No items to scrape (Recipe empty?)"})
	}

	// B. Init Browser & Login (Cukup 1x di awal)
	// Set isDebug=false agar headless (background)
	svc, err := scraper.NewMitraHiggsService(false) 
	if err != nil {
		return c.JSON(500, echo.Map{"error": "Browser init failed: " + err.Error()})
	}
	defer svc.Close()

	if err := svc.Login(os.Getenv("MH_USERNAME"), os.Getenv("MH_PASSWORD")); err != nil {
		return c.JSON(502, echo.Map{"error": "Provider login failed: " + err.Error()})
	}

	// ======================================================
	// 5) EKSEKUSI ITEM SATU PER SATU (LOOPING MIX)
	// ======================================================
	var allTrxIDs []string
	var failedReasons []string

	for idx, item := range items {
		log.Printf("🤖 Processing Item %d/%d: HTML_ID=%s, QtyLoop=%d", idx+1, len(items), item.HtmlID, item.Quantity)
		
		// Panggil Fungsi Scraper untuk Item ini
		trxID, err := svc.PlaceOrder(req.Destination, item.HtmlID, item.Quantity)
		
		if err != nil {
			log.Printf("❌ Gagal di item %s: %v", item.HtmlID, err)
			failedReasons = append(failedReasons, err.Error())
			// Opsional: Break loop jika satu gagal, atau lanjut?
			// Biasanya jika satu gagal, transaksi dianggap gagal total.
			break 
		}
		
		allTrxIDs = append(allTrxIDs, trxID)
	}

	// ======================================================
	// 6) FINALISASI STATUS
	// ======================================================
	
	if len(failedReasons) > 0 {
		// Jika ada error
		errMsg := strings.Join(failedReasons, "; ")
		h.DB.Prisma.ExecuteRaw("UPDATE supplier_order SET status='failed', last_error=? WHERE id=?", errMsg, supplierOrder.ID).Exec(ctx)
		h.DB.Prisma.ExecuteRaw("UPDATE internal_order SET status='failed' WHERE id=?", internalOrderID).Exec(ctx)
		
		return c.JSON(502, echo.Map{
			"status": "failed",
			"error":  "Provider partial/full failure: " + errMsg,
			"success_trx": allTrxIDs, // Info transaksi yang sempat sukses
		})
	}

	// Sukses Full
	finalTrxString := strings.Join(allTrxIDs, ", ")
	h.DB.Prisma.ExecuteRaw("UPDATE supplier_order SET status='success' WHERE id=?", supplierOrder.ID).Exec(ctx)
	h.DB.Prisma.ExecuteRaw("UPDATE internal_order SET status='success' WHERE id=?", internalOrderID).Exec(ctx)

	return c.JSON(200, echo.Map{
		"status":          "success",
		"message":         "Order Processed Successfully",
		"product":         product.Name,
		"supplier_id":     req.SupplierID,
		"trx_ids":         finalTrxString,
	})
}