package main

import (
	"log"

	"gerbangapi/app/handlers"
	mid "gerbangapi/app/middleware"
	"gerbangapi/prisma/db"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
    e := echo.New()
    e.Use(middleware.Logger())
    e.Use(middleware.Recover())

    // 1️⃣ Connect Prisma client
    client := db.NewClient()
    if err := client.Prisma.Connect(); err != nil {
        log.Fatal("❌ Prisma failed to connect:", err)
    }
    log.Println("✅ Prisma connected successfully!")

    // 2️⃣ Inject client ke handlers
    handlers.SetPrismaClient(client)

    // 3️⃣ Routes
    e.POST("/register", handlers.RegisterUser)
    e.POST("/login", handlers.LoginUser)

    api := e.Group("/api")
    api.Use(mid.JWTMiddleware())

    api.GET("/profile", func(c echo.Context) error {
        return c.JSON(200, echo.Map{
            "user_id": c.Get("user_id"),
            "email":   c.Get("email"),
        })
    })

    // 4️⃣ Start server
    e.Logger.Fatal(e.Start(":8080"))
}
