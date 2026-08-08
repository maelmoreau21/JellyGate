package config

import (
	"testing"
)

func TestDatabaseTypeAutoDetection(t *testing.T) {
	t.Run("defaults to sqlite when DB_TYPE and DB_HOST are empty", func(t *testing.T) {
		t.Setenv("JELLYGATE_SECRET_KEY", "12345678901234567890123456789012")
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
		t.Setenv("JELLYGATE_SECRET_KEY", "12345678901234567890123456789012")
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
