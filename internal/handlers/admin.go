// Package handlers — admin.go
//
// Gère les endpoints JSON du tableau de bord administrateur.
// Toutes les routes sont protégées par le middleware RequireAuth.
//
// Endpoints :
//   - GET    /admin/api/users         → Liste des utilisateurs (fusion SQLite + Jellyfin)
//   - POST   /admin/api/users/{id}/toggle → Active/désactive un compte (AD + Jellyfin)
//   - DELETE /admin/api/users/{id}    → Suppression totale (AD + Jellyfin + SQLite)
//
// Les erreurs partielles sont loggées mais ne bloquent pas les opérations
// restantes (ex: si l'utilisateur est déjà supprimé de l'AD, on continue
// avec Jellyfin et SQLite).
package handlers

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/maelmoreau21/JellyGate/internal/authentik"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	"github.com/maelmoreau21/JellyGate/internal/mail"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

// — Structures de réponse JSON ————————————————————————————————————————————————————————

// UserResponse est la représentation JSON d'un utilisateur pour l'API admin.
type UserResponse struct {
	ID                 int64  `json:"id"`
	JellyfinID         string `json:"jellyfin_id"`
	Username           string `json:"username"`
	Email              string `json:"email"`
	AuthentikID        string `json:"authentik_id"`
	GroupName          string `json:"group_name"`
	PresetID           string `json:"preset_id"` // NEW
	InvitedBy          string `json:"invited_by"`
	IsActive           bool   `json:"is_active"`
	IsBanned           bool   `json:"is_banned"`
	CanInvite          bool   `json:"can_invite"`
	AccessExpiresAt    string `json:"access_expires_at,omitempty"` // ISO 8601
	DeleteAt           string `json:"delete_at,omitempty"`
	ExpiryAction       string `json:"expiry_action"`
	DeleteAfterDays    int    `json:"expiry_delete_after_days"`
	ExpiredAt          string `json:"expired_at,omitempty"`
	ProfileApplyStatus string `json:"profile_apply_status"`
	ProfileApplyError  string `json:"profile_apply_error,omitempty"`
	ProfileAppliedAt   string `json:"profile_applied_at,omitempty"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`

	// Statuts temps réel depuis Jellyfin (enrichissement)
	JellyfinDisabled        bool   `json:"jellyfin_disabled"`
	JellyfinExists          bool   `json:"jellyfin_exists"`
	JellyfinPrimaryImageTag string `json:"jellyfin_primary_image_tag,omitempty"`
}

// APIResponse est l'enveloppe standard pour toutes les réponses JSON.
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Errors  []string    `json:"errors,omitempty"`
}

// DashboardStatsResponse regroupe les données pour les graphiques et le moniteur de santé.
const maxAvatarUploadBytes int64 = 5 * 1024 * 1024

type DashboardStatsResponse struct {
	Registrations []database.RegistrationDay `json:"registrations"`
	Invitations   database.InvitationStats   `json:"invitations"`
	Health        map[string]bool            `json:"health"`
}

type UserTimelineEvent struct {
	At       string `json:"at"`
	Action   string `json:"action"`
	Category string `json:"category"`
	Severity string `json:"severity"`
	Actor    string `json:"actor,omitempty"`
	Target   string `json:"target,omitempty"`
	Details  string `json:"details,omitempty"`
	Message  string `json:"message"`
}

type adminUserRecord struct {
	ID                 int64
	Username           string
	Email              string
	PendingEmail       string
	EmailVerified      bool
	JellyfinID         string
	AuthentikID        sql.NullString
	GroupName          string
	PresetID           string // NEW
	ContactDiscord     string
	ContactTelegram    string
	IsActive           bool
	CanInvite          bool
	PreferredLang      string
	NotifyExpiry       bool
	NotifyEvents       bool
	OptInEmail         bool
	OptInDiscord       bool
	OptInTelegram      bool
	ExpiryAction       string
	DeleteAfterDays    int
	DeleteAt           sql.NullString
	ExpiredAt          sql.NullString
	ProfileApplyStatus string
	ProfileApplyError  string
	ProfileAppliedAt   sql.NullString
	AccessExpiresAt    sql.NullString
	CreatedAt          sql.NullString
}

type UpdateUserRequest struct {
	Email           *string `json:"email"`
	GroupName       *string `json:"group_name"`
	PresetID        *string `json:"preset_id"` // NEW
	CanInvite       *bool   `json:"can_invite"`
	AccessExpiresAt *string `json:"access_expires_at"`
	ClearExpiry     bool    `json:"clear_expiry"`
}

type CreateAdminUserRequest struct {
	Username         string `json:"username"`
	Email            string `json:"email"`
	Password         string `json:"password"`
	PolicyPresetID   string `json:"policy_preset_id"`
	DisableAfterDays int    `json:"disable_after_days"`
	AccessExpiresAt  string `json:"access_expires_at"`
	CanInvite        bool   `json:"can_invite"`
	SendWelcomeEmail bool   `json:"send_welcome_email"`
}

type UpdateMyAccountRequest struct {
	Email                *string `json:"email"`
	ContactDiscord       *string `json:"contact_discord"`
	ContactTelegram      *string `json:"contact_telegram"`
	ContactMatrix        *string `json:"contact_matrix"`
	PreferredLang        *string `json:"preferred_lang"`
	NotifyExpiryReminder *bool   `json:"notify_expiry_reminder"`
	NotifyAccountEvents  *bool   `json:"notify_account_events"`
	OptInEmail           *bool   `json:"opt_in_email"`
	OptInDiscord         *bool   `json:"opt_in_discord"`
	OptInTelegram        *bool   `json:"opt_in_telegram"`
	OptInMatrix          *bool   `json:"opt_in_matrix"`
}

