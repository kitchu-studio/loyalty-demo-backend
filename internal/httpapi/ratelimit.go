package httpapi

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// limiter is a tiny in-memory sliding-window rate limiter keyed by string. It is
// enough to protect the paid AI endpoint and code guessing on a single server;
// behind several servers it would move to Redis.
type limiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newLimiter() *limiter { return &limiter{hits: map[string][]time.Time{}} }

func (l *limiter) allow(key string, n int, per time.Duration) bool {
	now := time.Now()
	cutoff := now.Add(-per)

	l.mu.Lock()
	defer l.mu.Unlock()

	recent := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) >= n {
		l.hits[key] = recent
		return false
	}
	l.hits[key] = append(recent, now)
	return true
}

// limit allows each signed-in user n requests per window for the named bucket.
// It must run after requireAuth.
func (s *Server) limit(bucket string, n int, per time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := bucket + ":" + strconv.FormatInt(c.GetInt64(ctxUserID), 10)
		if !s.limiter.allow(key, n, per) {
			abort(c, http.StatusTooManyRequests, "too many requests, please wait a moment")
			return
		}
		c.Next()
	}
}
