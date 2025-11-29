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

// HAPUS baris variabel global ini:
// var jwtSecret = []byte(os.Getenv("JWT_SECRET")) ❌

type JwtClaims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// REGISTER (Tidak ada perubahan)
func Register(ctx context.Context, client *db.PrismaClient,
	id, role_id, name, email, phone, status, password string) error {

	if id == "" {
		id = uuid.New().String()
	}

	hashed, _ := utils.HashPassword(password)

	_, err := client.User.CreateOne(
		db.User.ID.Set(id),
		db.User.RoleID.Set(role_id),
		db.User.Name.Set(name),
		db.User.Email.Set(email),
		db.User.Password.Set(hashed),
		db.User.Phone.Set(phone),
		db.User.Status.Set(status),
	).Exec(ctx)

	return err
}

// LOGIN
func Login(ctx context.Context, client *db.PrismaClient, email, password string) (string, error) {

	user, err := client.User.FindUnique(
		db.User.Email.Equals(email),
	).Exec(ctx)

	if err != nil || user == nil {
		return "", errors.New("email not found")
	}

	if !utils.CheckPassword(user.Password, password) {
		return "", errors.New("invalid password")
	}

	claims := &JwtClaims{
		UserID: user.ID,
		Email:  user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// 👇 PERBAIKAN: Baca secret DI SINI, saat fungsi dipanggil
	// Ini menjamin .env sudah dimuat oleh main.go
	jwtSecret := []byte(os.Getenv("JWT_SECRET")) 
	
	t, err := token.SignedString(jwtSecret) // Gunakan variabel lokal
    if err != nil {
        return "", err
    }

	return t, nil
}