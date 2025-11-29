package middleware

import (
	"net/http"
	"os"
	"strings" // Tambahkan strings

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
)

func JWTMiddleware() echo.MiddlewareFunc {
	// Secret dibaca saat middleware diinisialisasi di main() (sudah aman karena setelah load env)
	secret := []byte(os.Getenv("JWT_SECRET"))

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth := c.Request().Header.Get("Authorization")
			
			// Cek apakah kosong atau format salah
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
				return c.JSON(http.StatusUnauthorized, echo.Map{"error": "missing or invalid token format"})
			}

			// Ambil token setelah "Bearer "
			tokenStr := strings.TrimPrefix(auth, "Bearer ")

			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
				return secret, nil
			})

			if err != nil || !token.Valid {
				return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid token"})
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid claims"})
			}

			c.Set("user_id", claims["user_id"])
			c.Set("email", claims["email"])
			return next(c)
		}
	}
}