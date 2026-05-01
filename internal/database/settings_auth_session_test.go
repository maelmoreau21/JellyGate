package database

import (
	"testing"
	"time"
)

func TestAuthSessionConfigDefaultsAndPersistence(t *testing.T) {
	db := newPresetTestDB(t)

	cfg, err := db.GetAuthSessionConfig()
	if err != nil {
		t.Fatalf("GetAuthSessionConfig() error = %v", err)
	}
	if !cfg.Remember30Days {
		t.Fatalf("Remember30Days default = false, want true")
	}
	if cfg.RevokedBefore != 0 {
		t.Fatalf("RevokedBefore default = %d, want 0", cfg.RevokedBefore)
	}

	cfg.Remember30Days = false
	cfg.RevokedBefore = 123
	if err := db.SaveAuthSessionConfig(cfg); err != nil {
		t.Fatalf("SaveAuthSessionConfig() error = %v", err)
	}

	got, err := db.GetAuthSessionConfig()
	if err != nil {
		t.Fatalf("GetAuthSessionConfig() after save error = %v", err)
	}
	if got.Remember30Days || got.RevokedBefore != 123 {
		t.Fatalf("GetAuthSessionConfig() = %+v, want remember=false revoked_before=123", got)
	}
}

func TestAuthSessionRevocationUsesIssuedAt(t *testing.T) {
	cfg := AuthSessionConfig{RevokedBefore: 100}
	if cfg.AcceptsIssuedAt(100) {
		t.Fatalf("session issued at revocation timestamp should be rejected")
	}
	if cfg.AcceptsIssuedAt(99) {
		t.Fatalf("session issued before revocation timestamp should be rejected")
	}
	if !cfg.AcceptsIssuedAt(101) {
		t.Fatalf("session issued after revocation timestamp should be accepted")
	}

	db := newPresetTestDB(t)
	now := time.Now().Unix()
	saved, err := db.RevokeAuthSessionsBefore(now)
	if err != nil {
		t.Fatalf("RevokeAuthSessionsBefore() error = %v", err)
	}
	if saved.RevokedBefore != now {
		t.Fatalf("RevokedBefore = %d, want %d", saved.RevokedBefore, now)
	}
}
