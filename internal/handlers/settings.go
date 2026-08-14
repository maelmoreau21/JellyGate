// Package handlers — settings.go
//
// API REST pour la gestion des paramètres stockés en base (table settings).
// Permet de lire et sauvegarder la configuration générale, Authentik, SMTP et Webhooks
// depuis l'interface d'administration.
//
// Routes :
//   - GET  /admin/api/settings          → Récupérer toute la configuration
//   - POST /admin/api/settings/general  → Sauvegarder les paramètres généraux (langue)
//   - POST /admin/api/settings/authentik → Sauvegarder la config Authentik OIDC / API
//   - GET  /admin/api/settings/authentik/health → Diagnostic de santé Authentik
//   - POST /admin/api/settings/smtp     → Sauvegarder la config SMTP
//   - POST /admin/api/settings/webhooks → Sauvegarder la config Webhooks
//   - POST /admin/api/settings/backup   → Sauvegarder la config de sauvegarde planifiée
package handlers

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maelmoreau21/JellyGate/internal/authentik"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

// — SettingsHandler ———————————————————————————————————————————————————————————————————————————

// SettingsHandler gère les routes de configuration.
type SettingsHandler struct {
	cfg        *config.Config
	db         *database.DB
	jfClient   *jellyfin.Client
	authClient authentik.Client
	renderer   *render.Engine

	// Callbacks de rechargement à chaud
	OnSMTPReload      func(config.SMTPConfig)
	OnWebhooksReload  func(config.WebhooksConfig)
	OnAuthentikReload func(config.AuthentikConfig)
}

func (h *SettingsHandler) tr(r *http.Request, key, fallback string) string {
	if h.renderer == nil {
		return fallback
	}
	lang := jgmw.LangFromContext(r.Context())
	value := h.renderer.Translate(lang, key)
	if value == "["+key+"]" {
		return fallback
	}
	return value
}

// NewSettingsHandler crée un nouveau handler de paramètres.
func NewSettingsHandler(cfg *config.Config, db *database.DB, jf *jellyfin.Client, authClient authentik.Client, renderer *render.Engine) *SettingsHandler {
	return &SettingsHandler{cfg: cfg, db: db, jfClient: jf, authClient: authClient, renderer: renderer}
}

// SetAuthentikClient met à jour le client Authentik / SSO.
func (h *SettingsHandler) SetAuthentikClient(authClient authentik.Client) {
	h.authClient = authClient
}

// resolveEffectiveAuthentikConfig combine la configuration stockée en base SQL avec les variables d'environnement.
// Si une configuration est présente en base SQL, elle est prioritaire.
// Sinon, les variables d'environnement (Docker Compose / .env) sont utilisées en repli.
func (h *SettingsHandler) resolveEffectiveAuthentikConfig() config.AuthentikConfig {
	cfg := config.AuthentikConfig{
		Enabled:            false,
		UserGroup:          "jellygate-users",
		AdminGroup:         "jellygate-admins",
		JellyfinUserGroup:  "jellyfin-users",
		EnrollmentFlowSlug: "default-enrollment-flow",
	}

	var hasDBConfig bool
	// 1. Charger les valeurs existantes depuis la base de données SQL si présentes
	if h.db != nil {
		if dbCfg, err := h.db.GetAuthentikConfig(); err == nil {
			if dbCfg.URL != "" || dbCfg.IssuerURL != "" || dbCfg.ClientID != "" || dbCfg.APIToken != "" || dbCfg.Enabled {
				hasDBConfig = true
				cfg = dbCfg
				if cfg.UserGroup == "" {
					cfg.UserGroup = "jellygate-users"
				}
				if cfg.AdminGroup == "" {
					cfg.AdminGroup = "jellygate-admins"
				}
				if cfg.JellyfinUserGroup == "" {
					cfg.JellyfinUserGroup = "jellyfin-users"
				}
				if cfg.EnrollmentFlowSlug == "" {
					cfg.EnrollmentFlowSlug = "default-enrollment-flow"
				}
			}
		}
	}

	// 2. Si aucune configuration en base, charger les variables d'environnement (Docker Compose / .env)
	if !hasDBConfig && h.cfg != nil {
		env := h.cfg.Authentik
		if strings.TrimSpace(env.URL) != "" {
			cfg.URL = strings.TrimSpace(env.URL)
		}
		if strings.TrimSpace(env.IssuerURL) != "" {
			cfg.IssuerURL = strings.TrimSpace(env.IssuerURL)
		}
		if strings.TrimSpace(env.ClientID) != "" {
			cfg.ClientID = strings.TrimSpace(env.ClientID)
		}
		if strings.TrimSpace(env.ClientSecret) != "" {
			cfg.ClientSecret = strings.TrimSpace(env.ClientSecret)
		}
		if strings.TrimSpace(env.RedirectURL) != "" {
			cfg.RedirectURL = strings.TrimSpace(env.RedirectURL)
		}
		if strings.TrimSpace(env.APIToken) != "" {
			cfg.APIToken = strings.TrimSpace(env.APIToken)
		}
		if strings.TrimSpace(env.UserGroup) != "" {
			cfg.UserGroup = strings.TrimSpace(env.UserGroup)
		}
		if strings.TrimSpace(env.AdminGroup) != "" {
			cfg.AdminGroup = strings.TrimSpace(env.AdminGroup)
		}
		if strings.TrimSpace(env.JellyfinUserGroup) != "" {
			cfg.JellyfinUserGroup = strings.TrimSpace(env.JellyfinUserGroup)
		}
		if strings.TrimSpace(env.EnrollmentFlowSlug) != "" {
			cfg.EnrollmentFlowSlug = strings.TrimSpace(env.EnrollmentFlowSlug)
		}
		if env.Enabled || (env.URL != "" || env.IssuerURL != "" || env.ClientID != "") {
			cfg.Enabled = true
		}
	}

	return cfg
}

