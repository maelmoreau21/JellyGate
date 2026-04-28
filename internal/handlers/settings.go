// Package handlers Ã¢â‚¬â€� settings.go
//
// API REST pour la gestion des paramÃƒÂ¨tres stockÃƒÂ©s en base (table settings).
// Permet de lire et sauvegarder la configuration gÃƒÂ©nÃƒÂ©rale, LDAP, SMTP et Webhooks
// depuis l'interface d'administration.
//
// Routes :
//   - GET  /admin/api/settings          Ã¢â€ â€™ RÃƒÂ©cupÃƒÂ©rer toute la configuration
//   - POST /admin/api/settings/general  Ã¢â€ â€™ Sauvegarder les paramÃƒÂ¨tres gÃƒÂ©nÃƒÂ©raux (langue)
//   - POST /admin/api/settings/ldap     Ã¢â€ â€™ Sauvegarder la config LDAP
//   - POST /admin/api/settings/smtp     Ã¢â€ â€™ Sauvegarder la config SMTP
//   - POST /admin/api/settings/webhooks Ã¢â€ â€™ Sauvegarder la config Webhooks
//   - POST /admin/api/settings/backup    Ã¢â€ â€™ Sauvegarder la config de sauvegarde planifiÃƒÂ©e
package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	jgldap "github.com/maelmoreau21/JellyGate/internal/ldap"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

// Ã¢â€�â‚¬Ã¢â€�â‚¬ SettingsHandler Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// SettingsHandler gÃƒÂ¨re les routes de configuration.
type SettingsHandler struct {
	db       *database.DB
	jfClient *jellyfin.Client

	// Callbacks de rechargement Ã¢â‚¬â€� appelÃƒÂ©s aprÃƒÂ¨s sauvegarde pour
	// rÃƒÂ©initialiser les clients ÃƒÂ  chaud sans redÃƒÂ©marrer le conteneur.
	OnLDAPReload     func(config.LDAPConfig)
	OnSMTPReload     func(config.SMTPConfig)
	OnWebhooksReload func(config.WebhooksConfig)
}

// NewSettingsHandler crÃƒÂ©e un nouveau handler de paramÃƒÂ¨tres.
func NewSettingsHandler(db *database.DB, jf *jellyfin.Client) *SettingsHandler {
	return &SettingsHandler{db: db, jfClient: jf}
}

func (h *SettingsHandler) ensureAdmin(w http.ResponseWriter, r *http.Request) bool {
	sess := session.FromContext(r.Context())
	if sess == nil || !sess.IsAdmin {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Acces reserve aux administrateurs"})
		return false
	}
	return true
}

type ldapUserTestInput struct {
	config.LDAPConfig
	Username string `json:"username"`
}

type jellyfinLDAPAuthTestInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *SettingsHandler) normalizeLDAPInput(input *config.LDAPConfig) {
	if input.BindPassword == "Ã¢â‚¬Â¢Ã¢â‚¬Â¢Ã¢â‚¬Â¢Ã¢â‚¬Â¢Ã¢â‚¬Â¢Ã¢â‚¬Â¢Ã¢â‚¬Â¢Ã¢â‚¬Â¢" || input.BindPassword == "" {
		existing, _ := h.db.GetLDAPConfig()
		input.BindPassword = existing.BindPassword
	}
	if input.Port == 0 {
		input.Port = 636
	}
	input.SearchFilter = strings.TrimSpace(input.SearchFilter)
	if input.SearchFilter == "" {
		input.SearchFilter = "(&(|(objectClass=user)(objectClass=person)(objectClass=organizationalPerson)(objectClass=inetOrgPerson)(objectClass=posixAccount))(|(uid={username})(sAMAccountName={username})(cn={username})(userPrincipalName={username})(mail={username})))"
	}
	input.SearchAttributes = strings.TrimSpace(input.SearchAttributes)
	if input.SearchAttributes == "" {
		input.SearchAttributes = "uid,sAMAccountName,cn,userPrincipalName,mail"
	}
	input.UIDAttribute = strings.TrimSpace(input.UIDAttribute)
	if input.UIDAttribute == "" {
		input.UIDAttribute = "uid"
	}
	if strings.TrimSpace(input.UserOU) == "" {
		input.UserOU = "CN=Users"
	}
	input.UsernameAttribute = strings.TrimSpace(input.UsernameAttribute)
	if input.UsernameAttribute == "" {
		input.UsernameAttribute = "auto"
	}
	input.AdminFilter = strings.TrimSpace(input.AdminFilter)
	input.UserObjectClass = strings.TrimSpace(input.UserObjectClass)
	if input.UserObjectClass == "" {
		input.UserObjectClass = "auto"
	}
	input.GroupMemberAttr = strings.TrimSpace(input.GroupMemberAttr)
	if input.GroupMemberAttr == "" {
		input.GroupMemberAttr = "auto"
	}

	input.ProvisionMode = strings.ToLower(strings.TrimSpace(input.ProvisionMode))
	if input.ProvisionMode == "" {
		input.ProvisionMode = "hybrid"
	}

	input.JellyfinGroup = strings.TrimSpace(input.JellyfinGroup)
	input.InviterGroup = strings.TrimSpace(input.InviterGroup)
	input.AdministratorsGroup = strings.TrimSpace(input.AdministratorsGroup)
	if input.JellyfinGroup == "" {
		input.JellyfinGroup = "jellyfin"
	}
	if input.InviterGroup == "" {
		input.InviterGroup = "jellyfin-Parrainage"
	}
	if input.AdministratorsGroup == "" {
		input.AdministratorsGroup = "jellyfin-administrateur"
	}
	input.UserGroup = input.JellyfinGroup
}

