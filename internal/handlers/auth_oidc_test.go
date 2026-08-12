package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/oidc"
	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

var _ *database.DB

type mockOIDCClient struct {
	authURL          string
	generateAuthErr  error
	handleCallbackFn func(r *http.Request) (*oidc.Claims, error)
	claims           *oidc.Claims
	callbackErr      error
	isAdmin          bool
	hasAccess        bool
}

func (m *mockOIDCClient) GenerateAuthURL(w http.ResponseWriter, r *http.Request) (string, error) {
	if m.generateAuthErr != nil {
		return "", m.generateAuthErr
	}
	if m.authURL != "" {
		return m.authURL, nil
	}
	return "https://auth.example.com/application/o/authorize/?client_id=test", nil
}

func (m *mockOIDCClient) HandleCallback(r *http.Request) (*oidc.Claims, error) {
	if m.handleCallbackFn != nil {
		return m.handleCallbackFn(r)
	}
	if m.callbackErr != nil {
		return nil, m.callbackErr
	}
	return m.claims, nil
}

func (m *mockOIDCClient) ValidateIDToken(ctx context.Context, rawIDToken string, expectedNonce string) (*oidc.Claims, error) {
	if m.callbackErr != nil {
		return nil, m.callbackErr
	}
	return m.claims, nil
}

func (m *mockOIDCClient) DetermineUserRole(groups []string) (bool, bool) {
	var isAdmin, hasAccess bool
	for _, g := range groups {
		if g == "jellygate-admins" {
			isAdmin = true
			hasAccess = true
		}
		if g == "jellygate-users" {
			hasAccess = true
		}
	}
	if len(groups) == 0 {
		return m.isAdmin, m.hasAccess
	}
	return isAdmin, hasAccess
}

func (m *mockOIDCClient) GetEndSessionURL(ctx context.Context) string {
	return "https://auth.example.com/application/o/jellygate/end-session/"
}

func TestOIDCLoginRedirect(t *testing.T) {
	cfg := &config.Config{
		SecretKey: strings.Repeat("s", 32),
		Authentik: config.AuthentikConfig{
			Enabled: true,
			URL:     "https://auth.example.com",
		},
	}

	mockOIDC := &mockOIDCClient{
		authURL: "https://auth.example.com/application/o/authorize/?client_id=test&state=123",
	}

	handler := NewAuthHandler(cfg, nil, mockOIDC, nil, nil)

	t.Run("Redirects to OIDC Auth URL", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
		rec := httptest.NewRecorder()

		handler.LoginRedirect(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("Expected status 303 SeeOther, got %d", rec.Code)
		}
		if rec.Header().Get("Location") != mockOIDC.authURL {
			t.Fatalf("Expected Location %s, got %s", mockOIDC.authURL, rec.Header().Get("Location"))
		}
	})

	t.Run("Already Logged In Redirects to Admin", func(t *testing.T) {
		sessCookie, _ := session.Sign(session.Payload{
			UserID:   "1",
			Username: "admin",
			IsAdmin:  true,
			Exp:      time.Now().Add(1 * time.Hour).Unix(),
		}, cfg.SecretKey)

		req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: sessCookie})
		rec := httptest.NewRecorder()

		handler.LoginRedirect(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("Expected status 303, got %d", rec.Code)
		}
		if rec.Header().Get("Location") != "/admin/" {
			t.Fatalf("Expected Location /admin/, got %s", rec.Header().Get("Location"))
		}
	})
}

