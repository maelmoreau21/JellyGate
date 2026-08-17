package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
