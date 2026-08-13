package config

import (
	"testing"
)

func TestDatabaseTypeAutoDetection(t *testing.T) {
	t.Run("defaults to sqlite when DB_TYPE and DB_HOST are empty", func(t *testing.T) {
		t.Setenv("JELLYGATE_SECRET", "12345678901234567890123456789012")
		t.Setenv("JELLYFIN_URL", "http://localhost:8096")
		t.Setenv("JELLYFIN_API_KEY", "dummykey")
		t.Setenv("DB_TYPE", "")
		t.Setenv("DB_HOST", "")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Database.Type != "sqlite" {
			t.Errorf("expected sqlite, got %s", cfg.Database.Type)
		}
	})

	t.Run("auto detects postgres when DB_HOST is present and DB_TYPE is empty", func(t *testing.T) {
		t.Setenv("JELLYGATE_SECRET", "12345678901234567890123456789012")
		t.Setenv("JELLYFIN_URL", "http://localhost:8096")
		t.Setenv("JELLYFIN_API_KEY", "dummykey")
		t.Setenv("DB_TYPE", "")
		t.Setenv("DB_HOST", "postgres18")
		t.Setenv("DB_USER", "jellygate")
		t.Setenv("DB_NAME", "jellygate")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Database.Type != "postgres" {
			t.Errorf("expected postgres, got %s", cfg.Database.Type)
		}
	})
}

func TestOIDCAndAuthentikConfigLoading(t *testing.T) {
	t.Run("loads clean simplified OIDC and Authentik variables", func(t *testing.T) {
		t.Setenv("JELLYGATE_SECRET", "12345678901234567890123456789012")
		t.Setenv("JELLYGATE_BASE_URL", "https://jellygate.example.com")
		t.Setenv("OIDC_ENABLED", "true")
		t.Setenv("OIDC_URL", "https://auth.example.com/application/o/jellygate/")
		t.Setenv("OIDC_CLIENT_ID", "jellygate-client")
		t.Setenv("OIDC_CLIENT_SECRET", "super-secret-oidc")
		t.Setenv("AUTHENTIK_ENABLED", "true")
		t.Setenv("AUTHENTIK_URL", "https://auth.example.com")
		t.Setenv("AUTHENTIK_API_TOKEN", "ak-token-12345")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !cfg.Authentik.Enabled {
			t.Errorf("expected Authentik.Enabled to be true")
		}
		if cfg.Authentik.URL != "https://auth.example.com" {
			t.Errorf("expected Authentik.URL https://auth.example.com, got %s", cfg.Authentik.URL)
		}
		if cfg.Authentik.IssuerURL != "https://auth.example.com/application/o/jellygate" {
			t.Errorf("expected IssuerURL https://auth.example.com/application/o/jellygate, got %s", cfg.Authentik.IssuerURL)
		}
		if cfg.Authentik.ClientID != "jellygate-client" {
			t.Errorf("expected ClientID jellygate-client, got %s", cfg.Authentik.ClientID)
		}
		if cfg.Authentik.ClientSecret != "super-secret-oidc" {
			t.Errorf("expected ClientSecret super-secret-oidc, got %s", cfg.Authentik.ClientSecret)
		}
		if cfg.Authentik.APIToken != "ak-token-12345" {
			t.Errorf("expected APIToken ak-token-12345, got %s", cfg.Authentik.APIToken)
		}
		if cfg.Authentik.RedirectURL != "https://jellygate.example.com/auth/callback" {
			t.Errorf("expected RedirectURL https://jellygate.example.com/auth/callback, got %s", cfg.Authentik.RedirectURL)
		}
	})

	t.Run("auto-derives Authentik.URL from OIDC_URL", func(t *testing.T) {
		t.Setenv("JELLYGATE_SECRET", "12345678901234567890123456789012")
		t.Setenv("OIDC_ENABLED", "true")
		t.Setenv("OIDC_URL", "https://auth.myhost.org/application/o/jellygate/")
		t.Setenv("OIDC_CLIENT_ID", "jg-client")
		t.Setenv("AUTHENTIK_URL", "")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cfg.Authentik.URL != "https://auth.myhost.org" {
			t.Errorf("expected derived Authentik.URL https://auth.myhost.org, got %s", cfg.Authentik.URL)
		}
	})

	t.Run("auto-derives OIDC IssuerURL from AUTHENTIK_URL", func(t *testing.T) {
		t.Setenv("JELLYGATE_SECRET", "12345678901234567890123456789012")
		t.Setenv("AUTHENTIK_ENABLED", "true")
		t.Setenv("AUTHENTIK_URL", "https://authentik.mydomain.local")
		t.Setenv("OIDC_URL", "")
		t.Setenv("OIDC_ISSUER_URL", "")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedIssuer := "https://authentik.mydomain.local/application/o/jellygate/"
		if cfg.Authentik.IssuerURL != expectedIssuer {
			t.Errorf("expected derived IssuerURL %s, got %s", expectedIssuer, cfg.Authentik.IssuerURL)
		}
	})
}
