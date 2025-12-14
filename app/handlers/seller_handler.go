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
		ProductID   string `json:"product_id"` // Harap kirim UUID disini
		Destination string `json:"destination"`
		RefID       string `json:"ref_id"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "Invalid request"})
	}

	if req.ProductID == "" || req.Destination == "" {
		return c.JSON(400, echo.Map{"error": "product_id (UUID) & destination required"})
	}

	ctx := c.Request().Context()

	// ======================================================
	// 1) VALIDASI PRODUCT ID (UUID)
	// ======================================================
	var product *db.ProductModel
	var err error

	// A. Cek apakah ini UUID valid?
	// Jika payload benar (UUID), maka FindUnique akan sukses.
	product, err = h.DB.Product.FindUnique(
		db.Product.ID.Equals(req.ProductID),
	).Exec(ctx)

	// B. (Opsional) Fallback jika User masih bandel kirim "1M" atau Nama
	if err != nil {
		log.Printf("⚠️ Input '%s' bukan UUID valid, mencari berdasarkan Nama...", req.ProductID)
		product, err = h.DB.Product.FindFirst(
			db.Product.Name.Contains(req.ProductID), 
		).Exec(ctx)

		if err != nil {
			return c.JSON(404, echo.Map{
				"error": "Product tidak ditemukan. Pastikan mengirimkan UUID Product yang benar dari endpoint GET /seller/products",
			})
		}
	}

	realProductUUID := product.ID
	finalLoopCount := product.Qty

	log.Printf("✅ Order Diterima: %s (UUID: %s) | Qty Loop: %d", product.Name, realProductUUID, finalLoopCount)

	// ======================================================
	// 2) INSERT INTERNAL ORDER (Gunakan UUID Asli)
	// ======================================================
	internalOrderID := uuid.New().String()

	// Disini kuncinya: kita masukkan realProductUUID, bukan string mentah dari user
	_, err = h.DB.Prisma.ExecuteRaw(
		`INSERT INTO internal_order 
			(id, product_id, buyer_uid, quantity, status, created_at, updated_at) 
			VALUES (?, ?, ?, ?, 'pending', NOW(), NOW())`,
		internalOrderID, realProductUUID, req.Destination, 1,
	).Exec(ctx)

	if err != nil {
		// Jika masih error disini, berarti ada masalah serius di DB constraint
		return c.JSON(500, echo.Map{
			"error": "Database error saat menyimpan order: " + err.Error(),
		})
	}

	// ======================================================
	// 3) LOGIKA ORDER SERVICE & SCRAPER
	// ======================================================
	
	// A. Mixing
	supplierOrder, mixErr := h.OrderService.ProcessInternalOrder(ctx, internalOrderID)
	if mixErr != nil {
		return c.JSON(500, echo.Map{"error": "Mixing failed: " + mixErr.Error()})
	}

	// B. Init Scraper
	svc, err := scraper.NewMitraHiggsService(false) 

if err != nil {
    return c.JSON(500, echo.Map{"error": "Browser init failed: " + err.Error()})
}
defer svc.Close()

	if err := svc.Login(os.Getenv("MH_USERNAME"), os.Getenv("MH_PASSWORD")); err != nil {
		return c.JSON(502, echo.Map{"error": "Provider login failed: " + err.Error()})
	}

	// C. Cari Item HTML ID (untuk tombol klik)
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
		return c.JSON(500, echo.Map{"error": "Mixing failed (No supplier items found)"})
	}
	nominalID := items[0]["supplier_product_id"].(string)

	// D. Eksekusi Robot (Looping)
	trxID, err := svc.PlaceOrder(req.Destination, nominalID, finalLoopCount)
	if err != nil {
		h.DB.Prisma.ExecuteRaw("UPDATE supplier_order SET status='failed', last_error=? WHERE id=?", err.Error(), supplierOrder.ID).Exec(ctx)
		h.DB.Prisma.ExecuteRaw("UPDATE internal_order SET status='failed' WHERE id=?", internalOrderID).Exec(ctx)
		return c.JSON(502, echo.Map{"error": "Provider failed: " + err.Error()})
	}

	// E. Sukses
	h.DB.Prisma.ExecuteRaw("UPDATE supplier_order SET status='success' WHERE id=?", supplierOrder.ID).Exec(ctx)
	h.DB.Prisma.ExecuteRaw("UPDATE internal_order SET status='success' WHERE id=?", internalOrderID).Exec(ctx)

	return c.JSON(200, echo.Map{
		"status":          "success",
		"message":         "Order Processed Successfully",
		"product":         product.Name,
		"trx_id":          trxID,
		"quantity_loop":   finalLoopCount,
	})
}