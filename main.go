package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"gerbangapi/app/handlers"
	"gerbangapi/app/routes"
	"gerbangapi/app/services"
	"gerbangapi/app/worker"
	"gerbangapi/prisma/db"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/redis/go-redis/v9"
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
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379" // Default address
	}

	// Ambil password dari .env untuk production
	redisPassword := os.Getenv("REDIS_PASSWORD")

	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       0,
	})

	// Cek koneksi Redis
	if _, err := redisClient.Ping(context.Background()).Result(); err != nil {
		log.Printf("⚠️  Warning: Gagal connect ke Redis: %v", err)
	} else {
		log.Println("✅ Redis Connected")
	}

	// ---------------------------------------------------------
	// [2] START WORKER (BACKGROUND)
	// ---------------------------------------------------------
	// Worker berjalan otomatis di goroutine terpisah untuk memantau order
	worker.StartWorker(client, redisClient)

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
	authService := services.NewAuthService(client, redisClient)
	orderService := services.NewOrderService(client)

	// B. Handlers
	authHandler := handlers.NewAuthHandler(authService)
	sellerHandler := handlers.NewSellerHandler(client, orderService, redisClient)
	
	// [BARU] Inisialisasi Telegram Handler untuk Deep Linking
	telegramHandler := handlers.NewTelegramHandler(client) 

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
		telegramHandler, // Hanya TelegramHandler yang ditambahkan
	)

	// 7. Start Server
	log.Printf("🚀 Server running on http://localhost%s", serverAddress)
	e.Logger.Fatal(e.Start(serverAddress))
}