package digiflazz

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	digiservice "gerbangapi/app/services/digiflazz"
	"gerbangapi/app/utils"
	"gerbangapi/prisma/db"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

type Handler struct {
	DB    *db.PrismaClient
	Redis *redis.Client
}

type WebhookPayload struct {
	Data struct {
		RefID   string `json:"ref_id"`
		Status  string `json:"status"`
		SN      string `json:"sn"`
		Message string `json:"message"`
		Price   int    `json:"price"`
	} `json:"data"`
}

// NewHandler inisialisasi handler khusus Digiflazz
func NewHandler(dbClient *db.PrismaClient, redisClient *redis.Client) *Handler {
	return &Handler{
		DB:    dbClient,
		Redis: redisClient,
	}
}

// SyncProducts adalah endpoint manual untuk menarik data dari API Digiflazz ke Database
func (h *Handler) SyncProducts(c echo.Context) error {
	ctx := c.Request().Context()

	// 1. Cari data Supplier Digiflazz di Database berdasarkan 'code'
	supplier, err := h.DB.Supplier.FindFirst(
		db.Supplier.Code.Equals("DIGIFLAZZ_OFFICIAL"), // Disesuaikan dengan gambar database
	).Exec(ctx)

	if err != nil {
		return c.JSON(http.StatusNotFound, echo.Map{
			"error": "Supplier dengan kode DIGIFLAZZ_OFFICIAL tidak ditemukan di database",
		})
	}

	// 2. Ambil Kredensial Langsung dari Database
	// Kolom username untuk Username Digiflazz, kolom password untuk API Key
	dfUsername, okUser := supplier.Username()
	dfAPIKey, okPass := supplier.Password()

	if !okUser || !okPass || dfUsername == "" || dfAPIKey == "" {
		return c.JSON(http.StatusInternalServerError, echo.Map{
			"error": "Kredensial Digiflazz (Username / API Key) belum lengkap di tabel Supplier",
		})
	}

	// 3. Inisialisasi Service menggunakan kredensial dari DB
	dfService := digiservice.NewDigiflazzService(dfUsername, dfAPIKey)

	// 4. Eksekusi fungsi Sync yang ada di sync.go
	errSync := digiservice.SyncProducts(h.DB, dfService, supplier.ID, h.Redis)
	if errSync != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{
			"error":   "Gagal melakukan sinkronisasi",
			"details": errSync.Error(),
		})
	}

	return c.JSON(http.StatusOK, echo.Map{
		"message": "Sinkronisasi produk Digiflazz berhasil dijalankan! Silakan cek log terminal untuk detailnya.",
	})
}

