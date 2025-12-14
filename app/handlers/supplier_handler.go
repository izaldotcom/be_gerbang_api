package handlers

import (
	"gerbangapi/prisma/db"

	"github.com/labstack/echo/v4"
)

type SupplierHandler struct {
	DB *db.PrismaClient
}

func NewSupplierHandler(dbClient *db.PrismaClient) *SupplierHandler {
	return &SupplierHandler{DB: dbClient}
}

// CREATE
func (h *SupplierHandler) Create(c echo.Context) error {
	type Req struct {
		Name    string `json:"name"`
		Code    string `json:"code"`     // ex: "HIGGS_OFFICIAL"
		Type    string `json:"type"`     // ex: "scraper"
		BaseURL string `json:"base_url"` // Optional
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "Invalid request"})
	}

	// FIX URUTAN CREATEONE:
	// 1. Name (Wajib)
	// 2. Type (Wajib)
	// 3. Code (Opsional - karena ada @default)
	// 4. BaseURL (Opsional)
	// 5. Status (Opsional)
	supplier, err := h.DB.Supplier.CreateOne(
		// --- ARGUMEN WAJIB (Harus di Atas) ---
		db.Supplier.Name.Set(req.Name),
		db.Supplier.Type.Set(req.Type), // Pindahkan Type ke sini!

		// --- ARGUMEN OPSIONAL (Harus di Bawah) ---
		db.Supplier.Code.Set(req.Code), // Code pindah ke bawah karena punya @default
		db.Supplier.BaseURL.SetIfPresent(&req.BaseURL),
		db.Supplier.Status.Set(true),
	).Exec(c.Request().Context())

	if err != nil {
		return c.JSON(500, echo.Map{"error": err.Error()})
	}

	return c.JSON(201, echo.Map{"message": "Supplier created", "data": supplier})
}

// READ ALL
func (h *SupplierHandler) GetAll(c echo.Context) error {
	suppliers, err := h.DB.Supplier.FindMany().Exec(c.Request().Context())
	if err != nil {
		return c.JSON(500, echo.Map{"error": err.Error()})
	}
	return c.JSON(200, echo.Map{"data": suppliers})
}

// UPDATE
func (h *SupplierHandler) Update(c echo.Context) error {
	id := c.Param("id")
	type Req struct {
		Name    string `json:"name"`
		Code    string `json:"code"`
		Type    string `json:"type"`
		BaseURL string `json:"base_url"`
		Status  *bool  `json:"status"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "Invalid request"})
	}

	var updates []db.SupplierSetParam
	if req.Name != "" {
		updates = append(updates, db.Supplier.Name.Set(req.Name))
	}
	if req.Code != "" {
		updates = append(updates, db.Supplier.Code.Set(req.Code))
	}
	if req.Type != "" {
		updates = append(updates, db.Supplier.Type.Set(req.Type))
	}
	if req.BaseURL != "" {
		updates = append(updates, db.Supplier.BaseURL.Set(req.BaseURL))
	}
	if req.Status != nil {
		updates = append(updates, db.Supplier.Status.Set(*req.Status))
	}

	supplier, err := h.DB.Supplier.FindUnique(
		db.Supplier.ID.Equals(id),
	).Update(updates...).Exec(c.Request().Context())

	if err != nil {
		return c.JSON(500, echo.Map{"error": err.Error()})
	}

	return c.JSON(200, echo.Map{"message": "Updated", "data": supplier})
}

// DELETE
func (h *SupplierHandler) Delete(c echo.Context) error {
	id := c.Param("id")
	_, err := h.DB.Supplier.FindUnique(db.Supplier.ID.Equals(id)).Delete().Exec(c.Request().Context())
	if err != nil {
		return c.JSON(500, echo.Map{"error": err.Error()})
	}
	return c.JSON(200, echo.Map{"message": "Deleted"})
}