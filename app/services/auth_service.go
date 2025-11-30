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

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type JwtClaims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

func Register(ctx context.Context, client *db.PrismaClient,
	id, role_id, name, email, phone, status, password string) error {

	if id == "" {
		id = uuid.New().String()
	}

	hashed, _ := utils.HashPassword(password)

	_, err := client.User.CreateOne(
		db.User.Name.Set(name),       
		db.User.Email.Set(email),     
		db.User.Password.Set(hashed), 
		
		// Variadic / Opsional
		db.User.ID.Set(id),
		db.User.RoleID.Set(role_id),
		db.User.Phone.Set(phone),
		db.User.Status.Set(status),
	).Exec(ctx)

	return err
}

func Login(ctx context.Context, client *db.PrismaClient, email, password string) (*TokenResponse, error) {
	user, err := client.User.FindUnique(
		db.User.Email.Equals(email),
	).Exec(ctx)

	if err != nil || user == nil {
		return nil, errors.New("email not found")
	}

	if !utils.CheckPassword(user.Password, password) {
		return nil, errors.New("invalid password")
	}

	accessToken, err := generateAccessToken(user.ID, user.Email)
	if err != nil {
		return nil, err
	}

	refreshTokenStr := uuid.New().String()
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	// 👇 PERBAIKAN DI SINI: Gunakan .Link() untuk relasi
	_, err = client.RefreshToken.CreateOne(
		db.RefreshToken.Token.Set(refreshTokenStr),
		
		// Masukkan Relasi User via Link
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

func generateAccessToken(userID, email string) (string, error) {
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

func RefreshTokenProcess(ctx context.Context, client *db.PrismaClient, refreshTokenInput string) (string, error) {
	storedToken, err := client.RefreshToken.FindUnique(
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
	newAccessToken, err := generateAccessToken(user.ID, user.Email)
	
	return newAccessToken, err
}