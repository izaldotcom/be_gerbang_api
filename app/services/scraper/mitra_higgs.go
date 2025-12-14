package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
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

func NewMitraHiggsService(isDebug bool) (*MitraHiggsService, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, err
	}

	// OPTIMASI 1: HEADLESS TRUE & CHROMIUM ARGS
	// Menggunakan headless agar tidak merender UI (lebih cepat & ringan CPU)
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
	// Cek visibilitas dengan timeout sangat singkat
	isPasswordVisible := false
	if vis, _ := s.Page.Locator("input[type='password']").IsVisible(); vis {
		isPasswordVisible = true
	}

	if !isPasswordVisible {
		// Klik "ID Login"
		s.Page.Locator("span[name='index-html-id-login']").Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)})
		
		// Fallback klik text jika span gagal
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
	// Tunggu redirect
	err = s.Page.WaitForURL("**/trade/index**", playwright.PageWaitForURLOptions{
		Timeout: playwright.Float(15000), // Timeout dipercepat
	})

	if err != nil {
		if vis, _ := s.Page.Locator(".alert-danger").IsVisible(); vis {
			msg, _ := s.Page.Locator(".alert-danger").TextContent()
			return fmt.Errorf("login ditolak: %s", msg)
		}
		// OPTIMASI 2: HAPUS SCREENSHOT
		return fmt.Errorf("login timeout/gagal")
	}

	log.Println("✅ Login Sukses.")

	// 5. TUTUP POPUP (REQUEST KHUSUS)
	// Gunakan JS Inject langsung untuk menutup, lebih cepat daripada menunggu animasi klik
	s.Page.Evaluate(`
		try { 
			hideInvitation(); 
			document.getElementById('thickdivInvitation').style.display = 'none';
		} catch(e) {}
	`)
	
	// Simpan Cookie
	cookies, _ := s.Context.Cookies()
	cookieBytes, _ := json.Marshal(cookies)
	db.Rdb.Set(ctx, s.RedisKey, string(cookieBytes), 24*time.Hour)
	
	return nil
}

