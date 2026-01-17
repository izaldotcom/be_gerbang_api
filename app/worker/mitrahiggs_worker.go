package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"gerbangapi/app/services/scraper"
	"gerbangapi/prisma/db"

	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

// StartWorker memulai worker di background (Goroutine)
// Dipanggil dari main.go
func StartWorker(dbClient *db.PrismaClient, redisClient *redis.Client) {
	log.Println("🚀 Starting MitraHiggs Order Worker (Background Mode)...")

	// Jalankan di Goroutine agar tidak memblokir server API
	go func() {
		for {
			err := processNextSupplierOrder(dbClient, redisClient)
			if err != nil {
				// Log error tapi worker tetap jalan
				log.Printf("❌ Worker Error: %v", err)
			}

			// Jeda 5 detik sebelum mengecek antrian lagi
			time.Sleep(5 * time.Second)
		}
	}()
}

func processNextSupplierOrder(dbClient *db.PrismaClient, redisClient *redis.Client) error {
	// =================================================================
	// LANGKAH A: Cari Supplier UUID berdasarkan CODE 'MH_OFFICIAL'
	// =================================================================
	supplierMH, err := dbClient.Supplier.FindFirst(
		db.Supplier.Code.Equals("MH_OFFICIAL"),
	).Exec(ctx)

	if err != nil {
		return fmt.Errorf("Supplier 'MH_OFFICIAL' tidak ditemukan di Database")
	}

	// =================================================================
	// LANGKAH B: Cari Order Pending
	// =================================================================
	supplierOrder, err := dbClient.SupplierOrder.FindFirst(
		db.SupplierOrder.Status.Equals("pending"),
		db.SupplierOrder.SupplierID.Equals(supplierMH.ID),
	).Exec(ctx)

	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil // Tidak ada antrian
		}
		return fmt.Errorf("failed to fetch pending order: %v", err)
	}

	orderID := supplierOrder.ID
	log.Printf("🔥 Processing Order #%s", orderID)

	// Update status -> processing
	dbClient.Prisma.ExecuteRaw("UPDATE supplier_order SET status='processing' WHERE id=?", orderID).Exec(ctx)

	// =================================================================
	// LANGKAH C: Ambil Data Lengkap (Internal Order + User)
	// =================================================================
	
	internalOrder, err := dbClient.InternalOrder.FindUnique(
		db.InternalOrder.ID.Equals(supplierOrder.InternalOrderID),
	).With(
		db.InternalOrder.Product.Fetch(), 
		db.InternalOrder.User.Fetch(),
	).Exec(ctx)

	if err != nil {
		failOrder(dbClient, orderID, supplierOrder.InternalOrderID, "Internal Order Not Found")
		return nil
	}

	// Ambil Item Detail
	var items []map[string]interface{}
	dbClient.Prisma.QueryRaw(
		`SELECT sp.supplier_product_id, soi.quantity 
		 FROM supplier_order_item soi
		 JOIN supplier_product sp ON soi.supplier_product_id = sp.id
		 WHERE soi.supplier_order_id = ? 
		 LIMIT 1`,
		supplierOrder.ID,
	).Exec(ctx, &items)

	if len(items) == 0 {
		failOrder(dbClient, orderID, supplierOrder.InternalOrderID, "No items found for this order")
		return nil
	}

	productHTMLID := items[0]["supplier_product_id"].(string)
	
	// Parse Quantity
	var repeatCount int
	if qtyFloat, ok := items[0]["quantity"].(float64); ok {
		repeatCount = int(qtyFloat)
	} else if qtyInt, ok := items[0]["quantity"].(int64); ok {
		repeatCount = int(qtyInt)
	} else {
		repeatCount = 1
	}

	// =================================================================
	// LANGKAH D: Eksekusi Scraping (Browser)
	// =================================================================
	
	svc, err := scraper.NewMitraHiggsService(false, redisClient)
	if err != nil {
		failOrder(dbClient, orderID, supplierOrder.InternalOrderID, "Browser Init Failed: "+err.Error())
		return nil
	}
	defer svc.Close()

	// Jeda waktu agar halaman loading sempurna
	log.Println("⏳ Waiting for browser page load...")
	time.Sleep(5 * time.Second) 

	// Login
	mhUsername := os.Getenv("MH_USERNAME")
	mhPassword := os.Getenv("MH_PASSWORD")

	log.Println("🔑 Logging in...")
	if err := svc.Login(mhUsername, mhPassword); err != nil {
		failOrder(dbClient, orderID, supplierOrder.InternalOrderID, "Login Failed: "+err.Error())
		return nil
	}

	// Place Order
	log.Printf("🛒 Placing order for Player: %s, Item: %s", internalOrder.BuyerUID, productHTMLID)
	trxIDs, err := svc.PlaceOrder(internalOrder.BuyerUID, productHTMLID, repeatCount)

	if err != nil {
		failOrder(dbClient, orderID, supplierOrder.InternalOrderID, "Place Order Failed: "+err.Error())
		return nil
	}

	// =================================================================
	// LANGKAH E: Sukses & Notifikasi
	// =================================================================
	log.Printf("✅ Order #%s Success! Trx: %v", orderID, trxIDs)

	// Update DB Success
	providerTrx := trxIDs[0]
	dbClient.Prisma.ExecuteRaw("UPDATE supplier_order SET status='success', provider_trx_id=? WHERE id=?", providerTrx, orderID).Exec(ctx)
	dbClient.Prisma.ExecuteRaw("UPDATE internal_order SET status='success' WHERE id=?", supplierOrder.InternalOrderID).Exec(ctx)

	// --- Siapkan Data Notifikasi ---
	productName := internalOrder.Product().Name
	productPrice := internalOrder.Product().Price // Fix variable undefined
	tujuan := internalOrder.BuyerUID 
	supplierName := supplierMH.Name
	tanggal := time.Now().Format("2006-01-02 15:04:05")

	// Kirim Telegram (Internal Log)
	msg := fmt.Sprintf(`
✅ <b>TRANSAKSI SUKSES</b>

<b>User ID   :</b> %s
<b>Produk    :</b> %s
<b>SN / Trx  :</b> %s
<b>Supplier  :</b> %s
<b>Tanggal   :</b> %s

<i>Ref ID: %s</i>
`, tujuan, productName, providerTrx, supplierName, tanggal, supplierOrder.InternalOrderID)

	go sendTelegramNotification(msg)

	// Kirim Webhook ke User
	user, ok := internalOrder.User() 
	if ok && user != nil {
		if url, okURL := user.WebhookURL(); okURL && url != "" {
			log.Printf("🔗 Sending rich webhook to User: %s", url)

			// PAYLOAD LENGKAP
			webhookPayload := map[string]interface{}{
				"seller_id":    user.ID,
				"message_type": "transaction_update",
				"timestamp":    tanggal,
				"data": map[string]interface{}{
					"trx_id":       internalOrder.ID,
					"ref_id":       internalOrder.ID,
					"product_name": productName,
					"code":         internalOrder.ProductID,
					"price":        productPrice,
					"status":       "success",
					"status_code":  1,
					"sn":           providerTrx,
					"destination":  tujuan,
					"message":      "Transaksi berhasil diproses",
				},
			}

			go sendWebhookCallback(url, webhookPayload)
		} else {
			log.Println("⚠️ User tidak memiliki Webhook URL, skip callback.")
		}
	}

	return nil
}