// webhookHandler akan menerima notifikasi dari Digiflazz setiap kali ada perubahan status order
func (h *Handler) HandleWebhook(c echo.Context) error {
	ctx := c.Request().Context()

	// ==========================================
	// A. AMBIL SECRET DARI DATABASE
	// ==========================================
	supplier, errSupp := h.DB.Supplier.FindFirst(db.Supplier.Code.Equals("DIGIFLAZZ_OFFICIAL")).Exec(ctx)
	if errSupp != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Konfigurasi Supplier tidak ditemukan"})
	}
	secret, okSecret := supplier.WebhookInbound()

	// ==========================================
	// B. VALIDASI KEAMANAN (SIGNATURE HMAC)
	// ==========================================
	if okSecret && secret != "" {
		signatureHeader := c.Request().Header.Get("X-Hub-Signature")
		bodyBytes, _ := io.ReadAll(c.Request().Body)
		c.Request().Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		mac := hmac.New(sha1.New, []byte(secret))
		mac.Write(bodyBytes)
		if signatureHeader != "sha1="+hex.EncodeToString(mac.Sum(nil)) {
			log.Println("⚠️ Webhook mencurigakan! Signature tidak valid.")
			return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Signature Tidak Valid"})
		}
	}

	// ==========================================
	// C. PROSES DATA WEBHOOK
	// ==========================================
	payload := new(WebhookPayload)
	if err := c.Bind(payload); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Format payload tidak valid"})
	}

	data := payload.Data
	if data.RefID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "ref_id kosong"})
	}

	parts := strings.Split(data.RefID, "-")
	if len(parts) < 1 || len(parts[0]) < 10 {
		return c.JSON(http.StatusOK, echo.Map{"message": "RefID bukan milik GerbangAPI, diabaikan"})
	}
	
	internalOrderID := parts[0]
	if len(parts) >= 5 {
		internalOrderID = strings.Join(parts[0:5], "-")
	}

	var finalStatus string
	if data.Status == "Sukses" {
		finalStatus = "success"
	} else if data.Status == "Gagal" {
		finalStatus = "failed"
	} else {
		return c.JSON(http.StatusOK, echo.Map{"message": "Status masih pending"})
	}

	log.Printf("📥 Webhook Inbound: Order %s status menjadi %s", internalOrderID, finalStatus)

	// Update Status di Database
	h.DB.Prisma.ExecuteRaw(`UPDATE supplier_order SET status=?, provider_trx_id=?, last_error=?, updated_at=NOW() WHERE internal_order_id=?`, finalStatus, data.SN, data.Message, internalOrderID).Exec(ctx)
	h.DB.Prisma.ExecuteRaw(`UPDATE internal_order SET status=?, updated_at=NOW() WHERE id=?`, finalStatus, internalOrderID).Exec(ctx)

	// =================================================================
	// D. REFUND & NOTIFIKASI KE SELLER
	// =================================================================
	internalOrder, errQuery := h.DB.InternalOrder.FindUnique(
		db.InternalOrder.ID.Equals(internalOrderID),
	).With(
		db.InternalOrder.User.Fetch(),
		db.InternalOrder.Product.Fetch(),
	).Exec(ctx)

	if errQuery == nil {
		user, okUser := internalOrder.User()
		product := internalOrder.Product()

		if okUser && user != nil {
			productName := "Produk Tidak Diketahui"
			productPrice := float64(0)
			
			if product != nil {
				productName = product.Name
				productPrice = float64(product.Price)
			}
			
			tujuan := internalOrder.BuyerUID
			tanggal := time.Now().Format("02 Jan 2006 15:04")
			statusEmoji, statusText, statusCode := "✅", "BERHASIL", 1

			// -------------------------------------------------------------
			// [BARU] LOGIKA REFUND JIKA TRANSAKSI GAGAL
			// -------------------------------------------------------------
			if finalStatus == "failed" {
				statusEmoji, statusText, statusCode = "❌", "GAGAL", 2

				// 1. Tambahkan saldo kembali ke User
				balanceBefore := user.Balance
				balanceAfter := balanceBefore + productPrice

				_, errRefund := h.DB.Prisma.ExecuteRaw(
					`UPDATE user SET balance = balance + ?, updated_at=NOW() WHERE id = ?`, 
					productPrice, user.ID,
				).Exec(ctx)

				if errRefund == nil {
					// 2. Catat di Buku Kas (Mutasi Masuk / CREDIT)
					desc := fmt.Sprintf("Refund Transaksi Gagal - %s ke %s", productName, tujuan)
					h.DB.WalletMutation.CreateOne(
						db.WalletMutation.Type.Set("CREDIT"),
						db.WalletMutation.Amount.Set(productPrice),
						db.WalletMutation.BalanceBefore.Set(balanceBefore),
						db.WalletMutation.BalanceAfter.Set(balanceAfter),
						db.WalletMutation.Description.Set(desc),
						db.WalletMutation.User.Link(db.User.ID.Equals(user.ID)),
						db.WalletMutation.ReferenceID.Set(internalOrder.ID),
					).Exec(ctx)

					log.Printf("♻️ Auto-Refund Berhasil: Rp %v dikembalikan ke User %s", productPrice, user.ID)
				}
			}
			// -------------------------------------------------------------

			keterangan := data.SN
			if finalStatus == "failed" { keterangan = data.Message }
			if keterangan == "" { keterangan = "-" }

			// KIRIM TELEGRAM
			if chatID, okID := user.TelegramChatID(); okID && chatID != "" {
				msg := fmt.Sprintf(`
<b>%s TRANSAKSI %s</b>
▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬
<b>Detail Produk:</b>
🔹 %s

<b>Informasi Pengiriman:</b>
📍 <b>Tujuan:</b> <code>%s</code>
<b>SN/Keterangan:</b> <code>%s</code>
🏢 <b>Supplier:</b> Digiflazz

<b>Tanggal:</b> %s
▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬
<i>Ref ID: %s</i>`, statusEmoji, statusText, productName, tujuan, keterangan, tanggal, internalOrder.ID)

				go utils.SendTelegramNotification(chatID, msg)
			}

			// KIRIM WEBHOOK CALLBACK KE SELLER
			if webhookURL, okURL := user.WebhookURL(); okURL && webhookURL != "" {
				webhookPayload := map[string]interface{}{
					"seller_id":    user.ID,
					"message_type": "transaction_update",
					"timestamp":    tanggal,
					"data": map[string]interface{}{
						"trx_id":       internalOrder.ID,
						"ref_id":       internalOrder.ID,
						"product_name": productName,
						"code":         internalOrder.ProductID,
						"price":        productPrice,
						"status":       finalStatus,
						"status_code":  statusCode,
						"sn":           data.SN,
						"destination":  tujuan,
						"message":      fmt.Sprintf("Transaksi %s", statusText),
					},
				}
				go utils.SendWebhookCallback(webhookURL, webhookPayload)
			}
		}
	}

	return c.JSON(http.StatusOK, echo.Map{"message": "Webhook berhasil diproses"})
}