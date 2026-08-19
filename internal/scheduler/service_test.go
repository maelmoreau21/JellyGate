package scheduler

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/authentik"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
)

type mockAuthSchedulerClient struct {
	tokens        map[string]authentik.InvitationTokenResponse
	createdTokens map[string]string
	deletedTokens []string
}

func newMockAuthSchedulerClient() *mockAuthSchedulerClient {
	return &mockAuthSchedulerClient{
		tokens:        make(map[string]authentik.InvitationTokenResponse),
		createdTokens: make(map[string]string),
		deletedTokens: make([]string, 0),
	}
}

func (m *mockAuthSchedulerClient) CheckHealth(ctx context.Context, cfg config.AuthentikConfig) *authentik.HealthCheckResult {
	return &authentik.HealthCheckResult{OverallStatus: "ok"}
}
func (m *mockAuthSchedulerClient) CheckAPI(ctx context.Context) authentik.HealthComponent {
	return authentik.HealthComponent{Status: "ok"}
}
func (m *mockAuthSchedulerClient) CheckOIDC(ctx context.Context, issuerURL string) authentik.HealthComponent {
	return authentik.HealthComponent{Status: "ok"}
}
func (m *mockAuthSchedulerClient) CheckEnrollment(ctx context.Context, flowSlug string) authentik.HealthComponent {
	return authentik.HealthComponent{Status: "ok"}
}
func (m *mockAuthSchedulerClient) CheckGroups(ctx context.Context, groups []string) authentik.HealthComponent {
	return authentik.HealthComponent{Status: "ok"}
}
func (m *mockAuthSchedulerClient) ResolveGroupID(ctx context.Context, nameOrPK string) (string, error) {
	return "00000000-0000-0000-0000-000000000001", nil
}
func (m *mockAuthSchedulerClient) CreateUser(ctx context.Context, payload authentik.UserCreatePayload) (*authentik.UserResponse, error) {
	return &authentik.UserResponse{PK: 1, Username: payload.Username}, nil
}
func (m *mockAuthSchedulerClient) CreateRecoveryLink(ctx context.Context, authentikPK int64) (string, error) {
	return "http://localhost/recovery", nil
}
func (m *mockAuthSchedulerClient) CreateRecoveryLinkByString(ctx context.Context, identifier string) (string, error) {
	return "http://localhost/recovery", nil
}
func (m *mockAuthSchedulerClient) AddUserToGroup(ctx context.Context, userPK int64, groupID string) error {
	return nil
}
func (m *mockAuthSchedulerClient) RemoveUserFromGroup(ctx context.Context, userPK int64, groupID string) error {
	return nil
}
func (m *mockAuthSchedulerClient) AddUserToGroupByString(ctx context.Context, authentikID string, groupID string) error {
	return nil
}
func (m *mockAuthSchedulerClient) RemoveUserFromGroupByString(ctx context.Context, authentikID string, groupID string) error {
	return nil
}
func (m *mockAuthSchedulerClient) SetUserActiveStatus(ctx context.Context, userPK int64, active bool) error {
	return nil
}
func (m *mockAuthSchedulerClient) SetUserActiveStatusByString(ctx context.Context, authentikID string, active bool) error {
	return nil
}
func (m *mockAuthSchedulerClient) DeleteUser(ctx context.Context, userPK int64) error {
	return nil
}
func (m *mockAuthSchedulerClient) DeleteUserByString(ctx context.Context, authentikID string) error {
	return nil
}
func (m *mockAuthSchedulerClient) ListUsers(ctx context.Context) ([]authentik.UserResponse, error) {
	return nil, nil
}
func (m *mockAuthSchedulerClient) GetUserByUsername(ctx context.Context, username string) (*authentik.UserDetailResponse, error) {
	return &authentik.UserDetailResponse{PK: 1, Username: username}, nil
}
func (m *mockAuthSchedulerClient) CreateInvitationStageToken(ctx context.Context, name string, expiresAt time.Time, fixedData map[string]interface{}, singleUse bool, flow string) (string, error) {
	code := ""
	if fixedData != nil {
		if c, ok := fixedData["code"].(string); ok {
			code = c
		}
	}
	pk := "new-stage-token-" + code
	m.createdTokens[code] = pk
	m.tokens[pk] = authentik.InvitationTokenResponse{
		PK:        pk,
		Name:      name,
		Expires:   expiresAt,
		FixedData: fixedData,
	}
	return pk, nil
}
func (m *mockAuthSchedulerClient) ListInvitationStageTokens(ctx context.Context) ([]authentik.InvitationTokenResponse, error) {
	list := make([]authentik.InvitationTokenResponse, 0, len(m.tokens))
	for _, t := range m.tokens {
		list = append(list, t)
	}
	return list, nil
}
func (m *mockAuthSchedulerClient) DeleteInvitationStageToken(ctx context.Context, invitationID string) error {
	m.deletedTokens = append(m.deletedTokens, invitationID)
	delete(m.tokens, invitationID)
	return nil
}
func (m *mockAuthSchedulerClient) GetEnrollmentFlowSlug(ctx context.Context, preferred string) string {
	return "default-enrollment-flow"
}
func (m *mockAuthSchedulerClient) GetBaseURL() string {
	return "http://localhost:9000"
}

func newTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.New(config.DatabaseConfig{Type: "sqlite"}, t.TempDir(), "test-secret-key-0123456789012345")
	if err != nil {
		t.Fatalf("Failed to initialize test db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func TestSchedulerReconcileAuthentik(t *testing.T) {
	db := newTestDB(t)
	mockAuth := newMockAuthSchedulerClient()

	svc := NewService(db, nil, nil, nil)
	svc.SetAuthentikClient(mockAuth)

	// 1. Insérer une invitation active SANS jeton Authentik (ou supprimé dans Authentik)
	expiry30d := time.Now().AddDate(0, 0, 30).Format("2006-01-02 15:04:05")
	_, err := db.Exec(`
		INSERT INTO invitations (code, label, max_uses, used_count, expires_at, created_by, authentik_invitation_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "ACTIVE30DAYS", "Invitation 30 jours", 1, 0, expiry30d, "admin", "")
	if err != nil {
		t.Fatalf("Insert active invitation error: %v", err)
	}

	// 2. Insérer une invitation expirée avec un ancien token présent dans Authentik
	expiredDate := time.Now().AddDate(0, 0, -5).Format("2006-01-02 15:04:05")
	oldTokenPK := "old-expired-token-pk"
	mockAuth.tokens[oldTokenPK] = authentik.InvitationTokenResponse{
		PK:   oldTokenPK,
		Name: "JellyGate - EXPIREDCODE",
		FixedData: map[string]interface{}{
			"code": "EXPIREDCODE",
		},
	}
	_, err = db.Exec(`
		INSERT INTO invitations (code, label, max_uses, used_count, expires_at, created_by, authentik_invitation_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "EXPIREDCODE", "Invitation Expirée", 1, 0, expiredDate, "admin", oldTokenPK)
	if err != nil {
		t.Fatalf("Insert expired invitation error: %v", err)
	}

	// Exécuter la réconciliation
	recreated, cleaned, err := svc.ReconcileAuthentik(context.Background())
	if err != nil {
		t.Fatalf("ReconcileAuthentik returned error: %v", err)
	}

	if recreated != 1 {
		t.Errorf("Expected 1 recreated token, got %d", recreated)
	}
	if cleaned != 1 {
		t.Errorf("Expected 1 cleaned token, got %d", cleaned)
	}

	// Vérifier que l'invitation active a bien reçu son nouveau token Authentik en base
	var newAuthID sql.NullString
	_ = db.QueryRow(`SELECT authentik_invitation_id FROM invitations WHERE code = ?`, "ACTIVE30DAYS").Scan(&newAuthID)
	if !newAuthID.Valid || newAuthID.String != "new-stage-token-ACTIVE30DAYS" {
		t.Errorf("Expected authentik_invitation_id to be updated to new-stage-token-ACTIVE30DAYS, got %v", newAuthID)
	}

	// Vérifier que le token expiré a bien été supprimé d'Authentik
	if len(mockAuth.deletedTokens) != 1 || mockAuth.deletedTokens[0] != oldTokenPK {
		t.Errorf("Expected deletedTokens to contain %s, got %+v", oldTokenPK, mockAuth.deletedTokens)
	}
}

func TestSchedulerExecuteTaskSyncAuthentik(t *testing.T) {
	db := newTestDB(t)
	mockAuth := newMockAuthSchedulerClient()

	svc := NewService(db, nil, nil, nil)
	svc.SetAuthentikClient(mockAuth)

	task := TaskRecord{
		ID:       1,
		Name:     "Synchro Authentik Test",
		TaskType: "sync_authentik",
		Enabled:  true,
	}

	err := svc.executeTask(task)
	if err != nil {
		t.Fatalf("executeTask(sync_authentik) error: %v", err)
	}
}
