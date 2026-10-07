package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders_Directives(t *testing.T) {
	mw := SecurityHeaders("https://jellygate.example.com")

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %s, want nosniff", got)
	}
	if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %s, want DENY", got)
	}
	if got := rr.Header().Get("X-XSS-Protection"); got != "0" {
		t.Errorf("X-XSS-Protection = %s, want 0", got)
	}
	if got := rr.Header().Get("Strict-Transport-Security"); !strings.Contains(got, "preload") {
		t.Errorf("Strict-Transport-Security = %s, want preload directive", got)
	}
	if got := rr.Header().Get("Permissions-Policy"); !strings.Contains(got, "payment=()") {
		t.Errorf("Permissions-Policy = %s, want payment=()", got)
	}
}

func TestLimitRequestBody(t *testing.T) {
	limit := int64(1024) // 1 KB
	mw := LimitRequestBody(limit)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("allows requests within size limit", func(t *testing.T) {
		smallBody := strings.Repeat("a", 512)
		req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(smallBody))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", rr.Code)
		}
	})

	t.Run("blocks requests exceeding size limit", func(t *testing.T) {
		largeBody := strings.Repeat("a", 2048)
		req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(largeBody))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("expected 413 Request Entity Too Large, got %d", rr.Code)
		}
	})
}

func TestStaticFileFilter(t *testing.T) {
	filter := StaticFileFilter()

	handler := filter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("blocks hidden files", func(t *testing.T) {
		for _, path := range []string{"/static/.env", "/static/.git/config", "/static/.hidden.js"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusForbidden {
				t.Errorf("path %s should be blocked with 403, got %d", path, rr.Code)
			}
		}
	})

	t.Run("blocks sensitive extensions", func(t *testing.T) {
		for _, path := range []string{"/static/config.env", "/static/database.sql", "/static/backup.bak", "/static/data.sqlite"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusForbidden {
				t.Errorf("path %s should be blocked with 403, got %d", path, rr.Code)
			}
		}
	})

	t.Run("blocks directory traversal", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/static/../secrets.txt", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("directory traversal should be blocked with 403, got %d", rr.Code)
		}
	})

	t.Run("allows legitimate static assets", func(t *testing.T) {
		for _, path := range []string{"/static/css/styles.css", "/static/js/main.js", "/static/img/logo.svg"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("path %s should be allowed with 200, got %d", path, rr.Code)
			}
		}
	})
}

func TestCSRF_Protection(t *testing.T) {
	secretKey := "test-super-secret-key-that-is-at-least-32-chars-long"

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	ensureMW := EnsureCSRFCookie(secretKey, "http://localhost:8080")
	requireMW := RequireCSRF(secretKey)
	fullStack := ensureMW(requireMW(dummyHandler))

	t.Run("safe method GET allowed without CSRF headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		rr := httptest.NewRecorder()
		fullStack.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 OK for GET, got %d", rr.Code)
		}
		// Should set signed cookie
		cookieHeader := rr.Header().Get("Set-Cookie")
		if !strings.Contains(cookieHeader, "jg_csrf=") {
			t.Errorf("expected Set-Cookie to include jg_csrf, got %s", cookieHeader)
		}
	})

	t.Run("POST rejected when cookie is missing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
		rr := httptest.NewRecorder()
		requireMW(dummyHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden, got %d", rr.Code)
		}
	})

	t.Run("POST rejected with forged / unsigned cookie (subdomain injection simulation)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
		forgedToken := "forged-attacker-token-12345678901234567890"
		req.AddCookie(&http.Cookie{Name: "jg_csrf", Value: forgedToken})
		req.Header.Set("X-CSRF-Token", forgedToken)
		rr := httptest.NewRecorder()
		requireMW(dummyHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for forged cookie, got %d", rr.Code)
		}
	})

	t.Run("POST rejected when token does not match cookie", func(t *testing.T) {
		validToken, err := generateSignedCSRFToken(secretKey)
		if err != nil {
			t.Fatalf("failed to generate signed token: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
		req.AddCookie(&http.Cookie{Name: "jg_csrf", Value: validToken})
		req.Header.Set("X-CSRF-Token", "different-token")
		rr := httptest.NewRecorder()
		requireMW(dummyHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for mismatched token, got %d", rr.Code)
		}
	})

	t.Run("POST accepted with valid signed cookie and matching X-CSRF-Token header", func(t *testing.T) {
		validToken, err := generateSignedCSRFToken(secretKey)
		if err != nil {
			t.Fatalf("failed to generate signed token: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
		req.AddCookie(&http.Cookie{Name: "jg_csrf", Value: validToken})
		req.Header.Set("X-CSRF-Token", validToken)
		rr := httptest.NewRecorder()
		requireMW(dummyHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 OK for valid CSRF, got %d", rr.Code)
		}
	})
}

