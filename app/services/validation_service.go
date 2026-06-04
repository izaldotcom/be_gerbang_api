package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

type ValidationService struct {
	Redis *redis.Client
}

func NewValidationService(redisClient *redis.Client) *ValidationService {
	return &ValidationService{Redis: redisClient}
}

// CekNickname mengecek nama target berdasarkan Game dan User ID
func (s *ValidationService) CekNickname(kodeGame, targetID string) (string, error) {
	ctx := context.Background()

	// Format Key Redis (Contoh: "nickname:ml:12345678_1234")
	redisKey := fmt.Sprintf("nickname:%s:%s", kodeGame, targetID)

	// 1. CEK CACHE DI REDIS DULU (Sangat Cepat)
	cachedName, err := s.Redis.Get(ctx, redisKey).Result()
	if err == nil && cachedName != "" {
		log.Printf("⚡ [CACHE HIT] Nickname %s ditemukan di Redis!", targetID)
		return cachedName, nil
	}

	// 2. JIKA TIDAK ADA DI REDIS, TEMBAK API PIHAK KETIGA
	log.Printf("🌐 [CACHE MISS] Mengecek Nickname %s ke API Pihak ke-3...", targetID)
	nickname, errApi := s.hitThirdPartyAPI(kodeGame, targetID)
	if errApi != nil {
		return "", errApi
	}

	// 3. SIMPAN HASILNYA KE REDIS (TTL: 7 Hari)
	// 7 * 24 jam. Jika dalam 7 hari user topup lagi, tidak perlu nembak API lagi
	errSet := s.Redis.Set(ctx, redisKey, nickname, 168*time.Hour).Err()
	if errSet != nil {
		log.Printf("⚠️ Gagal menyimpan nickname ke Redis: %v", errSet)
	}

	return nickname, nil
}

// hitThirdPartyAPI adalah fungsi untuk menembak agregator Cek ID (Misal: API Games / VIP)
func (s *ValidationService) hitThirdPartyAPI(kodeGame, targetID string) (string, error) {
	// =======================================================
	// KONTROL MODE DARI .ENV
	// =======================================================
	isMockMode := os.Getenv("MOCK_API")

	if isMockMode == "true" {
		// [MODE TESTING] Langsung kembalikan sukses tanpa nembak API
		log.Printf("🛠️ [MOCK API MODE] Berpura-pura berhasil mengecek ID %s untuk game %s", targetID, kodeGame)
		return "PELANGGAN_TESTING", nil
	}

	// =======================================================
	// [MODE PRODUCTION] Logika Asli Menembak API Pihak Ketiga
	// =======================================================
	
	// TODO: Nanti ganti domain "api-sungguhan.com" ini dengan provider API andalan Anda
	apiURL := fmt.Sprintf("https://api.api-sungguhan.com/v1/cek-id?game=%s&id=%s", kodeGame, targetID)

	resp, err := http.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("gagal menghubungi server cek ID")
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Asumsi format response JSON: {"status": true, "nickname": "Nama Player"}
	var result struct {
		Status   bool   `json:"status"`
		Nickname string `json:"nickname"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("format response tidak valid")
	}

	if !result.Status || result.Nickname == "" {
		return "", fmt.Errorf("ID Game tidak ditemukan")
	}

	return result.Nickname, nil
}