const maskedSecretValue = "********"

func isMaskedSecret(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == maskedSecretValue || strings.Contains(trimmed, "\u2022")
}

func maskedWebhooksConfig(cfg config.WebhooksConfig) config.WebhooksConfig {
	if strings.TrimSpace(cfg.Discord.URL) != "" {
		cfg.Discord.URL = maskedSecretValue
	}
	if strings.TrimSpace(cfg.Telegram.Token) != "" {
		cfg.Telegram.Token = maskedSecretValue
	}
	if strings.TrimSpace(cfg.Matrix.Token) != "" {
		cfg.Matrix.Token = maskedSecretValue
	}
	return cfg
}

func preserveMaskedWebhooks(input *config.WebhooksConfig, existing config.WebhooksConfig) {
	if input == nil {
		return
	}
	if isMaskedSecret(input.Discord.URL) {
		input.Discord.URL = existing.Discord.URL
	}
	if isMaskedSecret(input.Telegram.Token) {
		input.Telegram.Token = existing.Telegram.Token
	}
	if isMaskedSecret(input.Matrix.Token) {
		input.Matrix.Token = existing.Matrix.Token
	}
}

func normalizeWebhooksInput(input *config.WebhooksConfig) error {
	if input == nil {
		return fmt.Errorf("configuration webhooks vide")
	}

	var err error
	if input.Discord.URL, err = normalizeWebhookURL(input.Discord.URL); err != nil {
		return fmt.Errorf("discord.url: %w", err)
	}
	if input.Matrix.URL, err = normalizeWebhookURL(input.Matrix.URL); err != nil {
		return fmt.Errorf("matrix.url: %w", err)
	}
	input.Telegram.Token = strings.TrimSpace(input.Telegram.Token)
	input.Telegram.ChatID = strings.TrimSpace(input.Telegram.ChatID)
	input.Matrix.RoomID = strings.TrimSpace(input.Matrix.RoomID)
	input.Matrix.Token = strings.TrimSpace(input.Matrix.Token)
	return nil
}

func normalizeWebhookURL(raw string) (string, error) {
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

func (h *SettingsHandler) ensureAdmin(w http.ResponseWriter, r *http.Request) bool {
	sess := session.FromContext(r.Context())
	if sess == nil || !sess.IsAdmin {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: h.tr(r, "login_error_forbidden", "Acces reserve aux administrateurs")})
		return false
	}
	return true
}

// SaveAuthentik sauvegarde la configuration Authentik OIDC et API.
func (h *SettingsHandler) SaveAuthentik(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	var input config.AuthentikConfig
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "JSON invalide : " + err.Error()})
		return
	}

	existing := h.resolveEffectiveAuthentikConfig()
	if isMaskedSecret(input.APIToken) || input.APIToken == "" {
		input.APIToken = existing.APIToken
	}
	if isMaskedSecret(input.ClientSecret) || input.ClientSecret == "" {
		input.ClientSecret = existing.ClientSecret
	}

	if err := h.db.SaveAuthentikConfig(input); err != nil {
		slog.Error("Erreur sauvegarde config Authentik", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de sauvegarde"})
		return
	}

	if h.OnAuthentikReload != nil {
		h.OnAuthentikReload(input)
	}

	slog.Info("Configuration Authentik sauvegardée avec succès", "url", input.URL, "enabled", input.Enabled)
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Configuration Authentik sauvegardée"})
}

