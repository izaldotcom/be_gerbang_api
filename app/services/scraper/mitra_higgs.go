package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/redis/go-redis/v9"
)

type MitraHiggsService struct {
	Pw       *playwright.Playwright
	Browser  playwright.Browser
	Context  playwright.BrowserContext
	Page     playwright.Page
	RedisKey string
	Redis    *redis.Client
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

// [FIXED] Constructor dengan Smart Driver Detection
func NewMitraHiggsService(isDebug bool, redisClient *redis.Client) (*MitraHiggsService, error) {
	serverDriverPath := "/opt/playwright/ms-playwright-go/1.52.0"
	
	runOptions := &playwright.RunOptions{}

	if _, err := os.Stat(serverDriverPath); err == nil {
		log.Println("🖥️  Terdeteksi lingkungan Server: Menggunakan Custom Driver Path:", serverDriverPath)
		runOptions.DriverDirectory = serverDriverPath
	} else {
		log.Println("💻 Terdeteksi lingkungan Lokal: Menggunakan Default Driver Path")
	}

	pw, err := playwright.Run(runOptions)
	if err != nil {
		return nil, fmt.Errorf("gagal start playwright: %v", err)
	}

	// OPTIMASI: Headless & Args
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(!isDebug),
		Args: []string{
			"--no-sandbox",
			"--disable-setuid-sandbox",
			"--disable-gpu",
			"--disable-dev-shm-usage",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("gagal launch browser: %v", err)
	}

	// Setup Mobile View
	ctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		UserAgent: playwright.String("Mozilla/5.0 (Linux; Android 10; SM-G960F) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Mobile Safari/537.36"),
	})
	if err != nil {
		return nil, fmt.Errorf("gagal buat context: %v", err)
	}

	page, err := ctx.NewPage()
	if err != nil {
		return nil, fmt.Errorf("gagal buat page: %v", err)
	}

	if err := page.SetViewportSize(375, 812); err != nil {
		return nil, err
	}

	return &MitraHiggsService{
		Pw:       pw,
		Browser:  browser,
		Context:  ctx,
		Page:     page,
		RedisKey: "mitrahiggs:cookies",
		Redis:    redisClient,
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

// === LOGIC LOGIN ===
func (s *MitraHiggsService) Login(gameID, password string) error {
	ctx := context.Background()
	log.Println("🚀 Memulai proses Login (Optimized)...")

	// Timeout login dikurangi agar fail-fast jika macet
	_, err := s.Page.Goto("https://mitrahiggs.com/", playwright.PageGotoOptions{
		Timeout: playwright.Float(30000),
	})
	if err != nil {
		return fmt.Errorf("gagal buka web: %v", err)
	}

	s.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateDomcontentloaded,
	})

	// 1. CEK & PINDAH KE ID LOGIN
	isPasswordVisible := false
	if vis, _ := s.Page.Locator("input[type='password']").IsVisible(); vis {
		isPasswordVisible = true
	}

	if !isPasswordVisible {
		s.Page.Locator("span[name='index-html-id-login']").Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)})
		if vis, _ := s.Page.Locator("input[type='password']").IsVisible(); !vis {
			s.Page.Locator(".login-text").Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)})
		}
	}

	// 2. ISI FORM
	s.Page.Locator("input[type='text']:visible").First().Fill(gameID)
	s.Page.Locator("input[type='password']").Fill(password)

	// 3. KLIK LOGIN
	err = s.Page.Locator("#pwdLoginButton").Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)})
	if err != nil {
		s.Page.Locator(".btnLogin").Click()
	}

	// 4. VERIFIKASI SUKSES
	err = s.Page.WaitForURL("**/trade/index**", playwright.PageWaitForURLOptions{
		Timeout: playwright.Float(15000),
	})

	if err != nil {
		if vis, _ := s.Page.Locator(".alert-danger").IsVisible(); vis {
			msg, _ := s.Page.Locator(".alert-danger").TextContent()
			return fmt.Errorf("login ditolak: %s", msg)
		}
		return fmt.Errorf("login timeout/gagal")
	}

	log.Println("✅ Login Sukses.")

	// 5. TUTUP POPUP
	s.Page.Evaluate(`
    try { 
      hideInvitation(); 
      document.getElementById('thickdivInvitation').style.display = 'none';
    } catch(e) {}
  `)

	// Simpan Cookie
	cookies, _ := s.Context.Cookies()
	cookieBytes, _ := json.Marshal(cookies)

	err = s.Redis.Set(ctx, s.RedisKey, string(cookieBytes), 24*time.Hour).Err()
	if err != nil {
		log.Println("⚠️ Warning: Gagal simpan cookie ke Redis:", err)
	}

	return nil
}

