package handlers

import (
	"gerbangapi/app/services/scraper"
	"gerbangapi/prisma/db"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

type SupplierHandler struct {
	DB    *db.PrismaClient
	Redis *redis.Client
}

func NewSupplierHandler(dbClient *db.PrismaClient, redisClient *redis.Client) *SupplierHandler {
	return &SupplierHandler{DB: dbClient, Redis: redisClient}
}

func (h *SupplierHandler) Create(c echo.Context) error {
	type Req struct {
		Name     string `json:"name"`
		Code     string `json:"code"`
		Type     string `json:"type"`
		BaseURL  string `json:"base_url"`
		Username string `json:"username"`
		Password string `json:"password"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "Invalid request"})
	}

	supplier, err := h.DB.Supplier.CreateOne(
		db.Supplier.Name.Set(req.Name),
		db.Supplier.Type.Set(req.Type),
		db.Supplier.Code.Set(req.Code),
		db.Supplier.BaseURL.SetIfPresent(&req.BaseURL),
		db.Supplier.Username.SetIfPresent(&req.Username),
		db.Supplier.Password.SetIfPresent(&req.Password),
		db.Supplier.Status.Set(true),
	).Exec(c.Request().Context())

	if err != nil {
		return c.JSON(500, echo.Map{"error": err.Error()})
	}

	return c.JSON(201, echo.Map{"message": "Supplier created", "data": supplier})
}

func (h *SupplierHandler) GetAll(c echo.Context) error {
	suppliers, err := h.DB.Supplier.FindMany().Exec(c.Request().Context())
	if err != nil {
		return c.JSON(500, echo.Map{"error": err.Error()})
	}
	return c.JSON(200, echo.Map{"data": suppliers})
}

func (h *SupplierHandler) Update(c echo.Context) error {
	id := c.Param("id")
	type Req struct {
		Name     string `json:"name"`
		Code     string `json:"code"`
		Type     string `json:"type"`
		BaseURL  string `json:"base_url"`
		Username string `json:"username"`
		Password string `json:"password"`
		Status   *bool  `json:"status"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "Invalid request"})
	}

	var updates []db.SupplierSetParam
	if req.Name != "" {
		updates = append(updates, db.Supplier.Name.Set(req.Name))
	}
	if req.Code != "" {
		updates = append(updates, db.Supplier.Code.Set(req.Code))
	}
	if req.Type != "" {
		updates = append(updates, db.Supplier.Type.Set(req.Type))
	}
	if req.BaseURL != "" {
		updates = append(updates, db.Supplier.BaseURL.Set(req.BaseURL))
	}
	if req.Username != "" {
		updates = append(updates, db.Supplier.Username.Set(req.Username))
	}
	if req.Password != "" {
		updates = append(updates, db.Supplier.Password.Set(req.Password))
	}
	if req.Status != nil {
		updates = append(updates, db.Supplier.Status.Set(*req.Status))
	}

	supplier, err := h.DB.Supplier.FindUnique(
		db.Supplier.ID.Equals(id),
	).Update(updates...).Exec(c.Request().Context())

	if err != nil {
		return c.JSON(500, echo.Map{"error": err.Error()})
	}

	return c.JSON(200, echo.Map{"message": "Updated", "data": supplier})
}

func (h *SupplierHandler) Delete(c echo.Context) error {
	id := c.Param("id")
	_, err := h.DB.Supplier.FindUnique(db.Supplier.ID.Equals(id)).Delete().Exec(c.Request().Context())
	if err != nil {
		return c.JSON(500, echo.Map{"error": err.Error()})
	}
	return c.JSON(200, echo.Map{"message": "Deleted"})
}

func (h *SupplierHandler) CheckConnection(c echo.Context) error {
	type Req struct {
		SupplierID string `json:"supplier_id"`
		Username   string `json:"username"`
		Password   string `json:"password"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "Invalid request"})
	}

	username := req.Username
	password := req.Password
	supplierName := "New/Unregistered Supplier"
	supplierID := req.SupplierID

	if supplierID != "" {
		supplier, err := h.DB.Supplier.FindUnique(
			db.Supplier.ID.Equals(supplierID),
		).Exec(c.Request().Context())

		if err != nil {
			return c.JSON(404, echo.Map{"error": "Supplier tidak ditemukan di database"})
		}
		
		supplierName = supplier.Name

		if username == "" {
			valUser, _ := supplier.Username()
			username = valUser
		}
		if password == "" {
			valPass, _ := supplier.Password()
			password = valPass
		}
	}

	if username == "" || password == "" {
		return c.JSON(400, echo.Map{"error": "Username dan password tidak boleh kosong (isi di JSON atau pastikan sudah tersimpan di DB)"})
	}

	svc, err := scraper.NewMitraHiggsService(false, h.Redis)
	if err != nil {
		return c.JSON(500, echo.Map{"error": "Gagal memulai service browser", "details": err.Error()})
	}
	defer svc.Close()

	identityData := echo.Map{
		"supplier_id":   supplierID,
		"supplier_name": supplierName,
		"username":      username,
	}

	err = svc.Login(username, password)
	if err != nil {
		return c.JSON(401, echo.Map{
			"status":  "FAILED",
			"message": "Koneksi ke supplier gagal",
			"details": err.Error(),
			"data":    identityData,
		})
	}

	return c.JSON(200, echo.Map{
		"status":  "SUCCESS",
		"message": "Login berhasil, supplier telah terhubung!",
		"data":    identityData,
	})
}