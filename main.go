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
	// 👇 2. Load file .env paling pertama!
	// Ini penting agar JWT_SECRET terbaca sebelum digunakan oleh middleware
	err := godotenv.Load()
	if err != nil {
		log.Println("⚠️  Warning: .env file not found. Menggunakan environment system variables.")
	} else {
		log.Println("✅ .env file loaded successfully.")
	}

	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS()) // Tambahkan CORS agar aman diakses frontend

	// 1️⃣ Connect Prisma client
	client := db.NewClient()
	if err := client.Prisma.Connect(); err != nil {
		log.Fatal("❌ Prisma failed to connect:", err)
	}
	log.Println("✅ Prisma connected successfully!")

	// Pastikan disconnect saat aplikasi mati (Best Practice)
	defer func() {
		if err := client.Prisma.Disconnect(); err != nil {
			panic(err)
		}
	}()

	// 2️⃣ Inject client ke handlers
	handlers.SetPrismaClient(client)

	// 3️⃣ Routing Grouping
	// Base URL: http://localhost:8080/api/v1
	v1 := e.Group("/api/v1")

	// --- PUBLIC ROUTES (Tidak butuh Token) ---
	v1.POST("/register", handlers.RegisterUser) // -> /api/v1/register
	v1.POST("/login", handlers.LoginUser)       // -> /api/v1/login

	// --- PROTECTED ROUTES (Butuh Token JWT) ---
	// Kita buat sub-group dari v1 agar prefix-nya tetap ikut
	protected := v1.Group("")
	protected.Use(mid.JWTMiddleware())

	// Endpoint untuk mendapatkan profile
	protected.GET("/get-profile", func(c echo.Context) error {
		return c.JSON(200, echo.Map{
			"user_id": c.Get("user_id"),
			"email":   c.Get("email"),
			"message": "You are accessing a protected route",
		})
	}) // -> /api/v1/profile

	// --- Seller Routes ---
	// Menambahkan endpoint untuk seller dengan autentikasi
	protected.GET("/seller/products", handlers.SellerProducts) // -> /api/v1/seller/products
	protected.POST("/seller/order", handlers.SellerOrder)      // -> /api/v1/seller/order
	protected.GET("/seller/status", handlers.SellerStatus)     // -> /api/v1/seller/status

	// 4️⃣ Start server
	log.Println("🚀 Server running on http://localhost:8080")
	e.Logger.Fatal(e.Start(":8080"))
}