type BulkJellyfinPolicyPatch struct {
	EnableDownloads  *bool `json:"enable_downloads"`
	EnableRemote     *bool `json:"enable_remote_access"`
	MaxActiveSession *int  `json:"max_active_sessions"`
	BitrateLimit     *int  `json:"remote_bitrate_limit"`
}

type BulkUsersActionRequest struct {
	UserIDs         []int64                  `json:"user_ids"`
	Action          string                   `json:"action"`
	Preview         bool                     `json:"preview"`
	PolicyPresetID  string                   `json:"policy_preset_id"`
	EmailSubject    string                   `json:"email_subject"`
	EmailBody       string                   `json:"email_body"`
	CanInvite       *bool                    `json:"can_invite"`
	AccessExpiresAt *string                  `json:"access_expires_at"`
	ClearExpiry     bool                     `json:"clear_expiry"`
	JellyfinPolicy  *BulkJellyfinPolicyPatch `json:"jellyfin_policy"`
}

// — Admin Handler —————————————————————————————————————————————————————————————————————

// AdminHandler gère les endpoints d'administration.
type AdminHandler struct {
	cfg        *config.Config
	db         *database.DB
	jfClient   *jellyfin.Client
	authClient authentik.Client
	mailer     *mail.Mailer
	renderer   *render.Engine
}

// NewAdminHandler crée un nouveau handler d'administration.
func NewAdminHandler(cfg *config.Config, db *database.DB, jf *jellyfin.Client, auth authentik.Client, m *mail.Mailer, renderer *render.Engine) *AdminHandler {
	return &AdminHandler{
		cfg:        cfg,
		db:         db,
		jfClient:   jf,
		authClient: auth,
		mailer:     m,
		renderer:   renderer,
	}
}

// SetMailer remplace le Mailer SMTP (rechargement à chaud).
func (h *AdminHandler) SetMailer(m *mail.Mailer) { h.mailer = m }

func (h *AdminHandler) tr(r *http.Request, key, fallback string) string {
	if h.renderer == nil {
		return fallback
	}
	lang := jgmw.LangFromContext(r.Context())
	value := h.renderer.Translate(lang, key)
	if value == "["+key+"]" {
		return fallback
	}
	return value
}

func (h *AdminHandler) sendUserEventEmail(rec *adminUserRecord, subject, lang, templateKey, templateBody string, emailCfg config.EmailTemplatesConfig, extra map[string]string) error {
	if rec == nil {
		return nil
	}
	if strings.TrimSpace(rec.Email) == "" {
		return nil
	}

	links := resolvePortalLinks(h.cfg, h.db)

	helpURL := strings.TrimSpace(links.JellyGateURL)
	if helpURL == "" {
		helpURL = strings.TrimSpace(h.cfg.BaseURL)
	}

	data := map[string]string{
		"Username":           rec.Username,
		"Email":              rec.Email,
		"HelpURL":            helpURL,
		"JellyGateURL":       helpURL,
		"JellyfinURL":        links.JellyfinURL,
		"JellyfinServerName": links.JellyfinServerName,
		"JellyseerrURL":      links.JellyseerrURL,
		"JellyTrackURL":      links.JellyTrackURL,
	}
	if rec.AccessExpiresAt.Valid {
		if t, err := parseAccessExpiry(rec.AccessExpiresAt.String); err == nil {
			data["ExpiryDate"] = emailTime(t)
		}
	}
	for key, value := range extra {
		data[key] = value
	}

	return sendTemplateIfConfigured(h.mailer, rec.Email, subject, lang, templateKey, templateBody, emailCfg, data)
}

func (h *AdminHandler) canSendUserTemplate(userID int64, templateKey string) bool {
	if userID <= 0 {
		return true
	}

	var notifyExpiry, notifyEvents bool
	err := h.db.QueryRow(
		`SELECT notify_expiry_reminder, notify_account_events FROM users WHERE id = ?`,
		userID,
	).Scan(&notifyExpiry, &notifyEvents)
	if err != nil {
		return true
	}

	if templateKey == "expiry_reminder" {
		return notifyExpiry
	}
	return notifyEvents
}

func (h *AdminHandler) sendUserTemplateByKey(rec *adminUserRecord, templateKey string, extra map[string]string) error {
	if rec == nil {
		return nil
	}
	if !h.canSendUserTemplate(rec.ID, templateKey) {
		return nil
	}

	emailCfg, usedLang, err := loadEmailTemplatesForLanguage(h.db, "", emailLanguageContext{
		PreferredLang: rec.PreferredLang,
		GroupName:     rec.GroupName,
	})
	if err != nil {
		return err
	}
	defaults := config.DefaultEmailTemplatesForLanguage(usedLang)

	var subject, body string
	switch templateKey {
	case "user_enabled":
		if emailCfg.DisableUserEnabledEmail {
			return nil
		}
		subject = firstNonEmpty(emailCfg.UserEnabledSubject, defaults.UserEnabledSubject)
		body = emailCfg.UserEnabled
	case "user_disabled":
		if emailCfg.DisableUserDisabledEmail {
			return nil
		}
		subject = firstNonEmpty(emailCfg.UserDisabledSubject, defaults.UserDisabledSubject)
		body = emailCfg.UserDisabled
	case "user_deleted":
		if emailCfg.DisableUserDeletionEmail {
			return nil
		}
		subject = firstNonEmpty(emailCfg.UserDeletionSubject, defaults.UserDeletionSubject)
		body = emailCfg.UserDeletion
	case "user_expired":
		if emailCfg.DisableUserExpiredEmail {
			return nil
		}
		subject = firstNonEmpty(emailCfg.UserExpiredSubject, defaults.UserExpiredSubject)
		body = emailCfg.UserExpired
	case "expiry_adjusted":
		if emailCfg.DisableExpiryAdjustedEmail {
			return nil
		}
		subject = firstNonEmpty(emailCfg.ExpiryAdjustedSubject, defaults.ExpiryAdjustedSubject)
		body = emailCfg.ExpiryAdjusted
	case "expiry_reminder":
		if emailCfg.DisableExpiryReminderEmails {
			return nil
		}
		subject = firstNonEmpty(emailCfg.ExpiryReminderSubject, defaults.ExpiryReminderSubject)
		body = emailCfg.ExpiryReminder
	default:
		return nil
	}

	return h.sendUserEventEmail(rec, subject, usedLang, templateKey, body, emailCfg, extra)
}

