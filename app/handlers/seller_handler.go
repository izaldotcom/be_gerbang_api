package handlers

import (
	"log"
	"net/http"
	"strings"

	"gerbangapi/app/services"
	"gerbangapi/app/utils"
	"gerbangapi/prisma/db"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

type SellerHandler struct {
	DB           *db.PrismaClient
	OrderService *services.OrderService
	Redis        *redis.Client
}

// NewSellerHandler menginisialisasi handler dengan DB, Service, dan Redis
func NewSellerHandler(dbClient *db.PrismaClient, orderService *services.OrderService, redisClient *redis.Client) *SellerHandler {
	return &SellerHandler{
		DB:           dbClient,
		OrderService: orderService,
		Redis:        redisClient,
	}
}

// ==========================================
// 1. GET PROFILE (Via X-API-KEY)
// ==========================================
func (h *SellerHandler) GetProfile(c echo.Context) error {
	apiKey := c.Request().Header.Get("X-API-KEY")
	if apiKey == "" {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Missing X-API-KEY header"})
	}

	ctx := c.Request().Context()

	// Cari Data User berdasarkan API Key
	// [UPDATE] Tambahkan Fetch Role di dalam relasi User
	keyData, err := h.DB.APIKey.FindUnique(
		db.APIKey.APIKey.Equals(apiKey),
	).With(
		db.APIKey.User.Fetch().With(
			db.User.Role.Fetch(), // <--- Ambil Data Role
		),
	).Exec(ctx)

	if err != nil {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Invalid API Key"})
	}

	user := keyData.User()
	
	// Handle Nullable Fields
	phoneVal, _ := user.Phone()
	webhookVal, _ := user.WebhookURL()
	statusVal, _ := user.Status()

	// [BARU] Ambil Role Name
	roleName := "-"
	if r, ok := user.Role(); ok {
		roleName = r.Name
	}

	return c.JSON(http.StatusOK, echo.Map{
		"message": "Success retrieving seller profile",
		"data": echo.Map{
			"id":          user.ID,
			"name":        user.Name,
			"email":       user.Email,
			"phone":       phoneVal,
			"webhook_url": webhookVal,
			"api_key":     keyData.APIKey,
			"status":      statusVal,
			"role_name":   roleName, // <--- Field Baru
		},
	})
}

// ==========================================
// 2. UPDATE PROFILE (Via X-API-KEY)
// ==========================================
func (h *SellerHandler) UpdateProfile(c echo.Context) error {
	apiKey := c.Request().Header.Get("X-API-KEY")
	if apiKey == "" {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Missing X-API-KEY header"})
	}

	ctx := c.Request().Context()

	// Cari Data API Key dulu untuk dapat UserID
	keyData, err := h.DB.APIKey.FindUnique(
		db.APIKey.APIKey.Equals(apiKey),
	).Exec(ctx)

	if err != nil {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Invalid API Key"})
	}

	userID := keyData.UserID

	// Bind Request
	type Req struct {
		Name       string `json:"name"`
		Email      string `json:"email"`
		Phone      string `json:"phone"`
		WebhookURL string `json:"webhook_url"`
		Password   string `json:"password"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request format"})
	}

	// Siapkan Data Update
	var ops []db.UserSetParam

	if req.Name != "" {
		ops = append(ops, db.User.Name.Set(req.Name))
	}
	if req.Email != "" {
		ops = append(ops, db.User.Email.Set(req.Email))
	}
	if req.Phone != "" {
		ops = append(ops, db.User.Phone.Set(req.Phone))
	}
	if req.WebhookURL != "" {
		ops = append(ops, db.User.WebhookURL.Set(req.WebhookURL))
	}
	
	// Hash password jika ada perubahan
	if req.Password != "" {
		hashed, _ := utils.HashPassword(req.Password)
		ops = append(ops, db.User.Password.Set(hashed))
	}

	// Eksekusi Update
	updatedUser, err := h.DB.User.FindUnique(
		db.User.ID.Equals(userID),
	).Update(
		ops...,
	).Exec(ctx)

	if err != nil {
		if strings.Contains(err.Error(), "Unique constraint") {
			return c.JSON(http.StatusConflict, echo.Map{"error": "Email/Phone already taken"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Failed to update: " + err.Error()})
	}

	// Handle Nullable Fields untuk Response
	phoneVal, _ := updatedUser.Phone()
	webhookVal, _ := updatedUser.WebhookURL()

	return c.JSON(http.StatusOK, echo.Map{
		"message": "Seller profile updated successfully",
		"data": echo.Map{
			"id":          updatedUser.ID,
			"name":        updatedUser.Name,
			"email":       updatedUser.Email,
			"phone":       phoneVal,
			"webhook_url": webhookVal,
		},
	})
}

// ==========================================
// 3. GET SELLER PRODUCTS
// ==========================================
func (h *SellerHandler) SellerProducts(c echo.Context) error {
	products, err := h.DB.Product.FindMany().With(
		db.Product.Supplier.Fetch(),
	).Exec(c.Request().Context())

	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, echo.Map{
		"message": "List produk internal",
		"data":    products,
	})
}

// ==========================================
// 4. CREATE ORDER (Asynchronous / Pending)
// ==========================================
func (h *SellerHandler) SellerOrder(c echo.Context) error {
	type Req struct {
		ProductID   string `json:"product_id"`
		Destination string `json:"destination"`
		RefID       string `json:"ref_id"`
		SupplierID  string `json:"supplier_id"`
		WebhookURL  string `json:"webhook_url"` // Opsional
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
	}

	if req.ProductID == "" || req.Destination == "" || req.SupplierID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "product_id, destination, dan supplier_id required"})
	}

	ctx := c.Request().Context()

	// A. VALIDASI PRODUCT
	product, err := h.DB.Product.FindUnique(
		db.Product.ID.Equals(req.ProductID),
	).Exec(ctx)

	if err != nil {
		// Fallback search by name
		product, err = h.DB.Product.FindFirst(
			db.Product.Name.Contains(req.ProductID),
		).Exec(ctx)
		if err != nil {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "Product tidak ditemukan."})
		}
	}
	realProductUUID := product.ID

	// B. AMBIL USER ID (Dari Context Middleware)
	// Pastikan SellerSecurityMiddleware sudah men-set "user_id"
	userID, ok := c.Get("user_id").(string)
	if !ok || userID == "" {
		// Fallback manual jika context kosong (Safety net)
		apiKey := c.Request().Header.Get("X-API-KEY")
		if apiKey != "" {
			keyData, _ := h.DB.APIKey.FindUnique(db.APIKey.APIKey.Equals(apiKey)).Exec(ctx)
			if keyData != nil {
				userID = keyData.UserID
			}
		}
	}

	if userID == "" {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Unauthorized: User ID not found"})
	}

	// C. INSERT INTERNAL ORDER (Status: Pending)
	internalOrderID := uuid.New().String()
	_, err = h.DB.Prisma.ExecuteRaw(
		`INSERT INTO internal_order 
         (id, product_id, user_id, buyer_uid, quantity, status, created_at, updated_at) 
         VALUES (?, ?, ?, ?, ?, 'pending', NOW(), NOW())`,
		internalOrderID, realProductUUID, userID, req.Destination, 1,
	).Exec(ctx)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Database error: " + err.Error()})
	}

	// D. MIXING PROCESS (Memecah menjadi Supplier Order)
	// Fungsi ini akan membuat row di tabel supplier_order dengan status 'pending'
	supplierOrder, mixErr := h.OrderService.ProcessInternalOrder(ctx, internalOrderID, req.SupplierID)

	if mixErr != nil {
		// Update failed jika mixing gagal
		h.DB.Prisma.ExecuteRaw("UPDATE internal_order SET status='failed' WHERE id=?", internalOrderID).Exec(ctx)
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Mixing failed: " + mixErr.Error()})
	}

	// E. RESPONSE CEPAT (Accepted)
	// Worker di background akan memproses order yang statusnya 'pending'
	log.Printf("✅ Order Accepted: %s -> Masuk Antrian Worker", internalOrderID)

	return c.JSON(http.StatusAccepted, echo.Map{
		"status":            "pending",
		"message":           "Order accepted and queued for processing",
		"order_id":          internalOrderID,
		"supplier_order_id": supplierOrder.ID,
		"estimated_time":    "1-2 minutes",
	})
}

// ==========================================
// 5. GET HISTORY ORDER
// ==========================================
func (h *SellerHandler) HistoryOrder(c echo.Context) error {
	// 1. Ambil User ID dari JWT (karena route ini biasanya diproteksi JWT di dashboard)
	// Jika dipanggil via API Key, pastikan Middleware men-set user_id
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

		// Ambil Product
		p := o.Product()
		if p != nil {
			productName = p.Name
			productPrice = p.Price
		}

		sn := "-"
		
		// Ambil SN dari SupplierOrder pertama yang punya TrxID
		sos := o.SupplierOrders()
		for _, so := range sos {
			if val, ok := so.ProviderTrxID(); ok && val != "" {
				sn = val
				break 
			}
		}

		item := map[string]interface{}{
			"id":           o.ID,
			"ref_id":       o.ID,
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