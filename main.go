package main

import (
	"log"

	"gerbangapi/app/handlers"
	mid "gerbangapi/app/middleware"
	"gerbangapi/prisma/db"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	// 👇 1. Load file .env paling pertama!
	err := godotenv.Load()
	if err != nil {
		log.Println("⚠️  Warning: .env file not found. Menggunakan environment system variables.")
	} else {
		log.Println("✅ .env file loaded successfully.")
	}

	e := echo.New()

	// --- GLOBAL MIDDLEWARE ---
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	// 👇 2. RATE LIMITER (Sesuai Plan Week 1)
	e.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(20)))

	// 3️⃣ Connect Prisma client
	client := db.NewClient()
	if err := client.Prisma.Connect(); err != nil {
		log.Fatal("❌ Prisma failed to connect:", err)
	}
	log.Println("✅ Prisma connected successfully!")

	defer func() {
		if err := client.Prisma.Disconnect(); err != nil {
			panic(err)
		}
	}()

	// 4️⃣ Inject client ke handlers
	handlers.SetPrismaClient(client)

	// 5️⃣ Routing Grouping
	// Base URL: http://localhost:8080/api/v1
	v1 := e.Group("/api/v1")

	// === A. PUBLIC ROUTES (Tidak butuh Token) ===
	// Endpoint ini boleh diakses siapa saja (termasuk saat token mati)
	v1.POST("/register", handlers.RegisterUser)      // -> /api/v1/register
	v1.POST("/login", handlers.LoginUser)            // -> /api/v1/login
	v1.POST("/refresh-token", handlers.RefreshToken) // -> /api/v1/refresh-token (✅ SUDAH DIPERBAIKI)

	// === B. USER PROTECTED ROUTES (Butuh JWT Token) ===
	// Group ini menggunakan JWTMiddleware
	userGroup := v1.Group("")
	userGroup.Use(mid.JWTMiddleware())

	// Endpoint untuk mendapatkan profile user
	userGroup.GET("/get-profile", func(c echo.Context) error {
		return c.JSON(200, echo.Map{
			"user_id": c.Get("user_id"),
			"email":   c.Get("email"),
			"message": "You are accessing a protected route",
		})
	}) // -> /api/v1/get-profile

	// === C. SELLER ROUTES (Butuh API Key & HMAC) ===
	// Group ini terpisah dari JWT user, menggunakan SellerSecurityMiddleware
	sellerGroup := v1.Group("/seller")
	sellerGroup.Use(mid.SellerSecurityMiddleware(client))

	// Endpoint Seller
	sellerGroup.GET("/products", handlers.SellerProducts) // -> /api/v1/seller/products
	sellerGroup.POST("/order", handlers.SellerOrder)      // -> /api/v1/seller/order
	sellerGroup.GET("/status", handlers.SellerStatus)     // -> /api/v1/seller/status

	// 6️⃣ Start server
	log.Println("🚀 Server running on http://localhost:8080")
	e.Logger.Fatal(e.Start(":8080"))
}