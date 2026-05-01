package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
