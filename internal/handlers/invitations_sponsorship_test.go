package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	createdUsers      []string
	lastStageTokenReq struct {
		Name      string
		ExpiresAt time.Time
		FixedData map[string]interface{}
		SingleUse bool
		Flow      string
	}
}

func (m *mockAuthentikClient) ResolveGroupID(ctx context.Context, nameOrPK string) (string, error) {
	return "00000000-0000-0000-0000-000000000001", nil
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

func (m *mockAuthentikClient) CreateRecoveryLinkByString(ctx context.Context, identifier string) (string, error) {
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
	m.lastStageTokenReq.Name = name
	m.lastStageTokenReq.ExpiresAt = expiresAt
	m.lastStageTokenReq.FixedData = fixedData
	m.lastStageTokenReq.SingleUse = singleUse
	m.lastStageTokenReq.Flow = flow
	return "stage-pk-123", nil
}

func (m *mockAuthentikClient) ListInvitationStageTokens(ctx context.Context) ([]authentik.InvitationTokenResponse, error) {
	return nil, nil
}

func (m *mockAuthentikClient) DeleteInvitationStageToken(ctx context.Context, invitationID string) error {
	return nil
}

func (m *mockAuthentikClient) GetEnrollmentFlowSlug(ctx context.Context, preferred string) string {
	if preferred != "" {
		return preferred
	}
	return "default-enrollment-flow"
}

func (m *mockAuthentikClient) GetBaseURL() string {
	return "https://auth.example.com"
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

	t.Run("InvitePage Redirects to Authentik or Renders Preview", func(t *testing.T) {
		renderEngine, _ := newTestRenderEngine(t)
		invHandler := NewInvitationHandler(cfg, db, nil, nil, nil, renderEngine)
		invHandler.SetAuthentikClient(mockAuthentik)

		_ = db.SavePortalLinksConfig(config.PortalLinksConfig{JellyfinServerName: "MonSuperJellyfin"})
		_ = db.SaveProductFeaturesConfig(config.ProductFeaturesConfig{
			AntiAbuse: config.AntiAbuseConfig{Enabled: false, Captcha: false},
		})

		// Create an invitation
		_, err := db.Exec(`INSERT INTO invitations (code, max_uses, used_count, created_by, expires_at) VALUES ('JG-TESTCODE', 5, 0, 'sponsor_alice', datetime('now', '+7 days'))`)
		if err != nil {
			t.Fatalf("Insert invitation failed: %v", err)
		}

		// 1. Consultation standard : redirection automatique vers Authentik
		req := httptest.NewRequest(http.MethodGet, "/invite/JG-TESTCODE", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("code", "JG-TESTCODE")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		rec := httptest.NewRecorder()
		invHandler.InvitePage(rec, req)

		if rec.Code != http.StatusTemporaryRedirect {
			t.Fatalf("InvitePage returned %d, want %d: %s", rec.Code, http.StatusTemporaryRedirect, rec.Body.String())
		}
		location := rec.Header().Get("Location")
		if !strings.Contains(location, "/if/flow/") || !strings.Contains(location, "itoken=") {
			t.Errorf("Expected redirect Location to contain Authentik flow and itoken, got: %s", location)
		}

		// 2. Mode prévisualisation (?preview=1) : affichage de la page SSO
		reqPreview := httptest.NewRequest(http.MethodGet, "/invite/JG-TESTCODE?preview=1", nil)
		reqPreview = reqPreview.WithContext(context.WithValue(reqPreview.Context(), chi.RouteCtxKey, rctx))
		recPreview := httptest.NewRecorder()
		invHandler.InvitePage(recPreview, reqPreview)

		if recPreview.Code != http.StatusOK {
			t.Fatalf("InvitePage preview returned %d: %s", recPreview.Code, recPreview.Body.String())
		}
		bodyPreview := recPreview.Body.String()
		if !strings.Contains(bodyPreview, "MonSuperJellyfin") {
			t.Errorf("Expected body to contain server name 'MonSuperJellyfin', got: %s", bodyPreview)
		}
		if !strings.Contains(bodyPreview, "authentik-enroll-btn") {
			t.Errorf("Expected body to contain authentik enrollment button")
		}
	})

	t.Run("InviteSubmit Automatically Provisions User in Authentik and Links Sponsor", func(t *testing.T) {
		renderEngine, _ := newTestRenderEngine(t)
		invHandler := NewInvitationHandler(cfg, db, nil, nil, nil, renderEngine)
		invHandler.SetAuthentikClient(mockAuthentik)

		_ = db.SaveProductFeaturesConfig(config.ProductFeaturesConfig{
			AntiAbuse: config.AntiAbuseConfig{Enabled: false, Captcha: false},
		})

		formValues := url.Values{
			"username": []string{"bob_invitee"},
			"email":    []string{"bob@example.com"},
		}
		req := newFormRequest(http.MethodPost, "/invite/JG-TESTCODE", formValues)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("code", "JG-TESTCODE")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		rec := httptest.NewRecorder()
		invHandler.InviteSubmit(rec, req)

		// Should redirect to recovery link or return 200/303
		if rec.Code != http.StatusSeeOther && rec.Code != http.StatusOK {
			t.Fatalf("InviteSubmit failed with status %d: %s", rec.Code, rec.Body.String())
		}

		// Verify user exists in database
		var createdUser struct {
			ID          int64
			Username    string
			Email       string
			AuthentikID string
			InvitedByID int64
		}
		err := db.QueryRow(`SELECT id, username, email, COALESCE(authentik_id, ''), COALESCE(invited_by_id, 0) FROM users WHERE username = 'bob_invitee'`).Scan(
			&createdUser.ID, &createdUser.Username, &createdUser.Email, &createdUser.AuthentikID, &createdUser.InvitedByID,
		)
		if err != nil {
			t.Fatalf("User was not found in DB: %v", err)
		}
		if createdUser.Username != "bob_invitee" || createdUser.Email != "bob@example.com" {
			t.Errorf("Unexpected user values: %+v", createdUser)
		}
		if createdUser.AuthentikID == "" {
			t.Errorf("Expected user to have authentik_id set")
		}
		if createdUser.InvitedByID != sponsorID {
			t.Errorf("Expected invited_by_id=%d, got %d", sponsorID, createdUser.InvitedByID)
		}

		// Verify invitation usage count incremented
		var usedCount int
		_ = db.QueryRow(`SELECT used_count FROM invitations WHERE code = 'JG-TESTCODE'`).Scan(&usedCount)
		if usedCount != 1 {
			t.Errorf("Expected used_count=1, got %d", usedCount)
		}
	})
}

func newFormRequest(method, target string, values url.Values) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestAdminCreateInvitationAuthentikStageToken(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := &config.Config{
		BaseURL:   "http://localhost:8097",
		SecretKey: testAuthSecret,
		Authentik: config.AuthentikConfig{
			Enabled:           true,
			JellyfinUserGroup: "jellyfin-users",
			InvitersGroup:     "jellygate-inviters",
		},
	}

	_ = db.SaveAuthentikConfig(cfg.Authentik)

	mockAuthentik := &mockAuthentikClient{}
	renderEngine, _ := newTestRenderEngine(t)
	adminHandler := NewAdminHandler(cfg, db, nil, mockAuthentik, nil, renderEngine)

	sessCookie, _ := session.Sign(session.Payload{
		UserID:   "1",
		Username: "admin_user",
		IsAdmin:  true,
		Exp:      time.Now().Add(1 * time.Hour).Unix(),
	}, cfg.SecretKey)

	payload := CreateInvitationRequest{
		Label:            "Test Authentik Stage",
		ForcedUsername:   "forced_alice",
		SendToEmail:      "alice@example.com",
		NewUserCanInvite: true,
		MaxUses:          1,
		ExpiresInDays:    7,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/admin/api/invitations", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: sessCookie})
	req = req.WithContext(session.NewContext(req.Context(), &session.Payload{
		UserID:   "1",
		Username: "admin_user",
		IsAdmin:  true,
	}))

	rec := httptest.NewRecorder()
	adminHandler.CreateInvitation(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("CreateInvitation failed status %d: %s", rec.Code, rec.Body.String())
	}

	// Verify Authentik Stage Token creation parameters
	if mockAuthentik.lastStageTokenReq.FixedData == nil {
		t.Fatalf("Expected Authentik stage token to have FixedData")
	}
	if mockAuthentik.lastStageTokenReq.FixedData["username"] != "forced_alice" {
		t.Errorf("Expected FixedData username='forced_alice', got %v", mockAuthentik.lastStageTokenReq.FixedData["username"])
	}
	if mockAuthentik.lastStageTokenReq.FixedData["email"] != "alice@example.com" {
		t.Errorf("Expected FixedData email='alice@example.com', got %v", mockAuthentik.lastStageTokenReq.FixedData["email"])
	}
	if mockAuthentik.lastStageTokenReq.FixedData["sponsor"] != "admin_user" {
		t.Errorf("Expected FixedData sponsor='admin_user', got %v", mockAuthentik.lastStageTokenReq.FixedData["sponsor"])
	}
	groups, ok := mockAuthentik.lastStageTokenReq.FixedData["groups"].([]string)
	if !ok {
		t.Fatalf("Expected FixedData groups to be []string, got %T", mockAuthentik.lastStageTokenReq.FixedData["groups"])
	}
	var hasJellyfin, hasInviters bool
	for _, g := range groups {
		if g == "jellyfin-users" {
			hasJellyfin = true
		}
		if g == "jellygate-inviters" {
			hasInviters = true
		}
	}
	if !hasJellyfin || !hasInviters {
		t.Errorf("Expected groups to contain jellyfin-users and jellygate-inviters, got %v", groups)
	}

	// Verify authentik_invitation_id stored in database
	var storedAuthID string
	err := db.QueryRow(`SELECT COALESCE(authentik_invitation_id, '') FROM invitations WHERE label = 'Test Authentik Stage'`).Scan(&storedAuthID)
	if err != nil {
		t.Fatalf("Failed to query created invitation: %v", err)
	}
	if storedAuthID != "stage-pk-123" {
		t.Errorf("Expected stored authentik_invitation_id='stage-pk-123', got %q", storedAuthID)
	}
}

func TestNonAdminReferralInviteWithAuthentik(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := &config.Config{
		BaseURL:   "http://localhost:8097",
		SecretKey: testAuthSecret,
		Authentik: config.AuthentikConfig{
			Enabled:            true,
			URL:                "https://authentik.local/application/o/jellygate/",
			EnrollmentFlowSlug: "default-enrollment-flow",
			JellyfinUserGroup:  "jellyfin-users",
			InvitersGroup:      "jellygate-inviters",
		},
	}

	mockAuthentik := &mockAuthentikClient{}
	renderEngine, _ := newTestRenderEngine(t)
	adminHandler := NewAdminHandler(cfg, db, nil, mockAuthentik, nil, renderEngine)

	_ = db.SaveJellyfinPolicyPresets([]config.JellyfinPolicyPreset{
		{ID: "default", Name: "Default Profile", EnableAllFolders: true},
	})
	_ = db.SaveInvitationProfileConfig(config.InvitationProfileConfig{
		PolicyPresetID: "default",
	})
	_ = db.SaveAuthentikConfig(cfg.Authentik)

	// Inserer un utilisateur non-administrateur avec droit d'invitation (parrain)
	res, err := db.Exec(`INSERT INTO users (username, email, can_invite, custom_quota) VALUES ('bob_sponsor', 'bob@example.com', 1, 5)`)
	if err != nil {
		t.Fatalf("Insert user failed: %v", err)
	}
	bobID, _ := res.LastInsertId()

	reqPayload := map[string]interface{}{
		"target_preset": "default",
		"max_uses":      2,
		"validity_days": 7,
	}
	body, _ := json.Marshal(reqPayload)

	req := httptest.NewRequest(http.MethodPost, "/admin/api/my-invitations", bytes.NewReader(body))
	req = req.WithContext(session.NewContext(req.Context(), &session.Payload{
		UserID:    fmt.Sprintf("%d", bobID),
		Username:  "bob_sponsor",
		IsAdmin:   false,
		CanInvite: true,
	}))

	rec := httptest.NewRecorder()
	adminHandler.CreateMyInvitation(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("CreateMyInvitation returned status %d: %s", rec.Code, rec.Body.String())
	}

	// Verifier les metadonnees du jeton Stage Authentik genere pour le parrain non-admin
	if mockAuthentik.lastStageTokenReq.FixedData == nil {
		t.Fatalf("Expected FixedData to be populated")
	}
	if mockAuthentik.lastStageTokenReq.FixedData["source"] != "JellyGate" {
		t.Errorf("Expected source='JellyGate', got %v", mockAuthentik.lastStageTokenReq.FixedData["source"])
	}
	if mockAuthentik.lastStageTokenReq.FixedData["created_by"] != "JellyGate" {
		t.Errorf("Expected created_by='JellyGate', got %v", mockAuthentik.lastStageTokenReq.FixedData["created_by"])
	}
	if mockAuthentik.lastStageTokenReq.FixedData["created_by_app"] != "JellyGate" {
		t.Errorf("Expected created_by_app='JellyGate', got %v", mockAuthentik.lastStageTokenReq.FixedData["created_by_app"])
	}
	if mockAuthentik.lastStageTokenReq.FixedData["sponsor"] != "bob_sponsor" {
		t.Errorf("Expected sponsor='bob_sponsor', got %v", mockAuthentik.lastStageTokenReq.FixedData["sponsor"])
	}
	if !strings.HasPrefix(mockAuthentik.lastStageTokenReq.Name, "jellygate-sponsor-bob_sponsor-") {
		t.Errorf("Expected token name to start with 'jellygate-sponsor-bob_sponsor-', got %s", mockAuthentik.lastStageTokenReq.Name)
	}
}

func TestAuthentikURLSanitizationInInvitePage(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := &config.Config{
		BaseURL:   "http://localhost:8097",
		SecretKey: testAuthSecret,
		Authentik: config.AuthentikConfig{
			Enabled:            true,
			URL:                "https://authentik.mydomain.local/application/o/jellygate/",
			EnrollmentFlowSlug: "default-enrollment-flow",
		},
	}

	mockAuthentik := &mockAuthentikClient{}
	renderEngine, _ := newTestRenderEngine(t)
	invHandler := NewInvitationHandler(cfg, db, nil, nil, nil, renderEngine)
	invHandler.SetAuthentikClient(mockAuthentik)

	_ = db.SaveAuthentikConfig(cfg.Authentik)
	_ = db.SaveProductFeaturesConfig(config.ProductFeaturesConfig{
		AntiAbuse: config.AntiAbuseConfig{Enabled: false, Captcha: false},
	})

	_, err := db.Exec(`INSERT INTO invitations (code, max_uses, used_count, created_by, authentik_invitation_id, expires_at) VALUES ('JG-URLTEST', 1, 0, 'admin', 'stage-auth-valid-uuid', datetime('now', '+7 days'))`)
	if err != nil {
		t.Fatalf("Insert invitation failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/invite/JG-URLTEST", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("code", "JG-URLTEST")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	invHandler.InvitePage(rec, req)

	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("InvitePage returned status %d, want %d: %s", rec.Code, http.StatusTemporaryRedirect, rec.Body.String())
	}

	location := rec.Header().Get("Location")
	// L'URL DOIT pointer vers https://authentik.mydomain.local/if/flow/default-enrollment-flow/?itoken=stage-auth-valid-uuid
	// et NE DOIT JAMAIS contenir /application/o/
	if strings.Contains(location, "/application/o/") {
		t.Errorf("Redirection URL contains subpath /application/o/: %s", location)
	}
	expectedPrefix := "https://authentik.mydomain.local/if/flow/default-enrollment-flow/?itoken=stage-auth-valid-uuid"
	if location != expectedPrefix {
		t.Errorf("Expected redirect URL %q, got %q", expectedPrefix, location)
	}
}

type mockFailingAuthentikClient struct {
	mockAuthentikClient
}

func (m *mockFailingAuthentikClient) CreateInvitationStageToken(ctx context.Context, name string, expiresAt time.Time, fixedData map[string]interface{}, singleUse bool, flow string) (string, error) {
	return "", fmt.Errorf("authentik api down")
}

func TestAuthentikStageTokenFailureGracefulFallback(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := &config.Config{
		BaseURL:   "http://localhost:8097",
		SecretKey: testAuthSecret,
		Authentik: config.AuthentikConfig{
			Enabled:            true,
			URL:                "https://authentik.local",
			EnrollmentFlowSlug: "default-enrollment-flow",
		},
	}

	mockFailing := &mockFailingAuthentikClient{}
	renderEngine, _ := newTestRenderEngine(t)
	invHandler := NewInvitationHandler(cfg, db, nil, nil, nil, renderEngine)
	invHandler.SetAuthentikClient(mockFailing)

	_ = db.SaveAuthentikConfig(cfg.Authentik)
	_ = db.SaveProductFeaturesConfig(config.ProductFeaturesConfig{
		AntiAbuse: config.AntiAbuseConfig{Enabled: false, Captcha: false},
	})

	// Invitation SANS jeton Authentik initial
	_, err := db.Exec(`INSERT INTO invitations (code, max_uses, used_count, created_by, expires_at) VALUES ('JG-FAILTEST', 1, 0, 'admin', datetime('now', '+7 days'))`)
	if err != nil {
		t.Fatalf("Insert invitation failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/invite/JG-FAILTEST", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("code", "JG-FAILTEST")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	invHandler.InvitePage(rec, req)

	// Ne doit PAS rediriger vers une page 404 Authentik avec un jeton invalide
	if rec.Code == http.StatusTemporaryRedirect {
		t.Fatalf("InvitePage redirected when token generation failed! Location: %s", rec.Header().Get("Location"))
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("InvitePage returned %d, want 200 OK fallback", rec.Code)
	}
}

func TestInvitePageLookupByAuthentikID(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := &config.Config{
		BaseURL:   "http://localhost:8097",
		SecretKey: testAuthSecret,
		Authentik: config.AuthentikConfig{
			Enabled:            true,
			URL:                "https://authentik.local",
			EnrollmentFlowSlug: "default-enrollment-flow",
		},
	}

	mockAuthentik := &mockAuthentikClient{}
	renderEngine, _ := newTestRenderEngine(t)
	invHandler := NewInvitationHandler(cfg, db, nil, nil, nil, renderEngine)
	invHandler.SetAuthentikClient(mockAuthentik)

	_ = db.SaveAuthentikConfig(cfg.Authentik)
	_ = db.SaveProductFeaturesConfig(config.ProductFeaturesConfig{
		AntiAbuse: config.AntiAbuseConfig{Enabled: false, Captcha: false},
	})

	const authStageUUID = "98765432-1111-2222-3333-444455556666"
	_, err := db.Exec(`INSERT INTO invitations (code, max_uses, used_count, created_by, authentik_invitation_id, expires_at) VALUES ('JG-STAGEUUID', 1, 0, 'admin', ?, datetime('now', '+7 days'))`, authStageUUID)
	if err != nil {
		t.Fatalf("Insert invitation failed: %v", err)
	}

	// Accès direct avec l'authentik_invitation_id
	req := httptest.NewRequest(http.MethodGet, "/invite/"+authStageUUID+"?preview=1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("code", authStageUUID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	invHandler.InvitePage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("InvitePage lookup by authentik_invitation_id returned %d, want 200: %s", rec.Code, rec.Body.String())
	}
}
