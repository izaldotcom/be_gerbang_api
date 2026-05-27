package handlers

import (
	"gerbangapi/prisma/db"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

type PaymentTypeHandler struct {
	DB    *db.PrismaClient
	Redis *redis.Client
}

func NewPaymentTypeHandler(dbClient *db.PrismaClient, redisClient *redis.Client) *PaymentTypeHandler {
	return &PaymentTypeHandler{DB: dbClient, Redis: redisClient}
}

func (h *PaymentTypeHandler) GetAll(c echo.Context) error {
	paymentTypes, err := h.DB.PaymentType.FindMany().Exec(c.Request().Context())
	if err != nil {
		return c.JSON(500, echo.Map{"error": err.Error()})
	}
	
	return c.JSON(200, echo.Map{
		"message": "Berhasil mengambil data payment types",
		"data":    paymentTypes,
	})
}