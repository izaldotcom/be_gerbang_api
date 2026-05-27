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
	"strings"
	"time"

	"gerbangapi/app/services/scraper"
	"gerbangapi/prisma/db"

	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

// StartWorker memulai worker di background (Goroutine)
func StartWorker(dbClient *db.PrismaClient, redisClient *redis.Client) {
	log.Println("🚀 Starting MitraHiggs Order Worker (Background Mode)...")

	go func() {
		for {
			err := processNextSupplierOrder(dbClient, redisClient)
			if err != nil {
				log.Printf("❌ Worker Error: %v", err)
			}
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
		db.InternalOrder.PaymentType.Fetch(), 
	).Exec(ctx)

	if err != nil {
		failOrder(dbClient, orderID, supplierOrder.InternalOrderID, "Internal Order Not Found")
		return nil
	}

	// === [PERBAIKAN] Ambil Item Detail TANPA LIMIT 1 ===
	var items []map[string]interface{}
	dbClient.Prisma.QueryRaw(
		`SELECT sp.supplier_product_id, soi.quantity 
		 FROM supplier_order_item soi
		 JOIN supplier_product sp ON soi.supplier_product_id = sp.id
		 WHERE soi.supplier_order_id = ?`, // LIMIT 1 dihapus agar semua bahan campuran terbaca
		supplierOrder.ID,
	).Exec(ctx, &items)

	if len(items) == 0 {
		failOrder(dbClient, orderID, supplierOrder.InternalOrderID, "No items found for this order")
		return nil
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

	// Mengambil Username dan Password dari database
	mhUsername, okUser := supplierMH.Username()
	mhPassword, okPass := supplierMH.Password()
	
	if !okUser || !okPass || mhUsername == "" || mhPassword == "" {
		failOrder(dbClient, orderID, supplierOrder.InternalOrderID, "Kredensial Supplier Belum Diset di Database")
		return nil
	}

	log.Println("🔑 Logging in...")
	if err := svc.Login(mhUsername, mhPassword); err != nil {
		failOrder(dbClient, orderID, supplierOrder.InternalOrderID, "Login Failed: "+err.Error())
		return nil
	}

	paymentCode := "40" // Default ke QRIS
	if pt, ok := internalOrder.PaymentType(); ok && pt != nil {
		paymentCode = pt.Code
	}

	var allPaymentURLs []string

	// === [PERBAIKAN] Looping untuk Setiap Bahan Baku di dalam Resep ===
	for i, item := range items {
		productHTMLID := item["supplier_product_id"].(string)
		
		// Parse Quantity
		var repeatCount int
		if qtyFloat, ok := item["quantity"].(float64); ok {
			repeatCount = int(qtyFloat)
		} else if qtyInt, ok := item["quantity"].(int64); ok {
			repeatCount = int(qtyInt)
		} else {
			repeatCount = 1
		}

		log.Printf("🛒 [Mix %d/%d] Placing order for Player: %s, Item HTML ID: %s, Qty: %d", i+1, len(items), internalOrder.BuyerUID, productHTMLID, repeatCount)
		
		paymentURLs, err := svc.PlaceOrder(internalOrder.BuyerUID, productHTMLID, repeatCount, paymentCode)

		if err != nil {
			// Jika salah satu bahan gagal, gagalkan seluruh transaksi
			failOrder(dbClient, orderID, supplierOrder.InternalOrderID, fmt.Sprintf("Place Order Failed on item %s: %v", productHTMLID, err))
			return nil 
		}

		if paymentURLs != "" {
			allPaymentURLs = append(allPaymentURLs, paymentURLs)
		}
		
		// Beri jeda antar bahan baku jika ada lebih dari 1 bahan
		if i < len(items)-1 {
			time.Sleep(2 * time.Second)
		}
	}

	// =================================================================
	// LANGKAH E: Sukses & Notifikasi
	// =================================================================
	
	// Gabungkan semua URL pembayaran (jika campuran, akan dipisah koma)
	providerTrx := strings.Join(allPaymentURLs, ",")
	log.Printf("✅ Order #%s Success! URLs: %s", orderID, providerTrx)

	dbClient.Prisma.ExecuteRaw("UPDATE supplier_order SET status='success', provider_trx_id=? WHERE id=?", providerTrx, orderID).Exec(ctx)
	dbClient.Prisma.ExecuteRaw("UPDATE internal_order SET status='success' WHERE id=?", supplierOrder.InternalOrderID).Exec(ctx)

	// --- Siapkan Data Notifikasi ---
	productName := internalOrder.Product().Name
	productPrice := internalOrder.Product().Price
	tujuan := internalOrder.BuyerUID 
	tanggal := time.Now().Format("02 Jan 2006 15:04")
	supplierName := supplierMH.Name

	// Buat tautan URL dinamis (Jika URL lebih dari 1, ubah kalimatnya)
	var urlLinks string
	if len(allPaymentURLs) > 1 {
		for idx, url := range allPaymentURLs {
			urlLinks += fmt.Sprintf("\n🔗 <a href=\"%s\">Bayar Bagian %d</a>", url, idx+1)
		}
	} else {
		urlLinks = fmt.Sprintf("\n🔗 <a href=\"%s\">Klik untuk bayar</a>", providerTrx)
	}

	// --- Template Notifikasi Profesional ---
	msg := fmt.Sprintf(`
<b>📦 TRANSAKSI BERHASIL</b>
▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬
<b>Detail Produk:</b>
🔹 %s

<b>Informasi Pengiriman:</b>
📍 <b>Tujuan:</b> <code>%s</code>
<b>Payment URL:</b> %s
🏢 <b>Supplier:</b> %s

<b>Tanggal:</b> %s
<b>Status:</b> <pre>SUCCESS (MENUNGGU PEMBAYARAN)</pre>
▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬
<i>Ref ID: %s</i>
`, productName, tujuan, urlLinks, supplierName, tanggal, supplierOrder.InternalOrderID)

	// 1. Kirim ke ADMIN (Wajib)
	adminChatID := os.Getenv("TELEGRAM_CHAT_ID")
	if adminChatID != "" {
		go sendTelegramNotification(adminChatID, "<b>[ADMIN REPORT]</b>\n"+msg)
	}

	// 2. Kirim ke USER (Personal) & Webhook
	user, ok := internalOrder.User() 
	if ok && user != nil {
		
		// A. Telegram Personal
		if userChatID, okID := user.TelegramChatID(); okID && userChatID != "" {
			log.Printf("📩 Sending Telegram msg to User: %s", userChatID)
			go sendTelegramNotification(userChatID, msg)
		} else {
			log.Println("⚠️ User belum menghubungkan Telegram (Chat ID kosong).")
		}

		// B. Webhook URL (Server to Server)
		if url, okURL := user.WebhookURL(); okURL && url != "" {
			log.Printf("🔗 Sending rich webhook to User: %s", url)

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
					"message":      "Transaksi berhasil, silakan lakukan pembayaran melalui URL terlampir",
				},
			}
			go sendWebhookCallback(url, webhookPayload)
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

	// Notif Telegram Gagal ke ADMIN
	adminChatID := os.Getenv("TELEGRAM_CHAT_ID")
	if adminChatID != "" {
		msg := fmt.Sprintf(`
<b>❌ TRANSAKSI GAGAL</b>
▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬
<b>ID Order:</b> <code>%s</code>
<b>Penyebab:</b> <pre>%s</pre>
<b>Internal ID:</b> %s
▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬`, orderID, reason, internalID)
		
		go sendTelegramNotification(adminChatID, msg)
	}
}

func sendTelegramNotification(targetChatID string, messageHTML string) {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" || targetChatID == "" {
		return 
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	payload := map[string]interface{}{
		"chat_id":                    targetChatID,
		"text":                       messageHTML,
		"parse_mode":                 "HTML",
		"disable_web_page_preview":   true,
	}

	jsonVal, _ := json.Marshal(payload)
	
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonVal))
	if err != nil {
		log.Printf("⚠️ Gagal kirim Telegram: %v", err)
		return
	}
	defer resp.Body.Close()
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