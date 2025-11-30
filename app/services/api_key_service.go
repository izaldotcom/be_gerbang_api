package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"gerbangapi/prisma/db"
)

func generateRandomKey(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Perhatikan return type: db.APIKeyModel (Huruf besar API)
func CreateApiKey(ctx context.Context, client *db.PrismaClient, userID string, sellerName string) (*db.APIKeyModel, error) {
	apiKey := generateRandomKey(32)
	secret := generateRandomKey(32)

	// 👇 PERBAIKAN: Gunakan client.APIKey (Besar) & User.Link
	key, err := client.APIKey.CreateOne(
		// 1. Relasi User (Wajib) -> Pakai Link, bukan UserID.Set
		db.APIKey.User.Link(
			db.User.ID.Equals(userID),
		),
		
		// 2. API Key (Wajib)
		db.APIKey.APIKey.Set(apiKey),
		
		// 3. Secret (Wajib)
		db.APIKey.Secret.Set(secret),
		
		// 4. Field Opsional (SellerName)
		db.APIKey.SellerName.Set(sellerName),
	).Exec(ctx)

	return key, err
}

func ValidateApiKey(ctx context.Context, client *db.PrismaClient, apiKey string) (*db.APIKeyModel, error) {
	// 👇 PERBAIKAN: Gunakan client.APIKey
	return client.APIKey.FindUnique(
		db.APIKey.APIKey.Equals(apiKey),
	).Exec(ctx)
}