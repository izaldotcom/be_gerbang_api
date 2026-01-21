package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os" // [ADDED] Perlu import os untuk cek folder driver
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
	// --- LOGIC TAMBAHAN UNTUK SERVER UBUNTU ---
	// Path driver yang kita install manual di /opt
	serverDriverPath := "/opt/playwright/ms-playwright-go/1.52.0"
	
	var runOptions *playwright.RunOptions

	// Cek apakah folder driver server ada?
	if _, err := os.Stat(serverDriverPath); err == nil {
		// Jika ada (di Server), gunakan path ini
		log.Println("🖥️  Terdeteksi lingkungan Server: Menggunakan Custom Driver Path:", serverDriverPath)
		runOptions = &playwright.RunOptions{
			DriverDirectory: serverDriverPath,
		}
	} else {
		// Jika tidak ada (di Laptop/Lokal), biarkan default (nil)
		log.Println("💻 Terdeteksi lingkungan Lokal: Menggunakan Default Driver Path")
	}
	// -------------------------------------------

	// Jalankan Playwright dengan opsi yang sudah ditentukan
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

// === PLACE ORDER (OPTIMIZED) ===
func (s *MitraHiggsService) PlaceOrder(playerID, productID string, quantity int) (string, error) {
	log.Printf("🛒 Memulai %d Transaksi untuk Player %s (Item %s)", quantity, playerID, productID)

	s.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateDomcontentloaded})

	// Handle Popup Awal
	s.Page.Evaluate("try { hideInvitation(); Common.close(); } catch(e) {}")

	var successTrx []string

	for i := 1; i <= quantity; i++ {
		if i == 1 || i%5 == 0 {
			log.Printf("🔄 Loop %d/%d...", i, quantity)
		}

		// A. BERSIHKAN POPUP
		s.Page.Evaluate("try { Common.close(); } catch(e) {}")
		time.Sleep(200 * time.Millisecond)

		// B. PILIH PRODUK
		itemSelector := fmt.Sprintf("#itemId_%s", productID)

		if count, _ := s.Page.Locator(itemSelector).Count(); count == 0 {
			return "", fmt.Errorf("produk %s tidak ditemukan", itemSelector)
		}

		stockSelector := fmt.Sprintf("%s .itemPriceLabel", itemSelector)
		if txt, _ := s.Page.Locator(stockSelector).InnerText(); strings.TrimSpace(txt) == "0" {
			return "", fmt.Errorf("stok habis saat loop ke-%d", i)
		}

		err := s.Page.Locator(itemSelector).Click(playwright.LocatorClickOptions{
			Force: playwright.Bool(true),
		})
		if err != nil {
			return "", fmt.Errorf("gagal klik produk loop %d: %v", i, err)
		}
		time.Sleep(300 * time.Millisecond)

		// C. INPUT ID PLAYER
		inputSelector := "#buyerId"
		currentVal, _ := s.Page.Locator(inputSelector).InputValue()

		if currentVal != playerID {
			s.Page.Locator(inputSelector).Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)})
			s.Page.Locator(inputSelector).Fill("")
			err = s.Page.Locator(inputSelector).Type(playerID, playwright.LocatorTypeOptions{
				Delay: playwright.Float(20),
			})
			if err != nil {
				return "", fmt.Errorf("gagal ketik ID: %v", err)
			}
		}

		// D. PROSES TRANSAKSI
		s.Page.Evaluate("try { Index.queryBuyer(); } catch(e) {}")

		modalSelector := "#queryBuyerName"
		modalAppeared := false

		for tryCount := 0; tryCount < 15; tryCount++ {
			if vis, _ := s.Page.Locator(modalSelector).IsVisible(); vis {
				if txt, _ := s.Page.Locator(modalSelector).InnerText(); txt != "" {
					modalAppeared = true
					break
				}
			}

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
			return "", fmt.Errorf("timeout: modal konfirmasi tidak muncul loop %d", i)
		}

		time.Sleep(500 * time.Millisecond)

		confirmSelector := "a[onclick*='Index.sellItem']"
		transactionConfirmed := false

		for attempt := 1; attempt <= 3; attempt++ {
			if vis, _ := s.Page.Locator(confirmSelector).IsVisible(); vis {
				s.Page.Locator(confirmSelector).Click(playwright.LocatorClickOptions{Force: playwright.Bool(true)})
			}

			for waitTick := 0; waitTick < 10; waitTick++ {
				time.Sleep(300 * time.Millisecond)

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
			if transactionConfirmed {
				break
			}
		}

		if !transactionConfirmed {
			return "", fmt.Errorf("loop %d: tombol diklik tapi tidak ada respon", i)
		}

		trxID := fmt.Sprintf("TRX-%d-%d", time.Now().Unix(), i)
		successTrx = append(successTrx, trxID)

		time.Sleep(800 * time.Millisecond)
	}

	log.Println("🏁 Looping selesai.")
	finalTrxString := fmt.Sprintf("%v", successTrx)
	return finalTrxString, nil
}