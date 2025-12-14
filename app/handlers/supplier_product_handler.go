package handlers

import (
	"gerbangapi/prisma/db"

	"github.com/labstack/echo/v4"
)

type SupplierProductHandler struct {
	DB *db.PrismaClient
}

func NewSupplierProductHandler(dbClient *db.PrismaClient) *SupplierProductHandler {
	return &SupplierProductHandler{DB: dbClient}
}

// CREATE
func (h *SupplierProductHandler) Create(c echo.Context) error {
	type Req struct {
		SupplierID        string `json:"supplier_id"`
		SupplierProductID string `json:"supplier_product_id"` // ID asli dari web supplier
		Name              string `json:"name"`
		Denom             int    `json:"denom"`
		CostPrice         int    `json:"cost_price"`
		Price             int    `json:"price"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "Invalid request"})
	}

	sp, err := h.DB.SupplierProduct.CreateOne(
		db.SupplierProduct.SupplierProductID.Set(req.SupplierProductID),
		db.SupplierProduct.Name.Set(req.Name),
		db.SupplierProduct.Denom.Set(req.Denom),
		db.SupplierProduct.CostPrice.Set(req.CostPrice),
		db.SupplierProduct.Supplier.Link(db.Supplier.ID.Equals(req.SupplierID)), // Relasi ke Supplier
		db.SupplierProduct.Price.Set(req.Price),
		db.SupplierProduct.Status.Set(true),
	).Exec(c.Request().Context())

	if err != nil {
		return c.JSON(500, echo.Map{"error": err.Error()})
	}

	return c.JSON(201, echo.Map{"data": sp})
}

// READ ALL (Bisa filter by Supplier ID)
func (h *SupplierProductHandler) GetAll(c echo.Context) error {
	supplierID := c.QueryParam("supplier_id")
	
	var err error
	var products []db.SupplierProductModel

	if supplierID != "" {
		products, err = h.DB.SupplierProduct.FindMany(
			db.SupplierProduct.SupplierID.Equals(supplierID),
		).With(db.SupplierProduct.Supplier.Fetch()).Exec(c.Request().Context())
	} else {
		products, err = h.DB.SupplierProduct.FindMany().With(db.SupplierProduct.Supplier.Fetch()).Exec(c.Request().Context())
	}

	if err != nil {
		return c.JSON(500, echo.Map{"error": err.Error()})
	}
	return c.JSON(200, echo.Map{"data": products})
}

// UPDATE
func (h *SupplierProductHandler) Update(c echo.Context) error {
	id := c.Param("id")
	type Req struct {
		Name      string `json:"name"`
		CostPrice int    `json:"cost_price"`
		Price     int    `json:"price"`
		Status    *bool  `json:"status"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil { return c.JSON(400, echo.Map{"error": "Invalid request"}) }

	var updates []db.SupplierProductSetParam
	if req.Name != "" { updates = append(updates, db.SupplierProduct.Name.Set(req.Name)) }
	if req.CostPrice > 0 { updates = append(updates, db.SupplierProduct.CostPrice.Set(req.CostPrice)) }
	if req.Price > 0 { updates = append(updates, db.SupplierProduct.Price.Set(req.Price)) }
	if req.Status != nil { updates = append(updates, db.SupplierProduct.Status.Set(*req.Status)) }

	sp, err := h.DB.SupplierProduct.FindUnique(
		db.SupplierProduct.ID.Equals(id),
	).Update(updates...).Exec(c.Request().Context())

	if err != nil {
		return c.JSON(500, echo.Map{"error": err.Error()})
	}
	return c.JSON(200, echo.Map{"data": sp})
}

// DELETE
func (h *SupplierProductHandler) Delete(c echo.Context) error {
	id := c.Param("id")
	_, err := h.DB.SupplierProduct.FindUnique(db.SupplierProduct.ID.Equals(id)).Delete().Exec(c.Request().Context())
	if err != nil { return c.JSON(500, echo.Map{"error": err.Error()}) }
	return c.JSON(200, echo.Map{"message": "Deleted"})
}