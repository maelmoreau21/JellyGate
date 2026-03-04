// Package config gère le chargement et la validation de la configuration
// de JellyGate à partir des variables d'environnement.
//
// Seules les variables essentielles au démarrage sont gérées ici :
//   - JELLYGATE_*  : Application (port, URL, data, secret, langue)
//   - JELLYFIN_*   : Connexion à Jellyfin
//
// Les paramètres LDAP, SMTP et Webhooks sont désormais stockés dans SQLite
// (table `settings`) et gérés via l'interface d'administration.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config contient la configuration chargée depuis les variables d'environnement.
// Ne contient que les paramètres essentiels au démarrage de l'application.
type Config struct {
	// Application
	Port        int    // Port d'écoute HTTP (défaut: 8097)
	BaseURL     string // URL de base publique
	DataDir     string // Répertoire des données (SQLite, etc.)
	SecretKey   string // Clé secrète pour sessions/tokens (min 32 chars)
	DefaultLang string // Langue par défaut : "fr" ou "en"

	// Jellyfin (seul service externe requis au démarrage)
	Jellyfin JellyfinConfig
}

// JellyfinConfig contient les paramètres de connexion à Jellyfin.
type JellyfinConfig struct {
	URL    string // URL de l'instance Jellyfin (ex: http://jellyfin:8096)
	APIKey string // Clé API d'administration
}

// ── Types de configuration stockés en base (table settings) ─────────────────
// Ces structs sont utilisées par database/settings.go et handlers/settings.go
// pour sérialiser/désérialiser les paramètres depuis SQLite.

// LDAPConfig contient les paramètres de connexion au Synology AD (LDAP/LDAPS).
type LDAPConfig struct {
	Enabled      bool   `json:"enabled"`       // Intégration LDAP activée
	Host         string `json:"host"`          // Hostname du serveur LDAP
	Port         int    `json:"port"`          // Port (défaut: 636 pour LDAPS)
	UseTLS       bool   `json:"use_tls"`       // Utiliser LDAPS (TLS)
	SkipVerify   bool   `json:"skip_verify"`   // Ignorer la vérification du certificat TLS
	BindDN       string `json:"bind_dn"`       // DN de l'utilisateur pour le bind
	BindPassword string `json:"bind_password"` // Mot de passe de bind
	BaseDN       string `json:"base_dn"`       // Base DN de recherche
	UserOU       string `json:"user_ou"`       // OU pour la création des utilisateurs
	UserGroup    string `json:"user_group"`    // Groupe AD (optionnel)
	Domain       string `json:"domain"`        // Domaine AD (ex: home.lan)
}

// SMTPConfig contient les paramètres d'envoi d'emails.
type SMTPConfig struct {
	Host     string `json:"host"`     // Serveur SMTP
	Port     int    `json:"port"`     // Port SMTP (défaut: 587)
	Username string `json:"username"` // Utilisateur SMTP
	Password string `json:"password"` // Mot de passe SMTP
	From     string `json:"from"`     // Adresse expéditeur
	UseTLS   bool   `json:"use_tls"`  // Utiliser STARTTLS
}

// WebhooksConfig contient les paramètres des webhooks sortants (optionnels).
type WebhooksConfig struct {
	Discord  DiscordWebhook  `json:"discord"`
	Telegram TelegramWebhook `json:"telegram"`
	Matrix   MatrixWebhook   `json:"matrix"`
}

// DiscordWebhook contient la configuration du webhook Discord.
type DiscordWebhook struct {
	URL string `json:"url"`
}

// TelegramWebhook contient la configuration du bot Telegram.
type TelegramWebhook struct {
	Token  string `json:"token"`
	ChatID string `json:"chat_id"`
}

// MatrixWebhook contient la configuration de la connexion Matrix.
type MatrixWebhook struct {
	URL    string `json:"url"`
	RoomID string `json:"room_id"`
	Token  string `json:"token"`
}

// ── Chargement depuis l'environnement ───────────────────────────────────────

// Load charge la configuration depuis les variables d'environnement,
// applique les valeurs par défaut, et valide les champs requis.
//
// Seuls les paramètres App + Jellyfin sont chargés ici.
// LDAP, SMTP et Webhooks sont chargés depuis la base de données.
func Load() (*Config, error) {
	cfg := &Config{
		Port:        getEnvInt("JELLYGATE_PORT", 8097),
		BaseURL:     getEnv("JELLYGATE_BASE_URL", "http://localhost:8097"),
		DataDir:     getEnv("JELLYGATE_DATA_DIR", "/data"),
		SecretKey:   getEnv("JELLYGATE_SECRET_KEY", ""),
		DefaultLang: getEnv("JELLYGATE_DEFAULT_LANG", "fr"),

		Jellyfin: JellyfinConfig{
			URL:    getEnv("JELLYFIN_URL", ""),
			APIKey: getEnv("JELLYFIN_API_KEY", ""),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("configuration invalide: %w", err)
	}

	return cfg, nil
}

// validate vérifie que tous les champs requis sont renseignés.
func (c *Config) validate() error {
	var errs []string

	// Application
	if c.SecretKey == "" {
		errs = append(errs, "JELLYGATE_SECRET_KEY est requis")
	} else if len(c.SecretKey) < 32 {
		errs = append(errs, "JELLYGATE_SECRET_KEY doit faire au minimum 32 caractères")
	}

	if c.DefaultLang != "fr" && c.DefaultLang != "en" {
		errs = append(errs, "JELLYGATE_DEFAULT_LANG doit être 'fr' ou 'en'")
	}

	// Jellyfin
	if c.Jellyfin.URL == "" {
		errs = append(errs, "JELLYFIN_URL est requis")
	}
	if c.Jellyfin.APIKey == "" {
		errs = append(errs, "JELLYFIN_API_KEY est requis")
	}

	if len(errs) > 0 {
		return fmt.Errorf("%d erreur(s):\n  - %s", len(errs), strings.Join(errs, "\n  - "))
	}

	return nil
}

// ── Fonctions utilitaires pour lire les variables d'environnement ───────────

// getEnv renvoie la valeur de la variable d'environnement key,
// ou defaultVal si la variable est vide ou absente.
func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// getEnvInt renvoie la valeur entière de la variable d'environnement key,
// ou defaultVal si la variable est vide, absente ou invalide.
func getEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return n
}

// getEnvBool renvoie la valeur booléenne de la variable d'environnement key,
// ou defaultVal si la variable est vide, absente ou invalide.
// Valeurs acceptées : "true", "1", "yes" → true ; "false", "0", "no" → false.
func getEnvBool(key string, defaultVal bool) bool {
	val := strings.ToLower(os.Getenv(key))
	switch val {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return defaultVal
	}
}
