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
	recipeHandler *handlers.RecipeHandler, // <--- (BARU) Terima parameter ini
) {
	// Grouping v1
	v1 := e.Group("/api/v1")

	// ==========================================
	// A. PUBLIC ROUTES (Tanpa Token)
	// ==========================================
	v1.POST("/register", authHandler.RegisterUser)
	v1.POST("/login", authHandler.LoginUser)
	v1.POST("/refresh-token", authHandler.RefreshToken)
	v1.POST("/verify", authHandler.VerifyUser)
	v1.GET("/users", authHandler.GetUsers)
	// ==========================================
	// B. PROTECTED ROUTES (Butuh Bearer Token)
	// ==========================================
	protected := v1.Group("")
	protected.Use(mid.JWTMiddleware())

	// [BARU] Endpoint Auth Me
	protected.GET("/auth/me", authHandler.Me)

	// 1. Internal Products (CRUD)
	protected.POST("/products", productHandler.Create)
	protected.GET("/products", productHandler.GetAll)
	protected.PUT("/products", productHandler.Update)
	protected.DELETE("/products", productHandler.Delete)

	// 2. Suppliers (CRUD)
	protected.POST("/suppliers", supplierHandler.Create)
	protected.GET("/suppliers", supplierHandler.GetAll)
	protected.PUT("/suppliers/:id", supplierHandler.Update)
	protected.DELETE("/suppliers/:id", supplierHandler.Delete)

	// 3. Supplier Products (CRUD)
	protected.POST("/supplier-products", supplierProductHandler.Create)
	protected.GET("/supplier-products", supplierProductHandler.GetAll)
	protected.PUT("/supplier-products/:id", supplierProductHandler.Update)
	protected.DELETE("/supplier-products/:id", supplierProductHandler.Delete)

	// 4. Recipe Items (CRUD DETAIL) <--- (BARU)
	protected.POST("/recipes", recipeHandler.Create)
	protected.GET("/recipes", recipeHandler.GetAll)
	protected.GET("/recipes/:id", recipeHandler.GetByID)
	protected.PUT("/recipes/replace", recipeHandler.ReplaceAll)
	protected.PUT("/recipes/:id", recipeHandler.UpdateItem)
	protected.PUT("/recipes", recipeHandler.UpdateItem)
	protected.DELETE("/recipes/:id", recipeHandler.Delete)

	// 5. User Profile
	protected.GET("/get-profile", func(c echo.Context) error {
		return c.JSON(200, echo.Map{
			"user_id": c.Get("user_id"),
			"email":   c.Get("email"),
			"message": "You are accessing a protected route",
		})
	})

	// ==========================================
	// C. SELLER ROUTES (Butuh API KEY)
	// ==========================================
	sellerGroup := v1.Group("/seller")
	sellerGroup.Use(mid.SellerSecurityMiddleware(dbClient))

	sellerGroup.GET("/products", sellerHandler.SellerProducts)
	sellerGroup.POST("/order", sellerHandler.SellerOrder)
	sellerGroup.GET("/order/history", sellerHandler.HistoryOrder)
	sellerGroup.GET("/status", func(c echo.Context) error {
		return c.JSON(200, echo.Map{"message": "Seller status endpoint"})
	})
}