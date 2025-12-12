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

// === PLACE ORDER (FIX: KLIK TOMBOL CONFIRM FISIK) ===
func (s *MitraHiggsService) PlaceOrder(playerID, productID string, quantity int) (string, error) {
	log.Printf("🛒 Memulai %d Transaksi untuk Player %s (Item %s)", quantity, playerID, productID)

	// 1. Persiapan Awal
	s.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateDomcontentloaded})
	
	// Handle Popup
	if vis, _ := s.Page.Locator("#thickdivInvitation").IsVisible(); vis {
		s.Page.Locator("#thickdivInvitation").Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)})
		time.Sleep(500 * time.Millisecond)
	}

	// 2. INJECT ID PLAYER
	log.Println("💉 Injecting Player ID...")
	jsInjectID := fmt.Sprintf(`(() => {
		var input = document.getElementById('buyerId');
		if (input) {
			input.value = '%s';
			input.dispatchEvent(new Event('input', { bubbles: true }));
			return true;
		}
		return false;
	})()`, playerID)
	
	if _, err := s.Page.Evaluate(jsInjectID); err != nil {
		return "", fmt.Errorf("gagal inject ID: %v", err)
	}

	// 3. PILIH PRODUK
	itemSelector := fmt.Sprintf("#itemId_%s", productID)
	if err := s.Page.Locator(itemSelector).Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)}); err != nil {
		return "", fmt.Errorf("produk ID %s tidak ditemukan", productID)
	}

	// 4. LOOPING TRANSAKSI
	var successTrx []string
	
	for i := 1; i <= quantity; i++ {
		log.Printf("🔄 Proses Transaksi ke-%d dari %d...", i, quantity)

		// ============================================================
		// TAHAP A: QUERY / CEK USER
		// ============================================================
		// Kita panggil queryBuyer via JS (karena tombolnya kadang susah diklik)
		_, err := s.Page.Evaluate("try { Index.queryBuyer(); } catch(e) {}")
		if err != nil {
			// Ignore error query, lanjut validasi visual
		}

		// ============================================================
		// TAHAP B: VALIDASI POPUP KONFIRMASI (TUNGGU NAMA MUNCUL)
		// ============================================================
		log.Println("⏳ Menunggu validasi user & tombol konfirmasi...")
		
		userIsValid := false
		for attempt := 0; attempt < 8; attempt++ { // Tunggu max 4 detik
			time.Sleep(500 * time.Millisecond)

			// 1. Cek Error User
			if vis, _ := s.Page.Locator("#publicTip").IsVisible(); vis {
				txt, _ := s.Page.Locator("#publicTxt").InnerText()
				if txt != "" {
					s.Page.Evaluate("Common.close()")
					return "", fmt.Errorf("TRANSAKSI GAGAL: %s", txt)
				}
			}

			// 2. Cek Nama Muncul (#queryBuyerName)
			if vis, _ := s.Page.Locator("#queryBuyerName").IsVisible(); vis {
				userIsValid = true
				break 
			}
		}

		if !userIsValid {
			s.Page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String("debug_timeout_validasi.png")})
			return "", fmt.Errorf("timeout: nama player tidak muncul setelah query")
		}

		// ============================================================
		// TAHAP C: EKSEKUSI FINAL (KLIK TOMBOL SELLITEM)
		// ============================================================
		// HTML Target: <a onclick="Index.sellItem();"></a>
		// Kita cari elemen yang punya atribut onclick berisi 'Index.sellItem'
		// Ini LEBIH AMAN daripada s.Page.Evaluate() karena memastikan tombolnya ada.
		
		log.Println("🚀 Mencari tombol Konfirmasi Beli...")
		
		sellBtnSelector := "a[onclick*='Index.sellItem']"
		
		// Tunggu tombol tersebut visible (biasanya muncul di modal/popup)
		_, err = s.Page.WaitForSelector(sellBtnSelector, playwright.PageWaitForSelectorOptions{
			State: playwright.WaitForSelectorStateVisible,
			Timeout: playwright.Float(5000),
		})

		if err != nil {
			s.Page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String("debug_no_sell_btn.png")})
			return "", fmt.Errorf("tombol konfirmasi (Index.sellItem) tidak ditemukan")
		}

		// KLIK TOMBOL TERSEBUT
		err = s.Page.Locator(sellBtnSelector).Click(playwright.LocatorClickOptions{
			Force: playwright.Bool(true),
		})
		
		if err != nil {
			return "", fmt.Errorf("gagal klik tombol sellItem: %v", err)
		}

		// Sukses
		trxID := fmt.Sprintf("TRX-%d-%d", time.Now().Unix(), i)
		successTrx = append(successTrx, trxID)

		log.Printf("✅ Transaksi ke-%d Berhasil diklik.", i)

		// Jeda agar modal tertutup & siap transaksi berikutnya
		time.Sleep(3 * time.Second) 
	}

	finalTrxString := fmt.Sprintf("%v", successTrx)
	log.Println("🏁 Selesai. TRX:", finalTrxString)

	return finalTrxString, nil
}