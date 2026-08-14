package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/authentik"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
)

type mockAuthentikDecoupledClient struct {
	userActive map[string]bool
	userGroups map[string][]string
}

func newMockAuthentikDecoupledClient() *mockAuthentikDecoupledClient {
	return &mockAuthentikDecoupledClient{
		userActive: make(map[string]bool),
		userGroups: make(map[string][]string),
	}
}

func (m *mockAuthentikDecoupledClient) CheckHealth(ctx context.Context, cfg config.AuthentikConfig) *authentik.HealthCheckResult {
	return &authentik.HealthCheckResult{OverallStatus: "ok"}
}
func (m *mockAuthentikDecoupledClient) CheckAPI(ctx context.Context) authentik.HealthComponent {
	return authentik.HealthComponent{Status: "ok"}
}
func (m *mockAuthentikDecoupledClient) CheckOIDC(ctx context.Context, issuerURL string) authentik.HealthComponent {
	return authentik.HealthComponent{Status: "ok"}
}
func (m *mockAuthentikDecoupledClient) CheckEnrollment(ctx context.Context, flowSlug string) authentik.HealthComponent {
	return authentik.HealthComponent{Status: "ok"}
}
func (m *mockAuthentikDecoupledClient) CheckGroups(ctx context.Context, groups []string) authentik.HealthComponent {
	return authentik.HealthComponent{Status: "ok"}
}
func (m *mockAuthentikDecoupledClient) CreateUser(ctx context.Context, payload authentik.UserCreatePayload) (*authentik.UserResponse, error) {
	m.userActive[payload.Username] = payload.IsActive
	m.userGroups[payload.Username] = payload.Groups
	return &authentik.UserResponse{ID: "auth-sub-123", PK: 1, Username: payload.Username, Email: payload.Email, IsActive: payload.IsActive}, nil
}
func (m *mockAuthentikDecoupledClient) CreateRecoveryLink(ctx context.Context, userPK int64) (string, error) {
	return "https://auth.example.com/recovery", nil
}
func (m *mockAuthentikDecoupledClient) AddUserToGroup(ctx context.Context, userPK int64, groupID string) error {
	return nil
}
func (m *mockAuthentikDecoupledClient) RemoveUserFromGroup(ctx context.Context, userPK int64, groupID string) error {
	return nil
}
func (m *mockAuthentikDecoupledClient) AddUserToGroupByString(ctx context.Context, authentikID string, groupID string) error {
	m.userGroups[authentikID] = append(m.userGroups[authentikID], groupID)
	return nil
}
func (m *mockAuthentikDecoupledClient) RemoveUserFromGroupByString(ctx context.Context, authentikID string, groupID string) error {
	var remaining []string
	for _, g := range m.userGroups[authentikID] {
		if g != groupID {
			remaining = append(remaining, g)
		}
	}
	m.userGroups[authentikID] = remaining
	return nil
}
func (m *mockAuthentikDecoupledClient) SetUserActiveStatus(ctx context.Context, userPK int64, active bool) error {
	return nil
}
func (m *mockAuthentikDecoupledClient) SetUserActiveStatusByString(ctx context.Context, authentikID string, active bool) error {
	m.userActive[authentikID] = active
	return nil
}
func (m *mockAuthentikDecoupledClient) DeleteUser(ctx context.Context, userPK int64) error {
	return nil
}
func (m *mockAuthentikDecoupledClient) DeleteUserByString(ctx context.Context, authentikID string) error {
	return nil
}
func (m *mockAuthentikDecoupledClient) ListUsers(ctx context.Context) ([]authentik.UserResponse, error) {
	return []authentik.UserResponse{
		{ID: "auth-sub-123", PK: 1, Username: "testuser", Email: "user@example.com", IsActive: true},
	}, nil
}
func (m *mockAuthentikDecoupledClient) GetUserByUsername(ctx context.Context, username string) (*authentik.UserDetailResponse, error) {
	return &authentik.UserDetailResponse{
		PK:       1,
		Username: username,
		IsActive: true,
		Groups:   []string{"jellygate-users"},
	}, nil
}
func (m *mockAuthentikDecoupledClient) CreateInvitationStageToken(ctx context.Context, name string, expiresAt time.Time, fixedData map[string]interface{}, singleUse bool, flow string) (string, error) {
	return "stage-pk-456", nil
}
func (m *mockAuthentikDecoupledClient) ListInvitationStageTokens(ctx context.Context) ([]authentik.InvitationTokenResponse, error) {
	return nil, nil
}
func (m *mockAuthentikDecoupledClient) DeleteInvitationStageToken(ctx context.Context, invitationID string) error {
	return nil
}

