package config

import (
	"strings"
	"testing"
)

func TestDefaultEmailTemplatesForLanguageUsesServerNameVariable(t *testing.T) {
	cfg := DefaultEmailTemplatesForLanguage("en")

	if !strings.Contains(cfg.ConfirmationSubject, "{{.JellyfinServerName}}") {
		t.Fatalf("confirmation subject should use JellyfinServerName, got %q", cfg.ConfirmationSubject)
	}
	if !strings.Contains(cfg.Welcome, "{{.JellyfinServerName}}") {
		t.Fatalf("welcome body should use JellyfinServerName")
	}
}

func TestUpgradeLegacyEmailTemplatesReplacesHardcodedJellyfinCreationName(t *testing.T) {
	cfg := EmailTemplatesConfig{
		UserCreationSubject: "Compte Jellyfin cree",
		UserCreation:        "Bonjour {{.Username}},\n\nUn administrateur vient de creer ton compte Jellyfin.\n\nTu peux utiliser les informations recues pour te connecter.",
	}

	UpgradeLegacyEmailTemplates(&cfg)

	if strings.Contains(cfg.UserCreationSubject, "Compte Jellyfin") || strings.Contains(cfg.UserCreation, "compte Jellyfin") {
		t.Fatalf("user creation template should not keep a hardcoded Jellyfin name: subject=%q body=%q", cfg.UserCreationSubject, cfg.UserCreation)
	}
	if !strings.Contains(cfg.UserCreationSubject, "{{.serveurname}}") || !strings.Contains(cfg.UserCreation, "{{.serveurname}}") {
		t.Fatalf("user creation template should use serveurname: subject=%q body=%q", cfg.UserCreationSubject, cfg.UserCreation)
	}
}

func TestAutomaticEmailBlockForLanguageIsLocalized(t *testing.T) {
	enBlock := automaticEmailBlockForLanguage("en", "invitation")
	frBlock := automaticEmailBlockForLanguage("fr", "invitation")

	if !strings.Contains(enBlock, "Create my account") {
		t.Fatalf("english invitation block should be localized, got %q", enBlock)
	}
	if !strings.Contains(frBlock, "Creer mon compte") {
		t.Fatalf("french invitation block should be localized, got %q", frBlock)
	}
}

func TestDefaultEmailPreviewTextIsLocalized(t *testing.T) {
	if got := DefaultEmailPreviewDurationForLanguage("fr"); got != "15 minutes" {
		t.Fatalf("DefaultEmailPreviewDurationForLanguage(fr) = %q", got)
	}
	if got := DefaultEmailPreviewDurationForLanguage("en"); got != "15 minutes" {
		t.Fatalf("DefaultEmailPreviewDurationForLanguage(en) = %q", got)
	}
	if got := DefaultEmailPreviewMessageForLanguage("en"); !strings.Contains(got, "{{.JellyfinServerName}}") {
		t.Fatalf("preview message should mention JellyfinServerName, got %q", got)
	}
}
