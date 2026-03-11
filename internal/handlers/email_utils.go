package handlers

import (
	"bytes"
	"strings"
	texttemplate "text/template"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/mail"
)

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
	tmpl, err := texttemplate.New("inline").Parse(tpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func sendTemplateIfConfigured(mailer *mail.Mailer, to, subject, templateKey, tpl string, emailCfg config.EmailTemplatesConfig, data map[string]string) error {
	if mailer == nil {
		return nil
	}
	normalizeEmailBaseTemplates(&emailCfg)
	preparedTemplate := config.PrepareEmailTemplateBodyFor(templateKey, tpl, emailCfg.BaseTemplateHeader, emailCfg.BaseTemplateFooter)
	if strings.TrimSpace(to) == "" || strings.TrimSpace(preparedTemplate) == "" {
		return nil
	}
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
	if strings.TrimSpace(links.JellyseerrURL) == "" && cfg != nil {
		links.JellyseerrURL = strings.TrimSpace(cfg.ThirdParty.JellyseerrURL)
	}
	if strings.TrimSpace(links.JellyTrackURL) == "" && cfg != nil {
		links.JellyTrackURL = strings.TrimSpace(cfg.ThirdParty.JellyTrackURL)
	}

	links.JellyGateURL = strings.TrimSpace(links.JellyGateURL)
	links.JellyfinURL = strings.TrimSpace(links.JellyfinURL)
	links.JellyseerrURL = strings.TrimSpace(links.JellyseerrURL)
	links.JellyTrackURL = strings.TrimSpace(links.JellyTrackURL)
	return links
}