// GetAuthentikHealth renvoie le diagnostic complet et structuré de l'intégration Authentik.
func (h *SettingsHandler) GetAuthentikHealth(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	cfg := h.resolveEffectiveAuthentikConfig()

	client := h.authClient
	if client == nil || cfg.URL != "" || cfg.IssuerURL != "" {
		client = authentik.NewClient(cfg)
	}

	health := client.CheckHealth(r.Context(), cfg)
	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    health,
	})
}

// ReloadAuthentikFromEnv recharge la configuration SSO directement depuis les variables d'environnement (Docker Compose).
func (h *SettingsHandler) ReloadAuthentikFromEnv(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	if h.cfg == nil {
		writeJSON(w, http.StatusOK, APIResponse{
			Success: false,
			Message: "Aucune configuration d'environnement disponible",
		})
		return
	}

	envCfg := h.cfg.Authentik
	masked := envCfg
	if masked.APIToken != "" {
		masked.APIToken = maskedSecretValue
	}
	if masked.ClientSecret != "" {
		masked.ClientSecret = maskedSecretValue
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Paramètres SSO rechargés depuis l'environnement Docker (.env)",
		Data:    masked,
	})
}

type testUserPayload struct {
	Username string `json:"username"`
}

type testUserResult struct {
	Found            bool     `json:"found"`
	Username         string   `json:"username"`
	Name             string   `json:"name,omitempty"`
	Email            string   `json:"email,omitempty"`
	IsActive         bool     `json:"is_active"`
	Groups           []string `json:"groups"`
	IsJellyGateUser  bool     `json:"is_jellygate_user"`
	IsJellyGateAdmin bool     `json:"is_jellygate_admin"`
	IsJellyfinUser   bool     `json:"is_jellyfin_user"`
	Source           string   `json:"source"`
}

