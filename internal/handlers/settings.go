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
	DefaultLang    string                      `json:"default_lang"`
	LDAP           config.LDAPConfig           `json:"ldap"`
	SMTP           config.SMTPConfig           `json:"smtp"`
	Webhooks       config.WebhooksConfig       `json:"webhooks"`
	EmailTemplates config.EmailTemplatesConfig `json:"email_templates"`
}

// generalInput est le corps JSON attendu par SaveGeneral.
type generalInput struct {
	DefaultLang string `json:"default_lang"`
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
			DefaultLang:    defaultLang,
			LDAP:           maskedLDAP,
			SMTP:           maskedSMTP,
			Webhooks:       webhooksCfg,
			EmailTemplates: emailTemplatesCfg,
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
