package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

var rl = struct {
    mu   sync.Mutex
    hits map[string]int
}{
    hits: make(map[string]int),
}

func RateLimit(limit int, window time.Duration) echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {

            key := c.RealIP()

            rl.mu.Lock()
            rl.hits[key]++
            count := rl.hits[key]
            rl.mu.Unlock()

            if count == 1 {
                go func() {
                    time.Sleep(window)
                    rl.mu.Lock()
                    rl.hits[key] = 0
                    rl.mu.Unlock()
                }()
            }

            if count > limit {
                return c.JSON(http.StatusTooManyRequests,
                    echo.Map{"error": "rate limit exceeded"})
            }

            return next(c)
        }
    }
}
