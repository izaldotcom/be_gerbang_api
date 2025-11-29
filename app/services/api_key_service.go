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

func CreateApiKey(ctx context.Context, client *db.PrismaClient, sellerName string) (*db.APIKeyModel, error) {
    apiKey := generateRandomKey(32)
    secret := generateRandomKey(32)

    key, err := client.APIKey.CreateOne(
        db.APIKey.SellerName.Set(sellerName),
        db.APIKey.APIKey.Set(apiKey),
        db.APIKey.Secret.Set(secret),
    ).Exec(ctx)

    return key, err
}

func ValidateApiKey(ctx context.Context, client *db.PrismaClient, apiKey string) (*db.APIKeyModel, error) {
    return client.APIKey.FindUnique(
        db.APIKey.APIKey.Equals(apiKey),
    ).Exec(ctx)
}
