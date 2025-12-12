package main

import (
	"gerbangapi/app/services/scraper"
	"gerbangapi/prisma/db"
	"log"

	"github.com/joho/godotenv"
)

func main() {
	// 1. Load Env & Redis
	godotenv.Load()
	db.ConnectRedis()

	// 2. Init Scraper
	log.Println("🤖 Menjalankan Bot...")
	svc, err := scraper.NewMitraHiggsService()
	if err != nil {
		log.Fatal(err)
	}
	defer svc.Close()

	// 3. TEST LOGIN DENGAN AKUN ANDA
	// ID: 946235
	// Pass: Aldy2015
	log.Println("🔑 Mencoba Login dengan ID: 946235...")
	
	err = svc.Login("946235", "Aldy2015")
	
	if err != nil {
		log.Fatal("❌ Login Gagal:", err)
	}

	log.Println("🎉 Login Berhasil! Bot akan tetap terbuka selama 10 detik agar Anda bisa lihat hasilnya.")
	
	// Tahan browser sebentar biar bisa dilihat (jangan langsung close)
	svc.Page.WaitForTimeout(300000) 
}