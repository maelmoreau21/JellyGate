package handlers

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
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

	cfg := &config.Config{
		Authentik: config.AuthentikConfig{
			Enabled:           true,
			URL:               "https://auth.example.com",
			IssuerURL:         "https://auth.example.com/application/o/jellygate/",
			ClientID:          "test-client-id",
			UserGroup:         "custom-users",
			AdminGroup:        "custom-admins",
			JellyfinUserGroup: "custom-jellyfin",
		},
	}
	return NewSettingsHandler(cfg, db, nil, nil, nil), db
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

func TestSettingsHandlerSaveAuthentik(t *testing.T) {
	handler, db := newTestSettingsHandler(t)

	body, err := json.Marshal(config.AuthentikConfig{
		Enabled:            true,
		URL:                "https://auth.example.com",
		IssuerURL:          "https://auth.example.com/application/o/jellygate/",
		ClientID:           "jellygate",
		ClientSecret:       "secret123",
		RedirectURL:        "https://jellygate.example.com/auth/callback",
		APIToken:           "ak-token-12345",
		UserGroup:          "jellygate-users",
		AdminGroup:         "jellygate-admins",
		JellyfinUserGroup:  "jellyfin-users",
		EnrollmentFlowSlug: "default-enrollment-flow",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	rec := httptest.NewRecorder()
	handler.SaveAuthentik(rec, newAdminRequest(http.MethodPost, "/admin/api/settings/authentik", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("SaveAuthentik status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	saved, err := db.GetAuthentikConfig()
	if err != nil {
		t.Fatalf("GetAuthentikConfig() error = %v", err)
	}
	if !saved.Enabled || saved.URL != "https://auth.example.com" || saved.UserGroup != "jellygate-users" {
		t.Fatalf("saved Authentik config mismatch: %+v", saved)
	}
}

func TestSettingsHandlerGetAllWithEnvDefaults(t *testing.T) {
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
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if !resp.Data.Authentik.Enabled {
		t.Fatalf("expected Authentik.Enabled=true from env defaults, got false")
	}
	if resp.Data.Authentik.URL != "https://auth.example.com" {
		t.Fatalf("expected Authentik.URL=https://auth.example.com, got %q", resp.Data.Authentik.URL)
	}
	if resp.Data.Authentik.UserGroup != "custom-users" {
		t.Fatalf("expected Authentik.UserGroup=custom-users, got %q", resp.Data.Authentik.UserGroup)
	}
	if resp.Data.Authentik.AdminGroup != "custom-admins" {
		t.Fatalf("expected Authentik.AdminGroup=custom-admins, got %q", resp.Data.Authentik.AdminGroup)
	}
	if resp.Data.Authentik.JellyfinUserGroup != "custom-jellyfin" {
		t.Fatalf("expected Authentik.JellyfinUserGroup=custom-jellyfin, got %q", resp.Data.Authentik.JellyfinUserGroup)
	}
}

func TestSettingsHandlerSaveEmailTemplatesSyncsSharedFields(t *testing.T) {
	handler, db := newTestSettingsHandler(t)

	if err := db.SetSetting(database.SettingDefaultLang, "fr"); err != nil {
		t.Fatalf("SetSetting(default_lang) error = %v", err)
	}

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

func TestSettingsHandlerSaveEmailTemplatesMultilingualOffSavesSelectedServerLanguage(t *testing.T) {
	handler, db := newTestSettingsHandler(t)

	if err := db.SetSetting(database.SettingDefaultLang, "fr"); err != nil {
		t.Fatalf("SetSetting(default_lang) error = %v", err)
	}
	templates, err := db.GetEmailTemplatesConfigByLanguage()
	if err != nil {
		t.Fatalf("GetEmailTemplatesConfigByLanguage() error = %v", err)
	}

	fr := templates["fr"]
	en := templates["en"]
	fr.ConfirmationSubject = "Sujet FR conserve"
	fr.Confirmation = "Bonjour {{.Username}}"
	en.ConfirmationSubject = "Subject EN selected"
	en.Confirmation = "Hello selected {{.Username}}"
	if err := db.SaveEmailTemplatesConfigByLanguage(map[string]config.EmailTemplatesConfig{
		"fr": fr,
		"en": en,
	}); err != nil {
		t.Fatalf("SaveEmailTemplatesConfigByLanguage() error = %v", err)
	}

	updatedEN := en
	updatedEN.ConfirmationSubject = "Subject EN mono"
	multilingual := false
	payload := saveEmailTemplatesInput{
		Language:            "en",
		DefaultLang:         "en",
		MultilingualEnabled: &multilingual,
		TemplatesByLang: map[string]config.EmailTemplatesConfig{
			"en": updatedEN,
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
	if got := db.GetDefaultLang(); got != "en" {
		t.Fatalf("default language = %q, want en", got)
	}

	saved, err := db.GetEmailTemplatesConfigByLanguage()
	if err != nil {
		t.Fatalf("GetEmailTemplatesConfigByLanguage() after save error = %v", err)
	}
	if saved["en"].ConfirmationSubject != "Subject EN mono" {
		t.Fatalf("en subject = %q, want updated mono subject", saved["en"].ConfirmationSubject)
	}
	if saved["fr"].ConfirmationSubject != "Sujet FR conserve" {
		t.Fatalf("fr translation should be preserved, got %q", saved["fr"].ConfirmationSubject)
	}

	cfg, usedLang, err := loadEmailTemplatesForLanguage(db, "fr", emailLanguageContext{PreferredLang: "fr"})
	if err != nil {
		t.Fatalf("loadEmailTemplatesForLanguage() error = %v", err)
	}
	if usedLang != "en" || cfg.ConfirmationSubject != "Subject EN mono" {
		t.Fatalf("mono-language send should use server lang en, got %s/%q", usedLang, cfg.ConfirmationSubject)
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
	if !strings.Contains(resp.Data.HTML, "/static/img/icons/icon-192.png") {
		t.Fatalf("preview should use JellyGate logo, got %q", resp.Data.HTML)
	}
	if !strings.Contains(resp.Data.HTML, "Media Lab") {
		t.Fatalf("preview should render Jellyfin server name, got %q", resp.Data.HTML)
	}
}

func TestSettingsHandlerEmailTemplatesExportImportRoundtrip(t *testing.T) {
	handler, db := newTestSettingsHandler(t)

	templates, err := db.GetEmailTemplatesConfigByLanguage()
	if err != nil {
		t.Fatalf("GetEmailTemplatesConfigByLanguage() error = %v", err)
	}
	fr := templates["fr"]
	fr.ConfirmationSubject = "Sujet export FR"
	fr.Confirmation = config.PrepareEmailTemplateBodyForLanguage("fr", "confirmation", "Bonjour export {{.Username}}", fr.BaseTemplateHeader, fr.BaseTemplateFooter)
	if err := db.SaveEmailTemplatesConfigForLang("fr", fr); err != nil {
		t.Fatalf("SaveEmailTemplatesConfigForLang(fr) error = %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ExportEmailTemplates(rec, newAdminRequest(http.MethodGet, "/admin/api/settings/email-templates/export?lang=fr", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("ExportEmailTemplates(fr) status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	zipBytes := rec.Body.Bytes()
	if got := readZipEntryForTest(t, zipBytes, "fr/confirmation/subject.txt"); strings.TrimSpace(got) != "Sujet export FR" {
		t.Fatalf("exported fr subject = %q", got)
	}
	if got := readZipEntryForTest(t, zipBytes, "fr/confirmation/body.txt"); !strings.Contains(got, "Bonjour export {{.Username}}") {
		t.Fatalf("exported fr body should be editable text, got %q", got)
	}

	recAll := httptest.NewRecorder()
	handler.ExportEmailTemplates(recAll, newAdminRequest(http.MethodGet, "/admin/api/settings/email-templates/export?lang=all", nil))
	if recAll.Code != http.StatusOK {
		t.Fatalf("ExportEmailTemplates(all) status = %d, want %d", recAll.Code, http.StatusOK)
	}
	for _, lang := range config.SupportedLanguageTags() {
		if got := strings.TrimSpace(readZipEntryForTest(t, recAll.Body.Bytes(), lang+"/confirmation/subject.txt")); got == "" {
			t.Fatalf("export all missing %s confirmation subject", lang)
		}
	}

	importHandler, importDB := newTestSettingsHandler(t)
	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	fileWriter, err := writer.CreateFormFile("file", "fr.zip")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := fileWriter.Write(zipBytes); err != nil {
		t.Fatalf("write upload zip error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart writer close error = %v", err)
	}

	req := newAdminRequest(http.MethodPost, "/admin/api/settings/email-templates/import", upload.Bytes())
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec = httptest.NewRecorder()
	importHandler.ImportEmailTemplates(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ImportEmailTemplates status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	imported, _, err := importDB.GetEmailTemplatesConfigForLang("fr")
	if err != nil {
		t.Fatalf("GetEmailTemplatesConfigForLang(fr) after import error = %v", err)
	}
	if imported.ConfirmationSubject != "Sujet export FR" {
		t.Fatalf("imported fr subject = %q, want roundtrip subject", imported.ConfirmationSubject)
	}
	if !strings.Contains(imported.Confirmation, "Bonjour export {{.Username}}") {
		t.Fatalf("imported fr body should contain roundtrip body, got %q", imported.Confirmation)
	}
}

func readZipEntryForTest(t *testing.T, raw []byte, name string) string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open zip entry %s error = %v", name, err)
		}
		defer rc.Close()
		value, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read zip entry %s error = %v", name, err)
		}
		return string(value)
	}
	t.Fatalf("zip entry %s not found", name)
	return ""
}

func TestSettingsHandlerReloadAuthentikFromEnv(t *testing.T) {
	handler, _ := newTestSettingsHandler(t)

	rec := httptest.NewRecorder()
	handler.ReloadAuthentikFromEnv(rec, newAdminRequest(http.MethodPost, "/admin/api/settings/authentik/reload-env", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("ReloadAuthentikFromEnv status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Success bool                   `json:"success"`
		Data    config.AuthentikConfig `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("ReloadAuthentikFromEnv success = false")
	}
	if resp.Data.URL != "https://auth.example.com" {
		t.Errorf("expected URL https://auth.example.com, got %s", resp.Data.URL)
	}
	if resp.Data.UserGroup != "custom-users" {
		t.Errorf("expected UserGroup custom-users, got %s", resp.Data.UserGroup)
	}
}

func TestSettingsHandlerTestAuthentikUser(t *testing.T) {
	handler, db := newTestSettingsHandler(t)

	// Inserer un utilisateur local de test
	_, err := db.Exec(`INSERT INTO users (username, email, can_invite, is_active, created_at) VALUES ('testadmin', 'testadmin@example.com', 1, 1, datetime('now'))`)
	if err != nil {
		t.Fatalf("insert user error = %v", err)
	}

	payload := []byte(`{"username":"testadmin"}`)
	rec := httptest.NewRecorder()
	handler.TestAuthentikUser(rec, newAdminRequest(http.MethodPost, "/admin/api/settings/authentik/test-user", payload))

	if rec.Code != http.StatusOK {
		t.Fatalf("TestAuthentikUser status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Success bool           `json:"success"`
		Data    testUserResult `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Success || !resp.Data.Found {
		t.Fatalf("expected user to be found: %+v", resp)
	}
	if !resp.Data.IsJellyGateAdmin {
		t.Errorf("expected is_jellygate_admin to be true")
	}
	if !resp.Data.IsJellyGateUser {
		t.Errorf("expected is_jellygate_user to be true")
	}
}

func TestSettingsHandlerSaveJellyfin(t *testing.T) {
	handler, db := newTestSettingsHandler(t)

	var reloadedConfig config.JellyfinConfig
	handler.OnJellyfinReload = func(c config.JellyfinConfig) {
		reloadedConfig = c
	}

	payload := []byte(`{"url":"http://192.168.1.50:8096","api_key":"test_jf_key_123"}`)
	rec := httptest.NewRecorder()
	handler.SaveJellyfin(rec, newAdminRequest(http.MethodPost, "/admin/api/settings/jellyfin", payload))

	if rec.Code != http.StatusOK {
		t.Fatalf("SaveJellyfin status = %d, want %d", rec.Code, http.StatusOK)
	}

	saved, err := db.GetJellyfinConfig()
	if err != nil {
		t.Fatalf("GetJellyfinConfig error: %v", err)
	}
	if saved.URL != "http://192.168.1.50:8096" {
		t.Errorf("expected URL http://192.168.1.50:8096, got %s", saved.URL)
	}
	if saved.APIKey != "test_jf_key_123" {
		t.Errorf("expected APIKey test_jf_key_123, got %s", saved.APIKey)
	}
	if reloadedConfig.URL != "http://192.168.1.50:8096" {
		t.Errorf("expected callback reloaded URL http://192.168.1.50:8096, got %s", reloadedConfig.URL)
	}
}

func TestSettingsHandlerJellyfinEnvPrecedence(t *testing.T) {
	handler, db := newTestSettingsHandler(t)

	// Save one config in DB
	_ = db.SaveJellyfinConfig(config.JellyfinConfig{
		URL:    "http://from-db:8096",
		APIKey: "db_api_key",
	})

	// Handler has app config with Env overrides
	handler.cfg.Jellyfin = config.JellyfinConfig{
		URL:    "http://from-env:8096",
		APIKey: "env_api_key",
	}

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

	// Environment variable must take strict precedence
	if resp.Data.Jellyfin.URL != "http://from-env:8096" {
		t.Errorf("expected Jellyfin URL from env 'http://from-env:8096', got %s", resp.Data.Jellyfin.URL)
	}
	if !resp.Data.JellyfinEnvManaged {
		t.Errorf("expected JellyfinEnvManaged to be true")
	}
}
