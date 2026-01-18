package routes

import (
	"gerbangapi/app/handlers"
	mid "gerbangapi/app/middleware"
	"gerbangapi/prisma/db"

	"github.com/labstack/echo/v4"
)

// Init mendaftarkan semua route API
func Init(
	e *echo.Echo,
	dbClient *db.PrismaClient,
	authHandler *handlers.AuthHandler,
	sellerHandler *handlers.SellerHandler,
	supplierHandler *handlers.SupplierHandler,
	supplierProductHandler *handlers.SupplierProductHandler,
	productHandler *handlers.ProductHandler,
	recipeHandler *handlers.RecipeHandler,
	telegramHandler *handlers.TelegramHandler, // Tambahan TelegramHandler
) {
	// Grouping v1
	v1 := e.Group("/api/v1")

	// ==========================================
	// A. PUBLIC ROUTES (Tanpa Token)
	// ==========================================
	v1.POST("/register", authHandler.RegisterUser)
	v1.POST("/login", authHandler.LoginUser)
	v1.POST("/refresh-token", authHandler.RefreshToken)
	
	// Note: Verify & GetUsers sebaiknya diproteksi middleware admin kedepannya
	v1.POST("/verify", authHandler.VerifyUser) 
	v1.GET("/users", authHandler.GetUsers) 

	// [BARU] Route untuk Telegram Webhook
	v1.POST("/webhook/telegram", telegramHandler.HandleWebhook)

	// ==========================================
	// B. PROTECTED ROUTES (Butuh Bearer Token)
	// ==========================================
	protected := v1.Group("")
	protected.Use(mid.JWTMiddleware())

	// --- 1. User & Auth Management ---
	protected.GET("/auth/me", authHandler.Me)

	// [BARU] Delete User (Admin Only - via query param ?id=...)
	protected.DELETE("/users", authHandler.DeleteUser)

	// --- 3. Internal Products (CRUD) ---
	protected.POST("/products", productHandler.Create)
	protected.GET("/products", productHandler.GetAll)
	protected.PUT("/products", productHandler.Update)
	protected.DELETE("/products", productHandler.Delete)

	// --- 4. Suppliers (CRUD) ---
	protected.POST("/suppliers", supplierHandler.Create)
	protected.GET("/suppliers", supplierHandler.GetAll)
	protected.PUT("/suppliers/:id", supplierHandler.Update)
	protected.DELETE("/suppliers/:id", supplierHandler.Delete)

	// --- 5. Supplier Products (CRUD) ---
	protected.POST("/supplier-products", supplierProductHandler.Create)
	protected.GET("/supplier-products", supplierProductHandler.GetAll)
	protected.PUT("/supplier-products/:id", supplierProductHandler.Update)
	protected.DELETE("/supplier-products/:id", supplierProductHandler.Delete)

	// --- 6. Recipe Items (CRUD DETAIL) ---
	protected.POST("/recipes", recipeHandler.Create)
	protected.GET("/recipes", recipeHandler.GetAll)
	protected.GET("/recipes/:id", recipeHandler.GetByID)
	protected.PUT("/recipes/replace", recipeHandler.ReplaceAll)
	protected.PUT("/recipes/:id", recipeHandler.UpdateItem)
	protected.PUT("/recipes", recipeHandler.UpdateItem)
	protected.DELETE("/recipes/:id", recipeHandler.Delete)

	// ==========================================
	// C. SELLER ROUTES (Butuh API KEY)
	// ==========================================
	sellerGroup := v1.Group("/seller")
	sellerGroup.Use(mid.SellerSecurityMiddleware(dbClient))
	sellerGroup.GET("/profile", sellerHandler.GetProfile)
	sellerGroup.PUT("/profile", sellerHandler.UpdateProfile)
	sellerGroup.GET("/products", sellerHandler.SellerProducts)
	sellerGroup.POST("/order", sellerHandler.SellerOrder)
	sellerGroup.GET("/order/history", sellerHandler.HistoryOrder)
	
	sellerGroup.GET("/status", func(c echo.Context) error {
		return c.JSON(200, echo.Map{"message": "Seller status endpoint"})
	})
}