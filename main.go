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

	// 👇 2. RATE LIMITER
	e.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(20)))

	// ---------------------------------------------------------
	// 3️⃣ DATABASE CONNECTIONS (MySQL & Redis)
	// ---------------------------------------------------------
	
	// A. Connect Prisma (MySQL)
	client := db.NewClient()
	if err := client.Prisma.Connect(); err != nil {
		log.Fatal("❌ Prisma failed to connect:", err)
	}
	log.Println("✅ Prisma connected successfully!")

	// Pastikan disconnect saat aplikasi mati
	defer func() {
		if err := client.Prisma.Disconnect(); err != nil {
			panic(err)
		}
	}()

	// B. Connect Redis (👇 TAMBAHKAN INI DI SINI)
	// Fungsi ini akan menginisialisasi variabel global db.Rdb
	db.ConnectRedis() 

	// ---------------------------------------------------------
	// 4️⃣ HANDLERS & ROUTING
	// ---------------------------------------------------------

	// Inject client ke handlers
	handlers.SetPrismaClient(client)

	// 5️⃣ Routing Grouping
	// Base URL: http://localhost:8080/api/v1
	v1 := e.Group("/api/v1")

	// === A. PUBLIC ROUTES ===
	v1.POST("/register", handlers.RegisterUser)      
	v1.POST("/login", handlers.LoginUser)            
	v1.POST("/refresh-token", handlers.RefreshToken) 

	// === B. USER PROTECTED ROUTES ===
	userGroup := v1.Group("")
	userGroup.Use(mid.JWTMiddleware())

	userGroup.GET("/get-profile", func(c echo.Context) error {
		return c.JSON(200, echo.Map{
			"user_id": c.Get("user_id"),
			"email":   c.Get("email"),
			"message": "You are accessing a protected route",
		})
	})

	// === C. SELLER ROUTES ===
	sellerGroup := v1.Group("/seller")
	sellerGroup.Use(mid.SellerSecurityMiddleware(client))

	sellerGroup.GET("/products", handlers.SellerProducts) 
	sellerGroup.POST("/order", handlers.SellerOrder)      
	sellerGroup.GET("/status", handlers.SellerStatus)     

	// 6️⃣ Start server
	log.Println("🚀 Server running on http://localhost:8080")
	e.Logger.Fatal(e.Start(":8080"))
}