func containsInt(values []int, target int) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func chooseExpiryReminderTemplate(cfg config.EmailTemplatesConfig, stageDays int) string {
	switch stageDays {
	case 14:
		if strings.TrimSpace(cfg.ExpiryReminder14) != "" {
			return cfg.ExpiryReminder14
		}
	case 7:
		if strings.TrimSpace(cfg.ExpiryReminder7) != "" {
			return cfg.ExpiryReminder7
		}
	case 1:
		if strings.TrimSpace(cfg.ExpiryReminder1) != "" {
			return cfg.ExpiryReminder1
		}
	}
	return cfg.ExpiryReminder
}

func normalizeExpiryAction(raw string) string {
	action := strings.TrimSpace(strings.ToLower(raw))
	switch action {
	case "delete", "disable_then_delete":
		return action
	default:
		return "disable"
	}
}


// — Background Jobs ———————————————————————————————————————————————————————————————————

// StartExpirationJob lance une routine en arrière-plan qui vérifie périodiquement
// si des comptes utilisateurs ont expiré, afin de les désactiver automatiquement.
func (h *AdminHandler) StartExpirationJob(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		// Faire une première vérification au démarrage court
		time.Sleep(5 * time.Second)
		h.runExpirationCheck()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.runExpirationCheck()
			}
		}
	}()
}

