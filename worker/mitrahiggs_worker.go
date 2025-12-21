package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"gerbangapi/app/services/scraper"
	"gerbangapi/prisma/db"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9" // [BARU] Import Redis
)

// Gunakan context background untuk proses worker yang berjalan terus menerus
var ctx = context.Background()

func main() {
	// Load env agar bisa baca MH_USERNAME/PASSWORD
	if err := godotenv.Load(".env"); err != nil {
		if err2 := godotenv.Load("../.env"); err2 != nil {
			log.Println("⚠️  Warning: .env file not found. Menggunakan System Env.")
		}
	}

	log.Println("🚀 Starting MitraHiggs Order Worker...")

	// 1. Inisialisasi DB Client
	dbClient := db.NewClient()
	if err := dbClient.Prisma.Connect(); err != nil {
		log.Fatalf("Fatal Error: Failed to connect to database: %v", err)
	}
	defer dbClient.Prisma.Disconnect()

	// 2. [BARU] Inisialisasi Redis Client secara manual (Sama seperti di main.go)
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	// Cek koneksi Redis
	if _, err := redisClient.Ping(ctx).Result(); err != nil {
		log.Printf("⚠️  Warning: Gagal connect ke Redis: %v", err)
	} else {
		log.Println("✅ Database & Redis Connected. Worker is running...")
	}

	// Worker akan berputar terus menerus
	for {
		// [UPDATE] Kirim redisClient ke fungsi process
		err := processNextSupplierOrder(dbClient, redisClient)
		if err != nil {
			log.Printf("❌ Worker Error: %v", err)
		}

		// Jeda sebelum mengecek antrian lagi (misalnya 5 detik)
		time.Sleep(5 * time.Second)
	}
}

// [UPDATE] Terima parameter redisClient
func processNextSupplierOrder(dbClient *db.PrismaClient, redisClient *redis.Client) error {
	// A. Ambil order 'pending' pertama
	supplierOrder, err := dbClient.SupplierOrder.FindFirst(
		db.SupplierOrder.Status.Equals("pending"),
		db.SupplierOrder.SupplierID.Equals("mitra-higgs"),
	).Exec(ctx)

	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil // Tidak ada antrian
		}
		return fmt.Errorf("failed to fetch pending order: %v", err)
	}

	orderID := supplierOrder.ID
	log.Printf("🔥 Processing Order #%s", orderID)

	// B. Tandai order sebagai 'processing'
	_, err = dbClient.Prisma.ExecuteRaw(
		"UPDATE supplier_order SET status='processing' WHERE id=?",
		orderID,
	).Exec(ctx)

	if err != nil {
		return fmt.Errorf("failed to mark order as processing: %v", err)
	}

	// C. Panggil Scraper (Core Logic)
	// [UPDATE] Pass redisClient ke fungsi eksekusi
	err = executeScrapingOrder(dbClient, supplierOrder, redisClient)

	// D. Handle Hasil (Success/Fail)
	if err != nil {
		log.Printf("⚠️ Order #%s FAILED: %v", orderID, err)

		errMsg := err.Error()
		dbClient.Prisma.ExecuteRaw(
			"UPDATE supplier_order SET status='failed', last_error=? WHERE id=?",
			errMsg,
			orderID,
		).Exec(ctx)

		dbClient.Prisma.ExecuteRaw(
			"UPDATE internal_order SET status='failed' WHERE id=?",
			supplierOrder.InternalOrderID,
		).Exec(ctx)

		return nil
	}

	log.Printf("✅ Order #%s Success!", orderID)

	dbClient.Prisma.ExecuteRaw(
		"UPDATE supplier_order SET status='success' WHERE id=?",
		orderID,
	).Exec(ctx)

	dbClient.Prisma.ExecuteRaw(
		"UPDATE internal_order SET status='success' WHERE id=?",
		supplierOrder.InternalOrderID,
	).Exec(ctx)

	return nil
}

// [UPDATE] Terima parameter redisClient
func executeScrapingOrder(dbClient *db.PrismaClient, supplierOrder *db.SupplierOrderModel, redisClient *redis.Client) error {

	// 1. Ambil Item & Quantity
	var items []map[string]interface{}
	queryExec := dbClient.Prisma.QueryRaw(
		`SELECT sp.supplier_product_id, soi.quantity 
		 FROM supplier_order_item soi
		 JOIN supplier_product sp ON soi.supplier_product_id = sp.id
		 WHERE soi.supplier_order_id = ? 
		 LIMIT 1`,
		supplierOrder.ID,
	)

	queryErr := queryExec.Exec(ctx, &items)
	if queryErr != nil {
		return fmt.Errorf("failed to query supplier order item: %v", queryErr)
	}
	if len(items) == 0 {
		return errors.New("no supplier product item found for this order")
	}

	productHTMLID := items[0]["supplier_product_id"].(string)

	var repeatCount int
	if qtyFloat, ok := items[0]["quantity"].(float64); ok {
		repeatCount = int(qtyFloat)
	} else if qtyInt, ok := items[0]["quantity"].(int64); ok {
		repeatCount = int(qtyInt)
	} else {
		repeatCount = 1
	}

	// 2. Ambil Buyer UID
	internalOrder, err := dbClient.InternalOrder.FindUnique(
		db.InternalOrder.ID.Equals(supplierOrder.InternalOrderID),
	).Exec(ctx)

	if err != nil || internalOrder == nil {
		return errors.New("internal order not found or error fetching it")
	}

	playerID := internalOrder.BuyerUID

	// 3. Inisialisasi Scraper Service
	// [FIX] Inject redisClient di sini
	svc, err := scraper.NewMitraHiggsService(false, redisClient)
	if err != nil {
		return fmt.Errorf("browser init failed: %v", err)
	}
	defer svc.Close()

	// 4. Login
	log.Printf("   -> Logging in with MH_USERNAME...")
	mhUsername := os.Getenv("MH_USERNAME")
	mhPassword := os.Getenv("MH_PASSWORD")

	if mhUsername == "" || mhPassword == "" {
		return errors.New("MH_USERNAME or MH_PASSWORD environment variable not set")
	}

	if err := svc.Login(mhUsername, mhPassword); err != nil {
		return fmt.Errorf("provider login failed: %v", err)
	}

	// 5. Place Order
	log.Printf("   -> Placing order for Player: %s, ItemID: %s, Qty: %d", playerID, productHTMLID, repeatCount)

	trxIDs, err := svc.PlaceOrder(playerID, productHTMLID, repeatCount)

	if err != nil {
		return fmt.Errorf("mitrahiggs place order failed: %v", err)
	}

	// 6. Simpan provider_trx_id
	_, err = dbClient.Prisma.ExecuteRaw(
		"UPDATE supplier_order SET provider_trx_id=? WHERE id=?",
		trxIDs,
		supplierOrder.ID,
	).Exec(ctx)

	if err != nil {
		log.Printf("   -> WARNING: Failed to save provider_trx_id %s: %v", trxIDs, err)
	}

	return nil
}