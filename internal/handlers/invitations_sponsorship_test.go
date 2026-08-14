package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/maelmoreau21/JellyGate/internal/authentik"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

type mockAuthentikClient struct {
	createdUsers []string
}

func (m *mockAuthentikClient) CreateUser(ctx context.Context, payload authentik.UserCreatePayload) (*authentik.UserResponse, error) {
	m.createdUsers = append(m.createdUsers, payload.Username)
	return &authentik.UserResponse{
		PK:       42,
		ID:       "uuid-auth-" + payload.Username,
		Username: payload.Username,
		Email:    payload.Email,
		IsActive: true,
	}, nil
}

func (m *mockAuthentikClient) CreateRecoveryLink(ctx context.Context, userPK int64) (string, error) {
	return "http://localhost:9000/recovery", nil
}

func (m *mockAuthentikClient) AddUserToGroup(ctx context.Context, userPK int64, groupID string) error {
	return nil
}
func (m *mockAuthentikClient) RemoveUserFromGroup(ctx context.Context, userPK int64, groupID string) error {
	return nil
}
func (m *mockAuthentikClient) AddUserToGroupByString(ctx context.Context, authentikID string, groupID string) error {
	return nil
}
func (m *mockAuthentikClient) RemoveUserFromGroupByString(ctx context.Context, authentikID string, groupID string) error {
	return nil
}

func (m *mockAuthentikClient) SetUserActiveStatus(ctx context.Context, userPK int64, active bool) error {
	return nil
}

func (m *mockAuthentikClient) SetUserActiveStatusByString(ctx context.Context, authentikID string, active bool) error {
	return nil
}

func (m *mockAuthentikClient) DeleteUser(ctx context.Context, userPK int64) error {
	return nil
}

func (m *mockAuthentikClient) ListUsers(ctx context.Context) ([]authentik.UserResponse, error) {
	return nil, nil
}
func (m *mockAuthentikClient) GetUserByUsername(ctx context.Context, username string) (*authentik.UserDetailResponse, error) {
	return &authentik.UserDetailResponse{
		PK:       42,
		Username: username,
		IsActive: true,
		Groups:   []string{"jellygate-users"},
	}, nil
}

func (m *mockAuthentikClient) CheckHealth(ctx context.Context, cfg config.AuthentikConfig) *authentik.HealthCheckResult {
	return &authentik.HealthCheckResult{OverallStatus: "ok"}
}
func (m *mockAuthentikClient) CheckAPI(ctx context.Context) authentik.HealthComponent {
	return authentik.HealthComponent{Status: "ok"}
}
func (m *mockAuthentikClient) CheckOIDC(ctx context.Context, issuerURL string) authentik.HealthComponent {
	return authentik.HealthComponent{Status: "ok"}
}
func (m *mockAuthentikClient) CheckEnrollment(ctx context.Context, flowSlug string) authentik.HealthComponent {
	return authentik.HealthComponent{Status: "ok"}
}
func (m *mockAuthentikClient) CheckGroups(ctx context.Context, groups []string) authentik.HealthComponent {
	return authentik.HealthComponent{Status: "ok"}
}
func (m *mockAuthentikClient) DeleteUserByString(ctx context.Context, authentikID string) error {
	return nil
}

func (m *mockAuthentikClient) CreateInvitationStageToken(ctx context.Context, name string, expiresAt time.Time, fixedData map[string]interface{}, singleUse bool, flow string) (string, error) {
	return "stage-pk-123", nil
}

func (m *mockAuthentikClient) ListInvitationStageTokens(ctx context.Context) ([]authentik.InvitationTokenResponse, error) {
	return nil, nil
}

func (m *mockAuthentikClient) DeleteInvitationStageToken(ctx context.Context, invitationID string) error {
	return nil
}

