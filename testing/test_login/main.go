package main

import (
	"log"
	"os"
	"time"

	"gerbangapi/app/services/scraper" // Sesuaikan nama module Anda

	"github.com/joho/godotenv"
)

func main() {
	// 1. Load Env (Arahkan ke file .env project)
	if err := godotenv.Load("../../.env"); err != nil {
		log.Println("⚠️ Warning: Tidak bisa load .env dari folder parent, mencoba default...")
		godotenv.Load() 
	}

	log.Println("🧪 TESTING: Memulai Browser (Visual Mode)...")

	// 2. Init Service dengan Debug Mode = TRUE (Browser Muncul)
	svc, err := scraper.NewMitraHiggsService(true)
	if err != nil {
		log.Fatal(err)
	}
	// Jangan lupa Close, tapi mungkin kita mau lihat hasilnya dulu jadi bisa di-comment atau diberi delay
	defer svc.Close() 

	// 3. Tes Login
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