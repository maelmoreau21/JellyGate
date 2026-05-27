package config

import (
	"bytes"
	"encoding/json"
	htmltemplate "html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
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
	if !strings.Contains(frBlock, "Créer mon compte") {
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

func TestEmailTemplateDefaultFilesAreCompleteAndUTF8(t *testing.T) {
	root := EmailTemplateDefaultsPath()
	for _, lang := range SupportedLanguageTags() {
		metaPath := filepath.Join(root, lang, "_meta.json")
		metaRaw, err := os.ReadFile(metaPath)
		if err != nil {
			t.Fatalf("%s _meta.json missing: %v", lang, err)
		}
		if !utf8.Valid(metaRaw) {
			t.Fatalf("%s _meta.json is not valid UTF-8", lang)
		}
		var meta map[string]string
		if err := json.Unmarshal(metaRaw, &meta); err != nil {
			t.Fatalf("%s _meta.json invalid JSON: %v", lang, err)
		}
		if strings.TrimSpace(meta["useful_links_title"]) == "" {
			t.Fatalf("%s _meta.json missing useful_links_title", lang)
		}

		for _, key := range EmailTemplateFileKeys() {
			for _, filename := range []string{"subject.txt", "body.txt"} {
				filePath := filepath.Join(root, lang, key.Dir, filename)
				raw, err := os.ReadFile(filePath)
				if err != nil {
					t.Fatalf("%s %s/%s missing: %v", lang, key.Dir, filename, err)
				}
				text := string(raw)
				if !utf8.Valid(raw) {
					t.Fatalf("%s %s/%s is not valid UTF-8", lang, key.Dir, filename)
				}
				if strings.TrimSpace(text) == "" {
					t.Fatalf("%s %s/%s is empty", lang, key.Dir, filename)
				}
				if strings.Contains(text, "Ã") || strings.Contains(text, "Â") || strings.Contains(text, "\uFFFD") {
					t.Fatalf("%s %s/%s contains mojibake: %q", lang, key.Dir, filename, text)
				}
			}
		}
	}
}

func TestAutomaticPortalLinksRenderOnlyWhenConfigured(t *testing.T) {
	cfg := DefaultEmailTemplatesForLanguage("fr")
	data := map[string]string{
		"Username":           "maelle",
		"JellyfinServerName": "Jellyfin",
		"HelpURL":            "https://help.example.com",
		"JellyfinURL":        "https://jellyfin.example.com",
		"JellyGateURL":       "https://gate.example.com",
		"JellyseerrURL":      "",
		"JellyTrackURL":      "",
	}

	withoutLinks := renderEmailTemplateForTest(t, cfg.Confirmation, data)
	if strings.Contains(withoutLinks, "Liens utiles") || strings.Contains(withoutLinks, "JellyseerrURL") || strings.Contains(withoutLinks, "JellyTrackURL") {
		t.Fatalf("portal links block should be hidden when URLs are empty: %s", withoutLinks)
	}

	data["JellyseerrURL"] = "https://seerr.example.com"
	data["JellyTrackURL"] = "https://track.example.com"
	withLinks := renderEmailTemplateForTest(t, cfg.Confirmation, data)
	if !strings.Contains(withLinks, "Liens utiles") || !strings.Contains(withLinks, "https://seerr.example.com") || !strings.Contains(withLinks, "https://track.example.com") {
		t.Fatalf("portal links block should render configured URLs: %s", withLinks)
	}
}

func renderEmailTemplateForTest(t *testing.T, tpl string, data map[string]string) string {
	t.Helper()
	parsed, err := htmltemplate.New("email").Parse(tpl)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	var out bytes.Buffer
	if err := parsed.Execute(&out, data); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	return out.String()
}
