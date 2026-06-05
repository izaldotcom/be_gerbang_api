package handlers

import (
	"fmt"
	"log"
	"net/http"

	"gerbangapi/prisma/db"

	"github.com/labstack/echo/v4"
)

type TopUpHandler struct {
	DB *db.PrismaClient
}

func NewTopUpHandler(dbClient *db.PrismaClient) *TopUpHandler {
	return &TopUpHandler{
		DB: dbClient,
	}
}

// ==========================================
// 1. REQUEST TOP-UP (OLEH SELLER)
// ==========================================
func (h *TopUpHandler) RequestTopUp(c echo.Context) error {
	ctx := c.Request().Context()

	// 1. Ekstraksi User ID yang Sangat Aman (Mendukung JWT & API Key)
	var userID string
	
	// Coba ambil dari konteks (biasanya diisi oleh Middleware JWT)
	if id, ok := c.Get("user_id").(string); ok && id != "" {
		userID = id
	}

	// Jika kosong, coba ambil paksa dari Header X-API-KEY
	if userID == "" {
		apiKey := c.Request().Header.Get("X-API-KEY")
		if apiKey != "" {
			keyData, _ := h.DB.APIKey.FindUnique(db.APIKey.APIKey.Equals(apiKey)).Exec(ctx)
			if keyData != nil {
				userID = keyData.UserID
			}
		}
	}

	// Jika identitas benar-benar tidak ditemukan, tolak dengan lembut
	if userID == "" {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Sesi tidak valid atau API Key salah. Silakan relogin."})
	}

	// 2. Tangkap Payload Input
	type Req struct {
		Amount        float64 `json:"amount"`
		PaymentMethod string  `json:"payment_method"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Format JSON tidak valid"})
	}

	if req.Amount < 10000 {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Minimal top-up adalah Rp 10.000"})
	}

	// 3. Simpan ke Tabel TopUp (Prisma Go)
	topup, err := h.DB.TopUp.CreateOne(
		db.TopUp.Amount.Set(req.Amount),
		db.TopUp.User.Link(db.User.ID.Equals(userID)),
		db.TopUp.PaymentMethod.Set(req.PaymentMethod),
	).Exec(ctx)

	// 4. Tangani Error Database (Mencegah Internal Server Error)
	if err != nil {
		log.Printf("❌ DATABASE ERROR SAAT TOPUP: %v\n", err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Terjadi masalah pada database: " + err.Error()})
	}

	paymentMethodVal, _ := topup.PaymentMethod()

	return c.JSON(http.StatusOK, echo.Map{
		"message": "Request Top-Up berhasil dibuat",
		"data": echo.Map{
			"topup_id":       topup.ID,
			"amount":         topup.Amount,
			"payment_method": paymentMethodVal, // <--- Gunakan variabel yang sudah diekstrak
			"status":         topup.Status,
		},
	})
}

// ==========================================
// 2. APPROVE TOP-UP (OLEH ADMIN)
// ==========================================
func (h *TopUpHandler) ApproveTopUp(c echo.Context) error {
	// Pastikan hanya Admin yang bisa mengakses ini (Bisa lewat Middleware Role)
	
	type Req struct {
		TopUpID string `json:"topup_id"`
		Action  string `json:"action"` // "approve" atau "reject"
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Format input tidak valid"})
	}

	ctx := c.Request().Context()

	// 1. Cari data TopUp berdasarkan ID
	topup, err := h.DB.TopUp.FindUnique(
		db.TopUp.ID.Equals(req.TopUpID),
	).With(
		db.TopUp.User.Fetch(),
	).Exec(ctx)

	if err != nil {
		return c.JSON(http.StatusNotFound, echo.Map{"error": "Data Top-Up tidak ditemukan"})
	}

	// Jika sudah diproses sebelumnya, tolak eksekusi ganda
	if topup.Status != "pending" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Top-Up ini sudah diproses (" + topup.Status + ")"})
	}

	// 2. Jika Admin memilih REJECT
	if req.Action == "reject" {
		_, errUpdate := h.DB.TopUp.FindUnique(db.TopUp.ID.Equals(req.TopUpID)).Update(
			db.TopUp.Status.Set("failed"),
		).Exec(ctx)
		
		if errUpdate != nil {
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Gagal menolak Top-Up"})
		}
		return c.JSON(http.StatusOK, echo.Map{"message": "Top-Up berhasil ditolak/dibatalkan"})
	}

	// 3. Jika Admin memilih APPROVE
	if req.Action == "approve" {
		user := topup.User()
		balanceBefore := user.Balance
		balanceAfter := balanceBefore + topup.Amount

		// Gunakan Raw Query untuk mencegah Race Condition saat menambah saldo
		_, errUpdateBalance := h.DB.Prisma.ExecuteRaw(
			`UPDATE user SET balance = balance + ?, updated_at = NOW() WHERE id = ?`, 
			topup.Amount, user.ID,
		).Exec(ctx)

		if errUpdateBalance != nil {
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Gagal menambah saldo user"})
		}

		// Update status TopUp menjadi success
		h.DB.TopUp.FindUnique(db.TopUp.ID.Equals(req.TopUpID)).Update(
			db.TopUp.Status.Set("success"),
		).Exec(ctx)

		paymentMethod, _ := topup.PaymentMethod()
		desc := fmt.Sprintf("Top-Up Manual via %s", paymentMethod)
		
		h.DB.WalletMutation.CreateOne(
			db.WalletMutation.Type.Set("CREDIT"),                       // 1. Type
			db.WalletMutation.Amount.Set(topup.Amount),                 // 2. Amount
			db.WalletMutation.BalanceBefore.Set(balanceBefore),         // 3. BalanceBefore
			db.WalletMutation.BalanceAfter.Set(balanceAfter),           // 4. BalanceAfter
			db.WalletMutation.Description.Set(desc),                    // 5. Description
			db.WalletMutation.User.Link(db.User.ID.Equals(user.ID)),    // 6. User (Relasi)
			
			// Kolom opsional selalu bebas diletakkan paling akhir:
			db.WalletMutation.ReferenceID.Set(topup.ID),
		).Exec(ctx)

		log.Printf("💰 Saldo ditambahkan: %v ke User ID: %s", topup.Amount, user.ID)

		return c.JSON(http.StatusOK, echo.Map{
			"message": "Top-Up berhasil disetujui, saldo User telah bertambah.",
			"data": echo.Map{
				"user_id":       user.ID,
				"topup_amount":  topup.Amount,
				"final_balance": balanceAfter,
			},
		})
	}

	return c.JSON(http.StatusBadRequest, echo.Map{"error": "Action tidak dikenali. Gunakan 'approve' atau 'reject'"})
}

// ==========================================
// 3. GET ALL TOP-UPS (UNTUK ADMIN)
// ==========================================
func (h *TopUpHandler) GetAllTopUps(c echo.Context) error {
	ctx := c.Request().Context()

	// Ambil semua topup, urutkan dari yang terbaru, dan bawa data User-nya
	topups, err := h.DB.TopUp.FindMany().With(
		db.TopUp.User.Fetch(),
	).OrderBy(
		db.TopUp.CreatedAt.Order(db.SortOrderDesc),
	).Exec(ctx)

	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Gagal mengambil data Top-Up: " + err.Error()})
	}

	var response []map[string]interface{}
	for _, t := range topups {
		userName := "Unknown"
		
		user := t.User() 
		if user != nil {
			userName = user.Name
		}
		
		pm, _ := t.PaymentMethod()

		response = append(response, map[string]interface{}{
			"id":             t.ID,
			"amount":         t.Amount,
			"payment_method": pm,
			"status":         t.Status,
			"created_at":     t.CreatedAt,
			"user_name":      userName,
		})
	}

	return c.JSON(http.StatusOK, echo.Map{
		"message": "Berhasil mengambil data",
		"data":    response,
	})
}