func validateLDAPMinimalConfig(input config.LDAPConfig) error {
	if strings.TrimSpace(input.Host) == "" {
		return fmt.Errorf("host LDAP requis")
	}
	if strings.TrimSpace(input.BindDN) == "" {
		return fmt.Errorf("bind_dn requis")
	}
	if strings.TrimSpace(input.BindPassword) == "" {
		return fmt.Errorf("bind_password requis")
	}
	if strings.TrimSpace(input.BaseDN) == "" {
		return fmt.Errorf("base_dn requis")
	}
	return nil
}

// TestLDAPConnection teste la connexion et le bind LDAP sans sauvegarder la configuration.
func (h *SettingsHandler) TestLDAPConnection(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	var input config.LDAPConfig
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "JSON invalide : " + err.Error()})
		return
	}

	h.normalizeLDAPInput(&input)
	if err := validateLDAPMinimalConfig(input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}

	client := jgldap.New(input)
	if err := client.TestConnection(); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Echec connexion LDAP: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Connexion LDAP OK (reseau + bind)"})
}

// TestLDAPUserLookup teste la recherche d'un utilisateur LDAP via l'attribut
// de login configure (ex: sAMAccountName, uid).
func (h *SettingsHandler) TestLDAPUserLookup(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	var input ldapUserTestInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "JSON invalide : " + err.Error()})
		return
	}

	h.normalizeLDAPInput(&input.LDAPConfig)
	if err := validateLDAPMinimalConfig(input.LDAPConfig); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}

	username := strings.TrimSpace(input.Username)
	if username == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "username de test requis"})
		return
	}

	client := jgldap.New(input.LDAPConfig)
	entry, isAdmin, err := client.ResolveUserAccess(username)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Echec recherche LDAP: " + err.Error()})
		return
	}
	if entry == nil {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: "Utilisateur LDAP introuvable"})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Utilisateur LDAP trouve (filtre d'acces applique)",
		Data: map[string]interface{}{
			"dn":            entry.DN,
			"username":      entry.Username,
			"uid":           entry.UID,
			"username_attr": entry.UsernameAttribute,
			"display_name":  entry.DisplayName,
			"email":         entry.Email,
			"upn":           entry.UPN,
			"is_disabled":   entry.IsDisabled,
			"is_admin":      isAdmin,
			"search_filter": input.SearchFilter,
			"admin_filter":  input.AdminFilter,
		},
	})
}

