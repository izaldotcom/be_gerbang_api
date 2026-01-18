package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"gerbangapi/prisma/db"

	"github.com/labstack/echo/v4"
)

type TelegramHandler struct {
	DB *db.PrismaClient
}

func NewTelegramHandler(dbClient *db.PrismaClient) *TelegramHandler {
	return &TelegramHandler{DB: dbClient}
}

// Struct untuk memparsing JSON dari Telegram
type TelegramUpdate struct {
	UpdateID int `json:"update_id"`
	Message  struct {
		MessageID int    `json:"message_id"`
		From      struct {
			ID        int64  `json:"id"`
			FirstName string `json:"first_name"`
			Username  string `json:"username"`
		} `json:"from"`
		Chat struct {
			ID int64 `json:"id"` // <--- INI YANG KITA CARI
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

// Method untuk menerima Webhook
func (h *TelegramHandler) HandleWebhook(c echo.Context) error {
	var update TelegramUpdate
	
	// 1. Bind JSON dari Telegram
	if err := c.Bind(&update); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid payload"})
	}

	messageText := update.Message.Text
	chatID := update.Message.Chat.ID
	
	// Log untuk debug
	log.Printf("📩 Telegram msg received: %s | ChatID: %d", messageText, chatID)

	// 2. Cek apakah ini command /start dengan Payload (Deep Linking)
	// Format pesan akan terlihat seperti: "/start c545e249-3c5e-4172-..."
	if strings.HasPrefix(messageText, "/start ") {
		
		// Ambil UUID (pisahkan "/start " dengan ID)
		parts := strings.Split(messageText, " ")
		if len(parts) < 2 {
			return c.JSON(http.StatusOK, "No token provided")
		}
		
		userUUID := parts[1] // Ini UUID User dari Database

		// 3. Update Database: Simpan Chat ID ke User tersebut
		// Convert int64 ChatID ke string
		chatIDStr := fmt.Sprintf("%d", chatID)

		_, err := h.DB.User.FindUnique(
			db.User.ID.Equals(userUUID),
		).Update(
			db.User.TelegramChatID.Set(chatIDStr),
		).Exec(c.Request().Context())

		if err != nil {
			log.Printf("❌ Gagal update user binding: %v", err)
			h.sendReply(chatID, "❌ Gagal menghubungkan akun. Pastikan ID valid atau hubungi admin.")
			return c.JSON(http.StatusOK, "Failed")
		}

		// 4. Sukses! Balas ke User
		successMsg := fmt.Sprintf("✅ <b>BERHASIL!</b>\n\nHalo %s, akun Anda telah terhubung.\nNotifikasi transaksi akan dikirim ke sini.", update.Message.From.FirstName)
		h.sendReply(chatID, successMsg)
	}

	// Selalu return 200 OK agar Telegram tidak mengirim ulang pesan
	return c.JSON(http.StatusOK, "OK")
}

// Helper simpel untuk membalas pesan
func (h *TelegramHandler) sendReply(chatID int64, text string) {
	// Panggil API sendMessage (Sederhana, pakai http.Post biasa atau copas logic dari worker)
	// Implementasi detailnya mirip function sendTelegramNotification di worker
    // ... (Code request ke https://api.telegram.org/bot<TOKEN>/sendMessage) ...
    // Agar singkat, saya skip implementasi detail HTTP client disini, 
    // tapi logic-nya sama persis dengan yang ada di worker.
}