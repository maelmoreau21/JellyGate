package handlers

import (
	"regexp"
	"strings"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/mail"
)

var inlineTemplateTokenPattern = regexp.MustCompile(`\{\{\s*\.([A-Za-z][A-Za-z0-9_]*)\s*\}\}`)

func joinTemplateSections(sections ...string) string {
	parts := make([]string, 0, len(sections))
	seen := make(map[string]struct{}, len(sections))

	for _, section := range sections {
		trimmed := strings.TrimSpace(config.EditableEmailTemplateBody(section))
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		parts = append(parts, trimmed)
	}

	return strings.Join(parts, "\n\n")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeEmailBaseTemplates(cfg *config.EmailTemplatesConfig) {
	if cfg == nil {
		return
	}
	cfg.BaseTemplateHeader = strings.TrimSpace(cfg.BaseTemplateHeader)
	cfg.BaseTemplateFooter = strings.TrimSpace(cfg.BaseTemplateFooter)
	if cfg.BaseTemplateHeader == "" {
		cfg.BaseTemplateHeader = config.DefaultEmailBaseHeader()
	}
	if cfg.BaseTemplateFooter == "" {
		cfg.BaseTemplateFooter = config.DefaultEmailBaseFooter()
	}
}

func renderInlineTemplate(tpl string, data map[string]string) (string, error) {
	if strings.TrimSpace(tpl) == "" {
		return "", nil
	}
	rendered := inlineTemplateTokenPattern.ReplaceAllStringFunc(tpl, func(token string) string {
		matches := inlineTemplateTokenPattern.FindStringSubmatch(token)
		if len(matches) != 2 || data == nil {
			return ""
		}
		return data[matches[1]]
	})
	rendered = strings.NewReplacer("\r", " ", "\n", " ").Replace(rendered)
	return strings.Join(strings.Fields(rendered), " "), nil
}

func resolveEmailLogoURL(data map[string]string, configuredLogo string) string {
	logoPath := strings.TrimSpace(configuredLogo)
	if logoPath == "" {
		logoPath = "/static/img/logos/jellygate.svg"
	}
	if data == nil {
		return logoPath
	}

	if explicit := strings.TrimSpace(data["EmailLogoURL"]); explicit != "" {
		if strings.HasPrefix(explicit, "http://") || strings.HasPrefix(explicit, "https://") {
			return explicit
		}
		logoPath = explicit
	}

	baseURL := strings.TrimSpace(data["JellyGateURL"])
	if baseURL == "" {
		return logoPath
	}

	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasPrefix(logoPath, "http://") || strings.HasPrefix(logoPath, "https://") {
		return logoPath
	}
	if strings.HasPrefix(logoPath, "/") {
		return baseURL + logoPath
	}
	return baseURL + "/" + logoPath
}

func sendTemplateIfConfigured(mailer *mail.Mailer, to, subject, lang, templateKey, tpl string, emailCfg config.EmailTemplatesConfig, data map[string]string) error {
	if mailer == nil {
		return nil
	}
	normalizeEmailBaseTemplates(&emailCfg)
	preparedTemplate := config.PrepareEmailTemplateBodyForLanguage(lang, templateKey, tpl, emailCfg.BaseTemplateHeader, emailCfg.BaseTemplateFooter)
	if strings.TrimSpace(to) == "" || strings.TrimSpace(preparedTemplate) == "" {
		return nil
	}
	if data == nil {
		data = map[string]string{}
	}
	if strings.TrimSpace(data["JellyfinServerName"]) == "" {
		data["JellyfinServerName"] = "Jellyfin"
	}
	if strings.TrimSpace(data["AutomaticFooter"]) == "" {
		data["AutomaticFooter"] = config.DefaultEmailAutomaticFooterForLanguage(lang)
	}
	data["EmailLogoURL"] = resolveEmailLogoURL(data, emailCfg.EmailLogoURL)
	renderedSubject, err := renderInlineTemplate(subject, data)
	if err != nil {
		return err
	}
	return mailer.SendTemplateString(to, strings.TrimSpace(renderedSubject), preparedTemplate, data)
}

func emailTime(t time.Time) string {
	return t.Format("02/01/2006 15:04")
}

func resolvePortalLinks(cfg *config.Config, db *database.DB) config.PortalLinksConfig {
	links := config.DefaultPortalLinks()
	if db != nil {
		if saved, err := db.GetPortalLinksConfig(); err == nil {
			links = saved
		}
	}

	if strings.TrimSpace(links.JellyGateURL) == "" && cfg != nil {
		links.JellyGateURL = strings.TrimSpace(cfg.BaseURL)
	}

	if strings.TrimSpace(links.JellyfinURL) == "" && cfg != nil {
		links.JellyfinURL = strings.TrimSpace(cfg.Jellyfin.URL)
	}
	if strings.TrimSpace(links.JellyfinServerName) == "" {
		links.JellyfinServerName = "Jellyfin"
	}
	if strings.TrimSpace(links.JellyseerrURL) == "" && cfg != nil {
		links.JellyseerrURL = strings.TrimSpace(cfg.ThirdParty.JellyseerrURL)
	}
	if strings.TrimSpace(links.JellyTrackURL) == "" && cfg != nil {
		links.JellyTrackURL = strings.TrimSpace(cfg.ThirdParty.JellyTrackURL)
	}

	links.JellyGateURL = strings.TrimSpace(links.JellyGateURL)
	links.JellyfinURL = strings.TrimSpace(links.JellyfinURL)
	links.JellyfinServerName = strings.TrimSpace(links.JellyfinServerName)
	if links.JellyfinServerName == "" {
		links.JellyfinServerName = "Jellyfin"
	}
	links.JellyseerrURL = strings.TrimSpace(links.JellyseerrURL)
	links.JellyTrackURL = strings.TrimSpace(links.JellyTrackURL)
	return links
}
