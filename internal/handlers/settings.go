// Package handlers — settings.go
//
// API REST pour la gestion des paramètres stockés en base (table settings).
// Permet de lire et sauvegarder la configuration générale, LDAP, SMTP et Webhooks
// depuis l'interface d'administration.
//
// Routes :
//   - GET  /admin/api/settings          → Récupérer toute la configuration
//   - POST /admin/api/settings/general  → Sauvegarder les paramètres généraux (langue)
//   - POST /admin/api/settings/ldap     → Sauvegarder la config LDAP
//   - POST /admin/api/settings/smtp     → Sauvegarder la config SMTP
//   - POST /admin/api/settings/webhooks → Sauvegarder la config Webhooks
//   - POST /admin/api/settings/backup    → Sauvegarder la config de sauvegarde planifiée
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
)

// ── SettingsHandler ─────────────────────────────────────────────────────────

// SettingsHandler gère les routes de configuration.
type SettingsHandler struct {
	db *database.DB

	// Callbacks de rechargement — appelés après sauvegarde pour
	// réinitialiser les clients à chaud sans redémarrer le conteneur.
	OnLDAPReload     func(config.LDAPConfig)
	OnSMTPReload     func(config.SMTPConfig)
	OnWebhooksReload func(config.WebhooksConfig)
}

// NewSettingsHandler crée un nouveau handler de paramètres.
func NewSettingsHandler(db *database.DB) *SettingsHandler {
	return &SettingsHandler{db: db}
}

// ── Structures de réponse ───────────────────────────────────────────────────

// settingsResponse contient toute la configuration pour le frontend.
type settingsResponse struct {
	DefaultLang      string                      `json:"default_lang"`
	DatabaseType     string                      `json:"database_type"`
	BackupSQLiteOnly bool                        `json:"backup_sqlite_only"`
	PortalLinks      config.PortalLinksConfig    `json:"portal_links"`
	LDAP             config.LDAPConfig           `json:"ldap"`
	SMTP             config.SMTPConfig           `json:"smtp"`
	Webhooks         config.WebhooksConfig       `json:"webhooks"`
	Backup           config.BackupConfig         `json:"backup"`
	EmailTemplates   config.EmailTemplatesConfig `json:"email_templates"`
}

// generalInput est le corps JSON attendu par SaveGeneral.
type generalInput struct {
	DefaultLang   string `json:"default_lang"`
	JellyfinURL   string `json:"jellyfin_url"`
	JellyseerrURL string `json:"jellyseerr_url"`
	JellyTulliURL string `json:"jellytulli_url"`
}

// ── GET /admin/api/settings ─────────────────────────────────────────────────

// GetAll retourne toute la configuration stockée en base.
func (h *SettingsHandler) GetAll(w http.ResponseWriter, r *http.Request) {
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

	// Masquer le mot de passe LDAP et SMTP dans la réponse
	maskedLDAP := ldapCfg
	if maskedLDAP.BindPassword != "" {
		maskedLDAP.BindPassword = "••••••••"
	}
	maskedSMTP := smtpCfg
	if maskedSMTP.Password != "" {
		maskedSMTP.Password = "••••••••"
	}

	emailTemplatesCfg, err := h.db.GetEmailTemplatesConfig()
	if err != nil {
		slog.Error("Erreur lecture config Email Templates", "error", err)
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: settingsResponse{
			DefaultLang:      defaultLang,
			DatabaseType:     h.db.Driver(),
			BackupSQLiteOnly: h.db.IsSQLite(),
			PortalLinks:      portalLinks,
			LDAP:             maskedLDAP,
			SMTP:             maskedSMTP,
			Webhooks:         webhooksCfg,
			Backup:           backupCfg,
			EmailTemplates:   emailTemplatesCfg,
		},
	})
}

// ── POST /admin/api/settings/general ────────────────────────────────────────

// SaveGeneral sauvegarde les paramètres généraux (langue par défaut).
func (h *SettingsHandler) SaveGeneral(w http.ResponseWriter, r *http.Request) {
	var input generalInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "JSON invalide : " + err.Error(),
		})
		return
	}

	// Validation : seules "fr" et "en" sont acceptées
	if input.DefaultLang != "fr" && input.DefaultLang != "en" {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "Langue invalide : doit être 'fr' ou 'en'",
		})
		return
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
		JellyfinURL:   input.JellyfinURL,
		JellyseerrURL: input.JellyseerrURL,
		JellyTulliURL: input.JellyTulliURL,
	}); err != nil {
		slog.Error("Erreur sauvegarde portal_links", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de sauvegarde des URLs publiques",
		})
		return
	}

	slog.Info("Langue par défaut mise à jour", "lang", input.DefaultLang)
	_ = h.db.LogAction("settings.general.saved", "", "", "default_lang="+input.DefaultLang)

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Paramètres généraux sauvegardés",
	})
}

