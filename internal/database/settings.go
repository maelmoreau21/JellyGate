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
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

// ── Clés de settings ────────────────────────────────────────────────────────

const (
	SettingLDAPConfig     = "ldap_config"     // JSON: config.LDAPConfig
	SettingSMTPConfig     = "smtp_config"     // JSON: config.SMTPConfig
	SettingWebhooksConfig = "webhooks_config" // JSON: config.WebhooksConfig
	SettingEmailTemplates = "email_templates" // JSON: config.EmailTemplatesConfig
	SettingBackupConfig   = "backup_config"   // JSON: config.BackupConfig
	SettingBackupLastRun  = "backup_last_run" // Date locale YYYY-MM-DD
	SettingDefaultLang    = "default_lang"    // Langue par défaut du serveur ("fr" ou "en")
)

// GetDefaultLang retourne la langue par défaut du serveur.
// Retourne "fr" si la clé n'existe pas ou en cas d'erreur.
func (db *DB) GetDefaultLang() string {
	val, err := db.GetSetting(SettingDefaultLang)
	if err != nil || val == "" {
		return "fr"
	}
	return val
}

// ── Get / Set générique ─────────────────────────────────────────────────────

// GetSetting récupère la valeur brute d'un paramètre par sa clé.
// Retourne "" si la clé n'existe pas.
func (db *DB) GetSetting(key string) (string, error) {
	var value sql.NullString
	err := db.conn.QueryRow(
		`SELECT value FROM settings WHERE key = ?`, key,
	).Scan(&value)

	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("GetSetting(%q): %w", key, err)
	}

	return value.String, nil
}

// SetSetting insère ou met à jour un paramètre (UPSERT).
func (db *DB) SetSetting(key, value string) error {
	_, err := db.conn.Exec(`
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
	rows, err := db.conn.Query(`SELECT key, value FROM settings`)
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

// ── LDAP Config ─────────────────────────────────────────────────────────────

// GetLDAPConfig récupère la configuration LDAP depuis la base.
// Retourne une config par défaut (Enabled=false) si non configurée.
func (db *DB) GetLDAPConfig() (config.LDAPConfig, error) {
	cfg := config.LDAPConfig{
		Enabled: false,
		Port:    636,
		UseTLS:  true,
		UserOU:  "CN=Users",
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

	return cfg, nil
}

// SaveLDAPConfig sauvegarde la configuration LDAP dans la base.
func (db *DB) SaveLDAPConfig(cfg config.LDAPConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("SaveLDAPConfig marshal: %w", err)
	}
	return db.SetSetting(SettingLDAPConfig, string(data))
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
	return db.SetSetting(SettingSMTPConfig, string(data))
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
	if cfg.RetentionCount < 1 {
		cfg.RetentionCount = 7
	}

	return cfg, nil
}

// SaveBackupConfig sauvegarde la configuration des sauvegardes planifiées.
func (db *DB) SaveBackupConfig(cfg config.BackupConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("SaveBackupConfig marshal: %w", err)
	}
	return db.SetSetting(SettingBackupConfig, string(data))
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
func (db *DB) GetEmailTemplatesConfig() (config.EmailTemplatesConfig, error) {
	cfg := config.DefaultEmailTemplates()

	raw, err := db.GetSetting(SettingEmailTemplates)
	if err != nil {
		return cfg, err
	}
	if raw == "" {
		return cfg, nil
	}

	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		slog.Warn("Erreur de parsing de la config EmailTemplates", "error", err)
		return cfg, nil // Fallback silenceus sur defaults
	}

	if cfg.ExpiryReminderDays < 1 || cfg.ExpiryReminderDays > 365 {
		cfg.ExpiryReminderDays = 3
	}

	return cfg, nil
}

// SaveEmailTemplatesConfig sauvegarde la configuration des gabarits.
func (db *DB) SaveEmailTemplatesConfig(cfg config.EmailTemplatesConfig) error {
	if cfg.ExpiryReminderDays < 1 || cfg.ExpiryReminderDays > 365 {
		cfg.ExpiryReminderDays = 3
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("SaveEmailTemplatesConfig marshal: %w", err)
	}
	return db.SetSetting(SettingEmailTemplates, string(data))
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
	return db.SetSetting(SettingWebhooksConfig, string(data))
}
