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
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
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

	return NewSettingsHandler(db, nil, nil), db
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
	if !resp.Data.AuthSession.Remember30Days {
		t.Fatalf("default auth session Remember30Days = false, want true")
	}
	if !resp.Data.EmailTemplatesMultilingualEnabled {
		t.Fatalf("default email templates multilingual flag = false, want true")
	}
}

func TestSettingsHandlerSaveAndRevokeAuthSession(t *testing.T) {
	handler, db := newTestSettingsHandler(t)

	body, err := json.Marshal(authSessionInput{Remember30Days: false})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	rec := httptest.NewRecorder()
	handler.SaveAuthSession(rec, newAdminRequest(http.MethodPost, "/admin/api/settings/auth-session", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("SaveAuthSession status = %d, want %d", rec.Code, http.StatusOK)
	}

	cfg, err := db.GetAuthSessionConfig()
	if err != nil {
		t.Fatalf("GetAuthSessionConfig() error = %v", err)
	}
	if cfg.Remember30Days {
		t.Fatalf("Remember30Days = true, want false")
	}

	rec = httptest.NewRecorder()
	handler.RevokeAuthSessions(rec, newAdminRequest(http.MethodPost, "/admin/api/settings/auth-session/revoke", []byte(`{}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("RevokeAuthSessions status = %d, want %d", rec.Code, http.StatusOK)
	}

	cfg, err = db.GetAuthSessionConfig()
	if err != nil {
		t.Fatalf("GetAuthSessionConfig() after revoke error = %v", err)
	}
	if cfg.RevokedBefore <= 0 {
		t.Fatalf("RevokedBefore = %d, want > 0", cfg.RevokedBefore)
	}
	if cfg.Remember30Days {
		t.Fatalf("RevokeAuthSessions should preserve Remember30Days=false, got true")
	}
}

func TestSettingsHandlerTestJellyfinLDAPAuthUsesSharedAuthFlow(t *testing.T) {
	handler, _ := newTestSettingsHandler(t)
	requests := map[string]int{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests[r.Method+" "+r.URL.Path]++
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/Users/AuthenticateByName":
			if strings.Contains(r.Header.Get("Authorization"), "Token=") {
				t.Fatalf("authenticate request should not include a token: %q", r.Header.Get("Authorization"))
			}
			var payload struct {
				Username string `json:"Username"`
				Pw       string `json:"Pw"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode auth payload: %v", err)
			}
			if payload.Username != "ldap-user" || payload.Pw != "secret" {
				t.Fatalf("auth payload = %+v, want ldap-user/secret", payload)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"User": map[string]string{
					"Id":   "jf-user",
					"Name": "ldap-user",
				},
				"AccessToken": "session-token",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/Users/jf-user":
			if !strings.Contains(r.Header.Get("Authorization"), `Token="session-token"`) {
				t.Fatalf("policy refresh Authorization = %q, want session token", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(jellyfin.User{
				ID:   "jf-user",
				Name: "ldap-user",
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	handler.jfClient = jellyfin.New(config.JellyfinConfig{URL: server.URL, APIKey: "admin-api-key"})
	body, err := json.Marshal(jellyfinLDAPAuthTestInput{Username: "ldap-user", Password: "secret"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	rec := httptest.NewRecorder()
	handler.TestJellyfinLDAPAuth(rec, newAdminRequest(http.MethodPost, "/admin/api/settings/ldap/test-jellyfin-auth", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("TestJellyfinLDAPAuth status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("response success = false")
	}
	if resp.Data["jellyfin_user_id"] != "jf-user" || resp.Data["jellyfin_name"] != "ldap-user" {
		t.Fatalf("response data = %#v, want Jellyfin user details", resp.Data)
	}
	if requests[http.MethodPost+" /Users/AuthenticateByName"] != 1 || requests[http.MethodGet+" /Users/jf-user"] != 1 {
		t.Fatalf("unexpected request counts: %#v", requests)
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

func TestSettingsHandlerSaveEmailTemplatesMultilingualOffPreservesTranslations(t *testing.T) {
	handler, db := newTestSettingsHandler(t)

	templates, err := db.GetEmailTemplatesConfigByLanguage()
	if err != nil {
		t.Fatalf("GetEmailTemplatesConfigByLanguage() error = %v", err)
	}

	fr := templates["fr"]
	en := templates["en"]
	fr.ConfirmationSubject = "Sujet FR"
	fr.Confirmation = "Bonjour {{.Username}}"
	en.ConfirmationSubject = "Subject EN"
	en.Confirmation = "Hello {{.Username}}"

	if err := db.SaveEmailTemplatesConfigByLanguage(map[string]config.EmailTemplatesConfig{
		"fr": fr,
		"en": en,
	}); err != nil {
		t.Fatalf("SaveEmailTemplatesConfigByLanguage() error = %v", err)
	}

	updatedFR := fr
	updatedFR.ConfirmationSubject = "Sujet FR modifie"
	multilingual := false
	payload := saveEmailTemplatesInput{
		Language:            "fr",
		MultilingualEnabled: &multilingual,
		TemplatesByLang: map[string]config.EmailTemplatesConfig{
			"fr": updatedFR,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	rec := httptest.NewRecorder()
	handler.SaveEmailTemplates(rec, newAdminRequest(http.MethodPost, "/admin/api/settings/email-templates", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("SaveEmailTemplates status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if db.GetEmailTemplatesMultilingualEnabled() {
		t.Fatalf("multilingual flag = true, want false")
	}

	saved, err := db.GetEmailTemplatesConfigByLanguage()
	if err != nil {
		t.Fatalf("GetEmailTemplatesConfigByLanguage() after save error = %v", err)
	}
	if saved["fr"].ConfirmationSubject != "Sujet FR modifie" {
		t.Fatalf("fr subject = %q, want updated subject", saved["fr"].ConfirmationSubject)
	}
	if saved["en"].ConfirmationSubject != "Subject EN" || saved["en"].Confirmation != "Hello {{.Username}}" {
		t.Fatalf("en translation should be preserved, got subject=%q body=%q", saved["en"].ConfirmationSubject, saved["en"].Confirmation)
	}
}

func TestLoadEmailTemplatesForLanguageHonorsMultilingualFlag(t *testing.T) {
	_, db := newTestSettingsHandler(t)

	if err := db.SetSetting(database.SettingDefaultLang, "fr"); err != nil {
		t.Fatalf("SetSetting(default_lang) error = %v", err)
	}

	templates, err := db.GetEmailTemplatesConfigByLanguage()
	if err != nil {
		t.Fatalf("GetEmailTemplatesConfigByLanguage() error = %v", err)
	}
	fr := templates["fr"]
	en := templates["en"]
	fr.ConfirmationSubject = "Sujet FR"
	fr.Confirmation = "Bonjour {{.Username}}"
	en.ConfirmationSubject = "Subject EN"
	en.Confirmation = "Hello {{.Username}}"
	if err := db.SaveEmailTemplatesConfigByLanguage(map[string]config.EmailTemplatesConfig{
		"fr": fr,
		"en": en,
	}); err != nil {
		t.Fatalf("SaveEmailTemplatesConfigByLanguage() error = %v", err)
	}

	if err := db.SetEmailTemplatesMultilingualEnabled(false); err != nil {
		t.Fatalf("SetEmailTemplatesMultilingualEnabled(false) error = %v", err)
	}
	cfg, usedLang, err := loadEmailTemplatesForLanguage(db, "en", emailLanguageContext{PreferredLang: "en"})
	if err != nil {
		t.Fatalf("loadEmailTemplatesForLanguage() disabled error = %v", err)
	}
	if usedLang != "fr" || cfg.ConfirmationSubject != "Sujet FR" {
		t.Fatalf("disabled multilingual used lang/config = %q/%q, want fr/Sujet FR", usedLang, cfg.ConfirmationSubject)
	}

	if err := db.SetEmailTemplatesMultilingualEnabled(true); err != nil {
		t.Fatalf("SetEmailTemplatesMultilingualEnabled(true) error = %v", err)
	}
	cfg, usedLang, err = loadEmailTemplatesForLanguage(db, "en", emailLanguageContext{PreferredLang: "en"})
	if err != nil {
		t.Fatalf("loadEmailTemplatesForLanguage() enabled error = %v", err)
	}
	if usedLang != "en" || cfg.ConfirmationSubject != "Subject EN" {
		t.Fatalf("enabled multilingual used lang/config = %q/%q, want en/Subject EN", usedLang, cfg.ConfirmationSubject)
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
		Template:    "Hello {{.serveurname}}",
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
