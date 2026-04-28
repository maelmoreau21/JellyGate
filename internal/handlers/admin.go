// Package handlers Ã¢â‚¬â€� admin.go
//
// GÃƒÂ¨re les endpoints JSON du tableau de bord administrateur.
// Toutes les routes sont protÃƒÂ©gÃƒÂ©es par le middleware RequireAuth.
//
// Endpoints :
//   - GET    /admin/api/users         Ã¢â€ â€™ Liste des utilisateurs (fusion SQLite + Jellyfin)
//   - POST   /admin/api/users/{id}/toggle Ã¢â€ â€™ Active/dÃƒÂ©sactive un compte (AD + Jellyfin)
//   - DELETE /admin/api/users/{id}    Ã¢â€ â€™ Suppression totale (AD + Jellyfin + SQLite)
//
// Les erreurs partielles sont loggÃƒÂ©es mais ne bloquent pas les opÃƒÂ©rations
// restantes (ex: si l'utilisateur est dÃƒÂ©jÃƒÂ  supprimÃƒÂ© de l'AD, on continue
// avec Jellyfin et SQLite).
package handlers

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	netmail "net/mail"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	jgldap "github.com/maelmoreau21/JellyGate/internal/ldap"
	"github.com/maelmoreau21/JellyGate/internal/mail"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

// Ã¢â€�â‚¬Ã¢â€�â‚¬ Structures de rÃƒÂ©ponse JSON Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// UserResponse est la reprÃƒÂ©sentation JSON d'un utilisateur pour l'API admin.
type UserResponse struct {
	ID              int64  `json:"id"`
	JellyfinID      string `json:"jellyfin_id"`
	Username        string `json:"username"`
	Email           string `json:"email"`
	LDAPDN          string `json:"ldap_dn"`
	GroupName       string `json:"group_name"`
	PresetID        string `json:"preset_id"` // NEW
	InvitedBy       string `json:"invited_by"`
	IsActive        bool   `json:"is_active"`
	IsBanned        bool   `json:"is_banned"`
	CanInvite       bool   `json:"can_invite"`
	AccessExpiresAt string `json:"access_expires_at,omitempty"` // ISO 8601
	DeleteAt        string `json:"delete_at,omitempty"`
	ExpiryAction    string `json:"expiry_action"`
	DeleteAfterDays int    `json:"expiry_delete_after_days"`
	ExpiredAt       string `json:"expired_at,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`

	// Statuts temps rÃƒÂ©el depuis Jellyfin (enrichissement)
	JellyfinDisabled        bool   `json:"jellyfin_disabled"`
	JellyfinExists          bool   `json:"jellyfin_exists"`
	JellyfinPrimaryImageTag string `json:"jellyfin_primary_image_tag,omitempty"`
}

// APIResponse est l'enveloppe standard pour toutes les rÃƒÂ©ponses JSON.
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Errors  []string    `json:"errors,omitempty"`
}

// DashboardStatsResponse regroupe les donnÃ©es pour les graphiques et le moniteur de santÃ©.
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
	ID              int64
	Username        string
	Email           string
	PendingEmail    string
	EmailVerified   bool
	JellyfinID      string
	LDAPDN          string // FIXED
	GroupName       string
	PresetID        string // NEW
	ContactDiscord  string
	ContactTelegram string
	IsActive        bool
	CanInvite       bool
	PreferredLang   string
	NotifyExpiry    bool
	NotifyEvents    bool
	OptInEmail      bool
	OptInDiscord    bool
	OptInTelegram   bool
	ExpiryAction    string
	DeleteAfterDays int
	DeleteAt        sql.NullString
	ExpiredAt       sql.NullString
	AccessExpiresAt sql.NullString
	CreatedAt       sql.NullString
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
	PreferredLang        *string `json:"preferred_lang"`
	NotifyExpiryReminder *bool   `json:"notify_expiry_reminder"`
	NotifyAccountEvents  *bool   `json:"notify_account_events"`
	OptInEmail           *bool   `json:"opt_in_email"`
	OptInDiscord         *bool   `json:"opt_in_discord"`
	OptInTelegram        *bool   `json:"opt_in_telegram"`
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

// Ã¢â€�â‚¬Ã¢â€�â‚¬ Admin Handler Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// AdminHandler gÃƒÂ¨re les endpoints d'administration.
type AdminHandler struct {
	cfg      *config.Config
	db       *database.DB
	jfClient *jellyfin.Client
	ldClient *jgldap.Client
	mailer   *mail.Mailer
	renderer *render.Engine
}

// NewAdminHandler crÃƒÂ©e un nouveau handler d'administration.
func NewAdminHandler(cfg *config.Config, db *database.DB, jf *jellyfin.Client, ld *jgldap.Client, m *mail.Mailer, renderer *render.Engine) *AdminHandler {
	return &AdminHandler{
		cfg:      cfg,
		db:       db,
		jfClient: jf,
		ldClient: ld,
		mailer:   m,
		renderer: renderer,
	}
}

// SetLDAPClient remplace le client LDAP (rechargement ÃƒÂ  chaud).
func (h *AdminHandler) SetLDAPClient(ld *jgldap.Client) { h.ldClient = ld }

// SetMailer remplace le Mailer SMTP (rechargement ÃƒÂ  chaud).
func (h *AdminHandler) SetMailer(m *mail.Mailer) { h.mailer = m }

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

func normalizePhoneForLDAP(raw string) string {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return ""
	}

	replacer := strings.NewReplacer(" ", "", "-", "", ".", "", "(", "", ")", "")
	normalized := replacer.Replace(candidate)
	if strings.HasPrefix(normalized, "00") {
		normalized = "+" + normalized[2:]
	}

	if strings.HasPrefix(normalized, "+") {
		if len(normalized) < 7 || len(normalized) > 21 {
			return ""
		}
		for _, r := range normalized[1:] {
			if r < '0' || r > '9' {
				return ""
			}
		}
		return normalized
	}

	if len(normalized) < 6 || len(normalized) > 20 {
		return ""
	}
	for _, r := range normalized {
		if r < '0' || r > '9' {
			return ""
		}
	}

	return normalized
}

