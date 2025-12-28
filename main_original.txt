package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"gerbangapi/app/handlers"
	"gerbangapi/app/routes"
	"gerbangapi/app/services"
	"gerbangapi/prisma/db"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/redis/go-redis/v9" // <--- Import Redis
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

	// 2. Setup DB Connections (Prisma)
	client := db.NewClient()
	if err := client.Prisma.Connect(); err != nil {
		log.Fatal("❌ Prisma failed to connect:", err)
	}
	defer client.Prisma.Disconnect()

	// 3. Setup Redis Connection
	// Kita inisialisasi disini agar variabel 'redisClient' bisa di-passing ke Service
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379" // Default address
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	// Cek koneksi Redis
	if _, err := redisClient.Ping(context.Background()).Result(); err != nil {
		log.Printf("⚠️  Warning: Gagal connect ke Redis: %v", err)
	} else {
		log.Println("✅ Redis Connected")
	}

	// 4. Create Echo Instance & Global Middleware
	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())
	e.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(20)))

	// ---------------------------------------------------------
	// 5. DEPENDENCY INJECTION (Services & Handlers)
	// ---------------------------------------------------------

	// A. Services
	// [UPDATE] Inject Redis Client ke Auth Service
	authService := services.NewAuthService(client, redisClient)
	
	orderService := services.NewOrderService(client)

	// B. Handlers
	authHandler := handlers.NewAuthHandler(authService)
	sellerHandler := handlers.NewSellerHandler(client, orderService, redisClient)

	// CRUD Handlers
	supplierHandler := handlers.NewSupplierHandler(client)
	supplierProductHandler := handlers.NewSupplierProductHandler(client)
	productHandler := handlers.NewProductHandler(client)
	recipeHandler := handlers.NewRecipeHandler(client)

	// ---------------------------------------------------------
	// 6. REGISTER ROUTES
	// ---------------------------------------------------------
	routes.Init(
		e,
		client,
		authHandler,
		sellerHandler,
		supplierHandler,
		supplierProductHandler,
		productHandler,
		recipeHandler,
	)

	// 7. Start Server
	log.Printf("🚀 Server running on http://localhost%s", serverAddress)
	e.Logger.Fatal(e.Start(serverAddress))
}