package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStartRateLimitMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	os.Setenv("RATE_LIMIT_START_RPM", "2")
	defer os.Unsetenv("RATE_LIMIT_START_RPM")
	rateBuckets = map[string]*rateBucket{}

	r := gin.New()
	r.POST("/api/start", StartRateLimitMiddleware(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/start", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/start", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
}
