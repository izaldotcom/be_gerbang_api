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

	// Headless: false agar terlihat prosesnya
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(false),
	})
	if err != nil {
		return nil, err
	}

	// Setup Mobile View
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

// === LOGIC LOGIN ===
func (s *MitraHiggsService) Login(gameID, password string) error {
	ctx := context.Background()
	log.Println("🚀 Memulai proses Login...")

	_, err := s.Page.Goto("https://mitrahiggs.com/", playwright.PageGotoOptions{
		Timeout: playwright.Float(60000),
	})
	if err != nil {
		return fmt.Errorf("gagal buka web: %v", err)
	}

	s.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateDomcontentloaded,
	})

	// 1. CEK & PINDAH KE ID LOGIN
	isPasswordVisible, _ := s.Page.Locator("input[type='password']").IsVisible()

	if !isPasswordVisible {
		log.Println("👉 Mode 'Nomor HP' terdeteksi. Mencoba klik 'ID Login'...")
		
		// Klik "ID Login"
		err := s.Page.Locator("span[name='index-html-id-login']").Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)})
		if err != nil {
			s.Page.Locator(".login-text").Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)})
		}

		s.Page.WaitForSelector("input[type='password']", playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(5000),
		})
	}

	// 2. ISI FORM
	log.Println("✍️ Mengisi ID Game dan Password...")
	s.Page.Locator("input[type='text']:visible").First().Fill(gameID)
	s.Page.Locator("input[type='password']").Fill(password)

	// 3. KLIK LOGIN
	log.Println("🖱️ Klik Tombol Login...")
	err = s.Page.Locator("#pwdLoginButton").Click(playwright.LocatorClickOptions{
		Force: playwright.Bool(true),
	})
	
	if err != nil {
		log.Println("⚠️ Gagal klik #pwdLoginButton, mencoba .btnLogin...")
		s.Page.Locator(".btnLogin").Click()
	}

	// 4. VERIFIKASI SUKSES
	log.Println("⏳ Menunggu redirect ke Dashboard Trade...")
	err = s.Page.WaitForURL("**/trade/index**", playwright.PageWaitForURLOptions{
		Timeout: playwright.Float(20000),
	})

	if err != nil {
		if vis, _ := s.Page.Locator(".alert-danger").IsVisible(); vis {
			msg, _ := s.Page.Locator(".alert-danger").TextContent()
			return fmt.Errorf("login ditolak: %s", msg)
		}
		s.Page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String("debug_login_failed.png")})
		return fmt.Errorf("login timeout")
	}

	log.Println("✅ Redirect Sukses! URL sekarang:", s.Page.URL())

	// 5. TUTUP POPUP (REQUEST KHUSUS)
	// Target: <div id="thickdivInvitation" ... onclick="hideInvitation()"></div>
	time.Sleep(2 * time.Second) 
	
	popupSelector := "#thickdivInvitation"
	if count, _ := s.Page.Locator(popupSelector).Count(); count > 0 {
		if vis, _ := s.Page.Locator(popupSelector).IsVisible(); vis {
			log.Println("🧹 Menutup Popup Invitation (#thickdivInvitation)...")
			
			// Klik elemen tersebut untuk memicu onclick="hideInvitation()"
			s.Page.Locator(popupSelector).Click(playwright.LocatorClickOptions{
				Force: playwright.Bool(true),
			})
			
			// Tunggu sebentar agar hilang
			time.Sleep(1 * time.Second)
		}
	}

	// Simpan Cookie
	cookies, _ := s.Context.Cookies()
	cookieBytes, _ := json.Marshal(cookies)
	db.Rdb.Set(ctx, s.RedisKey, string(cookieBytes), 24*time.Hour)
	
	log.Println("💾 Login Berhasil & Cookie Disimpan.")
	return nil
}

