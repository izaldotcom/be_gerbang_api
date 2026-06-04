package digiflazz

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DigiflazzBaseURL adalah endpoint utama API Digiflazz
const DigiflazzBaseURL = "https://api.digiflazz.com/v1"

type DigiflazzService struct {
	Username string
	APIKey   string
}

// NewDigiflazzService menginisialisasi service dengan kredensial dari .env atau Database
func NewDigiflazzService(username, apiKey string) *DigiflazzService {
	return &DigiflazzService{
		Username: username,
		APIKey:   apiKey,
	}
}

// generateSign membuat hash MD5 sesuai aturan Digiflazz
func (s *DigiflazzService) generateSign(suffix string) string {
	data := s.Username + s.APIKey + suffix
	hash := md5.Sum([]byte(data))
	return fmt.Sprintf("%x", hash)
}

// ==========================================
// STRUKTUR PAYLOAD REQUEST
// ==========================================

type PayloadDeposit struct {
	Cmd      string `json:"cmd"`
	Username string `json:"username"`
	Sign     string `json:"sign"`
}

type PayloadPriceList struct {
	Cmd      string `json:"cmd"`
	Username string `json:"username"`
	Sign     string `json:"sign"`
}

type PayloadTransaction struct {
	Username     string `json:"username"`
	BuyerSKUCode string `json:"buyer_sku_code"`
	CustomerNo   string `json:"customer_no"`
	RefID        string `json:"ref_id"`
	Sign         string `json:"sign"`
	Msg          string `json:"msg,omitempty"`
}

// ==========================================
// FUNGSI UTAMA (CORE FEATURES)
// ==========================================

// 1. CheckBalance: Untuk mengecek sisa saldo di akun Digiflazz Anda
func (s *DigiflazzService) CheckBalance() (float64, error) {
	payload := PayloadDeposit{
		Cmd:      "deposit",
		Username: s.Username,
		Sign:     s.generateSign("depo"), // Suffix untuk saldo selalu "depo"
	}

	respBody, err := s.sendRequest("/cek-saldo", payload)
	if err != nil {
		return 0, err
	}

	var result struct {
		Data struct {
			Deposit float64 `json:"deposit"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, fmt.Errorf("gagal parsing response cek saldo: %v", err)
	}

	return result.Data.Deposit, nil
}

// 2. GetPriceList: Untuk sinkronisasi produk dan harga ke database lokal Anda
// Parameter cmdType: "prepaid" (Pulsa/Game) atau "pasca" (Tagihan)
func (s *DigiflazzService) GetPriceList(cmdType string) ([]map[string]interface{}, error) {
	if cmdType == "" {
		cmdType = "prepaid"
	}

	payload := PayloadPriceList{
		Cmd:      cmdType,
		Username: s.Username,
		Sign:     s.generateSign("depo"), // Pricelist juga menggunakan suffix "depo"
	}

	respBody, err := s.sendRequest("/price-list", payload)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data []map[string]interface{} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("gagal parsing response pricelist: %v", err)
	}

	return result.Data, nil
}

// 3. TopUp: Eksekusi transaksi (Dipanggil oleh Worker/Engine saat ada pesanan masuk)
func (s *DigiflazzService) TopUp(skuCode, customerNo, refID string) (map[string]interface{}, error) {
	payload := PayloadTransaction{
		Username:     s.Username,
		BuyerSKUCode: skuCode,
		CustomerNo:   customerNo,
		RefID:        refID,
		Sign:         s.generateSign(refID), // WAJIB menggunakan ref_id sebagai suffix transaksi
	}

	respBody, err := s.sendRequest("/transaction", payload)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data map[string]interface{} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("gagal parsing response transaksi: %v", err)
	}

	// Deteksi dini jika status langsung "Gagal" dari Digiflazz (misal: Saldo tidak cukup, Nomor salah)
	if status, ok := result.Data["status"].(string); ok && status == "Gagal" {
		message := "Transaksi Gagal di Provider"
		if msg, ok := result.Data["message"].(string); ok {
			message = msg
		}
		return result.Data, fmt.Errorf(message)
	}

	// Mengembalikan map berisi status "Pending" atau "Sukses" beserta SN-nya
	return result.Data, nil
}

// ==========================================
// HTTP CLIENT HELPER (PRIVATE)
// ==========================================
func (s *DigiflazzService) sendRequest(endpoint string, payload interface{}) ([]byte, error) {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("gagal encode payload JSON: %v", err)
	}

	url := DigiflazzBaseURL + endpoint
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("gagal membuat HTTP request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Timeout 30 detik agar server tidak hang jika Digiflazz sedang down
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("koneksi ke server Digiflazz terputus: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca body response: %v", err)
	}

	return body, nil
}