// TestAuthentikUser vérifie l'existence et les appartenances aux groupes d'un utilisateur dans le SSO / annuaire.
func (h *SettingsHandler) TestAuthentikUser(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	var input testUserPayload
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || strings.TrimSpace(input.Username) == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "Nom d'utilisateur requis",
		})
		return
	}

	username := strings.TrimSpace(input.Username)
	cfg := h.resolveEffectiveAuthentikConfig()

	reqUserGroup := strings.TrimSpace(cfg.UserGroup)
	if reqUserGroup == "" {
		reqUserGroup = "jellygate-users"
	}
	reqAdminGroup := strings.TrimSpace(cfg.AdminGroup)
	if reqAdminGroup == "" {
		reqAdminGroup = "jellygate-admins"
	}
	reqJfGroup := strings.TrimSpace(cfg.JellyfinUserGroup)
	if reqJfGroup == "" {
		reqJfGroup = "jellyfin-users"
	}

	hasGroup := func(groups []string, target string) bool {
		for _, g := range groups {
			if strings.EqualFold(strings.TrimSpace(g), target) {
				return true
			}
		}
		return false
	}

	// 1. Essayer via le client Authentik REST API si configuré
	client := h.authClient
	if client == nil || cfg.URL != "" || cfg.IssuerURL != "" {
		client = authentik.NewClient(cfg)
	}

	if client != nil && cfg.APIToken != "" {
		user, err := client.GetUserByUsername(r.Context(), username)
		if err == nil && user != nil {
			writeJSON(w, http.StatusOK, APIResponse{
				Success: true,
				Data: testUserResult{
					Found:            true,
					Username:         user.Username,
					Name:             user.Name,
					Email:            user.Email,
					IsActive:         user.IsActive,
					Groups:           user.Groups,
					IsJellyGateUser:  hasGroup(user.Groups, reqUserGroup) || hasGroup(user.Groups, reqAdminGroup),
					IsJellyGateAdmin: hasGroup(user.Groups, reqAdminGroup),
					IsJellyfinUser:   hasGroup(user.Groups, reqJfGroup),
					Source:           "authentik_api",
				},
			})
			return
		}
	}

	// 2. Recherche de secours dans la base locale / Jellyfin
	if h.db != nil {
		var (
			dbUser      string
			dbEmail     string
			dbCanInvite bool
			dbIsActive  bool
		)
		err := h.db.QueryRow(
			`SELECT username, email, can_invite, is_active FROM users WHERE username = ? OR email = ? LIMIT 1`,
			username, username,
		).Scan(&dbUser, &dbEmail, &dbCanInvite, &dbIsActive)
		if err == nil {
			groups := []string{reqUserGroup}
			if dbCanInvite {
				groups = append(groups, reqAdminGroup)
			}
			groups = append(groups, reqJfGroup)
			writeJSON(w, http.StatusOK, APIResponse{
				Success: true,
				Data: testUserResult{
					Found:            true,
					Username:         dbUser,
					Email:            dbEmail,
					IsActive:         dbIsActive,
					Groups:           groups,
					IsJellyGateUser:  true,
					IsJellyGateAdmin: dbCanInvite,
					IsJellyfinUser:   true,
					Source:           "database",
				},
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: testUserResult{
			Found:    false,
			Username: username,
		},
	})
}

// ── Structures de réponse ─────────────────────────────────────────────────────────────

// settingsResponse contient toute la configuration pour le frontend.
type settingsResponse struct {
	DefaultLang                       string                                 `json:"default_lang"`
	DatabaseType                      string                                 `json:"database_type"`
	BackupSQLiteOnly                  bool                                   `json:"backup_sqlite_only"`
	DefaultEmailBaseHeader            string                                 `json:"default_email_base_header"`
	DefaultEmailBaseFooter            string                                 `json:"default_email_base_footer"`
	PortalLinks                       config.PortalLinksConfig               `json:"portal_links"`
	InvitationProfile                 config.InvitationProfileConfig         `json:"invitation_profile"`
	AuthSession                       database.AuthSessionConfig             `json:"auth_session"`
	Authentik                         config.AuthentikConfig                 `json:"authentik"`
	SMTP                              config.SMTPConfig                      `json:"smtp"`
	Webhooks                          config.WebhooksConfig                  `json:"webhooks"`
	Backup                            config.BackupConfig                    `json:"backup"`
	EmailTemplates                    config.EmailTemplatesConfig            `json:"email_templates"`
	EmailTemplatesByLang              map[string]config.EmailTemplatesConfig `json:"email_templates_by_lang"`
	EmailTemplatesMultilingualEnabled bool                                   `json:"email_templates_multilingual_enabled"`
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

type authSessionInput struct {
	Remember30Days bool `json:"remember_30_days"`
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

	authentikCfg := h.resolveEffectiveAuthentikConfig()
	if authentikCfg.ClientSecret != "" {
		authentikCfg.ClientSecret = maskedSecretValue
	}
	if authentikCfg.APIToken != "" {
		authentikCfg.APIToken = maskedSecretValue
	}

	smtpCfg, err := h.db.GetSMTPConfig()
	if err != nil {
		slog.Error("Erreur lecture config SMTP", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: h.tr(r, "settings_error_smtp_read", "Erreur lecture configuration SMTP"),
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

	authSessionCfg, err := h.db.GetAuthSessionConfig()
	if err != nil {
		slog.Error("Erreur lecture config AuthSession", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: h.tr(r, "settings_auth_session_read_error", "Erreur lecture de la politique de session"),
		})
		return
	}

	// Masquer le token API et le client secret Authentik ainsi que le mot de passe SMTP dans la réponse
	maskedAuthentik := authentikCfg
	if maskedAuthentik.APIToken != "" {
		maskedAuthentik.APIToken = maskedSecretValue
	}
	if maskedAuthentik.ClientSecret != "" {
		maskedAuthentik.ClientSecret = maskedSecretValue
	}
	maskedSMTP := smtpCfg
	if maskedSMTP.Password != "" {
		maskedSMTP.Password = maskedSecretValue
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
			DefaultLang:                       defaultLang,
			DatabaseType:                      h.db.Driver(),
			BackupSQLiteOnly:                  h.db.IsSQLite(),
			DefaultEmailBaseHeader:            config.DefaultEmailBaseHeader(),
			DefaultEmailBaseFooter:            config.DefaultEmailBaseFooter(),
			PortalLinks:                       portalLinks,
			InvitationProfile:                 inviteProfileCfg,
			AuthSession:                       authSessionCfg,
			Authentik:                         maskedAuthentik,
			SMTP:                              maskedSMTP,
			Webhooks:                          maskedWebhooksConfig(webhooksCfg),
			Backup:                            backupCfg,
			EmailTemplates:                    emailTemplatesCfg,
			EmailTemplatesByLang:              emailTemplatesByLang,
			EmailTemplatesMultilingualEnabled: h.db.GetEmailTemplatesMultilingualEnabled(),
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
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "URL publique seerrr invalide: " + err.Error()})
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
		Message: h.tr(r, "settings_success_general_saved", "ParamÃ¨tres gÃ©nÃ©raux sauvegardÃ©s"),
	})
}

