package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	jgldap "github.com/maelmoreau21/JellyGate/internal/ldap"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

func newTestAdminDataStudioHandler(t *testing.T) (*AdminHandler, *database.DB) {
	t.Helper()
	db, err := database.New(config.DatabaseConfig{Type: "sqlite"}, t.TempDir(), "test-secret-key-0123456789012345")
	if err != nil {
		t.Fatalf("database.New() error = %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return NewAdminHandler(&config.Config{BaseURL: "https://gate.example"}, db, nil, nil, nil, nil), db
}

func decodeAPIData(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var resp APIResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("response success=false: %+v", resp)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("response data = %#v, want object", resp.Data)
	}
	return data
}

func TestPreviewInvitationDoesNotCreateInvitation(t *testing.T) {
	handler, db := newTestAdminDataStudioHandler(t)
	body := []byte(`{"max_uses":1,"expires_in_days":7,"policy_preset_id":"family","email_message":"Bienvenue"}`)

	rec := httptest.NewRecorder()
	handler.PreviewInvitation(rec, newAdminRequest(http.MethodPost, "/admin/api/invitations/preview", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("PreviewInvitation status = %d, body=%s", rec.Code, rec.Body.String())
	}
	data := decodeAPIData(t, rec)
	if data["public_url"] == "" {
		t.Fatalf("preview public_url missing: %#v", data)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(1) FROM invitations`).Scan(&count); err != nil {
		t.Fatalf("count invitations: %v", err)
	}
	if count != 0 {
		t.Fatalf("preview created %d invitation(s), want 0", count)
	}
}

func TestLDAPPageRedirectsToSettingsWhenDisabled(t *testing.T) {
	handler, _ := newTestAdminDataStudioHandler(t)

	rec := httptest.NewRecorder()
	handler.LDAPPage(rec, newAdminRequest(http.MethodGet, "/admin/ldap", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("LDAPPage status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if got := rec.Header().Get("Location"); got != "/admin/settings#ldap" {
		t.Fatalf("LDAPPage redirect = %q, want /admin/settings#ldap", got)
	}
}

func TestPreviewInvitationRejectsAdminPresetForNonAdmin(t *testing.T) {
	handler, db := newTestAdminDataStudioHandler(t)
	if err := db.SaveInvitationProfileConfig(config.InvitationProfileConfig{PolicyPresetID: "admin"}); err != nil {
		t.Fatalf("SaveInvitationProfileConfig() error = %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (jellyfin_id, username, can_invite, is_active) VALUES (?, ?, ?, ?)`, "jf-sponsor", "sponsor", true, true); err != nil {
		t.Fatalf("insert sponsor: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/api/invitations/preview", strings.NewReader(`{"max_uses":1}`))
	req = req.WithContext(session.NewContext(req.Context(), &session.Payload{
		UserID:   "jf-sponsor",
		Username: "sponsor",
		IsAdmin:  false,
	}))
	rec := httptest.NewRecorder()
	handler.PreviewInvitation(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PreviewInvitation status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "administrateur") {
		t.Fatalf("response should mention admin-only preset, body=%s", rec.Body.String())
	}
}

func TestSecurityOverviewAggregatesEvents(t *testing.T) {
	handler, db := newTestAdminDataStudioHandler(t)
	events := []database.SecurityEvent{
		{Category: "invite_abuse", EventType: "invite.ip.blocked", Severity: "critical", IP: "203.0.113.1"},
		{Category: "captcha", EventType: "invite.captcha.failed", Severity: "warning", IP: "203.0.113.2"},
		{Category: "invalid_invite", EventType: "invite.validation.failed", Severity: "warning", IP: "203.0.113.3"},
		{Category: "admin_login", EventType: "admin.login.success", Severity: "info", Actor: "admin"},
		{Category: "admin_login", EventType: "admin.login.failed", Severity: "warning", Actor: "admin"},
		{Category: "smtp", EventType: "invite.email.failed", Severity: "warning"},
	}
	for _, event := range events {
		if err := db.LogSecurityEvent(event); err != nil {
			t.Fatalf("LogSecurityEvent() error = %v", err)
		}
	}

	rec := httptest.NewRecorder()
	handler.SecurityOverview(rec, newAdminRequest(http.MethodGet, "/admin/api/security/overview", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("SecurityOverview status = %d, body=%s", rec.Code, rec.Body.String())
	}
	data := decodeAPIData(t, rec)
	overview := data["overview"].(map[string]interface{})
	if overview["blocked_ips"].(float64) != 1 || overview["smtp_errors"].(float64) != 1 || overview["suspicious_alerts"].(float64) != 1 {
		t.Fatalf("overview = %#v", overview)
	}
}

func TestPendingActionsReturnsExpectedBuckets(t *testing.T) {
	handler, db := newTestAdminDataStudioHandler(t)
	expiry := time.Now().Add(48 * time.Hour).Format("2006-01-02 15:04:05")
	if _, err := db.Exec(`INSERT INTO users (jellyfin_id, username, email, email_verified, pending_email, is_active, access_expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "jf-exp", "expiring", "exp@example.com", true, "", true, expiry); err != nil {
		t.Fatalf("insert expiring user: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (jellyfin_id, username, email, email_verified, pending_email, is_active) VALUES (?, ?, ?, ?, ?, ?)`, "jf-mail", "mail", "mail@example.com", false, "new@example.com", true); err != nil {
		t.Fatalf("insert unverified user: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO invitations (code, label, max_uses, used_count, expires_at, created_by) VALUES (?, ?, ?, ?, ?, ?)`, "ABC123", "Test", 1, 0, expiry, "admin"); err != nil {
		t.Fatalf("insert invitation: %v", err)
	}
	if err := db.LogSecurityEvent(database.SecurityEvent{Category: "smtp", EventType: "invite.email.failed", Severity: "warning"}); err != nil {
		t.Fatalf("LogSecurityEvent() error = %v", err)
	}

	rec := httptest.NewRecorder()
	handler.PendingActions(rec, newAdminRequest(http.MethodGet, "/admin/api/pending-actions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("PendingActions status = %d, body=%s", rec.Code, rec.Body.String())
	}
	data := decodeAPIData(t, rec)
	summary := data["summary"].(map[string]interface{})
	if summary["expiring_accounts"].(float64) != 1 || summary["unverified_emails"].(float64) != 1 || summary["expiring_invitations"].(float64) != 1 || summary["smtp_errors"].(float64) != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestLDAPDryRunUsesSimulatedLookupWithoutWrites(t *testing.T) {
	handler, db := newTestSettingsHandler(t)
	if _, err := db.Exec(`INSERT INTO users (jellyfin_id, username, ldap_dn, is_active) VALUES (?, ?, ?, ?)`, "jf-sync", "sync", "cn=sync,dc=example", true); err != nil {
		t.Fatalf("insert sync user: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (jellyfin_id, username, ldap_dn, is_active) VALUES (?, ?, ?, ?)`, "jf-conflict", "conflict", "cn=old,dc=example", true); err != nil {
		t.Fatalf("insert conflict user: %v", err)
	}

	original := runLDAPDryRunLookup
	runLDAPDryRunLookup = func(cfg config.LDAPConfig, mappings []config.GroupPolicyMapping, limit int) ([]ldapDryRunLookupItem, []string, error) {
		return []ldapDryRunLookupItem{
			{Mapping: mappings[0], User: jgldap.UserEntry{Username: "new", DN: "cn=new,dc=example", Email: "new@example.com"}},
			{Mapping: mappings[0], User: jgldap.UserEntry{Username: "sync", DN: "cn=sync,dc=example", Email: "sync@example.com"}},
			{Mapping: mappings[0], User: jgldap.UserEntry{Username: "conflict", DN: "cn=conflict,dc=example", Email: "conflict@example.com"}},
		}, nil, nil
	}
	t.Cleanup(func() { runLDAPDryRunLookup = original })

	body := []byte(`{
		"host":"ldap.example",
		"bind_dn":"cn=admin,dc=example",
		"bind_password":"secret",
		"base_dn":"dc=example",
		"group_mappings":[{"group_name":"jellyfin","source":"ldap","ldap_group_dn":"cn=jellyfin,dc=example","policy_preset_id":"family"}]
	}`)
	rec := httptest.NewRecorder()
	handler.LDAPDryRun(rec, newAdminRequest(http.MethodPost, "/admin/api/settings/ldap/dry-run", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("LDAPDryRun status = %d, body=%s", rec.Code, rec.Body.String())
	}
	data := decodeAPIData(t, rec)
	summary := data["summary"].(map[string]interface{})
	if summary["would_create"].(float64) != 1 || summary["would_sync"].(float64) != 1 || summary["blocking_conflicts"].(float64) != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}
