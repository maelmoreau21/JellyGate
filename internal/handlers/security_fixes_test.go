package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
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

func TestReserveInvitationUseEnforcesMaxUses(t *testing.T) {
	_, db := newTestSettingsHandler(t)
	if _, err := db.Exec(`INSERT INTO invitations (code, max_uses, used_count) VALUES (?, 1, 0)`, "quota-once"); err != nil {
		t.Fatalf("insert invitation: %v", err)
	}
	handler := NewInvitationHandler(&config.Config{}, db, nil, nil, nil, nil, nil, nil)
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

func TestInviteVerificationGetDoesNotCreatePendingAccount(t *testing.T) {
	_, db := newTestSettingsHandler(t)
	profile, err := json.Marshal(jellyfin.InviteProfile{
		RequireEmail:             true,
		RequireEmailVerification: true,
		PasswordMinLength:        8,
	})
	if err != nil {
		t.Fatalf("marshal profile: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO invitations (code, max_uses, used_count, jellyfin_profile)
		 VALUES (?, 1, 0, ?)`,
		"invite-code", string(profile),
	); err != nil {
		t.Fatalf("insert invitation: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO pending_invite_signups (code, invitation_code, username, email, password_ciphertext, expires_at, used)
		 VALUES (?, ?, ?, ?, ?, ?, FALSE)`,
		"verify-token", "invite-code", "pendinguser", "pending@example.com", "", time.Now().Add(time.Hour).Format("2006-01-02 15:04:05"),
	); err != nil {
		t.Fatalf("insert pending signup: %v", err)
	}

	handler := NewInvitationHandler(&config.Config{}, db, nil, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/verify-email/verify-token", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("code", "verify-token")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	rec := httptest.NewRecorder()
	handler.VerifyEmailPage(rec, req)

	var userCount, usedCount int
	var pendingUsed bool
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&userCount); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if err := db.QueryRow(`SELECT used_count FROM invitations WHERE code = ?`, "invite-code").Scan(&usedCount); err != nil {
		t.Fatalf("read invitation used_count: %v", err)
	}
	if err := db.QueryRow(`SELECT used FROM pending_invite_signups WHERE code = ?`, "verify-token").Scan(&pendingUsed); err != nil && err != sql.ErrNoRows {
		t.Fatalf("read pending used: %v", err)
	}
	if userCount != 0 || usedCount != 0 || pendingUsed {
		t.Fatalf("GET side effects: users=%d used_count=%d pending_used=%v, want zero/false", userCount, usedCount, pendingUsed)
	}
}

func TestConsumeResetTokenIsSingleUse(t *testing.T) {
	_, db := newTestSettingsHandler(t)
	if _, err := db.Exec(
		`INSERT INTO users (jellyfin_id, username, email, is_active)
		 VALUES (?, ?, ?, TRUE)`,
		"reset-jf", "resetuser", "reset@example.com",
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var userID int64
	if err := db.QueryRow(`SELECT id FROM users WHERE username = ?`, "resetuser").Scan(&userID); err != nil {
		t.Fatalf("read user id: %v", err)
	}
	res, err := db.Exec(
		`INSERT INTO password_resets (user_id, code, used, expires_at)
		 VALUES (?, ?, FALSE, ?)`,
		userID, "reset-token", time.Now().Add(time.Hour).Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		t.Fatalf("insert reset token: %v", err)
	}
	resetID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	handler := NewPasswordResetHandler(&config.Config{}, db, nil, nil, nil, nil)

	if err := handler.consumeResetToken(resetID); err != nil {
		t.Fatalf("first consumeResetToken() error = %v", err)
	}
	if err := handler.consumeResetToken(resetID); err == nil {
		t.Fatalf("second consumeResetToken() error = nil, want replay failure")
	}
}
