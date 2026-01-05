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
	"github.com/redis/go-redis/v9" // [BARU] Import Redis
)

type SellerHandler struct {
	DB           *db.PrismaClient
	OrderService *services.OrderService
	Redis        *redis.Client // [BARU] Tambah Field Redis
}

// [BARU] Update Constructor terima Redis
func NewSellerHandler(dbClient *db.PrismaClient, orderService *services.OrderService, redisClient *redis.Client) *SellerHandler {
	return &SellerHandler{
		DB:           dbClient,
		OrderService: orderService,
		Redis:        redisClient, // Assign ke struct
	}
}

func (h *SellerHandler) SellerProducts(c echo.Context) error {
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

func (h *SellerHandler) SellerOrderOld(c echo.Context) error {

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

	// 1) VALIDASI PRODUCT
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

	// 3) MIXING PROCESS
	supplierOrder, mixErr := h.OrderService.ProcessInternalOrder(ctx, internalOrderID, req.SupplierID)

	if mixErr != nil {
		h.DB.Prisma.ExecuteRaw("UPDATE internal_order SET status='failed' WHERE id=?", internalOrderID).Exec(ctx)
		return c.JSON(400, echo.Map{"error": "Mixing failed: " + mixErr.Error()})
	}

	// ======================================================
	// 4) PERSIAPAN SCRAPER
	// ======================================================

	type ItemToScrape struct {
		SupplierProductID string `json:"supplier_product_id"` // Matches DB column name
		Quantity          int    `json:"quantity"`
	}
	var items []ItemToScrape

	// Query Join
	queryExec := h.DB.Prisma.QueryRaw(
		`SELECT sp.supplier_product_id, soi.quantity 
		 FROM supplier_order_item soi
		 JOIN supplier_product sp ON soi.supplier_product_id = sp.id
		 WHERE soi.supplier_order_id = ?`,
		supplierOrder.ID,
	)

	if err := queryExec.Exec(ctx, &items); err != nil {
		return c.JSON(500, echo.Map{"error": "Failed to fetch order items: " + err.Error()})
	}

	if len(items) == 0 {
		return c.JSON(500, echo.Map{"error": "No items to scrape (Recipe empty?)"})
	}

	// [FIX] Pass h.Redis ke Service Scraper
	svc, err := scraper.NewMitraHiggsService(false, h.Redis)
	if err != nil {
		log.Printf("❌ Browser Init Failed: %v", err)
		return c.JSON(500, echo.Map{"error": "Browser init failed: " + err.Error()})
	}
	defer svc.Close()

	if err := svc.Login(os.Getenv("MH_USERNAME"), os.Getenv("MH_PASSWORD")); err != nil {
		return c.JSON(502, echo.Map{"error": "Provider login failed: " + err.Error()})
	}

	// ======================================================
	// 5) EKSEKUSI ITEM SATU PER SATU
	// ======================================================
	var allTrxIDs []string
	var failedReasons []string

	for idx, item := range items {
		log.Printf("🤖 Processing Item %d/%d: HTML_ID=%s, QtyLoop=%d", idx+1, len(items), item.SupplierProductID, item.Quantity)

		trxID, err := svc.PlaceOrder(req.Destination, item.SupplierProductID, item.Quantity)

		if err != nil {
			log.Printf("❌ Gagal di item %s: %v", item.SupplierProductID, err)
			failedReasons = append(failedReasons, err.Error())
			break
		}

		allTrxIDs = append(allTrxIDs, trxID)
	}

	// ======================================================
	// 6) FINALISASI STATUS
	// ======================================================

	if len(failedReasons) > 0 {
		errMsg := strings.Join(failedReasons, "; ")
		h.DB.Prisma.ExecuteRaw("UPDATE supplier_order SET status='failed', last_error=? WHERE id=?", errMsg, supplierOrder.ID).Exec(ctx)
		h.DB.Prisma.ExecuteRaw("UPDATE internal_order SET status='failed' WHERE id=?", internalOrderID).Exec(ctx)

		return c.JSON(502, echo.Map{
			"status":      "failed",
			"error":       "Provider partial/full failure: " + errMsg,
			"success_trx": allTrxIDs,
		})
	}

	// Sukses Full
	finalTrxString := strings.Join(allTrxIDs, ", ")
	h.DB.Prisma.ExecuteRaw("UPDATE supplier_order SET status='success' WHERE id=?", supplierOrder.ID).Exec(ctx)
	h.DB.Prisma.ExecuteRaw("UPDATE internal_order SET status='success' WHERE id=?", internalOrderID).Exec(ctx)

	return c.JSON(200, echo.Map{
		"status":      "success",
		"message":     "Order Processed Successfully",
		"product":     product.Name,
		"supplier_id": req.SupplierID,
		"trx_ids":     finalTrxString,
	})
}

func (h *SellerHandler) SellerOrder(c echo.Context) error {
	type Req struct {
		ProductID   string `json:"product_id"`
		Destination string `json:"destination"`
		RefID       string `json:"ref_id"`
		SupplierID  string `json:"supplier_id"`
		// Opsional: Jika seller ingin mengirim URL webhook dinamis
		WebhookURL  string `json:"webhook_url"` 
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "Invalid request"})
	}

	if req.ProductID == "" || req.Destination == "" || req.SupplierID == "" {
		return c.JSON(400, echo.Map{"error": "product_id, destination, dan supplier_id required"})
	}

	ctx := c.Request().Context()

	// 1) VALIDASI PRODUCT (TETAP SAMA)
	product, err := h.DB.Product.FindUnique(
		db.Product.ID.Equals(req.ProductID),
	).Exec(ctx)

	if err != nil {
		// Fallback search by name
		product, err = h.DB.Product.FindFirst(
			db.Product.Name.Contains(req.ProductID),
		).Exec(ctx)
		if err != nil {
			return c.JSON(404, echo.Map{"error": "Product tidak ditemukan."})
		}
	}
	realProductUUID := product.ID

	// [BARU] Ambil User ID dari Context (diset oleh Middleware Security)
	userID := c.Get("user_id").(string)

	// 2) INSERT INTERNAL ORDER (Status Awal: Pending)
	internalOrderID := uuid.New().String()
    _, err = h.DB.Prisma.ExecuteRaw(
        `INSERT INTO internal_order 
            (id, product_id, user_id, buyer_uid, quantity, status, created_at, updated_at) 
            VALUES (?, ?, ?, ?, ?, 'pending', NOW(), NOW())`,
        internalOrderID, realProductUUID, userID, req.Destination, 1, // <--- Tambah userID di sini
    ).Exec(ctx)

	if err != nil {
		return c.JSON(500, echo.Map{"error": "Database error: " + err.Error()})
	}

	// 3) MIXING PROCESS (Memecah menjadi Supplier Order)
	// Fungsi ini akan membuat row di tabel supplier_order dengan status 'pending'
	supplierOrder, mixErr := h.OrderService.ProcessInternalOrder(ctx, internalOrderID, req.SupplierID)

	if mixErr != nil {
		// Update failed jika mixing gagal
		h.DB.Prisma.ExecuteRaw("UPDATE internal_order SET status='failed' WHERE id=?", internalOrderID).Exec(ctx)
		return c.JSON(400, echo.Map{"error": "Mixing failed: " + mixErr.Error()})
	}

	// ======================================================
	// 4) RESPONSE CEPAT (ASYNCHRONOUS)
	// ======================================================
	// Kita tidak melakukan scraping di sini. Worker akan mengambil job berdasarkan status 'pending'.
	
	log.Printf("✅ Order Accepted: %s -> Masuk Antrian Worker", internalOrderID)

	return c.JSON(http.StatusAccepted, echo.Map{
		"status":      "pending",
		"message":     "Order accepted and queued for processing",
		"order_id":    internalOrderID, // ID Internal
		"supplier_order_id": supplierOrder.ID, // ID untuk tracking worker
		"estimated_time": "1-2 minutes",
	})
}

