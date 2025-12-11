// worker/mitrahiggs_worker.go

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"gerbangapi/app/services/scraper"
	"gerbangapi/prisma/db" // Pastikan jalur import ke Prisma Client Anda benar
)

// Gunakan context background untuk proses worker yang berjalan terus menerus
var ctx = context.Background()

func main() {
	log.Println("🚀 Starting MitraHiggs Order Worker...")

	// 1. Inisialisasi DB Client
	// Pastikan Anda telah mengatur ENVIRONMENT VARIABLES yang dibutuhkan oleh Prisma Client
	dbClient := db.NewClient()
	if err := dbClient.Prisma.Connect(); err != nil {
		log.Fatalf("Fatal Error: Failed to connect to database: %v", err)
	}
	defer dbClient.Prisma.Disconnect()
	
	log.Println("✅ Database Connected. Worker is running...")

	// Worker akan berputar terus menerus
	for {
		err := processNextSupplierOrder(dbClient)
		if err != nil {
			// Hanya log error, jangan sampai proses worker berhenti
			log.Printf("❌ Worker Error: %v", err)
		}
		
		// Jeda sebelum mengecek antrian lagi (misalnya 5 detik)
		time.Sleep(5 * time.Second) 
	}
}

// processNextSupplierOrder mencari satu order 'pending', menandainya sebagai 'processing',
// dan memanggil fungsi scraping untuk eksekusi.
func processNextSupplierOrder(dbClient *db.PrismaClient) error {
	// A. Ambil order 'pending' pertama
	// Kita cari SupplierOrder yang statusnya 'pending' dan suppliernya 'mitra-higgs'
	supplierOrder, err := dbClient.SupplierOrder.FindFirst(
		db.SupplierOrder.Status.Equals("pending"),
		db.SupplierOrder.SupplierID.Equals("mitra-higgs"),
	).Exec(ctx)

	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			// Ini bukan error fatal, hanya tidak ada antrian
			// log.Println("🧘 No pending orders. Resting...") // Komen ini agar log tidak terlalu ramai
			return nil 
		}
		return fmt.Errorf("failed to fetch pending order: %v", err)
	}
	
	orderID := supplierOrder.ID
	log.Printf("🔥 Processing Order #%s", orderID)

	// B. Tandai order sebagai 'processing' (Optimistic Lock)
	// Kita gunakan raw query untuk mengubah status agar worker lain tidak mengambilnya
	_, err = dbClient.Prisma.ExecuteRaw(
		"UPDATE supplier_order SET status='processing' WHERE id=?",
		orderID,
	).Exec(ctx)

	if err != nil {
		return fmt.Errorf("failed to mark order as processing: %v", err)
	}
	
	// C. Panggil Scraper (Core Logic)
	err = executeScrapingOrder(dbClient, supplierOrder)
	
	// D. Handle Hasil (Success/Fail)
	if err != nil {
		log.Printf("⚠️ Order #%s FAILED: %v", orderID, err)
		
		// Update status menjadi 'failed' dan simpan error
		errMsg := err.Error()
		dbClient.Prisma.ExecuteRaw(
			"UPDATE supplier_order SET status='failed', last_error=? WHERE id=?",
			errMsg,
			orderID,
		).Exec(ctx)
		
		// Update juga Internal Order (status='failed')
		dbClient.Prisma.ExecuteRaw(
			"UPDATE internal_order SET status='failed' WHERE id=?",
			supplierOrder.InternalOrderID,
		).Exec(ctx)
		
		return nil // Return nil agar loop utama worker tidak terhenti
	}

	log.Printf("✅ Order #%s Success!", orderID)
	
	// Update status menjadi 'success'
	dbClient.Prisma.ExecuteRaw(
		"UPDATE supplier_order SET status='success' WHERE id=?",
		orderID,
	).Exec(ctx)
	
	// Update juga Internal Order (status='success')
	dbClient.Prisma.ExecuteRaw(
		"UPDATE internal_order SET status='success' WHERE id=?",
		supplierOrder.InternalOrderID,
	).Exec(ctx)

	// TODO: (Minggu 4) Lakukan Webhook Callback di sini jika sudah diimplementasikan
	
	return nil
}


// executeScrapingOrder menangani seluruh alur Playwright untuk order yang diberikan.
func executeScrapingOrder(dbClient *db.PrismaClient, supplierOrder *db.SupplierOrderModel) error {
	
	// 1. Ambil Item & Destination
	
	// Ambil Item yang dibutuhkan: supplier_product_id
	var items []map[string]interface{}
	queryExec := dbClient.Prisma.QueryRaw(
		// Asumsi hanya ada 1 item per supplier order
		"SELECT supplier_product_id FROM supplier_order_item WHERE supplier_order_id = ? LIMIT 1",
		supplierOrder.ID,
	)
	
	queryErr := queryExec.Exec(ctx, &items)
	if queryErr != nil {
		return fmt.Errorf("failed to query supplier order item: %v", queryErr)
	}
	if len(items) == 0 {
		return errors.New("no supplier product item found for this order")
	}

	nominalID := items[0]["supplier_product_id"].(string)

	// Ambil Destination (Buyer UID) dari InternalOrder
	internalOrder, err := dbClient.InternalOrder.FindUnique(
		db.InternalOrder.ID.Equals(supplierOrder.InternalOrderID),
	).Exec(ctx)
	
	if err != nil || internalOrder == nil {
		return errors.New("internal order not found or error fetching it")
	}

	playerID := internalOrder.BuyerUID

	// 2. Inisialisasi Scraper Service
	svc, err := scraper.NewMitraHiggsService()
	if err != nil {
		return fmt.Errorf("browser init failed: %v", err)
	}
	defer svc.Close()
	
	// 3. Login
	log.Printf("   -> Logging in with MH_USERNAME...")
	mhUsername := os.Getenv("MH_USERNAME")
	mhPassword := os.Getenv("MH_PASSWORD")

	if mhUsername == "" || mhPassword == "" {
		return errors.New("MH_USERNAME or MH_PASSWORD environment variable not set")
	}

	if err := svc.Login(mhUsername, mhPassword); err != nil {
		return fmt.Errorf("provider login failed: %v", err)
	}
	
	// 4. Place Order
	log.Printf("   -> Placing order for Player: %s, Product: %s", playerID, nominalID)
	trxID, err := svc.PlaceOrder(playerID, nominalID)
	
	if err != nil {
		return fmt.Errorf("mitrahiggs place order failed: %v", err)
	}
	
	// 5. Simpan provider_trx_id
	_, err = dbClient.Prisma.ExecuteRaw(
		"UPDATE supplier_order SET provider_trx_id=? WHERE id=?",
		trxID,
		supplierOrder.ID,
	).Exec(ctx)

	if err != nil {
		// Ini bukan error yang membatalkan order, tapi harus di-log
		log.Printf("   -> WARNING: Failed to save provider_trx_id %s: %v", trxID, err)
	}
	
	return nil
}