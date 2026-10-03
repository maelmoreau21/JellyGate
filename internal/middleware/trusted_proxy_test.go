package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTrustedProxyRealIP(t *testing.T) {
	trusted := []string{"127.0.0.1/32", "10.0.0.0/8", "172.16.0.1/32"}
	mw := TrustedProxyRealIP(trusted)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Detected-Remote", r.RemoteAddr)
		w.WriteHeader(http.StatusOK)
	})

	handler := mw(testHandler)

	t.Run("ignores X-Forwarded-For when connection comes from untrusted peer", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "198.51.100.25:45678" // Untrusted public IP
		req.Header.Set("X-Forwarded-For", "203.0.113.195")
		req.Header.Set("X-Real-IP", "203.0.113.195")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		detected := rr.Header().Get("X-Detected-Remote")
		if detected != "198.51.100.25:45678" {
			t.Errorf("expected untrusted RemoteAddr to be preserved, got %s", detected)
		}
	})

	t.Run("extracts real client IP when connection comes from trusted proxy", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "172.16.0.1:45678" // Trusted proxy IP
		req.Header.Set("X-Forwarded-For", "203.0.113.195")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		detected := rr.Header().Get("X-Detected-Remote")
		if detected != "203.0.113.195:45678" {
			t.Errorf("expected extracted client IP from X-Forwarded-For, got %s", detected)
		}
	})

	t.Run("extracts real client IP from multi-hop X-Forwarded-For via trusted reverse proxy", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "127.0.0.1:5555" // Local trusted proxy
		// Chain: Client (203.0.113.50) -> Intermediate Trusted Proxy (10.0.0.5) -> Local Proxy (127.0.0.1)
		req.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.0.5")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		detected := rr.Header().Get("X-Detected-Remote")
		if detected != "203.0.113.50:5555" {
			t.Errorf("expected outermost non-trusted client IP 203.0.113.50:5555, got %s", detected)
		}
	})

	t.Run("falls back to X-Real-IP when from trusted proxy and X-Forwarded-For is missing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.1.2:1234" // Trusted proxy (in 10.0.0.0/8)
		req.Header.Set("X-Real-IP", "198.51.100.99")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		detected := rr.Header().Get("X-Detected-Remote")
		if detected != "198.51.100.99:1234" {
			t.Errorf("expected X-Real-IP 198.51.100.99:1234, got %s", detected)
		}
	})
}