func TestOIDCCallbackScenarios(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := &config.Config{
		BaseURL:   "http://localhost:8097",
		SecretKey: testAuthSecret,
		Authentik: config.AuthentikConfig{
			Enabled:    true,
			UserGroup:  "jellygate-users",
			AdminGroup: "jellygate-admins",
		},
	}

	t.Run("Unknown User JIT Creation", func(t *testing.T) {
		mockOIDC := &mockOIDCClient{
			claims: &oidc.Claims{
				Sub:               "uuid-new-user-1",
				PreferredUsername: "jit_user",
				Email:             "jit@example.com",
				Groups:            []string{"jellygate-users"},
			},
			hasAccess: true,
		}
		handler := NewAuthHandler(cfg, db, mockOIDC, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=123&state=abc", nil)
		rec := httptest.NewRecorder()

		handler.Callback(rec, req)

		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/" {
			t.Fatalf("Expected redirect to /admin/, got status %d, loc %s", rec.Code, rec.Header().Get("Location"))
		}

		user, err := db.GetUserByAuthentikID(context.Background(), "uuid-new-user-1")
		if err != nil || user == nil {
			t.Fatalf("Expected JIT created user in DB, got error: %v", err)
		}
		if user.Username != "jit_user" || user.Email != "jit@example.com" {
			t.Errorf("Unexpected user fields: username=%s, email=%s", user.Username, user.Email)
		}
	})

	t.Run("Existing User Linking", func(t *testing.T) {
		// Insert pre-existing user without authentik_id
		_, err := db.Exec(`INSERT INTO users (username, email, is_active) VALUES ('legacy_user', 'legacy@example.com', 1)`)
		if err != nil {
			t.Fatalf("Failed to seed existing user: %v", err)
		}

		mockOIDC := &mockOIDCClient{
			claims: &oidc.Claims{
				Sub:               "uuid-legacy-user-99",
				PreferredUsername: "legacy_user",
				Email:             "legacy@example.com",
				Groups:            []string{"jellygate-users"},
			},
			hasAccess: true,
		}
		handler := NewAuthHandler(cfg, db, mockOIDC, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=123&state=abc", nil)
		rec := httptest.NewRecorder()

		handler.Callback(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("Expected 303 redirect, got %d", rec.Code)
		}

		linkedUser, err := db.GetUserByUsername(context.Background(), "legacy_user")
		if err != nil || linkedUser == nil {
			t.Fatalf("Failed to fetch linked user: %v", err)
		}
		if linkedUser.AuthentikID.String != "uuid-legacy-user-99" {
			t.Errorf("Expected linked authentik_id uuid-legacy-user-99, got %s", linkedUser.AuthentikID.String)
		}
	})

	t.Run("Admin Group Mapping", func(t *testing.T) {
		mockOIDC := &mockOIDCClient{
			claims: &oidc.Claims{
				Sub:               "uuid-admin-777",
				PreferredUsername: "admin_user",
				Email:             "admin@example.com",
				Groups:            []string{"jellygate-users", "jellygate-admins"},
			},
			isAdmin:   true,
			hasAccess: true,
		}
		handler := NewAuthHandler(cfg, db, mockOIDC, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=123&state=abc", nil)
		rec := httptest.NewRecorder()

		handler.Callback(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("Expected 303 redirect, got %d", rec.Code)
		}

		cookies := rec.Result().Cookies()
		var sessCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == session.CookieName {
				sessCookie = c
				break
			}
		}
		if sessCookie == nil {
			t.Fatal("Expected jellygate_session cookie to be set")
		}

		payload, err := session.Verify(sessCookie.Value, cfg.SecretKey)
		if err != nil {
			t.Fatalf("Session verify failed: %v", err)
		}
		if !payload.IsAdmin {
			t.Errorf("Expected IsAdmin=true for admin group member")
		}
	})

	t.Run("Unauthorized User Missing Required Group", func(t *testing.T) {
		mockOIDC := &mockOIDCClient{
			claims: &oidc.Claims{
				Sub:               "uuid-unauthorized-88",
				PreferredUsername: "stranger",
				Email:             "stranger@example.com",
				Groups:            []string{"unauthorized-group"},
			},
			isAdmin:   false,
			hasAccess: false,
		}
		renderEngine, _ := newTestRenderEngine(t)
		handler := NewAuthHandler(cfg, db, mockOIDC, nil, renderEngine)

		req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=123&state=abc", nil)
		rec := httptest.NewRecorder()

		handler.Callback(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("Expected 403 Forbidden for user missing required group, got %d", rec.Code)
		}
	})

	t.Run("Bad State Error", func(t *testing.T) {
		mockOIDC := &mockOIDCClient{
			callbackErr: errors.New("invalid or mismatched OIDC state parameter"),
		}
		handler := NewAuthHandler(cfg, db, mockOIDC, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=123&state=bad", nil)
		rec := httptest.NewRecorder()

		handler.Callback(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("Expected redirect on state error, got %d", rec.Code)
		}
		u, _ := url.Parse(rec.Header().Get("Location"))
		if u.Query().Get("error") != "bad_state" {
			t.Errorf("Expected error=bad_state, got %s", u.Query().Get("error"))
		}
	})

	t.Run("Bad Nonce Error", func(t *testing.T) {
		mockOIDC := &mockOIDCClient{
			callbackErr: errors.New("mismatched nonce in ID token"),
		}
		handler := NewAuthHandler(cfg, db, mockOIDC, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=123&state=abc", nil)
		rec := httptest.NewRecorder()

		handler.Callback(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("Expected redirect on nonce error, got %d", rec.Code)
		}
		u, _ := url.Parse(rec.Header().Get("Location"))
		if u.Query().Get("error") != "bad_nonce" {
			t.Errorf("Expected error=bad_nonce, got %s", u.Query().Get("error"))
		}
	})

	t.Run("Token Expired Error", func(t *testing.T) {
		mockOIDC := &mockOIDCClient{
			callbackErr: errors.New("ID token has expired"),
		}
		handler := NewAuthHandler(cfg, db, mockOIDC, nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=123&state=abc", nil)
		rec := httptest.NewRecorder()

		handler.Callback(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("Expected redirect on expired token error, got %d", rec.Code)
		}
		u, _ := url.Parse(rec.Header().Get("Location"))
		if u.Query().Get("error") != "token_expired" {
			t.Errorf("Expected error=token_expired, got %s", u.Query().Get("error"))
		}
	})
}

func TestOIDCLogout(t *testing.T) {
	cfg := &config.Config{
		SecretKey: testAuthSecret,
		Authentik: config.AuthentikConfig{
			URL: "https://auth.example.com",
		},
	}
	handler := NewAuthHandler(cfg, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/auth/logout", nil)
	rec := httptest.NewRecorder()

	handler.Logout(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("Expected 303 redirect, got %d", rec.Code)
	}

	expectedEndSession := "https://auth.example.com/application/o/jellygate/end-session/"
	if rec.Header().Get("Location") != expectedEndSession {
		t.Errorf("Expected logout redirect to %s, got %s", expectedEndSession, rec.Header().Get("Location"))
	}

	// Verify session cookie cleared
	cookies := rec.Result().Cookies()
	for _, c := range cookies {
		if c.Name == session.CookieName && c.MaxAge != -1 {
			t.Errorf("Session cookie MaxAge should be -1, got %d", c.MaxAge)
		}
	}
}

func newTestRenderEngine(t *testing.T) (*render.Engine, error) {
	t.Helper()
	return render.NewEngine("../../web/templates", "../../web/i18n")
}