// ── POST /admin/api/settings/ldap ───────────────────────────────────────────

// SaveLDAP sauvegarde la configuration LDAP.
func (h *SettingsHandler) SaveLDAP(w http.ResponseWriter, r *http.Request) {
	var input config.LDAPConfig
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "JSON invalide : " + err.Error(),
		})
		return
	}

	// Si le mot de passe est masqué (pas changé), conserver l'ancien
	if input.BindPassword == "••••••••" || input.BindPassword == "" {
		existing, _ := h.db.GetLDAPConfig()
		input.BindPassword = existing.BindPassword
	}

	// Valeurs par défaut
	if input.Port == 0 {
		input.Port = 636
	}
	if input.UserOU == "" {
		input.UserOU = "CN=Users"
	}

	if err := h.db.SaveLDAPConfig(input); err != nil {
		slog.Error("Erreur sauvegarde config LDAP", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de sauvegarde",
		})
		return
	}

	slog.Info("Configuration LDAP sauvegardée",
		"enabled", input.Enabled,
		"host", input.Host,
	)

	// Rechargement à chaud
	if h.OnLDAPReload != nil {
		h.OnLDAPReload(input)
	}

	_ = h.db.LogAction("settings.ldap.saved", "", "", "")

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Configuration LDAP sauvegardée",
	})
}

// ── POST /admin/api/settings/smtp ───────────────────────────────────────────

// SaveSMTP sauvegarde la configuration SMTP.
func (h *SettingsHandler) SaveSMTP(w http.ResponseWriter, r *http.Request) {
	var input config.SMTPConfig
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "JSON invalide : " + err.Error(),
		})
		return
	}

	// Si le mot de passe est masqué, conserver l'ancien
	if input.Password == "••••••••" || input.Password == "" {
		existing, _ := h.db.GetSMTPConfig()
		input.Password = existing.Password
	}

	// Valeurs par défaut
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

	slog.Info("Configuration SMTP sauvegardée", "host", input.Host)

	// Rechargement à chaud
	if h.OnSMTPReload != nil {
		h.OnSMTPReload(input)
	}

	_ = h.db.LogAction("settings.smtp.saved", "", "", "")

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Configuration SMTP sauvegardée",
	})
}

// ── POST /admin/api/settings/webhooks ───────────────────────────────────────

// SaveWebhooks sauvegarde la configuration Webhooks.
func (h *SettingsHandler) SaveWebhooks(w http.ResponseWriter, r *http.Request) {
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

	slog.Info("Configuration Webhooks sauvegardée")

	// Rechargement à chaud
	if h.OnWebhooksReload != nil {
		h.OnWebhooksReload(input)
	}

	_ = h.db.LogAction("settings.webhooks.saved", "", "", "")

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Configuration Webhooks sauvegardée",
	})
}

// ── POST /admin/api/settings/backup ────────────────────────────────────────

// SaveBackup sauvegarde la configuration des sauvegardes planifiées.
func (h *SettingsHandler) SaveBackup(w http.ResponseWriter, r *http.Request) {
	if !h.db.IsSQLite() {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "Planification backup locale indisponible en mode PostgreSQL (utiliser pg_dump/pg_restore)",
		})
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
	if input.RetentionCount < 1 || input.RetentionCount > 365 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Rétention invalide (1-365)"})
		return
	}

	if err := h.db.SaveBackupConfig(input); err != nil {
		slog.Error("Erreur sauvegarde config Backup", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de sauvegarde",
		})
		return
	}

	_ = h.db.LogAction("settings.backup.saved", "", "", "")
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Configuration de sauvegarde sauvegardée"})
}

// ── POST /admin/api/settings/email-templates ────────────────────────────────

// SaveEmailTemplates sauvegarde les modèles de courriels personnalisés.
func (h *SettingsHandler) SaveEmailTemplates(w http.ResponseWriter, r *http.Request) {
	var input config.EmailTemplatesConfig
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "JSON invalide : " + err.Error(),
		})
		return
	}

	if input.ExpiryReminderDays == 0 {
		input.ExpiryReminderDays = 3
	}
	if input.ExpiryReminderDays < 1 || input.ExpiryReminderDays > 365 {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "expiry_reminder_days doit etre entre 1 et 365",
		})
		return
	}

	if err := h.db.SaveEmailTemplatesConfig(input); err != nil {
		slog.Error("Erreur sauvegarde config Email Templates", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de sauvegarde des modèles",
		})
		return
	}

	slog.Info("Configuration Email Templates sauvegardée")
	_ = h.db.LogAction("settings.email_templates.saved", "", "", "")

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Modèles d'emails sauvegardés avec succès",
	})
}
