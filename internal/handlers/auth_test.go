package handlers

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
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

	NewAuthHandler(&config.Config{SecretKey: secret}, nil, nil, nil, nil, nil).LoginPage(rec, req)

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
	handler := NewAuthHandler(&config.Config{SecretKey: testAuthSecret}, db, nil, nil, nil, nil)

	if handler.hasValidSession(req) {
		t.Fatalf("revoked session should not be accepted")
	}
}

func TestLoginSubmitRefreshesJellyfinPolicyAndSetsRememberCookie(t *testing.T) {
	db := newAuthTestDB(t)
	server := newAuthJellyfinServer(t, http.StatusOK, http.StatusOK, true)
	defer server.Close()

	handler := newAuthTestHandler(db, server.URL)
	rec := httptest.NewRecorder()
	handler.LoginSubmit(rec, newLoginSubmitRequest("admin", "secret", true))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if got := rec.Header().Get("Location"); got != "/admin/" {
		t.Fatalf("Location = %q, want /admin/", got)
	}

	cookie := responseCookie(rec, session.CookieName)
	if cookie == nil {
		t.Fatalf("session cookie missing")
	}
	if cookie.MaxAge != int(session.RememberDuration.Seconds()) {
		t.Fatalf("cookie MaxAge = %d, want %d", cookie.MaxAge, int(session.RememberDuration.Seconds()))
	}

	payload, err := session.Verify(cookie.Value, testAuthSecret)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !payload.IsAdmin || payload.UserID != "admin-id" || payload.Username != "admin" {
		t.Fatalf("session payload = %+v, want refreshed admin user", payload)
	}
}

func TestLoginSubmitRememberDurations(t *testing.T) {
	tests := []struct {
		name           string
		remember       bool
		remember30Days bool
		wantMaxAge     int
	}{
		{
			name:           "default session",
			remember:       false,
			remember30Days: true,
			wantMaxAge:     int(session.Duration.Seconds()),
		},
		{
			name:           "remember for 30 days",
			remember:       true,
			remember30Days: true,
			wantMaxAge:     int(session.RememberDuration.Seconds()),
		},
		{
			name:           "remember indefinitely",
			remember:       true,
			remember30Days: false,
			wantMaxAge:     int(session.IndefiniteDuration.Seconds()),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newAuthTestDB(t)
			if err := db.SaveAuthSessionConfig(database.AuthSessionConfig{Remember30Days: tt.remember30Days}); err != nil {
				t.Fatalf("SaveAuthSessionConfig() error = %v", err)
			}
			server := newAuthJellyfinServer(t, http.StatusOK, http.StatusOK, false)
			defer server.Close()

			handler := newAuthTestHandler(db, server.URL)
			rec := httptest.NewRecorder()
			handler.LoginSubmit(rec, newLoginSubmitRequest("admin", "secret", tt.remember))

			cookie := responseCookie(rec, session.CookieName)
			if cookie == nil {
				t.Fatalf("session cookie missing")
			}
			if cookie.MaxAge != tt.wantMaxAge {
				t.Fatalf("cookie MaxAge = %d, want %d", cookie.MaxAge, tt.wantMaxAge)
			}
		})
	}
}

func TestLoginSubmitJellyfinFailuresDoNotSetSessionCookie(t *testing.T) {
	tests := []struct {
		name       string
		authStatus int
		userStatus int
	}{
		{name: "unauthorized", authStatus: http.StatusUnauthorized, userStatus: http.StatusOK},
		{name: "forbidden", authStatus: http.StatusForbidden, userStatus: http.StatusOK},
		{name: "auth server error", authStatus: http.StatusInternalServerError, userStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newAuthTestDB(t)
			server := newAuthJellyfinServer(t, tt.authStatus, tt.userStatus, true)
			defer server.Close()

			handler := newAuthTestHandler(db, server.URL)
			rec := httptest.NewRecorder()
			handler.LoginSubmit(rec, newLoginSubmitRequest("admin", "secret", true))

			if rec.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
			}
			if got := rec.Header().Get("Location"); !strings.Contains(got, "/admin/login?") || !strings.Contains(got, "error=invalid") {
				t.Fatalf("Location = %q, want login invalid redirect", got)
			}
			if cookie := responseCookie(rec, session.CookieName); cookie != nil && cookie.MaxAge >= 0 {
				t.Fatalf("session cookie should not be set on failure: %+v", cookie)
			}
		})
	}
}

