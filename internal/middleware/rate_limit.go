package middleware

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type rateBucket struct {
	Count    int
	ResetAt  time.Time
	LastSeen time.Time
}

type inMemoryRateLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	limit   int
	buckets map[string]*rateBucket
	stopCh  chan struct{}
}

func newInMemoryRateLimiter(limit int, window time.Duration) *inMemoryRateLimiter {
	limiter := &inMemoryRateLimiter{
		window:  window,
		limit:   limit,
		buckets: make(map[string]*rateBucket),
		stopCh:  make(chan struct{}),
	}
	go limiter.startCleanupLoop()
	return limiter
}

func (l *inMemoryRateLimiter) startCleanupLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			l.mu.Lock()
			for key, b := range l.buckets {
				if now.After(b.ResetAt.Add(l.window)) {
					delete(l.buckets, key)
				}
			}
			l.mu.Unlock()
		case <-l.stopCh:
			return
		}
	}
}

// Stop terminates the cleanup goroutine.
func (l *inMemoryRateLimiter) Stop() {
	select {
	case <-l.stopCh:
		// already stopped
	default:
		close(l.stopCh)
	}
}

func (l *inMemoryRateLimiter) allow(key string, now time.Time) (bool, int, time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, exists := l.buckets[key]
	if !exists || now.After(b.ResetAt) {
		b = &rateBucket{Count: 0, ResetAt: now.Add(l.window)}
		l.buckets[key] = b
	}

	b.LastSeen = now
	if b.Count >= l.limit {
		remaining := int(b.ResetAt.Sub(now).Seconds())
		if remaining < 0 {
			remaining = 0
		}
		return false, 0, b.ResetAt
	}

	b.Count++
	return true, l.limit - b.Count, b.ResetAt
}

// RateLimitByIP applique une limitation simple par IP pour les routes sensibles.
func RateLimitByIP(limit int, window time.Duration) func(http.Handler) http.Handler {
	limiter := newInMemoryRateLimiter(limit, window)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			allowed, remaining, resetAt := limiter.allow(ip+"|"+r.URL.Path, time.Now().UTC())

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))

			if !allowed {
				w.Header().Set("Retry-After", strconv.Itoa(int(time.Until(resetAt).Seconds())))
				http.Error(w, "Trop de requetes, reessayez plus tard", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	if r == nil {
		return "unknown"
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}

	if strings.TrimSpace(r.RemoteAddr) != "" {
		return strings.TrimSpace(r.RemoteAddr)
	}

	return "unknown"
}
