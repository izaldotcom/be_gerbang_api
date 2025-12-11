package main

import (
	"log"

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
		log.Println("⚠️  Warning: .env file not found. Menggunakan environment system variables.")
	} else {
		log.Println("✅ .env file loaded successfully.")
	}

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
	// 4️⃣ HANDLERS & ROUTING
	// ---------------------------------------------------------

	// 4.1. Initialize Services
	orderService := services.NewOrderService(client)
	
	// 4.2. Initialize Handlers (SellerHandler requires DB and OrderService)
	sellerHandler := handlers.NewSellerHandler(client, orderService)

	// 5️⃣ Routing Grouping
	v1 := e.Group("/api/v1")

	// === A. PUBLIC ROUTES (Asumsi ini adalah fungsi standalone) ===
	v1.POST("/register", handlers.RegisterUser)
	v1.POST("/login", handlers.LoginUser)
	v1.POST("/refresh-token", handlers.RefreshToken)

	// === B. USER PROTECTED ROUTES ===
	userGroup := v1.Group("")
	
	// FIX: Panggil tanpa argumen, sesuai error kompilasi
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

	// Menggunakan instance sellerHandler untuk memanggil metodenya
	sellerGroup.GET("/products", sellerHandler.SellerProducts) 
	sellerGroup.POST("/order", sellerHandler.SellerOrder)
	
	// Asumsi SellerStatus juga merupakan metode dari SellerHandler
	// Karena SellerStatus tidak ada di kode handler sebelumnya, kita buat placeholder.
	sellerGroup.GET("/status", func(c echo.Context) error {
		// Jika Anda memiliki metode SellerStatus di SellerHandler, gunakan:
		// sellerHandler.SellerStatus
		return c.JSON(200, echo.Map{"message": "Seller status endpoint"})
	})

	// 6️⃣ Start server
	log.Println("🚀 Server running on http://localhost:8080")
	e.Logger.Fatal(e.Start(":8080"))
}