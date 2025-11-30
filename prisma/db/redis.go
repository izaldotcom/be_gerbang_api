package db

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

var Rdb *redis.Client

func ConnectRedis() {
	Rdb = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379", // Pastikan Redis sudah jalan
		Password: "",               // No password set
		DB:       0,                // Use default DB
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := Rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatal("❌ Gagal connect Redis:", err)
	}
	log.Println("✅ Redis connected successfully!")
}