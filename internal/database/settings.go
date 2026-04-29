// Package database — settings.go
//
// CRUD pour la table `settings` (clé/valeur).
// Stocke la configuration LDAP, SMTP et Webhooks en JSON,
// ainsi que des flags comme ldap_enabled.
package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

// ── Clés de settings ────────────────────────────────────────────────────────

const (
	SettingLDAPConfig                  = "ldap_config"                    // JSON: config.LDAPConfig
	SettingSMTPConfig                  = "smtp_config"                    // JSON: config.SMTPConfig
	SettingWebhooksConfig              = "webhooks_config"                // JSON: config.WebhooksConfig
	SettingPortalLinks                 = "portal_links"                   // JSON: config.PortalLinksConfig
	SettingEmailTemplates              = "email_templates"                // JSON: config.EmailTemplatesConfig
	SettingEmailTemplatesByLang        = "email_templates_by_lang"        // JSON: map[lang]config.EmailTemplatesConfig
	SettingBackupConfig                = "backup_config"                  // JSON: config.BackupConfig
	SettingProductFeatures             = "product_features"               // JSON: config.ProductFeaturesConfig
	SettingJellyfinPresets             = "jellyfin_presets"               // JSON: []config.JellyfinPolicyPreset
	SettingGroupMappings               = "group_mappings"                 // JSON: []config.GroupPolicyMapping
	SettingInviteProfile               = "invite_profile"                 // JSON: config.InvitationProfileConfig
	SettingBackupLastRun               = "backup_last_run"                // Date locale YYYY-MM-DD
	SettingDefaultLang                 = "default_lang"                   // Langue par defaut du serveur (fr, en, de, es, it, nl, pl, pt-br, ru, zh)
	SettingEmailVerificationBackfillV1 = "email_verification_backfill_v1" // Flag one-shot pour les comptes historiques
)

// GetDefaultLang retourne la langue par défaut du serveur.
// Retourne "fr" si la clé n'existe pas ou en cas d'erreur.
func (db *DB) GetDefaultLang() string {
	val, err := db.GetSetting(SettingDefaultLang)
	if err != nil || val == "" {
		return "fr"
	}
	lang := config.NormalizeLanguageTag(val)
	if !config.IsSupportedLanguage(lang) {
		return "fr"
	}
	return lang
}

// ── Get / Set générique ─────────────────────────────────────────────────────

// GetSetting récupère la valeur brute d'un paramètre par sa clé.
// Retourne "" si la clé n'existe pas.
func (db *DB) GetSetting(key string) (string, error) {
	var value sql.NullString
	err := db.QueryRow(
		`SELECT value FROM settings WHERE key = ?`, key,
	).Scan(&value)

	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("GetSetting(%q): %w", key, err)
	}

	return db.decrypt(value.String)
}

