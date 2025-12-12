package main

import (
	"context"
	"fmt"
	"log"

	"gerbangapi/app/utils"
	"gerbangapi/prisma/db"

	"github.com/joho/godotenv"
)

func main() {
	// 0. Load Env
	if err := godotenv.Load(".env"); err != nil {
		if err2 := godotenv.Load("../.env"); err2 != nil {
			log.Println("⚠️  Warning: .env file not found.")
		}
	}

	// 1. Setup DB
	client := db.NewClient()
	if err := client.Prisma.Connect(); err != nil {
		log.Fatal("❌ Gagal connect DB:", err)
	}
	defer client.Prisma.Disconnect()

	ctx := context.Background()
	log.Println("🚀 Memulai Seeding Lengkap...")

	// ==========================================
	// 1. SEED ROLES
	// ==========================================
	roleNames := []string{"Admin", "Customer", "Operator"}
	for _, name := range roleNames {
		existingRole, err := client.Role.FindFirst(db.Role.Name.Equals(name)).Exec(ctx)
		if err != nil || existingRole == nil {
			client.Role.CreateOne(
				db.Role.Name.Set(name),
				db.Role.Description.Set(fmt.Sprintf("Role for %s", name)),
			).Exec(ctx)
			log.Printf("✅ Role Created: %s", name)
		}
	}

	// ==========================================
	// 2. SEED USER ADMIN
	// ==========================================
	adminRole, err := client.Role.FindFirst(db.Role.Name.Equals("Admin")).Exec(ctx)
	if err == nil && adminRole != nil {
		adminEmail := "admin@gerbangapi.com"
		existingUser, _ := client.User.FindUnique(db.User.Email.Equals(adminEmail)).Exec(ctx)

		if existingUser == nil {
			hashedPwd, _ := utils.HashPassword("password123")
			newUser, err := client.User.CreateOne(
				db.User.Name.Set("Super Administrator"),
				db.User.Email.Set(adminEmail),
				db.User.Password.Set(hashedPwd),
				db.User.Role.Link(db.Role.ID.Equals(adminRole.ID)),
				db.User.Status.Set("active"),
			).Exec(ctx)

			if err == nil {
				log.Printf("✅ ADMIN DIBUAT: %s (ID: %s)", adminEmail, newUser.ID)
				
				// 3. API KEY
				apiKeyVal := "MTR-TEST-KEY"
				existingKey, _ := client.APIKey.FindUnique(db.APIKey.APIKey.Equals(apiKeyVal)).Exec(ctx)
				if existingKey == nil {
					client.APIKey.CreateOne(
						db.APIKey.User.Link(db.User.ID.Equals(newUser.ID)),
						db.APIKey.APIKey.Set(apiKeyVal),
						db.APIKey.Secret.Set("RAHASIA-SANGAT-AMAN"),
						db.APIKey.SellerName.Set("Mitra Admin Seeder"),
					).Exec(ctx)
					log.Println("✅ API KEY CREATED")
				}
			}
		} else {
			log.Println("ℹ️ Admin User sudah ada.")
		}
	}

	// ==========================================
	// 4. SEED INTERNAL PRODUCT
	// ==========================================
	// URUTAN: Name -> Denom -> Price
	prodID := "1M"
	existingProd, _ := client.Product.FindUnique(db.Product.ID.Equals(prodID)).Exec(ctx)

	if existingProd == nil {
		_, err := client.Product.CreateOne(
			// Field WAJIB (Urutan sesuai Prisma Generator)
			db.Product.Name.Set("Koin Emas 1M"),
			db.Product.Denom.Set(1000000), 
			db.Product.Price.Set(1500),
			
			// Field Opsional/ID/Default di bawah
			db.Product.ID.Set(prodID),
			db.Product.Status.Set(true),
		).Exec(ctx)
		
		if err != nil {
			log.Printf("❌ Gagal Product 1M: %v", err)
		} else {
			log.Println("✅ Product '1M' Created")
		}
	} else {
		log.Println("ℹ️ Product '1M' sudah ada.")
	}

	// ==========================================
	// 5. SEED SUPPLIER
	// ==========================================
	// FIX: Menambahkan field Type yang wajib
	suppID := "mitra-higgs"
	existingSupp, _ := client.Supplier.FindUnique(db.Supplier.ID.Equals(suppID)).Exec(ctx)

	if existingSupp == nil {
		client.Supplier.CreateOne(
			// Field Wajib: Name -> Type
			db.Supplier.Name.Set("Mitra Higgs Official"),
			db.Supplier.Type.Set("official"), // <-- INI YANG MEMPERBAIKI ERROR ANDA
			
			// Baru ID (Opsional/Manual)
			db.Supplier.ID.Set(suppID),
		).Exec(ctx)
		log.Println("✅ Supplier Created")
	}

	// ==========================================
	// 6. SEED SUPPLIER PRODUCT
	// ==========================================
	// FIX: Urutan Alfabetis (CostPrice -> Denom -> Name -> SupplierProductID)
	suppProdID := "MH_COIN_1M"
	existingSuppProd, _ := client.SupplierProduct.FindUnique(db.SupplierProduct.ID.Equals(suppProdID)).Exec(ctx)

	if existingSuppProd == nil {
		_, err := client.SupplierProduct.CreateOne(
			// Urutan Field Wajib (Alfabetis)
			db.SupplierProduct.CostPrice.Set(1000),         // C
			db.SupplierProduct.Denom.Set(1000000),          // D
			db.SupplierProduct.Name.Set("Bahan 1M MH"),     // N
			db.SupplierProduct.SupplierProductID.Set("1M"), // S
			
			// Relation & ID & Optional ditaruh di akhir
			db.SupplierProduct.Supplier.Link(db.Supplier.ID.Equals(suppID)),
			db.SupplierProduct.ID.Set(suppProdID),
		).Exec(ctx)
		
		if err != nil {
			log.Printf("❌ Gagal Supplier Product: %v", err)
		} else {
			log.Println("✅ Supplier Product Created")
		}
	} else {
		log.Println("ℹ️ Supplier Product sudah ada.")
	}

	// ==========================================
	// 7. SEED RECIPE
	// ==========================================
	existingRecipe, _ := client.ProductRecipe.FindFirst(
		db.ProductRecipe.ProductID.Equals(prodID),
		db.ProductRecipe.SupplierProductID.Equals(suppProdID),
	).Exec(ctx)

	if existingRecipe == nil {
		_, err := client.ProductRecipe.CreateOne(
			db.ProductRecipe.Quantity.Set(1),
			db.ProductRecipe.Product.Link(db.Product.ID.Equals(prodID)),
			db.ProductRecipe.SupplierProduct.Link(db.SupplierProduct.ID.Equals(suppProdID)),
		).Exec(ctx)
		if err != nil {
			log.Printf("❌ Gagal Recipe: %v", err)
		} else {
			log.Println("✅ Recipe Created")
		}
	} else {
		log.Println("ℹ️ Recipe sudah ada.")
	}

	log.Println("🏁 Seeding Selesai!")
}