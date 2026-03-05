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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	jgldap "github.com/maelmoreau21/JellyGate/internal/ldap"
)

// ── SettingsHandler ─────────────────────────────────────────────────────────

// SettingsHandler gère les routes de configuration.
type SettingsHandler struct {
	db          *database.DB
	jellyfinURL string

	// Callbacks de rechargement — appelés après sauvegarde pour
	// réinitialiser les clients à chaud sans redémarrer le conteneur.
	OnLDAPReload     func(config.LDAPConfig)
	OnSMTPReload     func(config.SMTPConfig)
	OnWebhooksReload func(config.WebhooksConfig)
}

// NewSettingsHandler crée un nouveau handler de paramètres.
func NewSettingsHandler(db *database.DB, jellyfinURL string) *SettingsHandler {
	return &SettingsHandler{db: db, jellyfinURL: strings.TrimSpace(jellyfinURL)}
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
	if input.BindPassword == "••••••••" || input.BindPassword == "" {
		existing, _ := h.db.GetLDAPConfig()
		input.BindPassword = existing.BindPassword
	}
	if input.Port == 0 {
		input.Port = 636
	}
	if strings.TrimSpace(input.UserOU) == "" {
		input.UserOU = "CN=Users"
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

// TestLDAPUserLookup teste la recherche d'un utilisateur LDAP par sAMAccountName.
func (h *SettingsHandler) TestLDAPUserLookup(w http.ResponseWriter, r *http.Request) {
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
	entry, err := client.FindUser(username)
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
		Message: "Utilisateur LDAP trouve",
		Data: map[string]interface{}{
			"dn":           entry.DN,
			"username":     entry.Username,
			"display_name": entry.DisplayName,
			"email":        entry.Email,
			"upn":          entry.UPN,
			"is_disabled":  entry.IsDisabled,
		},
	})
}

// TestJellyfinLDAPAuth vérifie que l'authentification LDAP via le plugin Jellyfin fonctionne.
func (h *SettingsHandler) TestJellyfinLDAPAuth(w http.ResponseWriter, r *http.Request) {
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

	baseURL := strings.TrimRight(strings.TrimSpace(h.jellyfinURL), "/")
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
	req.Header.Set("X-Emby-Authorization", `MediaBrowser Client="JellyGate", Device="Server", DeviceId="jellygate", Version="0.1.0"`)

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

	input.DefaultLang = config.NormalizeLanguageTag(input.DefaultLang)

	// Validation : langues supportees par l'application
	if !config.IsSupportedLanguage(input.DefaultLang) {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "Langue invalide: fr, en, de, es, it, nl, pl, pt-BR, ru, zh",
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

	slog.Info("Configuration LDAP sauvegardée",
		"enabled", input.Enabled,
		"host", input.Host,
		"provision_mode", input.ProvisionMode,
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

	// Politique produit: toujours conserver les 7 dernières sauvegardes.
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
