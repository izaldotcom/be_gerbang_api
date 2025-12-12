package db

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/redis/go-redis/v9"
)

// ✅ PENTING: Gunakan Huruf Besar 'R' agar bisa diakses dari package lain (Exported)
var Rdb *redis.Client

func ConnectRedis() {
	// Ambil konfigurasi dari .env atau default
	redisHost := os.Getenv("REDIS_HOST")
	if redisHost == "" {
		redisHost = "localhost"
	}

	redisPort := os.Getenv("REDIS_PORT")
	if redisPort == "" {
		redisPort = "6379"
	}

	redisPassword := os.Getenv("REDIS_PASSWORD")

	addr := fmt.Sprintf("%s:%s", redisHost, redisPort)

	// Inisialisasi Client Redis
	Rdb = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: redisPassword, // kosongkan jika tidak ada password
		DB:       0,             // use default DB
	})

	// Test Koneksi
	_, err := Rdb.Ping(context.Background()).Result()
	if err != nil {
		log.Printf("⚠️  Warning: Gagal connect ke Redis di %s: %v", addr, err)
		log.Println("➡️  Fitur yang butuh Redis (seperti Scraper) mungkin error.")
	} else {
		log.Printf("✅ Redis connected at %s", addr)
	}
}