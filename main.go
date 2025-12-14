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
	e.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(20)))

	// ---------------------------------------------------------
	// 3️⃣ DATABASE CONNECTIONS
	// ---------------------------------------------------------
	client := db.NewClient()
	if err := client.Prisma.Connect(); err != nil {
		log.Fatal("❌ Prisma failed to connect:", err)
	}
	defer func() {
		if err := client.Prisma.Disconnect(); err != nil {
			panic(err)
		}
	}()

	db.ConnectRedis()

	// ---------------------------------------------------------
	// 4️⃣ SERVICES & HANDLERS
	// ---------------------------------------------------------
	authService := services.NewAuthService(client)
	orderService := services.NewOrderService(client)

	authHandler := handlers.NewAuthHandler(authService)
	sellerHandler := handlers.NewSellerHandler(client, orderService)

	// CRUD Handlers
	supplierHandler := handlers.NewSupplierHandler(client)
	supplierProductHandler := handlers.NewSupplierProductHandler(client)
	productHandler := handlers.NewProductHandler(client)

	// ---------------------------------------------------------
	// 5️⃣ ROUTING
	// ---------------------------------------------------------
	v1 := e.Group("/api/v1")

	// === A. PUBLIC ROUTES (Tanpa Token) ===
	v1.POST("/register", authHandler.RegisterUser)
	v1.POST("/login", authHandler.LoginUser)
	v1.POST("/refresh-token", authHandler.RefreshToken)

	// === B. PROTECTED ROUTES (Butuh Bearer Token / JWT) ===
	// Kita buat Group baru yang menggunakan JWT Middleware
	protected := v1.Group("")
	protected.Use(mid.JWTMiddleware())

	// 1. Internal Products (CRUD) - SEKARANG SECURE 🔒
	protected.POST("/products", productHandler.Create)
	protected.GET("/products", productHandler.GetAll)
	protected.PUT("/products/:id", productHandler.Update)
	protected.DELETE("/products/:id", productHandler.Delete)

	// 2. Suppliers (CRUD) - SEKARANG SECURE 🔒
	protected.POST("/suppliers", supplierHandler.Create)
	protected.GET("/suppliers", supplierHandler.GetAll)
	protected.PUT("/suppliers/:id", supplierHandler.Update)
	protected.DELETE("/suppliers/:id", supplierHandler.Delete)

	// 3. Supplier Products (CRUD) - SEKARANG SECURE 🔒
	protected.POST("/supplier-products", supplierProductHandler.Create)
	protected.GET("/supplier-products", supplierProductHandler.GetAll)
	protected.PUT("/supplier-products/:id", supplierProductHandler.Update)
	protected.DELETE("/supplier-products/:id", supplierProductHandler.Delete)

	// 4. User Profile
	protected.GET("/get-profile", func(c echo.Context) error {
		return c.JSON(200, echo.Map{
			"user_id": c.Get("user_id"),
			"email":   c.Get("email"),
			"message": "You are accessing a protected route",
		})
	})

	// === C. SELLER ROUTES (Butuh API KEY) ===
	// Ini terpisah karena menggunakan mekanisme X-Seller-Key, bukan JWT User
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