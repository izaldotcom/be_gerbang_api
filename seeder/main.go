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

	// ======================================================
	// 6. SUPPLIER PRODUCTS (Modal Dasar: 1M & 60M)
	// ======================================================
	
	// BASE 1: Koin 1M (ID HTML = '6')
	base1M := "MH_COIN_1M"
	if _, err := client.SupplierProduct.FindUnique(db.SupplierProduct.ID.Equals(base1M)).Exec(ctx); err != nil {
			client.SupplierProduct.CreateOne(
					db.SupplierProduct.ID.Set(base1M),
					db.SupplierProduct.Name.Set("Base Koin 1M"),
					db.SupplierProduct.SupplierProductID.Set("6"), // ID HTML
					db.SupplierProduct.Denom.Set(1000000),
					db.SupplierProduct.CostPrice.Set(1000),
					db.SupplierProduct.Supplier.Link(db.Supplier.ID.Equals(suppID)),
			).Exec(ctx)
	}

	// BASE 2: Koin 60M (ID HTML = '1')
	base60M := "MH_COIN_60M"
	if _, err := client.SupplierProduct.FindUnique(db.SupplierProduct.ID.Equals(base60M)).Exec(ctx); err != nil {
			client.SupplierProduct.CreateOne(
					db.SupplierProduct.ID.Set(base60M),
					db.SupplierProduct.Name.Set("Base Koin 60M"),
					db.SupplierProduct.SupplierProductID.Set("1"), // ID HTML
					db.SupplierProduct.Denom.Set(60000000),
					db.SupplierProduct.CostPrice.Set(60000),
					db.SupplierProduct.Supplier.Link(db.Supplier.ID.Equals(suppID)),
			).Exec(ctx)
	}

	// ======================================================
	// 7. INTERNAL PRODUCTS & RECIPES (Logika Perkalian)
	// ======================================================
	
	products := []struct {
			ID       string
			Name     string
			Price    int
			Denom    int
			BaseID   string // Bahan bakunya apa?
			Qty      int    // Dikali berapa?
	}{
			{"1M", "Koin Emas 1M", 1500, 1000000, base1M, 1},
			{"5M", "Koin Emas 5M", 7500, 5000000, base1M, 1},
			{"10M", "Koin Emas 10M", 15000, 10000000, base1M, 10},     // 10 x 1M
			{"60M", "Koin Emas 60M", 65000, 60000000, base60M, 1},     // 1 x 60M
			{"100M", "Koin Emas 100M", 140000, 100000000, base1M, 100}, // 100 x 1M
			{"200M", "Koin Emas 200M", 270000, 200000000, base1M, 200}, // 200 x 1M
	}

	for _, p := range products {
			// 1. Buat Produk Internal
			if _, err := client.Product.FindUnique(db.Product.ID.Equals(p.ID)).Exec(ctx); err != nil {
					client.Product.CreateOne(
							db.Product.ID.Set(p.ID),
							db.Product.Name.Set(p.Name),
							db.Product.Price.Set(p.Price),
							db.Product.Denom.Set(p.Denom),
							db.Product.Status.Set(true),
					).Exec(ctx)
					log.Printf("✅ Produk Internal Created: %s", p.Name)
			}

			// 2. Buat Resep (Perkalian)
			existingRecipe, _ := client.ProductRecipe.FindFirst(
					db.ProductRecipe.ProductID.Equals(p.ID),
			).Exec(ctx)

			if existingRecipe == nil {
					client.ProductRecipe.CreateOne(
							db.ProductRecipe.Quantity.Set(p.Qty), // KUNCI PERKALIAN DI SINI
							db.ProductRecipe.Product.Link(db.Product.ID.Equals(p.ID)),
							db.ProductRecipe.SupplierProduct.Link(db.SupplierProduct.ID.Equals(p.BaseID)),
					).Exec(ctx)
					log.Printf("   -> Resep: %d x %s", p.Qty, p.BaseID)
			}
	}

	log.Println("🏁 Seeding Selesai!")
}