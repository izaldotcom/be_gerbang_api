package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"gerbangapi/app/services/scraper" // Sesuaikan nama module Anda

	"github.com/joho/godotenv"
	"github.com/playwright-community/playwright-go" // Import Playwright
	"github.com/redis/go-redis/v9"                  //
)

// TopupPayload mensimulasikan data yang didapat dari request API
type TopupPayload struct {
	TargetUserID   string
	ProductID      string // ID Produk dari HTML
	PaymentChannel string // ID Channel dari HTML
}

func main() {
	// 1. Load Env
	if err := godotenv.Load(".env"); err != nil {
		log.Println("⚠️ Warning: Tidak bisa load .env, mencoba default system env...")
	}

	log.Println("🧪 TESTING: Memulai Browser (Visual Mode Playwright)...")

	// 2. Init Redis Client
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: "", // Sesuaikan jika ada password
		DB:       0,
	})

	// 3. Init Service
	svc, err := scraper.NewMitraHiggsService(true, redisClient)
	if err != nil {
		log.Fatal(err)
	}
	defer svc.Close()

	// 4. Tes Login
	username := os.Getenv("MH_USERNAME")
	password := os.Getenv("MH_PASSWORD")

	if username == "" || password == "" {
		log.Fatal("❌ MH_USERNAME atau MH_PASSWORD belum diset di .env")
	}

	log.Printf("👤 Login sebagai: %s", username)

	err = svc.Login(username, password)
	if err != nil {
		log.Fatalf("❌ Login Gagal: %v", err)
	}
	log.Println("✅ Login Berhasil!")

	// ==========================================
	// 5. PROSES SCRAPING DENGAN PLAYWRIGHT
	// ==========================================

	/* REFERENSI OPSI PRODUK KOIN:
	   "7"  = 30M 		"10" = 400M
	   "8"  = 60M 		"11" = 1B
	   "9"  = 200M 		"12" = 2B

	   REFERENSI OPSI PEMBAYARAN:
	   "40" = QRIS		"22" = ShopeePay
	   "14" = DANA		"13" = OVO
	*/

	payload := TopupPayload{
		TargetUserID:   "3145526",
		ProductID:      "7",  // Pilih 30M
		PaymentChannel: "14", // Pilih QRIS
	}

	log.Printf("🛒 Memulai order untuk User ID: %s | Produk ID: %s | Pembayaran ID: %s\n",
		payload.TargetUserID, payload.ProductID, payload.PaymentChannel)

	// Selector dinamis (Sudah di-FIX strict mode-nya)
	productSelector := fmt.Sprintf(`li[onclick*="ShopGoldcoinsInfull.chooseItem(%s"]`, payload.ProductID)
	paymentSelector := fmt.Sprintf(`li[onclick*="ShopGoldcoinsInfull.chooseInfull"][infullchannel="%s"]`, payload.PaymentChannel)

	// Selector statis
	idInputSelector := `#userId`
	topupBtnSelector := `a[onclick="ShopGoldcoinsInfull.queryBuyer();"]`
	kirimBtnSelector := `a[onclick="ShopGoldcoinsInfull.buyItem();"]`

	// Pastikan instance Page di struct service diawali huruf kapital (svc.Page)

	// Langkah 1: Pilih Produk Dinamis
	if err := svc.Page.Locator(productSelector).Click(); err != nil {
		log.Fatalf("❌ Gagal klik produk: %v", err)
	}

	// Langkah 2: Masukkan User ID
	if err := svc.Page.Locator(idInputSelector).Fill(payload.TargetUserID); err != nil {
		log.Fatalf("❌ Gagal mengisi ID: %v", err)
	}

	// Langkah 3: Pilih Metode Pembayaran Dinamis
	if err := svc.Page.Locator(paymentSelector).Click(); err != nil {
		log.Fatalf("❌ Gagal memilih metode pembayaran: %v", err)
	}

	// Langkah 4: Klik Top Up
	if err := svc.Page.Locator(topupBtnSelector).Click(); err != nil {
		log.Fatalf("❌ Gagal klik topup: %v", err)
	}

	// Langkah 5 & 6: Konfirmasi dan Tangkap Tab Baru
	log.Println("⏳ Mengkonfirmasi pesanan dan menunggu tab pembayaran baru terbuka...")

	newPage, err := svc.Page.ExpectPopup(func() error {
		return svc.Page.Locator(kirimBtnSelector).Click()
	})
	if err != nil {
		log.Fatalf("❌ Gagal menangkap tab pembayaran baru: %v", err)
	}

	// [PERBAIKAN]: Tunggu sampai URL berubah dari "about:blank" akibat redirect AJAX/Fetch
	log.Println("⏳ Menunggu redirect dari server payment...")
	for i := 0; i < 20; i++ { // Maksimal tunggu 10 detik (20 * 500ms)
		if newPage.URL() != "about:blank" {
			break // Keluar dari loop jika URL sudah bukan about:blank
		}
		time.Sleep(500 * time.Millisecond) // Jeda 0.5 detik sebelum cek lagi
	}

	// Tunggu sampai DOM di halaman payment yang baru termuat dengan sempurna
	newPage.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateDomcontentloaded,
	})

	// Ambil URL yang sebenarnya dari tab baru
	finalURL := newPage.URL()

	log.Printf("🚀 Sukses! URL Pembayaran di tab baru ditemukan: %s", finalURL)
	// ==========================================

	// (Opsional) Tutup tab baru jika sudah mendapatkan URL untuk menghemat memori
	// newPage.Close()

	log.Println("Browser akan menutup dalam 10 detik...")
	time.Sleep(10 * time.Second) //
}