// SaveAuthSession sauvegarde la politique de sessions persistantes.
func (h *SettingsHandler) SaveAuthSession(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	var input authSessionInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "JSON invalide : " + err.Error(),
		})
		return
	}

	cfg, err := h.db.GetAuthSessionConfig()
	if err != nil {
		slog.Error("Erreur lecture config AuthSession", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: h.tr(r, "settings_auth_session_save_error", "Erreur de sauvegarde de la politique de session"),
		})
		return
	}

	cfg.Remember30Days = input.Remember30Days
	if err := h.db.SaveAuthSessionConfig(cfg); err != nil {
		slog.Error("Erreur sauvegarde config AuthSession", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: h.tr(r, "settings_auth_session_save_error", "Erreur de sauvegarde de la politique de session"),
		})
		return
	}

	_ = h.db.LogAction("settings.auth_session.saved", "", "", fmt.Sprintf("remember_30_days=%t", cfg.Remember30Days))
	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: h.tr(r, "settings_auth_session_saved", "Politique de session sauvegardee"),
		Data:    cfg,
	})
}

// RevokeAuthSessions invalide toutes les sessions admin actives.
func (h *SettingsHandler) RevokeAuthSessions(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	cfg, err := h.db.RevokeAuthSessionsBefore(time.Now().Unix())
	if err != nil {
		slog.Error("Erreur revocation sessions", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: h.tr(r, "settings_auth_session_revoke_error", "Impossible de deconnecter les sessions"),
		})
		return
	}

	actor := ""
	if sess := session.FromContext(r.Context()); sess != nil {
		actor = sess.Username
	}
	_ = h.db.LogAction("settings.auth_session.revoked", actor, "", fmt.Sprintf("revoked_before=%d", cfg.RevokedBefore))
	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: h.tr(r, "settings_auth_session_revoked", "Toutes les sessions ont ete deconnectees"),
		Data:    cfg,
	})
}

