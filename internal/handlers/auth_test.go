package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
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

	NewAuthHandler(&config.Config{SecretKey: secret}, nil, nil, nil, nil).LoginPage(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if got := rec.Header().Get("Location"); got != "/admin/" {
		t.Fatalf("Location = %q, want /admin/", got)
	}
}

func TestAuthHandlerRejectsRevokedSessionCookie(t *testing.T) {
	db := newAuthTestDB(t)
	revokedBefore := time.Now().Unix()
	if _, err := db.RevokeAuthSessionsBefore(revokedBefore); err != nil {
		t.Fatalf("RevokeAuthSessionsBefore() error = %v", err)
	}

	cookieValue, err := session.Sign(session.Payload{
		UserID:   "user-1",
		Username: "mael",
		IsAdmin:  true,
		Exp:      time.Now().Add(session.RememberDuration).Unix(),
		Iat:      revokedBefore,
	}, testAuthSecret)
	if err != nil {
		t.Fatalf("sign session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookieValue})
	handler := NewAuthHandler(&config.Config{SecretKey: testAuthSecret}, db, nil, nil, nil)

	if handler.hasValidSession(req) {
		t.Fatalf("revoked session should not be accepted")
	}
}

func TestLoginSubmitRedirectsToOIDC(t *testing.T) {
	db := newAuthTestDB(t)
	handler := newAuthTestHandler(db)
	rec := httptest.NewRecorder()
	handler.LoginSubmit(rec, newLoginSubmitRequest("admin", "secret", true))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if got := rec.Header().Get("Location"); got != "/auth/login" {
		t.Fatalf("Location = %q, want /auth/login", got)
	}
}

const testAuthSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func newAuthTestDB(t *testing.T) *database.DB {
	t.Helper()

	db, err := database.New(config.DatabaseConfig{Type: "sqlite"}, t.TempDir(), "test-secret-key-0123456789012345")
	if err != nil {
		t.Fatalf("database.New() error = %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func newAuthTestHandler(db *database.DB) *AuthHandler {
	return NewAuthHandler(&config.Config{
		BaseURL:   "http://jellygate.local",
		SecretKey: testAuthSecret,
	}, db, nil, nil, nil)
}

func newLoginSubmitRequest(username, password string, remember bool) *http.Request {
	values := url.Values{}
	values.Set("username", username)
	values.Set("password", password)
	if remember {
		values.Set("remember_me", "1")
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestLocalLoginSubmit(t *testing.T) {
	db := newAuthTestDB(t)
	handler := newAuthTestHandler(db)

	t.Run("successful login with secret redirects to /admin/authentik", func(t *testing.T) {
		values := url.Values{}
		values.Set("secret", testAuthSecret)
		req := httptest.NewRequest(http.MethodPost, "/local", strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		handler.LocalLoginSubmit(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		if got := rec.Header().Get("Location"); got != "/admin/authentik" {
			t.Fatalf("Location = %q, want /admin/authentik", got)
		}

		cookies := rec.Result().Cookies()
		var sessionCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == session.CookieName {
				sessionCookie = c
				break
			}
		}
		if sessionCookie == nil {
			t.Fatalf("expected session cookie to be set")
		}

		sess, err := session.Verify(sessionCookie.Value, testAuthSecret)
		if err != nil {
			t.Fatalf("session.Verify error = %v", err)
		}
		if !sess.IsAdmin {
			t.Errorf("expected session.IsAdmin to be true")
		}
	})

	t.Run("invalid secret returns 401 unauthorized", func(t *testing.T) {
		values := url.Values{}
		values.Set("secret", "wrong-secret-key")
		req := httptest.NewRequest(http.MethodPost, "/local", strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		handler.LocalLoginSubmit(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})
}

