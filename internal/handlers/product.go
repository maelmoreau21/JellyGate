package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

type productHealthCheck struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type productMarkdownPreviewInput struct {
	Markdown string `json:"markdown"`
}

// ProductPage affiche le centre produit qui regroupe les nouvelles idees JellyGate.
func (h *AdminHandler) ProductPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.AdminUsername = sess.Username
	td.IsAdmin = true
	td.CanInvite = true
	td.Section = "product"

	if err := h.renderer.Render(w, "admin/product.html", td); err != nil {
		slog.Error("Erreur rendu product page", "error", err)
		http.Error(w, h.tr(r, "common_server_error_page", "Erreur serveur : impossible de charger la page"), http.StatusInternalServerError)
	}
}

// ProductConfig retourne la configuration des modules produit.
func (h *AdminHandler) ProductConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.db.GetProductFeaturesConfig()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur lecture configuration produit"})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: config.NormalizeProductFeaturesConfig(cfg)})
}

// SaveProductConfig sauvegarde la configuration des modules produit.
func (h *AdminHandler) SaveProductConfig(w http.ResponseWriter, r *http.Request) {
	var input config.ProductFeaturesConfig
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "JSON invalide : " + err.Error()})
		return
	}
	input = config.NormalizeProductFeaturesConfig(input)
	if err := h.db.SaveProductFeaturesConfig(input); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur sauvegarde configuration produit"})
		return
	}
	_ = h.db.LogAction("settings.product_features.saved", currentActorName(r), "", "")
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Configuration produit sauvegardee", Data: input})
}

// ProductMarkdownPreview rend un contenu Markdown avec le moteur simple et securise.
func (h *AdminHandler) ProductMarkdownPreview(w http.ResponseWriter, r *http.Request) {
	var input productMarkdownPreviewInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "JSON invalide : " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]string{
		"html": string(renderProductMarkdownHTML(input.Markdown)),
	}})
}

// ProductHealth retourne les diagnostics operationnels du portail.
func (h *AdminHandler) ProductHealth(w http.ResponseWriter, r *http.Request) {
	checks := make([]productHealthCheck, 0, 10)
	add := func(key, label, status, message string) {
		checks = append(checks, productHealthCheck{Key: key, Label: label, Status: status, Message: message})
	}

	if err := h.db.QueryRow(`SELECT 1`).Scan(new(int)); err != nil {
		add("database", "Base de donnees", "error", err.Error())
	} else {
		add("database", "Base de donnees", "ok", h.db.Driver())
	}

	productCfg, _ := h.db.GetProductFeaturesConfig()
	if productCfg.Setup.Completed {
		add("setup", "Assistant premier lancement", "ok", "Marque comme termine")
	} else {
		add("setup", "Assistant premier lancement", "warn", "Checklist a finaliser")
	}

	if h.jfClient == nil {
		add("jellyfin", "Jellyfin", "error", "Client non configure")
	} else if _, err := h.jfClient.GetPublicSystemInfo(); err != nil {
		add("jellyfin", "Jellyfin", "error", err.Error())
	} else {
		add("jellyfin", "Jellyfin", "ok", "API publique joignable")
	}

	smtpCfg, err := h.db.GetSMTPConfig()
	if err != nil {
		add("smtp", "SMTP", "error", err.Error())
	} else if strings.TrimSpace(smtpCfg.Host) == "" {
		add("smtp", "SMTP", "warn", "Non configure")
	} else {
		add("smtp", "SMTP", "ok", fmt.Sprintf("%s:%d", smtpCfg.Host, smtpCfg.Port))
	}

	webhooksCfg, err := h.db.GetWebhooksConfig()
	if err != nil {
		add("webhooks", "Webhooks", "error", err.Error())
	} else {
		total := 0
		if strings.TrimSpace(webhooksCfg.Discord.URL) != "" {
			total++
		}
		if strings.TrimSpace(webhooksCfg.Telegram.Token) != "" && strings.TrimSpace(webhooksCfg.Telegram.ChatID) != "" {
			total++
		}
		if strings.TrimSpace(webhooksCfg.Matrix.URL) != "" && strings.TrimSpace(webhooksCfg.Matrix.RoomID) != "" && strings.TrimSpace(webhooksCfg.Matrix.Token) != "" {
			total++
		}
		if total == 0 {
			add("webhooks", "Webhooks", "warn", "Aucun canal configure")
		} else {
			add("webhooks", "Webhooks", "ok", fmt.Sprintf("%d canal(aux) configure(s)", total))
		}
	}

	links, err := h.db.GetPortalLinksConfig()
	if err != nil {
		add("public_urls", "URLs publiques", "error", err.Error())
	} else if strings.TrimSpace(links.JellyGateURL) == "" || strings.TrimSpace(links.JellyfinURL) == "" {
		add("public_urls", "URLs publiques", "warn", "JellyGate ou Jellyfin manquant")
	} else {
		add("public_urls", "URLs publiques", "ok", "JellyGate et Jellyfin renseignes")
	}

	backupCfg, err := h.db.GetBackupConfig()
	if err != nil {
		add("backups", "Sauvegardes", "error", err.Error())
	} else if !backupCfg.Enabled {
		add("backups", "Sauvegardes", "warn", "Sauvegarde planifiee desactivee")
	} else {
		add("backups", "Sauvegardes", "ok", fmt.Sprintf("Planifiee a %02d:%02d", backupCfg.Hour, backupCfg.Minute))
	}

	defaultLang := h.db.GetDefaultLang()
	if config.IsSupportedLanguage(defaultLang) {
		add("i18n", "i18n", "ok", "Langue par defaut: "+defaultLang)
	} else {
		add("i18n", "i18n", "warn", "Langue par defaut invalide")
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: checks})
}

