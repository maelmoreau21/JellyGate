package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

func newTestSettingsHandler(t *testing.T) (*SettingsHandler, *database.DB) {
	t.Helper()

	db, err := database.New(config.DatabaseConfig{Type: "sqlite"}, t.TempDir(), "test-secret-key-0123456789012345")
	if err != nil {
		t.Fatalf("database.New() error = %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	return NewSettingsHandler(db, ""), db
}

func newAdminRequest(method, target string, body []byte) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	return req.WithContext(session.NewContext(req.Context(), &session.Payload{
		Username: "admin",
		IsAdmin:  true,
	}))
}

func TestSettingsHandlerGetAllReturnsAllSupportedEmailLanguages(t *testing.T) {
	handler, _ := newTestSettingsHandler(t)

	rec := httptest.NewRecorder()
	handler.GetAll(rec, newAdminRequest(http.MethodGet, "/admin/api/settings", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GetAll status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Success bool             `json:"success"`
		Data    settingsResponse `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("GetAll success = false")
	}
	if got, want := len(resp.Data.EmailTemplatesByLang), len(config.SupportedLanguageTags()); got != want {
		t.Fatalf("email_templates_by_lang count = %d, want %d", got, want)
	}
	if resp.Data.PortalLinks.JellyfinServerName != "Jellyfin" {
		t.Fatalf("default Jellyfin server name = %q, want %q", resp.Data.PortalLinks.JellyfinServerName, "Jellyfin")
	}
}

func TestSettingsHandlerSaveEmailTemplatesSyncsSharedFields(t *testing.T) {
	handler, db := newTestSettingsHandler(t)

	templates, err := db.GetEmailTemplatesConfigByLanguage()
	if err != nil {
		t.Fatalf("GetEmailTemplatesConfigByLanguage() error = %v", err)
	}

	fr := templates["fr"]
	en := templates["en"]

	fr.BaseTemplateHeader = "<fr-header>"
	fr.BaseTemplateFooter = "<fr-footer>"
	fr.EmailLogoURL = "/fr.svg"
	fr.DisableConfirmationEmail = true
	fr.ExpiryReminderDays = 14
	fr.ConfirmationSubject = "Sujet FR"
	fr.Confirmation = "Bonjour {{.Username}}"

	en.BaseTemplateHeader = "<en-header>"
	en.BaseTemplateFooter = "<en-footer>"
	en.EmailLogoURL = "/en.svg"
	en.DisableConfirmationEmail = false
	en.ExpiryReminderDays = 3
	en.ConfirmationSubject = "Subject EN"
	en.Confirmation = "Hello {{.Username}}"

	payload := saveEmailTemplatesInput{
		Language: "fr",
		TemplatesByLang: map[string]config.EmailTemplatesConfig{
			"fr": fr,
			"en": en,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	rec := httptest.NewRecorder()
	handler.SaveEmailTemplates(rec, newAdminRequest(http.MethodPost, "/admin/api/settings/email-templates", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("SaveEmailTemplates status = %d, want %d", rec.Code, http.StatusOK)
	}

	saved, err := db.GetEmailTemplatesConfigByLanguage()
	if err != nil {
		t.Fatalf("GetEmailTemplatesConfigByLanguage() after save error = %v", err)
	}

	gotFR := saved["fr"]
	gotEN := saved["en"]

	if gotEN.EmailLogoURL != gotFR.EmailLogoURL || gotEN.EmailLogoURL != "/fr.svg" {
		t.Fatalf("shared logo not synced: fr=%q en=%q", gotFR.EmailLogoURL, gotEN.EmailLogoURL)
	}
	if gotEN.BaseTemplateHeader != "<fr-header>" || gotEN.BaseTemplateFooter != "<fr-footer>" {
		t.Fatalf("shared base template not synced: header=%q footer=%q", gotEN.BaseTemplateHeader, gotEN.BaseTemplateFooter)
	}
	if !gotEN.DisableConfirmationEmail || !gotFR.DisableConfirmationEmail {
		t.Fatalf("disable_confirmation_email should be shared across languages")
	}
	if gotEN.ExpiryReminderDays != 14 || gotFR.ExpiryReminderDays != 14 {
		t.Fatalf("expiry_reminder_days should be shared across languages: fr=%d en=%d", gotFR.ExpiryReminderDays, gotEN.ExpiryReminderDays)
	}
	if gotFR.ConfirmationSubject != "Sujet FR" || gotEN.ConfirmationSubject != "Subject EN" {
		t.Fatalf("localized subjects should stay distinct: fr=%q en=%q", gotFR.ConfirmationSubject, gotEN.ConfirmationSubject)
	}
	if gotFR.Confirmation != "Bonjour {{.Username}}" || gotEN.Confirmation != "Hello {{.Username}}" {
		t.Fatalf("localized bodies should stay distinct: fr=%q en=%q", gotFR.Confirmation, gotEN.Confirmation)
	}
}

func TestSettingsHandlerPreviewEmailTemplateUsesJellyGateBranding(t *testing.T) {
	handler, db := newTestSettingsHandler(t)

	if err := db.SavePortalLinksConfig(config.PortalLinksConfig{
		JellyGateURL:       "https://jellygate.example.com",
		JellyfinURL:        "https://jellyfin.example.com",
		JellyfinServerName: "Media Lab",
	}); err != nil {
		t.Fatalf("SavePortalLinksConfig() error = %v", err)
	}

	payload := emailTemplatePreviewInput{
		Template:    "Hello {{.JellyfinServerName}}",
		TemplateKey: "confirmation",
		Language:    "en",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	rec := httptest.NewRecorder()
	handler.PreviewEmailTemplate(rec, newAdminRequest(http.MethodPost, "/admin/api/settings/email-templates/preview", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("PreviewEmailTemplate status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			HTML string `json:"html"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode preview response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("PreviewEmailTemplate success = false")
	}
	if !strings.Contains(resp.Data.HTML, "linear-gradient(135deg,#22d3ee,#10b981)") {
		t.Fatalf("preview should contain restored gradient header")
	}
	if !strings.Contains(resp.Data.HTML, "/static/img/logos/jellygate.svg") {
		t.Fatalf("preview should use JellyGate logo, got %q", resp.Data.HTML)
	}
	if !strings.Contains(resp.Data.HTML, "Media Lab") {
		t.Fatalf("preview should render Jellyfin server name, got %q", resp.Data.HTML)
	}
}
