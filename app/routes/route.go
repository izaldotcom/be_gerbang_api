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
) {
	// Grouping v1
	v1 := e.Group("/api/v1")

	// ==========================================
	// A. PUBLIC ROUTES (Tanpa Token)
	// ==========================================
	v1.POST("/register", authHandler.RegisterUser)
	v1.POST("/login", authHandler.LoginUser)
	v1.POST("/refresh-token", authHandler.RefreshToken)

	// ==========================================
	// B. PROTECTED ROUTES (Butuh Bearer Token)
	// ==========================================
	protected := v1.Group("")
	protected.Use(mid.JWTMiddleware())

	// 1. Internal Products (CRUD)
	protected.POST("/products", productHandler.Create)
	protected.GET("/products", productHandler.GetAll)
	protected.PUT("/products/:id", productHandler.Update)
	protected.DELETE("/products/:id", productHandler.Delete)

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

	// 4. User Profile
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
	sellerGroup.GET("/status", func(c echo.Context) error {
		return c.JSON(200, echo.Map{"message": "Seller status endpoint"})
	})
}