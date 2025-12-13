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

// === PLACE ORDER (FIX: USER'S CLICK METHOD + CORRECT LOOPING) ===
func (s *MitraHiggsService) PlaceOrder(playerID, productID string, quantity int) (string, error) {
	log.Printf("🛒 Memulai %d Transaksi untuk Player %s (Item %s)", quantity, playerID, productID)

	// 1. Persiapan Awal
	s.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateDomcontentloaded})
	
	// Handle Popup Iklan Awal (Persis kode Anda)
	if count, _ := s.Page.Locator("#thickdivInvitation").Count(); count > 0 {
		s.Page.Evaluate("try { hideInvitation(); } catch(e) {}")
		if vis, _ := s.Page.Locator("#thickdivInvitation").IsVisible(); vis {
			s.Page.Locator("#thickdivInvitation").Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)})
		}
		s.Page.Locator("#thickdivInvitation").WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateHidden, 
			Timeout: playwright.Float(3000),
		})
		time.Sleep(500 * time.Millisecond)
	}

	var successTrx []string
	
	// ============================================================
	// MULAI LOOPING TRANSAKSI (Quantity 1 s/d X)
	// ============================================================
	for i := 1; i <= quantity; i++ {
		log.Printf("🔄 [LOOP %d/%d] Memulai Siklus...", i, quantity)

		// A. BERSIHKAN POPUP SISA TRANSAKSI SEBELUMNYA
		// Kita tutup popup hasil transaksi sebelumnya agar produk bisa diklik lagi
		s.Page.Evaluate("try { Common.close(); } catch(e) {}") 
		time.Sleep(800 * time.Millisecond)

		// ============================================================
		// LANGKAH 2: PILIH PRODUK (METODE KLIK DARI KODE ANDA)
		// ============================================================
		itemSelector := fmt.Sprintf("#itemId_%s", productID)
		
		// 1. Pastikan elemen produk ada
		if count, _ := s.Page.Locator(itemSelector).Count(); count == 0 {
			return "", fmt.Errorf("produk dengan selector %s tidak ditemukan", itemSelector)
		}

		// 2. CEK STOK
		stockSelector := fmt.Sprintf("%s .itemPriceLabel", itemSelector)
		if txt, _ := s.Page.Locator(stockSelector).InnerText(); strings.TrimSpace(txt) == "0" {
			return "", fmt.Errorf("stok habis saat loop ke-%d", i)
		}

		// 3. KLIK PRODUK (MENGGUNAKAN METODE FORCE CLICK ANDA)
		log.Println("   -> Klik Produk...")
		err := s.Page.Locator(itemSelector).Click(playwright.LocatorClickOptions{
			Force: playwright.Bool(true),
		})
		if err != nil {
			return "", fmt.Errorf("gagal klik produk di loop ke-%d: %v", i, err)
		}

		// Jeda agar seleksi produk teregistrasi
		time.Sleep(800 * time.Millisecond)

		// ============================================================
		// LANGKAH 3: INPUT ID PLAYER
		// ============================================================
		inputSelector := "#buyerId"
		
		// Cek dulu apakah ID sudah terisi (biar lebih cepat)
		currentVal, _ := s.Page.Locator(inputSelector).InputValue()
		
		if currentVal != playerID {
			log.Println("   -> Mengisi ID Player...")
			
			// Pastikan input visible
			s.Page.WaitForSelector(inputSelector, playwright.PageWaitForSelectorOptions{
				State: playwright.WaitForSelectorStateVisible,
			})

			s.Page.Locator(inputSelector).Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)})
			s.Page.Locator(inputSelector).Fill("") 
			
			err = s.Page.Locator(inputSelector).Type(playerID, playwright.LocatorTypeOptions{
				Delay: playwright.Float(100), 
			})
			if err != nil {
				return "", fmt.Errorf("gagal mengetik ID: %v", err)
			}
		} else {
			log.Println("   -> ID Player sudah terisi.")
		}

		// ============================================================
		// LANGKAH 4: PROSES TRANSAKSI (Query & Confirm)
		// ============================================================
		
		// Trigger Query
		s.Page.Evaluate("try { Index.queryBuyer(); } catch(e) {}")

		// Tunggu Modal Konfirmasi Muncul
		log.Println("   -> Menunggu modal...")
		modalSelector := "#queryBuyerName" 
		modalAppeared := false
		
		for tryCount := 0; tryCount < 10; tryCount++ { 
			if vis, _ := s.Page.Locator(modalSelector).IsVisible(); vis {
				if txt, _ := s.Page.Locator(modalSelector).InnerText(); txt != "" {
					modalAppeared = true
					break
				}
			}
			// Cek Error (misal User Tidak Ada)
			if vis, _ := s.Page.Locator("#publicTip").IsVisible(); vis {
				txt, _ := s.Page.Locator("#publicTxt").InnerText()
				// Filter kata 'Berhasil' agar sisa popup sukses tidak dianggap error
				if txt != "" && txt != "null" && !strings.Contains(txt, "Berhasil") { 
					s.Page.Evaluate("Common.close()")
					return "", fmt.Errorf("GAGAL SAAT CEK USER (Loop %d): %s", i, txt)
				}
			}
			time.Sleep(500 * time.Millisecond)
		}

		if !modalAppeared {
			s.Page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String("debug_loop_modal_fail.png")})
			return "", fmt.Errorf("timeout: modal konfirmasi tidak muncul di loop ke-%d", i)
		}

		// Jeda agar tombol konfirmasi siap
		time.Sleep(1000 * time.Millisecond)

		// KLIK KONFIRMASI (RETRY MECHANISM)
		log.Println("   -> Klik Konfirmasi...")
		confirmSelector := "a[onclick*='Index.sellItem']"
		transactionConfirmed := false
		
		for attempt := 1; attempt <= 3; attempt++ {
			// Klik
			if vis, _ := s.Page.Locator(confirmSelector).IsVisible(); vis {
				s.Page.Locator(confirmSelector).Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)})
			}

			// Tunggu Respon (3 detik per attempt)
			for waitTick := 0; waitTick < 8; waitTick++ {
				time.Sleep(500 * time.Millisecond)
				
				if vis, _ := s.Page.Locator("#publicTip").IsVisible(); vis {
					resultText, _ := s.Page.Locator("#publicTxt").InnerText()
					log.Printf("   -> Server Response (Loop %d): %s", i, resultText)
					
					if resultText == "Saldo tidak cukup" || resultText == "Gagal" || resultText == "Error" {
						s.Page.Evaluate("Common.close()")
						return "", fmt.Errorf("TRANSAKSI DITOLAK: %s", resultText)
					}
					
					// Jika respon ada (sukses), tandai flag true
					transactionConfirmed = true
					// Kita tutup popup di AWAL loop berikutnya (Step A), jangan sekarang.
					// Biarkan popup sukses terlihat sebentar.
					break 
				}
			}
			if transactionConfirmed { break }
			log.Printf("   ⚠️ Retry klik konfirmasi ke-%d...", attempt)
		}

		if !transactionConfirmed {
			s.Page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String("debug_loop_no_resp.png")})
			return "", fmt.Errorf("transaksi gagal di loop ke-%d: tombol diklik tapi tidak ada respon", i)
		}

		// Simpan Sukses
		trxID := fmt.Sprintf("TRX-%d-%d", time.Now().Unix(), i)
		successTrx = append(successTrx, trxID)
		log.Printf("✅ Transaksi ke-%d Berhasil.", i)

		// Jeda sebelum loop berikutnya (PENTING untuk animasi)
		time.Sleep(2000 * time.Millisecond) 
	}

	// ============================================================
	// FINISH
	// ============================================================
	log.Println("🏁 Semua looping selesai.")
	finalTrxString := fmt.Sprintf("%v", successTrx)
	return finalTrxString, nil
}