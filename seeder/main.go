package main

import (
	"context"
	"fmt"
	"log"

	"gerbangapi/app/utils"
	"gerbangapi/prisma/db"
)

func main() {
	// 1. Setup Koneksi Database
	client := db.NewClient()
	if err := client.Prisma.Connect(); err != nil {
		log.Fatal("❌ Gagal connect DB:", err)
	}
	defer func() {
		if err := client.Prisma.Disconnect(); err != nil {
			panic(err)
		}
	}()

	ctx := context.Background()
	log.Println("🚀 Memulai Seeding Lengkap (Role, User, API Key)...")

	// ==========================================
	// 1. SEED ROLES (Admin, Customer, Operator)
	// ==========================================
	roleNames := []string{"Admin", "Customer", "Operator"}

	for _, name := range roleNames {
		// Cek apakah role sudah ada
		existingRole, err := client.Role.FindFirst(
			db.Role.Name.Equals(name),
		).Exec(ctx)

		if err != nil || existingRole == nil {
			createdRole, err := client.Role.CreateOne(
				db.Role.Name.Set(name),
				db.Role.Description.Set(fmt.Sprintf("Role for %s", name)),
			).Exec(ctx)

			if err != nil {
				log.Printf("⚠️ Gagal buat role %s: %v", name, err)
			} else {
				log.Printf("✅ Role Created: %s (UUID: %s)", name, createdRole.ID)
			}
		} else {
			log.Printf("ℹ️ Role %s sudah ada (UUID: %s)", name, existingRole.ID)
		}
	}

	// ==========================================
	// 2. SEED USER ADMIN
	// ==========================================
	
	adminRole, err := client.Role.FindFirst(
		db.Role.Name.Equals("Admin"),
	).Exec(ctx)

	if err != nil || adminRole == nil {
		log.Fatal("❌ Error: Role 'Admin' tidak ditemukan. Seeding gagal.")
	}

	adminEmail := "admin@gerbangapi.com"
	adminPassword := "password123"
	var userID string

	existingUser, err := client.User.FindUnique(
		db.User.Email.Equals(adminEmail),
	).Exec(ctx)

	if err != nil || existingUser == nil {
		hashedPwd, _ := utils.HashPassword(adminPassword)

		newUser, err := client.User.CreateOne(
			db.User.Name.Set("Super Administrator"),
			db.User.Email.Set(adminEmail),
			db.User.Password.Set(hashedPwd),
			
			db.User.Role.Link(
				db.Role.ID.Equals(adminRole.ID),
			),
		).Exec(ctx)

		if err != nil {
			log.Fatal("❌ Gagal buat user admin:", err)
		}
		
		userID = newUser.ID
		fmt.Printf("✅ USER ADMIN DIBUAT (Email: %s)\n", newUser.Email)
	} else {
		userID = existingUser.ID
		log.Println("ℹ️ User admin sudah ada, menggunakan ID yang lama.")
	}

	// ==========================================
	// 3. SEED API KEY (Seller)
	// ==========================================
	
	apiKeyVal := "MTR-TEST-KEY"
	apiSecret := "RAHASIA-SANGAT-AMAN"

	// 👇 PERBAIKAN: Gunakan client.APIKey (Besar) dan db.APIKey (Besar)
	existingKey, err := client.APIKey.FindUnique(
		db.APIKey.APIKey.Equals(apiKeyVal),
	).Exec(ctx)

	if err != nil || existingKey == nil {
		newKey, err := client.APIKey.CreateOne(
			// 1. Relasi ke User
			db.APIKey.User.Link(
				db.User.ID.Equals(userID),
			),
			// 2. Data Key
			db.APIKey.APIKey.Set(apiKeyVal),
			db.APIKey.Secret.Set(apiSecret),
			
			// 3. Data Tambahan
			db.APIKey.SellerName.Set("Mitra Admin Seeder"),
		).Exec(ctx)

		if err != nil {
			log.Printf("❌ Gagal buat API Key: %v", err)
		} else {
			fmt.Println("-------------------------------------------")
			fmt.Println("✅ API KEY BERHASIL DIBUAT")
			fmt.Printf("   User Owner  : %s\n", adminEmail)
			fmt.Printf("   X-API-KEY   : %s\n", newKey.APIKey)
			fmt.Printf("   X-SIGNATURE : %s\n", newKey.Secret)
			fmt.Println("-------------------------------------------")
		}
	} else {
		log.Printf("ℹ️ API Key '%s' sudah ada, skip creation.", apiKeyVal)
	}
	
	log.Println("🚀 Seeding Selesai!")
}