// ProductTimeline retourne les derniers evenements d'audit pour la timeline admin.
func (h *AdminHandler) ProductTimeline(w http.ResponseWriter, r *http.Request) {
	productCfg, _ := h.db.GetProductFeaturesConfig()
	limit := config.NormalizeProductFeaturesConfig(productCfg).AdminTimeline.Limit
	if limit <= 0 {
		limit = 80
	}

	rows, err := h.db.Query(`
		SELECT action, actor, target, details, created_at
		FROM audit_log
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur lecture timeline admin"})
		return
	}
	defer rows.Close()

	events := make([]UserTimelineEvent, 0, limit)
	for rows.Next() {
		var action, actor, target, details, createdAt sql.NullString
		if err := rows.Scan(&action, &actor, &target, &details, &createdAt); err != nil {
			continue
		}
		events = append(events, UserTimelineEvent{
			At:       normalizeTimelineAt(createdAt.String),
			Action:   action.String,
			Category: timelineCategory(action.String),
			Severity: timelineSeverity(action.String, details.String),
			Actor:    actor.String,
			Target:   target.String,
			Details:  details.String,
			Message:  describeTimelineAction(action.String, actor.String, target.String, details.String),
		})
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: events})
}

// ProductLifecyclePreview expose l'impact actuel des regles de cycle de vie.
func (h *AdminHandler) ProductLifecyclePreview(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	count := func(query string, args ...interface{}) int {
		var n int
		if err := h.db.QueryRow(query, args...).Scan(&n); err != nil {
			return 0
		}
		return n
	}

	productCfg, _ := h.db.GetProductFeaturesConfig()
	lifecycle := config.NormalizeProductFeaturesConfig(productCfg).Lifecycle
	data := map[string]interface{}{
		"enabled":                    lifecycle.Enabled,
		"expiry_reminder_days":       lifecycle.ExpiryReminderDays,
		"disable_inactive_days":      lifecycle.DisableInactiveDays,
		"delete_disabled_after_days": lifecycle.DeleteDisabledAfterDays,
		"expired_due": count(`
			SELECT COUNT(1) FROM users
			WHERE is_active = TRUE AND access_expires_at IS NOT NULL AND access_expires_at < ?
		`, now),
		"delete_due": count(`
			SELECT COUNT(1) FROM users
			WHERE delete_at IS NOT NULL AND delete_at < ?
		`, now),
		"disable_then_delete_pending": count(`
			SELECT COUNT(1) FROM users
			WHERE is_active = FALSE AND expiry_action = 'disable_then_delete' AND expired_at IS NOT NULL
		`),
		"active_without_expiry": count(`
			SELECT COUNT(1) FROM users
			WHERE is_active = TRUE AND access_expires_at IS NULL
		`),
		"generated_at": now.Format(time.RFC3339),
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: data})
}

func currentActorName(r *http.Request) string {
	if sess := session.FromContext(r.Context()); sess != nil {
		return sess.Username
	}
	return ""
}

var (
	productBoldPattern   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	productItalicPattern = regexp.MustCompile(`\*([^*]+)\*`)
)

func renderProductMarkdownHTML(raw string) template.HTML {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	var b strings.Builder
	inList := false
	flushList := func() {
		if inList {
			b.WriteString("</ul>")
			inList = false
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flushList()
			continue
		}

		switch {
		case strings.HasPrefix(trimmed, "### "):
			flushList()
			b.WriteString(`<h4>`)
			b.WriteString(productInlineMarkdown(trimmed[4:]))
			b.WriteString(`</h4>`)
		case strings.HasPrefix(trimmed, "## "):
			flushList()
			b.WriteString(`<h3>`)
			b.WriteString(productInlineMarkdown(trimmed[3:]))
			b.WriteString(`</h3>`)
		case strings.HasPrefix(trimmed, "# "):
			flushList()
			b.WriteString(`<h2>`)
			b.WriteString(productInlineMarkdown(trimmed[2:]))
			b.WriteString(`</h2>`)
		case strings.HasPrefix(trimmed, "- "):
			if !inList {
				b.WriteString("<ul>")
				inList = true
			}
			b.WriteString(`<li>`)
			b.WriteString(productInlineMarkdown(trimmed[2:]))
			b.WriteString(`</li>`)
		default:
			flushList()
			b.WriteString(`<p>`)
			b.WriteString(productInlineMarkdown(trimmed))
			b.WriteString(`</p>`)
		}
	}
	flushList()

	return template.HTML(b.String())
}

func productInlineMarkdown(raw string) string {
	escaped := template.HTMLEscapeString(strings.TrimSpace(raw))
	escaped = productBoldPattern.ReplaceAllString(escaped, "<strong>$1</strong>")
	escaped = productItalicPattern.ReplaceAllString(escaped, "<em>$1</em>")
	return escaped
}