func (h *AdminHandler) runExpirationCheck() {
	slog.Debug("Lancement du job d'expiration automatique des utilisateurs...")
	now := time.Now()
	productCfg, _ := h.db.GetProductFeaturesConfig()
	productCfg = config.NormalizeProductFeaturesConfig(productCfg)

	emailCfg, _ := h.db.GetEmailTemplatesConfig()
	if emailCfg.DisableExpiryReminderEmails {
		return
	}
	reminderStages := []int{14, 7, 1}
	if productCfg.Lifecycle.Enabled && len(productCfg.Lifecycle.ExpiryReminderDays) > 0 {
		reminderStages = append([]int(nil), productCfg.Lifecycle.ExpiryReminderDays...)
	}
	if emailCfg.ExpiryReminderDays > 0 && !containsInt(reminderStages, emailCfg.ExpiryReminderDays) {
		reminderStages = append(reminderStages, emailCfg.ExpiryReminderDays)
	}

	maxStage := 1
	for _, stage := range reminderStages {
		if stage > maxStage {
			maxStage = stage
		}
	}

	reminderWindow := now.Add(time.Duration(maxStage+1) * 24 * time.Hour)

	// Rappels d'expiration imminente
	reminderRows, err := h.db.Query(`
		SELECT id, username, email, access_expires_at, notify_expiry_reminder
		FROM users
		WHERE is_active = TRUE
		  AND access_expires_at IS NOT NULL
		  AND access_expires_at >= ?
		  AND access_expires_at <= ?
	`, now, reminderWindow)
	if err == nil {
		for reminderRows.Next() {
			var id int64
			var username string
			var notifyExpiry bool
			var email, expiryRaw sql.NullString
			if err := reminderRows.Scan(&id, &username, &email, &expiryRaw, &notifyExpiry); err != nil {
				continue
			}
			if !email.Valid || strings.TrimSpace(email.String) == "" || !expiryRaw.Valid {
				continue
			}
			if !notifyExpiry {
				continue
			}

			expiryTime, err := parseAccessExpiry(expiryRaw.String)
			if err != nil {
				continue
			}
			hoursLeft := expiryTime.Sub(now).Hours()
			if hoursLeft <= 0 {
				continue
			}

			stageDays := int(math.Ceil(hoursLeft / 24.0))
			if stageDays < 1 || !containsInt(reminderStages, stageDays) {
				continue
			}
			if !h.canSendUserTemplate(id, "expiry_reminder") {
				continue
			}

			details := fmt.Sprintf("stage=%d|expiry=%s", stageDays, expiryRaw.String)

			var alreadySent int
			_ = h.db.QueryRow(
				`SELECT COUNT(1) FROM audit_log WHERE action = 'user.expiry_reminder.sent' AND target = ? AND details = ?`,
				username,
				details,
			).Scan(&alreadySent)
			if alreadySent > 0 {
				continue
			}

			rec := &adminUserRecord{
				ID:              id,
				Username:        username,
				Email:           email.String,
				AccessExpiresAt: expiryRaw,
			}
			templateBody := chooseExpiryReminderTemplate(emailCfg, stageDays)
			usedLang := h.db.GetDefaultLang()
			subject := firstNonEmpty(emailCfg.ExpiryReminderSubject, config.DefaultEmailTemplatesForLanguage(usedLang).ExpiryReminderSubject)
			if err := h.sendUserEventEmail(rec, subject, usedLang, "expiry_reminder", templateBody, emailCfg, map[string]string{
				"ExpiryDate":    emailTime(expiryTime),
				"ReminderStage": fmt.Sprintf("J-%d", stageDays),
			}); err != nil {
				slog.Error("Erreur envoi reminder d'expiration", "user", username, "error", err)
				continue
			}
			_ = h.db.LogAction("user.expiry_reminder.sent", "system", username, details)
		}
		if err := reminderRows.Close(); err != nil {
			slog.Warn("Erreur fermeture rows reminders expiration", "error", err)
		}
	}

	// Suppression planifiee (simple): delete_at atteint.
	deleteRows, err := h.db.Query(`
		SELECT id, username
		FROM users
		WHERE delete_at IS NOT NULL
		  AND delete_at < ?
	`, now)
	if err == nil {
		for deleteRows.Next() {
			var id int64
			var username string
			if err := deleteRows.Scan(&id, &username); err != nil {
				continue
			}

			rec, err := h.loadAdminUserByID(id)
			if err != nil {
				continue
			}

			partials, err := h.deleteUserRecord(rec, "system")
			if err != nil {
				slog.Error("Erreur suppression planifiee", "user", username, "error", err, "partials", partials)
				continue
			}
			if len(partials) > 0 {
				slog.Warn("Suppression planifiee avec erreurs partielles", "user", username, "partials", partials)
			}

			_ = h.db.LogAction("user.expired.deleted", "system", username, "Suppression planifiee (delete_at)")
		}
		if err := deleteRows.Close(); err != nil {
			slog.Warn("Erreur fermeture rows suppressions planifiees", "error", err)
		}
	}

	// Rechercher les utilisateurs actifs dont access_expires_at est dépassé.
	rows, err := h.db.Query(`
		SELECT id, username, email, jellyfin_id, authentik_id, access_expires_at, expiry_action, expiry_delete_after_days
		FROM users
		WHERE is_active = TRUE
		  AND access_expires_at IS NOT NULL
		  AND access_expires_at < ?
	`, now)
	if err != nil {
		slog.Error("Erreur SQL lors du job d'expiration", "error", err)
		return
	}

	type expiredUser struct {
		ID              int64
		Username        string
		Email           string
		JellyfinID      string
		AuthentikID     sql.NullString
		ExpiresAt       string
		ExpiryAction    string
		DeleteAfterDays int
	}

	usersToProcess := make([]expiredUser, 0)
	for rows.Next() {
		var u expiredUser
		var email, jfID, authID, expiresAt, expiryAction sql.NullString
		if err := rows.Scan(&u.ID, &u.Username, &email, &jfID, &authID, &expiresAt, &expiryAction, &u.DeleteAfterDays); err != nil {
			continue
		}
		u.Email = email.String
		u.JellyfinID = jfID.String
		u.AuthentikID = authID
		u.ExpiresAt = expiresAt.String
		u.ExpiryAction = normalizeExpiryAction(expiryAction.String)
		usersToProcess = append(usersToProcess, u)
	}
	if err := rows.Close(); err != nil {
		slog.Warn("Erreur fermeture rows expiration", "error", err)
	}

	if len(usersToProcess) > 0 {
		slog.Info("Comptes expires detectes", "count", len(usersToProcess))
	}

	for _, u := range usersToProcess {
		if u.ExpiryAction == "delete" {
			rec, err := h.loadAdminUserByID(u.ID)
			if err != nil {
				continue
			}
			partials, err := h.deleteUserRecord(rec, "system")
			if err != nil {
				slog.Error("Erreur suppression auto a expiration", "user", u.Username, "error", err, "partials", partials)
				continue
			}
			if len(partials) > 0 {
				slog.Warn("Suppression auto avec erreurs partielles", "user", u.Username, "partials", partials)
			}
			_ = h.db.LogAction("user.expired.deleted", "system", u.Username, "Suppression automatique a expiration")
			continue
		}

		slog.Info("Desactivation automatique de l'utilisateur (Expire)", "user", u.Username, "policy", u.ExpiryAction)

		if h.authClient != nil && u.AuthentikID.Valid && u.AuthentikID.String != "" {
			if err := h.authClient.SetUserActiveStatusByString(context.Background(), u.AuthentikID.String, false); err != nil {
				slog.Error("Erreur lors de la desactivation Authentik (Expiration)", "error", err)
			}
		}

		if u.JellyfinID != "" {
			if err := h.jfClient.DisableUser(u.JellyfinID); err != nil {
				slog.Error("Erreur lors de la desactivation Jellyfin (Expiration)", "error", err)
			}
		}

		_, err := h.db.Exec(`UPDATE users SET is_active = FALSE, expired_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, u.ID)
		if err != nil {
			slog.Error("Erreur lors de la desactivation (Expiration)", "error", err, "driver", h.db.Driver())
		}

		_ = h.db.LogAction("user.expired", "system", u.Username, fmt.Sprintf("Compte desactive automatiquement (policy=%s)", u.ExpiryAction))

		rec := &adminUserRecord{
			ID:              u.ID,
			Username:        u.Username,
			Email:           u.Email,
			DeleteAfterDays: u.DeleteAfterDays,
			ExpiryAction:    u.ExpiryAction,
			AccessExpiresAt: sql.NullString{String: u.ExpiresAt, Valid: u.ExpiresAt != ""},
		}
		if err := h.sendUserTemplateByKey(rec, "user_expired", map[string]string{"ExpiryDate": u.ExpiresAt}); err != nil {
			slog.Error("Erreur envoi email user_expired", "user", u.Username, "error", err)
		}
	}

	// Politique disable_then_delete: suppression différée apres la desactivation.
	deletionRows, err := h.db.Query(`
		SELECT id, username, expired_at, expiry_delete_after_days
		FROM users
		WHERE is_active = FALSE
		  AND expiry_action = 'disable_then_delete'
		  AND expired_at IS NOT NULL
	`)
	if err != nil {
		return
	}
	defer deletionRows.Close()

	for deletionRows.Next() {
		var (
			id              int64
			username        string
			expiredAtRaw    string
			deleteAfterDays int
		)
		if err := deletionRows.Scan(&id, &username, &expiredAtRaw, &deleteAfterDays); err != nil {
			continue
		}

		expiredAt, err := parseAccessExpiry(expiredAtRaw)
		if err != nil {
			continue
		}

		if deleteAfterDays <= 0 && productCfg.Lifecycle.Enabled {
			deleteAfterDays = productCfg.Lifecycle.DeleteDisabledAfterDays
		}
		readyAt := expiredAt.AddDate(0, 0, deleteAfterDays)
		if deleteAfterDays > 0 && now.Before(readyAt) {
			continue
		}

		rec, err := h.loadAdminUserByID(id)
		if err != nil {
			continue
		}
		partials, err := h.deleteUserRecord(rec, "system")
		if err != nil {
			slog.Error("Erreur suppression differee apres expiration", "user", username, "error", err, "partials", partials)
			continue
		}
		if len(partials) > 0 {
			slog.Warn("Suppression differee avec erreurs partielles", "user", username, "partials", partials)
		}
		_ = h.db.LogAction("user.expired.deleted", "system", username, fmt.Sprintf("Suppression differree apres %d jour(s)", deleteAfterDays))
	}
}

// — Pages HTML ————————————————————————————————————————————————————————————————————————

// DashboardPage affiche la page principale du tableau de bord.
func (h *AdminHandler) DashboardPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	productCfg, _ := h.db.GetProductFeaturesConfig()
	td.Data["AccountMarkdownHTML"] = renderProductMarkdownHTML(productCfg.Content.AccountMarkdown)
	td.AdminUsername = sess.Username
	td.IsAdmin = sess.IsAdmin
	td.CanInvite = h.resolveCanInviteForSession(sess)
	td.AuthentikEnabled = h.db.IsAuthentikEnabled()
	td.Section = "dashboard"

	if err := h.renderer.Render(w, "admin/dashboard.html", td); err != nil {
		slog.Error("Erreur rendu dashboard", "error", err)
		http.Error(w, h.tr(r, "common_server_error_page", "Erreur serveur : impossible de charger la page"), http.StatusInternalServerError)
	}
}

// DashboardStats retourne les données pour les graphiques du dashboard (AJAX).
func (h *AdminHandler) DashboardStats(w http.ResponseWriter, r *http.Request) {
	// 1. Historique des inscriptions (30 jours)
	history, err := h.db.GetRegistrationHistory(30)
	if err != nil {
		slog.Error("Erreur GetRegistrationHistory", "error", err)
	}

	// 2. Stats des invitations
	invStats, err := h.db.GetInvitationStats()
	if err != nil {
		slog.Error("Erreur GetInvitationStats", "error", err)
	}

	// 3. Santé des services
	health := map[string]bool{
		"database":  true,
		"jellyfin":  false,
		"authentik": h.db.IsAuthentikEnabled(),
	}

	// Test Jellyfin (léger)
	if h.jfClient != nil {
		if _, err := h.jfClient.GetPublicSystemInfo(); err == nil {
			health["jellyfin"] = true
		}
	}

	// Test Authentik (si activé)
	if h.authClient != nil && h.cfg != nil && h.cfg.Authentik.Enabled {
		if _, err := h.authClient.ListUsers(r.Context()); err == nil {
			health["authentik"] = true
		} else {
			health["authentik"] = false
		}
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: DashboardStatsResponse{
			Registrations: history,
			Invitations:   invStats,
			Health:        health,
		},
	})
}

// MyAccountPage affiche la page "Mon compte" pour l'utilisateur connecté.
func (h *AdminHandler) MyAccountPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.AdminUsername = sess.Username
	td.IsAdmin = sess.IsAdmin
	td.CanInvite = h.resolveCanInviteForSession(sess)
	td.Section = "my_account"

	if err := h.renderer.Render(w, "admin/my_account.html", td); err != nil {
		slog.Error("Erreur rendu my account page", "error", err)
		http.Error(w, h.tr(r, "common_server_error_page", "Erreur serveur : impossible de charger la page"), http.StatusInternalServerError)
	}
}

func (h *AdminHandler) ensureUserRowForSession(sess *session.Payload) error {
	if sess == nil {
		return fmt.Errorf("session absente")
	}

	authID := strings.TrimSpace(sess.AuthentikID)
	if authID == "" {
		authID = strings.TrimSpace(sess.UserID)
	}
	if authID == "" {
		return fmt.Errorf("session sans authentik_id")
	}

	var userID int64
	err := h.db.QueryRow(`SELECT id FROM users WHERE authentik_id = ?`, authID).Scan(&userID)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}

	err = h.db.QueryRow(`SELECT id FROM users WHERE LOWER(username) = LOWER(?)`, strings.TrimSpace(sess.Username)).Scan(&userID)
	if err == nil {
		_, upErr := h.db.Exec(
			`UPDATE users
			 SET authentik_id = ?, is_active = TRUE, can_invite = ?, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ?`,
			authID,
			sess.IsAdmin,
			userID,
		)
		if upErr != nil {
			slog.Error("Erreur mise a jour profil session (Authentik path)", "username", sess.Username, "error", upErr)
		}
		return upErr
	}
	if err != sql.ErrNoRows {
		return err
	}

	insertQuery := `INSERT INTO users (authentik_id, username, email, is_active, can_invite) VALUES (?, ?, ?, TRUE, ?)`
	if h.db.IsSQLite() {
		insertQuery = `INSERT INTO users (authentik_id, username, email, is_active, can_invite) VALUES (?, ?, ?, 1, ?) ON CONFLICT(authentik_id) DO UPDATE SET username = excluded.username, updated_at = datetime('now')`
	}
	_, err = h.db.Exec(insertQuery, authID, sess.Username, sess.Email, sess.IsAdmin)
	if err != nil {
		slog.Error("Erreur insertion profil session Authentik", "username", sess.Username, "error", err)
	}
	return err
}

func (h *AdminHandler) resolveCanInviteForSession(sess *session.Payload) bool {
	if sess == nil {
		return false
	}
	if sess.IsAdmin {
		return true
	}

	_ = h.ensureUserRowForSession(sess)

	var canInvite bool
	var presetID sql.NullString
	err := h.db.QueryRow(
		`SELECT can_invite, preset_id
		 FROM users
		 WHERE jellyfin_id = ? OR username = ?
		 ORDER BY CASE WHEN jellyfin_id = ? THEN 0 ELSE 1 END
		 LIMIT 1`,
		sess.UserID,
		sess.Username,
		sess.UserID,
	).Scan(&canInvite, &presetID)

	if err != nil {
		return false
	}

	if presetID.Valid && presetID.String != "" {
		preset, _ := h.getJellyfinPresetByID(presetID.String)
		if preset != nil && preset.CanInvite {
			return true
		}
	}

	return canInvite
}

// GetMyAccount, UpdateMyAccount, GetMyInvitations, CreateMyInvitation, UpdateMyAccountAvatar, UpdateMyPassword, ResendEmailVerification have been moved to separate modular files.

func (h *AdminHandler) UsersPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.AdminUsername = sess.Username
	td.IsAdmin = true
	td.CanInvite = true
	td.AuthentikEnabled = h.db.IsAuthentikEnabled()
	td.Section = "users"
	if err := h.renderer.Render(w, "admin/users.html", td); err != nil {
		slog.Error("Erreur rendu users page", "error", err)
		http.Error(w, h.tr(r, "common_server_error_page", "Erreur serveur : impossible de charger la page"), http.StatusInternalServerError)
	}
}

func (h *AdminHandler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.AdminUsername = sess.Username
	td.IsAdmin = true
	td.CanInvite = true
	td.AuthentikEnabled = h.db.IsAuthentikEnabled()
	td.Section = "settings"
	if err := h.renderer.Render(w, "admin/settings.html", td); err != nil {
		slog.Error("Erreur rendu settings page", "error", err)
		http.Error(w, h.tr(r, "common_server_error_page", "Erreur serveur : impossible de charger la page"), http.StatusInternalServerError)
	}
}

func (h *AdminHandler) EmailTemplatesPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.AdminUsername = sess.Username
	td.IsAdmin = true
	td.CanInvite = true
	td.AuthentikEnabled = h.db.IsAuthentikEnabled()
	td.Section = "email_templates"
	if err := h.renderer.Render(w, "admin/email_templates.html", td); err != nil {
		slog.Error("Erreur rendu email templates page", "error", err)
		http.Error(w, h.tr(r, "common_server_error_page", "Erreur serveur : impossible de charger la page"), http.StatusInternalServerError)
	}
}

// InvitationsPage affiche la page de gestion des invitations.
func (h *AdminHandler) InvitationsPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.AdminUsername = sess.Username
	td.IsAdmin = sess.IsAdmin
	td.AuthentikEnabled = h.db.IsAuthentikEnabled()

	inviteCfg, err := h.db.GetInvitationProfileConfig()
	if err != nil {
		slog.Warn("Impossible de charger la config invitation pour la page", "error", err)
		inviteCfg = config.DefaultInvitationProfileConfig()
	}

	// links has been declared above
	inviteBaseURL := strings.TrimSpace(links.JellyGateURL)
	if inviteBaseURL == "" {
		inviteBaseURL = strings.TrimSpace(h.cfg.BaseURL)
	}
	if inviteBaseURL == "" {
		inviteBaseURL = requestBaseURL(r)
	}

	limits, err := h.resolveInvitationCreatorLimits(sess, inviteCfg)
	if err != nil {
		slog.Warn("Impossible de resoudre les limites d'invitation", "error", err)
		limits = invitationCreatorLimits{
			CanInvite:         sess != nil && sess.IsAdmin,
			AllowGrant:        inviteCfg.AllowInviterGrant,
			AllowUserExpiry:   inviteCfg.AllowInviterUserExpiry,
			AllowIgnoreLimits: sess != nil && sess.IsAdmin,
			MaxUses:           inviteCfg.InviterMaxUses,
			LinkValidityDays:  0,
			UserExpiryDays:    inviteCfg.DisableAfterDays,
			QuotaDay:          inviteCfg.InviterQuotaDay,
			QuotaMonth:        inviteCfg.InviterQuotaMonth,
		}
		if inviteCfg.InviterMaxLinkHours > 0 {
			limits.LinkValidityDays = (inviteCfg.InviterMaxLinkHours + 23) / 24
		}
	}

	td.Data["InviteBaseURL"] = strings.TrimRight(inviteBaseURL, "/")
	td.Data["InviteAllowInviterGrant"] = limits.AllowGrant
	td.Data["InviteAllowInviterUserExpiry"] = limits.AllowUserExpiry
	td.Data["InviteAllowIgnoreLimits"] = limits.AllowIgnoreLimits
	td.Data["InviteAllowLanguage"] = limits.AllowLanguage
	td.Data["InviteInviterMaxUses"] = limits.MaxUses
	td.Data["InviteLimitLinkValidityDays"] = limits.LinkValidityDays
	td.Data["InviteInviterMaxLinkHours"] = limits.LinkValidityDays * 24
	td.Data["InviteInviterQuotaDay"] = limits.QuotaDay
	td.Data["InviteInviterQuotaWeek"] = 0
	td.Data["InviteInviterQuotaMonth"] = limits.QuotaMonth
	td.Data["InviteDefaultDisableAfterDays"] = limits.UserExpiryDays
	td.Data["InviteTargetPresetID"] = strings.TrimSpace(limits.TargetPresetID)
	td.Data["InviteAllowedTargetPresetIDs"] = strings.Join(limits.AllowedTargetPresetIDs, ",")
	td.Data["InviteCanCreateTemporaryInvitations"] = limits.CanCreateTemporaryInvitations
	td.Data["InviteAllowedTemporaryPresetIDs"] = strings.Join(limits.AllowedTemporaryPresetIDs, ",")
	td.Data["InviteDefaultTemporaryDurationDays"] = limits.DefaultTemporaryDurationDays
	td.Data["InviteMaxTemporaryDurationDays"] = limits.MaxTemporaryDurationDays
	td.Data["InviteRequireEmail"] = inviteCfg.RequireEmail
	td.Data["DefaultLang"] = h.db.GetDefaultLang()

	if td.IsAdmin {
		td.CanInvite = true
	} else {
		td.CanInvite = limits.CanInvite

		if !td.CanInvite {
			http.Error(w, h.tr(r, "admin_invite_forbidden", "Accès interdit au programme de parrainage"), http.StatusForbidden)
			return
		}
	}
	td.Section = "invitations"

	if err := h.renderer.Render(w, "admin/invitations.html", td); err != nil {
		slog.Error("Erreur rendu invitations page", "error", err)
		http.Error(w, h.tr(r, "common_server_error_page", "Erreur serveur"), http.StatusInternalServerError)
	}
}

func (h *AdminHandler) LogsPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.AdminUsername = sess.Username
	td.IsAdmin = true
	td.CanInvite = true
	td.AuthentikEnabled = h.db.IsAuthentikEnabled()
	td.Section = "logs"
	if err := h.renderer.Render(w, "admin/logs.html", td); err != nil {
		slog.Error("Erreur rendu logs page", "error", err)
		http.Error(w, h.tr(r, "common_server_error_page", "Erreur serveur"), http.StatusInternalServerError)
	}
}

// — GET /admin/api/logs —————————————————————————————————————————————————————————————

// AuditLogResponse représente une ligne formatée du journal d'audit JSON.
type AuditLogResponse struct {
	ID        int64  `json:"id"`
	Action    string `json:"action"`
	Actor     string `json:"actor"`
	Target    string `json:"target"`
	RequestID string `json:"request_id,omitempty"`
	Details   string `json:"details"`
	CreatedAt string `json:"created_at"`
}

// — GET /admin/api/logs —————————————————————————————————————————————————————————————

// LogsAPI retourne le journal d'audit en JSON avec filtres avances et export CSV/JSON.
func (h *AdminHandler) LogsAPI(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page := 1
	limit := 50
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		page = p
	}
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 {
		limit = l
	}
	if limit > 500 {
		limit = 500
	}

	sortCol := strings.TrimSpace(q.Get("sort"))
	if sortCol == "" {
		sortCol = "created_at"
	}
	orderDir := strings.ToLower(strings.TrimSpace(q.Get("order")))
	if orderDir != "asc" && orderDir != "desc" {
		orderDir = "desc"
	}

	validCols := map[string]bool{"id": true, "action": true, "actor": true, "target": true, "created_at": true}
	if !validCols[sortCol] {
		sortCol = "created_at"
	}

	search := strings.TrimSpace(q.Get("search"))
	actionFilter := strings.TrimSpace(q.Get("action"))
	actorFilter := strings.TrimSpace(q.Get("actor"))
	targetFilter := strings.TrimSpace(q.Get("target"))
	resultFilter := strings.ToLower(strings.TrimSpace(q.Get("result")))
	requestIDFilter := strings.TrimSpace(q.Get("request_id"))
	fromDate := strings.TrimSpace(q.Get("from"))
	toDate := strings.TrimSpace(q.Get("to"))
	exportFmt := strings.ToLower(strings.TrimSpace(q.Get("export")))
	category := strings.ToLower(strings.TrimSpace(q.Get("category")))

	whereParts := make([]string, 0, 10)
	args := make([]interface{}, 0, 20)

	if category == "system" {
		whereParts = append(whereParts, "(action LIKE 'admin.login.%' OR action LIKE 'task.%' OR action LIKE 'backup.%' OR action LIKE 'settings.%' OR action LIKE 'automation.%' OR action = 'users.sync' OR actor = 'system' OR actor = 'scheduler')")
	} else if category == "app" {
		whereParts = append(whereParts, "( (action LIKE 'invite.%') OR (action LIKE 'user.%' AND action != 'users.sync') OR (action LIKE 'reset.%') OR (action LIKE 'email.%') OR (action LIKE 'auth.%' AND action != 'auth.login') )")
	}

	if search != "" {
		term := "%" + search + "%"
		whereParts = append(whereParts, "(action LIKE ? OR actor LIKE ? OR target LIKE ? OR details LIKE ?)")
		args = append(args, term, term, term, term)
	}
	if actionFilter != "" {
		whereParts = append(whereParts, "action LIKE ?")
		args = append(args, "%"+actionFilter+"%")
	}
	if actorFilter != "" {
		whereParts = append(whereParts, "actor LIKE ?")
		args = append(args, "%"+actorFilter+"%")
	}
	if targetFilter != "" {
		whereParts = append(whereParts, "target LIKE ?")
		args = append(args, "%"+targetFilter+"%")
	}
	if requestIDFilter != "" {
		whereParts = append(whereParts, "details LIKE ?")
		args = append(args, "%request_id="+requestIDFilter+"%")
	}
	if fromDate != "" {
		whereParts = append(whereParts, "created_at >= ?")
		args = append(args, fromDate+" 00:00:00")
	}
	if toDate != "" {
		whereParts = append(whereParts, "created_at <= ?")
		args = append(args, toDate+" 23:59:59")
	}
	if resultFilter != "" {
		switch resultFilter {
		case "success":
			whereParts = append(whereParts, "(action LIKE ? OR action LIKE ? OR action LIKE ?)")
			args = append(args, "%success%", "%created%", "%enabled%")
		case "failure", "error":
			whereParts = append(whereParts, "(action LIKE ? OR action LIKE ? OR action LIKE ?)")
			args = append(args, "%failed%", "%error%", "%denied%")
		}
	}

	whereClause := ""
	if len(whereParts) > 0 {
		whereClause = "WHERE " + strings.Join(whereParts, " AND ")
	}

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM audit_log %s", whereClause)
	if err := h.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		slog.Error("Erreur comptage des logs", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "db_error", "Database error")})
		return
	}

	baseQuery := fmt.Sprintf("SELECT id, action, actor, target, details, created_at FROM audit_log %s ORDER BY %s %s", whereClause, sortCol, orderDir)
	queryArgs := append([]interface{}{}, args...)
	query := baseQuery

	if exportFmt != "csv" && exportFmt != "json" {
		offset := (page - 1) * limit
		query += " LIMIT ? OFFSET ?"
		queryArgs = append(queryArgs, limit, offset)
	}

	rows, err := h.db.Query(query, queryArgs...)
	if err != nil {
		slog.Error("Erreur lecture table audit_log", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "db_error", "Database error")})
		return
	}
	defer rows.Close()

	type LogEntry struct {
		ID        int64  `json:"id"`
		Action    string `json:"action"`
		Actor     string `json:"actor"`
		Target    string `json:"target"`
		RequestID string `json:"request_id,omitempty"`
		Details   string `json:"details"`
		CreatedAt string `json:"created_at"`
	}

	logs := make([]LogEntry, 0)
	for rows.Next() {
		var l LogEntry
		if err := rows.Scan(&l.ID, &l.Action, &l.Actor, &l.Target, &l.Details, &l.CreatedAt); err != nil {
			continue
		}
		l.RequestID = extractRequestIDFromDetails(l.Details)
		logs = append(logs, l)
	}

	if exportFmt == "json" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\"audit_logs.json\"")
		_ = json.NewEncoder(w).Encode(logs)
		return
	}

	if exportFmt == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\"audit_logs.csv\"")
		csvWriter := csv.NewWriter(w)
		_ = csvWriter.Write([]string{"id", "created_at", "action", "actor", "target", "request_id", "details"})
		for _, l := range logs {
			_ = csvWriter.Write([]string{
				strconv.FormatInt(l.ID, 10),
				l.CreatedAt,
				l.Action,
				l.Actor,
				l.Target,
				l.RequestID,
				l.Details,
			})
		}
		csvWriter.Flush()
		return
	}

	totalPages := 1
	if total > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(limit)))
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"logs": logs,
			"meta": map[string]interface{}{
				"total":       total,
				"page":        page,
				"limit":       limit,
				"total_pages": totalPages,
			},
		},
	})
}

func extractRequestIDFromDetails(details string) string {
	details = strings.TrimSpace(details)
	if details == "" {
		return ""
	}
	idx := strings.Index(details, "request_id=")
	if idx < 0 {
		return ""
	}
	start := idx + len("request_id=")
	rest := details[start:]
	end := strings.IndexAny(rest, ",; }\\\"")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

// SetUserQuotaRequest payload pour l'ajustement administrateur des quotas.
type SetUserQuotaRequest struct {
	CustomQuota *int `json:"custom_quota"`
	BonusQuota  int  `json:"bonus_quota"`
	MalusQuota  int  `json:"malus_quota"`
}

// SetUserQuota permet à un administrateur d'ajuster les quotas d'un parrain.
func (h *AdminHandler) SetUserQuota(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	userID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || userID <= 0 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID d'utilisateur invalide"})
		return
	}

	var req SetUserQuotaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}

	if err := h.db.SetUserQuotaOverrides(r.Context(), userID, req.CustomQuota, req.BonusQuota, req.MalusQuota); err != nil {
		slog.Error("Erreur mise à jour quota utilisateur", "user_id", userID, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur base de données"})
		return
	}

	calc, err := h.db.CalculateUserQuota(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur calcul quota"})
		return
	}

	actor := "system"
	if sess := session.FromContext(r.Context()); sess != nil {
		actor = sess.Username
	}
	_ = h.db.LogAction("user.quota.updated", actor, idStr, fmt.Sprintf(`{"bonus":%d,"malus":%d}`, req.BonusQuota, req.MalusQuota))

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Quota utilisateur mis à jour", Data: calc})
}

// GetReferrals renvoie l'arbre complet de parrainage pour la vue administrateur.
func (h *AdminHandler) GetReferrals(w http.ResponseWriter, r *http.Request) {
	referrals, err := h.db.GetAllReferrals(r.Context())
	if err != nil {
		slog.Error("Erreur récupération liste des parrainages", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur base de données"})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: referrals})
}

// writeJSON écrit une réponse JSON avec le code HTTP donné.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		slog.Error("Erreur encodage JSON", "error", err)
	}
}
