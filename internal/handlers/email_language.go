package handlers

import (
	"database/sql"
	"strings"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
)

type emailLanguageContext struct {
	PreferredLang string
	GroupName     string
}

func normalizeSupportedEmailLang(raw string) string {
	lang := config.NormalizeLanguageTag(raw)
	if !config.IsSupportedLanguage(lang) {
		return ""
	}
	return lang
}

func isEmailMultilangDisabledForGroup(groupName string) bool {
	// Regle metier validee: la desactivation multi-langue repose uniquement
	// sur users.group_name.
	return strings.TrimSpace(groupName) != ""
}

func resolveEmailLanguage(defaultLang, invitationLang, preferredLang, groupName string) string {
	fallback := normalizeSupportedEmailLang(defaultLang)
	if fallback == "" {
		fallback = "fr"
	}

	if isEmailMultilangDisabledForGroup(groupName) {
		return fallback
	}

	if candidate := normalizeSupportedEmailLang(invitationLang); candidate != "" {
		return candidate
	}
	if candidate := normalizeSupportedEmailLang(preferredLang); candidate != "" {
		return candidate
	}
	return fallback
}

func loadUserEmailLanguageContextByID(db *database.DB, userID int64) (emailLanguageContext, error) {
	ctx := emailLanguageContext{}
	if db == nil || userID <= 0 {
		return ctx, nil
	}

	var preferredLang, groupName sql.NullString
	err := db.QueryRow(`SELECT preferred_lang, group_name FROM users WHERE id = ?`, userID).Scan(&preferredLang, &groupName)
	if err == sql.ErrNoRows {
		return ctx, nil
	}
	if err != nil {
		return ctx, err
	}

	ctx.PreferredLang = strings.TrimSpace(preferredLang.String)
	ctx.GroupName = strings.TrimSpace(groupName.String)
	return ctx, nil
}

func loadEmailTemplatesForLanguage(db *database.DB, invitationLang string, ctx emailLanguageContext) (config.EmailTemplatesConfig, string, error) {
	defaultLang := "fr"
	if db != nil {
		defaultLang = db.GetDefaultLang()
	}
	resolved := resolveEmailLanguage(defaultLang, invitationLang, ctx.PreferredLang, ctx.GroupName)

	if db == nil {
		return config.DefaultEmailTemplates(), resolved, nil
	}

	cfg, usedLang, err := db.GetEmailTemplatesConfigForLang(resolved)
	if err != nil {
		return config.DefaultEmailTemplates(), resolved, err
	}
	return cfg, usedLang, nil
}
