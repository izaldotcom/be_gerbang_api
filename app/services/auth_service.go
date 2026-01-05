package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"gerbangapi/app/utils"
	"gerbangapi/prisma/db"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type AuthService struct {
	DB    *db.PrismaClient
	Redis *redis.Client
}

func NewAuthService(dbClient *db.PrismaClient, redisClient *redis.Client) *AuthService {
	return &AuthService{
		DB:    dbClient,
		Redis: redisClient,
	}
}

// --- STRUCTS ---

type TokenResponse struct {
	AccessToken  string      `json:"access_token"`
	RefreshToken string      `json:"refresh_token"`
	User         UserSession `json:"user"`
}

type UserSession struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	RoleID   string `json:"role_id"`
	RoleName string `json:"role_name"`
	Status   string `json:"status"`
	ApiKey   string `json:"api_key"`
}

type JwtClaims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

type RegisterInput struct {
	ID       string
	RoleID   string
	Name     string
	Email    string
	Phone    string
	Status   string
	Password string
}

type LoginInput struct {
	Identifier string
	Password   string
}

// --- METHODS ---

// 1. REGISTER
func (s *AuthService) Register(ctx context.Context, input RegisterInput) error {
	if input.ID == "" {
		input.ID = uuid.New().String()
	}
	hashed, _ := utils.HashPassword(input.Password)

	var ops []db.UserSetParam

	ops = append(ops, db.User.ID.Set(input.ID))
	if input.Phone != "" {
		ops = append(ops, db.User.Phone.Set(input.Phone))
	}
	if input.RoleID != "" {
		ops = append(ops, db.User.RoleID.Set(input.RoleID))
	}

	// Tentukan Status Awal
	initialStatus := "register" // Default
	if input.Status != "" {
		initialStatus = input.Status
	}
	ops = append(ops, db.User.Status.Set(initialStatus))

	// Create User
	userCreated, err := s.DB.User.CreateOne(
		db.User.Name.Set(input.Name),
		db.User.Email.Set(input.Email),
		db.User.Password.Set(hashed),
		ops...,
	).Exec(ctx)

	if err != nil {
		return err
	}

	// AUTO-GENERATE API KEY
	if input.RoleID != "" {
		roleData, err := s.DB.Role.FindUnique(
			db.Role.ID.Equals(input.RoleID),
		).Exec(ctx)

		if err == nil && roleData != nil && strings.EqualFold(roleData.Name, "Customer") {
			generatedKey := "MH-" + uuid.New().String()
			generatedSecret := uuid.New().String()

			// [LOGIC BARU]
			// Jika status user masih "register", API Key default-nya NON-AKTIF (False).
			// Jika status user "active", API Key langsung AKTIF (True).
			apiKeyActive := false
			if initialStatus == "active" {
				apiKeyActive = true
			}

			_, errKey := s.DB.APIKey.CreateOne(
				db.APIKey.APIKey.Set(generatedKey),
				db.APIKey.Secret.Set(generatedSecret),
				db.APIKey.User.Link(
					db.User.ID.Equals(userCreated.ID),
				),
				// Set status API Key sesuai status User
				db.APIKey.IsActive.Set(apiKeyActive), 
				db.APIKey.Status.Set(apiKeyActive),
				
				db.APIKey.SellerName.Set(input.Name),
			).Exec(ctx)

			if errKey != nil {
				return errKey
			}
		}
	}

	return nil
}

// 2. LOGIN
func (s *AuthService) Login(ctx context.Context, input LoginInput) (*TokenResponse, error) {
	// A. Cari User
	user, err := s.DB.User.FindFirst(
		db.User.Or(
			db.User.Email.Equals(input.Identifier),
			db.User.Phone.Equals(input.Identifier),
		),
	).With(
		db.User.Role.Fetch(),
		db.User.APIKeys.Fetch(),
	).Exec(ctx)

	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, errors.New("user not found")
		}
		return nil, err
	}

	// Cek Status User
	currentStatus, ok := user.Status()
	if !ok || currentStatus != "active" {
		if currentStatus == "register" {
			return nil, errors.New("akun anda sedang dalam proses verifikasi (status: register)")
		} else if currentStatus == "reject" {
			return nil, errors.New("akun anda telah ditolak (status: reject)")
		}
		return nil, errors.New("akun tidak aktif")
	}

	// B. Cek Password
	if !utils.CheckPassword(user.Password, input.Password) {
		return nil, errors.New("invalid password")
	}

	// C. Generate Token
	accessToken, err := s.generateAccessToken(user.ID, user.Email)
	if err != nil {
		return nil, err
	}

	refreshTokenStr := uuid.New().String()
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	_, err = s.DB.RefreshToken.CreateOne(
		db.RefreshToken.Token.Set(refreshTokenStr),
		db.RefreshToken.User.Link(
			db.User.ID.Equals(user.ID),
		),
		db.RefreshToken.ExpiresAt.Set(expiresAt),
	).Exec(ctx)
	if err != nil {
		return nil, err
	}

	// D. Data Session
	phoneVal := ""
	if v, ok := user.Phone(); ok { phoneVal = v }

	roleVal := ""
	if v, ok := user.RoleID(); ok { roleVal = v }

	statusVal := currentStatus

	roleNameVal := ""
	if roleData, ok := user.Role(); ok { roleNameVal = roleData.Name }

	userApiKey := ""
	if apiKeys := user.APIKeys(); len(apiKeys) > 0 {
		for _, key := range apiKeys {
			// Hanya ambil key yang Active
			if key.IsActive {
				userApiKey = key.APIKey
				break
			}
		}
	}

	userSession := UserSession{
		ID:       user.ID,
		Name:     user.Name,
		Email:    user.Email,
		Phone:    phoneVal,
		RoleID:   roleVal,
		RoleName: roleNameVal,
		Status:   statusVal,
		ApiKey:   userApiKey,
	}

	jsonData, _ := json.Marshal(userSession)

	// E. Simpan ke Redis
	redisKey := "user_session:" + user.ID
	err = s.Redis.Set(ctx, redisKey, jsonData, 24*time.Hour).Err()

	return &TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshTokenStr,
		User:         userSession,
	}, nil
}

