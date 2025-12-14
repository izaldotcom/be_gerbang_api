package main

import (
	"fmt"
	"log"
	"os"

	"gerbangapi/app/handlers"
	"gerbangapi/app/routes" // Import package routes yang baru dibuat
	"gerbangapi/app/services"
	"gerbangapi/prisma/db"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	// 1. Load Env
	err := godotenv.Load()
	if err != nil {
		log.Println("⚠️  Warning: .env file not found.")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	serverAddress := fmt.Sprintf(":%s", port)

	// 2. Setup DB Connections
	client := db.NewClient()
	if err := client.Prisma.Connect(); err != nil {
		log.Fatal("❌ Prisma failed to connect:", err)
	}
	defer client.Prisma.Disconnect()

	db.ConnectRedis()

	// 3. Create Echo Instance & Global Middleware
	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())
	e.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(20)))

	// ---------------------------------------------------------
	// 4. DEPENDENCY INJECTION (Services & Handlers)
	// ---------------------------------------------------------
	
	// A. Services
	authService := services.NewAuthService(client)
	orderService := services.NewOrderService(client)

	// B. Handlers
	authHandler := handlers.NewAuthHandler(authService)
	sellerHandler := handlers.NewSellerHandler(client, orderService)
	
	// CRUD Handlers
	supplierHandler := handlers.NewSupplierHandler(client)
	supplierProductHandler := handlers.NewSupplierProductHandler(client)
	productHandler := handlers.NewProductHandler(client)

	// ---------------------------------------------------------
	// 5. REGISTER ROUTES (Panggil fungsi dari package routes)
	// ---------------------------------------------------------
	routes.Init(
		e, 
		client, 
		authHandler, 
		sellerHandler, 
		supplierHandler, 
		supplierProductHandler, 
		productHandler,
	)

	// 6. Start Server
	log.Printf("🚀 Server running on http://localhost%s", serverAddress)
	e.Logger.Fatal(e.Start(serverAddress))
}