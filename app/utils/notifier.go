package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// SendTelegramNotification mengirimkan pesan ke Telegram user/admin
func SendTelegramNotification(targetChatID string, messageHTML string) {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" || targetChatID == "" {
		return
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	payload := map[string]interface{}{
		"chat_id":                  targetChatID,
		"text":                     messageHTML,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}

	jsonVal, _ := json.Marshal(payload)

	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonVal))
	if err != nil {
		log.Printf("⚠️ Gagal kirim Telegram: %v", err)
		return
	}
	defer resp.Body.Close()
}

// SendWebhookCallback mengirimkan payload notifikasi ke URL Webhook milik Seller
func SendWebhookCallback(targetURL string, payload interface{}) {
	jsonVal, _ := json.Marshal(payload)

	// Mekanisme Retry: Coba kirim hingga 3 kali jika server Seller sedang down
	for i := 0; i < 3; i++ {
		client := http.Client{Timeout: 10 * time.Second}
		resp, err := client.Post(targetURL, "application/json", bytes.NewBuffer(jsonVal))
		
		if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if resp.Body != nil {
				resp.Body.Close()
			}
			log.Printf("✅ Webhook outbound sent successfully to %s", targetURL)
			return
		}
		
		// Beri jeda 2 detik sebelum mencoba lagi
		time.Sleep(2 * time.Second)
	}
	log.Printf("❌ Webhook outbound gave up after 3 attempts targeting %s", targetURL)
}