package handlers

import (
	"gerbangapi/prisma/db"
	"net/http"

	"github.com/labstack/echo/v4"
)

type ProductHandler struct {
	DB *db.PrismaClient
}

func NewProductHandler(dbClient *db.PrismaClient) *ProductHandler {
	return &ProductHandler{DB: dbClient}
}

// ==========================================
// STRUCT REQUEST (Murni Product Saja)
// ==========================================
type ProductRequest struct {
	Name       string `json:"name"`
	Denom      int    `json:"denom"`
	Price      int    `json:"price"`
	Qty        int    `json:"qty"`
	Status     bool   `json:"status"`
	SupplierID string `json:"supplier_id"` // Wajib Link ke Supplier
}

// ==========================================
// 1. CREATE (POST /products)
// ==========================================
func (h *ProductHandler) Create(c echo.Context) error {
	req := new(ProductRequest)
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid JSON format"})
	}

	// Validasi Input
	if req.SupplierID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "supplier_id wajib diisi"})
	}
	if req.Name == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "name wajib diisi"})
	}

	ctx := c.Request().Context()

	// Default Qty
	finalQty := req.Qty
	if finalQty <= 0 {
		finalQty = 1
	}

	// Eksekusi Create (Tanpa Transaction, karena single table)
	product, err := h.DB.Product.CreateOne(
		db.Product.Name.Set(req.Name),
		db.Product.Denom.Set(req.Denom),
		db.Product.Price.Set(req.Price),
		db.Product.Qty.Set(finalQty),
		db.Product.Status.Set(req.Status),
		db.Product.Supplier.Link(
			db.Supplier.ID.Equals(req.SupplierID),
		),
	).Exec(ctx)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Gagal membuat product: " + err.Error()})
	}

	return c.JSON(http.StatusCreated, echo.Map{
		"message": "Product created successfully",
		"data":    product,
	})
}

// ==========================================
// 2. GET ALL & BY ID (GET /products?id=...)
// ==========================================
func (h *ProductHandler) GetAll(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.QueryParam("id")

	// A. GET DETAIL
	if id != "" {
		product, err := h.DB.Product.FindUnique(
			db.Product.ID.Equals(id),
		).With(
			db.Product.Supplier.Fetch(), // Tetap ambil info supplier
		).Exec(ctx)

		if err != nil {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "Product not found"})
		}
		return c.JSON(http.StatusOK, echo.Map{"data": product})
	}

	// B. GET LIST
	products, err := h.DB.Product.FindMany().With(
		db.Product.Supplier.Fetch(),
	).Exec(ctx)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"data": products})
}

// ==========================================
// 3. UPDATE (PUT /products?id=...)
// ==========================================
func (h *ProductHandler) Update(c echo.Context) error {
	id := c.QueryParam("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Query param 'id' required"})
	}

	req := new(ProductRequest)
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request"})
	}

	ctx := c.Request().Context()
	var updates []db.ProductSetParam

	if req.Name != "" {
		updates = append(updates, db.Product.Name.Set(req.Name))
	}
	if req.Denom > 0 {
		updates = append(updates, db.Product.Denom.Set(req.Denom))
	}
	if req.Price > 0 {
		updates = append(updates, db.Product.Price.Set(req.Price))
	}
	
	updates = append(updates, db.Product.Status.Set(req.Status))
	updates = append(updates, db.Product.Qty.Set(req.Qty))

	if req.SupplierID != "" {
		updates = append(updates, db.Product.Supplier.Link(db.Supplier.ID.Equals(req.SupplierID)))
	}

	updatedProduct, err := h.DB.Product.FindUnique(
		db.Product.ID.Equals(id),
	).Update(updates...).Exec(ctx)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Gagal update: " + err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"message": "Updated", "data": updatedProduct})
}

// ==========================================
// 4. DELETE (DELETE /products?id=...)
// ==========================================
func (h *ProductHandler) Delete(c echo.Context) error {
	id := c.QueryParam("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Query param 'id' required"})
	}

	ctx := c.Request().Context()
	
	// Hapus Product langsung (Tanpa hapus recipe manual, pastikan DB support cascade atau recipe kosong)
	_, err := h.DB.Product.FindUnique(
		db.Product.ID.Equals(id),
	).Delete().Exec(ctx)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Gagal delete: " + err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{"message": "Deleted"})
}