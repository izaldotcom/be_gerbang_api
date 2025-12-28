package middleware

import (
	"context"
	"gerbangapi/prisma/db"
	"net/http"

	"github.com/labstack/echo/v4"
)

// SellerSecurityMiddleware: Cek API Key
func SellerSecurityMiddleware(client *db.PrismaClient) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// 1. Ambil Header X-API-KEY
			apiKey := c.Request().Header.Get("X-API-KEY")

			if apiKey == "" {
				return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Missing X-API-KEY header"})
			}

			// 2. Cek ke Database
			// Perbaikan: Gunakan 'client.ApiKey' (Sesuai nama model di schema.prisma)
			// Field: 'db.ApiKey.APIKey' (Biasanya Prisma Go mengkapitalisasi acronym)
			keyData, err := client.APIKey.FindUnique(
				db.APIKey.APIKey.Equals(apiKey),
			).Exec(context.Background())

			if err != nil || keyData == nil {
				return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Invalid API Key"})
			}

			// 3. Cek Status Aktif
			if !keyData.IsActive {
				return c.JSON(http.StatusForbidden, echo.Map{"error": "API Key is inactive"})
			}

			// 4. Sukses: Simpan User ID ke context
			// [PENTING] String ini harus "user_id" agar cocok dengan Handler (c.Get("user_id"))
			c.Set("user_id", keyData.UserID)
			
			// Lanjut ke endpoint berikutnya
			return next(c)
		}
	}
}