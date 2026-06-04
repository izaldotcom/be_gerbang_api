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
	"os"
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
	// A. VALIDASI KEAMANAN (SIGNATURE SECRET)
	// ==========================================
	secret := os.Getenv("DIGIFLAZZ_WEBHOOK_SECRET")
	if secret != "" {
		signatureHeader := c.Request().Header.Get("X-Hub-Signature")

		// Baca body mentah untuk dienkripsi
		bodyBytes, errRead := io.ReadAll(c.Request().Body)
		if errRead != nil {
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Gagal membaca body"})
		}

		// Kembalikan body ke dalam request agar fungsi c.Bind() nanti tetap bisa membacanya
		c.Request().Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		// Buat enkripsi HMAC SHA1 dari body request menggunakan Secret kita
		mac := hmac.New(sha1.New, []byte(secret))
		mac.Write(bodyBytes)
		expectedMAC := hex.EncodeToString(mac.Sum(nil))
		expectedSignature := "sha1=" + expectedMAC

		// Cocokkan signature dari Digiflazz dengan hasil hitungan kita
		if signatureHeader != expectedSignature {
			log.Println("⚠️ [WARNING] Ada request Webhook mencurigakan! Signature tidak cocok.")
			return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Akses Ditolak: Signature Tidak Valid"})
		}
	}

	// ==========================================
	// B. PROSES DATA WEBHOOK
	// ==========================================
	payload := new(WebhookPayload)

	if err := c.Bind(payload); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Format payload tidak valid"})
	}

	data := payload.Data
	if data.RefID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "ref_id kosong"})
	}

	// 1. Ekstrak Internal Order ID dari RefID
	// Ingat: Di worker, ref_id formatnya = internalOrderID-itemIndex-qtyIndex (Contoh: uuid-1-1)
	// Kita ambil UUID murninya dengan mengambil elemen pertama sebelum tanda strip pertama.
	parts := strings.Split(data.RefID, "-")
	
	// Jika RefID kurang dari 5 karakter, berarti UUID tidak valid / bukan dari sistem kita
	if len(parts) < 1 || len(parts[0]) < 10 {
		return c.JSON(http.StatusOK, echo.Map{"message": "RefID bukan milik GerbangAPI, diabaikan"})
	}
	
	// Kita harus menggabungkan kembali bagian-bagian UUID yang terpisah oleh strip
	// UUID standar memiliki 4 tanda strip (contoh: 550e8400-e29b-41d4-a716-446655440000)
	// Jadi kita ambil 5 bagian pertama dan gabungkan kembali
	var internalOrderID string
	if len(parts) >= 5 {
		internalOrderID = strings.Join(parts[0:5], "-")
	} else {
		// Fallback jika formatnya tidak standar
		internalOrderID = parts[0]
	}

	// 2. Tentukan Status Akhir untuk Database
	var finalStatus string
	if data.Status == "Sukses" {
		finalStatus = "success"
	} else if data.Status == "Gagal" {
		finalStatus = "failed"
	} else {
		// Jika status masih "Pending", abaikan saja dan biarkan sistem menunggu
		return c.JSON(http.StatusOK, echo.Map{"message": "Status masih pending, diabaikan"})
	}

	log.Printf("📥 Webhook Inbound Digiflazz: Order %s status menjadi %s", internalOrderID, finalStatus)

	// 3. Update Database (Tabel supplier_order)
	// Simpan SN dan Message dari Digiflazz ke kolom provider_trx_id dan last_error
	_, err := h.DB.Prisma.ExecuteRaw(
		`UPDATE supplier_order SET status=?, provider_trx_id=?, last_error=?, updated_at=NOW() WHERE internal_order_id=?`,
		finalStatus, data.SN, data.Message, internalOrderID,
	).Exec(ctx)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Gagal update tabel supplier_order"})
	}

	// 4. Update Database (Tabel internal_order utama)
	_, err = h.DB.Prisma.ExecuteRaw(
		`UPDATE internal_order SET status=?, updated_at=NOW() WHERE id=?`,
		finalStatus, internalOrderID,
	).Exec(ctx)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Gagal update tabel internal_order"})
	}

	// =================================================================
	// C. NOTIFIKASI KE SELLER (OUTBOUND WEBHOOK & TELEGRAM)
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

			// Tentukan pesan status
			statusEmoji := "✅"
			statusText := "BERHASIL"
			statusCode := 1

			if finalStatus == "failed" {
				statusEmoji = "❌"
				statusText = "GAGAL"
				statusCode = 2
			}

			// Format SN/Pesan agar lebih informatif
			keterangan := data.SN
			if finalStatus == "failed" {
				keterangan = data.Message
			}
			if keterangan == "" {
				keterangan = "-"
			}

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

			// 2. KIRIM KE WEBHOOK SELLER (CALLBACK)
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
	} else {
		log.Printf("⚠️ Gagal mengambil data User/Product untuk notifikasi: %v", errQuery)
	}

	// Respons Wajib: Kembalikan status HTTP 200 agar Digiflazz tahu Webhook sudah diterima
	return c.JSON(http.StatusOK, echo.Map{"message": "Webhook berhasil diproses"})
}