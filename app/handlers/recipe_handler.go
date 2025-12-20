package handlers

import (
	"gerbangapi/prisma/db"

	"github.com/labstack/echo/v4"
)

type RecipeHandler struct {
	DB *db.PrismaClient
}

func NewRecipeHandler(dbClient *db.PrismaClient) *RecipeHandler {
	return &RecipeHandler{DB: dbClient}
}

// ==========================================
// STRUCT REQUEST BARU (BULK / MULTIPLE)
// ==========================================

// Item kecil (bahan baku)
type RecipeItemInput struct {
	SupplierProductID string `json:"supplier_product_id"`
	Quantity          int    `json:"quantity"`
}

// Request Utama: 1 Product ID + Banyak Bahan
type RecipeBulkReq struct {
	ProductID string            `json:"product_id"`
	Items     []RecipeItemInput `json:"items"`
}

// ==========================================
// 1. CREATE BULK RECIPE (Multiple Ingredients)
// ==========================================
func (h *RecipeHandler) Create(c echo.Context) error {
	req := new(RecipeBulkReq)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "Invalid JSON format"})
	}

	// Validasi Input Dasar
	if req.ProductID == "" {
		return c.JSON(400, echo.Map{"error": "product_id required"})
	}
	if len(req.Items) == 0 {
		return c.JSON(400, echo.Map{"error": "items list cannot be empty"})
	}

	ctx := c.Request().Context()

	// Gunakan Transaction agar semua bahan masuk atau gagal sama sekali
	var ops []db.PrismaTransaction

	// Opsional: Hapus resep lama dulu? (Uncomment jika ingin mode "Replace All")
	// opDelete := h.DB.ProductRecipe.FindMany(db.ProductRecipe.ProductID.Equals(req.ProductID)).Delete().Tx()
	// ops = append(ops, opDelete)

	// Loop semua item di request
	for _, item := range req.Items {
		if item.Quantity <= 0 {
			return c.JSON(400, echo.Map{"error": "Quantity must be > 0 for item " + item.SupplierProductID})
		}

		// Siapkan operasi Create
		op := h.DB.ProductRecipe.CreateOne(
			db.ProductRecipe.Quantity.Set(item.Quantity),
			db.ProductRecipe.Product.Link(
				db.Product.ID.Equals(req.ProductID), // Link ke 1 Product ID yang sama
			),
			db.ProductRecipe.SupplierProduct.Link(
				db.SupplierProduct.ID.Equals(item.SupplierProductID), // Link ke Supplier Product beda-beda
			),
		).Tx()

		ops = append(ops, op)
	}

	// Eksekusi Batch Transaction
	if err := h.DB.Prisma.Transaction(ops...).Exec(ctx); err != nil {
		return c.JSON(500, echo.Map{"error": "Gagal menyimpan resep: " + err.Error()})
	}

	return c.JSON(201, echo.Map{
		"message":    "Recipes added successfully",
		"product_id": req.ProductID,
		"items_count": len(req.Items),
	})
}

// ==========================================
// 2. GET ALL RECIPES (Filter by Product)
// ==========================================
func (h *RecipeHandler) GetAll(c echo.Context) error {
	productID := c.QueryParam("product_id")
	
	ctx := c.Request().Context()
	var recipes []db.ProductRecipeModel
	var err error

	if productID != "" {
		// Ambil resep spesifik untuk 1 produk
		recipes, err = h.DB.ProductRecipe.FindMany(
			db.ProductRecipe.ProductID.Equals(productID),
		).With(
			db.ProductRecipe.Product.Fetch(),
			db.ProductRecipe.SupplierProduct.Fetch(),
		).Exec(ctx)
	} else {
		// Ambil semua resep
		recipes, err = h.DB.ProductRecipe.FindMany().With(
			db.ProductRecipe.Product.Fetch(),
			db.ProductRecipe.SupplierProduct.Fetch(),
		).Exec(ctx)
	}

	if err != nil {
		return c.JSON(500, echo.Map{"error": err.Error()})
	}

	return c.JSON(200, echo.Map{"data": recipes})
}

// ==========================================
// 3. GET BY ID (Detail Item Resep)
// ==========================================
func (h *RecipeHandler) GetByID(c echo.Context) error {
	id := c.Param("id")

	recipe, err := h.DB.ProductRecipe.FindUnique(
		db.ProductRecipe.ID.Equals(id),
	).With(
		db.ProductRecipe.Product.Fetch(),
		db.ProductRecipe.SupplierProduct.Fetch(),
	).Exec(c.Request().Context())

	if err != nil {
		return c.JSON(404, echo.Map{"error": "Recipe item not found"})
	}

	return c.JSON(200, echo.Map{"data": recipe})
}

