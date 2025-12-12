package main

import (
	"fmt"
	"log"
	"os"

	"gerbangapi/app/handlers"
	mid "gerbangapi/app/middleware"
	"gerbangapi/app/services"
	"gerbangapi/prisma/db"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	// 1. Load file .env
	err := godotenv.Load()
	if err != nil {
		log.Println("⚠️  Warning: .env file not found. Menggunakan environment system variables.")
	} else {
		log.Println("✅ .env file loaded successfully.")
	}

	// 1.1. Tentukan Port
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	serverAddress := fmt.Sprintf(":%s", port)

	// Create Echo instance
	e := echo.New()

	// --- GLOBAL MIDDLEWARE ---
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	// 2. RATE LIMITER
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

	// Ensure disconnect when the app shuts down
	defer func() {
		if err := client.Prisma.Disconnect(); err != nil {
			panic(err)
		}
	}()

	// B. Connect Redis
	db.ConnectRedis()

	// ---------------------------------------------------------
	// 4️⃣ HANDLERS & ROUTING (Dependency Injection)
	// ---------------------------------------------------------

	// 4.1. Initialize Services
	// ✅ FIX: Inisialisasi Auth Service dengan client DB
	authService := services.NewAuthService(client)
	orderService := services.NewOrderService(client)

	// 4.2. Initialize Handlers
	// ✅ FIX: Inisialisasi Auth Handler dengan Auth Service
	authHandler := handlers.NewAuthHandler(authService)
	sellerHandler := handlers.NewSellerHandler(client, orderService)

	// 5️⃣ Routing Grouping
	v1 := e.Group("/api/v1")

	// === A. PUBLIC ROUTES ===
	// ✅ FIX: Gunakan method dari authHandler (instance), bukan fungsi package langsung
	v1.POST("/register", authHandler.RegisterUser)
	v1.POST("/login", authHandler.LoginUser)
	// Pastikan RefreshToken juga ada di AuthHandler jika logic-nya butuh DB
	// Jika RefreshToken function mandiri, biarkan, tapi biasanya butuh DB:
	// v1.POST("/refresh-token", authHandler.RefreshToken) 
	v1.POST("/refresh-token", authHandler.RefreshToken)

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

	sellerGroup.GET("/products", sellerHandler.SellerProducts)
	sellerGroup.POST("/order", sellerHandler.SellerOrder)

	sellerGroup.GET("/status", func(c echo.Context) error {
		return c.JSON(200, echo.Map{"message": "Seller status endpoint"})
	})

	// 6️⃣ Start server
	log.Printf("🚀 Server running on http://localhost%s", serverAddress)
	e.Logger.Fatal(e.Start(serverAddress))
}