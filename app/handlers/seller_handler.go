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

	// Capture the error directly from ExecuteRaw
	_, err := h.DB.Prisma.ExecuteRaw(
		"INSERT INTO internal_order (id, product_id, buyer_uid, quantity, status) VALUES (?, ?, ?, ?, 'pending')",
		internalOrderID, req.ProductID, req.Destination, 1,
	).Exec(c.Request().Context()) // ExecuteRaw often requires .Exec(context)

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
	// 4) GET supplier_order_item
	// ======================================================
	// QueryRaw returns the data (as []map[string]interface{}) and an error directly
	var items []map[string]interface{}
	err = h.DB.Prisma.QueryRaw(
		"SELECT supplier_product_id FROM supplier_order_item WHERE supplier_order_id = ? LIMIT 1",
		supplierOrder.ID,
	).Exec(c.Request().Context(), &items) // Use .Exec(context, &target) to get data

	if err != nil {
		return c.JSON(500, echo.Map{"error": "Query supplier_order_item failed: " + err.Error()})
	}

	// Check the length of the items slice which now holds the data
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
	h.DB.Prisma.ExecuteRaw(
		"UPDATE supplier_order SET status='success' WHERE id=?",
		supplierOrder.ID,
	).Exec(c.Request().Context())

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