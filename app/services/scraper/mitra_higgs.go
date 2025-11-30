package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"gerbangapi/prisma/db"

	"github.com/playwright-community/playwright-go"
)

type MitraHiggsService struct {
	Pw       *playwright.Playwright
	Browser  playwright.Browser
	Context  playwright.BrowserContext
	Page     playwright.Page
	RedisKey string
}

type SerializableCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HttpOnly bool    `json:"http_only"`
	Secure   bool    `json:"secure"`
	SameSite string  `json:"same_site"`
}

func NewMitraHiggsService() (*MitraHiggsService, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, err
	}

	// Gunakan Headless: false agar browser terlihat di layar
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(false),
	})
	if err != nil {
		return nil, err
	}

	// Setup Mobile View (User Agent Android)
	ctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		UserAgent: playwright.String("Mozilla/5.0 (Linux; Android 10; SM-G960F) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Mobile Safari/537.36"),
	})
	if err != nil {
		return nil, err
	}

	page, err := ctx.NewPage()
	if err != nil {
		return nil, err
	}

	// Set Ukuran Layar HP (iPhone X / Android standar)
	if err := page.SetViewportSize(375, 812); err != nil {
		return nil, err
	}

	return &MitraHiggsService{
		Pw:       pw,
		Browser:  browser,
		Context:  ctx,
		Page:     page,
		RedisKey: "mitrahiggs:cookies",
	}, nil
}

func (s *MitraHiggsService) Close() {
	if s.Browser != nil {
		s.Browser.Close()
	}
	if s.Pw != nil {
		s.Pw.Stop()
	}
}

// === LOGIC LOGIN (FINAL FIX - BUTTON ID) ===
func (s *MitraHiggsService) Login(gameID, password string) error {
	ctx := context.Background()
	log.Println("🚀 Memulai proses Login...")

	// 1. Buka Website
	log.Println("🔄 Membuka Halaman Login...")
	_, err := s.Page.Goto("https://mitrahiggs.com/", playwright.PageGotoOptions{
		Timeout: playwright.Float(60000),
	})
	if err != nil {
		return fmt.Errorf("gagal buka web: %v", err)
	}

	s.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateDomcontentloaded,
	})

	// 2. CEK & PINDAH KE ID LOGIN
	isPasswordVisible, _ := s.Page.Locator("input[type='password']").IsVisible()

	if !isPasswordVisible {
		log.Println("👉 Mode 'Nomor HP' terdeteksi. Mencoba klik 'ID Login'...")
		s.Page.Evaluate("window.scrollTo(0, document.body.scrollHeight)")
		time.Sleep(500 * time.Millisecond)

		// Klik "ID Login"
		err := s.Page.Locator("span[name='index-html-id-login']").Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)})
		if err != nil {
			s.Page.Locator(".login-text").Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)})
		}

		// Tunggu Input Password Muncul
		log.Println("⏳ Menunggu form berubah...")
		s.Page.WaitForSelector("input[type='password']", playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(5000),
		})
	}

	// 3. ISI FORM
	log.Println("✍️ Mengisi ID Game dan Password...")
	s.Page.Locator("input[type='text']:visible").First().Fill(gameID)
	s.Page.Locator("input[type='password']").Fill(password)

	// 4. KLIK LOGIN (PERBAIKAN UTAMA DI SINI)
	log.Println("🖱️ Klik Tombol Login...")
	
	// Kita gunakan ID: #pwdLoginButton (Sesuai elemen HTML yang Anda kirim)
	// Ini JAUH lebih akurat daripada mencari teks.
	err = s.Page.Locator("#pwdLoginButton").Click(playwright.LocatorClickOptions{
		Force: playwright.Bool(true), // Force click walaupun ketutup dikit
	})
	
	if err != nil {
		// Fallback ke class jika ID entah kenapa gagal
		log.Println("⚠️ Gagal klik ID #pwdLoginButton, mencoba class .btnLogin...")
		s.Page.Locator(".btnLogin").Click()
	}

	// 5. VERIFIKASI SUKSES
	log.Println("⏳ Menunggu redirect ke Dashboard Trade...")

	// Tunggu URL berubah mengandung "/trade/index"
	err = s.Page.WaitForURL("**/trade/index**", playwright.PageWaitForURLOptions{
		Timeout: playwright.Float(20000),
	})

	if err != nil {
		// Cek error message
		if vis, _ := s.Page.Locator(".alert-danger").IsVisible(); vis {
			msg, _ := s.Page.Locator(".alert-danger").TextContent()
			return fmt.Errorf("login ditolak: %s", msg)
		}
		
		s.Page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String("debug_login_failed.png")})
		return fmt.Errorf("login timeout: tidak masuk ke halaman trade/index")
	}

	log.Println("✅ Redirect Sukses! URL sekarang:", s.Page.URL())

	// 6. TUTUP POPUP IKLAN (Gift Card)
	time.Sleep(2 * time.Second) // Tunggu popup muncul
	
	// Coba tutup popup. Selector tombol close biasanya .close atau .close-btn
	// Kita coba klik di pojok kanan atas popup atau cari elemen close umum
	if count, _ := s.Page.Locator(".close-btn").Count(); count > 0 {
		log.Println("🧹 Menutup Popup Iklan...")
		s.Page.Locator(".close-btn").Click()
	} else {
		// Kadang popup menutup jika klik di luar (backdrop)
		// Kita biarkan dulu, biasanya tidak menghalangi URL check
	}

	// Simpan Cookie
	cookies, _ := s.Context.Cookies()
	cookieBytes, _ := json.Marshal(cookies)
	db.Rdb.Set(ctx, s.RedisKey, string(cookieBytes), 24*time.Hour)
	
	log.Println("💾 Login Berhasil & Cookie Disimpan.")
	return nil
}

// PlaceOrder (Dummy Implementation for Week 3)
func (s *MitraHiggsService) PlaceOrder(playerID, productCode string) (string, error) {
	log.Println("🛒 Memulai Transaksi untuk Player:", playerID)

	if s.Page.URL() != "https://mitrahiggs.com/" {
		s.Page.Goto("https://mitrahiggs.com/")
	}

	// NOTE: Selector ini harus disesuaikan lagi nanti setelah berhasil login
	err := s.Page.Locator("input[placeholder*='ID']").First().Fill(playerID)
	if err != nil {
		return "", fmt.Errorf("gagal menemukan input ID Player")
	}

	log.Printf("🔍 Memilih produk: %s", productCode)
	// Klik elemen produk (Dummy selector)
	s.Page.Locator(fmt.Sprintf("text=%s", productCode)).Click()

	// Klik Beli
	s.Page.Locator("button:has-text('Beli')").Click()

	log.Println("✅ Order disubmit (Mocking Success)")
	return "TRX-PENDING-SCRAPE", nil
}