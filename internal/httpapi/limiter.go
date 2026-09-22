package httpapi

import (
	"net"
	"net/http"
	"sync"
	"time"

	"neighborparking/internal/platform"
)

type rateBucket struct {
	count int
	reset time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]rateBucket
}

func newRateLimiter() *rateLimiter { return &rateLimiter{buckets: make(map[string]rateBucket)} }

func (l *rateLimiter) allow(key string, limit int, window time.Duration) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b.reset.Before(now) {
		b = rateBucket{reset: now.Add(window)}
	}
	b.count++
	l.buckets[key] = b
	if len(l.buckets) > 10000 {
		for k, candidate := range l.buckets {
			if candidate.reset.Before(now) {
				delete(l.buckets, k)
			}
		}
	}
	return b.count <= limit
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *Server) limited(pattern string, limit int, window time.Duration, h handler) {
	s.handle(pattern, func(w http.ResponseWriter, r *http.Request) error {
		if !s.limiter.allow(pattern+":"+clientIP(r), limit, window) {
			w.Header().Set("Retry-After", "60")
			return platform.E(429, "RATE_LIMITED", "Too many requests. Please wait and try again.")
		}
		return h(w, r)
	})
}