func TestSponsorshipAndQuotaWorkflow(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := &config.Config{
		BaseURL:   "http://localhost:8097",
		SecretKey: testAuthSecret,
		Authentik: config.AuthentikConfig{
			Enabled: true,
		},
	}

	mockAuthentik := &mockAuthentikClient{}

	// Save default policy preset & invitation config
	_ = db.SaveJellyfinPolicyPresets([]config.JellyfinPolicyPreset{
		{ID: "default", Name: "Default Profile", EnableAllFolders: true},
	})
	_ = db.SaveInvitationProfileConfig(config.InvitationProfileConfig{
		PolicyPresetID: "default",
	})

	// Insert sponsor user
	res, err := db.Exec(`INSERT INTO users (username, email, can_invite) VALUES ('sponsor_alice', 'alice@example.com', 1)`)
	if err != nil {
		t.Fatalf("Insert sponsor user failed: %v", err)
	}
	sponsorID, _ := res.LastInsertId()

	t.Run("Quota Calculation Engine", func(t *testing.T) {
		calc, err := db.CalculateUserQuota(context.Background(), sponsorID)
		if err != nil {
			t.Fatalf("CalculateUserQuota failed: %v", err)
		}
		if calc.RemainingQuota <= 0 {
			t.Errorf("Sponsor should have remaining quota, got %d", calc.RemainingQuota)
		}
	})

	t.Run("Create Sponsor Invitation", func(t *testing.T) {
		renderEngine, _ := newTestRenderEngine(t)
		adminHandler := NewAdminHandler(cfg, db, nil, mockAuthentik, nil, renderEngine)

		sessCookie, _ := session.Sign(session.Payload{
			UserID:   "1",
			Username: "sponsor_alice",
			IsAdmin:  false,
			Exp:      time.Now().Add(1 * time.Hour).Unix(),
		}, cfg.SecretKey)

		req := httptest.NewRequest(http.MethodPost, "/admin/api/users/me/invitations", nil)
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: sessCookie})
		req = req.WithContext(session.NewContext(req.Context(), &session.Payload{
			UserID:   "1",
			Username: "sponsor_alice",
		}))

		rec := httptest.NewRecorder()
		adminHandler.CreateMyInvitation(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("CreateMyInvitation failed status %d: %s", rec.Code, rec.Body.String())
		}

		var resp APIResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if !resp.Success {
			t.Fatalf("Expected success response, got: %v", resp.Message)
		}

		dataMap, ok := resp.Data.(map[string]interface{})
		if !ok || dataMap["code"] == nil {
			t.Fatalf("Expected code in response data: %+v", resp.Data)
		}
	})

	t.Run("Admin Set Quota Overrides", func(t *testing.T) {
		renderEngine, _ := newTestRenderEngine(t)
		adminHandler := NewAdminHandler(cfg, db, nil, mockAuthentik, nil, renderEngine)

		payloadData, _ := json.Marshal(map[string]interface{}{
			"bonus_quota": 5,
			"malus_quota": 0,
		})

		req := httptest.NewRequest(http.MethodPost, "/admin/api/users/1/quota", bytes.NewReader(payloadData))
		req.Header.Set("Content-Type", "application/json")

		// Attach Chi URL param "id"
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		rec := httptest.NewRecorder()
		adminHandler.SetUserQuota(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("SetUserQuota failed with status %d: %s", rec.Code, rec.Body.String())
		}

		calc, err := db.CalculateUserQuota(context.Background(), sponsorID)
		if err != nil {
			t.Fatalf("CalculateUserQuota failed: %v", err)
		}
		if calc.BonusQuota != 5 {
			t.Errorf("Expected BonusQuota=5, got %d", calc.BonusQuota)
		}
	})

	t.Run("Get Referral Tree", func(t *testing.T) {
		renderEngine, _ := newTestRenderEngine(t)
		adminHandler := NewAdminHandler(cfg, db, nil, mockAuthentik, nil, renderEngine)

		req := httptest.NewRequest(http.MethodGet, "/admin/api/users/referrals", nil)
		rec := httptest.NewRecorder()

		adminHandler.GetReferrals(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GetReferrals failed status %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func newFormRequest(method, target string, values url.Values) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}