// [BARU] GET HISTORY ORDER
func (h *SellerHandler) HistoryOrder(c echo.Context) error {
	// 1. Ambil User ID dari JWT
	userID, ok := c.Get("user_id").(string)
	if !ok || userID == "" {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Unauthorized"})
	}

	// 2. Panggil Service
	orders, err := h.OrderService.GetOrderHistory(c.Request().Context(), userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Failed to fetch history: " + err.Error()})
	}

	// 3. Mapping Response
	var response []map[string]interface{}

	for _, o := range orders {
		productName := "Unknown Product"
		productPrice := 0

		// Ambil Product (Required Relasi)
		// Return 1 value (pointer)
		p := o.Product()
		if p != nil {
			productName = p.Name
			productPrice = p.Price
		}

		sn := "-"
		
		// Ambil SupplierOrders (Slice Relasi)
		sos := o.SupplierOrders()
		
		for _, so := range sos {
			// Cek ProviderTrxID (Optional String)
			if val, ok := so.ProviderTrxID(); ok && val != "" {
				sn = val
				break 
			}
		}

		item := map[string]interface{}{
			"id":           o.ID,
			"ref_id":       o.ID, // [BARU] Menampilkan ID Transaksi sebagai ref_id
			"product_name": productName,
			"destination":  o.BuyerUID,
			"quantity":     o.Quantity,
			"status":       o.Status,
			"sn":           sn,
			"created_at":   o.CreatedAt,
			"price":        productPrice,
		}

		response = append(response, item)
	}

	return c.JSON(http.StatusOK, echo.Map{
		"message": "Order History retrieved successfully",
		"data":    response,
	})
}