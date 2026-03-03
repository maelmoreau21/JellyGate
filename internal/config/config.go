// Package config gère le chargement et la validation de la configuration
// de JellyGate à partir des variables d'environnement.
//
// Toutes les variables sont préfixées par leur catégorie :
//   - JELLYGATE_*  : Application
//   - JELLYFIN_*   : Connexion à Jellyfin
//   - LDAP_*       : Connexion au Synology Active Directory
//   - SMTP_*       : Envoi d'emails
//   - WEBHOOK_*    : Notifications sortantes
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config contient toute la configuration de l'application.
type Config struct {
	// Application
	Port        int    // Port d'écoute HTTP (défaut: 8097)
	BaseURL     string // URL de base publique
	DataDir     string // Répertoire des données (SQLite, etc.)
	SecretKey   string // Clé secrète pour sessions/tokens (min 32 chars)
	DefaultLang string // Langue par défaut : "fr" ou "en"

	// Services externes
	Jellyfin JellyfinConfig
	LDAP     LDAPConfig
	SMTP     SMTPConfig
	Webhooks WebhooksConfig
}

// JellyfinConfig contient les paramètres de connexion à Jellyfin.
type JellyfinConfig struct {
	URL    string // URL de l'instance Jellyfin (ex: http://jellyfin:8096)
	APIKey string // Clé API d'administration
}

// LDAPConfig contient les paramètres de connexion au Synology AD (LDAP/LDAPS).
type LDAPConfig struct {
	Host         string // Hostname du serveur LDAP
	Port         int    // Port (défaut: 636 pour LDAPS)
	UseTLS       bool   // Utiliser LDAPS (TLS)
	SkipVerify   bool   // Ignorer la vérification du certificat TLS
	BindDN       string // DN de l'utilisateur pour le bind
	BindPassword string // Mot de passe de bind
	BaseDN       string // Base DN de recherche
	UserOU       string // OU pour la création des utilisateurs (défaut: CN=Users)
	UserGroup    string // Groupe AD auquel ajouter les utilisateurs (optionnel)
	Domain       string // Domaine AD (ex: home.lan) — pour userPrincipalName
}

// SMTPConfig contient les paramètres d'envoi d'emails.
type SMTPConfig struct {
	Host     string // Serveur SMTP
	Port     int    // Port SMTP (défaut: 587)
	Username string // Utilisateur SMTP
	Password string // Mot de passe SMTP
	From     string // Adresse expéditeur
	UseTLS   bool   // Utiliser STARTTLS
}

// WebhooksConfig contient les paramètres des webhooks sortants (optionnels).
type WebhooksConfig struct {
	Discord  DiscordWebhook
	Telegram TelegramWebhook
	Matrix   MatrixWebhook
}

// DiscordWebhook contient la configuration du webhook Discord.
type DiscordWebhook struct {
	URL string // URL du webhook Discord
}

// TelegramWebhook contient la configuration du bot Telegram.
type TelegramWebhook struct {
	Token  string // Token du bot Telegram
	ChatID string // ID du chat Telegram
}

// MatrixWebhook contient la configuration de la connexion Matrix.
type MatrixWebhook struct {
	URL    string // URL du serveur Matrix
	RoomID string // ID de la room Matrix
	Token  string // Token d'accès Matrix
}

// Load charge la configuration depuis les variables d'environnement,
// applique les valeurs par défaut, et valide les champs requis.
//
// Retourne une erreur si un champ requis est manquant ou invalide.
func Load() (*Config, error) {
	cfg := &Config{
		// ── Valeurs par défaut ───────────────────────────────────────────
		Port:        getEnvInt("JELLYGATE_PORT", 8097),
		BaseURL:     getEnv("JELLYGATE_BASE_URL", "http://localhost:8097"),
		DataDir:     getEnv("JELLYGATE_DATA_DIR", "/data"),
		SecretKey:   getEnv("JELLYGATE_SECRET_KEY", ""),
		DefaultLang: getEnv("JELLYGATE_DEFAULT_LANG", "fr"),

		Jellyfin: JellyfinConfig{
			URL:    getEnv("JELLYFIN_URL", ""),
			APIKey: getEnv("JELLYFIN_API_KEY", ""),
		},

		LDAP: LDAPConfig{
			Host:         getEnv("LDAP_HOST", ""),
			Port:         getEnvInt("LDAP_PORT", 636),
			UseTLS:       getEnvBool("LDAP_USE_TLS", true),
			SkipVerify:   getEnvBool("LDAP_SKIP_VERIFY", false),
			BindDN:       getEnv("LDAP_BIND_DN", ""),
			BindPassword: getEnv("LDAP_BIND_PASSWORD", ""),
			BaseDN:       getEnv("LDAP_BASE_DN", ""),
			UserOU:       getEnv("LDAP_USER_OU", "CN=Users"),
			UserGroup:    getEnv("LDAP_USER_GROUP", ""),
			Domain:       getEnv("LDAP_DOMAIN", ""),
		},

		SMTP: SMTPConfig{
			Host:     getEnv("SMTP_HOST", ""),
			Port:     getEnvInt("SMTP_PORT", 587),
			Username: getEnv("SMTP_USERNAME", ""),
			Password: getEnv("SMTP_PASSWORD", ""),
			From:     getEnv("SMTP_FROM", ""),
			UseTLS:   getEnvBool("SMTP_TLS", true),
		},

		Webhooks: WebhooksConfig{
			Discord: DiscordWebhook{
				URL: getEnv("WEBHOOK_DISCORD_URL", ""),
			},
			Telegram: TelegramWebhook{
				Token:  getEnv("WEBHOOK_TELEGRAM_TOKEN", ""),
				ChatID: getEnv("WEBHOOK_TELEGRAM_CHAT_ID", ""),
			},
			Matrix: MatrixWebhook{
				URL:    getEnv("WEBHOOK_MATRIX_URL", ""),
				RoomID: getEnv("WEBHOOK_MATRIX_ROOM_ID", ""),
				Token:  getEnv("WEBHOOK_MATRIX_TOKEN", ""),
			},
		},
	}

	// ── Validation des champs requis ────────────────────────────────────
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

	// LDAP
	if c.LDAP.Host == "" {
		errs = append(errs, "LDAP_HOST est requis")
	}
	if c.LDAP.BindDN == "" {
		errs = append(errs, "LDAP_BIND_DN est requis")
	}
	if c.LDAP.BindPassword == "" {
		errs = append(errs, "LDAP_BIND_PASSWORD est requis")
	}
	if c.LDAP.BaseDN == "" {
		errs = append(errs, "LDAP_BASE_DN est requis")
	}
	if c.LDAP.Domain == "" {
		errs = append(errs, "LDAP_DOMAIN est requis")
	}

	// SMTP
	if c.SMTP.Host == "" {
		errs = append(errs, "SMTP_HOST est requis")
	}
	if c.SMTP.Username == "" {
		errs = append(errs, "SMTP_USERNAME est requis")
	}
	if c.SMTP.Password == "" {
		errs = append(errs, "SMTP_PASSWORD est requis")
	}
	if c.SMTP.From == "" {
		errs = append(errs, "SMTP_FROM est requis")
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
