package server

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type rateBucket struct {
	count   int
	window  time.Time
}

var (
	rateMu      sync.Mutex
	rateBuckets = map[string]*rateBucket{}
)

func rateLimitPerMinute() int {
	v := strings.TrimSpace(os.Getenv("RATE_LIMIT_START_RPM"))
	if v == "" {
		return 20
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 20
	}
	return n
}

// StartRateLimitMiddleware limits POST /api/start requests per client IP per minute.
func StartRateLimitMiddleware() gin.HandlerFunc {
	limit := rateLimitPerMinute()
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || c.Request.URL.Path != "/api/start" {
			c.Next()
			return
		}
		key := strings.TrimSpace(c.ClientIP())
		if key == "" {
			key = "unknown"
		}
		now := time.Now()
		rateMu.Lock()
		b, ok := rateBuckets[key]
		if !ok || now.Sub(b.window) >= time.Minute {
			b = &rateBucket{count: 0, window: now}
			rateBuckets[key] = b
		}
		b.count++
		allowed := b.count <= limit
		retryAfter := int(time.Minute - now.Sub(b.window))
		if retryAfter < 1 {
			retryAfter = 1
		}
		rateMu.Unlock()
		if !allowed {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate limit exceeded",
				"retry_after": retryAfter,
			})
			return
		}
		c.Next()
	}
}