func (h *AdminHandler) syncUserContactToLDAP(userID int64) error {
	if userID <= 0 || h.ldClient == nil {
		return nil
	}

	var (
		ldapDN          sql.NullString
		email           sql.NullString
		contactTelegram sql.NullString
	)

	err := h.db.QueryRow(
		`SELECT ldap_dn, email, contact_telegram FROM users WHERE id = ?`,
		userID,
	).Scan(&ldapDN, &email, &contactTelegram)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lecture contact utilisateur: %w", err)
	}

	userDN := strings.TrimSpace(ldapDN.String)
	if userDN == "" {
		return nil
	}

	return h.ldClient.UpdateUserContact(
		userDN,
		strings.TrimSpace(email.String),
		normalizePhoneForLDAP(contactTelegram.String),
	)
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ Background Jobs Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// StartExpirationJob lance une routine en arriÃƒÂ¨re-plan qui vÃƒÂ©rifie pÃƒÂ©riodiquement
// si des comptes utilisateurs ont expirÃƒÂ©, afin de les dÃƒÂ©sactiver automatiquement.
func (h *AdminHandler) StartExpirationJob(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		// Faire une premiÃƒÂ¨re vÃƒÂ©rification au dÃƒÂ©marrage court
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

	emailCfg, _ := h.db.GetEmailTemplatesConfig()
	if emailCfg.DisableExpiryReminderEmails {
		return
	}
	reminderStages := []int{14, 7, 1}
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
		reminderRows.Close()
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
		deleteRows.Close()
	}

	// Rechercher les utilisateurs actifs dont access_expires_at est dÃƒÂ©passÃƒÂ©.
	rows, err := h.db.Query(`
		SELECT id, username, email, jellyfin_id, ldap_dn, access_expires_at, expiry_action, expiry_delete_after_days
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
		LDAPDN          string
		ExpiresAt       string
		ExpiryAction    string
		DeleteAfterDays int
	}

	usersToProcess := make([]expiredUser, 0)
	for rows.Next() {
		var u expiredUser
		var email, jfID, ldDN, expiresAt, expiryAction sql.NullString
		if err := rows.Scan(&u.ID, &u.Username, &email, &jfID, &ldDN, &expiresAt, &expiryAction, &u.DeleteAfterDays); err != nil {
			continue
		}
		u.Email = email.String
		u.JellyfinID = jfID.String
		u.LDAPDN = ldDN.String
		u.ExpiresAt = expiresAt.String
		u.ExpiryAction = normalizeExpiryAction(expiryAction.String)
		usersToProcess = append(usersToProcess, u)
	}
	rows.Close()

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

		if h.ldClient != nil && u.LDAPDN != "" {
			if err := h.ldClient.DisableUser(u.LDAPDN); err != nil {
				slog.Error("Erreur lors de la desactivation LDAP (Expiration)", "error", err)
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

	// Politique disable_then_delete: suppression differÃƒÂ©e apres la desactivation.
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

// Ã¢â€�â‚¬Ã¢â€�â‚¬ Pages HTML Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// DashboardPage affiche la page principale du tableau de bord.
func (h *AdminHandler) DashboardPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.AdminUsername = sess.Username
	td.IsAdmin = sess.IsAdmin
	td.CanInvite = h.resolveCanInviteForSession(sess)
	td.Section = "dashboard"

	if err := h.renderer.Render(w, "admin/dashboard.html", td); err != nil {
		slog.Error("Erreur rendu dashboard", "error", err)
		http.Error(w, "Erreur serveur : impossible de charger la page", http.StatusInternalServerError)
	}
}

// DashboardStats retourne les donnÃ©es pour les graphiques du dashboard (AJAX).
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

	// 3. SantÃ© des services
	health := map[string]bool{
		"database": true,
		"jellyfin": false,
		"ldap":     false,
	}

	// Test Jellyfin (lÃ©ger)
	if h.jfClient != nil {
		if _, err := h.jfClient.GetPublicSystemInfo(); err == nil {
			health["jellyfin"] = true
		}
	}

	// Test LDAP (si activÃ©)
	ldapCfg, _ := h.db.GetLDAPConfig()
	if ldapCfg.Enabled {
		client := jgldap.New(ldapCfg)
		if err := client.TestConnection(); err == nil {
			health["ldap"] = true
		}
	} else {
		// Pas d'erreur si dÃ©sactivÃ©, on peut mettre true ou le retirer.
		health["ldap"] = true
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

// MyAccountPage affiche la page "Mon compte" pour l'utilisateur connectÃƒÂ©.
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
		http.Error(w, "Erreur serveur : impossible de charger la page", http.StatusInternalServerError)
	}
}

func (h *AdminHandler) ensureUserRowForSession(sess *session.Payload) error {
	if sess == nil {
		return fmt.Errorf("session absente")
	}

	if strings.TrimSpace(sess.UserID) == "" {
		return fmt.Errorf("session sans user id jellyfin")
	}

	var userID int64
	err := h.db.QueryRow(`SELECT id FROM users WHERE jellyfin_id = ?`, sess.UserID).Scan(&userID)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}

	// Cas LDAP-only: l'utilisateur peut exister en base avec username sans jellyfin_id.
	err = h.db.QueryRow(`SELECT id FROM users WHERE username = ?`, sess.Username).Scan(&userID)
	if err == nil {
		_, upErr := h.db.Exec(
			`UPDATE users
			 SET jellyfin_id = ?, is_active = TRUE, can_invite = ?, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ?`,
			sess.UserID,
			sess.IsAdmin,
			userID,
		)
		if upErr != nil {
			slog.Error("Erreur mise a jour profil session (LDAP path)", "username", sess.Username, "error", upErr)
		}
		return upErr
	}
	if err != sql.ErrNoRows {
		return err
	}

	_, err = h.db.Exec(
		`INSERT INTO users (jellyfin_id, username, is_active, can_invite)
			 VALUES (?, ?, TRUE, ?)
		 ON CONFLICT(jellyfin_id) DO UPDATE SET username = excluded.username, updated_at = CURRENT_TIMESTAMP`,
		sess.UserID,
		sess.Username,
		sess.IsAdmin,
	)
	if err != nil {
		slog.Error("Erreur insertion profil session (Default path)", "username", sess.Username, "error", err)
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

// GetMyAccount retourne les informations ÃƒÂ©ditables de l'utilisateur connectÃƒÂ©.
func (h *AdminHandler) GetMyAccount(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if err := h.ensureUserRowForSession(sess); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible de prÃƒÂ©parer le profil utilisateur"})
		return
	}

	var (
		id              int64
		email           sql.NullString
		pendingEmail    sql.NullString
		emailVerified   bool
		contactDiscord  sql.NullString
		contactTelegram sql.NullString
		preferredLang   string
		notifyExpiry    bool
		notifyEvents    bool
		optInEmail      bool
		optInDiscord    bool
		optInTelegram   bool
		accessExpiresAt sql.NullString
		createdAt       sql.NullString
	)

	err := h.db.QueryRow(
		`SELECT id, email, contact_discord, contact_telegram,
		        pending_email, email_verified,
		        preferred_lang, notify_expiry_reminder, notify_account_events,
		        opt_in_email, opt_in_discord, opt_in_telegram,
		        access_expires_at, created_at
		 FROM users WHERE jellyfin_id = ?`,
		sess.UserID,
	).Scan(
		&id,
		&email,
		&contactDiscord,
		&contactTelegram,
		&pendingEmail,
		&emailVerified,
		&preferredLang,
		&notifyExpiry,
		&notifyEvents,
		&optInEmail,
		&optInDiscord,
		&optInTelegram,
		&accessExpiresAt,
		&createdAt,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de lecture du profil"})
		return
	}

	if preferredLang == "" {
		preferredLang = h.db.GetDefaultLang()
	}

	var jfPrimaryImageTag string
	if jfUser, err := h.jfClient.GetUser(sess.UserID); err == nil && jfUser != nil {
		jfPrimaryImageTag = jfUser.PrimaryImageTag
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"id":                         id,
			"username":                   sess.Username,
			"jellyfin_primary_image_tag": jfPrimaryImageTag,
			"email":                      email.String,
			"pending_email":              pendingEmail.String,
			"email_verified":             emailVerified,
			"contact_discord":            contactDiscord.String,
			"contact_telegram":           contactTelegram.String,
			"preferred_lang":             preferredLang,
			"notify_expiry_reminder":     notifyExpiry,
			"notify_account_events":      notifyEvents,
			"opt_in_email":               optInEmail,
			"opt_in_discord":             optInDiscord,
			"opt_in_telegram":            optInTelegram,
			"is_admin":                   sess.IsAdmin,
			"access_expires_at":          accessExpiresAt.String,
			"can_invite":                 h.resolveCanInviteForSession(sess),
			"created_at":                 createdAt.String,
		},
	})
}

// UpdateMyAccount met ÃƒÂ  jour les prÃƒÂ©fÃƒÂ©rences et l'email de l'utilisateur connectÃƒÂ©.
func (h *AdminHandler) UpdateMyAccount(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if err := h.ensureUserRowForSession(sess); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible de prÃƒÂ©parer le profil utilisateur"})
		return
	}

	var req UpdateMyAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}

	var (
		userID          int64
		currentEmail    sql.NullString
		currentPending  sql.NullString
		emailVerified   bool
		currentDiscord  sql.NullString
		currentTelegram sql.NullString
		preferredLang   string
		notifyExpiry    bool
		notifyEvents    bool
		optInEmail      bool
		optInDiscord    bool
		optInTelegram   bool
	)
	err := h.db.QueryRow(
		`SELECT id, email, pending_email, email_verified, contact_discord, contact_telegram,
		        preferred_lang, notify_expiry_reminder, notify_account_events,
		        opt_in_email, opt_in_discord, opt_in_telegram
		 FROM users WHERE jellyfin_id = ?`,
		sess.UserID,
	).Scan(
		&userID,
		&currentEmail,
		&currentPending,
		&emailVerified,
		&currentDiscord,
		&currentTelegram,
		&preferredLang,
		&notifyExpiry,
		&notifyEvents,
		&optInEmail,
		&optInDiscord,
		&optInTelegram,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de lecture des prÃƒÂ©fÃƒÂ©rences"})
		return
	}

	newEmail := strings.TrimSpace(currentEmail.String)
	newPendingEmail := strings.TrimSpace(currentPending.String)
	newEmailVerified := emailVerified
	shouldSendVerification := false
	if req.Email != nil {
		requestedEmail := strings.TrimSpace(*req.Email)
		if requestedEmail != "" {
			if _, err := netmail.ParseAddress(requestedEmail); err != nil {
				writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Adresse email invalide"})
				return
			}
		}

		switch {
		case requestedEmail == "":
			newEmail = ""
			newPendingEmail = ""
			newEmailVerified = false
		case strings.EqualFold(requestedEmail, newEmail):
			newPendingEmail = ""
			if !emailVerified {
				shouldSendVerification = true
			}
		default:
			newPendingEmail = requestedEmail
			shouldSendVerification = true
		}
	}
	newDiscord := strings.TrimSpace(currentDiscord.String)
	if req.ContactDiscord != nil {
		newDiscord = strings.TrimSpace(*req.ContactDiscord)
	}
	newTelegram := strings.TrimSpace(currentTelegram.String)
	if req.ContactTelegram != nil {
		newTelegram = strings.TrimSpace(*req.ContactTelegram)
	}

	newPreferredLang := strings.TrimSpace(preferredLang)
	if req.PreferredLang != nil {
		candidate := config.NormalizeLanguageTag(*req.PreferredLang)
		if candidate != "" && !config.IsSupportedLanguage(candidate) {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Langue invalide (fr, en, de, es, it, nl, pl, pt-BR, ru, zh)"})
			return
		}
		newPreferredLang = candidate
	}

	newNotifyExpiry := notifyExpiry
	if req.NotifyExpiryReminder != nil {
		newNotifyExpiry = *req.NotifyExpiryReminder
	}

	newNotifyEvents := notifyEvents
	if req.NotifyAccountEvents != nil {
		newNotifyEvents = *req.NotifyAccountEvents
	}

	newOptInEmail := optInEmail
	if req.OptInEmail != nil {
		newOptInEmail = *req.OptInEmail
	}
	newOptInDiscord := optInDiscord
	if req.OptInDiscord != nil {
		newOptInDiscord = *req.OptInDiscord
	}
	newOptInTelegram := optInTelegram
	if req.OptInTelegram != nil {
		newOptInTelegram = *req.OptInTelegram
	}

	_, err = h.db.Exec(
		`UPDATE users
		 SET email = ?, pending_email = ?, email_verified = ?, contact_discord = ?, contact_telegram = ?,
		     preferred_lang = ?, notify_expiry_reminder = ?, notify_account_events = ?,
		     opt_in_email = ?, opt_in_discord = ?, opt_in_telegram = ?,
		     email_verification_sent_at = CASE WHEN ? THEN NULL ELSE email_verification_sent_at END,
		     updated_at = datetime('now')
		 WHERE jellyfin_id = ?`,
		newEmail,
		newPendingEmail,
		newEmailVerified,
		newDiscord,
		newTelegram,
		newPreferredLang,
		newNotifyExpiry,
		newNotifyEvents,
		newOptInEmail,
		newOptInDiscord,
		newOptInTelegram,
		req.Email != nil,
		sess.UserID,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de mise ÃƒÂ  jour des prÃƒÂ©fÃƒÂ©rences"})
		return
	}

	message := "Profil mis ÃƒÂ  jour"
	if shouldSendVerification {
		if err := sendEmailVerification(h.cfg, h.db, h.mailer, userID, true); err != nil {
			slog.Error("Erreur envoi verification email apres mise a jour profil", "user_id", userID, "error", err)
			message = "Profil mis ÃƒÂ  jour, mais l'email de vÃƒÂ©rification n'a pas pu ÃƒÂªtre envoyÃƒÂ©"
		} else {
			message = "Profil mis ÃƒÂ  jour, email de vÃƒÂ©rification envoyÃƒÂ©"
		}
	}

	if err := h.syncUserContactToLDAP(userID); err != nil {
		slog.Warn("Synchronisation LDAP du profil partielle", "user_id", userID, "error", err)
		message += " (synchronisation LDAP en attente)"
	}

	if req.PreferredLang != nil {
		if strings.TrimSpace(newPreferredLang) == "" {
			http.SetCookie(w, &http.Cookie{
				Name:     "lang",
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: false,
				Secure:   jgmw.RequestIsHTTPS(r, h.cfg.BaseURL),
				SameSite: http.SameSiteLaxMode,
			})
		} else {
			http.SetCookie(w, &http.Cookie{
				Name:     "lang",
				Value:    newPreferredLang,
				Path:     "/",
				MaxAge:   31536000,
				HttpOnly: false,
				Secure:   jgmw.RequestIsHTTPS(r, h.cfg.BaseURL),
				SameSite: http.SameSiteLaxMode,
			})
		}
	}

	_ = h.db.LogAction(
		"user.profile.updated",
		sess.Username,
		sess.Username,
		fmt.Sprintf(`{"preferred_lang":"%s","notify_expiry":%t,"notify_events":%t,"opt_in_email":%t,"opt_in_discord":%t,"opt_in_telegram":%t}`,
			newPreferredLang,
			newNotifyExpiry,
			newNotifyEvents,
			newOptInEmail,
			newOptInDiscord,
			newOptInTelegram,
		),
	)

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: message,
		Data: map[string]interface{}{
			"email":                  newEmail,
			"pending_email":          newPendingEmail,
			"email_verified":         newEmailVerified && newPendingEmail == "",
			"contact_discord":        newDiscord,
			"contact_telegram":       newTelegram,
			"preferred_lang":         newPreferredLang,
			"notify_expiry_reminder": newNotifyExpiry,
			"notify_account_events":  newNotifyEvents,
			"opt_in_email":           newOptInEmail,
			"opt_in_discord":         newOptInDiscord,
			"opt_in_telegram":        newOptInTelegram,
		},
	})
}

// GetMyInvitations retourne les invitations crÃ©Ã©es par l'utilisateur connectÃ©.
func (h *AdminHandler) GetMyInvitations(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	now := time.Now()

	inviteCfg, err := h.db.GetInvitationProfileConfig()
	if err != nil {
		inviteCfg = config.DefaultInvitationProfileConfig()
	}
	limits, err := h.resolveInvitationCreatorLimits(sess, inviteCfg)
	if err != nil {
		slog.Error("Erreur calcul limites de parrainage", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur base de donnees"})
		return
	}

	rows, err := h.db.Query(`
		SELECT id, code, max_uses, used_count, expires_at, created_at 
		FROM invitations 
		WHERE created_by = ? 
		ORDER BY created_at DESC`, sess.Username)
	if err != nil {
		slog.Error("Erreur lecture mes invitations", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur base de donnÃ©es"})
		return
	}
	defer rows.Close()

	var invs []InvitationResponse
	activeLinks := 0
	for rows.Next() {
		var i InvitationResponse
		var rawExpiresAt, rawCreatedAt interface{}
		if err := rows.Scan(&i.ID, &i.Code, &i.MaxUses, &i.UsedCount, &rawExpiresAt, &rawCreatedAt); err != nil {
			continue
		}
		i.ExpiresAt = anyToDateString(rawExpiresAt)
		i.CreatedAt = anyToDateString(rawCreatedAt)
		isExpired := false
		if strings.TrimSpace(i.ExpiresAt) != "" {
			if exp, parseErr := parseAccessExpiry(i.ExpiresAt); parseErr == nil {
				isExpired = !exp.After(now)
			}
		}
		if !isExpired && (i.MaxUses <= 0 || i.UsedCount < i.MaxUses) {
			activeLinks++
		}
		invs = append(invs, i)
	}

	todayCount, _ := h.countInvitationsCreatedSince(sess.Username, startOfLocalDay(now))
	monthCount, _ := h.countInvitationsCreatedSince(sess.Username, startOfLocalMonth(now))

	conversions := 0
	_ = h.db.QueryRow(`
		SELECT COUNT(u.id)
		FROM invitations i
		LEFT JOIN users u ON u.invited_by = i.code
		WHERE i.created_by = ?`, sess.Username,
	).Scan(&conversions)

	targetPresetName := ""
	if strings.TrimSpace(limits.TargetPresetID) != "" {
		if targetPreset, presetErr := h.getJellyfinPresetByID(strings.TrimSpace(limits.TargetPresetID)); presetErr == nil && targetPreset != nil {
			targetPresetName = strings.TrimSpace(targetPreset.Name)
		}
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"links": invs,
		"limits": map[string]interface{}{
			"can_invite":         limits.CanInvite,
			"max_uses":           limits.MaxUses,
			"link_validity_days": limits.LinkValidityDays,
			"quota_day":          limits.QuotaDay,
			"quota_month":        limits.QuotaMonth,
			"target_preset_id":   strings.TrimSpace(limits.TargetPresetID),
			"target_preset_name": targetPresetName,
		},
		"usage": map[string]interface{}{
			"today": todayCount,
			"month": monthCount,
		},
		"stats": map[string]interface{}{
			"total_links":  len(invs),
			"active_links": activeLinks,
			"conversions":  conversions,
		},
	}})
}

// CreateMyInvitation gÃ©nÃ¨re une invitation automatique (parrainage) basÃ©e sur le preset de l'utilisateur.
func (h *AdminHandler) CreateMyInvitation(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	now := time.Now()

	inviteCfg, err := h.db.GetInvitationProfileConfig()
	if err != nil {
		inviteCfg = config.DefaultInvitationProfileConfig()
	}

	limits, err := h.resolveInvitationCreatorLimits(sess, inviteCfg)
	if err != nil {
		slog.Error("Erreur calcul limites invitation utilisateur", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de lecture des limites"})
		return
	}

	if !limits.CanInvite {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Vous n'avez pas l'autorisation de parrainer"})
		return
	}

	if !limits.AllowIgnoreLimits {
		if limits.QuotaDay > 0 {
			todayCount, countErr := h.countInvitationsCreatedSince(sess.Username, startOfLocalDay(now))
			if countErr != nil {
				writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur verification quota journalier"})
				return
			}
			if todayCount >= limits.QuotaDay {
				writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: fmt.Sprintf("Quota journalier atteint (%d/%d)", todayCount, limits.QuotaDay)})
				return
			}
		}

		if limits.QuotaMonth > 0 {
			monthCount, countErr := h.countInvitationsCreatedSince(sess.Username, startOfLocalMonth(now))
			if countErr != nil {
				writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur verification quota mensuel"})
				return
			}
			if monthCount >= limits.QuotaMonth {
				writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: fmt.Sprintf("Quota mensuel atteint (%d/%d)", monthCount, limits.QuotaMonth)})
				return
			}
		}
	}

	targetPresetID := strings.TrimSpace(limits.TargetPresetID)
	if targetPresetID == "" && limits.SourcePreset != nil {
		targetPresetID = strings.TrimSpace(limits.SourcePreset.ID)
	}
	if targetPresetID == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Aucun profil de parrainage configurÃ© sur le serveur"})
		return
	}

	targetPreset, err := h.getJellyfinPresetByID(targetPresetID)
	if err != nil || targetPreset == nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur lecture profil de droit"})
		return
	}

	code, err := generateSecureToken(12)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible de generer un code d'invitation"})
		return
	}

	maxUses := limits.MaxUses
	if maxUses <= 0 {
		maxUses = targetPreset.InviteMaxUses
	}
	if maxUses <= 0 {
		maxUses = 1
	}

	validityDays := limits.LinkValidityDays
	if validityDays <= 0 {
		validityDays = presetInviteLinkValidityDays(targetPreset)
	}
	if validityDays <= 0 {
		validityDays = 30
	}

	expiresAt := now.AddDate(0, 0, validityDays)

	profile := jellyfin.InviteProfile{
		PresetID:                 targetPreset.ID,
		UserExpiryDays:           targetPreset.DisableAfterDays,
		EnableAllFolders:         targetPreset.EnableAllFolders,
		EnabledFolderIDs:         targetPreset.EnabledFolderIDs,
		EnableDownload:           targetPreset.EnableDownload,
		EnableRemoteAccess:       targetPreset.EnableRemoteAccess,
		CanInvite:                false,
		RequireEmail:             inviteCfg.RequireEmail,
		RequireEmailVerification: resolveInviteEmailVerificationRequirement(inviteCfg.EmailVerificationPolicy, inviteCfg.RequireEmailVerification, false, maxUses),
		DisableAfterDays:         targetPreset.DisableAfterDays,
		ExpiryAction:             normalizeExpiryAction(inviteCfg.ExpiryAction),
		DeleteAfterDays:          inviteCfg.DeleteAfterDays,
	}
	profileJSON, _ := json.Marshal(profile)

	_, err = h.db.Exec(`
		INSERT INTO invitations (code, label, max_uses, used_count, jellyfin_profile, expires_at, created_by)
		VALUES (?, ?, ?, 0, ?, ?, ?)`,
		code, "Parrainage de "+sess.Username, maxUses, string(profileJSON), expiresAt, sess.Username)

	if err != nil {
		slog.Error("Erreur creation invitation parrainage", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur base de donnÃ©es"})
		return
	}

	_ = h.db.LogAction("invite.created.sponsor", sess.Username, code, fmt.Sprintf(`{"target_preset":"%s","max_uses":%d,"validity_days":%d}`, targetPreset.ID, maxUses, validityDays))

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Lien de parrainage crÃ©Ã©", Data: map[string]interface{}{
		"code":               code,
		"max_uses":           maxUses,
		"expires_at":         expiresAt.Format(time.RFC3339),
		"target_preset_id":   targetPreset.ID,
		"target_preset_name": targetPreset.Name,
		"link_validity_days": validityDays,
		"invite_url":         strings.TrimRight(requestBaseURL(r), "/") + "/invite/" + code,
	}})
}

// UpdateMyAccountAvatar change la photo de profil Jellyfin de l'utilisateur connectÃ©.
func (h *AdminHandler) UpdateMyAccountAvatar(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if h.jfClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, APIResponse{Success: false, Message: "Service Jellyfin indisponible"})
		return
	}

	if err := r.ParseMultipartForm(5 * 1024 * 1024); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Fichier trop lourd ou invalide"})
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Image manquante"})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur lecture image"})
		return
	}

	contentType := header.Header.Get("Content-Type")

	if err := h.jfClient.SetUserImage(sess.UserID, contentType, data); err != nil {
		slog.Error("Erreur mise a jour avatar Jellyfin", "username", sess.Username, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur Jellyfin : " + err.Error()})
		return
	}

	_ = h.db.LogAction("user.avatar.updated", sess.Username, sess.Username, "Changement de photo de profil")

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Photo de profil mise Ã  jour"})
}

// UpdateMyPassword change le mot de passe de l'utilisateur sur Jellyfin.
func (h *AdminHandler) UpdateMyPassword(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if h.jfClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, APIResponse{Success: false, Message: "Service Jellyfin indisponible"})
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}

	if req.NewPassword == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Le nouveau mot de passe est obligatoire"})
		return
	}

	if err := h.jfClient.UpdateUserPassword(sess.UserID, req.CurrentPassword, req.NewPassword); err != nil {
		slog.Warn("Ã‰chec changement mot de passe", "username", sess.Username, "error", err)
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Ã‰chec : " + err.Error()})
		return
	}

	_ = h.db.LogAction("user.password.updated", sess.Username, sess.Username, "Changement de mot de passe")

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Mot de passe mis Ã  jour avec succÃ¨s"})
}

// ResendEmailVerification renvoie un code de vÃ©rification Ã  l'utilisateur connectÃ©.
func (h *AdminHandler) ResendEmailVerification(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if h.mailer == nil {
		writeJSON(w, http.StatusServiceUnavailable, APIResponse{Success: false, Message: "Service mail non configurÃ©"})
		return
	}

	var email, pendingEmail string
	var emailVerified bool
	var id int64
	err := h.db.QueryRow(`SELECT id, email, pending_email, email_verified FROM users WHERE jellyfin_id = ?`, sess.UserID).Scan(&id, &email, &pendingEmail, &emailVerified)
	if err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: "Utilisateur introuvable"})
		return
	}

	if emailVerified && pendingEmail == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Votre email est dÃ©jÃ  vÃ©rifiÃ©"})
		return
	}

	targetEmail := email
	usePending := false
	if pendingEmail != "" {
		targetEmail = pendingEmail
		usePending = true
	}

	if targetEmail == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Aucune adresse email configurÃ©e"})
		return
	}

	if err := sendEmailVerification(h.cfg, h.db, h.mailer, id, usePending); err != nil {
		slog.Error("Erreur renvoi verification email", "user_id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur lors de l'envoi de l'email"})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Email de vÃ©rification envoyÃ©"})
}

func (h *AdminHandler) UsersPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.AdminUsername = sess.Username
	td.IsAdmin = true
	td.CanInvite = true
	td.Section = "users"
	if err := h.renderer.Render(w, "admin/users.html", td); err != nil {
		slog.Error("Erreur rendu users page", "error", err)
		http.Error(w, "Erreur serveur : impossible de charger la page", http.StatusInternalServerError)
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
	td.Section = "settings"
	if err := h.renderer.Render(w, "admin/settings.html", td); err != nil {
		slog.Error("Erreur rendu settings page", "error", err)
		http.Error(w, "Erreur serveur : impossible de charger la page", http.StatusInternalServerError)
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
	td.Data["InviteRequireEmail"] = inviteCfg.RequireEmail
	td.Data["DefaultLang"] = h.db.GetDefaultLang()

	if td.IsAdmin {
		td.CanInvite = true
	} else {
		td.CanInvite = limits.CanInvite

		if !td.CanInvite {
			http.Error(w, "AccÃƒÂ¨s interdit au programme de parrainage", http.StatusForbidden)
			return
		}
	}
	td.Section = "invitations"

	if err := h.renderer.Render(w, "admin/invitations.html", td); err != nil {
		slog.Error("Erreur rendu invitations page", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
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
	td.Section = "logs"
	if err := h.renderer.Render(w, "admin/logs.html", td); err != nil {
		slog.Error("Erreur rendu logs page", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
	}
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ GET /admin/api/logs Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// AuditLogResponse reprÃƒÂ©sente une ligne formatÃƒÂ©e du journal d'audit JSON.
type AuditLogResponse struct {
	ID        int64  `json:"id"`
	Action    string `json:"action"`
	Actor     string `json:"actor"`
	Target    string `json:"target"`
	RequestID string `json:"request_id,omitempty"`
	Details   string `json:"details"`
	CreatedAt string `json:"created_at"`
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ GET /admin/api/logs Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

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
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur lecture base de donnees"})
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
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur traitement journaux"})
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
		totalPages = total / limit
		if total%limit != 0 {
			totalPages++
		}
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
	end := strings.IndexAny(rest, ",; }\"")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ POST /admin/api/users/sync Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// SyncJellyfinUsers synchronise manuellement les utilisateurs Jellyfin vers SQLite
func (h *AdminHandler) SyncJellyfinUsers(w http.ResponseWriter, r *http.Request) {
	jfUsers, err := h.jfClient.GetUsers()
	if err != nil {
		slog.Error("Erreur lors de la rÃƒÂ©cupÃƒÂ©ration des utilisateurs Jellyfin pour la sync", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de communication avec Jellyfin",
		})
		return
	}

	var addedCount int
	for _, ju := range jfUsers {
		// INSERT OR IGNORE dans SQLite
		res, err := h.db.Exec(`
			INSERT OR IGNORE INTO users (jellyfin_id, username, is_active)
			VALUES (?, ?, ?)
		`, ju.ID, ju.Name, !ju.Policy.IsDisabled)

		if err == nil {
			if affected, _ := res.RowsAffected(); affected > 0 {
				addedCount++
			}
		}
	}

	slog.Info("Synchronisation manuelle Jellyfin terminÃƒÂ©e", "users_added", addedCount)
	h.db.LogAction("users.sync", session.FromContext(r.Context()).Username, "all",
		fmt.Sprintf("Synchronisation manuelle dÃƒÂ©clenchÃƒÂ©e: %d nouveaux utilisateurs importÃƒÂ©s", addedCount))

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: fmt.Sprintf("Synchronisation terminÃƒÂ©e: %d nouveaux utilisateurs trouvÃƒÂ©s.", addedCount),
	})
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ GET /admin/api/users Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// ListUsers retourne la liste des utilisateurs avec pagination et recherche.
func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	q := r.URL.Query()

	page := 1
	limit := 25
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		page = p
	}
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 {
		limit = l
	}
	if limit > 500 {
		limit = 500
	}

	search := strings.TrimSpace(q.Get("search"))
	status := q.Get("status")
	invite := q.Get("invite")
	extra := q.Get("extra")
	includeJellyfin := true
	if raw := strings.ToLower(strings.TrimSpace(q.Get("include_jellyfin"))); raw == "0" || raw == "false" || raw == "no" {
		includeJellyfin = false
	}

	whereParts := make([]string, 0)
	args := make([]interface{}, 0)

	if search != "" {
		term := "%" + search + "%"
		whereParts = append(whereParts, "(username LIKE ? OR email LIKE ? OR group_name LIKE ?)")
		args = append(args, term, term, term)
	}

	if status == "active" {
		whereParts = append(whereParts, "is_active = 1 AND is_banned = 0")
	} else if status == "inactive" {
		whereParts = append(whereParts, "is_active = 0 AND is_banned = 0")
	} else if status == "banned" {
		whereParts = append(whereParts, "is_banned = 1")
	}

	if invite == "enabled" {
		whereParts = append(whereParts, "can_invite = 1")
	} else if invite == "disabled" {
		whereParts = append(whereParts, "can_invite = 0")
	}

	if extra == "with-email" {
		whereParts = append(whereParts, "email IS NOT NULL AND email != ''")
	} else if extra == "without-email" {
		whereParts = append(whereParts, "(email IS NULL OR email = '')")
	} else if extra == "expiry-active" {
		whereParts = append(whereParts, "access_expires_at IS NOT NULL AND access_expires_at > CURRENT_TIMESTAMP")
	} else if extra == "expiry-expired" {
		whereParts = append(whereParts, "access_expires_at IS NOT NULL AND access_expires_at <= CURRENT_TIMESTAMP")
	} else if extra == "expiry-none" {
		whereParts = append(whereParts, "access_expires_at IS NULL")
	}
	// Note: Jellyfin filters (ok, disabled, missing) are harder to do purely in SQL
	// if we don't sync Jellyfin status to the 'users' table regularly.
	// For now, we'll keep search/status/invite/extra in backend.
	// If jellyfinFilter is set, we might need a different approach or just accept it's less efficient.
	// To keep it simple and fast, I'll stick to these for now.

	whereClause := ""
	if len(whereParts) > 0 {
		whereClause = "WHERE " + strings.Join(whereParts, " AND ")
	}

	// 1. Compter le total (filtrÃ©)
	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM users %s", whereClause)
	if err := h.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		slog.Error("Erreur comptage des utilisateurs", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur lecture base de donnees"})
		return
	}

	// 1b. Statistiques globales pour l'aperÃ§u
	var totalGlobal, invitersCount, expiringCount int
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&totalGlobal)
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM users WHERE can_invite = 1`).Scan(&invitersCount)
	// Expiring: a une date d'expiration dans le futur (ou trÃ¨s proche)
	// On utilise une approche compatible SQLite/Postgres via prepareQuery si possible,
	// mais ici on va rester simple pour l'instant.
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_active = 1 AND access_expires_at IS NOT NULL AND access_expires_at > CURRENT_TIMESTAMP`).Scan(&expiringCount)

	// 2. RÃ©cupÃ©rer les donnÃ©es paginÃ©es
	offset := (page - 1) * limit
	query := fmt.Sprintf(`SELECT id, jellyfin_id, username, email, ldap_dn, invited_by,
		        group_name, preset_id, is_active, is_banned, can_invite, access_expires_at, delete_at,
		        expiry_action, expiry_delete_after_days, expired_at,
		        created_at, updated_at
		 FROM users %s ORDER BY created_at DESC LIMIT ? OFFSET ?`, whereClause)

	queryArgs := append(args, limit, offset)
	rows, err := h.db.Query(query, queryArgs...)
	if err != nil {
		slog.Error("Erreur lecture des utilisateurs", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de lecture de la base de donnÃ©es",
		})
		return
	}
	defer rows.Close()

	var users []UserResponse
	for rows.Next() {
		var u UserResponse
		var jellyfinID, email, ldapDN, invitedBy, groupName, presetID sql.NullString
		var accessExpiresAt, deleteAt, expiryAction, expiredAt, createdAt, updatedAt sql.NullString
		var deleteAfterDays sql.NullInt64

		err := rows.Scan(
			&u.ID, &jellyfinID, &u.Username, &email, &ldapDN, &invitedBy, &groupName, &presetID,
			&u.IsActive, &u.IsBanned, &u.CanInvite, &accessExpiresAt, &deleteAt,
			&expiryAction, &deleteAfterDays, &expiredAt,
			&createdAt, &updatedAt,
		)
		if err != nil {
			slog.Error("Erreur scan utilisateur", "error", err)
			continue
		}

		u.JellyfinID = jellyfinID.String
		u.Email = email.String
		u.LDAPDN = ldapDN.String
		u.InvitedBy = invitedBy.String
		u.GroupName = groupName.String
		u.PresetID = presetID.String
		u.AccessExpiresAt = accessExpiresAt.String
		u.DeleteAt = deleteAt.String
		u.ExpiryAction = normalizeExpiryAction(expiryAction.String)
		if deleteAfterDays.Valid {
			u.DeleteAfterDays = int(deleteAfterDays.Int64)
		}
		u.ExpiredAt = expiredAt.String
		u.CreatedAt = createdAt.String
		u.UpdatedAt = updatedAt.String

		users = append(users, u)
	}

	// 3. Enrichir avec Jellyfin (uniquement pour les IDs de cette page)
	if includeJellyfin && h.jfClient != nil && len(users) > 0 {
		jfIDs := make([]string, 0, len(users))
		for _, u := range users {
			if u.JellyfinID != "" {
				jfIDs = append(jfIDs, u.JellyfinID)
			}
		}

		if len(jfIDs) > 0 {
			jfUsers, err := h.jfClient.GetUsersBatch(jfIDs)
			if err == nil {
				jfIndex := make(map[string]*jellyfin.User, len(jfUsers))
				for i := range jfUsers {
					jfIndex[jfUsers[i].ID] = &jfUsers[i]
				}
				for i := range users {
					if jfUser, ok := jfIndex[users[i].JellyfinID]; ok {
						users[i].JellyfinExists = true
						users[i].JellyfinDisabled = jfUser.Policy.IsDisabled
						users[i].JellyfinPrimaryImageTag = jfUser.PrimaryImageTag
					}
				}
			}
		}
	}

	totalPages := 1
	if total > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(limit)))
	}

	slog.Info("Liste des utilisateurs renvoyee", "admin", sess.Username, "count", len(users), "total", total)

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"users": users,
			"meta": map[string]interface{}{
				"total":          total,
				"total_global":   totalGlobal,
				"inviters_count": invitersCount,
				"expiring_count": expiringCount,
				"page":           page,
				"limit":          limit,
				"total_pages":    totalPages,
			},
		},
	})
}

// UserAvatar sert l'image de profil d'un utilisateur Jellyfin.
func (h *AdminHandler) UserAvatar(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || userID <= 0 {
		http.Error(w, "ID invalide", http.StatusBadRequest)
		return
	}

	// 1. RÃ©cupÃ©rer le JellyfinID depuis la base
	var jfID string
	err = h.db.QueryRow(`SELECT jellyfin_id FROM users WHERE id = ?`, userID).Scan(&jfID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Utilisateur introuvable", http.StatusNotFound)
		} else {
			http.Error(w, "Base de donnÃ©es inaccessible", http.StatusInternalServerError)
		}
		return
	}

	if jfID == "" {
		http.Error(w, "Pas de lien Jellyfin", http.StatusNotFound)
		return
	}

	// 2. RÃ©cupÃ©rer l'image via le client Jellyfin
	data, contentType, err := h.jfClient.GetUserImage(jfID)
	if err != nil {
		slog.Warn("Avatar: erreur rÃ©cupÃ©ration Jellyfin", "user_id", userID, "jf_id", jfID, "error", err)
		http.Error(w, "Image non disponible", http.StatusNotFound)
		return
	}

	// 3. Servir l'image avec cache
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400") // 24h
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// UserTimeline retourne l'historique principal d'un utilisateur (audit + jalons internes).
func (h *AdminHandler) UserTimeline(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || userID <= 0 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID utilisateur invalide"})
		return
	}

	rec, err := h.loadAdminUserByID(userID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: "Utilisateur introuvable"})
		return
	}
	if err != nil {
		slog.Error("Erreur chargement utilisateur timeline", "user_id", userID, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur serveur"})
		return
	}

	idTarget := strconv.FormatInt(rec.ID, 10)
	rows, err := h.db.Query(
		`SELECT action, actor, target, details, created_at
		 FROM audit_log
		 WHERE target = ?
		    OR (? <> '' AND target = ?)
		    OR target = ?
		    OR actor = ?
		 ORDER BY created_at DESC
		 LIMIT 200`,
		rec.Username,
		rec.JellyfinID,
		rec.JellyfinID,
		idTarget,
		rec.Username,
	)
	if err != nil {
		slog.Error("Erreur lecture timeline", "user_id", userID, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de lecture de la timeline"})
		return
	}
	defer rows.Close()

	events := make([]UserTimelineEvent, 0, 32)
	for rows.Next() {
		var action, actor, target, details, createdAt sql.NullString
		if err := rows.Scan(&action, &actor, &target, &details, &createdAt); err != nil {
			continue
		}

		if !isUserTimelineAction(action.String, actor.String, target.String, rec.Username, rec.JellyfinID, idTarget) {
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

	sort.Slice(events, func(i, j int) bool {
		return timelineSortKey(events[i].At).After(timelineSortKey(events[j].At))
	})

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: events})
}

func (h *AdminHandler) loadAdminUserByID(userID int64) (*adminUserRecord, error) {
	var rec adminUserRecord
	var email, jellyfinID, ldapDN, groupName, presetID, discordContact, telegramContact sql.NullString

	err := h.db.QueryRow(
		`SELECT id, username, email, jellyfin_id, ldap_dn, group_name, preset_id, is_active, can_invite,
		        contact_discord, contact_telegram,
		        preferred_lang, notify_expiry_reminder, notify_account_events,
		        opt_in_email, opt_in_discord, opt_in_telegram,
		        expiry_action, expiry_delete_after_days, expired_at,
		        access_expires_at, delete_at, created_at
		 FROM users WHERE id = ?`,
		userID,
	).Scan(
		&rec.ID,
		&rec.Username,
		&email,
		&jellyfinID,
		&ldapDN,
		&groupName,
		&presetID,
		&rec.IsActive,
		&rec.CanInvite,
		&discordContact,
		&telegramContact,
		&rec.PreferredLang,
		&rec.NotifyExpiry,
		&rec.NotifyEvents,
		&rec.OptInEmail,
		&rec.OptInDiscord,
		&rec.OptInTelegram,
		&rec.ExpiryAction,
		&rec.DeleteAfterDays,
		&rec.ExpiredAt,
		&rec.AccessExpiresAt,
		&rec.DeleteAt,
		&rec.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	rec.Email = email.String
	rec.JellyfinID = jellyfinID.String
	rec.LDAPDN = ldapDN.String
	rec.GroupName = groupName.String
	rec.PresetID = presetID.String
	rec.ContactDiscord = discordContact.String
	rec.ContactTelegram = telegramContact.String

	return &rec, nil
}

func parseAccessExpiry(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("date d'expiration vide")
	}

	formats := []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04:05"}
	for _, f := range formats {
		if t, err := time.Parse(f, raw); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("format de date invalide")
}

func isUserTimelineAction(action, actor, target, username, jellyfinID, idTarget string) bool {
	action = strings.TrimSpace(action)
	if action == "" {
		return false
	}

	if strings.HasPrefix(action, "user.") || strings.HasPrefix(action, "invite.") || strings.HasPrefix(action, "reset.") {
		return true
	}

	if strings.HasPrefix(action, "admin.login.") && strings.TrimSpace(actor) == strings.TrimSpace(username) {
		return true
	}

	target = strings.TrimSpace(target)
	if target == strings.TrimSpace(username) || target == strings.TrimSpace(idTarget) {
		return true
	}
	if jellyfinID != "" && target == strings.TrimSpace(jellyfinID) {
		return true
	}

	return false
}

func describeTimelineAction(action, actor, target, details string) string {
	switch action {
	case "user.created":
		return "Compte cree"
	case "user.deleted", "user.bulk.delete":
		return "Compte supprime"
	case "user.toggled":
		return "Statut du compte modifie"
	case "user.access_extended", "user.bulk.expiry":
		return "Expiration du compte mise a jour"
	case "user.profile.updated":
		return "Profil utilisateur mis a jour"
	case "user.password.updated":
		return "Mot de passe modifie"
	case "user.avatar.updated":
		return "Photo de profil modifiee"
	case "invite.created", "invite.created.sponsor":
		return "Lien d'invitation cree"
	case "invite.deleted":
		return "Lien d'invitation supprime"
	case "invite.welcome_email.sent":
		return "Email de bienvenue envoye"
	case "invite.welcome_email.failed":
		return "Echec de l'envoi de l'email de bienvenue"
	case "reset.email.sent":
		return "Email de reinitialisation envoye"
	case "reset.email.failed":
		return "Echec de l'envoi de l'email de reinitialisation"
	case "reset.requested":
		return "Demande de reinitialisation (Email envoye)"
	case "reset.sent.admin":
		return "Lien de reinitialisation envoye par un admin"
	case "user.email_verification.sent":
		return "Email de verification envoye"
	case "user.email_verified":
		return "Email verifie avec succes"
	case "reset.completed", "reset.success":
		return "Mot de passe reinitialise"
	case "invite.used":
		return "Inscription realisee via invitation"
	}

	text := strings.TrimSpace(action)
	if strings.TrimSpace(actor) != "" {
		text = text + " par " + strings.TrimSpace(actor)
	}
	if strings.TrimSpace(target) != "" {
		text = text + " (cible: " + strings.TrimSpace(target) + ")"
	}
	if strings.TrimSpace(details) != "" {
		text = text + " - " + strings.TrimSpace(details)
	}

	return text
}

func timelineCategory(action string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	switch {
	case strings.HasPrefix(action, "invite."):
		return "invitation"
	case strings.HasPrefix(action, "reset."):
		return "password"
	case strings.Contains(action, "email") || strings.Contains(action, "verification"):
		return "email"
	case strings.HasPrefix(action, "admin.login"):
		return "security"
	case strings.HasPrefix(action, "automation."):
		return "automation"
	case strings.HasPrefix(action, "user."):
		return "account"
	default:
		return "general"
	}
}

func timelineSeverity(action, details string) string {
	text := strings.ToLower(strings.TrimSpace(action + " " + details))
	if strings.Contains(text, "failed") || strings.Contains(text, "error") || strings.Contains(text, "echec") {
		return "error"
	}
	if strings.Contains(text, "delete") || strings.Contains(text, "expired") || strings.Contains(text, "disabled") || strings.Contains(text, "banned") {
		return "warning"
	}
	if strings.Contains(text, "created") || strings.Contains(text, "success") || strings.Contains(text, "sent") || strings.Contains(text, "verified") || strings.Contains(text, "updated") {
		return "success"
	}
	return "info"
}

func normalizeTimelineAt(raw string) string {
	t := timelineSortKey(raw)
	if t.IsZero() {
		return strings.TrimSpace(raw)
	}
	return t.Format(time.RFC3339)
}

func timelineSortKey(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}

	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, raw); err == nil {
			return t
		}
	}

	return time.Time{}
}

func (h *AdminHandler) applyJellyfinPolicyPatch(jellyfinID string, patch *BulkJellyfinPolicyPatch) error {
	if patch == nil {
		return fmt.Errorf("aucun patch Jellyfin fourni")
	}
	if jellyfinID == "" {
		return fmt.Errorf("compte Jellyfin absent")
	}

	user, err := h.jfClient.GetUser(jellyfinID)
	if err != nil {
		return fmt.Errorf("impossible de lire la politique Jellyfin: %w", err)
	}

	policy := user.Policy
	if patch.EnableDownloads != nil {
		policy.EnableContentDownloading = *patch.EnableDownloads
	}
	if patch.EnableRemote != nil {
		policy.EnableRemoteAccess = *patch.EnableRemote
	}
	if patch.MaxActiveSession != nil {
		if *patch.MaxActiveSession < 0 {
			return fmt.Errorf("max_active_sessions doit ÃƒÂªtre >= 0")
		}
		policy.MaxActiveSessions = *patch.MaxActiveSession
	}
	if patch.BitrateLimit != nil {
		if *patch.BitrateLimit < 0 {
			return fmt.Errorf("remote_bitrate_limit doit ÃƒÂªtre >= 0")
		}
		policy.RemoteClientBitrateLimit = *patch.BitrateLimit
	}

	if err := h.jfClient.SetUserPolicy(jellyfinID, policy); err != nil {
		return fmt.Errorf("mise ÃƒÂ  jour de la politique Jellyfin: %w", err)
	}

	return nil
}

func (h *AdminHandler) getJellyfinPresetByID(presetID string) (*config.JellyfinPolicyPreset, error) {
	presetID = strings.TrimSpace(strings.ToLower(presetID))
	if presetID == "" {
		return nil, fmt.Errorf("preset manquant")
	}

	presets, err := h.db.GetJellyfinPolicyPresets()
	if err != nil {
		return nil, err
	}

	for i := range presets {
		if strings.TrimSpace(strings.ToLower(presets[i].ID)) == presetID {
			return &presets[i], nil
		}
	}

	return nil, fmt.Errorf("preset introuvable")
}

// applyPresetToUser rÃ©cupÃ¨re les rÃ©glages d'un preset et les applique Ã  Jellyfin pour l'utilisateur.
func (h *AdminHandler) applyPresetToUser(rec *adminUserRecord, presetID string) error {
	if rec == nil {
		return nil
	}
	presetID = strings.TrimSpace(presetID)
	if presetID == "" {
		return nil
	}

	preset, err := h.getJellyfinPresetByID(presetID)
	if err != nil {
		return fmt.Errorf("lecture preset %q: %w", presetID, err)
	}

	patch := &BulkJellyfinPolicyPatch{
		EnableDownloads:  &preset.EnableDownload,
		EnableRemote:     &preset.EnableRemoteAccess,
		MaxActiveSession: &preset.MaxSessions,
		BitrateLimit:     &preset.BitrateLimit,
	}

	if rec.JellyfinID != "" {
		if err := h.applyJellyfinPolicyPatch(rec.JellyfinID, patch); err != nil {
			return fmt.Errorf("application policy jellyfin: %w", err)
		}
	}

	// Persister le choix du preset dans SQLite
	_, err = h.db.Exec(`UPDATE users SET preset_id = ? WHERE id = ?`, preset.ID, rec.ID)
	if err != nil {
		return fmt.Errorf("maj preset_id sqlite: %w", err)
	}

	return nil
}

func (h *AdminHandler) applyGroupMappingToUser(rec *adminUserRecord, groupName string) error {
	if rec == nil {
		return nil
	}
	groupName = strings.TrimSpace(groupName)
	if groupName == "" {
		return nil
	}

	mappings, err := h.db.GetGroupPolicyMappings()
	if err != nil {
		return err
	}

	var mapping *config.GroupPolicyMapping
	for i := range mappings {
		if strings.EqualFold(strings.TrimSpace(mappings[i].GroupName), groupName) {
			mapping = &mappings[i]
			break
		}
	}
	if mapping == nil {
		return nil
	}

	// Application du preset associÃ© au groupe
	if err := h.applyPresetToUser(rec, mapping.PolicyPresetID); err != nil {
		return fmt.Errorf("applyPresetToUser via mapping: %w", err)
	}

	// Si c'est un groupe LDAP et que LDAP est activÃ©, on ajoute l'utilisateur au groupe LDAP s'il en manque
	if mapping.Source == "ldap" && h.ldClient != nil && strings.TrimSpace(rec.LDAPDN) != "" && strings.TrimSpace(mapping.LDAPGroupDN) != "" {
		if err := h.ldClient.AddUserToGroup(rec.LDAPDN, mapping.LDAPGroupDN); err != nil {
			return fmt.Errorf("assignation groupe ldap: %w", err)
		}
	}

	return nil
}

func (h *AdminHandler) setUserActiveState(rec *adminUserRecord, newActive bool, actor string) ([]string, error) {
	var partialErrors []string

	if h.ldClient != nil && rec.LDAPDN != "" {
		var err error
		if newActive {
			err = h.ldClient.EnableUser(rec.LDAPDN)
		} else {
			err = h.ldClient.DisableUser(rec.LDAPDN)
		}
		if err != nil {
			partialErrors = append(partialErrors, fmt.Sprintf("LDAP: %s", err.Error()))
		}
	}

	if rec.JellyfinID != "" {
		var err error
		if newActive {
			err = h.jfClient.EnableUser(rec.JellyfinID)
		} else {
			err = h.jfClient.DisableUser(rec.JellyfinID)
		}
		if err != nil {
			partialErrors = append(partialErrors, fmt.Sprintf("Jellyfin: %s", err.Error()))
		}
	}

	_, err := h.db.Exec(
		`UPDATE users SET is_active = ?, updated_at = datetime('now') WHERE id = ?`,
		newActive,
		rec.ID,
	)
	if err != nil {
		partialErrors = append(partialErrors, fmt.Sprintf("SQLite: %s", err.Error()))
		return partialErrors, err
	}

	rec.IsActive = newActive
	action := "user.enabled"
	if !newActive {
		action = "user.disabled"
	}
	_ = h.db.LogAction(action, actor, rec.Username, fmt.Sprintf(`{"user_id":%d,"errors":%d}`,
		rec.ID, len(partialErrors)))

	tplKey := "user_enabled"
	if !newActive {
		tplKey = "user_disabled"
	}
	if err := h.sendUserTemplateByKey(rec, tplKey, nil); err != nil {
		partialErrors = append(partialErrors, fmt.Sprintf("Email: %s", err.Error()))
	}

	return partialErrors, nil
}

func (h *AdminHandler) deleteUserRecord(rec *adminUserRecord, actor string) ([]string, error) {
	var partialErrors []string

	if h.ldClient != nil && rec.LDAPDN != "" {
		if err := h.ldClient.DeleteUser(rec.LDAPDN); err != nil {
			partialErrors = append(partialErrors, fmt.Sprintf("LDAP: %s", err.Error()))
		}
	}

	if rec.JellyfinID != "" {
		if err := h.jfClient.DeleteUser(rec.JellyfinID); err != nil {
			partialErrors = append(partialErrors, fmt.Sprintf("Jellyfin: %s", err.Error()))
		}
	}

	if err := h.sendUserTemplateByKey(rec, "user_deleted", nil); err != nil {
		partialErrors = append(partialErrors, fmt.Sprintf("Email: %s", err.Error()))
	}

	_, err := h.db.Exec(`DELETE FROM users WHERE id = ?`, rec.ID)
	if err != nil {
		partialErrors = append(partialErrors, fmt.Sprintf("SQLite: %s", err.Error()))
		return partialErrors, err
	}

	_ = h.db.LogAction("user.deleted", actor, rec.Username, fmt.Sprintf(`{"user_id":%d,"errors":%d}`,
		rec.ID, len(partialErrors)))

	return partialErrors, nil
}

func (h *AdminHandler) sendPasswordResetForUser(rec *adminUserRecord, actor string) error {
	if h.mailer == nil {
		return fmt.Errorf("SMTP non configurÃƒÂ©")
	}
	if strings.TrimSpace(rec.Email) == "" {
		return fmt.Errorf("utilisateur sans email")
	}

	token, err := generateSecureToken(resetTokenLength)
	if err != nil {
		return fmt.Errorf("gÃƒÂ©nÃƒÂ©ration du token: %w", err)
	}

	expiresAt := time.Now().Add(resetTokenExpiry)
	_, err = h.db.Exec(
		`INSERT INTO password_resets (user_id, code, used, expires_at)
		 VALUES (?, ?, FALSE, ?)`,
		rec.ID,
		token,
		expiresAt.Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return fmt.Errorf("insertion du token en base: %w", err)
	}

	links := resolvePortalLinks(h.cfg, h.db)
	publicBaseURL := strings.TrimRight(strings.TrimSpace(links.JellyGateURL), "/")
	if publicBaseURL == "" && h.cfg != nil {
		publicBaseURL = strings.TrimRight(strings.TrimSpace(h.cfg.BaseURL), "/")
	}
	resetURL := fmt.Sprintf("%s/reset/%s", publicBaseURL, token)
	mailCfg, usedLang, cfgErr := loadEmailTemplatesForLanguage(h.db, "", emailLanguageContext{
		PreferredLang: rec.PreferredLang,
		GroupName:     rec.GroupName,
	})
	if cfgErr != nil {
		mailCfg = config.DefaultEmailTemplatesForLanguage(usedLang)
	}
	tpl := mailCfg.PasswordReset
	if tpl == "" {
		tpl = "Bonjour {{.Username}},\n\nVoici votre lien de rÃƒÂ©initialisation de mot de passe : {{.ResetLink}}"
	}
	subject := firstNonEmpty(mailCfg.PasswordResetSubject, config.DefaultEmailTemplatesForLanguage(usedLang).PasswordResetSubject)

	data := map[string]string{
		"Username":           rec.Username,
		"ResetLink":          resetURL,
		"ResetURL":           resetURL,
		"ResetCode":          token,
		"ExpiresIn":          config.DefaultEmailPreviewDurationForLanguage(usedLang),
		"HelpURL":            publicBaseURL,
		"JellyGateURL":       publicBaseURL,
		"JellyfinURL":        links.JellyfinURL,
		"JellyfinServerName": links.JellyfinServerName,
		"JellyseerrURL":      links.JellyseerrURL,
		"JellyTrackURL":      links.JellyTrackURL,
	}

	if err := sendTemplateIfConfigured(h.mailer, rec.Email, subject, usedLang, "password_reset", tpl, mailCfg, data); err != nil {
		return fmt.Errorf("envoi de l'email: %w", err)
	}

	_ = h.db.LogAction("reset.sent.admin", actor, rec.Username, fmt.Sprintf(`{"user_id":%d}`, rec.ID))
	return nil
}

// CreateUser cree directement un compte utilisateur depuis l'admin.
func (h *AdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if sess == nil || !sess.IsAdmin {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Acces admin requis"})
		return
	}
	if h.jfClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, APIResponse{Success: false, Message: "Jellyfin indisponible"})
		return
	}

	var req CreateAdminUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)
	req.PolicyPresetID = strings.TrimSpace(req.PolicyPresetID)

	if req.Username == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Nom d'utilisateur requis"})
		return
	}
	if req.DisableAfterDays < 0 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Duree d'expiration invalide"})
		return
	}

	var alreadyExists int
	_ = h.db.QueryRow(`SELECT COUNT(1) FROM users WHERE lower(username) = lower(?)`, req.Username).Scan(&alreadyExists)
	if alreadyExists > 0 {
		writeJSON(w, http.StatusConflict, APIResponse{Success: false, Message: "Un utilisateur avec ce nom existe deja"})
		return
	}

	password := strings.TrimSpace(req.Password)
	generatedPassword := ""
	if password == "" {
		token, err := generateSecureToken(18)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible de generer un mot de passe temporaire"})
			return
		}
		password = token
		generatedPassword = token
	}

	inviteCfg, _ := h.db.GetInvitationProfileConfig()
	if req.PolicyPresetID == "" {
		req.PolicyPresetID = strings.TrimSpace(inviteCfg.PolicyPresetID)
	}

	var preset *config.JellyfinPolicyPreset
	if req.PolicyPresetID != "" {
		resolvedPreset, err := h.getJellyfinPresetByID(req.PolicyPresetID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Preset Jellyfin introuvable"})
			return
		}
		preset = resolvedPreset
		req.PolicyPresetID = resolvedPreset.ID
	}

	effectiveDisableAfterDays := req.DisableAfterDays
	if effectiveDisableAfterDays <= 0 && preset != nil && preset.DisableAfterDays > 0 {
		effectiveDisableAfterDays = preset.DisableAfterDays
	}

	var expiryAt time.Time
	if strings.TrimSpace(req.AccessExpiresAt) != "" {
		parsedExpiry, err := parseAccessExpiry(strings.TrimSpace(req.AccessExpiresAt))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Date d'expiration invalide"})
			return
		}
		expiryAt = parsedExpiry
	} else if effectiveDisableAfterDays > 0 {
		expiryAt = time.Now().AddDate(0, 0, effectiveDisableAfterDays)
	}

	created, err := h.jfClient.CreateUser(req.Username, password)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Creation Jellyfin echouee: " + err.Error()})
		return
	}

	if preset != nil {
		patch := &BulkJellyfinPolicyPatch{
			EnableDownloads:  &preset.EnableDownload,
			EnableRemote:     &preset.EnableRemoteAccess,
			MaxActiveSession: &preset.MaxSessions,
			BitrateLimit:     &preset.BitrateLimit,
		}
		if err := h.applyJellyfinPolicyPatch(created.ID, patch); err != nil {
			_ = h.jfClient.DeleteUser(created.ID)
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Application du preset impossible: " + err.Error()})
			return
		}
	}

	effectiveCanInvite := req.CanInvite
	if preset != nil && preset.CanInvite {
		effectiveCanInvite = true
	}

	storedExpiry := ""
	var expiryValue interface{}
	if !expiryAt.IsZero() {
		storedExpiry = expiryAt.Format("2006-01-02 15:04:05")
		expiryValue = storedExpiry
	}

	expiryAction := normalizeExpiryAction(inviteCfg.ExpiryAction)
	deleteAfterDays := inviteCfg.DeleteAfterDays
	if preset != nil {
		expiryAction = normalizeExpiryAction(preset.ExpiryAction)
		if preset.DeleteAfterDays >= 0 {
			deleteAfterDays = preset.DeleteAfterDays
		}
	}

	emailVerified := strings.TrimSpace(req.Email) == ""
	if _, err := h.db.Exec(
		`INSERT INTO users
			(jellyfin_id, username, email, email_verified, invited_by, is_active, can_invite, access_expires_at, preset_id, expiry_action, expiry_delete_after_days, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
		created.ID,
		req.Username,
		req.Email,
		emailVerified,
		"admin:"+sess.Username,
		effectiveCanInvite,
		expiryValue,
		req.PolicyPresetID,
		expiryAction,
		deleteAfterDays,
	); err != nil {
		_ = h.jfClient.DeleteUser(created.ID)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible d'enregistrer l'utilisateur"})
		return
	}

	var createdID int64
	_ = h.db.QueryRow(`SELECT id FROM users WHERE jellyfin_id = ?`, created.ID).Scan(&createdID)
	rec := &adminUserRecord{ID: createdID, Username: req.Username, Email: req.Email, JellyfinID: created.ID, CanInvite: effectiveCanInvite}
	if storedExpiry != "" {
		rec.AccessExpiresAt = sql.NullString{String: storedExpiry, Valid: true}
	}

	welcomeSent := false
	if req.SendWelcomeEmail && strings.TrimSpace(req.Email) != "" && h.mailer != nil {
		usedLang := h.db.GetDefaultLang()
		emailCfg, _, cfgErr := h.db.GetEmailTemplatesConfigForLang(usedLang)
		if cfgErr == nil && !emailCfg.DisableWelcomeEmail {
			defaults := config.DefaultEmailTemplatesForLanguage(usedLang)
			subject := firstNonEmpty(emailCfg.WelcomeSubject, defaults.WelcomeSubject)
			body := emailCfg.Welcome
			if strings.TrimSpace(body) == "" {
				body = defaults.Welcome
			}
			extra := map[string]string{}
			if !expiryAt.IsZero() {
				extra["ExpiryDate"] = emailTime(expiryAt)
			}
			if err := h.sendUserEventEmail(rec, subject, usedLang, "welcome", body, emailCfg, extra); err == nil {
				welcomeSent = true
			}
		}
	}

	_ = h.db.LogAction("user.created.admin", sess.Username, req.Username, fmt.Sprintf(`{"user_id":%d,"preset_id":"%s","can_invite":%t}`, createdID, req.PolicyPresetID, effectiveCanInvite))

	respData := map[string]interface{}{
		"id":                createdID,
		"username":          req.Username,
		"email":             req.Email,
		"jellyfin_id":       created.ID,
		"preset_id":         req.PolicyPresetID,
		"can_invite":        effectiveCanInvite,
		"access_expires_at": storedExpiry,
		"welcome_sent":      welcomeSent,
	}
	if generatedPassword != "" {
		respData["temporary_password"] = generatedPassword
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Utilisateur cree", Data: respData})
}

