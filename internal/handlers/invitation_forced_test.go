package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
)

func TestValidateInviteUsername_EnforcesForcedUsername(t *testing.T) {
	db := newAuthTestDB(t)
	renderEngine, _ := newTestRenderEngine(t)
	h := NewInvitationHandler(nil, db, nil, nil, nil, renderEngine)
	req := httptest.NewRequest(http.MethodPost, "/invite/test", nil)

	profile := &jellyfin.InviteProfile{
		ForcedUsername: "john_doe",
	}

	// 1. Should succeed if username matches exactly
	if err := h.validateInviteUsername(req, "john_doe", profile); err != nil {
		t.Fatalf("expected john_doe to pass, got err: %v", err)
	}

	// 2. Should succeed if username matches case-insensitively
	if err := h.validateInviteUsername(req, "JOHN_DOE", profile); err != nil {
		t.Fatalf("expected JOHN_DOE to pass case-insensitively, got err: %v", err)
	}

	// 3. Should fail if user attempts to submit a different username
	if err := h.validateInviteUsername(req, "hacker_bob", profile); err == nil {
		t.Fatalf("expected hacker_bob to be rejected when ForcedUsername is john_doe")
	}
}

func TestInvitePage_PassesForcedUsernameToTemplateData(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := &config.Config{
		BaseURL:   "http://localhost:8097",
		SecretKey: testAuthSecret,
		Authentik: config.AuthentikConfig{
			Enabled: false,
		},
	}
	renderEngine, _ := newTestRenderEngine(t)
	h := NewInvitationHandler(cfg, db, nil, nil, nil, renderEngine)

	profile := jellyfin.InviteProfile{
		ForcedUsername: "forced_user_99",
		RequireEmail:   false,
	}
	profJSON, _ := json.Marshal(profile)

	code := "forced-invite-code-123"
	_, err := db.Exec(`
		INSERT INTO invitations (code, label, max_uses, used_count, jellyfin_profile, expires_at, created_by)
		VALUES (?, ?, 1, 0, ?, ?, 'admin')`,
		code, "Forced Invite", string(profJSON), time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("failed to insert invitation: %v", err)
	}

	r := chi.NewRouter()
	r.Get("/invite/{code}", h.InvitePage)

	req := httptest.NewRequest(http.MethodGet, "/invite/"+code, nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body=%s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	// Verify that the forced username is rendered in the HTML and input has readonly attribute
	if !containsStr(body, "forced_user_99") {
		t.Errorf("expected body to contain 'forced_user_99', got:\n%s", body)
	}
	if !containsStr(body, "readonly") {
		t.Errorf("expected body to contain 'readonly' attribute on input, got:\n%s", body)
	}
}

func TestInvitePage_AuthentikOnTheFlyTokenCarriesForcedUsername(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := &config.Config{
		BaseURL:   "http://localhost:8097",
		SecretKey: testAuthSecret,
		Authentik: config.AuthentikConfig{
			Enabled: true,
			URL:     "http://authentik.local",
		},
	}
	_ = db.SaveAuthentikConfig(cfg.Authentik)

	mockAuth := &mockAuthentikClient{}
	renderEngine, _ := newTestRenderEngine(t)
	h := NewInvitationHandler(cfg, db, nil, nil, nil, renderEngine)
	h.SetAuthentikClient(mockAuth)

	profile := jellyfin.InviteProfile{
		ForcedUsername: "authentik_forced_sam",
	}
	profJSON, _ := json.Marshal(profile)

	code := "authentik-dynamic-token-code"
	_, err := db.Exec(`
		INSERT INTO invitations (code, label, max_uses, used_count, jellyfin_profile, expires_at, created_by)
		VALUES (?, ?, 1, 0, ?, ?, 'admin')`,
		code, "Authentik Dynamic Invite", string(profJSON), time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("failed to insert invitation: %v", err)
	}

	r := chi.NewRouter()
	r.Get("/invite/{code}", h.InvitePage)

	// Mode preview=1 to test without immediate redirect
	req := httptest.NewRequest(http.MethodGet, "/invite/"+code+"?preview=1", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body=%s", rec.Code, rec.Body.String())
	}

	if mockAuth.lastStageTokenReq.FixedData == nil {
		t.Fatalf("expected Authentik stage token to have FixedData")
	}
	if mockAuth.lastStageTokenReq.FixedData["username"] != "authentik_forced_sam" {
		t.Errorf("expected FixedData username='authentik_forced_sam', got %v", mockAuth.lastStageTokenReq.FixedData["username"])
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && len(sub) > 0 && searchSubstr(s, sub)))
}

func searchSubstr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
