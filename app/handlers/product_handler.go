package handlers

import (
	"gerbangapi/prisma/db"

	"github.com/labstack/echo/v4"
)

type ProductHandler struct {
	DB *db.PrismaClient
}

func NewProductHandler(dbClient *db.PrismaClient) *ProductHandler {
	return &ProductHandler{DB: dbClient}
}

// CREATE
func (h *ProductHandler) Create(c echo.Context) error {
	type Req struct {
		Name  string `json:"name"`
		Denom int    `json:"denom"`
		Price int    `json:"price"`
		Qty   int    `json:"qty"` // PENTING: Untuk logic looping (5M = 5)
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "Invalid request"})
	}

	product, err := h.DB.Product.CreateOne(
		db.Product.Name.Set(req.Name),
		db.Product.Denom.Set(req.Denom),
		db.Product.Price.Set(req.Price),
		db.Product.Qty.Set(req.Qty),
		db.Product.Status.Set(true),
	).Exec(c.Request().Context())

	if err != nil {
		return c.JSON(500, echo.Map{"error": err.Error()})
	}

	return c.JSON(201, echo.Map{"data": product})
}

// READ ALL
func (h *ProductHandler) GetAll(c echo.Context) error {
	products, err := h.DB.Product.FindMany().Exec(c.Request().Context())
	if err != nil {
		return c.JSON(500, echo.Map{"error": err.Error()})
	}
	return c.JSON(200, echo.Map{"data": products})
}

// UPDATE
func (h *ProductHandler) Update(c echo.Context) error {
	id := c.Param("id")
	type Req struct {
		Name   string `json:"name"`
		Price  int    `json:"price"`
		Qty    int    `json:"qty"`
		Status *bool  `json:"status"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil { return c.JSON(400, echo.Map{"error": "Invalid request"}) }

	var updates []db.ProductSetParam
	if req.Name != "" { updates = append(updates, db.Product.Name.Set(req.Name)) }
	if req.Price > 0 { updates = append(updates, db.Product.Price.Set(req.Price)) }
	if req.Qty > 0 { updates = append(updates, db.Product.Qty.Set(req.Qty)) }
	if req.Status != nil { updates = append(updates, db.Product.Status.Set(*req.Status)) }

	product, err := h.DB.Product.FindUnique(
		db.Product.ID.Equals(id),
	).Update(updates...).Exec(c.Request().Context())

	if err != nil {
		return c.JSON(500, echo.Map{"error": err.Error()})
	}
	return c.JSON(200, echo.Map{"data": product})
}

// DELETE
func (h *ProductHandler) Delete(c echo.Context) error {
	id := c.Param("id")
	_, err := h.DB.Product.FindUnique(db.Product.ID.Equals(id)).Delete().Exec(c.Request().Context())
	if err != nil { return c.JSON(500, echo.Map{"error": err.Error()}) }
	return c.JSON(200, echo.Map{"message": "Deleted"})
}