// SetSetting insère ou met à jour un paramètre (UPSERT).
func (db *DB) SetSetting(key, value string) error {
	_, err := db.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at
	`, key, value, time.Now().UTC().Format(time.RFC3339))

	if err != nil {
		return fmt.Errorf("SetSetting(%q): %w", key, err)
	}
	return nil
}

// GetAllSettings récupère tous les paramètres sous forme de map.
func (db *DB) GetAllSettings() (map[string]string, error) {
	rows, err := db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("GetAllSettings: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("GetAllSettings scan: %w", err)
		}
		result[k] = v
	}
	return result, rows.Err()
}

// GetPortalLinksConfig récupère les URL publiques (Jellyfin/Jellyseerr/JellyTrack).
func (db *DB) GetPortalLinksConfig() (config.PortalLinksConfig, error) {
	cfg := config.DefaultPortalLinks()

	raw, err := db.GetSetting(SettingPortalLinks)
	if err != nil {
		return cfg, err
	}
	if strings.TrimSpace(raw) == "" {
		return cfg, nil
	}

	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		slog.Warn("Erreur de parsing de la config PortalLinks", "error", err)
		return config.DefaultPortalLinks(), nil
	}

	cfg.JellyfinURL = strings.TrimSpace(cfg.JellyfinURL)
	cfg.JellyGateURL = strings.TrimSpace(cfg.JellyGateURL)
	cfg.JellyfinServerName = strings.TrimSpace(cfg.JellyfinServerName)
	if cfg.JellyfinServerName == "" {
		cfg.JellyfinServerName = "Jellyfin"
	}
	cfg.JellyseerrURL = strings.TrimSpace(cfg.JellyseerrURL)
	cfg.JellyTrackURL = strings.TrimSpace(cfg.JellyTrackURL)

	return cfg, nil
}

// SavePortalLinksConfig sauvegarde les URL publiques.
func (db *DB) SavePortalLinksConfig(cfg config.PortalLinksConfig) error {
	cfg.JellyfinURL = strings.TrimSpace(cfg.JellyfinURL)
	cfg.JellyGateURL = strings.TrimSpace(cfg.JellyGateURL)
	cfg.JellyfinServerName = strings.TrimSpace(cfg.JellyfinServerName)
	if cfg.JellyfinServerName == "" {
		cfg.JellyfinServerName = "Jellyfin"
	}
	cfg.JellyseerrURL = strings.TrimSpace(cfg.JellyseerrURL)
	cfg.JellyTrackURL = strings.TrimSpace(cfg.JellyTrackURL)

	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("SavePortalLinksConfig marshal: %w", err)
	}
	return db.SetSetting(SettingPortalLinks, string(data))
}

// ── LDAP Config ─────────────────────────────────────────────────────────────

// GetLDAPConfig récupère la configuration LDAP depuis la base.
// Retourne une config par défaut (Enabled=false) si non configurée.
func (db *DB) GetLDAPConfig() (config.LDAPConfig, error) {
	cfg := config.LDAPConfig{
		Enabled:              false,
		Port:                 636,
		UseTLS:               true,
		SearchFilter:         "(&(|(objectClass=user)(objectClass=person)(objectClass=organizationalPerson)(objectClass=inetOrgPerson)(objectClass=posixAccount))(|(uid={username})(sAMAccountName={username})(cn={username})(userPrincipalName={username})(mail={username})))",
		SearchAttributes:     "uid,sAMAccountName,cn,userPrincipalName,mail",
		UIDAttribute:         "uid",
		UsernameAttribute:    "auto",
		AdminFilter:          "",
		AdminFilterMemberUID: false,
		UserObjectClass:      "auto",
		GroupMemberAttr:      "auto",
		UserOU:               "CN=Users",
		ProvisionMode:        "hybrid",
		JellyfinGroup:        "jellyfin",
		InviterGroup:         "jellyfin-Parrainage",
		AdministratorsGroup:  "jellyfin-administrateur",
	}

	raw, err := db.GetSetting(SettingLDAPConfig)
	if err != nil {
		return cfg, err
	}
	if raw == "" {
		return cfg, nil
	}

	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		slog.Warn("Erreur de parsing de la config LDAP", "error", err)
		return cfg, nil
	}

	cfg.ProvisionMode = strings.ToLower(strings.TrimSpace(cfg.ProvisionMode))
	if cfg.ProvisionMode == "" {
		cfg.ProvisionMode = "hybrid"
	}
	if cfg.ProvisionMode != "hybrid" && cfg.ProvisionMode != "ldap_only" {
		cfg.ProvisionMode = "hybrid"
	}

	cfg.UsernameAttribute = strings.TrimSpace(cfg.UsernameAttribute)
	if cfg.UsernameAttribute == "" {
		cfg.UsernameAttribute = "auto"
	}

	cfg.SearchFilter = strings.TrimSpace(cfg.SearchFilter)
	if cfg.SearchFilter == "" {
		cfg.SearchFilter = "(&(|(objectClass=user)(objectClass=person)(objectClass=organizationalPerson)(objectClass=inetOrgPerson)(objectClass=posixAccount))(|(uid={username})(sAMAccountName={username})(cn={username})(userPrincipalName={username})(mail={username})))"
	}

	cfg.SearchAttributes = strings.TrimSpace(cfg.SearchAttributes)
	if cfg.SearchAttributes == "" {
		cfg.SearchAttributes = "uid,sAMAccountName,cn,userPrincipalName,mail"
	}

	cfg.UIDAttribute = strings.TrimSpace(cfg.UIDAttribute)
	if cfg.UIDAttribute == "" {
		cfg.UIDAttribute = "uid"
	}

	cfg.AdminFilter = strings.TrimSpace(cfg.AdminFilter)

	cfg.UserObjectClass = strings.TrimSpace(cfg.UserObjectClass)
	if cfg.UserObjectClass == "" {
		cfg.UserObjectClass = "auto"
	}

	cfg.GroupMemberAttr = strings.TrimSpace(cfg.GroupMemberAttr)
	if cfg.GroupMemberAttr == "" {
		cfg.GroupMemberAttr = "auto"
	}

	if strings.TrimSpace(cfg.JellyfinGroup) == "" {
		cfg.JellyfinGroup = strings.TrimSpace(cfg.UserGroup)
	}
	if strings.TrimSpace(cfg.JellyfinGroup) == "" {
		cfg.JellyfinGroup = "jellyfin"
	}
	if strings.TrimSpace(cfg.InviterGroup) == "" {
		cfg.InviterGroup = "jellyfin-Parrainage"
	}
	if strings.TrimSpace(cfg.AdministratorsGroup) == "" {
		cfg.AdministratorsGroup = "jellyfin-administrateur"
	}

	return cfg, nil
}

// SaveLDAPConfig sauvegarde la configuration LDAP dans la base.
func (db *DB) SaveLDAPConfig(cfg config.LDAPConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("SaveLDAPConfig marshal: %w", err)
	}
	enc, err := db.encrypt(string(data))
	if err != nil {
		return fmt.Errorf("SaveLDAPConfig encrypt: %w", err)
	}
	return db.SetSetting(SettingLDAPConfig, enc)
}

// IsLDAPEnabled vérifie rapidement si l'intégration LDAP est activée.
func (db *DB) IsLDAPEnabled() bool {
	cfg, err := db.GetLDAPConfig()
	if err != nil {
		slog.Warn("Erreur lecture config LDAP", "error", err)
		return false
	}
	return cfg.Enabled
}

// ── SMTP Config ─────────────────────────────────────────────────────────────

// GetSMTPConfig récupère la configuration SMTP depuis la base.
// Retourne une config par défaut si non configurée.
func (db *DB) GetSMTPConfig() (config.SMTPConfig, error) {
	cfg := config.SMTPConfig{
		Port:   587,
		UseTLS: true,
	}

	raw, err := db.GetSetting(SettingSMTPConfig)
	if err != nil {
		return cfg, err
	}
	if raw == "" {
		return cfg, nil
	}

	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		slog.Warn("Erreur de parsing de la config SMTP", "error", err)
		return cfg, nil
	}

	return cfg, nil
}

// SaveSMTPConfig sauvegarde la configuration SMTP dans la base.
func (db *DB) SaveSMTPConfig(cfg config.SMTPConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("SaveSMTPConfig marshal: %w", err)
	}
	enc, err := db.encrypt(string(data))
	if err != nil {
		return fmt.Errorf("SaveSMTPConfig encrypt: %w", err)
	}
	return db.SetSetting(SettingSMTPConfig, enc)
}

// ── Backup Config ───────────────────────────────────────────────────────────

// GetBackupConfig récupère la configuration des sauvegardes planifiées.
func (db *DB) GetBackupConfig() (config.BackupConfig, error) {
	cfg := config.DefaultBackupConfig()

	raw, err := db.GetSetting(SettingBackupConfig)
	if err != nil {
		return cfg, err
	}
	if raw == "" {
		return cfg, nil
	}

	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		slog.Warn("Erreur de parsing de la config Backup", "error", err)
		return config.DefaultBackupConfig(), nil
	}

	if cfg.Hour < 0 || cfg.Hour > 23 {
		cfg.Hour = 3
	}
	if cfg.Minute < 0 || cfg.Minute > 59 {
		cfg.Minute = 0
	}
	// Retention is intentionally fixed to the last 7 archives.
	cfg.RetentionCount = 7

	return cfg, nil
}

// SaveBackupConfig sauvegarde la configuration des sauvegardes planifiées.
func (db *DB) SaveBackupConfig(cfg config.BackupConfig) error {
	// Retention is intentionally fixed to the last 7 archives.
	cfg.RetentionCount = 7
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("SaveBackupConfig marshal: %w", err)
	}
	return db.SetSetting(SettingBackupConfig, string(data))
}

// GetProductFeaturesConfig récupère la configuration des modules produit avances.
func (db *DB) GetProductFeaturesConfig() (config.ProductFeaturesConfig, error) {
	cfg := config.DefaultProductFeaturesConfig()

	raw, err := db.GetSetting(SettingProductFeatures)
	if err != nil {
		return cfg, err
	}
	if strings.TrimSpace(raw) == "" {
		return cfg, nil
	}

	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		slog.Warn("Erreur de parsing de la config ProductFeatures", "error", err)
		return config.DefaultProductFeaturesConfig(), nil
	}

	return config.NormalizeProductFeaturesConfig(cfg), nil
}

// SaveProductFeaturesConfig sauvegarde la configuration des modules produit avances.
func (db *DB) SaveProductFeaturesConfig(cfg config.ProductFeaturesConfig) error {
	cfg = config.NormalizeProductFeaturesConfig(cfg)
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("SaveProductFeaturesConfig marshal: %w", err)
	}
	return db.SetSetting(SettingProductFeatures, string(data))
}

// GetBackupLastRun retourne la date locale YYYY-MM-DD du dernier backup auto.
func (db *DB) GetBackupLastRun() string {
	val, err := db.GetSetting(SettingBackupLastRun)
	if err != nil {
		return ""
	}
	return val
}

// SetBackupLastRun enregistre la date locale YYYY-MM-DD du dernier backup auto.
func (db *DB) SetBackupLastRun(day string) error {
	return db.SetSetting(SettingBackupLastRun, day)
}

// ── Emails Templates Config ─────────────────────────────────────────────────

// GetEmailTemplatesConfig récupère la configuration des gabarits d'emails.
func normalizeEmailTemplatesConfig(cfg *config.EmailTemplatesConfig) {
	if cfg == nil {
		return
	}
	config.UpgradeLegacyEmailTemplates(cfg)
	if cfg.ExpiryReminderDays < 1 || cfg.ExpiryReminderDays > 365 {
		cfg.ExpiryReminderDays = 3
	}
}

func defaultEmailTemplatesForSupportedLanguage(lang string) config.EmailTemplatesConfig {
	normalized := config.NormalizeLanguageTag(lang)
	if !config.IsSupportedLanguage(normalized) {
		normalized = "fr"
	}
	return config.DefaultEmailTemplatesForLanguage(normalized)
}

func copySharedEmailTemplateFields(dst *config.EmailTemplatesConfig, src config.EmailTemplatesConfig) {
	if dst == nil {
		return
	}
	dst.EmailLogoURL = src.EmailLogoURL
	dst.BaseTemplateHeader = src.BaseTemplateHeader
	dst.BaseTemplateFooter = src.BaseTemplateFooter
	dst.DisableConfirmationEmail = src.DisableConfirmationEmail
	dst.DisableExpiryReminderEmails = src.DisableExpiryReminderEmails
	dst.ExpiryReminderDays = src.ExpiryReminderDays
	dst.DisableInviteExpiryEmail = src.DisableInviteExpiryEmail
	dst.DisablePreSignupHelpEmail = src.DisablePreSignupHelpEmail
	dst.DisablePostSignupHelpEmail = src.DisablePostSignupHelpEmail
	dst.DisableUserCreationEmail = src.DisableUserCreationEmail
	dst.DisableUserDeletionEmail = src.DisableUserDeletionEmail
	dst.DisableUserDisabledEmail = src.DisableUserDisabledEmail
	dst.DisableUserEnabledEmail = src.DisableUserEnabledEmail
	dst.DisableUserExpiredEmail = src.DisableUserExpiredEmail
	dst.DisableExpiryAdjustedEmail = src.DisableExpiryAdjustedEmail
	dst.DisableWelcomeEmail = src.DisableWelcomeEmail
}

func syncSharedEmailTemplateFields(templates map[string]config.EmailTemplatesConfig, defaultLang string) {
	if len(templates) == 0 {
		return
	}

	anchorLang := config.NormalizeLanguageTag(defaultLang)
	anchor, ok := templates[anchorLang]
	if !ok {
		for _, lang := range config.SupportedLanguageTags() {
			if cfg, exists := templates[lang]; exists {
				anchor = cfg
				anchorLang = lang
				ok = true
				break
			}
		}
	}
	if !ok {
		anchorLang = "fr"
		anchor = defaultEmailTemplatesForSupportedLanguage(anchorLang)
	}

	for _, lang := range config.SupportedLanguageTags() {
		cfg, exists := templates[lang]
		if !exists {
			cfg = defaultEmailTemplatesForSupportedLanguage(lang)
		}
		if lang != anchorLang {
			copySharedEmailTemplateFields(&cfg, anchor)
		}
		normalizeEmailTemplatesConfig(&cfg)
		templates[lang] = cfg
	}
}

func (db *DB) getLegacyEmailTemplatesConfig() (config.EmailTemplatesConfig, error) {
	cfg := defaultEmailTemplatesForSupportedLanguage(db.GetDefaultLang())

	raw, err := db.GetSetting(SettingEmailTemplates)
	if err != nil {
		return cfg, err
	}
	if raw == "" {
		return cfg, nil
	}

	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		slog.Warn("Erreur de parsing de la config EmailTemplates", "error", err)
		return cfg, nil // Fallback silenceux sur defaults
	}

	normalizeEmailTemplatesConfig(&cfg)
	return cfg, nil
}

func parseEmailTemplatesByLanguage(raw string) (map[string]config.EmailTemplatesConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]config.EmailTemplatesConfig{}, nil
	}

	entries := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		var wrapped struct {
			Templates map[string]json.RawMessage `json:"templates"`
		}
		if wrappedErr := json.Unmarshal([]byte(raw), &wrapped); wrappedErr != nil {
			return nil, err
		}
		entries = wrapped.Templates
	}

	result := make(map[string]config.EmailTemplatesConfig, len(entries))
	for rawLang, payload := range entries {
		lang := config.NormalizeLanguageTag(rawLang)
		if !config.IsSupportedLanguage(lang) {
			continue
		}

		cfg := defaultEmailTemplatesForSupportedLanguage(lang)
		if len(payload) > 0 {
			if err := json.Unmarshal(payload, &cfg); err != nil {
				slog.Warn("Erreur parsing template e-mail par langue", "lang", rawLang, "error", err)
				continue
			}
		}
		normalizeEmailTemplatesConfig(&cfg)
		result[lang] = cfg
	}

	return result, nil
}

// GetEmailTemplatesConfigByLanguage retourne tous les templates par langue
// avec fallback compatible vers le stockage legacy.
func (db *DB) GetEmailTemplatesConfigByLanguage() (map[string]config.EmailTemplatesConfig, error) {
	defaultLang := config.NormalizeLanguageTag(db.GetDefaultLang())
	if !config.IsSupportedLanguage(defaultLang) {
		defaultLang = "fr"
	}
	legacyCfg, err := db.getLegacyEmailTemplatesConfig()
	if err != nil {
		return nil, err
	}

	templates := make(map[string]config.EmailTemplatesConfig, len(config.SupportedLanguageTags()))
	for _, lang := range config.SupportedLanguageTags() {
		templates[lang] = defaultEmailTemplatesForSupportedLanguage(lang)
	}
	templates[defaultLang] = legacyCfg

	raw, err := db.GetSetting(SettingEmailTemplatesByLang)
	if err != nil {
		return templates, err
	}
	if strings.TrimSpace(raw) == "" {
		syncSharedEmailTemplateFields(templates, defaultLang)
		return templates, nil
	}

	parsed, err := parseEmailTemplatesByLanguage(raw)
	if err != nil {
		slog.Warn("Erreur parsing email_templates_by_lang", "error", err)
		syncSharedEmailTemplateFields(templates, defaultLang)
		return templates, nil
	}
	for lang, cfg := range parsed {
		templates[lang] = cfg
	}

	syncSharedEmailTemplateFields(templates, defaultLang)
	return templates, nil
}

// GetEmailTemplatesConfigForLang retourne les templates pour la langue demandee
// avec fallback sur la langue par defaut.
func (db *DB) GetEmailTemplatesConfigForLang(lang string) (config.EmailTemplatesConfig, string, error) {
	templates, err := db.GetEmailTemplatesConfigByLanguage()
	defaultLang := config.NormalizeLanguageTag(db.GetDefaultLang())
	if !config.IsSupportedLanguage(defaultLang) {
		defaultLang = "fr"
	}
	if err != nil {
		return defaultEmailTemplatesForSupportedLanguage(defaultLang), defaultLang, err
	}

	requested := config.NormalizeLanguageTag(lang)
	if config.IsSupportedLanguage(requested) {
		if cfg, ok := templates[requested]; ok {
			return cfg, requested, nil
		}
	}

	if cfg, ok := templates[defaultLang]; ok {
		return cfg, defaultLang, nil
	}
	if cfg, ok := templates["en"]; ok {
		return cfg, "en", nil
	}
	if cfg, ok := templates["fr"]; ok {
		return cfg, "fr", nil
	}
	return defaultEmailTemplatesForSupportedLanguage(defaultLang), defaultLang, nil
}

// GetEmailTemplatesConfig retourne la configuration active de la langue par defaut.
func (db *DB) GetEmailTemplatesConfig() (config.EmailTemplatesConfig, error) {
	cfg, _, err := db.GetEmailTemplatesConfigForLang(db.GetDefaultLang())
	return cfg, err
}

// SaveEmailTemplatesConfigByLanguage sauvegarde les templates par langue
// et met a jour la cle legacy sur la langue par defaut.
func (db *DB) SaveEmailTemplatesConfigByLanguage(values map[string]config.EmailTemplatesConfig) error {
	existing, err := db.GetEmailTemplatesConfigByLanguage()
	if err != nil {
		existing = map[string]config.EmailTemplatesConfig{}
	}

	normalized := make(map[string]config.EmailTemplatesConfig, len(config.SupportedLanguageTags()))
	for _, lang := range config.SupportedLanguageTags() {
		if cfg, ok := existing[lang]; ok {
			normalized[lang] = cfg
			continue
		}
		normalized[lang] = defaultEmailTemplatesForSupportedLanguage(lang)
	}

	for rawLang, cfg := range values {
		lang := config.NormalizeLanguageTag(rawLang)
		if !config.IsSupportedLanguage(lang) {
			continue
		}
		normalizeEmailTemplatesConfig(&cfg)
		normalized[lang] = cfg
	}

	defaultLang := config.NormalizeLanguageTag(db.GetDefaultLang())
	if !config.IsSupportedLanguage(defaultLang) {
		defaultLang = "fr"
	}
	syncSharedEmailTemplateFields(normalized, defaultLang)

	payload, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("SaveEmailTemplatesConfigByLanguage marshal: %w", err)
	}
	if err := db.SetSetting(SettingEmailTemplatesByLang, string(payload)); err != nil {
		return err
	}

	legacyPayload, err := json.Marshal(normalized[defaultLang])
	if err != nil {
		return fmt.Errorf("SaveEmailTemplatesConfig legacy marshal: %w", err)
	}
	return db.SetSetting(SettingEmailTemplates, string(legacyPayload))
}

// SaveEmailTemplatesConfigForLang sauvegarde les templates pour une langue cible.
func (db *DB) SaveEmailTemplatesConfigForLang(lang string, cfg config.EmailTemplatesConfig) error {
	normalizedLang := config.NormalizeLanguageTag(lang)
	if !config.IsSupportedLanguage(normalizedLang) {
		normalizedLang = db.GetDefaultLang()
	}

	templates, err := db.GetEmailTemplatesConfigByLanguage()
	if err != nil {
		return err
	}
	templates[normalizedLang] = cfg
	return db.SaveEmailTemplatesConfigByLanguage(templates)
}

// SaveEmailTemplatesConfig sauvegarde la configuration des templates
// sur la langue par defaut de l'instance.
func (db *DB) SaveEmailTemplatesConfig(cfg config.EmailTemplatesConfig) error {
	return db.SaveEmailTemplatesConfigForLang(db.GetDefaultLang(), cfg)
}

// ── Jellyfin Presets Config ───────────────────────────────────────────────

func normalizeJellyfinPolicyPreset(preset config.JellyfinPolicyPreset) config.JellyfinPolicyPreset {
	if preset.MaxSessions < 0 {
		preset.MaxSessions = 0
	}
	if preset.BitrateLimit < 0 {
		preset.BitrateLimit = 0
	}
	if preset.PasswordMinLength < 0 {
		preset.PasswordMinLength = 0
	}
	if preset.DisableAfterDays < 0 {
		preset.DisableAfterDays = 0
	}
	if preset.DeleteAfterDays < 0 {
		preset.DeleteAfterDays = 0
	}

	if preset.InviteQuota < 0 {
		preset.InviteQuota = 0
	}
	if preset.InviteQuotaDay < 0 {
		preset.InviteQuotaDay = 0
	}
	if preset.InviteQuotaMonth < 0 {
		preset.InviteQuotaMonth = 0
	}
	if preset.InviteMaxUses < 0 {
		preset.InviteMaxUses = 0
	}
	if preset.InviteMaxLinkHours < 0 {
		preset.InviteMaxLinkHours = 0
	}
	if preset.InviteLinkValidityDays < 0 {
		preset.InviteLinkValidityDays = 0
	}

	// Legacy compatibility: invite_quota historically represented a monthly quota.
	if preset.InviteQuotaMonth <= 0 && preset.InviteQuota > 0 {
		preset.InviteQuotaMonth = preset.InviteQuota
	}
	if preset.InviteQuota == 0 && preset.InviteQuotaMonth > 0 {
		preset.InviteQuota = preset.InviteQuotaMonth
	}

	// Legacy compatibility: invite_max_link_hours historically represented link validity.
	if preset.InviteLinkValidityDays <= 0 && preset.InviteMaxLinkHours > 0 {
		preset.InviteLinkValidityDays = (preset.InviteMaxLinkHours + 23) / 24
	}
	if preset.InviteMaxLinkHours <= 0 && preset.InviteLinkValidityDays > 0 {
		preset.InviteMaxLinkHours = preset.InviteLinkValidityDays * 24
	}

	return preset
}

// GetJellyfinPolicyPresets récupère les presets de politique Jellyfin.
func (db *DB) GetJellyfinPolicyPresets() ([]config.JellyfinPolicyPreset, error) {
	defaults := config.DefaultJellyfinPolicyPresets()

	raw, err := db.GetSetting(SettingJellyfinPresets)
	if err != nil {
		return defaults, err
	}
	if raw == "" {
		return defaults, nil
	}

	var presets []config.JellyfinPolicyPreset
	if err := json.Unmarshal([]byte(raw), &presets); err != nil {
		slog.Warn("Erreur de parsing de la config JellyfinPresets", "error", err)
		return defaults, nil
	}

	if len(presets) == 0 {
		return defaults, nil
	}

	for i := range presets {
		if presets[i].ID == "" {
			presets[i].ID = fmt.Sprintf("preset-%d", i+1)
		}
		presets[i] = normalizeJellyfinPolicyPreset(presets[i])
	}

	return presets, nil
}

// SaveJellyfinPolicyPresets sauvegarde les presets de politique Jellyfin.
func (db *DB) SaveJellyfinPolicyPresets(presets []config.JellyfinPolicyPreset) error {
	if len(presets) == 0 {
		presets = config.DefaultJellyfinPolicyPresets()
	}

	for i := range presets {
		if presets[i].ID == "" {
			presets[i].ID = fmt.Sprintf("preset-%d", i+1)
		}
		presets[i] = normalizeJellyfinPolicyPreset(presets[i])
	}

	data, err := json.Marshal(presets)
	if err != nil {
		return fmt.Errorf("SaveJellyfinPolicyPresets marshal: %w", err)
	}
	return db.SetSetting(SettingJellyfinPresets, string(data))
}

// GetGroupPolicyMappings récupère les mappings groupe -> preset.
func (db *DB) GetGroupPolicyMappings() ([]config.GroupPolicyMapping, error) {
	raw, err := db.GetSetting(SettingGroupMappings)
	if err != nil {
		return []config.GroupPolicyMapping{}, err
	}
	if strings.TrimSpace(raw) == "" {
		return []config.GroupPolicyMapping{}, nil
	}

	var mappings []config.GroupPolicyMapping
	if err := json.Unmarshal([]byte(raw), &mappings); err != nil {
		slog.Warn("Erreur de parsing de la config GroupMappings", "error", err)
		return []config.GroupPolicyMapping{}, nil
	}

	normalized := make([]config.GroupPolicyMapping, 0, len(mappings))
	for i := range mappings {
		groupName := strings.TrimSpace(mappings[i].GroupName)
		presetID := strings.TrimSpace(strings.ToLower(mappings[i].PolicyPresetID))
		if groupName == "" || presetID == "" {
			continue
		}

		source := strings.TrimSpace(strings.ToLower(mappings[i].Source))
		if source != "ldap" {
			source = "internal"
		}

		normalized = append(normalized, config.GroupPolicyMapping{
			GroupName:      groupName,
			Source:         source,
			LDAPGroupDN:    strings.TrimSpace(mappings[i].LDAPGroupDN),
			PolicyPresetID: presetID,
		})
	}

	return normalized, nil
}

// SaveGroupPolicyMappings sauvegarde les mappings groupe -> preset.
func (db *DB) SaveGroupPolicyMappings(mappings []config.GroupPolicyMapping) error {
	normalized := make([]config.GroupPolicyMapping, 0, len(mappings))
	for i := range mappings {
		groupName := strings.TrimSpace(mappings[i].GroupName)
		presetID := strings.TrimSpace(strings.ToLower(mappings[i].PolicyPresetID))
		if groupName == "" || presetID == "" {
			continue
		}

		source := strings.TrimSpace(strings.ToLower(mappings[i].Source))
		if source != "ldap" {
			source = "internal"
		}

		normalized = append(normalized, config.GroupPolicyMapping{
			GroupName:      groupName,
			Source:         source,
			LDAPGroupDN:    strings.TrimSpace(mappings[i].LDAPGroupDN),
			PolicyPresetID: presetID,
		})
	}

	data, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("SaveGroupPolicyMappings marshal: %w", err)
	}

	return db.SetSetting(SettingGroupMappings, string(data))
}

// ── Invitation Profile Config ──────────────────────────────────────────────

func normalizeInvitationProfile(cfg config.InvitationProfileConfig) config.InvitationProfileConfig {
	cfg.PolicyPresetID = strings.TrimSpace(strings.ToLower(cfg.PolicyPresetID))
	cfg.TemplateUserID = strings.TrimSpace(cfg.TemplateUserID)
	cfg.EmailVerificationPolicy = strings.TrimSpace(strings.ToLower(cfg.EmailVerificationPolicy))
	switch cfg.EmailVerificationPolicy {
	case "required", "conditional", "admin_bypass", "disabled":
	default:
		if cfg.RequireEmailVerification {
			cfg.EmailVerificationPolicy = "required"
		} else {
			cfg.EmailVerificationPolicy = "disabled"
		}
	}

	switch cfg.EmailVerificationPolicy {
	case "required":
		cfg.RequireEmailVerification = true
		cfg.RequireEmail = true
	case "conditional":
		cfg.RequireEmail = true
	case "admin_bypass":
		cfg.RequireEmailVerification = true
		cfg.RequireEmail = true
	case "disabled":
		cfg.RequireEmailVerification = false
	}

	if cfg.RequireEmailVerification {
		cfg.RequireEmail = true
	}

	if cfg.DisableAfterDays < 0 {
		cfg.DisableAfterDays = 0
	}
	if cfg.DeleteAfterDays < 0 {
		cfg.DeleteAfterDays = 0
	}
	if cfg.InviterMaxUses < 0 {
		cfg.InviterMaxUses = 0
	}
	if cfg.InviterMaxLinkHours < 0 {
		cfg.InviterMaxLinkHours = 0
	}
	if cfg.InviterQuotaDay < 0 {
		cfg.InviterQuotaDay = 0
	}
	if cfg.InviterQuotaWeek < 0 {
		cfg.InviterQuotaWeek = 0
	}
	if cfg.InviterQuotaMonth < 0 {
		cfg.InviterQuotaMonth = 0
	}

	cfg.ExpiryAction = strings.TrimSpace(strings.ToLower(cfg.ExpiryAction))
	switch cfg.ExpiryAction {
	case "disable", "delete", "disable_then_delete":
	default:
		cfg.ExpiryAction = "disable"
	}

	if cfg.UsernameMinLength <= 0 {
		cfg.UsernameMinLength = 3
	}
	if cfg.UsernameMaxLength <= 0 {
		cfg.UsernameMaxLength = 32
	}
	if cfg.UsernameMaxLength < cfg.UsernameMinLength {
		cfg.UsernameMaxLength = cfg.UsernameMinLength
	}

	if cfg.PasswordMinLength <= 0 {
		cfg.PasswordMinLength = 8
	}
	if cfg.PasswordMaxLength <= 0 {
		cfg.PasswordMaxLength = 128
	}
	if cfg.PasswordMaxLength < cfg.PasswordMinLength {
		cfg.PasswordMaxLength = cfg.PasswordMinLength
	}

	return cfg
}

// GetInvitationProfileConfig recupere la politique globale appliquee aux nouvelles invitations.
func (db *DB) GetInvitationProfileConfig() (config.InvitationProfileConfig, error) {
	cfg := config.DefaultInvitationProfileConfig()

	raw, err := db.GetSetting(SettingInviteProfile)
	if err != nil {
		return cfg, err
	}
	if strings.TrimSpace(raw) == "" {
		return cfg, nil
	}

	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		slog.Warn("Erreur de parsing de la config InvitationProfile", "error", err)
		return config.DefaultInvitationProfileConfig(), nil
	}

	return normalizeInvitationProfile(cfg), nil
}

// SaveInvitationProfileConfig sauvegarde la politique globale des invitations.
func (db *DB) SaveInvitationProfileConfig(cfg config.InvitationProfileConfig) error {
	cfg = normalizeInvitationProfile(cfg)
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("SaveInvitationProfileConfig marshal: %w", err)
	}
	return db.SetSetting(SettingInviteProfile, string(data))
}

// DeleteClosedInvitations supprime les invitations expirées ou qui ont atteint leur quota.
func (db *DB) DeleteClosedInvitations(now time.Time) (int64, error) {
	res, err := db.Exec(
		`DELETE FROM invitations
		 WHERE (expires_at IS NOT NULL AND expires_at <= ?)
		    OR (max_uses > 0 AND used_count >= max_uses)`,
		now,
	)
	if err != nil {
		return 0, fmt.Errorf("DeleteClosedInvitations: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ── Webhooks Config ─────────────────────────────────────────────────────────

// GetWebhooksConfig récupère la configuration Webhooks depuis la base.
func (db *DB) GetWebhooksConfig() (config.WebhooksConfig, error) {
	var cfg config.WebhooksConfig

	raw, err := db.GetSetting(SettingWebhooksConfig)
	if err != nil {
		return cfg, err
	}
	if raw == "" {
		return cfg, nil
	}

	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		slog.Warn("Erreur de parsing de la config Webhooks", "error", err)
		return cfg, nil
	}

	return cfg, nil
}

// SaveWebhooksConfig sauvegarde la configuration Webhooks dans la base.
func (db *DB) SaveWebhooksConfig(cfg config.WebhooksConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("SaveWebhooksConfig marshal: %w", err)
	}
	enc, err := db.encrypt(string(data))
	if err != nil {
		return fmt.Errorf("SaveWebhooksConfig encrypt: %w", err)
	}
	return db.SetSetting(SettingWebhooksConfig, enc)
}