func TestLoginSubmitAllowsSessionWithoutAdminWhenPolicyRefreshUnavailable(t *testing.T) {
	db := newAuthTestDB(t)
	server := newAuthJellyfinServer(t, http.StatusOK, http.StatusInternalServerError, true)
	defer server.Close()

	handler := newAuthTestHandler(db, server.URL)
	rec := httptest.NewRecorder()
	handler.LoginSubmit(rec, newLoginSubmitRequest("admin", "secret", true))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if got := rec.Header().Get("Location"); got != "/admin/" {
		t.Fatalf("Location = %q, want /admin/", got)
	}

	cookie := responseCookie(rec, session.CookieName)
	if cookie == nil {
		t.Fatalf("session cookie missing")
	}
	payload, err := session.Verify(cookie.Value, testAuthSecret)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if payload.IsAdmin {
		t.Fatalf("session should not grant admin when policy is unconfirmed: %+v", payload)
	}
}

func TestLoginSubmitAllowsSessionWithoutAdminWhenAPIKeyInvalid(t *testing.T) {
	db := newAuthTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/Users/AuthenticateByName":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"User": map[string]interface{}{
					"Id":   "admin-id",
					"Name": "admin",
					"Policy": map[string]bool{
						"IsAdministrator": true,
					},
				},
				"AccessToken": "session-token",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/Users/admin-id" && strings.Contains(r.Header.Get("Authorization"), `Token="session-token"`):
			http.Error(w, "token cannot read policy", http.StatusForbidden)
		case r.Method == http.MethodGet && r.URL.Path == "/Users/admin-id" && strings.Contains(r.Header.Get("Authorization"), `Token="admin-api-key"`):
			http.Error(w, "api key invalid", http.StatusUnauthorized)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	handler := newAuthTestHandler(db, server.URL)
	rec := httptest.NewRecorder()
	handler.LoginSubmit(rec, newLoginSubmitRequest("admin", "secret", true))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	cookie := responseCookie(rec, session.CookieName)
	if cookie == nil {
		t.Fatalf("session cookie missing")
	}
	payload, err := session.Verify(cookie.Value, testAuthSecret)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if payload.IsAdmin {
		t.Fatalf("invalid API key should not grant admin: %+v", payload)
	}
}

func TestLoginSubmitAllowsJellyfinAdminWhenLDAPUnavailable(t *testing.T) {
	db := newAuthTestDB(t)
	saveUnavailableLDAPConfig(t, db)
	server := newAuthJellyfinServer(t, http.StatusOK, http.StatusOK, true)
	defer server.Close()

	handler := newAuthTestHandler(db, server.URL)
	rec := httptest.NewRecorder()
	handler.LoginSubmit(rec, newLoginSubmitRequest("admin", "secret", true))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if got := rec.Header().Get("Location"); got != "/admin/" {
		t.Fatalf("Location = %q, want /admin/", got)
	}
	cookie := responseCookie(rec, session.CookieName)
	if cookie == nil {
		t.Fatalf("session cookie missing")
	}
	payload, err := session.Verify(cookie.Value, testAuthSecret)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !payload.IsAdmin {
		t.Fatalf("Jellyfin admin should keep admin access when LDAP is unavailable: %+v", payload)
	}
}

