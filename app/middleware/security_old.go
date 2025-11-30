package middleware

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"gerbangapi/prisma/db"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"
)

// SellerSecurityMiddleware memvalidasi API Key dan HMAC Signature
func SellerSecurityMiddlewareOld(client *db.PrismaClient) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// 1. Ambil Header
			apiKey := c.Request().Header.Get("X-API-KEY")
			signature := c.Request().Header.Get("X-SIGNATURE")

			if apiKey == "" || signature == "" {
				return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Missing API Key or Signature"})
			}

			// 2. Cari API Key di Database (Tabel api_key)
			// Context background digunakan agar tidak membatalkan query database jika request putus mendadak
			keyData, err := client.APIKey.FindUnique(
				db.APIKey.APIKey.Equals(apiKey),
			).Exec(context.Background())

			if err != nil || keyData == nil {
				return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Invalid API Key"})
			}

			// Cek apakah key aktif
			if !keyData.IsActive {
				return c.JSON(http.StatusForbidden, echo.Map{"error": "API Key is inactive"})
			}

			// 3. Baca Body Request (Payload) untuk validasi Signature
			// Kita perlu membaca body, lalu mengembalikannya lagi agar bisa dibaca oleh Handler selanjutnya
			bodyBytes, err := io.ReadAll(c.Request().Body)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Failed to read request body"})
			}
			// Restore body agar bisa dibaca di handler/controller
			c.Request().Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			// 4. Hitung HMAC-SHA256
			// Rumus: HMAC_SHA256(payload, secret_key_dari_db)
			calculatedSignature := generateHMAC(string(bodyBytes), keyData.Secret)

			// 5. Bandingkan Signature (Client vs Server)
			if signature != calculatedSignature {
				return c.JSON(http.StatusUnauthorized, echo.Map{
					"error": "Invalid Signature (HMAC Mismatch)",
					// "debug_calc": calculatedSignature, // Hapus baris ini di production!
				})
			}

			// Jika lolos, simpan data user/seller ke context agar bisa dipakai di controller
			c.Set("seller_id", keyData.UserID)
			c.Set("api_key_id", keyData.ID)

			return next(c)
		}
	}
}

// Helper function untuk membuat HMAC SHA256
func generateHMAC(payload, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}