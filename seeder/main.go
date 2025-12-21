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

	// ==========================================
	// 🧹 STEP 0: CLEAN UP DATA (URUTAN PENTING!)
	// ==========================================
	log.Println("🧹 Membersihkan data lama...")

	// Hapus Child dulu (Foreign Key constraints)
	client.SupplierOrderItem.FindMany().Delete().Exec(ctx)
	client.SupplierOrder.FindMany().Delete().Exec(ctx)
	client.InternalOrder.FindMany().Delete().Exec(ctx)
	client.ProductRecipe.FindMany().Delete().Exec(ctx)
	
    // Product & SupplierProduct harus dihapus sebelum Supplier
    client.Product.FindMany().Delete().Exec(ctx) 
	client.SupplierProduct.FindMany().Delete().Exec(ctx)
    
    // Baru hapus Parent
	client.Supplier.FindMany().Delete().Exec(ctx)
	client.APIKey.FindMany().Delete().Exec(ctx)
	client.RefreshToken.FindMany().Delete().Exec(ctx)
	client.User.FindMany().Delete().Exec(ctx)
	client.Role.FindMany().Delete().Exec(ctx)

	log.Println("✨ Database bersih!")
	log.Println("🚀 Memulai Seeding Baru...")

	// ==========================================
	// 1. SEED ROLES
	// ==========================================
	roleNames := []string{"Admin", "Customer", "Operator"}
	for _, name := range roleNames {
		client.Role.CreateOne(
			db.Role.Name.Set(name),
			db.Role.Description.Set(fmt.Sprintf("Role for %s", name)),
		).Exec(ctx)
		log.Printf("✅ Role Created: %s", name)
	}

	// ==========================================
	// 2. SEED USER ADMIN
	// ==========================================
	adminRole, _ := client.Role.FindFirst(db.Role.Name.Equals("Admin")).Exec(ctx)
	adminEmail := "admin@gerbangapi.com"

	hashedPwd, _ := utils.HashPassword("password123")
	newUser, err := client.User.CreateOne(
		db.User.Name.Set("Super Administrator"),
		db.User.Email.Set(adminEmail),
		db.User.Password.Set(hashedPwd),
		db.User.Role.Link(db.Role.ID.Equals(adminRole.ID)),
		db.User.Status.Set("active"),
	).Exec(ctx)

	if err == nil {
		log.Printf("✅ ADMIN DIBUAT: %s (UUID: %s)", adminEmail, newUser.ID)

		// 3. API KEY
		client.APIKey.CreateOne(
			db.APIKey.User.Link(db.User.ID.Equals(newUser.ID)),
			db.APIKey.APIKey.Set("MTR-TEST-KEY"),
			db.APIKey.Secret.Set("RAHASIA-SANGAT-AMAN"),
			db.APIKey.SellerName.Set("Mitra Admin Seeder"),
		).Exec(ctx)
		log.Println("✅ API KEY CREATED")
	}

	// ==========================================
	// 4. SEED SUPPLIER
	// ==========================================
	suppCode := "MH_OFFICIAL"
	newSupp, err := client.Supplier.CreateOne(
		db.Supplier.Name.Set("Mitra Higgs Official"),
		db.Supplier.Type.Set("official"),
		db.Supplier.Code.Set(suppCode),
	).Exec(ctx)

	if err != nil {
		log.Fatal("❌ Gagal buat supplier: ", err)
	}
	supplierID := newSupp.ID
	log.Printf("✅ Supplier Created (UUID: %s)", supplierID)

	// ======================================================
	// 5. SUPPLIER PRODUCTS (Modal Dasar)
	// ======================================================

	var base1MID, base60MID string

	// --- BASE 1: Koin 1M (ID HTML '6') ---
	newSp1, _ := client.SupplierProduct.CreateOne(
		// WAJIB (Scalar)
		db.SupplierProduct.SupplierProductID.Set("6"),
		db.SupplierProduct.Name.Set("Base Koin 1M"),
		db.SupplierProduct.Denom.Set(1000000),
		db.SupplierProduct.CostPrice.Set(1000),

		// WAJIB (Relasi)
		db.SupplierProduct.Supplier.Link(db.Supplier.ID.Equals(supplierID)),

		// OPSIONAL (Scalar)
		db.SupplierProduct.Price.Set(1000),
	).Exec(ctx)
	base1MID = newSp1.ID
	log.Printf("✅ Supplier Prod 1M Created (UUID: %s)", base1MID)

	// --- BASE 2: Koin 60M (ID HTML '1') ---
	newSp60, _ := client.SupplierProduct.CreateOne(
		// WAJIB (Scalar)
		db.SupplierProduct.SupplierProductID.Set("1"),
		db.SupplierProduct.Name.Set("Base Koin 60M"),
		db.SupplierProduct.Denom.Set(60000000),
		db.SupplierProduct.CostPrice.Set(60000),

		// WAJIB (Relasi)
		db.SupplierProduct.Supplier.Link(db.Supplier.ID.Equals(supplierID)),

		// OPSIONAL (Scalar)
		db.SupplierProduct.Price.Set(60000),
	).Exec(ctx)
	base60MID = newSp60.ID
	log.Printf("✅ Supplier Prod 60M Created (UUID: %s)", base60MID)

	// ======================================================
	// 6. INTERNAL PRODUCTS & RECIPES
	// ======================================================

	products := []struct {
		Name    string
		Price   int
		Denom   int
		Recipes []struct {
			BaseUUID string
			Qty      int
		}
	}{
		// 1. Produk Simple (1M)
		{
			"Koin Emas 1M", 1500, 1000000,
			[]struct {
				BaseUUID string
				Qty      int
			}{{base1MID, 1}},
		},
		// 2. Produk Simple Loop (5M = 5x 1M)
		{
			"Koin Emas 5M", 7500, 5000000,
			[]struct {
				BaseUUID string
				Qty      int
			}{{base1MID, 5}},
		},
		// 3. PRODUK MIX (100M = 1x 60M + 40x 1M)
		{
			"Koin Emas 100M (Mix Hemat)", 140000, 100000000,
			[]struct {
				BaseUUID string
				Qty      int
			}{
				{base60MID, 1}, // Klik 60M 1 kali
				{base1MID, 40}, // Klik 1M 40 kali
			},
		},
	}

	for _, p := range products {
		// 1. Buat Produk Internal
		// PERBAIKAN: Menambahkan .Link ke SupplierID (Wajib karena Schema berubah)
		newProd, err := client.Product.CreateOne(
			db.Product.Name.Set(p.Name),
			db.Product.Denom.Set(p.Denom),
			db.Product.Price.Set(p.Price),
			db.Product.Qty.Set(1),
			db.Product.Status.Set(true),
			
            // --- [FIX] LINK KE SUPPLIER ---
			db.Product.Supplier.Link(db.Supplier.ID.Equals(supplierID)), 
		).Exec(ctx)

		if err != nil {
			log.Printf("❌ Gagal create product %s: %v", p.Name, err)
			continue
		}

		log.Printf("✅ Produk Internal Created: %s", p.Name)

		// 2. Buat Resep (Looping recipe items)
		for _, r := range p.Recipes {
			_, err := client.ProductRecipe.CreateOne(
				db.ProductRecipe.Quantity.Set(r.Qty),
				db.ProductRecipe.Product.Link(db.Product.ID.Equals(newProd.ID)),
				db.ProductRecipe.SupplierProduct.Link(db.SupplierProduct.ID.Equals(r.BaseUUID)),
			).Exec(ctx)

			if err != nil {
				log.Printf("   ⚠️ Gagal tambah resep: %v", err)
			} else {
				log.Printf("   -> Resep Added: %d x [BaseUUID: ...%s]", r.Qty, r.BaseUUID[len(r.BaseUUID)-5:])
			}
		}
	}

	log.Println("🏁 Seeding Selesai!")
}