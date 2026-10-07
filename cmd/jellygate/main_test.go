package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

func TestAdminLandingPath(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	t.Run("no session redirects to login", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if got := adminLandingPath(req, secret); got != "/admin/login" {
			t.Fatalf("adminLandingPath = %q, want /admin/login", got)
		}
	})

	t.Run("valid session redirects to dashboard", func(t *testing.T) {
		cookieValue, err := session.Sign(session.Payload{
			UserID:   "user-1",
			Username: "mael",
			IsAdmin:  true,
			Exp:      time.Now().Add(session.RememberDuration).Unix(),
			Iat:      time.Now().Unix(),
		}, secret)
		if err != nil {
			t.Fatalf("sign session: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookieValue})
		if got := adminLandingPath(req, secret); got != "/admin/" {
			t.Fatalf("adminLandingPath = %q, want /admin/", got)
		}
	})

	t.Run("revoked session redirects to login", func(t *testing.T) {
		issuedAt := time.Now().Unix() - 10
		cookieValue, err := session.Sign(session.Payload{
			UserID:   "user-1",
			Username: "mael",
			IsAdmin:  true,
			Exp:      time.Now().Add(session.RememberDuration).Unix(),
			Iat:      issuedAt,
		}, secret)
		if err != nil {
			t.Fatalf("sign session: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookieValue})
		validator := func(sess *session.Payload) bool {
			return sess.Iat > issuedAt
		}
		if got := adminLandingPath(req, secret, validator); got != "/admin/login" {
			t.Fatalf("adminLandingPath = %q, want /admin/login", got)
		}
	})
}

func TestErrorHandlers(t *testing.T) {
	renderEngine, err := render.NewEngine("../../web/templates", "../../web/i18n")
	if err != nil {
		t.Fatalf("failed to create render engine: %v", err)
	}

	t.Run("API 404 returns JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/nonexistent", nil)
		rr := httptest.NewRecorder()
		handleNotFound(renderEngine)(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rr.Code)
		}
		if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("content-type = %s, want json", ct)
		}
	})

	t.Run("Browser 404 returns HTML", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/some/random/page", nil)
		rr := httptest.NewRecorder()
		handleNotFound(renderEngine)(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rr.Code)
		}
		if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Errorf("content-type = %s, want html", ct)
		}
		body := rr.Body.String()
		if !strings.Contains(body, "404") {
			t.Errorf("body missing 404")
		}
	})

	t.Run("API 405 returns JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/some-endpoint", nil)
		rr := httptest.NewRecorder()
		handleMethodNotAllowed(renderEngine)(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want 405", rr.Code)
		}
		if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("content-type = %s, want json", ct)
		}
	})

	t.Run("Browser 405 returns HTML", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/dashboard", nil)
		rr := httptest.NewRecorder()
		handleMethodNotAllowed(renderEngine)(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want 405", rr.Code)
		}
		if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Errorf("content-type = %s, want html", ct)
		}
		body := rr.Body.String()
		if !strings.Contains(body, "405") {
			t.Errorf("body missing 405")
		}
	})
}
