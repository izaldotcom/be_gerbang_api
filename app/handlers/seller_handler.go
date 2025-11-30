package handlers

import (
	"log"
	"net/http"
	"os"

	"gerbangapi/app/services/scraper" // Import service scraper

	"github.com/labstack/echo/v4"
)

// Endpoint: GET /api/v1/seller/products
func SellerProducts(c echo.Context) error {
	// (Biarkan dummy dulu untuk list produk)
	return c.JSON(http.StatusOK, echo.Map{
		"message": "List produk internal",
		"data": []echo.Map{
			{"product_code": "1M", "price": 1000, "status": "active"},
			{"product_code": "100M", "price": 6000, "status": "active"},
		},
	})
}

// Endpoint: POST /api/v1/seller/order
func SellerOrder(c echo.Context) error {
	// 1. Ambil Data dari Context (Hasil Middleware Security)
	sellerID := c.Get("seller_id")

	// 2. Parse Request Body
	type Req struct {
		ProductCode string `json:"product_code"` // Misal: "1M", "100M"
		Destination string `json:"destination"`  // ID Player Tujuan
		RefID       string `json:"ref_id"`       // ID Unik dari seller
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request body"})
	}

	log.Printf("📥 New Order received from Seller %v: %s -> %s", sellerID, req.ProductCode, req.Destination)

	// ---------------------------------------------------------
	// 3. EKSEKUSI SCRAPING (Mitra Higgs)
	// ---------------------------------------------------------
	
	// A. Init Browser
	// (Catatan: Di production, sebaiknya browser standby/worker pool, jangan init tiap request biar cepat)
	svc, err := scraper.NewMitraHiggsService()
	if err != nil {
		log.Println("❌ Gagal start browser:", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Internal Server Error (Browser Init)"})
	}
	defer svc.Close() // Pastikan browser tertutup setelah selesai request

	// B. Login (Otomatis pakai Cookie Redis jika ada)
	mhUser := os.Getenv("MH_USERNAME")
	mhPass := os.Getenv("MH_PASSWORD")
	
	err = svc.Login(mhUser, mhPass)
	if err != nil {
		log.Println("❌ Gagal Login MitraHiggs:", err)
		return c.JSON(http.StatusBadGateway, echo.Map{"error": "Gagal login ke provider"})
	}

	// C. Place Order (Submit Transaksi)
	// Fungsi PlaceOrder akan mencari ID Player dan klik nominal
	trxID, err := svc.PlaceOrder(req.Destination, req.ProductCode)
	if err != nil {
		log.Println("❌ Gagal Place Order:", err)
		// Screenshot error jika perlu
		// svc.Page.Screenshot(...) 
		return c.JSON(http.StatusBadGateway, echo.Map{"error": "Gagal memproses order: " + err.Error()})
	}

	// ---------------------------------------------------------
	// 4. RESPONSE SUKSES
	// ---------------------------------------------------------
	
	return c.JSON(http.StatusOK, echo.Map{
		"message":      "Order processed successfully",
		"trx_id":       trxID,              // ID dari MitraHiggs (jika berhasil di-scrape) atau Dummy
		"seller_id":    sellerID,
		"product":      req.ProductCode,
		"destination":  req.Destination,
		"status":       "success",          // Atau 'pending' jika masuk antrian
		"provider":     "MitraHiggs",
	})
}

// Endpoint: GET /api/v1/seller/status
func SellerStatus(c echo.Context) error {
	// (Biarkan seperti sebelumnya)
	return c.JSON(http.StatusOK, echo.Map{"status": "dummy"})
}