// ==========================================
// HELPER FUNCTIONS
// ==========================================

func failOrder(dbClient *db.PrismaClient, orderID, internalID, reason string) {
	log.Printf("❌ Order %s Failed: %s", orderID, reason)
	
	dbClient.Prisma.ExecuteRaw("UPDATE supplier_order SET status='failed', last_error=? WHERE id=?", reason, orderID).Exec(ctx)
	dbClient.Prisma.ExecuteRaw("UPDATE internal_order SET status='failed' WHERE id=?", internalID).Exec(ctx)

	// Notif Telegram Gagal
	msg := fmt.Sprintf("❌ <b>TRANSAKSI GAGAL</b>\n\n<b>Err:</b> %s\n<b>ID:</b> %s", reason, orderID)
	go sendTelegramNotification(msg)
}

func sendTelegramNotification(messageHTML string) {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")

	if botToken == "" || chatID == "" {
		return 
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	payload := map[string]string{
		"chat_id":    chatID,
		"text":       messageHTML,
		"parse_mode": "HTML",
	}

	jsonVal, _ := json.Marshal(payload)
	http.Post(url, "application/json", bytes.NewBuffer(jsonVal))
}

func sendWebhookCallback(targetURL string, payload interface{}) {
	jsonVal, _ := json.Marshal(payload)

	for i := 0; i < 3; i++ {
		client := http.Client{Timeout: 10 * time.Second}
		resp, err := client.Post(targetURL, "application/json", bytes.NewBuffer(jsonVal))
		if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if resp.Body != nil { resp.Body.Close() }
			log.Printf("✅ Webhook sent successfully to %s", targetURL)
			return
		}
		time.Sleep(2 * time.Second)
	}
	log.Printf("❌ Webhook gave up after 3 attempts")
}