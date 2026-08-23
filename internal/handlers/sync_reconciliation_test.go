package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maelmoreau21/JellyGate/internal/authentik"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

type mockJFServerForSync struct {
	server *httptest.Server
	users  []jellyfin.User
}

func newMockJFServer(users []jellyfin.User) *mockJFServerForSync {
	s := &mockJFServerForSync{users: users}
	mux := http.NewServeMux()
	mux.HandleFunc("/Users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.users)
	})
	mux.HandleFunc("/System/Info/Public", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ServerName":"TestJF"}`))
	})
	s.server = httptest.NewServer(mux)
	return s
}

func TestSyncJellyfinUsers_ReconcilesAuthentikNameWithJellyfinUser(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := &config.Config{
		BaseURL:   "http://localhost:8097",
		SecretKey: testAuthSecret,
		Authentik: config.AuthentikConfig{
			Enabled: true,
		},
	}

	// Mock Jellyfin server containing user "Maël Moreau" (created via LDAP name attribute)
	jfServer := newMockJFServer([]jellyfin.User{
		{
			ID:   "jf-uuid-mael-moreau",
			Name: "Maël Moreau",
			Policy: jellyfin.Policy{
				IsDisabled: false,
			},
		},
	})
	defer jfServer.server.Close()

	jfClient := jellyfin.New(config.JellyfinConfig{
		URL:    jfServer.server.URL,
		APIKey: "test-api-key",
	})

	// Mock Authentik returning user "mmoreau" with Name "Maël Moreau"
	mockAuth := &mockAuthentikSyncClient{
		users: []authentik.UserResponse{
			{
				PK:       42,
				ID:       "auth-uuid-42",
				Username: "mmoreau",
				Name:     "Maël Moreau",
				Email:    "mael@example.com",
				IsActive: true,
			},
		},
	}

	renderEngine, _ := newTestRenderEngine(t)
	adminHandler := NewAdminHandler(cfg, db, jfClient, mockAuth, nil, renderEngine)

	req := httptest.NewRequest(http.MethodPost, "/admin/api/users/sync", nil)
	req = req.WithContext(session.NewContext(req.Context(), &session.Payload{
		UserID:   "1",
		Username: "admin",
		IsAdmin:  true,
	}))
	rec := httptest.NewRecorder()

	adminHandler.SyncJellyfinUsers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("SyncJellyfinUsers failed with status %d: %s", rec.Code, rec.Body.String())
	}

	// Verify that user was inserted and properly linked to jf-uuid-mael-moreau
	var jfID, authID, username string
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("Expected exactly 1 user in database, got %d (err=%v)", count, err)
	}

	err = db.QueryRow(`SELECT username, authentik_id, jellyfin_id FROM users WHERE authentik_id = 'auth-uuid-42'`).Scan(&username, &authID, &jfID)
	if err != nil {
		t.Fatalf("Failed to query reconciled user: %v", err)
	}

	if jfID != "jf-uuid-mael-moreau" {
		t.Errorf("Expected jellyfin_id='jf-uuid-mael-moreau', got %q", jfID)
	}
	if username != "mmoreau" {
		t.Errorf("Expected username='mmoreau', got %q", username)
	}
}

type mockAuthentikSyncClient struct {
	mockAuthentikClient
	users []authentik.UserResponse
}

func (m *mockAuthentikSyncClient) ListUsers(ctx context.Context) ([]authentik.UserResponse, error) {
	return m.users, nil
}