// TestJellyfinLDAPAuth vÃ©rifie que l'authentification LDAP via le plugin Jellyfin fonctionne.
func (h *SettingsHandler) TestJellyfinLDAPAuth(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	var input jellyfinLDAPAuthTestInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "JSON invalide : " + err.Error()})
		return
	}

	username := strings.TrimSpace(input.Username)
	password := input.Password
	if username == "" || password == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "username et mot de passe de test requis"})
		return
	}

	var baseURL string
	if baseURL == "" {
		if links, err := h.db.GetPortalLinksConfig(); err == nil {
			baseURL = strings.TrimRight(strings.TrimSpace(links.JellyfinURL), "/")
		}
	}
	if baseURL == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "URL Jellyfin indisponible"})
		return
	}

	body, _ := json.Marshal(map[string]string{
		"Username": username,
		"Pw":       password,
	})

	req, err := http.NewRequest(http.MethodPost, baseURL+"/Users/AuthenticateByName", bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Creation requete impossible"})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	embyAuth := fmt.Sprintf(`MediaBrowser Client="JellyGate", Device="Server", DeviceId="jellygate", Version="%s"`, config.AppVersion)
	req.Header.Set("Authorization", embyAuth)
	req.Header.Set("X-Emby-Authorization", embyAuth)

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Connexion Jellyfin impossible: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Authentification refusee (identifiants invalides ou plugin LDAP Jellyfin non fonctionnel)"})
		return
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: fmt.Sprintf("Jellyfin a retourne HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))})
		return
	}

	var authResp struct {
		User struct {
			ID   string `json:"Id"`
			Name string `json:"Name"`
		} `json:"User"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Reponse Jellyfin invalide"})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Authentification Jellyfin via LDAP plugin OK",
		Data: map[string]interface{}{
			"jellyfin_user_id": authResp.User.ID,
			"jellyfin_name":    authResp.User.Name,
		},
	})
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ Structures de rÃƒÂ©ponse Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// settingsResponse contient toute la configuration pour le frontend.
type settingsResponse struct {
	DefaultLang            string                                 `json:"default_lang"`
	DatabaseType           string                                 `json:"database_type"`
	BackupSQLiteOnly       bool                                   `json:"backup_sqlite_only"`
	DefaultEmailBaseHeader string                                 `json:"default_email_base_header"`
	DefaultEmailBaseFooter string                                 `json:"default_email_base_footer"`
	PortalLinks            config.PortalLinksConfig               `json:"portal_links"`
	InvitationProfile      config.InvitationProfileConfig         `json:"invitation_profile"`
	LDAP                   config.LDAPConfig                      `json:"ldap"`
	SMTP                   config.SMTPConfig                      `json:"smtp"`
	Webhooks               config.WebhooksConfig                  `json:"webhooks"`
	Backup                 config.BackupConfig                    `json:"backup"`
	EmailTemplates         config.EmailTemplatesConfig            `json:"email_templates"`
	EmailTemplatesByLang   map[string]config.EmailTemplatesConfig `json:"email_templates_by_lang"`
}

// generalInput est le corps JSON attendu par SaveGeneral.
type generalInput struct {
	JellyGateURL       string `json:"jellygate_url"`
	DefaultLang        string `json:"default_lang"`
	JellyfinURL        string `json:"jellyfin_url"`
	JellyfinServerName string `json:"jellyfin_server_name"`
	JellyseerrURL      string `json:"jellyseerr_url"`
	JellyTrackURL      string `json:"jellytrack_url"`
}

func normalizePublicPortalURL(raw string) (string, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return "", nil
	}

	parsed, err := url.ParseRequestURI(candidate)
	if err != nil {
		return "", fmt.Errorf("format invalide")
	}

	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("schema http/https requis")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("hote requis")
	}

	return strings.TrimRight(candidate, "/"), nil
}

func normalizeEmailTemplateBodies(lang string, cfg *config.EmailTemplatesConfig) {
	normalizeEmailBaseTemplates(cfg)
	cfg.Confirmation = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, "confirmation", cfg.Confirmation, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
	cfg.EmailVerification = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, "email_verification", cfg.EmailVerification, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
	cfg.ExpiryReminder = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, "expiry_reminder", cfg.ExpiryReminder, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
	cfg.ExpiryReminder14 = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, "expiry_reminder", cfg.ExpiryReminder14, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
	cfg.ExpiryReminder7 = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, "expiry_reminder", cfg.ExpiryReminder7, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
	cfg.ExpiryReminder1 = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, "expiry_reminder", cfg.ExpiryReminder1, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
	cfg.Invitation = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, "invitation", cfg.Invitation, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
	cfg.InviteExpiry = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, "invite_expiry", cfg.InviteExpiry, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
	cfg.PasswordReset = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, "password_reset", cfg.PasswordReset, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
	cfg.PreSignupHelp = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, "", cfg.PreSignupHelp, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
	cfg.PostSignupHelp = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, "", cfg.PostSignupHelp, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
	cfg.UserCreation = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, "user_creation", cfg.UserCreation, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
	cfg.UserDeletion = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, "user_deletion", cfg.UserDeletion, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
	cfg.UserDisabled = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, "user_disabled", cfg.UserDisabled, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
	cfg.UserEnabled = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, "user_enabled", cfg.UserEnabled, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
	cfg.UserExpired = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, "user_expired", cfg.UserExpired, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
	cfg.ExpiryAdjusted = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, "expiry_adjusted", cfg.ExpiryAdjusted, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
	cfg.Welcome = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, "welcome", cfg.Welcome, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
}

func trimEmailTemplateSubjects(cfg *config.EmailTemplatesConfig) {
	cfg.ConfirmationSubject = strings.TrimSpace(cfg.ConfirmationSubject)
	cfg.EmailVerificationSubject = strings.TrimSpace(cfg.EmailVerificationSubject)
	cfg.ExpiryReminderSubject = strings.TrimSpace(cfg.ExpiryReminderSubject)
	cfg.InvitationSubject = strings.TrimSpace(cfg.InvitationSubject)
	cfg.InviteExpirySubject = strings.TrimSpace(cfg.InviteExpirySubject)
	cfg.PasswordResetSubject = strings.TrimSpace(cfg.PasswordResetSubject)
	cfg.UserCreationSubject = strings.TrimSpace(cfg.UserCreationSubject)
	cfg.UserDeletionSubject = strings.TrimSpace(cfg.UserDeletionSubject)
	cfg.UserDisabledSubject = strings.TrimSpace(cfg.UserDisabledSubject)
	cfg.UserEnabledSubject = strings.TrimSpace(cfg.UserEnabledSubject)
	cfg.UserExpiredSubject = strings.TrimSpace(cfg.UserExpiredSubject)
	cfg.ExpiryAdjustedSubject = strings.TrimSpace(cfg.ExpiryAdjustedSubject)
	cfg.WelcomeSubject = strings.TrimSpace(cfg.WelcomeSubject)
}

func sanitizeEmailTemplatesInput(lang string, cfg *config.EmailTemplatesConfig) error {
	if cfg == nil {
		return fmt.Errorf("configuration email vide")
	}
	if cfg.ExpiryReminderDays == 0 {
		cfg.ExpiryReminderDays = 3
	}
	normalizeEmailBaseTemplates(cfg)
	normalizeEmailTemplateBodies(lang, cfg)
	trimEmailTemplateSubjects(cfg)
	cfg.PreSignupHelp = ""
	cfg.DisablePreSignupHelpEmail = true
	cfg.PostSignupHelp = ""
	cfg.DisablePostSignupHelpEmail = true
	if cfg.ExpiryReminderDays < 1 || cfg.ExpiryReminderDays > 365 {
		return fmt.Errorf("expiry_reminder_days doit etre entre 1 et 365")
	}
	return nil
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ GET /admin/api/settings Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// GetAll retourne toute la configuration stockÃ©e en base.
func (h *SettingsHandler) GetAll(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	defaultLang := h.db.GetDefaultLang()

	ldapCfg, err := h.db.GetLDAPConfig()
	if err != nil {
		slog.Error("Erreur lecture config LDAP", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur lecture configuration LDAP",
		})
		return
	}

	smtpCfg, err := h.db.GetSMTPConfig()
	if err != nil {
		slog.Error("Erreur lecture config SMTP", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur lecture configuration SMTP",
		})
		return
	}

	webhooksCfg, err := h.db.GetWebhooksConfig()
	if err != nil {
		slog.Error("Erreur lecture config Webhooks", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur lecture configuration Webhooks",
		})
		return
	}

	backupCfg, err := h.db.GetBackupConfig()
	if err != nil {
		slog.Error("Erreur lecture config Backup", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur lecture configuration sauvegardes",
		})
		return
	}

	portalLinks, err := h.db.GetPortalLinksConfig()
	if err != nil {
		slog.Error("Erreur lecture config Portal Links", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur lecture des URLs publiques",
		})
		return
	}

	inviteProfileCfg, err := h.db.GetInvitationProfileConfig()
	if err != nil {
		slog.Error("Erreur lecture config Invitation Profile", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur lecture du profil d'invitation",
		})
		return
	}

	// Masquer le mot de passe LDAP et SMTP dans la rÃƒÂ©ponse
	maskedLDAP := ldapCfg
	if maskedLDAP.BindPassword != "" {
		maskedLDAP.BindPassword = "â€¢â€¢â€¢â€¢â€¢â€¢â€¢â€¢"
	}
	maskedSMTP := smtpCfg
	if maskedSMTP.Password != "" {
		maskedSMTP.Password = "â€¢â€¢â€¢â€¢â€¢â€¢â€¢â€¢"
	}

	emailTemplatesByLang, err := h.db.GetEmailTemplatesConfigByLanguage()
	if err != nil {
		slog.Error("Erreur lecture config Email Templates (par langue)", "error", err)
		emailTemplatesByLang = map[string]config.EmailTemplatesConfig{}
	}
	for lang, cfg := range emailTemplatesByLang {
		normalizeEmailTemplateBodies(lang, &cfg)
		trimEmailTemplateSubjects(&cfg)
		emailTemplatesByLang[lang] = cfg
	}

	emailTemplatesCfg, ok := emailTemplatesByLang[defaultLang]
	if !ok {
		emailTemplatesCfg = config.DefaultEmailTemplatesForLanguage(defaultLang)
		normalizeEmailTemplateBodies(defaultLang, &emailTemplatesCfg)
		trimEmailTemplateSubjects(&emailTemplatesCfg)
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: settingsResponse{
			DefaultLang:            defaultLang,
			DatabaseType:           h.db.Driver(),
			BackupSQLiteOnly:       h.db.IsSQLite(),
			DefaultEmailBaseHeader: config.DefaultEmailBaseHeader(),
			DefaultEmailBaseFooter: config.DefaultEmailBaseFooter(),
			PortalLinks:            portalLinks,
			InvitationProfile:      inviteProfileCfg,
			LDAP:                   maskedLDAP,
			SMTP:                   maskedSMTP,
			Webhooks:               webhooksCfg,
			Backup:                 backupCfg,
			EmailTemplates:         emailTemplatesCfg,
			EmailTemplatesByLang:   emailTemplatesByLang,
		},
	})
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ POST /admin/api/settings/general Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// SaveGeneral sauvegarde les paramÃ¨tres gÃ©nÃ©raux (langue par dÃ©faut).
func (h *SettingsHandler) SaveGeneral(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	var input generalInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "JSON invalide : " + err.Error(),
		})
		return
	}

	input.DefaultLang = config.NormalizeLanguageTag(input.DefaultLang)

	// Validation : langues supportees par l'application
	if !config.IsSupportedLanguage(input.DefaultLang) {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "Langue invalide: fr, en, de, es, it, nl, pl, pt-BR, ru, zh",
		})
		return
	}

	var err error
	if input.JellyGateURL, err = normalizePublicPortalURL(input.JellyGateURL); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "URL publique JellyGate invalide: " + err.Error()})
		return
	}
	if input.JellyfinURL, err = normalizePublicPortalURL(input.JellyfinURL); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "URL publique Jellyfin invalide: " + err.Error()})
		return
	}
	if input.JellyseerrURL, err = normalizePublicPortalURL(input.JellyseerrURL); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "URL publique Jellyseerr invalide: " + err.Error()})
		return
	}
	if input.JellyTrackURL, err = normalizePublicPortalURL(input.JellyTrackURL); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "URL publique JellyTrack invalide: " + err.Error()})
		return
	}
	input.JellyfinServerName = strings.TrimSpace(input.JellyfinServerName)
	if input.JellyfinServerName == "" {
		input.JellyfinServerName = "Jellyfin"
	}

	if err := h.db.SetSetting(database.SettingDefaultLang, input.DefaultLang); err != nil {
		slog.Error("Erreur sauvegarde default_lang", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de sauvegarde",
		})
		return
	}

	if err := h.db.SavePortalLinksConfig(config.PortalLinksConfig{
		JellyGateURL:       input.JellyGateURL,
		JellyfinURL:        input.JellyfinURL,
		JellyfinServerName: input.JellyfinServerName,
		JellyseerrURL:      input.JellyseerrURL,
		JellyTrackURL:      input.JellyTrackURL,
	}); err != nil {
		slog.Error("Erreur sauvegarde portal_links", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de sauvegarde des URLs publiques",
		})
		return
	}

	slog.Info("Langue par dÃ©faut mise Ã  jour", "lang", input.DefaultLang)
	_ = h.db.LogAction("settings.general.saved", "", "", "default_lang="+input.DefaultLang)

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "ParamÃ¨tres gÃ©nÃ©raux sauvegardÃ©s",
	})
}

// FetchJellyfinServerName rÃƒÂ©cupÃƒÂ¨re le nom du serveur depuis l'API Jellyfin.
func (h *SettingsHandler) FetchJellyfinServerName(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	if h.jfClient == nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Client Jellyfin non configure"})
		return
	}

	info, err := h.jfClient.GetSystemInfo()
	if err != nil {
		// Fallback public info if authenticated fails
		info, err = h.jfClient.GetPublicSystemInfo()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Impossible de contacter Jellyfin: " + err.Error()})
			return
		}
	}

	serverName := ""
	if name, ok := info["ServerName"].(string); ok {
		serverName = name
	} else if name, ok := info["Name"].(string); ok {
		serverName = name
	}

	if serverName == "" {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: "Nom du serveur non trouve dans la reponse Jellyfin"})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    map[string]string{"server_name": serverName},
	})
}

type emailTemplatePreviewInput struct {
	Template           string            `json:"template"`
	TemplateKey        string            `json:"template_key"`
	Language           string            `json:"language"`
	BaseTemplateHeader string            `json:"base_template_header"`
	BaseTemplateFooter string            `json:"base_template_footer"`
	Context            map[string]string `json:"context"`
}

// PreviewEmailTemplate rend un modele d'email avec des donnees de demonstration.
func (h *SettingsHandler) PreviewEmailTemplate(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	var input emailTemplatePreviewInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "JSON invalide : " + err.Error()})
		return
	}

	tplRaw := strings.TrimSpace(input.Template)
	if tplRaw == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Template vide"})
		return
	}

	previewLang := config.NormalizeLanguageTag(input.Language)
	if !config.IsSupportedLanguage(previewLang) {
		if h.db != nil {
			previewLang = h.db.GetDefaultLang()
		}
	}
	if !config.IsSupportedLanguage(previewLang) {
		previewLang = "fr"
	}

	previewCfg := config.DefaultEmailTemplatesForLanguage(previewLang)
	previewCfg.BaseTemplateHeader = input.BaseTemplateHeader
	previewCfg.BaseTemplateFooter = input.BaseTemplateFooter
	normalizeEmailBaseTemplates(&previewCfg)
	tplRaw = config.PrepareEmailTemplateBodyForLanguage(previewLang, strings.TrimSpace(input.TemplateKey), tplRaw, previewCfg.BaseTemplateHeader, previewCfg.BaseTemplateFooter)

	links := resolvePortalLinks(nil, h.db)
	if strings.TrimSpace(links.JellyGateURL) == "" {
		links.JellyGateURL = "https://jellygate.example.com"
	}
	if strings.TrimSpace(links.JellyfinURL) == "" {
		links.JellyfinURL = "https://jellyfin.example.com"
	}
	if strings.TrimSpace(links.JellyseerrURL) == "" {
		links.JellyseerrURL = "https://jellyseerr.example.com"
	}
	if strings.TrimSpace(links.JellyTrackURL) == "" {
		links.JellyTrackURL = "https://jellytrack.example.com"
	}
	sample := map[string]string{
		"Username":           "demo.user",
		"DisplayName":        "demo.user",
		"Email":              "demo@example.com",
		"InviteLink":         links.JellyGateURL + "/invite/ABC123",
		"InviteURL":          links.JellyGateURL + "/invite/ABC123",
		"InviteCode":         "ABC123",
		"HelpURL":            links.JellyfinURL,
		"ResetLink":          links.JellyGateURL + "/reset/XYZ789",
		"ResetURL":           links.JellyGateURL + "/reset/XYZ789",
		"ResetCode":          "XYZ789",
		"VerificationLink":   links.JellyGateURL + "/verify-email/MAIL123",
		"VerificationURL":    links.JellyGateURL + "/verify-email/MAIL123",
		"VerificationCode":   "MAIL123",
		"ExpiresIn":          config.DefaultEmailPreviewDurationForLanguage(previewLang),
		"ExpiryDate":         time.Now().AddDate(0, 0, 7).Format("02/01/2006 15:04"),
		"JellyGateURL":       links.JellyGateURL,
		"JellyfinURL":        links.JellyfinURL,
		"JellyfinServerName": links.JellyfinServerName,
		"JellyseerrURL":      links.JellyseerrURL,
		"JellyTrackURL":      links.JellyTrackURL,
		"EmailLogoURL":       strings.TrimRight(links.JellyGateURL, "/") + "/static/img/logos/jellygate.svg",
		"Message":            config.DefaultEmailPreviewMessageForLanguage(previewLang),
	}
	for k, v := range input.Context {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		sample[key] = v
	}

	tpl, err := template.New("email_preview").Option("missingkey=zero").Parse(tplRaw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Erreur de syntaxe template: " + err.Error()})
		return
	}

	var out bytes.Buffer
	if err := tpl.Execute(&out, sample); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Erreur de rendu template: " + err.Error()})
		return
	}

	htmlOut := strings.TrimSpace(out.String())
	if htmlOut == "" {
		htmlOut = `<div style="font-family:Segoe UI,Arial,sans-serif;padding:24px;color:#334155;">Apercu vide.</div>`
	} else if !strings.Contains(strings.ToLower(htmlOut), "<html") && !strings.Contains(htmlOut, "<body") && !strings.Contains(htmlOut, "<div") {
		htmlOut = `<div style="font-family:Segoe UI,Arial,sans-serif;padding:24px;background:#f8fafc;color:#0f172a;white-space:pre-wrap;line-height:1.55;">` + template.HTMLEscapeString(htmlOut) + `</div>`
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"html": htmlOut,
		},
	})
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ POST /admin/api/settings/ldap Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// SaveLDAP sauvegarde la configuration LDAP.
func (h *SettingsHandler) SaveLDAP(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	var input config.LDAPConfig
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "JSON invalide : " + err.Error(),
		})
		return
	}

	// Si le mot de passe est masquÃƒÂ© (pas changÃƒÂ©), conserver l'ancien
	h.normalizeLDAPInput(&input)

	input.ProvisionMode = strings.ToLower(strings.TrimSpace(input.ProvisionMode))
	if input.ProvisionMode == "" {
		input.ProvisionMode = "hybrid"
	}
	if input.ProvisionMode != "hybrid" && input.ProvisionMode != "ldap_only" {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "Mode LDAP invalide: hybrid ou ldap_only",
		})
		return
	}

	// Compatibilite: user_group reste renseigne pour les anciennes versions/exports.
	input.UserGroup = input.JellyfinGroup

	if err := h.db.SaveLDAPConfig(input); err != nil {
		slog.Error("Erreur sauvegarde config LDAP", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de sauvegarde",
		})
		return
	}

	slog.Info("Configuration LDAP sauvegardÃ©e",
		"enabled", input.Enabled,
		"host", input.Host,
		"provision_mode", input.ProvisionMode,
	)

	// Rechargement ÃƒÂ  chaud
	if h.OnLDAPReload != nil {
		h.OnLDAPReload(input)
	}

	_ = h.db.LogAction("settings.ldap.saved", "", "", "")

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Configuration LDAP sauvegardÃ©e",
	})
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ POST /admin/api/settings/smtp Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// SaveSMTP sauvegarde la configuration SMTP.
func (h *SettingsHandler) SaveSMTP(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	var input config.SMTPConfig
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "JSON invalide : " + err.Error(),
		})
		return
	}

	// Si le mot de passe est masquÃƒÂ©, conserver l'ancien
	if input.Password == "Ã¢â‚¬Â¢Ã¢â‚¬Â¢Ã¢â‚¬Â¢Ã¢â‚¬Â¢Ã¢â‚¬Â¢Ã¢â‚¬Â¢Ã¢â‚¬Â¢Ã¢â‚¬Â¢" || input.Password == "" {
		existing, _ := h.db.GetSMTPConfig()
		input.Password = existing.Password
	}

	// Valeurs par dÃƒÂ©faut
	if input.Port == 0 {
		input.Port = 587
	}

	if err := h.db.SaveSMTPConfig(input); err != nil {
		slog.Error("Erreur sauvegarde config SMTP", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de sauvegarde",
		})
		return
	}

	slog.Info("Configuration SMTP sauvegardÃ©e", "host", input.Host)

	// Rechargement ÃƒÂ  chaud
	if h.OnSMTPReload != nil {
		h.OnSMTPReload(input)
	}

	_ = h.db.LogAction("settings.smtp.saved", "", "", "")

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Configuration SMTP sauvegardÃ©e",
	})
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ POST /admin/api/settings/webhooks Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// SaveWebhooks sauvegarde la configuration Webhooks.
func (h *SettingsHandler) SaveWebhooks(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	var input config.WebhooksConfig
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "JSON invalide : " + err.Error(),
		})
		return
	}

	if err := h.db.SaveWebhooksConfig(input); err != nil {
		slog.Error("Erreur sauvegarde config Webhooks", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de sauvegarde",
		})
		return
	}

	slog.Info("Configuration Webhooks sauvegardÃ©e")

	// Rechargement ÃƒÂ  chaud
	if h.OnWebhooksReload != nil {
		h.OnWebhooksReload(input)
	}

	_ = h.db.LogAction("settings.webhooks.saved", "", "", "")

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Configuration Webhooks sauvegardÃ©e",
	})
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ POST /admin/api/settings/backup Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// SaveBackup sauvegarde la configuration des sauvegardes planifiÃƒÂ©es.
func (h *SettingsHandler) SaveBackup(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	var input config.BackupConfig
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "JSON invalide : " + err.Error(),
		})
		return
	}

	if input.Hour < 0 || input.Hour > 23 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Heure invalide (0-23)"})
		return
	}
	if input.Minute < 0 || input.Minute > 59 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Minutes invalides (0-59)"})
		return
	}

	// Politique produit: toujours conserver les 7 derniÃƒÂ¨res sauvegardes.
	input.RetentionCount = 7

	if err := h.db.SaveBackupConfig(input); err != nil {
		slog.Error("Erreur sauvegarde config Backup", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de sauvegarde",
		})
		return
	}

	_ = h.db.LogAction("settings.backup.saved", "", "", "")
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Configuration de sauvegarde sauvegardÃ©e"})
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ POST /admin/api/settings/email-templates Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// SaveEmailTemplates sauvegarde les modÃƒÂ¨les de courriels personnalisÃƒÂ©s.
type saveEmailTemplatesInput struct {
	Language        string                                 `json:"language"`
	Template        *config.EmailTemplatesConfig           `json:"template"`
	TemplatesByLang map[string]config.EmailTemplatesConfig `json:"templates_by_lang"`
}

// SaveEmailTemplates sauvegarde les modeles e-mail (mono-langue ou multi-langue).
func (h *SettingsHandler) SaveEmailTemplates(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "Lecture du corps impossible: " + err.Error(),
		})
		return
	}

	var payload saveEmailTemplatesInput
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "JSON invalide : " + err.Error(),
		})
		return
	}

	if len(payload.TemplatesByLang) > 0 {
		sanitized := make(map[string]config.EmailTemplatesConfig, len(payload.TemplatesByLang))
		for rawLang, cfg := range payload.TemplatesByLang {
			lang := config.NormalizeLanguageTag(rawLang)
			if !config.IsSupportedLanguage(lang) {
				continue
			}
			if err := sanitizeEmailTemplatesInput(lang, &cfg); err != nil {
				writeJSON(w, http.StatusBadRequest, APIResponse{
					Success: false,
					Message: fmt.Sprintf("Langue %s: %s", lang, err.Error()),
				})
				return
			}
			sanitized[lang] = cfg
		}

		if len(sanitized) == 0 {
			writeJSON(w, http.StatusBadRequest, APIResponse{
				Success: false,
				Message: "Aucune langue valide dans templates_by_lang",
			})
			return
		}

		if err := h.db.SaveEmailTemplatesConfigByLanguage(sanitized); err != nil {
			slog.Error("Erreur sauvegarde config Email Templates (multi-langue)", "error", err)
			writeJSON(w, http.StatusInternalServerError, APIResponse{
				Success: false,
				Message: "Erreur de sauvegarde des modeles",
			})
			return
		}

		slog.Info("Configuration Email Templates sauvegardee (multi-langue)", "languages", len(sanitized))
		_ = h.db.LogAction("settings.email_templates.saved", "", "", fmt.Sprintf(`{"languages":%d}`, len(sanitized)))
		writeJSON(w, http.StatusOK, APIResponse{
			Success: true,
			Message: "Modeles e-mail sauvegardes",
		})
		return
	}

	if payload.Template != nil {
		cfg := *payload.Template
		targetLang := config.NormalizeLanguageTag(payload.Language)
		if !config.IsSupportedLanguage(targetLang) {
			targetLang = h.db.GetDefaultLang()
		}
		if err := sanitizeEmailTemplatesInput(targetLang, &cfg); err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{
				Success: false,
				Message: err.Error(),
			})
			return
		}

		if err := h.db.SaveEmailTemplatesConfigForLang(targetLang, cfg); err != nil {
			slog.Error("Erreur sauvegarde config Email Templates (langue cible)", "lang", targetLang, "error", err)
			writeJSON(w, http.StatusInternalServerError, APIResponse{
				Success: false,
				Message: "Erreur de sauvegarde des modeles",
			})
			return
		}

		slog.Info("Configuration Email Templates sauvegardee", "lang", targetLang)
		_ = h.db.LogAction("settings.email_templates.saved", "", "", fmt.Sprintf(`{"language":"%s"}`, targetLang))
		writeJSON(w, http.StatusOK, APIResponse{
			Success: true,
			Message: "Modeles e-mail sauvegardes",
		})
		return
	}

	var legacy config.EmailTemplatesConfig
	if err := json.Unmarshal(rawBody, &legacy); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "JSON invalide : " + err.Error(),
		})
		return
	}
	targetLang := config.NormalizeLanguageTag(payload.Language)
	if !config.IsSupportedLanguage(targetLang) {
		targetLang = h.db.GetDefaultLang()
	}
	if err := sanitizeEmailTemplatesInput(targetLang, &legacy); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: err.Error(),
		})
		return
	}
	if err := h.db.SaveEmailTemplatesConfigForLang(targetLang, legacy); err != nil {
		slog.Error("Erreur sauvegarde config Email Templates (legacy)", "lang", targetLang, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de sauvegarde des modeles",
		})
		return
	}

	slog.Info("Configuration Email Templates sauvegardee (legacy)", "lang", targetLang)
	_ = h.db.LogAction("settings.email_templates.saved", "", "", fmt.Sprintf(`{"language":"%s","legacy":true}`, targetLang))
	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Modeles e-mail sauvegardes",
	})
}

// SaveInvitationProfile sauvegarde la politique globale appliquee aux invitations.
func (h *SettingsHandler) SaveInvitationProfile(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	var input config.InvitationProfileConfig
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "JSON invalide : " + err.Error(),
		})
		return
	}

	if err := h.db.SaveInvitationProfileConfig(input); err != nil {
		slog.Error("Erreur sauvegarde config Invitation Profile", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de sauvegarde du profil d'invitation",
		})
		return
	}

	_ = h.db.LogAction("settings.invitation_profile.saved", "", "", "")
	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Profil d'invitation sauvegardÃ©",
	})
}
