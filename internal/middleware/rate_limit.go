package middleware

import (
	"container/list"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// rateBucket stocke le compteur de requêtes et les métadonnées temporelles pour une clé donnée.
type rateBucket struct {
	Count   int
	ResetAt time.Time
}

// lruEntry est un enregistrement dans la cache LRU.
type lruEntry struct {
	key     string
	bucket  *rateBucket
	element *list.Element
}

// inMemoryRateLimiter est un rate limiter par IP avec éviction LRU.
// L'éviction LRU remplace le reset brutal de la map (qui était exploitable via 20 000 IPs
// pour réinitialiser les compteurs de toutes les IP légitimes).
type inMemoryRateLimiter struct {
	mu       sync.Mutex
	stopOnce sync.Once
	window   time.Duration
	limit    int
	maxKeys  int
	items    map[string]*list.Element
	lruList  *list.List
	stopCh   chan struct{}
}

func newInMemoryRateLimiter(limit int, window time.Duration) *inMemoryRateLimiter {
	const defaultMaxKeys = 20000
	limiter := &inMemoryRateLimiter{
		window:  window,
		limit:   limit,
		maxKeys: defaultMaxKeys,
		items:   make(map[string]*list.Element, 512),
		lruList: list.New(),
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
			// Supprimer uniquement les buckets dont la fenêtre de reset est expirée.
			// L'éviction LRU gère les cas où maxKeys est atteint (voir allow()).
			for e := l.lruList.Front(); e != nil; {
				next := e.Next()
				entry := e.Value.(*lruEntry)
				if now.After(entry.bucket.ResetAt.Add(l.window)) {
					l.lruList.Remove(e)
					delete(l.items, entry.key)
				}
				e = next
			}
			l.mu.Unlock()
		case <-l.stopCh:
			return
		}
	}
}

// Stop termine la goroutine de nettoyage de manière concurrente et idempotente.
func (l *inMemoryRateLimiter) Stop() {
	l.stopOnce.Do(func() {
		close(l.stopCh)
	})
}

func (l *inMemoryRateLimiter) allow(key string, now time.Time) (bool, int, time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if elem, exists := l.items[key]; exists {
		entry := elem.Value.(*lruEntry)
		if now.After(entry.bucket.ResetAt) {
			// Fenêtre expirée : réinitialiser le compteur
			entry.bucket.Count = 0
			entry.bucket.ResetAt = now.Add(l.window)
		}
		// Marquer comme récemment utilisé (déplacer en fin de liste)
		l.lruList.MoveToBack(elem)
		entry.bucket.Count++
		if entry.bucket.Count > l.limit {
			return false, 0, entry.bucket.ResetAt
		}
		return true, l.limit - entry.bucket.Count, entry.bucket.ResetAt
	}

	// Nouvelle entrée : vérifier si on dépasse maxKeys → éviction LRU (plus ancien = front)
	if len(l.items) >= l.maxKeys {
		// Supprimer l'entrée la moins récemment utilisée
		oldest := l.lruList.Front()
		if oldest != nil {
			oldEntry := oldest.Value.(*lruEntry)
			delete(l.items, oldEntry.key)
			l.lruList.Remove(oldest)
		}
	}

	bucket := &rateBucket{Count: 1, ResetAt: now.Add(l.window)}
	entry := &lruEntry{key: key, bucket: bucket}
	elem := l.lruList.PushBack(entry)
	entry.element = elem
	l.items[key] = elem

	return true, l.limit - 1, bucket.ResetAt
}

// RateLimitByIP applique une limitation simple par IP pour les routes sensibles.
func RateLimitByIP(limit int, window time.Duration) func(http.Handler) http.Handler {
	limiter := newInMemoryRateLimiter(limit, window)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			key := fmt.Sprintf("%s|%s", ip, r.URL.Path)
			allowed, remaining, resetAt := limiter.allow(key, time.Now().UTC())

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))

			if !allowed {
				retrySec := int(time.Until(resetAt).Seconds())
				if retrySec < 1 {
					retrySec = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retrySec))
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
