package worker // Sesuaikan dengan nama package worker Anda

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"gerbangapi/app/services/digiflazz"
	"gerbangapi/prisma/db"
)

// StartDigiflazzWorker menjalankan engine Digiflazz di background
func StartDigiflazzWorker(dbClient *db.PrismaClient) {
	log.Println("🚀 Starting Digiflazz API Worker (Background Mode)...")

	go func() {
		for {
			err := processNextDigiflazzOrder(dbClient)
			if err != nil {
				log.Printf("❌ Digiflazz Worker Error: %v", err)
			}
			// Jeda 3 detik sebelum cek order berikutnya untuk menghemat resource CPU
			time.Sleep(3 * time.Second)
		}
	}()
}

func processNextDigiflazzOrder(dbClient *db.PrismaClient) error {
	ctx := context.Background()

	// 1. Cari Supplier DIGIFLAZZ_OFFICIAL
	supplierDF, err := dbClient.Supplier.FindFirst(
		db.Supplier.Code.Equals("DIGIFLAZZ_OFFICIAL"),
	).Exec(ctx)

	if err != nil {
		return fmt.Errorf("Supplier 'DIGIFLAZZ_OFFICIAL' tidak ditemukan")
	}

	// 2. Cari Order berstatus 'pending' khusus untuk Digiflazz
	supplierOrder, err := dbClient.SupplierOrder.FindFirst(
		db.SupplierOrder.Status.Equals("pending"),
		db.SupplierOrder.SupplierID.Equals(supplierDF.ID),
	).Exec(ctx)

	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil // Tidak ada antrian, lewati
		}
		return err
	}

	orderID := supplierOrder.ID
	log.Printf("🔥 Processing Digiflazz Order #%s", orderID)

	// 3. Kunci order agar tidak diproses ganda (Ubah ke 'processing')
	dbClient.Prisma.ExecuteRaw("UPDATE supplier_order SET status='processing' WHERE id=?", orderID).Exec(ctx)

	// 4. Ambil Detail Internal Order
	internalOrder, err := dbClient.InternalOrder.FindUnique(
		db.InternalOrder.ID.Equals(supplierOrder.InternalOrderID),
	).Exec(ctx)

	if err != nil {
		failDigiflazzOrder(dbClient, orderID, supplierOrder.InternalOrderID, "Internal Order Not Found")
		return nil
	}

	// 5. Ambil Bahan Baku (Item Detail)
	var items []map[string]interface{}
	dbClient.Prisma.QueryRaw(
		`SELECT sp.supplier_product_id, soi.quantity 
		 FROM supplier_order_item soi
		 JOIN supplier_product sp ON soi.supplier_product_id = sp.id
		 WHERE soi.supplier_order_id = ?`,
		orderID,
	).Exec(ctx, &items)

	if len(items) == 0 {
		failDigiflazzOrder(dbClient, orderID, supplierOrder.InternalOrderID, "Resep / Item tidak ditemukan")
		return nil
	}

	// 6. Siapkan Service Digiflazz dengan Kredensial dari DB
	dfUsername, _ := supplierDF.Username()
	dfAPIKey, _ := supplierDF.Password()

	if dfUsername == "" || dfAPIKey == "" {
		failDigiflazzOrder(dbClient, orderID, supplierOrder.InternalOrderID, "Kredensial Digiflazz (Username/API Key) kosong di DB")
		return nil
	}

	dfService := digiflazz.NewDigiflazzService(dfUsername, dfAPIKey)

	// 7. Eksekusi API Transaksi (Looping jika pesanan campuran)
	// Kita gunakan internalOrder.BuyerUID sebagai nomor tujuan
	customerNo := internalOrder.BuyerUID 

	for i, item := range items {
		skuCode := item["supplier_product_id"].(string)

		// Parsing Quantity (jika tipe datanya float/int dari Prisma QueryRaw)
		var repeatCount int
		if qtyFloat, ok := item["quantity"].(float64); ok {
			repeatCount = int(qtyFloat)
		} else if qtyInt, ok := item["quantity"].(int64); ok {
			repeatCount = int(qtyInt)
		} else {
			repeatCount = 1
		}

		// Jika dalam 1 pesanan butuh 2x item yang sama (atau beda item),
		// Digiflazz WAJIB menerima ref_id yang berbeda. Jika sama, akan ditolak (Idempotency).
		for j := 0; j < repeatCount; j++ {
			
			// Format Ref ID = ID_Pesanan-UrutanItem-UrutanQty
			// Contoh: order123-1-1
			finalRefID := fmt.Sprintf("%s-%d-%d", internalOrder.ID, i+1, j+1)

			log.Printf("🛒 [Hit API] Tujuan: %s, SKU: %s, RefID: %s", customerNo, skuCode, finalRefID)

			result, errTopup := dfService.TopUp(skuCode, customerNo, finalRefID)
			
			if errTopup != nil {
				// Transaksi langsung GAGAL (Contoh: Saldo tidak cukup, Nomor tidak valid, Produk Gangguan)
				failDigiflazzOrder(dbClient, orderID, internalOrder.ID, fmt.Sprintf("Gagal di Provider (%s): %v", skuCode, errTopup))
				return nil 
			}

			// Transaksi sukses dilempar ke Digiflazz (Biasanya statusnya "Pending")
			status, _ := result["status"].(string)
			log.Printf("✅ Permintaan Diterima Digiflazz. Status: %s (Tunggu Webhook)", status)

			// Jeda 1 detik antar request agar server Digiflazz tidak menganggap kita melakukan SPAM
			time.Sleep(1 * time.Second)
		}
	}

	// 8. SELESAI. Biarkan status pesanan tetap 'processing'.
	// Status akhir (Sukses & mendapatkan Serial Number) akan diselesaikan otomatis 
	// oleh Handler Webhook yang akan kita pasang di langkah berikutnya.
	return nil
}

// Fungsi helper mandiri untuk menghindari bentrok nama dengan file worker lain
func failDigiflazzOrder(dbClient *db.PrismaClient, orderID, internalID, reason string) {
	log.Printf("❌ Digiflazz Order %s Failed: %s", orderID, reason)
	ctx := context.Background()
	dbClient.Prisma.ExecuteRaw("UPDATE supplier_order SET status='failed', last_error=? WHERE id=?", reason, orderID).Exec(ctx)
	dbClient.Prisma.ExecuteRaw("UPDATE internal_order SET status='failed' WHERE id=?", internalID).Exec(ctx)
}