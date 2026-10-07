package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
)

func disableCaptcha(t *testing.T, db *database.DB) {
	pCfg := config.DefaultProductFeaturesConfig()
	pCfg.AntiAbuse.Captcha = false
	if err := db.SaveProductFeaturesConfig(pCfg); err != nil {
		t.Fatalf("failed to disable captcha in test: %v", err)
	}
}

func setupTestInviteRouter(h *InvitationHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/invite/{code}", h.InvitePage)
	r.Post("/invite/{code}", h.InviteSubmit)
	return r
}

func TestInviteSubmit_CaptchaRequiredWhenEnabled(t *testing.T) {
	db := newAuthTestDB(t)
	renderEngine, err := newTestRenderEngine(t)
	if err != nil {
		t.Fatalf("newTestRenderEngine failed: %v", err)
	}

	cfg := &config.Config{
		BaseURL:   "http://localhost:8097",
		SecretKey: testAuthSecret,
	}

	pCfg := config.DefaultProductFeaturesConfig()
	pCfg.AntiAbuse.Enabled = true
	pCfg.AntiAbuse.Captcha = true
	_ = db.SaveProductFeaturesConfig(pCfg)

	h := NewInvitationHandler(cfg, db, nil, nil, nil, renderEngine)
	router := setupTestInviteRouter(h)

	form := url.Values{
		"username": {"validuser"},
		"password": {"StrongPass123!"},
	}

	req := httptest.NewRequest(http.MethodPost, "/invite/somecode", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "captcha") {
		t.Fatalf("expected 400 captcha requis, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestInviteSubmit_InvalidCode(t *testing.T) {
	db := newAuthTestDB(t)
	disableCaptcha(t, db)
	renderEngine, err := newTestRenderEngine(t)
	if err != nil {
		t.Fatalf("newTestRenderEngine failed: %v", err)
	}

	cfg := &config.Config{
		BaseURL:   "http://localhost:8097",
		SecretKey: testAuthSecret,
	}

	h := NewInvitationHandler(cfg, db, nil, nil, nil, renderEngine)
	router := setupTestInviteRouter(h)

	form := url.Values{
		"username": {"validuser"},
		"password": {"StrongPass123!"},
	}

	req := httptest.NewRequest(http.MethodPost, "/invite/nonexistent-code", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for invalid code, got %d", rec.Code)
	}
}

func TestInviteSubmit_ExpiredCode(t *testing.T) {
	db := newAuthTestDB(t)
	disableCaptcha(t, db)
	renderEngine, err := newTestRenderEngine(t)
	if err != nil {
		t.Fatalf("newTestRenderEngine failed: %v", err)
	}

	cfg := &config.Config{
		BaseURL:   "http://localhost:8097",
		SecretKey: testAuthSecret,
	}

	// Insert expired invite
	expiredCode := "expired-code-123"
	_, err = db.Exec(`
		INSERT INTO invitations (code, label, max_uses, used_count, expires_at, created_by)
		VALUES (?, ?, 1, 0, ?, 'admin')`,
		expiredCode, "Expired", time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("failed to insert expired invitation: %v", err)
	}

	h := NewInvitationHandler(cfg, db, nil, nil, nil, renderEngine)
	router := setupTestInviteRouter(h)

	form := url.Values{
		"username": {"validuser"},
		"password": {"StrongPass123!"},
	}

	req := httptest.NewRequest(http.MethodPost, "/invite/"+expiredCode, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for expired code, got %d", rec.Code)
	}
}

func TestInviteSubmit_ValidationErrors(t *testing.T) {
	db := newAuthTestDB(t)
	disableCaptcha(t, db)
	renderEngine, err := newTestRenderEngine(t)
	if err != nil {
		t.Fatalf("newTestRenderEngine failed: %v", err)
	}

	cfg := &config.Config{
		BaseURL:   "http://localhost:8097",
		SecretKey: testAuthSecret,
	}

	profile := jellyfin.InviteProfile{
		RequireEmail: true,
	}
	profJSON, _ := json.Marshal(profile)

	code := "validation-test-code"
	_, err = db.Exec(`
		INSERT INTO invitations (code, label, max_uses, used_count, jellyfin_profile, expires_at, created_by)
		VALUES (?, ?, 1, 0, ?, ?, 'admin')`,
		code, "Validation Invite", string(profJSON), time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("failed to insert invitation: %v", err)
	}

	h := NewInvitationHandler(cfg, db, nil, nil, nil, renderEngine)
	router := setupTestInviteRouter(h)

	t.Run("username with spaces fails validation", func(t *testing.T) {
		form := url.Values{
			"username": {"user with space"},
			"email":    {"test@example.com"},
			"password": {"ValidPass123!"},
		}

		req := httptest.NewRequest(http.MethodPost, "/invite/"+code, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		// Handler re-renders invite.html on validation failure with 200
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200 (re-render form), got %d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "user with space") {
			t.Errorf("expected form to preserve submitted username")
		}
	})

	t.Run("missing required email fails validation", func(t *testing.T) {
		form := url.Values{
			"username": {"validuser"},
			"email":    {""},
			"password": {"ValidPass123!"},
		}

		req := httptest.NewRequest(http.MethodPost, "/invite/"+code, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200 (re-render form), got %d", rec.Code)
		}
	})
}

func TestInviteSubmit_SuccessfulLocalCreation(t *testing.T) {
	db := newAuthTestDB(t)
	disableCaptcha(t, db)
	renderEngine, err := newTestRenderEngine(t)
	if err != nil {
		t.Fatalf("newTestRenderEngine failed: %v", err)
	}

	cfg := &config.Config{
		BaseURL:   "http://localhost:8097",
		SecretKey: testAuthSecret,
		Authentik: config.AuthentikConfig{
			Enabled: false,
		},
	}
	_ = db.SaveAuthentikConfig(cfg.Authentik)

	code := "success-local-invite-code"
	_, err = db.Exec(`
		INSERT INTO invitations (code, label, max_uses, used_count, expires_at, created_by)
		VALUES (?, ?, 1, 0, ?, 'admin')`,
		code, "Success Local Invite", time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("failed to insert invitation: %v", err)
	}

	h := NewInvitationHandler(cfg, db, nil, nil, nil, renderEngine)
	router := setupTestInviteRouter(h)

	form := url.Values{
		"username":         {"newuser123"},
		"email":            {"newuser@example.com"},
		"password":         {"MySecretPassword123!"},
		"password_confirm": {"MySecretPassword123!"},
	}

	req := httptest.NewRequest(http.MethodPost, "/invite/"+code, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body=%s", rec.Code, rec.Body.String())
	}

	// Verify that user was created in the database
	var createdUsername string
	err = db.QueryRow("SELECT username FROM users WHERE username = ?", "newuser123").Scan(&createdUsername)
	if err != nil {
		t.Fatalf("expected user newuser123 to be created in DB, got err: %v", err)
	}

	// Verify invitation used_count was incremented
	var usedCount int
	err = db.QueryRow("SELECT used_count FROM invitations WHERE code = ?", code).Scan(&usedCount)
	if err != nil {
		t.Fatalf("failed to query used_count: %v", err)
	}
	if usedCount != 1 {
		t.Fatalf("expected used_count to be 1, got %d", usedCount)
	}
}
