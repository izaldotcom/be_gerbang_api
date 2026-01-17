package handlers

import (
	"fmt"
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
		RoleID     string `json:"role_id"`
		Name       string `json:"name"`
		Email      string `json:"email"`
		Phone      string `json:"phone"`
		WebhookURL string `json:"webhook_url"` // [BARU] Opsional
		Status     string `json:"status"`
		Password   string `json:"password"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request format"})
	}

	if req.Email == "" || req.Password == "" || req.Name == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Name, Email, and Password are required"})
	}

	err := h.Service.Register(c.Request().Context(), services.RegisterInput{
		RoleID:     req.RoleID,
		Name:       req.Name,
		Email:      req.Email,
		Phone:      req.Phone,
		WebhookURL: req.WebhookURL, // [BARU] Pass ke service
		Status:     req.Status,
		Password:   req.Password,
	})

	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Registration failed: " + err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{
		"message": "Registration successful. Please wait for admin approval.",
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
		Email      string `json:"email"`
		Password   string `json:"password"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid request payload"})
	}

	finalIdentifier := req.Identifier
	if finalIdentifier == "" {
		finalIdentifier = req.Email
	}

	if finalIdentifier == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Identifier (Email/Phone) is required"})
	}

	tokenResp, err := h.Service.Login(c.Request().Context(), services.LoginInput{
		Identifier: finalIdentifier,
		Password:   req.Password,
	})

	if err != nil {
		// Pesan error dari service sudah spesifik (reject/register/invalid)
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
// 4. GET CURRENT USER (ME)
// ==========================================
func (h *AuthHandler) Me(c echo.Context) error {
	userID, ok := c.Get("user_id").(string)
	if !ok || userID == "" {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Unauthorized"})
	}

	session, err := h.Service.GetSession(c.Request().Context(), userID)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "Session expired or invalid"})
	}

	return c.JSON(http.StatusOK, echo.Map{
		"data": session,
	})
}

// ==========================================
// 5. [BARU] VERIFY USER (ADMIN ONLY)
// ==========================================
func (h *AuthHandler) VerifyUser(c echo.Context) error {
	type Req struct {
		UserID string `json:"user_id"`
		Action string `json:"action"` // "approve" or "reject"
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid JSON"})
	}

	if req.UserID == "" || req.Action == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "user_id and action are required"})
	}

	newStatus, err := h.Service.VerifyUser(c.Request().Context(), req.UserID, req.Action)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{
		"message":    "User status updated successfully",
		"user_id":    req.UserID,
		"new_status": newStatus,
	})
}

// ==========================================
// 6. GET ALL USERS (ADMIN ONLY)
// ==========================================
func (h *AuthHandler) GetUsers(c echo.Context) error {
	// Panggil Service
	users, err := h.Service.GetAllUsers(c.Request().Context())
	
	if err != nil {
		// [DEBUG] Print error asli ke Terminal agar tahu penyebabnya
		fmt.Println("❌ Error GetUsers:", err) 
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Database Error: " + err.Error()})
	}

	// Mapping Response
	var response []map[string]interface{}

	for _, u := range users {
		roleName := "-"
		roleID := "-"
		
		// Gunakan pengecekan aman (ok) untuk relasi
		if r, ok := u.Role(); ok {
			roleName = r.Name
			roleID = r.ID
		}

		phoneVal := ""
		if v, ok := u.Phone(); ok { phoneVal = v }

		statusVal := ""
		if v, ok := u.Status(); ok { statusVal = v }

		response = append(response, map[string]interface{}{
			"id":         u.ID,
			"name":       u.Name,
			"email":      u.Email,
			"phone":      phoneVal,
			"status":     statusVal,
			"role_id":    roleID,
			"role_name":  roleName,
			"created_at": u.CreatedAt,
		})
	}

	return c.JSON(http.StatusOK, echo.Map{
		"message": "success",
		"data":    response,
	})
}