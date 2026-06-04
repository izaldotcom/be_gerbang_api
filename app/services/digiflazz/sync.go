package digiflazz

import (
	"context"
	"errors"
	"fmt"
	"log"

	"gerbangapi/prisma/db"

	"github.com/redis/go-redis/v9" // [BARU] Import library Redis
)

// SyncProducts mengambil data dari Digiflazz dan menyimpannya ke database
// [PERBAIKAN] Menambahkan parameter redisClient di bagian akhir
func SyncProducts(dbClient *db.PrismaClient, digiService *DigiflazzService, supplierID string, redisClient *redis.Client) error {
	ctx := context.Background()

	log.Println("🔄 Memulai sinkronisasi produk dari Digiflazz...")

	// 1. Tarik data dari API Digiflazz
	products, err := digiService.GetPriceList("prepaid")
	if err != nil {
		return fmt.Errorf("gagal menarik data pricelist: %v", err)
	}

	suksesBaru := 0
	suksesUpdate := 0

	// 2. Looping data JSON dan masukkan ke Database
	for _, p := range products {
		// Parsing data interface{} ke tipe data asli
		skuCode, _ := p["buyer_sku_code"].(string)
		name, _ := p["product_name"].(string)
		costPriceFloat, _ := p["price"].(float64)
		buyerStatus, _ := p["buyer_product_status"].(bool)
		sellerStatus, _ := p["seller_product_status"].(bool)

		// Konversi harga modal ke integer
		costPrice := int(costPriceFloat)

		// Status aktif hanya jika buyer dan seller statusnya true
		isActive := buyerStatus && sellerStatus

		// Cek apakah produk ini sudah ada di database untuk supplier ini
		existingProduct, err := dbClient.SupplierProduct.FindFirst(
			db.SupplierProduct.SupplierProductID.Equals(skuCode),
			db.SupplierProduct.SupplierID.Equals(supplierID),
		).Exec(ctx)

		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				// --- CREATE PRODUK BARU ---
				_, errCreate := dbClient.SupplierProduct.CreateOne(
					db.SupplierProduct.SupplierProductID.Set(skuCode),
					db.SupplierProduct.Name.Set(name),
					db.SupplierProduct.Denom.Set(1),
					db.SupplierProduct.CostPrice.Set(costPrice),
					db.SupplierProduct.Supplier.Link(
						db.Supplier.ID.Equals(supplierID),
					),
					db.SupplierProduct.Price.Set(costPrice),
					db.SupplierProduct.Status.Set(isActive),
				).Exec(ctx)

				if errCreate != nil {
					log.Printf("⚠️ Gagal membuat produk %s: %v", skuCode, errCreate)
				} else {
					suksesBaru++
				}
			} else {
				log.Printf("⚠️ Error saat mencari produk %s: %v", skuCode, err)
			}
		} else {
			// --- UPDATE PRODUK LAMA (Stok, Harga, Status) ---
			_, errUpdate := dbClient.SupplierProduct.FindUnique(
				db.SupplierProduct.ID.Equals(existingProduct.ID),
			).Update(
				db.SupplierProduct.Name.Set(name),
				db.SupplierProduct.CostPrice.Set(costPrice),
				db.SupplierProduct.Status.Set(isActive),
				// db.SupplierProduct.Price.Set(costPrice), // Buka baris ini jika harga jual ingin otomatis ngikutin harga modal
			).Exec(ctx)

			if errUpdate != nil {
				log.Printf("⚠️ Gagal update produk %s: %v", skuCode, errUpdate)
			} else {
				suksesUpdate++
			}
		}
	}

	log.Printf("✅ Sinkronisasi Selesai! %d Produk Baru, %d Produk Diperbarui.", suksesBaru, suksesUpdate)

	// ==========================================
	// [BARU] PENGHAPUSAN CACHE LAMA (CACHE INVALIDATION)
	// ==========================================
	errDel := redisClient.Del(ctx, "seller:pricelist:all").Err()
	if errDel != nil {
		log.Printf("⚠️ Gagal menghapus cache Redis (mungkin cache sudah kosong): %v", errDel)
	} else {
		log.Println("🧹 Cache lama berhasil dibersihkan dari Redis! Seller sekarang akan melihat harga terbaru.")
	}

	return nil
}