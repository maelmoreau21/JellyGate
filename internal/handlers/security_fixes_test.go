package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

func TestCreateInvitationRejectsPrivilegedFieldsForNonAdmin(t *testing.T) {
	_, db := newTestSettingsHandler(t)
	if _, err := db.Exec(
		`INSERT INTO users (jellyfin_id, username, email, can_invite, is_active)
		 VALUES (?, ?, ?, TRUE, TRUE)`,
		"sponsor-jf", "sponsor", "sponsor@example.com",
	); err != nil {
		t.Fatalf("insert sponsor user: %v", err)
	}

	handler := NewAdminHandler(&config.Config{}, db, nil, nil, nil, nil)
	body, err := json.Marshal(CreateInvitationRequest{MaxUses: 1, GroupName: "admin"})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/api/invitations", bytes.NewReader(body))
	req = req.WithContext(session.NewContext(req.Context(), &session.Payload{
		UserID:   "sponsor-jf",
		Username: "sponsor",
		IsAdmin:  false,
	}))
	rec := httptest.NewRecorder()

	handler.CreateInvitation(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("CreateInvitation status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM invitations`).Scan(&count); err != nil {
		t.Fatalf("count invitations: %v", err)
	}
	if count != 0 {
		t.Fatalf("invitations count = %d, want 0", count)
	}
}

func TestCreateInvitationAllowsSponsorTargetProfile(t *testing.T) {
	_, db := newTestSettingsHandler(t)
	if err := db.SaveJellyfinPolicyPresets([]config.JellyfinPolicyPreset{
		{ID: "member", Name: "Member", EnableAllFolders: true, EnableRemoteAccess: true},
		{ID: "sponsor", Name: "Sponsor", EnableAllFolders: true, EnableRemoteAccess: true, CanCreateInvitations: true, AllowedTargetPresetIDs: []string{"member"}, TargetPresetID: "member"},
	}); err != nil {
		t.Fatalf("SaveJellyfinPolicyPresets() error = %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO users (jellyfin_id, username, email, can_invite, preset_id, is_active)
		 VALUES (?, ?, ?, TRUE, ?, TRUE)`,
		"sponsor-jf", "sponsor", "sponsor@example.com", "sponsor",
	); err != nil {
		t.Fatalf("insert sponsor user: %v", err)
	}

	handler := NewAdminHandler(&config.Config{}, db, nil, nil, nil, nil)
	body, _ := json.Marshal(CreateInvitationRequest{MaxUses: 1, PolicyPresetID: "member"})
	req := httptest.NewRequest(http.MethodPost, "/admin/api/invitations", bytes.NewReader(body))
	req = req.WithContext(session.NewContext(req.Context(), &session.Payload{UserID: "sponsor-jf", Username: "sponsor"}))
	rec := httptest.NewRecorder()

	handler.CreateInvitation(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("CreateInvitation status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var profileID string
	if err := db.QueryRow(`SELECT profile_id FROM invitations LIMIT 1`).Scan(&profileID); err != nil {
		t.Fatalf("read invitation profile_id: %v", err)
	}
	if profileID != "member" {
		t.Fatalf("profile_id = %q, want member", profileID)
	}
}

func TestCreateInvitationRejectsTemporaryWhenSponsorProfileDisallows(t *testing.T) {
	_, db := newTestSettingsHandler(t)
	if err := db.SaveJellyfinPolicyPresets([]config.JellyfinPolicyPreset{
		{ID: "temp", Name: "Temp", EnableAllFolders: true, EnableRemoteAccess: true, IsTemporary: true, DefaultAccountDurationDays: 7},
		{ID: "sponsor", Name: "Sponsor", EnableAllFolders: true, EnableRemoteAccess: true, CanCreateInvitations: true, AllowedTargetPresetIDs: []string{"temp"}, TargetPresetID: "temp"},
	}); err != nil {
		t.Fatalf("SaveJellyfinPolicyPresets() error = %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO users (jellyfin_id, username, email, can_invite, preset_id, is_active)
		 VALUES (?, ?, ?, TRUE, ?, TRUE)`,
		"sponsor-jf", "sponsor", "sponsor@example.com", "sponsor",
	); err != nil {
		t.Fatalf("insert sponsor user: %v", err)
	}

	handler := NewAdminHandler(&config.Config{}, db, nil, nil, nil, nil)
	body, _ := json.Marshal(CreateInvitationRequest{MaxUses: 1, PolicyPresetID: "temp", IsTemporary: true, AccountDurationDays: 7})
	req := httptest.NewRequest(http.MethodPost, "/admin/api/invitations", bytes.NewReader(body))
	req = req.WithContext(session.NewContext(req.Context(), &session.Payload{UserID: "sponsor-jf", Username: "sponsor"}))
	rec := httptest.NewRecorder()

	handler.CreateInvitation(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("CreateInvitation status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestReserveInvitationUseEnforcesMaxUses(t *testing.T) {
	_, db := newTestSettingsHandler(t)
	if _, err := db.Exec(`INSERT INTO invitations (code, max_uses, used_count) VALUES (?, 1, 0)`, "quota-once"); err != nil {
		t.Fatalf("insert invitation: %v", err)
	}
	handler := NewInvitationHandler(&config.Config{}, db, nil, nil, nil, nil)
	inv, err := handler.getValidInvitation("quota-once")
	if err != nil {
		t.Fatalf("getValidInvitation: %v", err)
	}

	if err := handler.reserveInvitationUse(inv); err != nil {
		t.Fatalf("first reserveInvitationUse() error = %v", err)
	}
	if err := handler.reserveInvitationUse(inv); err == nil {
		t.Fatalf("second reserveInvitationUse() error = nil, want quota failure")
	}
	var used int
	if err := db.QueryRow(`SELECT used_count FROM invitations WHERE code = ?`, "quota-once").Scan(&used); err != nil {
		t.Fatalf("read used_count: %v", err)
	}
	if used != 1 {
		t.Fatalf("used_count = %d, want 1", used)
	}
}

func TestInvitationReservationReleasePolicy(t *testing.T) {
	if shouldReleaseInvitationReservation(errors.New("plain error")) {
		t.Fatalf("plain errors must not release an invitation reservation")
	}
	if !shouldReleaseInvitationReservation(inviteSignupFailure(errors.New("rollback clean"), true)) {
		t.Fatalf("clean rollback should release an invitation reservation")
	}
	if shouldReleaseInvitationReservation(inviteSignupFailure(errors.New("rollback failed"), false)) {
		t.Fatalf("failed rollback must keep the invitation reservation consumed")
	}
}

func TestSecurityFixes_AuthentikID_AccountResolution(t *testing.T) {
	_, db := newTestSettingsHandler(t)
	if _, err := db.Exec(
		`INSERT INTO users (authentik_id, username, email, is_active)
		 VALUES (?, ?, ?, TRUE)`,
		"ak-user-uuid-12345", "testauthentikuser", "authentik@example.com",
	); err != nil {
		t.Fatalf("insert authentik user: %v", err)
	}

	handler := NewAdminHandler(&config.Config{}, db, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/admin/api/users/me", nil)
	req = req.WithContext(session.NewContext(req.Context(), &session.Payload{
		UserID:      "ak-user-uuid-12345",
		AuthentikID: "ak-user-uuid-12345",
		Username:    "testauthentikuser",
		Email:       "authentik@example.com",
		IsAdmin:     false,
	}))
	rec := httptest.NewRecorder()

	handler.GetMyAccount(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GetMyAccount status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("resp.Success = false, want true; message=%s", resp.Message)
	}
}

func TestSyncAuthentikInvitationsRequiresAdmin(t *testing.T) {
	_, db := newTestSettingsHandler(t)
	handler := NewAdminHandler(&config.Config{}, db, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/admin/api/invitations/sync-authentik", nil)
	// Non-admin session
	req = req.WithContext(session.NewContext(req.Context(), &session.Payload{
		UserID:   "10",
		Username: "standarduser",
		IsAdmin:  false,
	}))
	rec := httptest.NewRecorder()

	handler.SyncAuthentikInvitations(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("SyncAuthentikInvitations for non-admin status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestSettingsGetAllMasksAuthentikSecrets(t *testing.T) {
	handler, db := newTestSettingsHandler(t)
	cfg := config.AuthentikConfig{
		Enabled:      true,
		URL:          "https://authentik.example.com",
		ClientSecret: "super-secret-oidc-client-key",
		APIToken:     "super-secret-authentik-api-token",
	}
	if err := db.SaveAuthentikConfig(cfg); err != nil {
		t.Fatalf("SaveAuthentikConfig error = %v", err)
	}

	req := newAdminRequest(http.MethodGet, "/admin/api/settings", nil)
	rec := httptest.NewRecorder()

	handler.GetAll(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GetAll status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Authentik struct {
				ClientSecret string `json:"oidc_client_secret"`
				APIToken     string `json:"authentik_api_token"`
			} `json:"authentik"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal GetAll response: %v", err)
	}

	if resp.Data.Authentik.ClientSecret != maskedSecretValue {
		t.Fatalf("ClientSecret = %q, want masked %q", resp.Data.Authentik.ClientSecret, maskedSecretValue)
	}
	if resp.Data.Authentik.APIToken != maskedSecretValue {
		t.Fatalf("APIToken = %q, want masked %q", resp.Data.Authentik.APIToken, maskedSecretValue)
	}
}

func TestSaveAuthentikAndJellyfinEncryptedAtRest(t *testing.T) {
	_, db := newTestSettingsHandler(t)

	authCfg := config.AuthentikConfig{
		Enabled:      true,
		ClientSecret: "secret-oidc-123",
		APIToken:     "token-api-456",
	}
	if err := db.SaveAuthentikConfig(authCfg); err != nil {
		t.Fatalf("SaveAuthentikConfig error = %v", err)
	}

	var rawAuth string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, "authentik_config").Scan(&rawAuth); err != nil {
		t.Fatalf("query raw authentik_config: %v", err)
	}
	if !strings.HasPrefix(rawAuth, "enc:") {
		t.Fatalf("raw authentik_config in DB should start with enc:, got: %s", rawAuth)
	}

	decryptedAuth, err := db.GetAuthentikConfig()
	if err != nil {
		t.Fatalf("GetAuthentikConfig error = %v", err)
	}
	if decryptedAuth.ClientSecret != "secret-oidc-123" || decryptedAuth.APIToken != "token-api-456" {
		t.Fatalf("decrypted values mismatch: %+v", decryptedAuth)
	}

	jfCfg := config.JellyfinConfig{
		URL:    "http://jellyfin:8096",
		APIKey: "jellyfin-api-secret-key",
	}
	if err := db.SaveJellyfinConfig(jfCfg); err != nil {
		t.Fatalf("SaveJellyfinConfig error = %v", err)
	}

	var rawJf string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, "jellyfin_config").Scan(&rawJf); err != nil {
		t.Fatalf("query raw jellyfin_config: %v", err)
	}
	if !strings.HasPrefix(rawJf, "enc:") {
		t.Fatalf("raw jellyfin_config in DB should start with enc:, got: %s", rawJf)
	}

	decryptedJf, err := db.GetJellyfinConfig()
	if err != nil {
		t.Fatalf("GetJellyfinConfig error = %v", err)
	}
	if decryptedJf.APIKey != "jellyfin-api-secret-key" {
		t.Fatalf("decrypted Jellyfin API key mismatch: %s", decryptedJf.APIKey)
	}
}

func TestPreventAdminSelfLockout(t *testing.T) {
	_, db := newTestSettingsHandler(t)
	res, err := db.Exec(`INSERT INTO users (username, email, is_active, is_banned) VALUES ('myadmin', 'admin@example.com', 1, 0)`)
	if err != nil {
		t.Fatalf("insert admin user: %v", err)
	}
	adminID, _ := res.LastInsertId()

	handler := NewAdminHandler(&config.Config{}, db, nil, nil, nil, nil)
	adminSess := &session.Payload{
		UserID:   "1",
		Username: "myadmin",
		IsAdmin:  true,
	}

	// 1. Test self-ban rejection
	reqBan := httptest.NewRequest(http.MethodPost, "/admin/api/users/"+strconv.FormatInt(adminID, 10)+"/ban", nil)
	reqBan = reqBan.WithContext(session.NewContext(reqBan.Context(), adminSess))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.FormatInt(adminID, 10))
	reqBan = reqBan.WithContext(context.WithValue(reqBan.Context(), chi.RouteCtxKey, rctx))
	recBan := httptest.NewRecorder()
	handler.BanUser(recBan, reqBan)
	if recBan.Code != http.StatusBadRequest {
		t.Fatalf("BanUser self-ban status = %d, want %d", recBan.Code, http.StatusBadRequest)
	}

	// 2. Test self-delete rejection
	reqDel := httptest.NewRequest(http.MethodDelete, "/admin/api/users/"+strconv.FormatInt(adminID, 10), nil)
	reqDel = reqDel.WithContext(session.NewContext(reqDel.Context(), adminSess))
	reqDel = reqDel.WithContext(context.WithValue(reqDel.Context(), chi.RouteCtxKey, rctx))
	recDel := httptest.NewRecorder()
	handler.DeleteUser(recDel, reqDel)
	if recDel.Code != http.StatusBadRequest {
		t.Fatalf("DeleteUser self-delete status = %d, want %d", recDel.Code, http.StatusBadRequest)
	}

	// 3. Test self-toggle rejection when active
	reqToggle := httptest.NewRequest(http.MethodPost, "/admin/api/users/"+strconv.FormatInt(adminID, 10)+"/toggle", nil)
	reqToggle = reqToggle.WithContext(session.NewContext(reqToggle.Context(), adminSess))
	reqToggle = reqToggle.WithContext(context.WithValue(reqToggle.Context(), chi.RouteCtxKey, rctx))
	recToggle := httptest.NewRecorder()
	handler.ToggleUser(recToggle, reqToggle)
	if recToggle.Code != http.StatusBadRequest {
		t.Fatalf("ToggleUser self-toggle status = %d, want %d", recToggle.Code, http.StatusBadRequest)
	}
}

func TestBannedOrInactiveUserSessionRejected(t *testing.T) {
	_, db := newTestSettingsHandler(t)
	_, err := db.Exec(`INSERT INTO users (authentik_id, username, email, is_active, is_banned) VALUES ('banned-sub', 'banneduser', 'banned@example.com', 0, 1)`)
	if err != nil {
		t.Fatalf("insert banned user: %v", err)
	}

	authHandler := NewAuthHandler(&config.Config{}, db, nil, nil, nil)
	bannedSess := &session.Payload{
		UserID:      "99",
		AuthentikID: "banned-sub",
		Username:    "banneduser",
		IsAdmin:     false,
	}

	if authHandler.sessionAccepted(bannedSess) {
		t.Fatalf("sessionAccepted must return false for banned user")
	}
}

func TestLocalLoginRejectsEmptyUsername(t *testing.T) {
	cfg := &config.Config{
		LocalAdmin: config.LocalAdminConfig{
			Enabled:  true,
			Username: "admin",
			Password: "localpassword123",
		},
	}
	authHandler := NewAuthHandler(cfg, nil, nil, nil, nil)

	form := url.Values{}
	form.Set("username", "")
	form.Set("password", "localpassword123")
	req := httptest.NewRequest(http.MethodPost, "/local", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	authHandler.LocalLoginSubmit(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("LocalLoginSubmit with empty username status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestDeleteInvitationIDORPrevention(t *testing.T) {
	_, db := newTestSettingsHandler(t)
	res, err := db.Exec(`INSERT INTO invitations (code, created_by, max_uses, used_count, authentik_invitation_id) VALUES ('code-abc', 'admin_user', 1, 0, 'tok-auth-456')`)
	if err != nil {
		t.Fatalf("insert test invitation: %v", err)
	}
	invID, _ := res.LastInsertId()

	handler := NewAdminHandler(&config.Config{}, db, nil, nil, nil, nil)

	// 1. Non-admin attacker tries to delete admin's invitation
	req := httptest.NewRequest(http.MethodDelete, "/admin/api/invitations/"+strconv.FormatInt(invID, 10), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", strconv.FormatInt(invID, 10))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = req.WithContext(session.NewContext(req.Context(), &session.Payload{
		UserID:   "user-2",
		Username: "other_user",
		IsAdmin:  false,
	}))
	rec := httptest.NewRecorder()

	handler.DeleteInvitation(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("DeleteInvitation by unauthorized user status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	// Verify invitation still exists in DB
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM invitations WHERE id = ?`, invID).Scan(&count)
	if count != 1 {
		t.Fatalf("Invitation should still exist in DB, got count = %d", count)
	}

	// 2. Admin successfully deletes the invitation
	adminReq := httptest.NewRequest(http.MethodDelete, "/admin/api/invitations/"+strconv.FormatInt(invID, 10), nil)
	adminReq = adminReq.WithContext(context.WithValue(adminReq.Context(), chi.RouteCtxKey, rctx))
	adminReq = adminReq.WithContext(session.NewContext(adminReq.Context(), &session.Payload{
		UserID:   "admin-1",
		Username: "admin_user",
		IsAdmin:  true,
	}))
	adminRec := httptest.NewRecorder()

	handler.DeleteInvitation(adminRec, adminReq)

	if adminRec.Code != http.StatusOK {
		t.Fatalf("DeleteInvitation by admin status = %d, want %d", adminRec.Code, http.StatusOK)
	}

	_ = db.QueryRow(`SELECT COUNT(*) FROM invitations WHERE id = ?`, invID).Scan(&count)
	if count != 0 {
		t.Fatalf("Invitation should be deleted, got count = %d", count)
	}
}
