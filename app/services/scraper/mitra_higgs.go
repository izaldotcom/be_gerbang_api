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

// === PLACE ORDER (FIX: TOMBOL BELI VIA JS) ===
func (s *MitraHiggsService) PlaceOrder(playerID, productID string) (string, error) {
	log.Printf("🛒 Memulai Transaksi: Player %s, Item ID %s", playerID, productID)

	// 1. TUNGGU HALAMAN STABIL
	s.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateDomcontentloaded,
	})
	
	// Handle Popup (Best Effort)
	if vis, _ := s.Page.Locator("#thickdivInvitation").IsVisible(); vis {
		s.Page.Locator("#thickdivInvitation").Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)})
		time.Sleep(500 * time.Millisecond)
	}

	// 2. INPUT ID PLAYER (JS INJECTION)
	log.Println("💉 Menyuntikkan ID via JavaScript...")
	jsScript := fmt.Sprintf(`(() => {
		var input = document.getElementById('buyerId');
		if (input) {
			input.value = '%s';
			input.dispatchEvent(new Event('input', { bubbles: true }));
			input.dispatchEvent(new Event('change', { bubbles: true }));
			return true;
		}
		return false;
	})()`, playerID)

	if _, err := s.Page.Evaluate(jsScript); err != nil {
		return "", fmt.Errorf("gagal inject ID: %v", err)
	}

	// 3. PILIH PRODUK
	itemSelector := fmt.Sprintf("#itemId_%s", productID)
	log.Printf("👉 Memilih produk: %s", itemSelector)
	
	// Cek keberadaan produk
	if count, _ := s.Page.Locator(itemSelector).Count(); count == 0 {
		return "", fmt.Errorf("produk ID #itemId_%s tidak ditemukan (Cek Seeder Database Anda)", productID)
	}

	// Klik Produk (Force)
	err := s.Page.Locator(itemSelector).Click(playwright.LocatorClickOptions{
		Force: playwright.Bool(true),
	})
	if err != nil {
		return "", fmt.Errorf("gagal klik produk: %v", err)
	}
	
	time.Sleep(500 * time.Millisecond)

	// ============================================================
	// 4. SUBMIT ORDER (FIX UTAMA: JS EXECUTION)
	// ============================================================
	// Target: <a onclick="Index.queryBuyer();" ...>
	// Masalah: Element ini tidak punya text, hanya background image.
	// Solusi: Kita panggil fungsi JS aslinya langsung, atau cari berdasarkan class.
	
	log.Println("🖱️ Mencoba Submit Order...")
	
	// STRATEGI 1: Cari elemen berdasarkan Class .buyBtns dan Klik
	submitSelector := ".buyBtns"
	
	// Cek apakah elemen ada di DOM (tidak harus visible, kadang ketutup footer)
	if count, _ := s.Page.Locator(submitSelector).Count(); count > 0 {
		log.Println("   -> Tombol .buyBtns ditemukan, melakukan Force Click...")
		err := s.Page.Locator(submitSelector).Click(playwright.LocatorClickOptions{
			Force: playwright.Bool(true), // Force click walaupun element dianggap hidden/kosong
		})
		if err != nil {
			log.Println("⚠️ Gagal klik tombol, mencoba Plan B (JS Exec)...")
			// Plan B lanjut di bawah
		}
	} else {
		log.Println("⚠️ Tombol .buyBtns tidak terdeteksi selector, mencoba Plan B (JS Exec)...")
	}

	// STRATEGI 2 (PLAN B): Panggil fungsi JS website-nya langsung
	// Ini meniru apa yang terjadi saat user klik tombol (onclick="Index.queryBuyer()")
	log.Println("🚀 Eksekusi JS: Index.queryBuyer()...")
	
	_, err = s.Page.Evaluate("try { Index.queryBuyer(); } catch(e) { console.error(e); }")
	if err != nil {
		s.Page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String("debug_submit_fail.png")})
		return "", fmt.Errorf("gagal eksekusi Index.queryBuyer(): %v", err)
	}

	log.Println("✅ Perintah Submit dikirim ke Browser.")
	time.Sleep(2 * time.Second) 
	
	// Screenshot bukti sukses klik/submit
	s.Page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String("debug_after_submit.png")})

	return "TRX-" + fmt.Sprintf("%d", time.Now().Unix()), nil
}