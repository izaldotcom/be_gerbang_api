package handlers

import (
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
			{"product_code": "100M", "price": 6000, "status": "active"},
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

	// FIX: ExecuteRaw hanya mengembalikan QueryExec, kita panggil .Exec() untuk menjalankan.
	_, err := h.DB.Prisma.ExecuteRaw(
			`INSERT INTO internal_order 
			(id, product_id, buyer_uid, quantity, status, created_at, updated_at) 
			VALUES (?, ?, ?, ?, 'pending', NOW(), NOW())`,
			internalOrderID, req.ProductID, req.Destination, 1,
	).Exec(c.Request().Context())

	if err != nil {
			return c.JSON(500, echo.Map{
					"error": "Failed inserting internal_order: " + err.Error(),
			})
	}

	// ======================================================
	// 2) MIXING (OrderService)
	// ======================================================
	supplierOrder, mixErr := h.OrderService.ProcessInternalOrder(nil, internalOrderID)
	if mixErr != nil {
		return c.JSON(500, echo.Map{"error": mixErr.Error()})
	}

	// ======================================================
	// 3) SCRAPER Login
	// ======================================================
	svc, err := scraper.NewMitraHiggsService()
	if err != nil {
		return c.JSON(500, echo.Map{"error": "Browser init failed"})
	}
	defer svc.Close()

	if err := svc.Login(os.Getenv("MH_USERNAME"), os.Getenv("MH_PASSWORD")); err != nil {
		return c.JSON(502, echo.Map{"error": "Provider login failed"})
	}

	// ======================================================
	// 4) GET supplier_order_item (FIXED: Menggunakan Exec(context, &target))
	// ======================================================

	var items []map[string]interface{}
	
	queryExec := h.DB.Prisma.QueryRaw(
		"SELECT supplier_product_id FROM supplier_order_item WHERE supplier_order_id = ? LIMIT 1",
		supplierOrder.ID,
	)

	// FIX: Panggil Exec dengan 2 argumen (context dan pointer ke target slice)
	// dan tangkap error-nya saja, karena Exec hanya mengembalikan 1 nilai (error).
	queryErr := queryExec.Exec(c.Request().Context(), &items)

	// Check if there was an error in the query result
	if queryErr != nil {
		return c.JSON(500, echo.Map{"error": "Query supplier_order_item failed: " + queryErr.Error()})
	}

	// Ensure data is available in the result
	if len(items) == 0 {
		return c.JSON(500, echo.Map{"error": "No supplier_order_item"})
	}

	nominalID := items[0]["supplier_product_id"].(string)

	// ======================================================
	// 5) Scraper PlaceOrder
	// ======================================================
	trxID, err := svc.PlaceOrder(req.Destination, nominalID)
	if err != nil {
		// supplier_order FAILED
		// FIX: Tambahkan .Exec(Context) untuk semua raw query yang menjalankan update.
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
	// FIX: Tambahkan .Exec(Context)
	h.DB.Prisma.ExecuteRaw(
		"UPDATE supplier_order SET status='success' WHERE id=?",
		supplierOrder.ID,
	).Exec(c.Request().Context())

	// FIX: Tambahkan .Exec(Context)
	h.DB.Prisma.ExecuteRaw(
		"UPDATE internal_order SET status='success' WHERE id=?",
		internalOrderID,
	).Exec(c.Request().Context())

	// ======================================================
	// 7) Response
	// ======================================================
	return c.JSON(200, echo.Map{
		"message":         "Order Success",
		"internal_order":  internalOrderID,
		"supplier_order":  supplierOrder.ID,
		"provider_trx_id": trxID,
		"destination":     req.Destination,
		"status":          "success",
	})
}