package handlers

import (
	"strings"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/mail"
)

func joinTemplateSections(sections ...string) string {
	parts := make([]string, 0, len(sections))
	seen := make(map[string]struct{}, len(sections))

	for _, section := range sections {
		trimmed := strings.TrimSpace(section)
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

func sendTemplateIfConfigured(mailer *mail.Mailer, to, subject, tpl string, data map[string]string) error {
	if mailer == nil {
		return nil
	}
	if strings.TrimSpace(to) == "" || strings.TrimSpace(tpl) == "" {
		return nil
	}
	return mailer.SendTemplateString(to, subject, tpl, data)
}

func emailTime(t time.Time) string {
	return t.Format("02/01/2006 15:04")
}
