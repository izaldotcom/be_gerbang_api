package services

import (
	"context"
	"errors"
	"os"
	"time"

	"gerbangapi/app/utils"
	"gerbangapi/prisma/db"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Struct Service utama
type AuthService struct {
	DB *db.PrismaClient
}

// Constructor (Wajib ada untuk main.go)
func NewAuthService(dbClient *db.PrismaClient) *AuthService {
	return &AuthService{
		DB: dbClient,
	}
}

// --- STRUCTS UNTUK DATA TRANSFER ---

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type JwtClaims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// Input Struct agar rapi saat dipanggil dari Handler
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
	Email    string
	Password string
}

// --- METHODS ---

// Register sekarang menjadi method dari *AuthService
func (s *AuthService) Register(ctx context.Context, input RegisterInput) error {

	// Logika ID Default jika kosong
	if input.ID == "" {
		input.ID = uuid.New().String()
	}

	// Hash Password menggunakan utils Anda
	hashed, _ := utils.HashPassword(input.Password)

	// Simpan ke DB
	_, err := s.DB.User.CreateOne(
		db.User.Name.Set(input.Name),
		db.User.Email.Set(input.Email),
		db.User.Password.Set(hashed),

		// Field Opsional / Variadic
		db.User.ID.Set(input.ID),
		
		// Pastikan RoleID tidak kosong jika skema DB mewajibkannya, 
		// atau gunakan logic default string "user" jika kosong
		db.User.RoleID.Set(input.RoleID), 
		
		db.User.Phone.Set(input.Phone),
		db.User.Status.Set(input.Status),
	).Exec(ctx)

	return err
}

// Login sekarang menjadi method dari *AuthService
func (s *AuthService) Login(ctx context.Context, input LoginInput) (*TokenResponse, error) {
	// 1. Cari User
	user, err := s.DB.User.FindUnique(
		db.User.Email.Equals(input.Email),
	).Exec(ctx)

	if err != nil || user == nil {
		return nil, errors.New("email not found")
	}

	// 2. Cek Password menggunakan utils Anda
	if !utils.CheckPassword(user.Password, input.Password) {
		return nil, errors.New("invalid password")
	}

	// 3. Generate Access Token
	accessToken, err := s.generateAccessToken(user.ID, user.Email)
	if err != nil {
		return nil, err
	}

	// 4. Generate Refresh Token
	refreshTokenStr := uuid.New().String()
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	// Simpan Refresh Token ke DB (Mempertahankan logika .Link Anda)
	_, err = s.DB.RefreshToken.CreateOne(
		db.RefreshToken.Token.Set(refreshTokenStr),

		// Masukkan Relasi User via Link (Sesuai kode existing Anda)
		db.RefreshToken.User.Link(
			db.User.ID.Equals(user.ID),
		),

		db.RefreshToken.ExpiresAt.Set(expiresAt),
	).Exec(ctx)

	if err != nil {
		return nil, err
	}

	return &TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshTokenStr,
	}, nil
}

// RefreshTokenProcess method
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

// Helper Private Method
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