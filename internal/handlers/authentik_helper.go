package handlers

import (
	"net/url"
	"strings"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
)

// ResolveEffectiveAuthentikConfig combine la configuration stockée en base SQL avec les variables d'environnement.
// Les variables d'environnement (Docker Compose / .env) sont STRICTEMENT prioritaires sur la base SQL.
func ResolveEffectiveAuthentikConfig(appCfg *config.Config, db *database.DB) config.AuthentikConfig {
	cfg := config.AuthentikConfig{
		Enabled:            false,
		UserGroup:          "jellygate-users",
		AdminGroup:         "jellygate-admins",
		JellyfinUserGroup:  "jellyfin-users",
		EnrollmentFlowSlug: "default-enrollment-flow",
	}

	if db != nil {
		if dbCfg, err := db.GetAuthentikConfig(); err == nil {
			if dbCfg.URL != "" || dbCfg.IssuerURL != "" || dbCfg.ClientID != "" || dbCfg.APIToken != "" || dbCfg.Enabled {
				cfg = dbCfg
			}
		}
	}

	if appCfg != nil {
		env := appCfg.Authentik

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
		if strings.TrimSpace(env.InvitersGroup) != "" {
			cfg.InvitersGroup = strings.TrimSpace(env.InvitersGroup)
		}
		if strings.TrimSpace(env.InvitersRecursiveGroup) != "" {
			cfg.InvitersRecursiveGroup = strings.TrimSpace(env.InvitersRecursiveGroup)
		}
		if strings.TrimSpace(env.EnrollmentFlowSlug) != "" {
			cfg.EnrollmentFlowSlug = strings.TrimSpace(env.EnrollmentFlowSlug)
		}
		if env.URL != "" || env.IssuerURL != "" || env.ClientID != "" || env.APIToken != "" {
			cfg.Enabled = env.Enabled
		}
	}

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

	if cfg.URL != "" {
		if u, err := url.Parse(cfg.URL); err == nil && u.Scheme != "" && u.Host != "" && u.Path != "" && u.Path != "/" {
			if cfg.IssuerURL == "" || cfg.IssuerURL == cfg.URL {
				cfg.IssuerURL = cfg.URL
			}
			cfg.URL = u.Scheme + "://" + u.Host
		}
	}
	if cfg.URL == "" && cfg.IssuerURL != "" {
		if u, err := url.Parse(cfg.IssuerURL); err == nil && u.Scheme != "" && u.Host != "" {
			cfg.URL = u.Scheme + "://" + u.Host
		}
	}
	if cfg.IssuerURL == "" && cfg.URL != "" {
		cfg.IssuerURL = cfg.URL + "/application/o/jellygate/"
	}

	if cfg.RedirectURL == "" && appCfg != nil && appCfg.BaseURL != "" {
		cfg.RedirectURL = strings.TrimRight(appCfg.BaseURL, "/") + "/auth/callback"
	}

	return cfg
}
