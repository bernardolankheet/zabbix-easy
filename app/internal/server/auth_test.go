package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAPIKeyMiddleware_NoKeyConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	os.Unsetenv("APP_API_KEY")
	r := gin.New()
	r.Use(APIKeyMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestAPIKeyMiddleware_InvalidKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	os.Setenv("APP_API_KEY", "secret")
	defer os.Unsetenv("APP_API_KEY")

	r := gin.New()
	r.Use(APIKeyMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAPIKeyMiddleware_ValidKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	os.Setenv("APP_API_KEY", "secret")
	defer os.Unsetenv("APP_API_KEY")

	r := gin.New()
	r.Use(APIKeyMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-API-Key", "secret")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
