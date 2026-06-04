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
	// A. AMBIL SECRET DARI DATABASE (TABEL SUPPLIER)
	// ==========================================
	supplier, errSupp := h.DB.Supplier.FindFirst(
		db.Supplier.Code.Equals("DIGIFLAZZ_OFFICIAL"),
	).Exec(ctx)

	if errSupp != nil {
		log.Println("⚠️ [WARNING] Supplier DIGIFLAZZ_OFFICIAL tidak ditemukan di database")
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Konfigurasi Supplier tidak ditemukan"})
	}

	// Ambil kolom webhook_inbound yang baru saja ditambahkan di schema.prisma
	secret, okSecret := supplier.WebhookInbound()

	// ==========================================
	// B. VALIDASI KEAMANAN (SIGNATURE HMAC)
	// ==========================================
	if okSecret && secret != "" {
		signatureHeader := c.Request().Header.Get("X-Hub-Signature")

		// Baca body mentah untuk dienkripsi
		bodyBytes, errRead := io.ReadAll(c.Request().Body)
		if errRead != nil {
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Gagal membaca body"})
		}

		// Kembalikan body ke dalam request agar fungsi c.Bind() nanti tetap bisa membacanya
		c.Request().Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		// Buat enkripsi HMAC SHA1 dari body request menggunakan Secret dari Database
		mac := hmac.New(sha1.New, []byte(secret))
		mac.Write(bodyBytes)
		expectedMAC := hex.EncodeToString(mac.Sum(nil))
		expectedSignature := "sha1=" + expectedMAC

		// Cocokkan signature dari Digiflazz dengan hasil hitungan kita
		if signatureHeader != expectedSignature {
			log.Println("⚠️ [WARNING] Ada request Webhook mencurigakan! Signature tidak cocok dengan Secret di Database.")
			return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Akses Ditolak: Signature Tidak Valid"})
		}
	} else {
		log.Println("⚠️ [WARNING] Webhook Inbound Secret untuk Digiflazz KOSONG di database! Keamanan dinonaktifkan sementara.")
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

	// Ekstrak Internal Order ID dari RefID dan perbaiki UUID
	parts := strings.Split(data.RefID, "-")
	if len(parts) < 1 || len(parts[0]) < 10 {
		return c.JSON(http.StatusOK, echo.Map{"message": "RefID bukan milik GerbangAPI, diabaikan"})
	}
	
	var internalOrderID string
	if len(parts) >= 5 {
		internalOrderID = strings.Join(parts[0:5], "-")
	} else {
		internalOrderID = parts[0]
	}

	// Tentukan Status Akhir
	var finalStatus string
	if data.Status == "Sukses" {
		finalStatus = "success"
	} else if data.Status == "Gagal" {
		finalStatus = "failed"
	} else {
		return c.JSON(http.StatusOK, echo.Map{"message": "Status masih pending, diabaikan"})
	}

	log.Printf("📥 Webhook Inbound Digiflazz: Order %s status menjadi %s", internalOrderID, finalStatus)

	// Update Database (Tabel supplier_order & internal_order)
	h.DB.Prisma.ExecuteRaw(`UPDATE supplier_order SET status=?, provider_trx_id=?, last_error=?, updated_at=NOW() WHERE internal_order_id=?`, finalStatus, data.SN, data.Message, internalOrderID).Exec(ctx)
	h.DB.Prisma.ExecuteRaw(`UPDATE internal_order SET status=?, updated_at=NOW() WHERE id=?`, finalStatus, internalOrderID).Exec(ctx)

	// =================================================================
	// D. NOTIFIKASI KE SELLER (TETAP PAKAI URL DARI TABEL USER)
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
			productPrice := 0
			if product != nil {
				productName = product.Name
				productPrice = product.Price
			}
			
			tujuan := internalOrder.BuyerUID
			tanggal := time.Now().Format("02 Jan 2006 15:04")
			statusEmoji, statusText, statusCode := "✅", "BERHASIL", 1

			if finalStatus == "failed" {
				statusEmoji, statusText, statusCode = "❌", "GAGAL", 2
			}

			keterangan := data.SN
			if finalStatus == "failed" { keterangan = data.Message }
			if keterangan == "" { keterangan = "-" }

			// 1. KIRIM KE TELEGRAM PERSONAL SELLER
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

			// 2. KIRIM KE WEBHOOK SELLER (CALLBACK KE TABEL USER)
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