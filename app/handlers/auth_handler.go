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

    token, err := services.Login(context.Background(), client, req.Email, req.Password)
    if err != nil {
        return c.JSON(401, echo.Map{"error": err.Error()})
    }

    return c.JSON(200, echo.Map{
        "message": "login success",
        "token":   token,
    })
}
