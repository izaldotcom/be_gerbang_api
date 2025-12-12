package handlers

import (
	"net/http"

	"gerbangapi/app/services"

	"github.com/labstack/echo/v4"
)

type AuthHandler struct {
	Service *services.AuthService
}

// ✅ Constructor (Dipanggil di main.go)
func NewAuthHandler(service *services.AuthService) *AuthHandler {
	return &AuthHandler{
		Service: service,
	}
}

// --- REGISTER ---
func (h *AuthHandler) RegisterUser(c echo.Context) error {
	// Request Struct sesuai kebutuhan input JSON
	type Req struct {
		ID       string `json:"id"`
		RoleID   string `json:"role_id"`
		Name     string `json:"name"`
		Email    string `json:"email"`
		Phone    string `json:"phone"`
		Status   string `json:"status"`
		Password string `json:"password"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request payload"})
	}

	// Mapping dari Req Handler -> Input Service Struct
	input := services.RegisterInput{
		ID:       req.ID,
		RoleID:   req.RoleID,
		Name:     req.Name,
		Email:    req.Email,
		Phone:    req.Phone,
		Status:   req.Status,
		Password: req.Password,
	}

	// Panggil Service (Sekarang menggunakan Method, bukan fungsi static)
	err := h.Service.Register(c.Request().Context(), input)

	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, echo.Map{"message": "register success"})
}

// --- LOGIN ---
func (h *AuthHandler) LoginUser(c echo.Context) error {
	type Req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request payload"})
	}

	// Panggil Service Login
	tokenResp, err := h.Service.Login(c.Request().Context(), services.LoginInput{
		Email:    req.Email,
		Password: req.Password,
	})

	if err != nil {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{
		"message":       "login success",
		"access_token":  tokenResp.AccessToken,
		"refresh_token": tokenResp.RefreshToken,
	})
}

// --- REFRESH TOKEN ---
// Sekarang menjadi Method agar bisa akses DB via Service
func (h *AuthHandler) RefreshToken(c echo.Context) error {
	type Req struct {
		RefreshToken string `json:"refresh_token"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request"})
	}

	// Panggil Service RefreshTokenProcess
	newAccessToken, err := h.Service.RefreshTokenProcess(c.Request().Context(), req.RefreshToken)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{
		"access_token": newAccessToken,
	})
}