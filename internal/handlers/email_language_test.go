package handlers

import "testing"

func TestResolveEmailLanguagePrefersUserLanguage(t *testing.T) {
	got := resolveEmailLanguage("fr", "de", "en", "team-a")
	if got != "en" {
		t.Fatalf("resolveEmailLanguage() = %q, want %q", got, "en")
	}
}

func TestResolveEmailLanguageFallsBackToInvitationLanguage(t *testing.T) {
	got := resolveEmailLanguage("fr", "de", "", "")
	if got != "de" {
		t.Fatalf("resolveEmailLanguage() = %q, want %q", got, "de")
	}
}

func TestResolveEmailLanguageFallsBackToDefault(t *testing.T) {
	got := resolveEmailLanguage("en", "", "", "")
	if got != "en" {
		t.Fatalf("resolveEmailLanguage() = %q, want %q", got, "en")
	}
}

func TestResolveEmailLanguageDoesNotDisableMultilangForGroup(t *testing.T) {
	got := resolveEmailLanguage("fr", "de", "en", "ldap-group")
	if got != "en" {
		t.Fatalf("resolveEmailLanguage() = %q, want %q", got, "en")
	}
}
