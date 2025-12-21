package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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

	// Slice untuk field OPSIONAL saja (ID, Phone, Role, Status)
	var ops []db.UserSetParam

	// 1. ID (Optional karena di schema ada @default(uuid()), tapi kita set manual)
	ops = append(ops, db.User.ID.Set(input.ID))

	// 2. Phone (Optional)
	if input.Phone != "" {
		ops = append(ops, db.User.Phone.Set(input.Phone))
	}

	// 3. RoleID (Optional / Nullable)
	if input.RoleID != "" {
		ops = append(ops, db.User.RoleID.Set(input.RoleID))
	}

	// 4. Status (Optional / Nullable)
	if input.Status != "" {
		ops = append(ops, db.User.Status.Set(input.Status))
	} else {
		ops = append(ops, db.User.Status.Set("active")) // Default status
	}

	// EKSEKUSI CREATE
	// Masukkan Field Wajib (Name, Email, Password) sebagai argumen terpisah di awal
	_, err := s.DB.User.CreateOne(
		db.User.Name.Set(input.Name),      // Argumen 1: Name (Wajib)
		db.User.Email.Set(input.Email),    // Argumen 2: Email (Wajib)
		db.User.Password.Set(hashed),      // Argumen 3: Password (Wajib)
		ops...,                            // Sisanya (Optional) via slice
	).Exec(ctx)

	return err
}

// 2. LOGIN
func (s *AuthService) Login(ctx context.Context, input LoginInput) (*TokenResponse, error) {
	// A. Cari User (Email OR Phone) + Include Role
	user, err := s.DB.User.FindFirst(
		db.User.Or(
			db.User.Email.Equals(input.Identifier),
			db.User.Phone.Equals(input.Identifier),
		),
	).With(
		db.User.Role.Fetch(), // Join tabel Role
	).Exec(ctx)

	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, errors.New("user not found")
		}
		return nil, err
	}

	// B. Cek Password
	if !utils.CheckPassword(user.Password, input.Password) {
		return nil, errors.New("invalid password")
	}

	// C. Generate Access Token
	accessToken, err := s.generateAccessToken(user.ID, user.Email)
	if err != nil {
		return nil, err
	}

	// D. Generate Refresh Token
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

	// E. Persiapkan Data Session untuk Redis
	// Gunakan Accessor Method (v, ok) untuk field optional
	phoneVal := ""
	if v, ok := user.Phone(); ok { phoneVal = v }

	roleVal := ""
	if v, ok := user.RoleID(); ok { roleVal = v }

	statusVal := ""
	if v, ok := user.Status(); ok { statusVal = v }

	// Ambil Role Name dari Relasi
	roleNameVal := ""
	if roleData, ok := user.Role(); ok {
		roleNameVal = roleData.Name
	}

	userSession := UserSession{
		ID:       user.ID,
		Name:     user.Name,
		Email:    user.Email,
		Phone:    phoneVal,
		RoleID:   roleVal,
		RoleName: roleNameVal,
		Status:   statusVal,
	}

	jsonData, _ := json.Marshal(userSession)

	// F. Simpan ke Redis (24 jam)
	redisKey := "user_session:" + user.ID
	err = s.Redis.Set(ctx, redisKey, jsonData, 24*time.Hour).Err()
	// Kita abaikan error redis agar login tetap jalan (opsional bisa di-log)

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
	newAccessToken, err := s.generateAccessToken(user.ID, user.Email)

	return newAccessToken, err
}

// 4. GET SESSION FROM REDIS
func (s *AuthService) GetSession(ctx context.Context, userID string) (*UserSession, error) {
	redisKey := "user_session:" + userID
	
	val, err := s.Redis.Get(ctx, redisKey).Result()
	if err != nil {
		return nil, err // Key not found / expired
	}

	var session UserSession
	if err := json.Unmarshal([]byte(val), &session); err != nil {
		return nil, err
	}

	return &session, nil
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