func TestJellyGateStartsWithoutJellyfin(t *testing.T) {
	cfg := &config.Config{
		Port:      8097,
		BaseURL:   "http://localhost:8097",
		SecretKey: "0123456789abcdef0123456789abcdef0123456789abcdef",
		Jellyfin:  config.JellyfinConfig{URL: "", APIKey: ""},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("cfg.Validate() error = %v, want successful startup without Jellyfin", err)
	}

	unconfiguredJF := jellyfin.New(cfg.Jellyfin)
	if unconfiguredJF.IsConfigured() {
		t.Fatalf("IsConfigured() = true, want false")
	}
	if status := unconfiguredJF.Status(); status != jellyfin.StatusDisabled {
		t.Fatalf("Status() = %v, want StatusDisabled", status)
	}
}

func TestJellyGateWorksWithJellyfinConfigured(t *testing.T) {
	jfServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ServerName":"TestServer"}`))
	}))
	defer jfServer.Close()

	cfg := config.JellyfinConfig{URL: jfServer.URL, APIKey: "test-api-key"}
	jfClient := jellyfin.New(cfg)
	if !jfClient.IsConfigured() {
		t.Fatalf("IsConfigured() = false, want true")
	}
	if status := jfClient.Status(); status != jellyfin.StatusAvailable {
		t.Fatalf("Status() = %v, want StatusAvailable", status)
	}
}

func TestJellyfinUnavailableDoesNotBreakJellyGate(t *testing.T) {
	jfClient := jellyfin.New(config.JellyfinConfig{URL: "http://127.0.0.1:59999", APIKey: "test-api-key"})
	if status := jfClient.Status(); status != jellyfin.StatusUnavailable {
		t.Fatalf("Status() = %v, want StatusUnavailable", status)
	}

	// Calling business methods on unavailable client returns errors cleanly without panic
	if _, err := jfClient.GetLibraries(); err == nil {
		t.Fatalf("GetLibraries() error = nil, want error")
	}
}

func TestOIDCWorksWithoutJellyfinAPI(t *testing.T) {
	cfg := &config.Config{
		BaseURL:   "http://localhost:8097",
		SecretKey: "0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	handler := NewAuthHandler(cfg, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/auth/logout", nil)
	rec := httptest.NewRecorder()
	handler.Logout(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("Logout code = %d, want %d", rec.Code, http.StatusSeeOther)
	}
}

func TestInvitationWorksWithoutJellyfinAPI(t *testing.T) {
	db, err := database.New(config.DatabaseConfig{Type: "sqlite"}, t.TempDir(), "test-secret-key-0123456789012345")
	if err != nil {
		t.Fatalf("database.New() error = %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`INSERT INTO invitations (code, max_uses, used_count) VALUES ('no-jf-invite', 10, 0)`); err != nil {
		t.Fatalf("insert invitation error = %v", err)
	}

	cfg := &config.Config{BaseURL: "http://localhost:8097"}
	mockAuth := newMockAuthentikDecoupledClient()
	handler := NewInvitationHandler(cfg, db, nil, nil, nil, nil)
	handler.SetAuthentikClient(mockAuth)

	inv, err := handler.getValidInvitation("no-jf-invite")
	if err != nil {
		t.Fatalf("getValidInvitation() error = %v", err)
	}
	if inv.Code != "no-jf-invite" {
		t.Fatalf("invitation code = %q, want no-jf-invite", inv.Code)
	}
}

func TestDeactivationPassesThroughAuthentik(t *testing.T) {
	db, err := database.New(config.DatabaseConfig{Type: "sqlite"}, t.TempDir(), "test-secret-key-0123456789012345")
	if err != nil {
		t.Fatalf("database.New() error = %v", err)
	}
	defer db.Close()

	mockAuth := newMockAuthentikDecoupledClient()
	adminHandler := NewAdminHandler(&config.Config{}, db, nil, mockAuth, nil, nil)

	rec := &adminUserRecord{
		ID:          1,
		Username:    "testuser",
		AuthentikID: sql.NullString{String: "auth-sub-123", Valid: true},
	}

	errs, err := adminHandler.setUserActiveState(rec, false, "admin")
	if err != nil {
		t.Fatalf("setUserActiveState() error = %v", err)
	}
	if len(errs) > 0 {
		t.Fatalf("setUserActiveState() partial errors = %v", errs)
	}
	if mockAuth.userActive["auth-sub-123"] != false {
		t.Fatalf("Authentik user active status = %v, want false", mockAuth.userActive["auth-sub-123"])
	}
}
