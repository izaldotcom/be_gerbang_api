package main

import (
	"log"
	"os"

	"gerbangapi/app/services/scraper"
	// "gerbangapi/prisma/db" // Tidak diperlukan jika kita init Redis manual

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9" // [WAJIB] Import Redis
)

func main() {
	godotenv.Load()

	// 1. [BARU] Setup Redis Client
	// Pastikan Redis server berjalan
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: "", // Kosongkan jika tanpa password
		DB:       0,
	})

	// 2. Init Scraper (Masukkan parameter ke-2: redisClient)
	svc, err := scraper.NewMitraHiggsService(false, redisClient)
	if err != nil {
		log.Fatal(err)
	}
	defer svc.Close()

	// 3. Coba Login
	// Sebaiknya gunakan Environment Variable agar tidak hardcode kredensial
	email := "email_mitra@gmail.com"
	password := "password_mitra"

	// Override dari env jika ada (opsional)
	if os.Getenv("MH_USERNAME") != "" {
		email = os.Getenv("MH_USERNAME")
		password = os.Getenv("MH_PASSWORD")
	}

	log.Printf("Login sebagai: %s", email)
	err = svc.Login(email, password)
	if err != nil {
		log.Fatal("Login Gagal:", err)
	}

	log.Println("✅ Login Berhasil, mencoba Place Order...")

	// 4. Test Place Order
	trxID, err := svc.PlaceOrder("12345678", "1M", 1) // ID Player Dummy
	if err != nil {
		log.Fatal("Order Gagal:", err)
	}
	log.Println("✅ Order Sukses! TRX ID:", trxID)
}