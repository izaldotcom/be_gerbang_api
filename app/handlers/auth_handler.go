package handlers

import (
	"gerbangapi/app/services"
	"net/http"

	"github.com/labstack/echo/v4"
)

type AuthHandler struct {
	Service *services.AuthService
}

func NewAuthHandler(service *services.AuthService) *AuthHandler {
	return &AuthHandler{Service: service}
}

// ==========================================
// 1. REGISTER USER
// ==========================================
func (h *AuthHandler) RegisterUser(c echo.Context) error {
	type Req struct {
		RoleID   string `json:"role_id"`
		Name     string `json:"name"`
		Email    string `json:"email"`
		Phone    string `json:"phone"`
		Status   string `json:"status"`
		Password string `json:"password"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request format"})
	}

	// Validasi Input Dasar
	if req.Email == "" || req.Password == "" || req.Name == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Name, Email, and Password are required"})
	}

	// Panggil Service Register
	err := h.Service.Register(c.Request().Context(), services.RegisterInput{
		RoleID:   req.RoleID,
		Name:     req.Name,
		Email:    req.Email,
		Phone:    req.Phone,
		Status:   req.Status,
		Password: req.Password,
	})

	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Registration failed: " + err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{
		"message": "User registered successfully",
		"data": echo.Map{
			"email": req.Email,
			"name":  req.Name,
		},
	})
}

// ==========================================
// 2. LOGIN USER
// ==========================================
func (h *AuthHandler) LoginUser(c echo.Context) error {
	type Req struct {
		Identifier string `json:"identifier"`
		Email      string `json:"email"` // Fallback for backward compatibility
		Password   string `json:"password"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request payload"})
	}

	// Logic Identifier (Priority: identifier > email)
	finalIdentifier := req.Identifier
	if finalIdentifier == "" {
		finalIdentifier = req.Email
	}

	if finalIdentifier == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Identifier (Email/Phone) is required"})
	}

	// Panggil Service Login
	tokenResp, err := h.Service.Login(c.Request().Context(), services.LoginInput{
		Identifier: finalIdentifier,
		Password:   req.Password,
	})

	if err != nil {
		// Return 401 Unauthorized
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{
		"message":       "login success",
		"access_token":  tokenResp.AccessToken,
		"refresh_token": tokenResp.RefreshToken,
		"user":          tokenResp.User,
	})
}

// ==========================================
// 3. REFRESH TOKEN
// ==========================================
func (h *AuthHandler) RefreshToken(c echo.Context) error {
	type Req struct {
		RefreshToken string `json:"refresh_token"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid JSON"})
	}

	newAccessToken, err := h.Service.RefreshTokenProcess(c.Request().Context(), req.RefreshToken)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{
		"access_token": newAccessToken,
	})
}

// ==========================================
// 4. GET CURRENT USER (ME) - VIA REDIS
// ==========================================
func (h *AuthHandler) Me(c echo.Context) error {
	// Ambil UserID dari Context (diset oleh Middleware JWT)
	userID, ok := c.Get("user_id").(string)
	if !ok || userID == "" {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Unauthorized"})
	}

	// Ambil session dari Redis
	session, err := h.Service.GetSession(c.Request().Context(), userID)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Session expired or invalid"})
	}

	return c.JSON(http.StatusOK, echo.Map{
		"data": session,
	})
}