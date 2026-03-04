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
)

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