// FetchJellyfinServerName recupere le nom du serveur depuis l'API Jellyfin.
func (h *SettingsHandler) FetchJellyfinServerName(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	if h.jfClient == nil || !h.jfClient.IsConfigured() {
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
		previewLang = "en"
	}

	previewCfg := config.DefaultEmailTemplatesForLanguage(previewLang)
	previewCfg.BaseTemplateHeader = input.BaseTemplateHeader
	previewCfg.BaseTemplateFooter = input.BaseTemplateFooter
	normalizeEmailBaseTemplates(&previewCfg)
	tplRaw = config.PrepareEmailTemplateBodyForLanguage(previewLang, strings.TrimSpace(input.TemplateKey), tplRaw, previewCfg.BaseTemplateHeader, previewCfg.BaseTemplateFooter)

	links := resolvePortalLinks(nil, h.db)
	if links.JellyGateURL == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		links.JellyGateURL = fmt.Sprintf("%s://%s", scheme, r.Host)
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
		"Message":            config.DefaultEmailPreviewMessageForLanguage(previewLang),
		"AutomaticFooter":    config.DefaultEmailAutomaticFooterForLanguage(previewLang),
	}
	for k, v := range input.Context {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		sample[key] = v
	}
	normalizeEmailServerNameData(sample)
	sample["EmailLogoURL"] = resolveEmailLogoURL(sample, previewCfg.EmailLogoURL)

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
	if isMaskedSecret(input.Password) || input.Password == "" {
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

	existing, _ := h.db.GetWebhooksConfig()
	preserveMaskedWebhooks(&input, existing)
	if err := normalizeWebhooksInput(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "Configuration Webhooks invalide : " + err.Error(),
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
	Language            string                                 `json:"language"`
	DefaultLang         string                                 `json:"default_lang"`
	Template            *config.EmailTemplatesConfig           `json:"template"`
	TemplatesByLang     map[string]config.EmailTemplatesConfig `json:"templates_by_lang"`
	MultilingualEnabled *bool                                  `json:"multilingual_enabled"`
}

// SaveEmailTemplates sauvegarde les modeles e-mail (mono-langue ou multi-langue).
func (h *SettingsHandler) SaveEmailTemplates(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	rawBody, err := io.ReadAll(io.LimitReader(r.Body, 2<<20)) // 2 MB max to prevent DoS
	if err != nil {
		slog.Warn("Erreur lecture corps SaveEmailTemplates", "error", err)
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: h.tr(r, "common_bad_request", "Requete invalide"),
		})
		return
	}

	var payload saveEmailTemplatesInput
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		slog.Warn("JSON invalide SaveEmailTemplates", "error", err)
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: h.tr(r, "common_bad_request", "Requete invalide"),
		})
		return
	}

	forcedDefaultLang := ""
	if payload.MultilingualEnabled != nil && !*payload.MultilingualEnabled {
		rawDefaultLang := strings.TrimSpace(payload.DefaultLang)
		if rawDefaultLang == "" {
			rawDefaultLang = strings.TrimSpace(payload.Language)
		}
		if rawDefaultLang == "" {
			rawDefaultLang = h.db.GetDefaultLang()
		}
		forcedDefaultLang = config.NormalizeLanguageTag(rawDefaultLang)
		if !config.IsSupportedLanguage(forcedDefaultLang) {
			writeJSON(w, http.StatusBadRequest, APIResponse{
				Success: false,
				Message: "Langue serveur invalide pour les modeles e-mail",
			})
			return
		}
	}

	saveEmailTemplateMode := func() bool {
		if forcedDefaultLang != "" {
			if err := h.db.SetSetting(database.SettingDefaultLang, forcedDefaultLang); err != nil {
				slog.Error("Erreur sauvegarde langue serveur Email Templates", "lang", forcedDefaultLang, "error", err)
				writeJSON(w, http.StatusInternalServerError, APIResponse{
					Success: false,
					Message: "Erreur de sauvegarde de la langue serveur",
				})
				return false
			}
		}
		if payload.MultilingualEnabled == nil {
			return true
		}
		if err := h.db.SetEmailTemplatesMultilingualEnabled(*payload.MultilingualEnabled); err != nil {
			slog.Error("Erreur sauvegarde mode multi-langue Email Templates", "error", err)
			writeJSON(w, http.StatusInternalServerError, APIResponse{
				Success: false,
				Message: "Erreur de sauvegarde du mode multi-langue",
			})
			return false
		}
		return true
	}

	if payload.MultilingualEnabled != nil && len(payload.TemplatesByLang) == 0 && payload.Template == nil {
		if !saveEmailTemplateMode() {
			return
		}
		writeJSON(w, http.StatusOK, APIResponse{
			Success: true,
			Message: "Mode multi-langue sauvegarde",
		})
		return
	}

	if len(payload.TemplatesByLang) > 0 {
		sanitized := make(map[string]config.EmailTemplatesConfig, len(payload.TemplatesByLang))
		if forcedDefaultLang != "" {
			cfg, ok := payload.TemplatesByLang[forcedDefaultLang]
			if !ok {
				writeJSON(w, http.StatusBadRequest, APIResponse{
					Success: false,
					Message: "La langue serveur choisie est absente des modeles envoyes",
				})
				return
			}
			if err := sanitizeEmailTemplatesInput(forcedDefaultLang, &cfg); err != nil {
				writeJSON(w, http.StatusBadRequest, APIResponse{
					Success: false,
					Message: fmt.Sprintf("Langue %s: %s", forcedDefaultLang, err.Error()),
				})
				return
			}
			sanitized[forcedDefaultLang] = cfg
		} else {
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
		}

		if len(sanitized) == 0 {
			writeJSON(w, http.StatusBadRequest, APIResponse{
				Success: false,
				Message: "Aucune langue valide dans templates_by_lang",
			})
			return
		}

		if !saveEmailTemplateMode() {
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
		if forcedDefaultLang != "" {
			targetLang = forcedDefaultLang
		}
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

		if !saveEmailTemplateMode() {
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
	if forcedDefaultLang != "" {
		targetLang = forcedDefaultLang
	}
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
	if !saveEmailTemplateMode() {
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

type emailTemplateArchiveSettings struct {
	BaseTemplateHeader          string `json:"base_template_header"`
	BaseTemplateFooter          string `json:"base_template_footer"`
	EmailLogoURL                string `json:"email_logo_url"`
	DisableConfirmationEmail    bool   `json:"disable_confirmation_email"`
	DisableExpiryReminderEmails bool   `json:"disable_expiry_reminder_emails"`
	ExpiryReminderDays          int    `json:"expiry_reminder_days"`
	DisableInviteExpiryEmail    bool   `json:"disable_invite_expiry_email"`
	DisableUserCreationEmail    bool   `json:"disable_user_creation_email"`
	DisableUserDeletionEmail    bool   `json:"disable_user_deletion_email"`
	DisableUserDisabledEmail    bool   `json:"disable_user_disabled_email"`
	DisableUserEnabledEmail     bool   `json:"disable_user_enabled_email"`
	DisableUserExpiredEmail     bool   `json:"disable_user_expired_email"`
	DisableExpiryAdjustedEmail  bool   `json:"disable_expiry_adjusted_email"`
	DisableWelcomeEmail         bool   `json:"disable_welcome_email"`
}

func emailTemplateArchiveSettingsFromConfig(cfg config.EmailTemplatesConfig) emailTemplateArchiveSettings {
	return emailTemplateArchiveSettings{
		BaseTemplateHeader:          cfg.BaseTemplateHeader,
		BaseTemplateFooter:          cfg.BaseTemplateFooter,
		EmailLogoURL:                cfg.EmailLogoURL,
		DisableConfirmationEmail:    cfg.DisableConfirmationEmail,
		DisableExpiryReminderEmails: cfg.DisableExpiryReminderEmails,
		ExpiryReminderDays:          cfg.ExpiryReminderDays,
		DisableInviteExpiryEmail:    cfg.DisableInviteExpiryEmail,
		DisableUserCreationEmail:    cfg.DisableUserCreationEmail,
		DisableUserDeletionEmail:    cfg.DisableUserDeletionEmail,
		DisableUserDisabledEmail:    cfg.DisableUserDisabledEmail,
		DisableUserEnabledEmail:     cfg.DisableUserEnabledEmail,
		DisableUserExpiredEmail:     cfg.DisableUserExpiredEmail,
		DisableExpiryAdjustedEmail:  cfg.DisableExpiryAdjustedEmail,
		DisableWelcomeEmail:         cfg.DisableWelcomeEmail,
	}
}

func applyEmailTemplateArchiveSettings(cfg *config.EmailTemplatesConfig, settings emailTemplateArchiveSettings) {
	if cfg == nil {
		return
	}
	if strings.TrimSpace(settings.BaseTemplateHeader) != "" {
		cfg.BaseTemplateHeader = settings.BaseTemplateHeader
	}
	if strings.TrimSpace(settings.BaseTemplateFooter) != "" {
		cfg.BaseTemplateFooter = settings.BaseTemplateFooter
	}
	cfg.EmailLogoURL = strings.TrimSpace(settings.EmailLogoURL)
	cfg.DisableConfirmationEmail = settings.DisableConfirmationEmail
	cfg.DisableExpiryReminderEmails = settings.DisableExpiryReminderEmails
	if settings.ExpiryReminderDays > 0 {
		cfg.ExpiryReminderDays = settings.ExpiryReminderDays
	}
	cfg.DisableInviteExpiryEmail = settings.DisableInviteExpiryEmail
	cfg.DisableUserCreationEmail = settings.DisableUserCreationEmail
	cfg.DisableUserDeletionEmail = settings.DisableUserDeletionEmail
	cfg.DisableUserDisabledEmail = settings.DisableUserDisabledEmail
	cfg.DisableUserEnabledEmail = settings.DisableUserEnabledEmail
	cfg.DisableUserExpiredEmail = settings.DisableUserExpiredEmail
	cfg.DisableExpiryAdjustedEmail = settings.DisableExpiryAdjustedEmail
	cfg.DisableWelcomeEmail = settings.DisableWelcomeEmail
}

// ExportEmailTemplates exporte les modeles e-mail dans un ZIP lisible par langue.
func (h *SettingsHandler) ExportEmailTemplates(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	requestedLang := strings.TrimSpace(r.URL.Query().Get("lang"))
	if requestedLang == "" {
		requestedLang = h.db.GetDefaultLang()
	}

	templates, err := h.db.GetEmailTemplatesConfigByLanguage()
	if err != nil {
		slog.Error("Erreur lecture modeles e-mail export", "error", err)
		http.Error(w, "Erreur de lecture des modeles", http.StatusInternalServerError)
		return
	}

	langs := config.SupportedLanguageTags()
	filename := "jellygate-email-templates-all.zip"
	if !strings.EqualFold(requestedLang, "all") {
		lang := config.NormalizeLanguageTag(requestedLang)
		if !config.IsSupportedLanguage(lang) {
			http.Error(w, "Langue invalide", http.StatusBadRequest)
			return
		}
		langs = []string{lang}
		filename = "jellygate-email-templates-" + lang + ".zip"
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, lang := range langs {
		cfg, ok := templates[lang]
		if !ok {
			cfg = config.DefaultEmailTemplatesForLanguage(lang)
		}
		if err := writeEmailTemplateLanguageToZip(zw, lang, cfg); err != nil {
			_ = zw.Close()
			slog.Error("Erreur export ZIP modeles e-mail", "lang", lang, "error", err)
			http.Error(w, "Erreur de generation du ZIP", http.StatusInternalServerError)
			return
		}
	}
	if err := zw.Close(); err != nil {
		slog.Error("Erreur fermeture ZIP modeles e-mail", "error", err)
		http.Error(w, "Erreur de generation du ZIP", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", buf.Len()))
	_, _ = w.Write(buf.Bytes())
}

func writeEmailTemplateLanguageToZip(zw *zip.Writer, lang string, cfg config.EmailTemplatesConfig) error {
	meta, err := config.DefaultEmailTemplateMetaJSONForLanguage(lang)
	if err != nil {
		return err
	}
	if err := writeZipTextFile(zw, path.Join(lang, "_meta.json"), string(meta)+"\n"); err != nil {
		return err
	}
	settings, err := json.MarshalIndent(emailTemplateArchiveSettingsFromConfig(cfg), "", "  ")
	if err != nil {
		return err
	}
	if err := writeZipTextFile(zw, path.Join(lang, "_settings.json"), string(settings)+"\n"); err != nil {
		return err
	}
	for _, key := range config.EmailTemplateFileKeys() {
		subject, _ := config.EmailTemplateSubjectByKey(cfg, key.Key)
		body, _ := config.EmailTemplateBodyByKey(cfg, key.Key)
		body = config.EditableNoCodeEmailTemplateBodyForLanguage(lang, key.Key, body, cfg.BaseTemplateHeader, cfg.BaseTemplateFooter)
		if err := writeZipTextFile(zw, path.Join(lang, key.Dir, "subject.txt"), strings.TrimSpace(subject)+"\n"); err != nil {
			return err
		}
		if err := writeZipTextFile(zw, path.Join(lang, key.Dir, "body.txt"), strings.TrimSpace(body)+"\n"); err != nil {
			return err
		}
	}
	return nil
}

func writeZipTextFile(zw *zip.Writer, name, value string) error {
	f, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.WriteString(f, value)
	return err
}

// ImportEmailTemplates importe un ZIP au format lang/template/subject.txt|body.txt.
func (h *SettingsHandler) ImportEmailTemplates(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Fichier ZIP invalide: " + err.Error()})
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Fichier ZIP manquant"})
		return
	}
	defer file.Close()

	raw, err := io.ReadAll(io.LimitReader(file, 32<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Lecture du ZIP impossible: " + err.Error()})
		return
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ZIP invalide: " + err.Error()})
		return
	}

	existing, err := h.db.GetEmailTemplatesConfigByLanguage()
	if err != nil {
		existing = map[string]config.EmailTemplatesConfig{}
	}
	imported := map[string]config.EmailTemplatesConfig{}
	for _, entry := range zr.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		name := path.Clean(strings.ReplaceAll(entry.Name, "\\", "/"))
		if strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") {
			continue
		}
		parts := strings.Split(name, "/")
		if len(parts) < 2 {
			continue
		}
		lang := config.NormalizeLanguageTag(parts[0])
		if !config.IsSupportedLanguage(lang) {
			continue
		}
		cfg, ok := imported[lang]
		if !ok {
			if current, exists := existing[lang]; exists {
				cfg = current
			} else {
				cfg = config.DefaultEmailTemplatesForLanguage(lang)
			}
		}
		content, err := readZipTextFile(entry)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Lecture du ZIP impossible: " + err.Error()})
			return
		}
		if len(parts) == 2 && parts[1] == "_settings.json" {
			var settings emailTemplateArchiveSettings
			if err := json.Unmarshal([]byte(content), &settings); err != nil {
				writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Fichier _settings.json invalide pour " + lang})
				return
			}
			applyEmailTemplateArchiveSettings(&cfg, settings)
			imported[lang] = cfg
			continue
		}
		if len(parts) != 3 {
			continue
		}
		templateKey, ok := config.EmailTemplateKeyFromDir(parts[1])
		if !ok {
			continue
		}
		switch parts[2] {
		case "subject.txt":
			config.SetEmailTemplateSubjectByKey(&cfg, templateKey, strings.TrimSpace(content))
		case "body.txt":
			config.SetEmailTemplateBodyByKey(&cfg, templateKey, strings.TrimSpace(content))
		default:
			continue
		}
		imported[lang] = cfg
	}

	if len(imported) == 0 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Aucun modele e-mail valide trouve dans le ZIP"})
		return
	}
	for lang, cfg := range imported {
		if err := sanitizeEmailTemplatesInput(lang, &cfg); err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: fmt.Sprintf("Langue %s: %s", lang, err.Error())})
			return
		}
		imported[lang] = cfg
	}
	if err := h.db.SaveEmailTemplatesConfigByLanguage(imported); err != nil {
		slog.Error("Erreur import modeles e-mail", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur d'import des modeles"})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: fmt.Sprintf("%d langue(s) importee(s)", len(imported)),
	})
}

func readZipTextFile(entry *zip.File) (string, error) {
	rc, err := entry.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	raw, err := io.ReadAll(io.LimitReader(rc, 2<<20))
	if err != nil {
		return "", err
	}
	if !utf8.Valid(raw) {
		return "", fmt.Errorf("fichier %s non UTF-8", entry.Name)
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.TrimSpace(text), nil
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
