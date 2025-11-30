package handlers

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// Endpoint: GET /api/v1/seller/products
func SellerProducts(c echo.Context) error {
	// TODO: Ambil data produk dari tabel InternalProduct
	// Saat ini kita return dummy data dulu
	return c.JSON(http.StatusOK, echo.Map{
		"message": "List produk internal",
		"data": []echo.Map{
			{"product_code": "PULSA10", "price": 10500, "status": "active"},
			{"product_code": "PLN20", "price": 20500, "status": "active"},
		},
	})
}

// Endpoint: POST /api/v1/seller/order
func SellerOrder(c echo.Context) error {
	// Ambil data dari Context (diset oleh middleware Security)
	sellerID := c.Get("seller_id")

	type Req struct {
		ProductCode string `json:"product_code"`
		Destination string `json:"destination"`
		RefID       string `json:"ref_id"` // ID Unik dari seller
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request body"})
	}

	// TODO: Masuk ke logic transaksi (Cek saldo, cek produk, lempar ke worker)
	
	return c.JSON(http.StatusOK, echo.Map{
		"message":      "Order accepted",
		"trx_id":       "TRX-DUMMY-12345",
		"seller_id":    sellerID,
		"product":      req.ProductCode,
		"destination":  req.Destination,
		"status":       "pending",
	})
}

// Endpoint: GET /api/v1/seller/status
func SellerStatus(c echo.Context) error {
	trxID := c.QueryParam("trx_id")
	refID := c.QueryParam("ref_id")

	if trxID == "" && refID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "trx_id or ref_id is required"})
	}

	// TODO: Cek status transaksi di database
	
	return c.JSON(http.StatusOK, echo.Map{
		"trx_id": trxID,
		"ref_id": refID,
		"status": "success",
		"sn":     "1234567890123456", // Serial Number (Bukti sukses)
	})
}