// === PLACE ORDER (OPTIMIZED UNTUK TOKO KOIN + PAYMENT URL) ===
func (s *MitraHiggsService) PlaceOrder(playerID, productID string, quantity int, paymentTypeID string) (string, error) {
	log.Printf("🛒 Memulai %d Transaksi untuk Player %s (Item ID: %s, Payment ID: %s)", quantity, playerID, productID, paymentTypeID)

	s.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateDomcontentloaded})

	// Handle Popup Awal
	s.Page.Evaluate("try { hideInvitation(); Common.close(); } catch(e) {}")

	var successTrx []string

	// Selector dinamis berdasarkan testing main.go
	productSelector := fmt.Sprintf(`li[onclick*="ShopGoldcoinsInfull.chooseItem(%s"]`, productID)
	paymentSelector := fmt.Sprintf(`li[onclick*="ShopGoldcoinsInfull.chooseInfull"][infullchannel="%s"]`, paymentTypeID)
	idInputSelector := `#userId`
	topupBtnSelector := `a[onclick="ShopGoldcoinsInfull.queryBuyer();"]`
	kirimBtnSelector := `a[onclick="ShopGoldcoinsInfull.buyItem();"]`

	for i := 1; i <= quantity; i++ {
		if i == 1 || i%5 == 0 {
			log.Printf("🔄 Loop %d/%d...", i, quantity)
		}

		// A. BERSIHKAN POPUP SISA
		s.Page.Evaluate("try { Common.close(); } catch(e) {}")
		time.Sleep(200 * time.Millisecond)

		// B. PILIH PRODUK
		if err := s.Page.Locator(productSelector).Click(); err != nil {
			return "", fmt.Errorf("gagal klik produk di loop %d: %v", i, err)
		}

		// C. INPUT ID PLAYER
		if err := s.Page.Locator(idInputSelector).Fill(playerID); err != nil {
			return "", fmt.Errorf("gagal mengisi ID di loop %d: %v", i, err)
		}

		// D. PILIH METODE PEMBAYARAN
		if err := s.Page.Locator(paymentSelector).Click(); err != nil {
			return "", fmt.Errorf("gagal memilih metode pembayaran di loop %d: %v", i, err)
		}

		// E. KLIK TOP UP
		if err := s.Page.Locator(topupBtnSelector).Click(); err != nil {
			return "", fmt.Errorf("gagal klik topup di loop %d: %v", i, err)
		}

		// Fail-Fast: Cek jika ID tidak valid maka web akan memunculkan alert `#publicTip`
		time.Sleep(500 * time.Millisecond)
		if vis, _ := s.Page.Locator("#publicTip").IsVisible(); vis {
			txt, _ := s.Page.Locator("#publicTxt").InnerText()
			if txt != "" && txt != "null" && !strings.Contains(strings.ToLower(txt), "loading") {
				s.Page.Evaluate("Common.close()")
				return "", fmt.Errorf("GAGAL CEK USER (Loop %d): %s", i, txt)
			}
		}

		// F. TUNGGU MODAL NAMA LALU KLIK KIRIM
		log.Printf("⏳ Mengkonfirmasi pesanan (Kirim) loop %d dan menunggu tab baru...", i)
		newPage, err := s.Page.ExpectPopup(func() error {
			return s.Page.Locator(kirimBtnSelector).Click()
		})
		if err != nil {
			return "", fmt.Errorf("gagal menangkap tab pembayaran baru di loop %d: %v", i, err)
		}

		// G. TUNGGU REDIRECT DARI ABOUT:BLANK KE HALAMAN PAYMENT
		log.Printf("⏳ Menunggu redirect dari server payment (Loop %d)...", i)
		for j := 0; j < 20; j++ { // Maksimal tunggu 10 detik (20 * 500ms)
			if newPage.URL() != "about:blank" {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}

		// Pastikan DOM halaman payment termuat
		newPage.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State: playwright.LoadStateDomcontentloaded,
		})

		// Ambil URL Pembayaran
		finalURL := newPage.URL()
		log.Printf("🚀 Sukses! URL Pembayaran loop %d ditemukan: %s", i, finalURL)

		// Simpan transaksi
		successTrx = append(successTrx, finalURL)

		// H. TUTUP TAB BARU (Sangat Penting: Menghindari Memory Leak)
		newPage.Close()

		// Jeda ringan antar transaksi jika quantity > 1
		if i < quantity {
			time.Sleep(1 * time.Second)
		}
	}

	log.Println("🏁 Semua proses looping PlaceOrder selesai.")
	
	// Return gabungan URL, dipisah dengan koma jika quantity > 1
	return strings.Join(successTrx, ","), nil
}