// ==========================================
// 4. A. UPDATE ONE ITEM (Hanya Edit Quantity)
// Endpoint: PUT /recipes/:id
// ==========================================
func (h *RecipeHandler) UpdateItem(c echo.Context) error {
	// Ambil ID dari URL (Prioritas Utama)
	paramID := c.Param("id")
	
	type UpdateReq struct {
		ID       string `json:"id"`       // <-- Tambahan Param ID di Body
		Quantity int    `json:"quantity"`
	}
	req := new(UpdateReq)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "Invalid request"})
	}

	// Logika Penentuan ID Target:
	// Gunakan ID dari URL jika ada. Jika tidak, coba ambil dari Body.
	targetID := paramID
	if targetID == "" {
		targetID = req.ID
	}

	// Validasi Akhir
	if targetID == "" {
		return c.JSON(400, echo.Map{"error": "ID Recipe Item harus diisi (via URL atau JSON body)"})
	}

	if req.Quantity <= 0 {
		return c.JSON(400, echo.Map{"error": "Quantity harus lebih dari 0"})
	}

	// Eksekusi Update
	recipe, err := h.DB.ProductRecipe.FindUnique(
		db.ProductRecipe.ID.Equals(targetID),
	).Update(
		db.ProductRecipe.Quantity.Set(req.Quantity),
	).Exec(c.Request().Context())

	if err != nil {
		return c.JSON(500, echo.Map{"error": "Gagal update item: " + err.Error()})
	}

	return c.JSON(200, echo.Map{
		"message": "Item updated successfully", 
		"data": recipe,
	})
}

// ==========================================
// 4. B. REPLACE ALL (Ganti Total Resep Produk)
// Endpoint: PUT /recipes/replace
// ==========================================
func (h *RecipeHandler) ReplaceAll(c echo.Context) error {
	// Gunakan Struct yang sama dengan Bulk Create
	req := new(RecipeBulkReq)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "Invalid JSON format"})
	}

	if req.ProductID == "" {
		return c.JSON(400, echo.Map{"error": "product_id required"})
	}

	ctx := c.Request().Context()

	// STRATEGI: WIPE & REPLACE (Hapus Lama -> Masukkan Baru)
	var ops []db.PrismaTransaction

	// 1. Operasi Hapus Semua Resep Lama milik ProductID ini
	opDelete := h.DB.ProductRecipe.FindMany(
		db.ProductRecipe.ProductID.Equals(req.ProductID),
	).Delete().Tx()
	
	ops = append(ops, opDelete)

	// 2. Operasi Masukkan Resep Baru (Looping)
	if len(req.Items) > 0 {
		for _, item := range req.Items {
			if item.Quantity <= 0 { continue } // Skip invalid qty

			opCreate := h.DB.ProductRecipe.CreateOne(
				db.ProductRecipe.Quantity.Set(item.Quantity),
				db.ProductRecipe.Product.Link(
					db.Product.ID.Equals(req.ProductID),
				),
				db.ProductRecipe.SupplierProduct.Link(
					db.SupplierProduct.ID.Equals(item.SupplierProductID),
				),
			).Tx()

			ops = append(ops, opCreate)
		}
	}

	// 3. Eksekusi Transaksi
	if err := h.DB.Prisma.Transaction(ops...).Exec(ctx); err != nil {
		return c.JSON(500, echo.Map{"error": "Gagal mengganti resep: " + err.Error()})
	}

	return c.JSON(200, echo.Map{
		"message":    "Recipe replaced successfully",
		"product_id": req.ProductID,
		"new_items":  len(req.Items),
	})
}

// ==========================================
// 5. DELETE (Hapus Satu Item dari Resep)
// ==========================================
func (h *RecipeHandler) Delete(c echo.Context) error {
	id := c.Param("id") // ID baris resep

	_, err := h.DB.ProductRecipe.FindUnique(
		db.ProductRecipe.ID.Equals(id),
	).Delete().Exec(c.Request().Context())

	if err != nil {
		return c.JSON(500, echo.Map{"error": "Gagal delete: " + err.Error()})
	}

	return c.JSON(200, echo.Map{"message": "Recipe item deleted"})
}