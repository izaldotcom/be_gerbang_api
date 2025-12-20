package handlers

import (
	"gerbangapi/prisma/db"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type ProductHandler struct {
	DB *db.PrismaClient
}

func NewProductHandler(dbClient *db.PrismaClient) *ProductHandler {
	return &ProductHandler{DB: dbClient}
}

// Struct Helper
type RecipeInput struct {
	SupplierProductID string `json:"supplier_product_id"`
	Quantity          int    `json:"quantity"`
}

type ProductMixReq struct {
	Name    string        `json:"name"`
	Denom   int           `json:"denom"`
	Price   int           `json:"price"`
	Qty     int           `json:"qty"`
	Status  bool          `json:"status"`
	Recipes []RecipeInput `json:"recipes"`
}

// ==========================================
// 1. CREATE (Batch Transaction + Manual UUID)
// ==========================================
func (h *ProductHandler) Create(c echo.Context) error {
	req := new(ProductMixReq)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "Invalid JSON request"})
	}

	if len(req.Recipes) == 0 {
		return c.JSON(400, echo.Map{"error": "Minimal harus ada 1 recipe/bahan"})
	}

	ctx := c.Request().Context()

	// 1. GENERATE UUID MANUAL
	// Agar kita bisa pakai ID ini untuk Product DAN Recipe di satu batch yang sama
	newProductID := uuid.New().String()

	finalQty := req.Qty
	if finalQty <= 0 { finalQty = 1 }

	// 2. SIAPKAN TRANSAKSI (Batch)
	// Gunakan interface db.PrismaTransaction
	var ops []db.PrismaTransaction 

	// A. Operasi Create Product
	opProduct := h.DB.Product.CreateOne(
		db.Product.ID.Set(newProductID), // Set ID Manual
		db.Product.Name.Set(req.Name),
		db.Product.Denom.Set(req.Denom),
		db.Product.Price.Set(req.Price),
		db.Product.Qty.Set(finalQty),
		db.Product.Status.Set(req.Status),
	).Tx() // Gunakan .Tx() agar jadi operasi transaksi

	ops = append(ops, opProduct)

	// B. Operasi Create Recipes (Looping)
	for _, r := range req.Recipes {
		opRecipe := h.DB.ProductRecipe.CreateOne(
			db.ProductRecipe.Quantity.Set(r.Quantity),
			db.ProductRecipe.Product.Link(
				db.Product.ID.Equals(newProductID), // Link ke ID yang kita buat diatas
			),
			db.ProductRecipe.SupplierProduct.Link(
				db.SupplierProduct.ID.Equals(r.SupplierProductID),
			),
		).Tx()
		
		ops = append(ops, opRecipe)
	}

	// 3. EKSEKUSI TRANSAKSI
	if err := h.DB.Prisma.Transaction(ops...).Exec(ctx); err != nil {
		return c.JSON(500, echo.Map{"error": "Gagal menyimpan produk: " + err.Error()})
	}

	return c.JSON(201, echo.Map{
		"message": "Product & Recipes created successfully",
		"id":      newProductID,
	})
}

// ==========================================
// 2. READ ALL
// ==========================================
func (h *ProductHandler) GetAll(c echo.Context) error {
	products, err := h.DB.Product.FindMany().With(
		db.Product.Recipe.Fetch().With(
			db.ProductRecipe.SupplierProduct.Fetch(),
		),
	).Exec(c.Request().Context())

	if err != nil {
		return c.JSON(500, echo.Map{"error": err.Error()})
	}
	return c.JSON(200, echo.Map{"data": products})
}

// ==========================================
// 3. GET BY ID
// ==========================================
func (h *ProductHandler) GetByID(c echo.Context) error {
	id := c.Param("id")

	product, err := h.DB.Product.FindUnique(
		db.Product.ID.Equals(id),
	).With(
		db.Product.Recipe.Fetch().With(
			db.ProductRecipe.SupplierProduct.Fetch(),
		),
	).Exec(c.Request().Context())

	if err != nil {
		return c.JSON(404, echo.Map{"error": "Product not found"})
	}
	return c.JSON(200, echo.Map{"data": product})
}

// ==========================================
// 4. UPDATE (Batch Transaction)
// ==========================================
func (h *ProductHandler) Update(c echo.Context) error {
	id := c.Param("id")
	req := new(ProductMixReq)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "Invalid request"})
	}

	ctx := c.Request().Context()
	
	// Gunakan interface db.PrismaTransaction
	var ops []db.PrismaTransaction

	// A. Update Produk
	var updates []db.ProductSetParam
	if req.Name != "" { updates = append(updates, db.Product.Name.Set(req.Name)) }
	if req.Price > 0 { updates = append(updates, db.Product.Price.Set(req.Price)) }
	if req.Denom > 0 { updates = append(updates, db.Product.Denom.Set(req.Denom)) }
	// Selalu update status sesuai kiriman
	updates = append(updates, db.Product.Status.Set(req.Status))

	opUpdateProd := h.DB.Product.FindUnique(db.Product.ID.Equals(id)).Update(updates...).Tx()
	ops = append(ops, opUpdateProd)

	// B. Update Resep (Wipe & Replace)
	if len(req.Recipes) > 0 {
		// 1. Hapus Resep Lama
		opDeleteOld := h.DB.ProductRecipe.FindMany(
			db.ProductRecipe.ProductID.Equals(id),
		).Delete().Tx()
		ops = append(ops, opDeleteOld)

		// 2. Buat Resep Baru
		for _, r := range req.Recipes {
			opNewRecipe := h.DB.ProductRecipe.CreateOne(
				db.ProductRecipe.Quantity.Set(r.Quantity),
				db.ProductRecipe.Product.Link(db.Product.ID.Equals(id)),
				db.ProductRecipe.SupplierProduct.Link(db.SupplierProduct.ID.Equals(r.SupplierProductID)),
			).Tx()
			ops = append(ops, opNewRecipe)
		}
	}

	// Eksekusi Batch
	if err := h.DB.Prisma.Transaction(ops...).Exec(ctx); err != nil {
		return c.JSON(500, echo.Map{"error": "Gagal update produk: " + err.Error()})
	}

	return c.JSON(200, echo.Map{"message": "Product updated successfully"})
}

// ==========================================
// 5. DELETE (Batch Transaction)
// ==========================================
func (h *ProductHandler) Delete(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	var ops []db.PrismaTransaction

	// 1. Hapus Resep (Child)
	opDeleteRecipe := h.DB.ProductRecipe.FindMany(db.ProductRecipe.ProductID.Equals(id)).Delete().Tx()
	ops = append(ops, opDeleteRecipe)
	
	// 2. Hapus Produk (Parent)
	opDeleteProd := h.DB.Product.FindUnique(db.Product.ID.Equals(id)).Delete().Tx()
	ops = append(ops, opDeleteProd)

	if err := h.DB.Prisma.Transaction(ops...).Exec(ctx); err != nil {
		return c.JSON(500, echo.Map{"error": "Gagal delete: " + err.Error()})
	}

	return c.JSON(200, echo.Map{"message": "Deleted successfully"})
}