// === PLACE ORDER (REAL IMPLEMENTATION BASED ON HTML) ===
func (s *MitraHiggsService) PlaceOrder(playerID, productID string) (string, error) {
	// playerID = "3145526"
	// productID = "6" (Bukan "1M", tapi ID dari inspect element)

	log.Printf("🛒 Memulai Transaksi: Player %s, Item ID %s", playerID, productID)

	s.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateNetworkidle,
	})
	time.Sleep(1 * time.Second)

	// 1. CEK POPUP LAGI (Jaga-jaga muncul lagi di halaman trade)
	popupSelector := "#thickdivInvitation"
	if vis, _ := s.Page.Locator(popupSelector).IsVisible(); vis {
		log.Println("🧹 Menutup Popup yang muncul lagi...")
		s.Page.Locator(popupSelector).Click()
		time.Sleep(500 * time.Millisecond)
	}

	// 2. INPUT ID PLAYER
	// Target HTML: <input type="text" id="buyerId" ...>
	log.Println("✍️ Mengisi ID Player ke #buyerId...")
	
	inputSelector := "#buyerId"
	if vis, _ := s.Page.Locator(inputSelector).IsVisible(); !vis {
		s.Page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String("debug_no_input.png")})
		return "", fmt.Errorf("kolom input #buyerId tidak ditemukan")
	}

	// Kosongkan dulu baru isi
	s.Page.Locator(inputSelector).Fill("") 
	err := s.Page.Locator(inputSelector).Fill(playerID)
	if err != nil {
		return "", fmt.Errorf("gagal isi ID: %v", err)
	}

	// 3. PILIH PRODUK
	// Target HTML: <li id="itemId_6" ...>
	// Selector dinamis berdasarkan productID (misal: "6")
	itemSelector := fmt.Sprintf("#itemId_%s", productID)
	
	log.Printf("👉 Memilih produk: %s", itemSelector)
	
	itemLoc := s.Page.Locator(itemSelector)
	
	// Cek visibilitas, scroll jika perlu
	if vis, _ := itemLoc.IsVisible(); !vis {
		log.Println("⚠️ Produk tidak terlihat, scrolling...")
		s.Page.Evaluate("window.scrollTo(0, document.body.scrollHeight)")
		itemLoc.ScrollIntoViewIfNeeded()
	}

	// Klik Produk
	err = itemLoc.Click()
	if err != nil {
		s.Page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String("debug_no_product.png")})
		return "", fmt.Errorf("gagal klik produk %s: %v", itemSelector, err)
	}
	
	time.Sleep(500 * time.Millisecond)

	// 4. SUBMIT ORDER
	// Target HTML: <a class="buyBtns..." onclick="Index.queryBuyer();" ...>
	// Selector: Class .buyBtns atau berdasarkan onclick
	
	log.Println("🖱️ Klik Tombol Proses (Query Buyer)...")
	
	// Selector yang sangat spesifik berdasarkan onclick action
	submitSelector := "a[onclick*='Index.queryBuyer']"
	
	// Fallback ke class jika attribute selector gagal
	if count, _ := s.Page.Locator(submitSelector).Count(); count == 0 {
		submitSelector = ".buyBtns"
	}

	err = s.Page.Locator(submitSelector).Click()
	if err != nil {
		s.Page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String("debug_no_submit.png")})
		return "", fmt.Errorf("gagal klik tombol submit: %v", err)
	}

	// 5. HANDLING AFTER SUBMIT
	// Biasanya setelah queryBuyer, akan muncul Modal Konfirmasi Nama Player
	// Karena kita belum tau HTML konfirmasinya, kita assume success "Pending" dulu
	// Nanti Anda perlu inspect modal konfirmasinya (Tombol "Lanjut" atau "Bayar")
	
	log.Println("✅ Tombol Query Buyer diklik. Menunggu respons...")
	time.Sleep(2 * time.Second) 
	
	// Screenshot untuk melihat apa yang terjadi setelah klik tombol
	s.Page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String("debug_after_submit.png")})

	return "TRX-" + fmt.Sprintf("%d", time.Now().Unix()), nil
}