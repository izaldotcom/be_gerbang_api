package main

import (
	"gerbangapi/app/services/scraper"
	"gerbangapi/prisma/db"
	"log"

	"github.com/joho/godotenv"
)

func main() {
    godotenv.Load()
	db.ConnectRedis()

	// 1. Init Scraper
	svc, err := scraper.NewMitraHiggsService()
	if err != nil {
		log.Fatal(err)
	}
	defer svc.Close()

	// 2. Coba Login
	// Masukkan akun asli MitraHiggs Anda di sini untuk tes
	err = svc.Login("email_mitra@gmail.com", "password_mitra")
	if err != nil {
		log.Fatal("Login Gagal:", err)
	}

	// 3. Coba Order Dummy (Hati-hati saldo kepotong kalau sukses)
	// trxID, err := svc.PlaceOrder("12345678", "1M")
	// if err != nil {
	// 	log.Fatal("Order Gagal:", err)
	// }
	// log.Println("Order ID:", trxID)
}