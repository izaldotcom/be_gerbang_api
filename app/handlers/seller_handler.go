package handlers

import (
	"context"
	"gerbangapi/prisma/db"
	"time"

	"github.com/labstack/echo/v4"
)

var sellerClient *db.PrismaClient

func SetSellerClient(c *db.PrismaClient) {
    sellerClient = c
}

func SellerProducts(c echo.Context) error {
    products, err := sellerClient.InternalProduct.FindMany().Exec(context.Background())
    if err != nil {
        return c.JSON(500, echo.Map{"error": err.Error()})
    }
    return c.JSON(200, products)
}

func SellerOrder(c echo.Context) error {
    type Req struct {
        ProductID string `json:"product_id"`
        UserID    string `json:"user_id"`
    }

    req := new(Req)
    if err := c.Bind(req); err != nil {
        return c.JSON(400, echo.Map{"error": "invalid request"})
    }

    return c.JSON(200, echo.Map{
        "message": "order received (dummy)",
        "req":     req,
    })
}

func SellerStatus(c echo.Context) error {
    return c.JSON(200, echo.Map{
        "status": "OK",
        "time":   time.Now(),
    })
}