// 3. REFRESH TOKEN
func (s *AuthService) RefreshTokenProcess(ctx context.Context, refreshTokenInput string) (string, error) {
	storedToken, err := s.DB.RefreshToken.FindUnique(
		db.RefreshToken.Token.Equals(refreshTokenInput),
	).With(
		db.RefreshToken.User.Fetch(),
	).Exec(ctx)

	if err != nil || storedToken == nil {
		return "", errors.New("invalid refresh token")
	}

	if time.Now().After(storedToken.ExpiresAt) {
		return "", errors.New("refresh token expired")
	}

	user := storedToken.User()
	
	if status, ok := user.Status(); !ok || status != "active" {
		return "", errors.New("user status is not active")
	}

	newAccessToken, err := s.generateAccessToken(user.ID, user.Email)

	return newAccessToken, err
}

// 4. GET SESSION FROM REDIS
func (s *AuthService) GetSession(ctx context.Context, userID string) (*UserSession, error) {
	redisKey := "user_session:" + userID

	val, err := s.Redis.Get(ctx, redisKey).Result()
	if err != nil {
		return nil, err
	}

	var session UserSession
	if err := json.Unmarshal([]byte(val), &session); err != nil {
		return nil, err
	}

	return &session, nil
}

// 5. VERIFY USER (APPROVE/REJECT + UPDATE API KEY)
func (s *AuthService) VerifyUser(ctx context.Context, userID, action string) (string, error) {
	var newStatus string

	// Mapping Action ke Status Database
	switch strings.ToLower(action) {
	case "approve":
		newStatus = "active"
	case "reject":
		newStatus = "reject"
	case "deactivate": // [BARU] Fitur Non-Aktifkan User
		newStatus = "register" // Kembalikan ke status awal (Register)
	default:
		return "", errors.New("invalid action. Use 'approve', 'reject', or 'deactivate'")
	}

	// 1. Update Status User di Database
	_, err := s.DB.User.FindUnique(
		db.User.ID.Equals(userID),
	).Update(
		db.User.Status.Set(newStatus),
	).Exec(ctx)

	if err != nil {
		return "", err
	}

	// 2. Update Status API Key
	if newStatus == "active" {
		// [AKTIFKAN] Jika Approved, nyalakan API Key
		s.DB.APIKey.FindMany(
			db.APIKey.UserID.Equals(userID),
		).Update(
			db.APIKey.IsActive.Set(true),
			db.APIKey.Status.Set(true),
		).Exec(ctx)

	} else {
		// [NON-AKTIFKAN] Jika Reject ATAU Deactivate (Register)
		// API Key dimatikan agar tidak bisa dipakai order/transaksi
		s.DB.APIKey.FindMany(
			db.APIKey.UserID.Equals(userID),
		).Update(
			db.APIKey.IsActive.Set(false),
			db.APIKey.Status.Set(false),
		).Exec(ctx)
	}

	// 3. Hapus Session Redis (Paksa User Logout / Refresh Token Gagal)
	s.Redis.Del(ctx, "user_session:"+userID)

	return newStatus, nil
}

// 6. GET ALL USERS
func (s *AuthService) GetAllUsers(ctx context.Context) ([]db.UserModel, error) {
    users, err := s.DB.User.FindMany().With(
        db.User.Role.Fetch(),
    ).Exec(ctx) // Hapus OrderBy sementara

    if err != nil {
        return nil, err
    }

    return users, nil
}

// Helper: Generate JWT
func (s *AuthService) generateAccessToken(userID, email string) (string, error) {
	claims := &JwtClaims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(os.Getenv("JWT_SECRET")))
}