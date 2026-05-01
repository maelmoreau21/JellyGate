package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

func TestLoginPageRedirectsWhenSessionCookieValid(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

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

	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookieValue})
	rec := httptest.NewRecorder()

	NewAuthHandler(&config.Config{SecretKey: secret}, nil, nil).LoginPage(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if got := rec.Header().Get("Location"); got != "/admin/" {
		t.Fatalf("Location = %q, want /admin/", got)
	}
}