// === PLACE ORDER (OPTIMIZED) ===
func (s *MitraHiggsService) PlaceOrder(playerID, productID string, quantity int) (string, error) {
	log.Printf("🛒 Memulai %d Transaksi untuk Player %s (Item %s)", quantity, playerID, productID)

	s.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateDomcontentloaded})
	
	// Handle Popup Awal (Agresif via JS)
	s.Page.Evaluate("try { hideInvitation(); Common.close(); } catch(e) {}")

	var successTrx []string
	
	// ============================================================
	// MULAI LOOPING TRANSAKSI
	// ============================================================
	for i := 1; i <= quantity; i++ {
		// OPTIMASI: Logging dipersingkat agar tidak spam buffer console berlebihan
		if i == 1 || i%5 == 0 {
			log.Printf("🔄 Loop %d/%d...", i, quantity)
		}

		// A. BERSIHKAN POPUP
		s.Page.Evaluate("try { Common.close(); } catch(e) {}") 
		// Sleep dikurangi: 800ms -> 200ms (cukup untuk JS eksekusi close)
		time.Sleep(200 * time.Millisecond)

		// ============================================================
		// LANGKAH 2: PILIH PRODUK
		// ============================================================
		itemSelector := fmt.Sprintf("#itemId_%s", productID)
		
		// Cek keberadaan elemen (Fail Fast)
		if count, _ := s.Page.Locator(itemSelector).Count(); count == 0 {
			return "", fmt.Errorf("produk %s tidak ditemukan", itemSelector)
		}

		// Cek Stok (Fail Fast)
		stockSelector := fmt.Sprintf("%s .itemPriceLabel", itemSelector)
		if txt, _ := s.Page.Locator(stockSelector).InnerText(); strings.TrimSpace(txt) == "0" {
			return "", fmt.Errorf("stok habis saat loop ke-%d", i)
		}

		// Klik Produk
		err := s.Page.Locator(itemSelector).Click(playwright.LocatorClickOptions{
			Force: playwright.Bool(true),
		})
		if err != nil {
			return "", fmt.Errorf("gagal klik produk loop %d: %v", i, err)
		}

		// Jeda dikurangi: 800ms -> 300ms (Cukup untuk highlight item terpilih)
		time.Sleep(300 * time.Millisecond)

		// ============================================================
		// LANGKAH 3: INPUT ID PLAYER
		// ============================================================
		inputSelector := "#buyerId"
		
		// Cek nilai saat ini untuk menghindari pengetikan ulang
		currentVal, _ := s.Page.Locator(inputSelector).InputValue()
		
		if currentVal != playerID {
			s.Page.Locator(inputSelector).Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)})
			s.Page.Locator(inputSelector).Fill("") 
			
			// OPTIMASI 3: PERCEPAT PENGETIKAN
			// Delay 20ms sudah cukup aman untuk menipu validasi regex, 100ms terlalu lama.
			err = s.Page.Locator(inputSelector).Type(playerID, playwright.LocatorTypeOptions{
				Delay: playwright.Float(20), 
			})
			if err != nil {
				return "", fmt.Errorf("gagal ketik ID: %v", err)
			}
		}

		// ============================================================
		// LANGKAH 4: PROSES TRANSAKSI
		// ============================================================
		
		// Trigger Query via JS (Lebih cepat daripada cari tombol Query lalu klik)
		s.Page.Evaluate("try { Index.queryBuyer(); } catch(e) {}")

		// Tunggu Modal Konfirmasi
		modalSelector := "#queryBuyerName" 
		modalAppeared := false
		
		// Loop cek modal (Max 3 detik)
		for tryCount := 0; tryCount < 15; tryCount++ { 
			if vis, _ := s.Page.Locator(modalSelector).IsVisible(); vis {
				// Cek teks inner untuk memastikan data sudah load
				if txt, _ := s.Page.Locator(modalSelector).InnerText(); txt != "" {
					modalAppeared = true
					break
				}
			}
			
			// Cek Error Cepat
			if vis, _ := s.Page.Locator("#publicTip").IsVisible(); vis {
				txt, _ := s.Page.Locator("#publicTxt").InnerText()
				if txt != "" && txt != "null" && !strings.Contains(txt, "Berhasil") { 
					s.Page.Evaluate("Common.close()")
					return "", fmt.Errorf("GAGAL CEK USER (Loop %d): %s", i, txt)
				}
			}
			time.Sleep(200 * time.Millisecond)
		}

		if !modalAppeared {
			// OPTIMASI: Hapus Screenshot debug
			return "", fmt.Errorf("timeout: modal konfirmasi tidak muncul loop %d", i)
		}

		// Jeda sedikit agar tombol konfirmasi clickable
		time.Sleep(500 * time.Millisecond)

		// KLIK KONFIRMASI (RETRY MECHANISM)
		confirmSelector := "a[onclick*='Index.sellItem']"
		transactionConfirmed := false
		
		for attempt := 1; attempt <= 3; attempt++ {
			if vis, _ := s.Page.Locator(confirmSelector).IsVisible(); vis {
				s.Page.Locator(confirmSelector).Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)})
			}

			// Tunggu Respon (Maks 3 detik)
			for waitTick := 0; waitTick < 10; waitTick++ {
				time.Sleep(300 * time.Millisecond) // Cek setiap 300ms
				
				if vis, _ := s.Page.Locator("#publicTip").IsVisible(); vis {
					resultText, _ := s.Page.Locator("#publicTxt").InnerText()
					
					if resultText == "Saldo tidak cukup" || resultText == "Gagal" || resultText == "Error" {
						s.Page.Evaluate("Common.close()")
						return "", fmt.Errorf("DITOLAK: %s", resultText)
					}
					
					transactionConfirmed = true
					break 
				}
			}
			if transactionConfirmed { break }
		}

		if !transactionConfirmed {
			// OPTIMASI: Hapus Screenshot debug
			return "", fmt.Errorf("loop %d: tombol diklik tapi tidak ada respon", i)
		}

		trxID := fmt.Sprintf("TRX-%d-%d", time.Now().Unix(), i)
		successTrx = append(successTrx, trxID)
		
		// OPTIMASI 4: KURANGI WAKTU TUNGGU ANTAR TRANSAKSI
		// Dari 2000ms -> 800ms. Popup sukses akan ditutup paksa oleh 
		// 'Common.close()' di awal loop berikutnya.
		time.Sleep(800 * time.Millisecond) 
	}

	log.Println("🏁 Looping selesai.")
	finalTrxString := fmt.Sprintf("%v", successTrx)
	return finalTrxString, nil
}