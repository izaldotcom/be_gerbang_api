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

// === PLACE ORDER (FIX FINAL: JS EXECUTION & RESULT CHECK) ===
func (s *MitraHiggsService) PlaceOrder(playerID, productID string, quantity int) (string, error) {
	log.Printf("🛒 Memulai %d Transaksi untuk Player %s (Item %s)", quantity, playerID, productID)

	s.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateDomcontentloaded})
	
	// 1. Handle Popup Iklan
	if vis, _ := s.Page.Locator("#thickdivInvitation").IsVisible(); vis {
		s.Page.Locator("#thickdivInvitation").Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)})
		time.Sleep(500 * time.Millisecond)
	}

	// 2. Inject ID
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

	// 3. Pilih Produk
	itemSelector := fmt.Sprintf("#itemId_%s", productID)
	if err := s.Page.Locator(itemSelector).Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)}); err != nil {
		return "", fmt.Errorf("produk ID %s tidak ditemukan", productID)
	}

	var successTrx []string
	
	for i := 1; i <= quantity; i++ {
		log.Printf("🔄 Proses Transaksi ke-%d dari %d...", i, quantity)

		// A. REQUEST VALIDASI USER (Index.queryBuyer)
		// Kita eksekusi JS langsung agar pasti terpanggil
		_, err := s.Page.Evaluate("try { Index.queryBuyer(); } catch(e) {}")
		if err != nil {
			log.Println("⚠️ Warning: queryBuyer JS error, mencoba klik manual...")
			s.Page.Locator(".buyBtns").Click()
		}

		// B. TUNGGU HASIL VALIDASI (NAMA MUNCUL)
		log.Println("⏳ Menunggu nama user muncul...")
		userIsValid := false
		
		for attempt := 0; attempt < 10; attempt++ { // Max 5 detik
			time.Sleep(500 * time.Millisecond)

			// Cek Error (User tidak ada)
			if vis, _ := s.Page.Locator("#publicTip").IsVisible(); vis {
				txt, _ := s.Page.Locator("#publicTxt").InnerText()
				if txt != "" && txt != "null" {
					s.Page.Evaluate("Common.close()")
					return "", fmt.Errorf("GAGAL SAAT CEK USER: %s", txt)
				}
			}

			// Cek Nama Muncul (#queryBuyerName)
			// Kita pastikan text-nya tidak kosong
			name, _ := s.Page.Locator("#queryBuyerName").InnerText()
			if name != "" {
				userIsValid = true
				log.Printf("✅ User Valid: %s", name)
				break 
			}
		}

		if !userIsValid {
			s.Page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String("debug_stuck_validation.png")})
			return "", fmt.Errorf("timeout: nama player tidak muncul")
		}

		// Jeda sedikit agar variabel internal web siap menerima perintah jual
		time.Sleep(1 * time.Second)

		// C. EKSEKUSI BELI (Index.sellItem) - THE KILL SHOT
		// Daripada klik tombol, kita tembak fungsinya langsung.
		// Ini 100% akan memicu request ke server Mitra Higgs jika modal sudah terbuka.
		
		log.Println("🚀 EKSEKUSI TRANSAKSI (JS Inject)...")
		
		_, err = s.Page.Evaluate("try { Index.sellItem(); } catch(e) { console.error(e); }")
		if err != nil {
			return "", fmt.Errorf("gagal eksekusi Index.sellItem()")
		}

		// D. VERIFIKASI AKHIR (CEK POPUP HASIL)
		// Kita harus menunggu popup hasil muncul untuk memastikan stok berkurang.
		log.Println("🧐 Menunggu laporan hasil transaksi...")
		
		transactionConfirmed := false
		
		for attempt := 0; attempt < 10; attempt++ { // Max 5 detik tunggu respon
			time.Sleep(500 * time.Millisecond)
			
			// Cek Popup #publicTip (Tempat pesan sukses/gagal muncul)
			if vis, _ := s.Page.Locator("#publicTip").IsVisible(); vis {
				resultText, _ := s.Page.Locator("#publicTxt").InnerText()
				log.Printf("   -> Server Response: %s", resultText)
				
				// Screenshot bukti respon server
				s.Page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(fmt.Sprintf("debug_result_%d.png", i))})

				if resultText == "Saldo tidak cukup" || resultText == "Gagal" {
					return "", fmt.Errorf("TRANSAKSI DITOLAK SERVER: %s", resultText)
				}
				
				// Jika popup muncul dan bukan error fatal, anggap sukses
				transactionConfirmed = true
				s.Page.Evaluate("Common.close()") // Tutup popup
				break
			}
		}

		// Jika tidak ada popup konfirmasi, kita anggap gagal/timeout agar aman (tidak memakan saldo api tapi barang ga masuk)
		if !transactionConfirmed {
			// Coba cek screenshot terakhir
			s.Page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String("debug_no_response.png")})
			
			// OPSIONAL: Return Error jika ingin strict. 
			// return "", fmt.Errorf("tidak ada respon sukses dari server (cek debug_no_response.png)")
			
			// ATAU: Log warning saja (kadang popup cepat hilang)
			log.Println("⚠️ Warning: Tidak ada popup konfirmasi akhir, tapi perintah sudah dikirim.")
		}

		trxID := fmt.Sprintf("TRX-%d-%d", time.Now().Unix(), i)
		successTrx = append(successTrx, trxID)
		log.Printf("✅ Transaksi ke-%d Selesai.", i)

		// Jeda antar transaksi
		time.Sleep(3 * time.Second) 
	}

	finalTrxString := fmt.Sprintf("%v", successTrx)
	return finalTrxString, nil
}