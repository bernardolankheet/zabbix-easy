package server

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// APIKeyRequired reports whether APP_API_KEY is configured.
func APIKeyRequired() bool {
	return strings.TrimSpace(os.Getenv("APP_API_KEY")) != ""
}

// APIKeyMiddleware protects mutating endpoints when APP_API_KEY is set.
func APIKeyMiddleware() gin.HandlerFunc {
	expected := strings.TrimSpace(os.Getenv("APP_API_KEY"))
	return func(c *gin.Context) {
		if expected == "" {
			c.Next()
			return
		}
		got := strings.TrimSpace(c.GetHeader("X-API-Key"))
		if got == "" {
			got = strings.TrimSpace(c.Query("api_key"))
		}
		if got != expected {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing API key"})
			return
		}
		c.Next()
	}
}
