package middleware

import (
    "net/http"
    "os"

    "github.com/golang-jwt/jwt/v5"
    "github.com/labstack/echo/v4"
)

func JWTMiddleware() echo.MiddlewareFunc {
    secret:=[]byte(os.Getenv("JWT_SECRET"))
    return func(next echo.HandlerFunc) echo.HandlerFunc{
        return func(c echo.Context)error{
            auth:=c.Request().Header.Get("Authorization")
            if auth==""{
                return c.JSON(http.StatusUnauthorized,echo.Map{"error":"missing token"})
            }
            tokenStr:=auth[len("Bearer "):]
            token,err:=jwt.Parse(tokenStr,func(t *jwt.Token)(interface{},error){
                return secret,nil
            })
            if err!=nil || !token.Valid{
                return c.JSON(http.StatusUnauthorized,echo.Map{"error":"invalid token"})
            }
            claims:=token.Claims.(jwt.MapClaims)
            c.Set("user_id",claims["user_id"])
            c.Set("email",claims["email"])
            return next(c)
        }
    }
}
