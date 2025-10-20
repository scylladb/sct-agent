package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware provides API key authentication middleware
func AuthMiddleware(apiKeys []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/health" {
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "Authorization header required",
				"message": "Please provide an Authorization header with Bearer token",
			})
			c.Abort()
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "Bearer token required",
				"message": "Authorization header must use Bearer token format",
			})
			c.Abort()
			return
		}

		token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "Empty token",
				"message": "Bearer token cannot be empty",
			})
			c.Abort()
			return
		}

		for _, key := range apiKeys {
			if token == key {
				c.Next()
				return
			}
		}

		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "Invalid API key",
			"message": "The provided API key is not valid",
		})
		c.Abort()
	}
}

// LoggingMiddleware provides request logging middleware
func LoggingMiddleware() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(p gin.LogFormatterParams) string {
		return fmt.Sprintf("[%s] %s %s %d %s %s\n",
			p.TimeStamp.Format("2006-01-02 15:04:05"),
			p.Method,
			p.Path,
			p.StatusCode,
			p.Latency,
			p.ClientIP,
		)
	})
}
