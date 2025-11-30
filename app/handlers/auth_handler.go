package handlers

import (
	"context"
	"gerbangapi/app/services"
	"gerbangapi/prisma/db"

	"github.com/labstack/echo/v4"
)

var client *db.PrismaClient

func SetPrismaClient(c *db.PrismaClient) {
	client = c
}

func RegisterUser(c echo.Context) error {
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
		return c.JSON(400, echo.Map{"error": "invalid request"})
	}

	err := services.Register(
		context.Background(),
		client,
		req.ID, req.RoleID, req.Name,
		req.Email, req.Phone, req.Status, req.Password,
	)

	if err != nil {
		return c.JSON(400, echo.Map{"error": err.Error()})
	}

	return c.JSON(200, echo.Map{"message": "register success"})
}

func LoginUser(c echo.Context) error {
	type Req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "invalid request"})
	}

	// Token sekarang berupa Struct (Access & Refresh)
	tokenResp, err := services.Login(context.Background(), client, req.Email, req.Password)
	if err != nil {
		return c.JSON(401, echo.Map{"error": err.Error()})
	}

	return c.JSON(200, echo.Map{
		"message":       "login success",
		"access_token":  tokenResp.AccessToken,
		"refresh_token": tokenResp.RefreshToken,
	})
}

// 👇 INI YANG KEMARIN BELUM ADA
func RefreshToken(c echo.Context) error {
	type Req struct {
		RefreshToken string `json:"refresh_token"`
	}

	req := new(Req)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, echo.Map{"error": "invalid request"})
	}

	// Panggil service RefreshTokenProcess
	newAccessToken, err := services.RefreshTokenProcess(context.Background(), client, req.RefreshToken)
	if err != nil {
		return c.JSON(401, echo.Map{"error": err.Error()})
	}

	return c.JSON(200, echo.Map{
		"access_token": newAccessToken,
	})
}