// UpdateUser met ÃƒÂ  jour les informations ÃƒÂ©ditables d'un utilisateur (email, parrainage, expiration).
func (h *AdminHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID utilisateur invalide"})
		return
	}

	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}

	rec, err := h.loadAdminUserByID(userID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: "Utilisateur introuvable"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de lecture utilisateur"})
		return
	}

	email := rec.Email
	if req.Email != nil {
		email = strings.TrimSpace(*req.Email)
	}

	canInvite := rec.CanInvite
	if req.CanInvite != nil {
		canInvite = *req.CanInvite
	}

	groupName := strings.TrimSpace(rec.GroupName)
	if req.GroupName != nil {
		groupName = strings.TrimSpace(*req.GroupName)
	}

	presetID := strings.TrimSpace(rec.PresetID)
	if req.PresetID != nil {
		presetID = strings.TrimSpace(*req.PresetID)
	}

	oldExpiry := ""
	if rec.AccessExpiresAt.Valid {
		oldExpiry = strings.TrimSpace(rec.AccessExpiresAt.String)
	}

	newExpiry := oldExpiry
	var accessExpiresAt interface{}
	if req.ClearExpiry {
		accessExpiresAt = nil
		newExpiry = ""
	} else if req.AccessExpiresAt != nil {
		raw := strings.TrimSpace(*req.AccessExpiresAt)
		if raw == "" {
			accessExpiresAt = nil
			newExpiry = ""
		} else {
			exp, err := parseAccessExpiry(raw)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Date d'expiration invalide"})
				return
			}
			accessExpiresAt = exp
			newExpiry = exp.Format("2006-01-02 15:04:05")
		}
	} else if rec.AccessExpiresAt.Valid {
		accessExpiresAt = rec.AccessExpiresAt.String
		newExpiry = strings.TrimSpace(rec.AccessExpiresAt.String)
	}

	_, err = h.db.Exec(
		`UPDATE users
		 SET email = ?, group_name = ?, preset_id = ?, can_invite = ?, access_expires_at = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		email,
		groupName,
		presetID,
		canInvite,
		accessExpiresAt,
		userID,
	)
	if err != nil {
		slog.Error("Erreur mise Ã  jour utilisateur", "id", userID, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de mise Ã  jour"})
		return
	}

	_ = h.db.LogAction("user.updated", sess.Username, rec.Username,
		fmt.Sprintf(`{"user_id":%d,"email":"%s","group_name":"%s","preset_id":"%s","can_invite":%t}`, userID, email, groupName, presetID, canInvite))

	// Application du preset si modifiÃ© (ou si le groupe est modifiÃ© via mapping)
	if req.PresetID != nil && presetID != "" {
		if err := h.applyPresetToUser(rec, presetID); err != nil {
			slog.Warn("Application preset echouee", "user", rec.Username, "preset", presetID, "error", err)
			_ = h.db.LogAction("user.preset.failed", sess.Username, rec.Username, err.Error())
		}
	} else if req.GroupName != nil && groupName != "" {
		rec.GroupName = groupName
		if err := h.applyGroupMappingToUser(rec, groupName); err != nil {
			slog.Warn("Application mapping groupe echouee", "user", rec.Username, "group", groupName, "error", err)
			_ = h.db.LogAction("user.group_mapping.failed", sess.Username, rec.Username, err.Error())
		}
	}

	if oldExpiry != newExpiry {
		rec.Email = email
		rec.AccessExpiresAt = sql.NullString{String: newExpiry, Valid: newExpiry != ""}
		if newExpiry != "" {
			if err := h.sendUserTemplateByKey(rec, "expiry_adjusted", map[string]string{"ExpiryDate": newExpiry}); err != nil {
				slog.Error("Erreur envoi email expiry_adjusted", "user", rec.Username, "error", err)
			}
		}
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Utilisateur mis Ã  jour",
		Data: map[string]interface{}{
			"id":                userID,
			"email":             email,
			"group_name":        groupName,
			"can_invite":        canInvite,
			"access_expires_at": accessExpiresAt,
		},
	})
}

// BanUser banni dÃ©finitvement un utilisateur (dÃ©sactivation + flag banni).
func (h *AdminHandler) BanUser(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID utilisateur invalide"})
		return
	}

	rec, err := h.loadAdminUserByID(userID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: "Utilisateur introuvable"})
		return
	}

	_, err = h.db.Exec(`UPDATE users SET is_active = 0, is_banned = 1, updated_at = datetime('now') WHERE id = ?`, userID)
	if err != nil {
		slog.Error("Erreur bannissement utilisateur", "id", userID, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de mise Ã  jour"})
		return
	}

	_ = h.db.LogAction("user.banned", sess.Username, rec.Username, fmt.Sprintf(`{"user_id":%d}`, userID))

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: fmt.Sprintf("Utilisateur %s banni", rec.Username)})
}

// ExtendAccess ajoute une durÃ©e d'accÃ¨s par dÃ©faut (30 jours) Ã  l'utilisateur.
func (h *AdminHandler) ExtendAccess(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID utilisateur invalide"})
		return
	}

	rec, err := h.loadAdminUserByID(userID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: "Utilisateur introuvable"})
		return
	}

	currentExpiry := time.Now()
	if rec.AccessExpiresAt.Valid {
		if t, err := time.Parse("2006-01-02 15:04:05", rec.AccessExpiresAt.String); err == nil {
			if t.After(currentExpiry) {
				currentExpiry = t
			}
		}
	}

	newExpiry := currentExpiry.AddDate(0, 0, 30)
	newExpiryStr := newExpiry.Format("2006-01-02 15:04:05")

	_, err = h.db.Exec(`UPDATE users SET access_expires_at = ?, updated_at = datetime('now') WHERE id = ?`, newExpiryStr, userID)
	if err != nil {
		slog.Error("Erreur prolongation utilisateur", "id", userID, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de mise Ã  jour"})
		return
	}

	_ = h.db.LogAction("user.access_extended", sess.Username, rec.Username, fmt.Sprintf(`{"user_id":%d,"new_expiry":"%s"}`, userID, newExpiryStr))

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: fmt.Sprintf("AccÃ¨s prolongÃ© pour %s jusqu'au %s", rec.Username, newExpiry.Format("02/01/2006")),
		Data: map[string]interface{}{
			"id":                userID,
			"access_expires_at": newExpiryStr,
		},
	})
}

// SendUserPasswordReset crÃƒÂ©e et envoie un lien de rÃƒÂ©initialisation ÃƒÂ  l'utilisateur ciblÃƒÂ©.
func (h *AdminHandler) SendUserPasswordReset(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID utilisateur invalide"})
		return
	}

	rec, err := h.loadAdminUserByID(userID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: "Utilisateur introuvable"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de lecture utilisateur"})
		return
	}

	if err := h.sendPasswordResetForUser(rec, sess.Username); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Email de rÃƒÂ©initialisation envoyÃƒÂ©"})
}

// BulkUsersAction applique une action de masse sur les utilisateurs sÃƒÂ©lectionnÃƒÂ©s.
func (h *AdminHandler) BulkUsersAction(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())

	var req BulkUsersActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}

	if len(req.UserIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Aucun utilisateur sÃƒÂ©lectionnÃƒÂ©"})
		return
	}

	action := strings.TrimSpace(req.Action)
	if action == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Action manquante"})
		return
	}
	previewOnly := req.Preview

	results := make([]map[string]interface{}, 0, len(req.UserIDs))
	successCount := 0

	for _, userID := range req.UserIDs {
		rec, err := h.loadAdminUserByID(userID)
		if err != nil {
			results = append(results, map[string]interface{}{
				"id":      userID,
				"success": false,
				"message": "Utilisateur introuvable",
			})
			continue
		}

		entry := map[string]interface{}{
			"id":       rec.ID,
			"username": rec.Username,
		}

		switch action {
		case "send_email":
			if previewOnly {
				subject := strings.TrimSpace(req.EmailSubject)
				body := strings.TrimSpace(req.EmailBody)
				if h.mailer == nil {
					entry["success"] = false
					entry["message"] = "SMTP non configure"
					break
				}
				if subject == "" || body == "" {
					entry["success"] = false
					entry["message"] = "Sujet/corps email requis"
					break
				}
				if strings.TrimSpace(rec.Email) == "" {
					entry["success"] = false
					entry["message"] = "Utilisateur sans email"
					break
				}
				entry["success"] = true
				entry["message"] = "Email sera envoye"
				entry["preview"] = true
				entry["impact"] = map[string]interface{}{"channel": "email", "subject": subject}
				break
			}

			if h.mailer == nil {
				entry["success"] = false
				entry["message"] = "SMTP non configurÃƒÂ©"
				break
			}
			subject := strings.TrimSpace(req.EmailSubject)
			body := strings.TrimSpace(req.EmailBody)
			if subject == "" || body == "" {
				entry["success"] = false
				entry["message"] = "Sujet/corps email requis"
				break
			}
			if strings.TrimSpace(rec.Email) == "" {
				entry["success"] = false
				entry["message"] = "Utilisateur sans email"
				break
			}

			err := h.mailer.SendTemplateString(rec.Email, subject, body, map[string]string{
				"Username": rec.Username,
				"Email":    rec.Email,
				"Actor":    sess.Username,
			})
			if err != nil {
				entry["success"] = false
				entry["message"] = err.Error()
				break
			}

			_ = h.db.LogAction("user.bulk.email", sess.Username, rec.Username, subject)
			entry["success"] = true
			entry["message"] = "Email envoyÃƒÂ©"

		case "jellyfin_policy":
			if previewOnly {
				if req.JellyfinPolicy == nil {
					entry["success"] = false
					entry["message"] = "Parametres Jellyfin manquants"
					break
				}
				entry["success"] = true
				entry["message"] = "Parametres Jellyfin seront mis a jour"
				entry["preview"] = true
				entry["impact"] = map[string]interface{}{"jellyfin_id": rec.JellyfinID}
				break
			}

			err := h.applyJellyfinPolicyPatch(rec.JellyfinID, req.JellyfinPolicy)
			if err != nil {
				entry["success"] = false
				entry["message"] = err.Error()
				break
			}

			_ = h.db.LogAction("user.bulk.jellyfin_policy", sess.Username, rec.Username, fmt.Sprintf(`{"user_id":%d}`, rec.ID))
			entry["success"] = true
			entry["message"] = "ParamÃƒÂ¨tres Jellyfin mis ÃƒÂ  jour"

		case "apply_preset":
			preset, err := h.getJellyfinPresetByID(req.PolicyPresetID)
			if err != nil {
				entry["success"] = false
				entry["message"] = err.Error()
				break
			}

			if previewOnly {
				entry["success"] = true
				entry["message"] = "Preset Jellyfin sera applique"
				entry["preview"] = true
				entry["impact"] = map[string]interface{}{"preset_id": preset.ID, "preset_name": preset.Name}
				break
			}

			patch := &BulkJellyfinPolicyPatch{
				EnableDownloads:  &preset.EnableDownload,
				EnableRemote:     &preset.EnableRemoteAccess,
				MaxActiveSession: &preset.MaxSessions,
				BitrateLimit:     &preset.BitrateLimit,
			}

			err = h.applyJellyfinPolicyPatch(rec.JellyfinID, patch)
			if err != nil {
				entry["success"] = false
				entry["message"] = err.Error()
				break
			}

			_ = h.db.LogAction("user.bulk.apply_preset", sess.Username, rec.Username, preset.ID)
			entry["success"] = true
			entry["message"] = "Preset Jellyfin appliquÃƒÂ©"

		case "set_parrainage":
			if req.CanInvite == nil {
				entry["success"] = false
				entry["message"] = "can_invite manquant"
				break
			}

			if previewOnly {
				entry["success"] = true
				entry["message"] = "Parrainage sera mis a jour"
				entry["preview"] = true
				entry["impact"] = map[string]interface{}{"can_invite": *req.CanInvite}
				break
			}

			_, err := h.db.Exec(`UPDATE users SET can_invite = ?, updated_at = datetime('now') WHERE id = ?`, *req.CanInvite, rec.ID)
			if err != nil {
				entry["success"] = false
				entry["message"] = "Erreur SQLite"
				break
			}

			_ = h.db.LogAction("user.bulk.can_invite", sess.Username, rec.Username, fmt.Sprintf(`{"can_invite":%t}`, *req.CanInvite))
			entry["success"] = true
			entry["message"] = "Parrainage mis ÃƒÂ  jour"

		case "set_expiry":
			var expiry interface{}
			if req.ClearExpiry {
				expiry = nil
			} else {
				if req.AccessExpiresAt == nil || strings.TrimSpace(*req.AccessExpiresAt) == "" {
					entry["success"] = false
					entry["message"] = "Date d'expiration manquante"
					break
				}
				exp, err := parseAccessExpiry(*req.AccessExpiresAt)
				if err != nil {
					entry["success"] = false
					entry["message"] = "Date d'expiration invalide"
					break
				}
				expiry = exp
			}

			if previewOnly {
				entry["success"] = true
				if req.ClearExpiry {
					entry["message"] = "Expiration sera supprimee"
					entry["impact"] = map[string]interface{}{"clear_expiry": true}
				} else {
					displayExpiry := ""
					if req.AccessExpiresAt != nil {
						displayExpiry = strings.TrimSpace(*req.AccessExpiresAt)
					}
					entry["message"] = "Expiration sera mise a jour"
					entry["impact"] = map[string]interface{}{"clear_expiry": false, "access_expires_at": displayExpiry}
				}
				entry["preview"] = true
				break
			}

			_, err := h.db.Exec(`UPDATE users SET access_expires_at = ?, updated_at = datetime('now') WHERE id = ?`, expiry, rec.ID)
			if err != nil {
				entry["success"] = false
				entry["message"] = "Erreur SQLite"
				break
			}

			_ = h.db.LogAction("user.bulk.expiry", sess.Username, rec.Username, "")
			if !req.ClearExpiry && req.AccessExpiresAt != nil && strings.TrimSpace(*req.AccessExpiresAt) != "" {
				if err := h.sendUserTemplateByKey(rec, "expiry_adjusted", map[string]string{"ExpiryDate": strings.TrimSpace(*req.AccessExpiresAt)}); err != nil {
					slog.Error("Erreur envoi email bulk expiry_adjusted", "user", rec.Username, "error", err)
					entry["success"] = true
					entry["message"] = "Expiration mise ÃƒÂ  jour (email non envoyÃƒÂ©)"
					break
				}
			}
			entry["success"] = true
			entry["message"] = "Expiration mise ÃƒÂ  jour"

		case "activate", "deactivate":
			newState := action == "activate"
			if previewOnly {
				entry["success"] = true
				if newState {
					entry["message"] = "Utilisateur sera active"
				} else {
					entry["message"] = "Utilisateur sera desactive"
				}
				entry["preview"] = true
				entry["impact"] = map[string]interface{}{"is_active": newState}
				break
			}

			partials, err := h.setUserActiveState(rec, newState, sess.Username)
			if err != nil {
				entry["success"] = false
				entry["message"] = strings.Join(partials, " | ")
				break
			}

			entry["success"] = true
			if len(partials) > 0 {
				entry["message"] = "Action appliquÃƒÂ©e avec erreurs partielles: " + strings.Join(partials, " | ")
			} else if newState {
				entry["message"] = "Utilisateur activÃƒÂ©"
			} else {
				entry["message"] = "Utilisateur dÃƒÂ©sactivÃƒÂ©"
			}

		case "send_password_reset":
			if previewOnly {
				if strings.TrimSpace(rec.Email) == "" {
					entry["success"] = false
					entry["message"] = "Utilisateur sans email"
					break
				}
				entry["success"] = true
				entry["message"] = "Lien de reinitialisation sera envoye"
				entry["preview"] = true
				entry["impact"] = map[string]interface{}{"channel": "email"}
				break
			}

			err := h.sendPasswordResetForUser(rec, sess.Username)
			if err != nil {
				entry["success"] = false
				entry["message"] = err.Error()
				break
			}

			entry["success"] = true
			entry["message"] = "Lien de rÃƒÂ©initialisation envoyÃƒÂ©"

		case "delete":
			if previewOnly {
				entry["success"] = true
				entry["message"] = "Utilisateur sera supprime"
				entry["preview"] = true
				entry["impact"] = map[string]interface{}{"delete": true}
				break
			}

			partials, err := h.deleteUserRecord(rec, sess.Username)
			if err != nil {
				entry["success"] = false
				entry["message"] = strings.Join(partials, " | ")
				break
			}

			entry["success"] = true
			if len(partials) > 0 {
				entry["message"] = "SupprimÃƒÂ© avec erreurs partielles: " + strings.Join(partials, " | ")
			} else {
				entry["message"] = "Utilisateur supprimÃƒÂ©"
			}

		default:
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Action de masse non supportÃƒÂ©e"})
			return
		}

		if ok, _ := entry["success"].(bool); ok {
			successCount++
		}
		results = append(results, entry)
	}

	message := fmt.Sprintf("Action de masse terminÃƒÂ©e: %d/%d succÃƒÂ¨s", successCount, len(req.UserIDs))
	if previewOnly {
		message = fmt.Sprintf("Preview action de masse: %d/%d impact(s) valides", successCount, len(req.UserIDs))
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: message,
		Data: map[string]interface{}{
			"total":   len(req.UserIDs),
			"success": successCount,
			"preview": previewOnly,
			"results": results,
		},
	})
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ POST /admin/api/users/{id}/toggle Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// ToggleUser active ou dÃƒÂ©sactive un utilisateur simultanÃƒÂ©ment dans l'AD
// et dans Jellyfin, puis met ÃƒÂ  jour le statut SQLite.
func (h *AdminHandler) ToggleUser(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "ID utilisateur invalide",
		})
		return
	}

	rec, err := h.loadAdminUserByID(userID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, APIResponse{
			Success: false,
			Message: "Utilisateur introuvable",
		})
		return
	}
	if err != nil {
		slog.Error("Erreur lecture utilisateur pour toggle", "id", userID, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de lecture de la base de donnÃƒÂ©es",
		})
		return
	}

	newActive := !rec.IsActive
	partialErrors, err := h.setUserActiveState(rec, newActive, sess.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur lors de la mise ÃƒÂ  jour du statut utilisateur",
			Errors:  partialErrors,
			Data: map[string]interface{}{
				"id":        rec.ID,
				"username":  rec.Username,
				"is_active": rec.IsActive,
			},
		})
		return
	}

	action := "activÃƒÂ©"
	if !newActive {
		action = "dÃƒÂ©sactivÃƒÂ©"
	}
	if len(partialErrors) > 0 {
		writeJSON(w, http.StatusOK, APIResponse{
			Success: true,
			Message: fmt.Sprintf("Utilisateur %s (avec %d erreur(s) partielle(s))", action, len(partialErrors)),
			Errors:  partialErrors,
			Data: map[string]interface{}{
				"id":        rec.ID,
				"username":  rec.Username,
				"is_active": newActive,
			},
		})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: fmt.Sprintf("Utilisateur %q %s avec succÃƒÂ¨s", rec.Username, action),
		Data: map[string]interface{}{
			"id":        rec.ID,
			"username":  rec.Username,
			"is_active": newActive,
		},
	})
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ POST /admin/api/users/{id}/invite-toggle Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// ToggleUserInvite active ou dÃƒÂ©sactive le droit de crÃƒÂ©er des invitations pour un utilisateur.
func (h *AdminHandler) ToggleUserInvite(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID utilisateur invalide"})
		return
	}

	var username string
	var canInvite bool
	err = h.db.QueryRow(`SELECT username, can_invite FROM users WHERE id = ?`, userID).
		Scan(&username, &canInvite)
	if err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: "Utilisateur introuvable"})
		return
	}

	newStatus := !canInvite
	_, err = h.db.Exec(`UPDATE users SET can_invite = ?, updated_at = datetime('now') WHERE id = ?`, newStatus, userID)
	if err != nil {
		slog.Error("Erreur modification can_invite", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur BDD"})
		return
	}

	actionTxt := "activÃƒÂ©"
	if !newStatus {
		actionTxt = "dÃƒÂ©sactivÃƒÂ©"
	}
	_ = h.db.LogAction("user.can_invite.toggle", sess.Username, username, fmt.Sprintf("Droit d'invitation %s", actionTxt))

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: fmt.Sprintf("Droit de parrainage %s pour %s", actionTxt, username),
		Data: map[string]interface{}{
			"id":         userID,
			"can_invite": newStatus,
		},
	})
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ POST /admin/api/users/me/password Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// ChangeMyPassword permet ÃƒÂ  l'utilisateur connectÃƒÂ© de modifier son propre mot de passe.
func (h *AdminHandler) ChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}

	if len(req.NewPassword) < 8 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Le nouveau mot de passe doit faire au moins 8 caractÃƒÂ¨res"})
		return
	}

	// RÃƒÂ©cupÃƒÂ©rer le DN LDAP depuis SQLite
	var ldapDN sql.NullString
	err := h.db.QueryRow(`SELECT ldap_dn FROM users WHERE jellyfin_id = ?`, sess.UserID).Scan(&ldapDN)
	if err != nil && err != sql.ErrNoRows {
		slog.Error("Erreur lecture ldap_dn pour changement MDP", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur base de donnÃƒÂ©es"})
		return
	}

	// Le changement s'effectue sur Jellyfin
	// (Note: l'API Jellyfin demande d'avoir l'ancien mot de passe pour les non-admins, ou un reset d'admin.
	// Ici nous utilisons un workaround: on authentifie via un webhook/API ? Non, change password endpoint direct.)
	// Pour simplifier dans l'exemple, on force le changement via le LDClient si dispo, puis le JfClient auth en tant qu'admin
	var partialErrors []string

	// 1. LDAP (Si activÃƒÂ©)
	if h.ldClient != nil && ldapDN.String != "" {
		if err := h.ldClient.ResetPassword(ldapDN.String, req.NewPassword); err != nil {
			partialErrors = append(partialErrors, fmt.Sprintf("LDAP: %v", err))
		}
	}

	// 2. Jellyfin (en passant par le token de l'APi admin configurÃƒÂ©e)
	if err := h.jfClient.ResetPassword(sess.UserID, req.NewPassword); err != nil {
		partialErrors = append(partialErrors, fmt.Sprintf("Jellyfin: %v", err))
	}

	if len(partialErrors) > 0 {
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Des erreurs sont survenues lors du changement",
			Errors:  partialErrors,
		})
		return
	}

	_ = h.db.LogAction("user.password.change", sess.Username, sess.Username, "L'utilisateur a changÃƒÂ© son mot de passe depuis le tableau de bord")

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Mot de passe modifiÃƒÂ© avec succÃƒÂ¨s",
	})
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ DELETE /admin/api/users/{id} Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// DeleteUser supprime un utilisateur de l'AD, de Jellyfin, puis de SQLite.
// Les erreurs partielles (ex: utilisateur dÃƒÂ©jÃƒÂ  supprimÃƒÂ© de l'AD) ne bloquent
// pas les suppressions restantes Ã¢â‚¬â€� tout est loggÃƒÂ©.
func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "ID utilisateur invalide",
		})
		return
	}

	rec, err := h.loadAdminUserByID(userID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, APIResponse{
			Success: false,
			Message: "Utilisateur introuvable",
		})
		return
	}
	if err != nil {
		slog.Error("Erreur lecture utilisateur pour suppression", "id", userID, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de lecture de la base de donnÃƒÂ©es",
		})
		return
	}

	partialErrors, err := h.deleteUserRecord(rec, sess.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur lors de la suppression de l'utilisateur",
			Errors:  partialErrors,
			Data: map[string]interface{}{
				"id":       rec.ID,
				"username": rec.Username,
				"deleted":  false,
			},
		})
		return
	}

	msg := fmt.Sprintf("Utilisateur %q supprimÃƒÂ© avec succÃƒÂ¨s", rec.Username)
	if len(partialErrors) > 0 {
		msg = fmt.Sprintf("Utilisateur %q supprimÃƒÂ© avec %d erreur(s) partielle(s)", rec.Username, len(partialErrors))
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: msg,
		Errors:  partialErrors,
		Data: map[string]interface{}{
			"id":       rec.ID,
			"username": rec.Username,
			"deleted":  true,
		},
	})
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ GET /admin/api/invitations Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// InvitationResponse reprÃƒÂ©sente une invitation formatÃƒÂ©e pour l'API JSON.
type InvitationResponse struct {
	ID              int64                  `json:"id"`
	Code            string                 `json:"code"`
	Label           string                 `json:"label"`
	PreferredLang   string                 `json:"preferred_lang"`
	MaxUses         int                    `json:"max_uses"`
	UsedCount       int                    `json:"used_count"`
	JellyfinProfile map[string]interface{} `json:"jellyfin_profile"`
	ExpiresAt       string                 `json:"expires_at,omitempty"`
	CreatedBy       string                 `json:"created_by"`
	CreatedAt       string                 `json:"created_at"`
}

func anyToDateString(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return ""
	case time.Time:
		return val.Format(time.RFC3339)
	case []byte:
		return strings.TrimSpace(string(val))
	case string:
		return strings.TrimSpace(val)
	default:
		return strings.TrimSpace(fmt.Sprint(val))
	}
}

func requestBaseURL(r *http.Request) string {
	if r == nil {
		return ""
	}

	scheme := "http"
	if xfProto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); xfProto != "" {
		scheme = strings.TrimSpace(strings.Split(xfProto, ",")[0])
	} else if jgmw.RequestIsHTTPS(r, "") {
		scheme = "https"
	}

	host := strings.TrimSpace(r.Host)
	if host == "" {
		return ""
	}

	return fmt.Sprintf("%s://%s", scheme, host)
}

func parseInvitationDateTimeInput(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("date vide")
	}

	if parsed, err := time.Parse("2006-01-02T15:04", trimmed); err == nil {
		return parsed, nil
	}
	if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return parsed, nil
	}

	return time.Time{}, fmt.Errorf("format de date invalide")
}

func startOfLocalDay(t time.Time) time.Time {
	lt := t.Local()
	y, m, d := lt.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, lt.Location())
}

func startOfLocalWeek(t time.Time) time.Time {
	day := startOfLocalDay(t)
	offset := (int(day.Weekday()) + 6) % 7 // Monday=0 ... Sunday=6
	return day.AddDate(0, 0, -offset)
}

func startOfLocalMonth(t time.Time) time.Time {
	lt := t.Local()
	y, m, _ := lt.Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, lt.Location())
}

type invitationCreatorLimits struct {
	CanInvite         bool
	AllowGrant        bool
	AllowUserExpiry   bool
	AllowIgnoreLimits bool
	AllowLanguage     bool

	MaxUses          int
	LinkValidityDays int
	UserExpiryDays   int
	QuotaDay         int
	QuotaMonth       int

	SourcePreset   *config.JellyfinPolicyPreset
	TargetPresetID string
}

func presetInviteQuotaMonth(preset *config.JellyfinPolicyPreset) int {
	if preset == nil {
		return 0
	}
	if preset.InviteQuotaMonth > 0 {
		return preset.InviteQuotaMonth
	}
	if preset.InviteQuota > 0 {
		return preset.InviteQuota
	}
	return 0
}

func presetInviteLinkValidityDays(preset *config.JellyfinPolicyPreset) int {
	if preset == nil {
		return 0
	}
	if preset.InviteLinkValidityDays > 0 {
		return preset.InviteLinkValidityDays
	}
	if preset.InviteMaxLinkHours > 0 {
		return (preset.InviteMaxLinkHours + 23) / 24
	}
	return 0
}

func (h *AdminHandler) resolveInvitationCreatorLimits(sess *session.Payload, inviteCfg config.InvitationProfileConfig) (invitationCreatorLimits, error) {
	limits := invitationCreatorLimits{
		AllowGrant:      inviteCfg.AllowInviterGrant,
		AllowUserExpiry: inviteCfg.AllowInviterUserExpiry,
		MaxUses:         inviteCfg.InviterMaxUses,
		UserExpiryDays:  inviteCfg.DisableAfterDays,
		QuotaDay:        inviteCfg.InviterQuotaDay,
		QuotaMonth:      inviteCfg.InviterQuotaMonth,
		TargetPresetID:  strings.TrimSpace(inviteCfg.PolicyPresetID),
	}

	if inviteCfg.InviterMaxLinkHours > 0 {
		limits.LinkValidityDays = (inviteCfg.InviterMaxLinkHours + 23) / 24
	}

	if sess == nil {
		return limits, nil
	}

	if sess.IsAdmin {
		limits.CanInvite = true
		limits.AllowGrant = true
		limits.AllowUserExpiry = true
		limits.AllowIgnoreLimits = true
		limits.AllowLanguage = true

		if limits.TargetPresetID != "" {
			if preset, err := h.getJellyfinPresetByID(limits.TargetPresetID); err == nil {
				if days := presetInviteLinkValidityDays(preset); days > 0 {
					limits.LinkValidityDays = days
				}
				if preset.DisableAfterDays > 0 {
					limits.UserExpiryDays = preset.DisableAfterDays
				}
			}
		}

		return limits, nil
	}

	var (
		canInvite bool
		presetID  sql.NullString
	)
	err := h.db.QueryRow(
		`SELECT can_invite, preset_id FROM users WHERE jellyfin_id = ?`,
		sess.UserID,
	).Scan(&canInvite, &presetID)
	if err != nil && err != sql.ErrNoRows {
		return limits, err
	}
	limits.CanInvite = canInvite

	presetIDStr := strings.TrimSpace(presetID.String)
	if presetIDStr != "" {
		preset, err := h.getJellyfinPresetByID(presetIDStr)
		if err == nil && preset != nil {
			limits.SourcePreset = preset
			if preset.CanInvite {
				limits.CanInvite = true
			}
			if preset.InviteAllowLanguage {
				limits.AllowLanguage = true
			}
			if preset.InviteMaxUses > 0 {
				limits.MaxUses = preset.InviteMaxUses
			}
			if days := presetInviteLinkValidityDays(preset); days > 0 {
				limits.LinkValidityDays = days
			}
			if preset.InviteQuotaDay > 0 {
				limits.QuotaDay = preset.InviteQuotaDay
			}
			if quotaMonth := presetInviteQuotaMonth(preset); quotaMonth > 0 {
				limits.QuotaMonth = quotaMonth
			}
			if strings.TrimSpace(preset.TargetPresetID) != "" {
				limits.TargetPresetID = strings.TrimSpace(preset.TargetPresetID)
			}
			if preset.DisableAfterDays > 0 {
				limits.UserExpiryDays = preset.DisableAfterDays
			}
		}
	}

	if limits.TargetPresetID != "" {
		if targetPreset, err := h.getJellyfinPresetByID(limits.TargetPresetID); err == nil && targetPreset != nil {
			if targetPreset.DisableAfterDays > 0 {
				limits.UserExpiryDays = targetPreset.DisableAfterDays
			}
		}
	}

	return limits, nil
}

func (h *AdminHandler) countInvitationsCreatedSince(creator string, since time.Time) (int, error) {
	creator = strings.TrimSpace(creator)
	if creator == "" {
		return 0, fmt.Errorf("creator vide")
	}

	var count int
	err := h.db.QueryRow(
		`SELECT COUNT(1) FROM invitations WHERE created_by = ? AND created_at >= ?`,
		creator,
		since,
	).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

func resolveInviteEmailVerificationRequirement(policy string, legacyRequire bool, createdByAdmin bool, maxUses int) bool {
	mode := strings.TrimSpace(strings.ToLower(policy))
	switch mode {
	case "required":
		return true
	case "disabled":
		return false
	case "admin_bypass":
		if createdByAdmin {
			return false
		}
		return true
	case "conditional":
		if createdByAdmin && maxUses == 1 {
			return false
		}
		return true
	default:
		return legacyRequire
	}
}

// ListInvitations retourne les invitations SQLite avec pagination et recherche.
func (h *AdminHandler) ListInvitations(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	q := r.URL.Query()

	page := 1
	limit := 25
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		page = p
	}
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 {
		limit = l
	}
	if limit > 500 {
		limit = 500
	}

	search := strings.TrimSpace(q.Get("search"))

	whereParts := make([]string, 0)
	args := make([]interface{}, 0)

	if !sess.IsAdmin {
		whereParts = append(whereParts, "created_by = ?")
		args = append(args, sess.Username)
	}

	if search != "" {
		term := "%" + search + "%"
		whereParts = append(whereParts, "(code LIKE ? OR label LIKE ? OR created_by LIKE ?)")
		args = append(args, term, term, term)
	}

	whereClause := ""
	if len(whereParts) > 0 {
		whereClause = "WHERE " + strings.Join(whereParts, " AND ")
	}

	// 1. Compter le total
	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM invitations %s", whereClause)
	if err := h.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		slog.Error("Erreur comptage des invitations", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur lecture base de donnees"})
		return
	}

	// 2. RÃ©cupÃ©rer les donnÃ©es paginÃ©es
	offset := (page - 1) * limit
	query := fmt.Sprintf(`SELECT id, code, label, preferred_lang, max_uses, used_count, jellyfin_profile, expires_at, created_by, created_at FROM invitations %s ORDER BY created_at DESC LIMIT ? OFFSET ?`, whereClause)

	queryArgs := append(args, limit, offset)
	rows, err := h.db.Query(query, queryArgs...)
	if err != nil {
		slog.Error("Erreur lecture des invitations", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de lecture de la base de donnees"})
		return
	}
	defer rows.Close()

	var invs []InvitationResponse
	for rows.Next() {
		var i InvitationResponse
		var label, profile, createdBy, preferredLang sql.NullString
		var rawExpiresAt interface{}
		var rawCreatedAt interface{}

		err := rows.Scan(
			&i.ID, &i.Code, &label, &preferredLang, &i.MaxUses, &i.UsedCount,
			&profile, &rawExpiresAt, &createdBy, &rawCreatedAt,
		)
		if err != nil {
			slog.Error("Erreur scan invitation", "error", err)
			continue
		}

		i.Label = label.String
		i.PreferredLang = normalizeSupportedEmailLang(preferredLang.String)
		i.ExpiresAt = anyToDateString(rawExpiresAt)
		i.CreatedBy = createdBy.String
		i.CreatedAt = anyToDateString(rawCreatedAt)

		if profile.String != "" {
			var p map[string]interface{}
			_ = json.Unmarshal([]byte(profile.String), &p)
			i.JellyfinProfile = p
		}

		invs = append(invs, i)
	}

	totalPages := 1
	if total > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(limit)))
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"invitations": invs,
			"meta": map[string]interface{}{
				"total":       total,
				"page":        page,
				"limit":       limit,
				"total_pages": totalPages,
			},
		},
	})
}

type InvitationSponsorStats struct {
	Sponsor        string  `json:"sponsor"`
	CreatedLinks   int     `json:"created_links"`
	ActiveLinks    int     `json:"active_links"`
	ClosedLinks    int     `json:"closed_links"`
	TotalUses      int     `json:"total_uses"`
	Conversions    int     `json:"conversions"`
	ConversionRate float64 `json:"conversion_rate"`
}

// InvitationStats retourne des statistiques de parrainage par createur d'invitations.
func (h *AdminHandler) InvitationStats(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	cleanupClosedInvitationsIfEnabled(h.db)

	scope := "all"
	filterByCreator := ""
	if !sess.IsAdmin {
		var canInvite bool
		_ = h.db.QueryRow(`SELECT can_invite FROM users WHERE jellyfin_id = ?`, sess.UserID).Scan(&canInvite)
		if !canInvite {
			writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Vous n'avez pas l'autorisation d'acceder aux statistiques de parrainage"})
			return
		}
		scope = "mine"
		filterByCreator = sess.Username
	}

	now := time.Now()
	statsQuery := `
		SELECT created_by,
		       COUNT(1) AS created_links,
		       SUM(CASE WHEN (expires_at IS NULL OR expires_at > ?) AND (max_uses = 0 OR used_count < max_uses) THEN 1 ELSE 0 END) AS active_links,
		       SUM(CASE WHEN NOT ((expires_at IS NULL OR expires_at > ?) AND (max_uses = 0 OR used_count < max_uses)) THEN 1 ELSE 0 END) AS closed_links,
		       SUM(used_count) AS total_uses
		FROM invitations`
	statsArgs := []interface{}{now, now}
	if filterByCreator != "" {
		statsQuery += ` WHERE created_by = ?`
		statsArgs = append(statsArgs, filterByCreator)
	}
	statsQuery += ` GROUP BY created_by ORDER BY total_uses DESC, created_by ASC`

	rows, err := h.db.Query(statsQuery, statsArgs...)
	if err != nil {
		slog.Error("Erreur lecture statistiques invitations", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de lecture des statistiques"})
		return
	}
	defer rows.Close()

	statsBySponsor := make(map[string]*InvitationSponsorStats)
	for rows.Next() {
		var creator sql.NullString
		var createdLinks, activeLinks, closedLinks, totalUses sql.NullInt64
		if scanErr := rows.Scan(&creator, &createdLinks, &activeLinks, &closedLinks, &totalUses); scanErr != nil {
			continue
		}

		sponsorKey := strings.TrimSpace(creator.String)
		if sponsorKey == "" {
			sponsorKey = "(inconnu)"
		}

		statsBySponsor[sponsorKey] = &InvitationSponsorStats{
			Sponsor:      sponsorKey,
			CreatedLinks: int(createdLinks.Int64),
			ActiveLinks:  int(activeLinks.Int64),
			ClosedLinks:  int(closedLinks.Int64),
			TotalUses:    int(totalUses.Int64),
		}
	}

	convQuery := `
		SELECT i.created_by, COUNT(u.id) AS conversions
		FROM invitations i
		LEFT JOIN users u ON u.invited_by = i.code`
	convArgs := []interface{}{}
	if filterByCreator != "" {
		convQuery += ` WHERE i.created_by = ?`
		convArgs = append(convArgs, filterByCreator)
	}
	convQuery += ` GROUP BY i.created_by`

	convRows, err := h.db.Query(convQuery, convArgs...)
	if err == nil {
		defer convRows.Close()
		for convRows.Next() {
			var creator sql.NullString
			var conversions sql.NullInt64
			if scanErr := convRows.Scan(&creator, &conversions); scanErr != nil {
				continue
			}

			sponsorKey := strings.TrimSpace(creator.String)
			if sponsorKey == "" {
				sponsorKey = "(inconnu)"
			}
			if item, ok := statsBySponsor[sponsorKey]; ok {
				item.Conversions = int(conversions.Int64)
			}
		}
	}

	stats := make([]InvitationSponsorStats, 0, len(statsBySponsor))
	totalLinks := 0
	totalActive := 0
	totalClosed := 0
	totalUses := 0
	totalConversions := 0

	for _, item := range statsBySponsor {
		if item.CreatedLinks > 0 {
			item.ConversionRate = (float64(item.Conversions) / float64(item.CreatedLinks)) * 100
		}
		stats = append(stats, *item)

		totalLinks += item.CreatedLinks
		totalActive += item.ActiveLinks
		totalClosed += item.ClosedLinks
		totalUses += item.TotalUses
		totalConversions += item.Conversions
	}

	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Conversions == stats[j].Conversions {
			return strings.ToLower(stats[i].Sponsor) < strings.ToLower(stats[j].Sponsor)
		}
		return stats[i].Conversions > stats[j].Conversions
	})

	globalRate := 0.0
	if totalLinks > 0 {
		globalRate = (float64(totalConversions) / float64(totalLinks)) * 100
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"scope":           scope,
			"total_links":     totalLinks,
			"active_links":    totalActive,
			"closed_links":    totalClosed,
			"total_uses":      totalUses,
			"conversions":     totalConversions,
			"conversion_rate": globalRate,
			"by_sponsor":      stats,
			"generated_at":    now.Format(time.RFC3339),
		},
	})
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ POST /admin/api/invitations Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// CreateInvitationRequest payload pour la crÃƒÂ©ation d'invitation
type CreateInvitationRequest struct {
	Label                  string   `json:"label"`
	PreferredLang          string   `json:"preferred_lang"`
	MaxUses                int      `json:"max_uses"`   // 0 = illimitÃƒÂ©
	ExpiresAt              string   `json:"expires_at"` // Legacy: date prÃƒÂ©cise, exemple "2026-10-05T12:00"
	ExpiresInDays          int      `json:"expires_in_days"`
	IgnorePresetLinkExpiry bool     `json:"ignore_preset_link_expiry"`
	ApplyUserExpiry        *bool    `json:"apply_user_expiry"`
	UserExpiryDays         int      `json:"user_expiry_days"` // Expiration finale du compte client (jours)
	UserExpiresAt          string   `json:"user_expires_at"`
	IgnorePresetUserExpiry bool     `json:"ignore_preset_user_expiry"`
	DisableAfterDays       int      `json:"disable_after_days"`
	NewUserCanInvite       bool     `json:"new_user_can_invite"`
	SendToEmail            string   `json:"send_to_email"` // Si renseignÃƒÂ©, un e-mail partira par SMTP
	Email                  string   `json:"email"`         // Legacy frontend key
	EmailMessage           string   `json:"email_message"`
	Libraries              []string `json:"libraries"` // ID des bibliothÃƒÂ¨ques Jellyfin
	EnableDownloads        bool     `json:"enable_downloads"`
	PolicyPresetID         string   `json:"policy_preset_id"`
	GroupName              string   `json:"group_name"`
	ForcedUsername         string   `json:"forced_username"`
	TemplateUserID         string   `json:"template_user_id"`
	UsernameMinLen         *int     `json:"username_min_length"`
	UsernameMaxLen         *int     `json:"username_max_length"`
	PasswordMinLen         *int     `json:"password_min_length"`
	PasswordMaxLen         *int     `json:"password_max_length"`
	RequireUpper           *bool    `json:"password_require_upper"`
	RequireLower           *bool    `json:"password_require_lower"`
	RequireDigit           *bool    `json:"password_require_digit"`
	RequireSpecial         *bool    `json:"password_require_special"`
	ExpiryAction           string   `json:"expiry_action"`
	DeleteAfterDays        *int     `json:"delete_after_days"`
}

// CreateInvitation crÃƒÂ©e un nouveau lien d'invitation avec un jeton robuste et logiques complexes (JFA-GO).
func (h *AdminHandler) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())

	var req CreateInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}

	if req.MaxUses < 0 {
		req.MaxUses = 0
	}
	if req.ExpiresInDays < 0 {
		req.ExpiresInDays = 0
	}
	if req.UserExpiryDays < 0 {
		req.UserExpiryDays = 0
	}
	if req.DisableAfterDays < 0 {
		req.DisableAfterDays = 0
	}
	req.PreferredLang = normalizeSupportedEmailLang(req.PreferredLang)

	inviteCfg, err := h.db.GetInvitationProfileConfig()
	if err != nil {
		slog.Error("Erreur chargement config profil invitation", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de lecture du profil d'invitation"})
		return
	}

	limits, err := h.resolveInvitationCreatorLimits(sess, inviteCfg)
	if err != nil {
		slog.Error("Erreur resolution limites invitations", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible de charger les limites de creation"})
		return
	}

	if !sess.IsAdmin && !limits.CanInvite {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Vous n'avez pas l'autorisation de crÃƒÂ©er des invitations"})
		return
	}

	if (req.IgnorePresetLinkExpiry || req.IgnorePresetUserExpiry) && !limits.AllowIgnoreLimits {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Le contournement des limites preset est reserve aux administrateurs"})
		return
	}

	if req.NewUserCanInvite && !sess.IsAdmin && !limits.AllowGrant {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "La delegation du droit d'invitation est reservee par la configuration admin"})
		return
	}

	if !sess.IsAdmin {
		if req.MaxUses <= 0 {
			req.MaxUses = 1
		}
		if limits.MaxUses > 0 && req.MaxUses > limits.MaxUses {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: fmt.Sprintf("Limite par lien: entre 1 et %d utilisations pour les parrains", limits.MaxUses)})
			return
		}
	}

	if strings.TrimSpace(req.ForcedUsername) != "" && req.MaxUses != 1 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Un nom d'utilisateur reservÃ© nÃ©cessite un lien Ã  usage unique (max_uses = 1)"})
		return
	}

	now := time.Now()

	if !sess.IsAdmin {
		if limits.QuotaDay > 0 {
			usedDay, countErr := h.countInvitationsCreatedSince(sess.Username, startOfLocalDay(now))
			if countErr != nil {
				slog.Error("Erreur calcul quota jour", "user", sess.Username, "error", countErr)
				writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible de verifier le quota journalier"})
				return
			}
			if usedDay >= limits.QuotaDay {
				writeJSON(w, http.StatusTooManyRequests, APIResponse{Success: false, Message: fmt.Sprintf("Quota journalier atteint (%d/%d)", usedDay, limits.QuotaDay)})
				return
			}
		}

		if limits.QuotaMonth > 0 {
			usedMonth, countErr := h.countInvitationsCreatedSince(sess.Username, startOfLocalMonth(now))
			if countErr != nil {
				slog.Error("Erreur calcul quota mois", "user", sess.Username, "error", countErr)
				writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible de verifier le quota mensuel"})
				return
			}
			if usedMonth >= limits.QuotaMonth {
				writeJSON(w, http.StatusTooManyRequests, APIResponse{Success: false, Message: fmt.Sprintf("Quota mensuel atteint (%d/%d)", usedMonth, limits.QuotaMonth)})
				return
			}
		}
	}

	var legacyLinkExpiry time.Time
	if strings.TrimSpace(req.ExpiresAt) != "" {
		parsed, parseErr := parseInvitationDateTimeInput(req.ExpiresAt)
		if parseErr != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Format d'expiration du lien invalide"})
			return
		}
		if !parsed.After(now) {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "La validite du lien doit etre dans le futur"})
			return
		}
		legacyLinkExpiry = parsed
	}

	effectiveLinkValidityDays := req.ExpiresInDays
	if effectiveLinkValidityDays <= 0 && !legacyLinkExpiry.IsZero() {
		hours := legacyLinkExpiry.Sub(now).Hours()
		effectiveLinkValidityDays = int(math.Ceil(hours / 24.0))
		if effectiveLinkValidityDays < 1 {
			effectiveLinkValidityDays = 1
		}
	}

	if limits.LinkValidityDays > 0 && !req.IgnorePresetLinkExpiry {
		if effectiveLinkValidityDays <= 0 {
			effectiveLinkValidityDays = limits.LinkValidityDays
		}
		if effectiveLinkValidityDays > limits.LinkValidityDays {
			if !sess.IsAdmin {
				writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: fmt.Sprintf("Validite du lien limitee a %d jour(s) pour ce preset", limits.LinkValidityDays)})
				return
			}
			effectiveLinkValidityDays = limits.LinkValidityDays
		}
	}

	var expiresAt interface{}
	inviteExpiryDate := ""
	if effectiveLinkValidityDays > 0 {
		resolvedExpiry := now.AddDate(0, 0, effectiveLinkValidityDays)
		expiresAt = resolvedExpiry
		inviteExpiryDate = emailTime(resolvedExpiry)
	} else if !legacyLinkExpiry.IsZero() {
		expiresAt = legacyLinkExpiry
		inviteExpiryDate = emailTime(legacyLinkExpiry)
	}

	applyUserExpiry := req.ApplyUserExpiry != nil && *req.ApplyUserExpiry

	if !sess.IsAdmin && !limits.AllowUserExpiry {
		if limits.UserExpiryDays > 0 && !req.IgnorePresetUserExpiry {
			applyUserExpiry = true
			req.UserExpiryDays = limits.UserExpiryDays
			req.UserExpiresAt = ""
		} else {
			applyUserExpiry = false
			req.UserExpiryDays = 0
			req.UserExpiresAt = ""
		}
	}

	if limits.UserExpiryDays > 0 && !req.IgnorePresetUserExpiry {
		if !applyUserExpiry {
			applyUserExpiry = true
		}
		if req.UserExpiryDays <= 0 {
			req.UserExpiryDays = limits.UserExpiryDays
		}
		if !sess.IsAdmin && req.UserExpiryDays > limits.UserExpiryDays {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: fmt.Sprintf("Expiration utilisateur limitee a %d jour(s) pour ce preset", limits.UserExpiryDays)})
			return
		}
	}

	var effectiveUserExpiresAt time.Time
	effectiveDisableAfterDays := 0
	if applyUserExpiry {
		if strings.TrimSpace(req.UserExpiresAt) != "" {
			parsedUserExpiry, parseErr := parseInvitationDateTimeInput(req.UserExpiresAt)
			if parseErr != nil {
				writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Format d'expiration utilisateur invalide"})
				return
			}
			if !parsedUserExpiry.After(now) {
				writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "L'expiration utilisateur doit etre dans le futur"})
				return
			}
			effectiveUserExpiresAt = parsedUserExpiry
		}

		if effectiveUserExpiresAt.IsZero() {
			overrideDays := req.UserExpiryDays
			if overrideDays <= 0 {
				overrideDays = req.DisableAfterDays
			}
			if overrideDays <= 0 {
				writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Renseigne un nombre de jours valide pour l'expiration utilisateur"})
				return
			}
			effectiveDisableAfterDays = overrideDays
		}
	}

	effectiveCanInvite := false
	if req.NewUserCanInvite {
		if sess.IsAdmin || limits.AllowGrant {
			effectiveCanInvite = true
		}
	}

	effectiveUserExpiresAtRaw := ""
	if !effectiveUserExpiresAt.IsZero() {
		effectiveUserExpiresAtRaw = effectiveUserExpiresAt.Format(time.RFC3339)
	}

	effectiveRequireEmailVerification := resolveInviteEmailVerificationRequirement(
		inviteCfg.EmailVerificationPolicy,
		inviteCfg.RequireEmailVerification,
		sess.IsAdmin,
		req.MaxUses,
	)

	// GÃƒÂ©nÃƒÂ©rer code alÃƒÂ©atoire (ici via crypt/rand classique, 12 caractÃƒÂ¨res)
	code, err := generateSecureToken(12)
	if err != nil {
		slog.Error("Erreur gÃƒÂ©nÃƒÂ©ration token d'invitation", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible de gÃƒÂ©nÃƒÂ©rer un code d'invitation"})
		return
	}
	// Construire profil Jellyfin depuis la configuration admin (paramÃƒÂ¨tres globaux).
	jfProfile := jellyfin.InviteProfile{
		EnableAllFolders:         len(req.Libraries) == 0,
		EnabledFolderIDs:         req.Libraries,
		EnableDownload:           inviteCfg.EnableDownloads,
		RequireEmail:             inviteCfg.RequireEmail,
		RequireEmailVerification: effectiveRequireEmailVerification,
		EnableRemoteAccess:       true,
		UserExpiryDays:           effectiveDisableAfterDays,
		UserExpiresAt:            effectiveUserExpiresAtRaw,
		DisableAfterDays:         effectiveDisableAfterDays,
		GroupName:                strings.TrimSpace(req.GroupName),
		ForcedUsername:           strings.TrimSpace(req.ForcedUsername),
		CanInvite:                effectiveCanInvite,
		TemplateUserID:           "",
		UsernameMinLength:        inviteCfg.UsernameMinLength,
		UsernameMaxLength:        inviteCfg.UsernameMaxLength,
		PasswordMinLength:        inviteCfg.PasswordMinLength,
		PasswordMaxLength:        inviteCfg.PasswordMaxLength,
		PasswordRequireUpper:     inviteCfg.PasswordRequireUpper,
		PasswordRequireLower:     inviteCfg.PasswordRequireLower,
		PasswordRequireDigit:     inviteCfg.PasswordRequireDigit,
		PasswordRequireSpecial:   inviteCfg.PasswordRequireSpecial,
		ExpiryAction:             normalizeExpiryAction(inviteCfg.ExpiryAction),
		DeleteAfterDays:          inviteCfg.DeleteAfterDays,
	}

	targetPresetID := strings.TrimSpace(limits.TargetPresetID)
	if sess.IsAdmin && strings.TrimSpace(req.PolicyPresetID) != "" {
		targetPresetID = strings.TrimSpace(req.PolicyPresetID)
	}

	if strings.TrimSpace(targetPresetID) != "" {
		preset, err := h.getJellyfinPresetByID(targetPresetID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Preset Jellyfin introuvable"})
			return
		}

		jfProfile.PresetID = preset.ID
		jfProfile.EnableAllFolders = preset.EnableAllFolders
		jfProfile.EnabledFolderIDs = preset.EnabledFolderIDs
		jfProfile.EnableDownload = preset.EnableDownload
		jfProfile.EnableRemoteAccess = preset.EnableRemoteAccess
		jfProfile.MaxSessions = preset.MaxSessions
		jfProfile.BitrateLimit = preset.BitrateLimit
		jfProfile.TemplateUserID = strings.TrimSpace(preset.TemplateUserID)
		jfProfile.UsernameMinLength = preset.UsernameMinLength
		jfProfile.UsernameMaxLength = preset.UsernameMaxLength
		jfProfile.PasswordMinLength = preset.PasswordMinLength
		jfProfile.PasswordMaxLength = preset.PasswordMaxLength
		jfProfile.PasswordRequireUpper = preset.RequireUpper
		jfProfile.PasswordRequireLower = preset.RequireLower
		jfProfile.PasswordRequireDigit = preset.RequireDigit
		jfProfile.PasswordRequireSpecial = preset.RequireSpecial
		jfProfile.DisableAfterDays = preset.DisableAfterDays
		jfProfile.ExpiryAction = normalizeExpiryAction(preset.ExpiryAction)
		jfProfile.DeleteAfterDays = preset.DeleteAfterDays
	}

	// Les options exposees dans "Profil utilisateur" sont forcees par les paramÃƒÂ¨tres admin.
	jfProfile.EnableDownload = inviteCfg.EnableDownloads
	jfProfile.RequireEmail = inviteCfg.RequireEmail
	jfProfile.RequireEmailVerification = effectiveRequireEmailVerification
	jfProfile.UsernameMinLength = inviteCfg.UsernameMinLength
	jfProfile.UsernameMaxLength = inviteCfg.UsernameMaxLength
	jfProfile.PasswordMinLength = inviteCfg.PasswordMinLength
	jfProfile.PasswordMaxLength = inviteCfg.PasswordMaxLength
	jfProfile.PasswordRequireUpper = inviteCfg.PasswordRequireUpper
	jfProfile.PasswordRequireLower = inviteCfg.PasswordRequireLower
	jfProfile.PasswordRequireDigit = inviteCfg.PasswordRequireDigit
	jfProfile.PasswordRequireSpecial = inviteCfg.PasswordRequireSpecial
	jfProfile.DisableAfterDays = effectiveDisableAfterDays
	jfProfile.UserExpiresAt = effectiveUserExpiresAtRaw
	if strings.TrimSpace(jfProfile.ExpiryAction) == "" {
		jfProfile.ExpiryAction = normalizeExpiryAction(inviteCfg.ExpiryAction)
	}
	if jfProfile.DeleteAfterDays < 0 {
		jfProfile.DeleteAfterDays = 0
	}
	jfProfile.CanInvite = effectiveCanInvite

	if !applyUserExpiry {
		jfProfile.DisableAfterDays = 0
		jfProfile.UserExpiryDays = 0
		jfProfile.UserExpiresAt = ""
	}

	jfProfile.UserExpiryDays = jfProfile.DisableAfterDays

	if jfProfile.UsernameMinLength <= 0 {
		jfProfile.UsernameMinLength = 3
	}
	if jfProfile.UsernameMaxLength <= 0 {
		jfProfile.UsernameMaxLength = 32
	}
	if jfProfile.UsernameMaxLength < jfProfile.UsernameMinLength {
		jfProfile.UsernameMaxLength = jfProfile.UsernameMinLength
	}

	if jfProfile.PasswordMinLength <= 0 {
		jfProfile.PasswordMinLength = 8
	}
	if jfProfile.PasswordMaxLength <= 0 {
		jfProfile.PasswordMaxLength = 128
	}
	if jfProfile.PasswordMaxLength < jfProfile.PasswordMinLength {
		jfProfile.PasswordMaxLength = jfProfile.PasswordMinLength
	}

	profileJSON, _ := json.Marshal(jfProfile)

	_, err = h.db.Exec(
		`INSERT INTO invitations (code, label, max_uses, jellyfin_profile, preferred_lang, expires_at, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		code, req.Label, req.MaxUses, string(profileJSON), req.PreferredLang, expiresAt, sess.Username,
	)

	if err != nil {
		slog.Error("Erreur crÃƒÂ©ation invitation DB", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur d'insertion BD"})
		return
	}

	h.db.LogAction("invite.created", sess.Username, req.Label, fmt.Sprintf("Code: %s", code))

	// Envoi SMTP si demandÃƒÂ©
	links := resolvePortalLinks(h.cfg, h.db)
	baseURL := strings.TrimSpace(links.JellyGateURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(h.cfg.BaseURL)
	}
	if baseURL == "" {
		baseURL = requestBaseURL(r)
	}
	inviteURL := fmt.Sprintf("%s/invite/%s", strings.TrimRight(baseURL, "/"), code)
	sendToEmail := strings.TrimSpace(req.SendToEmail)
	if sendToEmail == "" {
		sendToEmail = strings.TrimSpace(req.Email)
	}
	if sendToEmail != "" {
		if h.mailer != nil {
			customMessage := strings.TrimSpace(req.EmailMessage)
			inviteeName := strings.TrimSpace(req.ForcedUsername)
			if inviteeName == "" {
				inviteeName = "invitÃƒÂ©"
			}

			go func(recipient, username, expiryDate, customBody, invitationLang string) {
				emailCfg, usedLang, cfgErr := loadEmailTemplatesForLanguage(h.db, invitationLang, emailLanguageContext{})
				if cfgErr != nil {
					emailCfg = config.DefaultEmailTemplatesForLanguage(usedLang)
				}
				sections := []string{emailCfg.Invitation}
				if expiryDate != "" && !emailCfg.DisableInviteExpiryEmail {
					sections = append(sections, emailCfg.InviteExpiry)
				}
				if !emailCfg.DisablePreSignupHelpEmail {
					sections = append(sections, emailCfg.PreSignupHelp)
				}
				combinedTemplate := joinTemplateSections(sections...)

				if strings.TrimSpace(customBody) != "" {
					combinedTemplate = joinTemplateSections(combinedTemplate, "{{.Message}}")
				}

				if combinedTemplate == "" {
					combinedTemplate = "Bonjour,\n\nVous ÃƒÂªtes invitÃƒÂ© ÃƒÂ  rejoindre notre serveur. Cliquez sur ce lien pour crÃƒÂ©er votre compte : {{.InviteLink}}"
				}

				emailData := map[string]string{
					"InviteLink":         inviteURL,
					"InviteURL":          inviteURL,
					"InviteCode":         code,
					"HelpURL":            strings.TrimRight(baseURL, "/"),
					"JellyGateURL":       strings.TrimRight(baseURL, "/"),
					"Username":           username,
					"JellyfinURL":        links.JellyfinURL,
					"JellyfinServerName": links.JellyfinServerName,
					"JellyseerrURL":      links.JellyseerrURL,
					"JellyTrackURL":      links.JellyTrackURL,
				}
				if expiryDate != "" {
					emailData["ExpiryDate"] = expiryDate
				}
				if strings.TrimSpace(customBody) != "" {
					emailData["Message"] = customBody
				}

				subject := firstNonEmpty(emailCfg.InvitationSubject, emailCfg.InviteExpirySubject, config.DefaultEmailTemplatesForLanguage(usedLang).InvitationSubject)
				errMail := sendTemplateIfConfigured(h.mailer, recipient, subject, usedLang, "invitation", combinedTemplate, emailCfg, emailData)
				if errMail != nil {
					slog.Error("Erreur d'envoi SMTP (Invitation)", "email", recipient, "error", errMail)
					_ = h.db.LogAction("invite.email.failed", sess.Username, code, errMail.Error())
				}
			}(sendToEmail, inviteeName, inviteExpiryDate, customMessage, req.PreferredLang)
		} else {
			slog.Warn("Option e-mail cochÃƒÂ©e pour l'invitation, mais le serveur SMTP n'est pas configurÃƒÂ©.")
		}
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Invitation gÃƒÂ©nÃƒÂ©rÃƒÂ©e avec succÃƒÂ¨s",
		Data: map[string]interface{}{
			"code": code,
			"url":  inviteURL,
		},
	})
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ DELETE /admin/api/invitations/{id} Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// DeleteInvitation supprime brutalement l'invitation SQLite
func (h *AdminHandler) DeleteInvitation(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	invID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID invalide"})
		return
	}

	var errDB error
	if sess.IsAdmin {
		_, errDB = h.db.Exec(`DELETE FROM invitations WHERE id = ?`, invID)
	} else {
		// Security: Le standard user ne supprime que ses propres liens
		result, errDBQuery := h.db.Exec(`DELETE FROM invitations WHERE id = ? AND created_by = ?`, invID, sess.Username)
		errDB = errDBQuery
		if errDB == nil {
			rowsAffected, _ := result.RowsAffected()
			if rowsAffected == 0 {
				writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Vous n'avez pas l'autorisation de supprimer ce lien"})
				return
			}
		}
	}

	if errDB != nil {
		slog.Error("Erreur suppression invitation", "id", invID, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur DB"})
		return
	}

	h.db.LogAction("invite.deleted", sess.Username, fmt.Sprintf("%d", invID), "")

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Lien d'invitation dÃƒÂ©truit",
	})
}

// writeJSON ÃƒÂ©crit une rÃƒÂ©ponse JSON avec le code HTTP donnÃƒÂ©.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		slog.Error("Erreur d'encodage JSON", "error", err)
	}
}