func TestLoginSubmitRejectsStandardUserWhenLDAPUnavailable(t *testing.T) {
	db := newAuthTestDB(t)
	saveUnavailableLDAPConfig(t, db)
	server := newAuthJellyfinServer(t, http.StatusOK, http.StatusOK, false)
	defer server.Close()

	handler := newAuthTestHandler(db, server.URL)
	rec := httptest.NewRecorder()
	handler.LoginSubmit(rec, newLoginSubmitRequest("admin", "secret", true))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if got := rec.Header().Get("Location"); !strings.Contains(got, "/admin/login?") || !strings.Contains(got, "error=invalid") {
		t.Fatalf("Location = %q, want login invalid redirect", got)
	}
	if cookie := responseCookie(rec, session.CookieName); cookie != nil && cookie.MaxAge >= 0 {
		t.Fatalf("session cookie should not be set on LDAP failure for standard user: %+v", cookie)
	}
}

func TestLoginSubmitLogsRedactJellyfinSecrets(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	db := newAuthTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"AccessToken":"leaky-token","Password":"secret"}`, http.StatusInternalServerError)
	}))
	defer server.Close()

	handler := newAuthTestHandler(db, server.URL)
	rec := httptest.NewRecorder()
	handler.LoginSubmit(rec, newLoginSubmitRequest("admin", "secret", true))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	output := logs.String()
	if strings.Contains(output, "leaky-token") || strings.Contains(output, "Password\":\"secret") || strings.Contains(output, "password\":\"secret") {
		t.Fatalf("logs leaked Jellyfin secret: %s", output)
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Fatalf("logs = %s, want redacted Jellyfin detail", output)
	}
}

const testAuthSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func saveUnavailableLDAPConfig(t *testing.T, db *database.DB) {
	t.Helper()

	err := db.SaveLDAPConfig(config.LDAPConfig{
		Enabled:         true,
		Host:            "127.0.0.1",
		Port:            1,
		UseTLS:          false,
		BindDN:          "cn=readonly,dc=example,dc=com",
		BindPassword:    "secret",
		BaseDN:          "dc=example,dc=com",
		SearchFilter:    "(uid={username})",
		ProvisionMode:   "hybrid",
		JellyfinGroup:   "jellyfin",
		InviterGroup:    "jellyfin-Parrainage",
		UserObjectClass: "auto",
		GroupMemberAttr: "auto",
	})
	if err != nil {
		t.Fatalf("SaveLDAPConfig() error = %v", err)
	}
}

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

func newAuthTestHandler(db *database.DB, jellyfinURL string) *AuthHandler {
	jfClient := jellyfin.New(config.JellyfinConfig{URL: jellyfinURL, APIKey: "admin-api-key"})
	return NewAuthHandler(&config.Config{
		BaseURL:   "http://jellygate.local",
		SecretKey: testAuthSecret,
	}, db, jfClient, nil, nil, nil)
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

func responseCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func newAuthJellyfinServer(t *testing.T, authStatus, userStatus int, isAdmin bool) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && (r.URL.Path == "/Users/AuthenticateByName" || r.URL.Path == "/Users/authenticatebyname"):
			if authStatus != http.StatusOK {
				http.Error(w, "auth failed", authStatus)
				return
			}
			var payload struct {
				Username string `json:"Username"`
				Pw       string `json:"Pw"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode auth payload: %v", err)
			}
			if payload.Username != "admin" || payload.Pw != "secret" {
				t.Fatalf("auth payload = %+v, want admin/secret", payload)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"User": map[string]string{
					"Id":   "admin-id",
					"Name": "admin",
				},
				"AccessToken": "session-token",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/Users/admin-id":
			auth := r.Header.Get("Authorization")
			if !strings.Contains(auth, `Token="session-token"`) && !strings.Contains(auth, `Token="admin-api-key"`) {
				t.Fatalf("Authorization header %q missing expected token", auth)
			}
			if userStatus != http.StatusOK {
				http.Error(w, "policy unavailable", userStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(jellyfin.User{
				ID:   "admin-id",
				Name: "admin",
				Policy: jellyfin.Policy{
					IsAdministrator: isAdmin,
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
}
