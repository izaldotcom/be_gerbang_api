package main

import (
	"log"
	"os"
	"time"

	"gerbangapi/app/services/scraper" // Sesuaikan nama module Anda

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9" // [BARU] Import Redis
)

func main() {
	// 1. Load Env
	// Sesuaikan path ini dengan lokasi file .env Anda relatif terhadap file ini
	if err := godotenv.Load(".env"); err != nil {
			log.Println("⚠️ Warning: Tidak bisa load .env, mencoba default system env...")
	}

	log.Println("🧪 TESTING: Memulai Browser (Visual Mode)...")

	// 2. [BARU] Init Redis Client (Wajib untuk Scraper Service)
	// Pastikan Redis server sudah berjalan
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	
	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: "", // Sesuaikan jika ada password
		DB:       0,
	})

	// 3. Init Service dengan Debug Mode = TRUE dan Redis Client
	// Parameter ke-2 sekarang wajib diisi
	svc, err := scraper.NewMitraHiggsService(true, redisClient)
	if err != nil {
		log.Fatal(err)
	}
	defer svc.Close()

	// 4. Tes Login
	username := os.Getenv("MH_USERNAME")
	password := os.Getenv("MH_PASSWORD")

	if username == "" || password == "" {
		log.Fatal("❌ MH_USERNAME atau MH_PASSWORD belum diset di .env")
	}

	log.Printf("👤 Login sebagai: %s", username)

	err = svc.Login(username, password)
	if err != nil {
		log.Fatalf("❌ Login Gagal: %v", err)
	}

	log.Println("✅ Login Berhasil! Browser akan menutup dalam 10 detik...")

	// Tahan browser sebentar agar Anda bisa melihat hasilnya
	time.Sleep(10 * time.Second)
}