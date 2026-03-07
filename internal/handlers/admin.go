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
	netmail "net/mail"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/i18nreport"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	jgldap "github.com/maelmoreau21/JellyGate/internal/ldap"
	"github.com/maelmoreau21/JellyGate/internal/mail"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

// ── Structures de réponse JSON ──────────────────────────────────────────────

// UserResponse est la représentation JSON d'un utilisateur pour l'API admin.
type UserResponse struct {
	ID              int64  `json:"id"`
	JellyfinID      string `json:"jellyfin_id"`
	Username        string `json:"username"`
	Email           string `json:"email"`
	LdapDN          string `json:"ldap_dn"`
	GroupName       string `json:"group_name"`
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

	// Statuts temps réel depuis Jellyfin (enrichissement)
	JellyfinDisabled bool `json:"jellyfin_disabled"`
	JellyfinExists   bool `json:"jellyfin_exists"`
}

// APIResponse est l'enveloppe standard pour toutes les réponses JSON.
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Errors  []string    `json:"errors,omitempty"`
}

type UserTimelineEvent struct {
	At      string `json:"at"`
	Action  string `json:"action"`
	Actor   string `json:"actor,omitempty"`
	Target  string `json:"target,omitempty"`
	Details string `json:"details,omitempty"`
	Message string `json:"message"`
}

type adminUserRecord struct {
	ID              int64
	Username        string
	Email           string
	PendingEmail    string
	EmailVerified   bool
	JellyfinID      string
	LdapDN          string
	GroupName       string
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
	CanInvite       *bool   `json:"can_invite"`
	AccessExpiresAt *string `json:"access_expires_at"`
	ClearExpiry     bool    `json:"clear_expiry"`
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
	PolicyPresetID  string                   `json:"policy_preset_id"`
	EmailSubject    string                   `json:"email_subject"`
	EmailBody       string                   `json:"email_body"`
	CanInvite       *bool                    `json:"can_invite"`
	AccessExpiresAt *string                  `json:"access_expires_at"`
	ClearExpiry     bool                     `json:"clear_expiry"`
	JellyfinPolicy  *BulkJellyfinPolicyPatch `json:"jellyfin_policy"`
}

// ── Admin Handler ───────────────────────────────────────────────────────────

// AdminHandler gère les endpoints d'administration.
type AdminHandler struct {
	cfg      *config.Config
	db       *database.DB
	jfClient *jellyfin.Client
	ldClient *jgldap.Client
	mailer   *mail.Mailer
	renderer *render.Engine
}

// NewAdminHandler crée un nouveau handler d'administration.
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

// SetLDAPClient remplace le client LDAP (rechargement à chaud).
func (h *AdminHandler) SetLDAPClient(ld *jgldap.Client) { h.ldClient = ld }

// SetMailer remplace le Mailer SMTP (rechargement à chaud).
func (h *AdminHandler) SetMailer(m *mail.Mailer) { h.mailer = m }

func (h *AdminHandler) sendUserEventEmail(rec *adminUserRecord, subject, templateBody string, extra map[string]string) error {
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
		"Username":      rec.Username,
		"Email":         rec.Email,
		"HelpURL":       helpURL,
		"JellyGateURL":  helpURL,
		"JellyfinURL":   links.JellyfinURL,
		"JellyseerrURL": links.JellyseerrURL,
		"JellyTulliURL": links.JellyTulliURL,
	}
	if rec.AccessExpiresAt.Valid {
		if t, err := parseAccessExpiry(rec.AccessExpiresAt.String); err == nil {
			data["ExpiryDate"] = emailTime(t)
		}
	}
	for key, value := range extra {
		data[key] = value
	}

	return sendTemplateIfConfigured(h.mailer, rec.Email, subject, templateBody, data)
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

	emailCfg, err := h.db.GetEmailTemplatesConfig()
	if err != nil {
		return err
	}

	var subject, body string
	switch templateKey {
	case "user_enabled":
		subject = "Compte réactivé — JellyGate"
		body = emailCfg.UserEnabled
	case "user_disabled":
		subject = "Compte désactivé — JellyGate"
		body = emailCfg.UserDisabled
	case "user_deleted":
		if emailCfg.DisableUserDeletionEmail {
			return nil
		}
		subject = "Compte supprimé — JellyGate"
		body = emailCfg.UserDeletion
	case "user_expired":
		subject = "Compte expiré — JellyGate"
		body = emailCfg.UserExpired
	case "expiry_adjusted":
		subject = "Expiration ajustée — JellyGate"
		body = emailCfg.ExpiryAdjusted
	case "expiry_reminder":
		subject = "Rappel d'expiration — JellyGate"
		body = emailCfg.ExpiryReminder
	default:
		return nil
	}

	return h.sendUserEventEmail(rec, subject, body, extra)
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

// ── Background Jobs ─────────────────────────────────────────────────────────

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

	emailCfg, _ := h.db.GetEmailTemplatesConfig()
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
			subject := fmt.Sprintf("Rappel d'expiration J-%d — JellyGate", stageDays)
			if err := h.sendUserEventEmail(rec, subject, templateBody, map[string]string{
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

	// Rechercher les utilisateurs actifs dont access_expires_at est dépassé.
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
		LdapDN          string
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
		u.LdapDN = ldDN.String
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

		if h.ldClient != nil && u.LdapDN != "" {
			if err := h.ldClient.DisableUser(u.LdapDN); err != nil {
				slog.Error("Erreur lors de la desactivation LDAP (Expiration)", "error", err)
			}
		}

		if u.JellyfinID != "" {
			if err := h.jfClient.DisableUser(u.JellyfinID); err != nil {
				slog.Error("Erreur lors de la desactivation Jellyfin (Expiration)", "error", err)
			}
		}

		_, err := h.db.Exec(`UPDATE users SET is_active = FALSE, expired_at = datetime('now'), updated_at = datetime('now') WHERE id = ?`, u.ID)
		if err != nil {
			slog.Error("Erreur lors de la desactivation SQLite (Expiration)", "error", err)
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

	// Politique disable_then_delete: suppression differée apres la desactivation.
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

// ── Pages HTML ──────────────────────────────────────────────────────────────

// DashboardPage affiche la page principale du tableau de bord.
func (h *AdminHandler) DashboardPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.AdminUsername = sess.Username
	td.IsAdmin = sess.IsAdmin
	td.CanInvite = h.resolveCanInviteForSession(sess)

	if err := h.renderer.Render(w, "admin/dashboard.html", td); err != nil {
		slog.Error("Erreur rendu dashboard", "error", err)
		http.Error(w, "Erreur serveur : impossible de charger la page", http.StatusInternalServerError)
	}
}

// MyAccountPage affiche la page "Mon compte" pour l'utilisateur connecté.
func (h *AdminHandler) MyAccountPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.AdminUsername = sess.Username
	td.IsAdmin = sess.IsAdmin
	td.CanInvite = h.resolveCanInviteForSession(sess)

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
			 SET jellyfin_id = ?, is_active = TRUE, can_invite = ?, updated_at = datetime('now')
			 WHERE id = ?`,
			sess.UserID,
			sess.IsAdmin,
			userID,
		)
		return upErr
	}
	if err != sql.ErrNoRows {
		return err
	}

	_, err = h.db.Exec(
		`INSERT INTO users (jellyfin_id, username, is_active, can_invite)
			 VALUES (?, ?, TRUE, ?)
		 ON CONFLICT(jellyfin_id) DO UPDATE SET username = excluded.username, updated_at = datetime('now')`,
		sess.UserID,
		sess.Username,
		sess.IsAdmin,
	)
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
	err := h.db.QueryRow(
		`SELECT can_invite
		 FROM users
		 WHERE jellyfin_id = ? OR username = ?
		 ORDER BY CASE WHEN jellyfin_id = ? THEN 0 ELSE 1 END
		 LIMIT 1`,
		sess.UserID,
		sess.Username,
		sess.UserID,
	).Scan(&canInvite)
	if err != nil {
		return false
	}

	return canInvite
}

// GetMyAccount retourne les informations éditables de l'utilisateur connecté.
func (h *AdminHandler) GetMyAccount(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if err := h.ensureUserRowForSession(sess); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible de préparer le profil utilisateur"})
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

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"id":                     id,
			"username":               sess.Username,
			"email":                  email.String,
			"pending_email":          pendingEmail.String,
			"email_verified":         emailVerified,
			"contact_discord":        contactDiscord.String,
			"contact_telegram":       contactTelegram.String,
			"preferred_lang":         preferredLang,
			"notify_expiry_reminder": notifyExpiry,
			"notify_account_events":  notifyEvents,
			"opt_in_email":           optInEmail,
			"opt_in_discord":         optInDiscord,
			"opt_in_telegram":        optInTelegram,
			"is_admin":               sess.IsAdmin,
			"access_expires_at":      accessExpiresAt.String,
			"created_at":             createdAt.String,
		},
	})
}

// UpdateMyAccount met à jour les préférences et l'email de l'utilisateur connecté.
func (h *AdminHandler) UpdateMyAccount(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if err := h.ensureUserRowForSession(sess); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible de préparer le profil utilisateur"})
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
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de lecture des préférences"})
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
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de mise à jour des préférences"})
		return
	}

	message := "Profil mis à jour"
	if shouldSendVerification {
		if err := sendEmailVerification(h.cfg, h.db, h.mailer, userID, true); err != nil {
			slog.Error("Erreur envoi verification email apres mise a jour profil", "user_id", userID, "error", err)
			message = "Profil mis à jour, mais l'email de vérification n'a pas pu être envoyé"
		} else {
			message = "Profil mis à jour, email de vérification envoyé"
		}
	}

	if req.PreferredLang != nil {
		if strings.TrimSpace(newPreferredLang) == "" {
			http.SetCookie(w, &http.Cookie{
				Name:     "lang",
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: false,
				Secure:   r.TLS != nil,
				SameSite: http.SameSiteLaxMode,
			})
		} else {
			http.SetCookie(w, &http.Cookie{
				Name:     "lang",
				Value:    newPreferredLang,
				Path:     "/",
				MaxAge:   31536000,
				HttpOnly: false,
				Secure:   r.TLS != nil,
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

// UsersPage affiche la page de gestion des utilisateurs.
func (h *AdminHandler) UsersPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.AdminUsername = sess.Username
	td.IsAdmin = true
	td.CanInvite = true
	if err := h.renderer.Render(w, "admin/users.html", td); err != nil {
		slog.Error("Erreur rendu users page", "error", err)
		http.Error(w, "Erreur serveur : impossible de charger la page", http.StatusInternalServerError)
	}
}

// SettingsPage affiche la page de configuration globale.
func (h *AdminHandler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.AdminUsername = sess.Username
	td.IsAdmin = true
	td.CanInvite = true
	if err := h.renderer.Render(w, "admin/settings.html", td); err != nil {
		slog.Error("Erreur rendu settings page", "error", err)
		http.Error(w, "Erreur serveur : impossible de charger la page", http.StatusInternalServerError)
	}
}

// InvitationsPage affiche la page de gestion des invitations.
func (h *AdminHandler) InvitationsPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.AdminUsername = sess.Username
	td.IsAdmin = sess.IsAdmin

	inviteCfg, err := h.db.GetInvitationProfileConfig()
	if err != nil {
		slog.Warn("Impossible de charger la config invitation pour la page", "error", err)
		inviteCfg = config.DefaultInvitationProfileConfig()
	}

	links := resolvePortalLinks(h.cfg, h.db)
	inviteBaseURL := strings.TrimSpace(links.JellyGateURL)
	if inviteBaseURL == "" {
		inviteBaseURL = strings.TrimSpace(h.cfg.BaseURL)
	}
	if inviteBaseURL == "" {
		inviteBaseURL = requestBaseURL(r)
	}
	td.Data["InviteBaseURL"] = strings.TrimRight(inviteBaseURL, "/")
	td.Data["InviteAllowInviterGrant"] = inviteCfg.AllowInviterGrant
	td.Data["InviteAllowInviterUserExpiry"] = inviteCfg.AllowInviterUserExpiry
	td.Data["InviteInviterMaxUses"] = inviteCfg.InviterMaxUses
	td.Data["InviteInviterMaxLinkHours"] = inviteCfg.InviterMaxLinkHours
	td.Data["InviteInviterQuotaDay"] = inviteCfg.InviterQuotaDay
	td.Data["InviteInviterQuotaWeek"] = inviteCfg.InviterQuotaWeek
	td.Data["InviteInviterQuotaMonth"] = inviteCfg.InviterQuotaMonth
	td.Data["InviteDefaultDisableAfterDays"] = inviteCfg.DisableAfterDays
	td.Data["InviteRequireEmail"] = inviteCfg.RequireEmail

	if td.IsAdmin {
		td.CanInvite = true
	} else {
		td.CanInvite = h.resolveCanInviteForSession(sess)

		if !td.CanInvite {
			http.Error(w, "Accès interdit au programme de parrainage", http.StatusForbidden)
			return
		}
	}

	if err := h.renderer.Render(w, "admin/invitations.html", td); err != nil {
		slog.Error("Erreur rendu invitations page", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
	}
}

// LogsPage affiche la page du journal d'audit.
func (h *AdminHandler) LogsPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.AdminUsername = sess.Username
	td.IsAdmin = true
	td.CanInvite = true
	if err := h.renderer.Render(w, "admin/logs.html", td); err != nil {
		slog.Error("Erreur rendu logs page", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
	}
}

// I18nReportPage affiche la page du rapport de traduction.
func (h *AdminHandler) I18nReportPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.AdminUsername = sess.Username
	td.IsAdmin = true
	td.CanInvite = true
	if err := h.renderer.Render(w, "admin/i18n.html", td); err != nil {
		slog.Error("Erreur rendu i18n page", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
	}
}

// I18nReportAPI retourne un rapport de qualite des traductions.
func (h *AdminHandler) I18nReportAPI(w http.ResponseWriter, r *http.Request) {
	report, err := i18nreport.Generate("web/templates", "web/i18n", "en")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur generation rapport i18n"})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: report})
}

// ── GET /admin/api/logs ─────────────────────────────────────────────────────

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

// ── GET /admin/api/logs ─────────────────────────────────────────────────────

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

	whereParts := make([]string, 0, 10)
	args := make([]interface{}, 0, 20)

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

// ── POST /admin/api/users/sync ──────────────────────────────────────────────

// SyncJellyfinUsers synchronise manuellement les utilisateurs Jellyfin vers SQLite
func (h *AdminHandler) SyncJellyfinUsers(w http.ResponseWriter, r *http.Request) {
	jfUsers, err := h.jfClient.GetUsers()
	if err != nil {
		slog.Error("Erreur lors de la récupération des utilisateurs Jellyfin pour la sync", "error", err)
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

	slog.Info("Synchronisation manuelle Jellyfin terminée", "users_added", addedCount)
	h.db.LogAction("users.sync", session.FromContext(r.Context()).Username, "all",
		fmt.Sprintf("Synchronisation manuelle déclenchée: %d nouveaux utilisateurs importés", addedCount))

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: fmt.Sprintf("Synchronisation terminée: %d nouveaux utilisateurs trouvés.", addedCount),
	})
}

// ── GET /admin/api/users ────────────────────────────────────────────────────

// ListUsers retourne la liste de tous les utilisateurs avec leurs statuts
// enrichis depuis Jellyfin.
func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	slog.Info("Liste des utilisateurs demandée", "admin", sess.Username)

	// ── 1. Récupérer les utilisateurs depuis SQLite ─────────────────────
	rows, err := h.db.Query(
		`SELECT id, jellyfin_id, username, email, ldap_dn, invited_by,
		        group_name, is_active, is_banned, can_invite, access_expires_at, delete_at,
		        expiry_action, expiry_delete_after_days, expired_at,
		        created_at, updated_at
		 FROM users ORDER BY created_at DESC`)
	if err != nil {
		slog.Error("Erreur lecture des utilisateurs", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de lecture de la base de données",
		})
		return
	}
	defer rows.Close()

	var users []UserResponse
	for rows.Next() {
		var u UserResponse
		var jellyfinID, email, ldapDN, invitedBy, groupName sql.NullString
		var accessExpiresAt, deleteAt, expiryAction, expiredAt, createdAt, updatedAt sql.NullString
		var deleteAfterDays sql.NullInt64

		err := rows.Scan(
			&u.ID, &jellyfinID, &u.Username, &email, &ldapDN, &invitedBy, &groupName,
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
		u.LdapDN = ldapDN.String
		u.InvitedBy = invitedBy.String
		u.GroupName = groupName.String
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

	// ── 2. Enrichir avec le statut Jellyfin en temps réel ───────────────
	// On fait un seul appel pour récupérer tous les utilisateurs Jellyfin,
	// puis on fusionne par ID pour éviter N requêtes individuelles.
	jfUsers, err := h.jfClient.GetUsers()
	if err != nil {
		slog.Warn("Impossible de récupérer les utilisateurs Jellyfin (enrichissement dégradé)",
			"error", err,
		)
		// On continue sans enrichissement — les données SQLite suffisent
	} else {
		// Construire un index par ID Jellyfin
		jfIndex := make(map[string]*jellyfin.User, len(jfUsers))
		for i := range jfUsers {
			jfIndex[jfUsers[i].ID] = &jfUsers[i]
		}

		// Fusionner
		for i := range users {
			if jfUser, ok := jfIndex[users[i].JellyfinID]; ok {
				users[i].JellyfinExists = true
				users[i].JellyfinDisabled = jfUser.Policy.IsDisabled
			}
		}
	}

	slog.Info("Liste des utilisateurs renvoyée", "count", len(users))

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    users,
	})
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
			At:      normalizeTimelineAt(createdAt.String),
			Action:  action.String,
			Actor:   actor.String,
			Target:  target.String,
			Details: details.String,
			Message: describeTimelineAction(action.String, actor.String, target.String, details.String),
		})
	}

	if rec.CreatedAt.Valid {
		events = append(events, UserTimelineEvent{
			At:      normalizeTimelineAt(rec.CreatedAt.String),
			Action:  "user.created",
			Target:  rec.Username,
			Message: "Compte cree",
		})
	}

	if rec.AccessExpiresAt.Valid {
		details := fmt.Sprintf("Policy=%s", normalizeExpiryAction(rec.ExpiryAction))
		if rec.DeleteAfterDays > 0 {
			details = fmt.Sprintf("%s, delete_after_days=%d", details, rec.DeleteAfterDays)
		}
		events = append(events, UserTimelineEvent{
			At:      normalizeTimelineAt(rec.AccessExpiresAt.String),
			Action:  "user.expiry.scheduled",
			Target:  rec.Username,
			Details: details,
			Message: "Expiration planifiee",
		})
	}

	if rec.ExpiredAt.Valid {
		events = append(events, UserTimelineEvent{
			At:      normalizeTimelineAt(rec.ExpiredAt.String),
			Action:  "user.expired",
			Target:  rec.Username,
			Message: "Compte marque comme expire",
		})
	}

	sort.Slice(events, func(i, j int) bool {
		return timelineSortKey(events[i].At).After(timelineSortKey(events[j].At))
	})

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: events})
}

func (h *AdminHandler) loadAdminUserByID(userID int64) (*adminUserRecord, error) {
	var rec adminUserRecord
	var email, jellyfinID, ldapDN, groupName, discordContact, telegramContact sql.NullString

	err := h.db.QueryRow(
		`SELECT id, username, email, jellyfin_id, ldap_dn, group_name, is_active, can_invite,
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
	rec.LdapDN = ldapDN.String
	rec.GroupName = groupName.String
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
	case "invite.used":
		return "Inscription realisee via invitation"
	case "user.updated":
		return "Profil utilisateur mis a jour"
	case "user.enabled":
		return "Compte active"
	case "user.disabled":
		return "Compte desactive"
	case "user.deleted":
		return "Compte supprime"
	case "user.expired":
		return "Compte expire automatiquement"
	case "user.expired.deleted":
		return "Compte supprime automatiquement apres expiration"
	case "reset.requested":
		return "Demande de reinitialisation mot de passe"
	case "reset.success":
		return "Mot de passe reinitialise"
	case "reset.partial":
		return "Mot de passe partiellement reinitialise"
	case "reset.failed.total":
		return "Echec complet de reinitialisation mot de passe"
	case "reset.completed":
		return "Mot de passe reinitialise"
	case "reset.sent.admin":
		return "Lien de reinitialisation envoye par un admin"
	case "user.password.change":
		return "Mot de passe change depuis My Account"
	case "user.can_invite.toggle":
		return "Droit de parrainage mis a jour"
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
			return fmt.Errorf("max_active_sessions doit être >= 0")
		}
		policy.MaxActiveSessions = *patch.MaxActiveSession
	}
	if patch.BitrateLimit != nil {
		if *patch.BitrateLimit < 0 {
			return fmt.Errorf("remote_bitrate_limit doit être >= 0")
		}
		policy.RemoteClientBitrateLimit = *patch.BitrateLimit
	}

	if err := h.jfClient.SetUserPolicy(jellyfinID, policy); err != nil {
		return fmt.Errorf("mise à jour de la politique Jellyfin: %w", err)
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

	preset, err := h.getJellyfinPresetByID(mapping.PolicyPresetID)
	if err != nil {
		return err
	}

	patch := &BulkJellyfinPolicyPatch{
		EnableDownloads:  &preset.EnableDownload,
		EnableRemote:     &preset.EnableRemoteAccess,
		MaxActiveSession: &preset.MaxSessions,
		BitrateLimit:     &preset.BitrateLimit,
	}

	if err := h.applyJellyfinPolicyPatch(rec.JellyfinID, patch); err != nil {
		return err
	}

	if mapping.Source == "ldap" && h.ldClient != nil && strings.TrimSpace(rec.LdapDN) != "" && strings.TrimSpace(mapping.LDAPGroupDN) != "" {
		if err := h.ldClient.AddUserToGroup(rec.LdapDN, mapping.LDAPGroupDN); err != nil {
			return fmt.Errorf("assignation groupe ldap: %w", err)
		}
	}

	return nil
}

func (h *AdminHandler) setUserActiveState(rec *adminUserRecord, newActive bool, actor string) ([]string, error) {
	var partialErrors []string

	if h.ldClient != nil && rec.LdapDN != "" {
		var err error
		if newActive {
			err = h.ldClient.EnableUser(rec.LdapDN)
		} else {
			err = h.ldClient.DisableUser(rec.LdapDN)
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

	if h.ldClient != nil && rec.LdapDN != "" {
		if err := h.ldClient.DeleteUser(rec.LdapDN); err != nil {
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
		return fmt.Errorf("SMTP non configuré")
	}
	if strings.TrimSpace(rec.Email) == "" {
		return fmt.Errorf("utilisateur sans email")
	}

	token, err := generateSecureToken(resetTokenLength)
	if err != nil {
		return fmt.Errorf("génération du token: %w", err)
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

	resetURL := fmt.Sprintf("%s/reset/%s", strings.TrimRight(h.cfg.BaseURL, "/"), token)
	mailCfg, _ := h.db.GetEmailTemplatesConfig()
	tpl := mailCfg.PasswordReset
	if tpl == "" {
		tpl = "Bonjour {{.Username}},\n\nVoici votre lien de réinitialisation de mot de passe : {{.ResetLink}}"
	}

	data := map[string]string{
		"Username":  rec.Username,
		"ResetLink": resetURL,
		"ResetURL":  resetURL,
		"ResetCode": token,
		"ExpiresIn": "15 minutes",
	}
	links := resolvePortalLinks(h.cfg, h.db)
	data["JellyfinURL"] = links.JellyfinURL
	data["JellyseerrURL"] = links.JellyseerrURL
	data["JellyTulliURL"] = links.JellyTulliURL

	if err := h.mailer.SendTemplateString(rec.Email, "Réinitialisation de votre mot de passe — JellyGate", tpl, data); err != nil {
		return fmt.Errorf("envoi de l'email: %w", err)
	}

	_ = h.db.LogAction("reset.sent.admin", actor, rec.Username, fmt.Sprintf(`{"user_id":%d}`, rec.ID))
	return nil
}

// UpdateUser met à jour les informations éditables d'un utilisateur (email, parrainage, expiration).
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
		 SET email = ?, group_name = ?, can_invite = ?, access_expires_at = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		email,
		groupName,
		canInvite,
		accessExpiresAt,
		userID,
	)
	if err != nil {
		slog.Error("Erreur mise à jour utilisateur", "id", userID, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de mise à jour"})
		return
	}

	_ = h.db.LogAction("user.updated", sess.Username, rec.Username,
		fmt.Sprintf(`{"user_id":%d,"email":"%s","group_name":"%s","can_invite":%t}`, userID, email, groupName, canInvite))

	if req.GroupName != nil {
		rec.GroupName = groupName
		if err := h.applyGroupMappingToUser(rec, groupName); err != nil {
			slog.Warn("Application mapping groupe echouee", "user", rec.Username, "group", groupName, "error", err)
			_ = h.db.LogAction("user.group_mapping.failed", sess.Username, rec.Username, err.Error())
		}
	}

	if oldExpiry != newExpiry {
		rec.Email = email
		rec.AccessExpiresAt = sql.NullString{String: newExpiry, Valid: newExpiry != ""}
		if err := h.sendUserTemplateByKey(rec, "expiry_adjusted", map[string]string{"ExpiryDate": newExpiry}); err != nil {
			slog.Error("Erreur envoi email expiry_adjusted", "user", rec.Username, "error", err)
		}
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Utilisateur mis à jour",
		Data: map[string]interface{}{
			"id":                userID,
			"email":             email,
			"group_name":        groupName,
			"can_invite":        canInvite,
			"access_expires_at": accessExpiresAt,
		},
	})
}

// SendUserPasswordReset crée et envoie un lien de réinitialisation à l'utilisateur ciblé.
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

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Email de réinitialisation envoyé"})
}

// BulkUsersAction applique une action de masse sur les utilisateurs sélectionnés.
func (h *AdminHandler) BulkUsersAction(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())

	var req BulkUsersActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}

	if len(req.UserIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Aucun utilisateur sélectionné"})
		return
	}

	action := strings.TrimSpace(req.Action)
	if action == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Action manquante"})
		return
	}

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
			if h.mailer == nil {
				entry["success"] = false
				entry["message"] = "SMTP non configuré"
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
			entry["message"] = "Email envoyé"

		case "jellyfin_policy":
			err := h.applyJellyfinPolicyPatch(rec.JellyfinID, req.JellyfinPolicy)
			if err != nil {
				entry["success"] = false
				entry["message"] = err.Error()
				break
			}

			_ = h.db.LogAction("user.bulk.jellyfin_policy", sess.Username, rec.Username, fmt.Sprintf(`{"user_id":%d}`, rec.ID))
			entry["success"] = true
			entry["message"] = "Paramètres Jellyfin mis à jour"

		case "apply_preset":
			preset, err := h.getJellyfinPresetByID(req.PolicyPresetID)
			if err != nil {
				entry["success"] = false
				entry["message"] = err.Error()
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
			entry["message"] = "Preset Jellyfin appliqué"

		case "set_parrainage":
			if req.CanInvite == nil {
				entry["success"] = false
				entry["message"] = "can_invite manquant"
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
			entry["message"] = "Parrainage mis à jour"

		case "set_expiry":
			var expiry interface{}
			emailExpiryDate := "Aucune expiration"
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
				emailExpiryDate = emailTime(exp)
			}

			_, err := h.db.Exec(`UPDATE users SET access_expires_at = ?, updated_at = datetime('now') WHERE id = ?`, expiry, rec.ID)
			if err != nil {
				entry["success"] = false
				entry["message"] = "Erreur SQLite"
				break
			}

			_ = h.db.LogAction("user.bulk.expiry", sess.Username, rec.Username, "")
			if err := h.sendUserTemplateByKey(rec, "expiry_adjusted", map[string]string{"ExpiryDate": emailExpiryDate}); err != nil {
				slog.Error("Erreur envoi email bulk expiry_adjusted", "user", rec.Username, "error", err)
				entry["success"] = true
				entry["message"] = "Expiration mise à jour (email non envoyé)"
				break
			}
			entry["success"] = true
			entry["message"] = "Expiration mise à jour"

		case "activate", "deactivate":
			newState := action == "activate"
			partials, err := h.setUserActiveState(rec, newState, sess.Username)
			if err != nil {
				entry["success"] = false
				entry["message"] = strings.Join(partials, " | ")
				break
			}

			entry["success"] = true
			if len(partials) > 0 {
				entry["message"] = "Action appliquée avec erreurs partielles: " + strings.Join(partials, " | ")
			} else if newState {
				entry["message"] = "Utilisateur activé"
			} else {
				entry["message"] = "Utilisateur désactivé"
			}

		case "send_password_reset":
			err := h.sendPasswordResetForUser(rec, sess.Username)
			if err != nil {
				entry["success"] = false
				entry["message"] = err.Error()
				break
			}

			entry["success"] = true
			entry["message"] = "Lien de réinitialisation envoyé"

		case "delete":
			partials, err := h.deleteUserRecord(rec, sess.Username)
			if err != nil {
				entry["success"] = false
				entry["message"] = strings.Join(partials, " | ")
				break
			}

			entry["success"] = true
			if len(partials) > 0 {
				entry["message"] = "Supprimé avec erreurs partielles: " + strings.Join(partials, " | ")
			} else {
				entry["message"] = "Utilisateur supprimé"
			}

		default:
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Action de masse non supportée"})
			return
		}

		if ok, _ := entry["success"].(bool); ok {
			successCount++
		}
		results = append(results, entry)
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: fmt.Sprintf("Action de masse terminée: %d/%d succès", successCount, len(req.UserIDs)),
		Data: map[string]interface{}{
			"total":   len(req.UserIDs),
			"success": successCount,
			"results": results,
		},
	})
}

// ── POST /admin/api/users/{id}/toggle ───────────────────────────────────────

// ToggleUser active ou désactive un utilisateur simultanément dans l'AD
// et dans Jellyfin, puis met à jour le statut SQLite.
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
			Message: "Erreur de lecture de la base de données",
		})
		return
	}

	newActive := !rec.IsActive
	partialErrors, err := h.setUserActiveState(rec, newActive, sess.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur lors de la mise à jour du statut utilisateur",
			Errors:  partialErrors,
			Data: map[string]interface{}{
				"id":        rec.ID,
				"username":  rec.Username,
				"is_active": rec.IsActive,
			},
		})
		return
	}

	action := "activé"
	if !newActive {
		action = "désactivé"
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
		Message: fmt.Sprintf("Utilisateur %q %s avec succès", rec.Username, action),
		Data: map[string]interface{}{
			"id":        rec.ID,
			"username":  rec.Username,
			"is_active": newActive,
		},
	})
}

// ── POST /admin/api/users/{id}/invite-toggle ────────────────────────────────

// ToggleUserInvite active ou désactive le droit de créer des invitations pour un utilisateur.
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

	actionTxt := "activé"
	if !newStatus {
		actionTxt = "désactivé"
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

// ── POST /admin/api/users/me/password ───────────────────────────────────────

// ChangeMyPassword permet à l'utilisateur connecté de modifier son propre mot de passe.
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
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Le nouveau mot de passe doit faire au moins 8 caractères"})
		return
	}

	// Récupérer le DN LDAP depuis SQLite
	var ldapDN sql.NullString
	err := h.db.QueryRow(`SELECT ldap_dn FROM users WHERE jellyfin_id = ?`, sess.UserID).Scan(&ldapDN)
	if err != nil && err != sql.ErrNoRows {
		slog.Error("Erreur lecture ldap_dn pour changement MDP", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur base de données"})
		return
	}

	// Le changement s'effectue sur Jellyfin
	// (Note: l'API Jellyfin demande d'avoir l'ancien mot de passe pour les non-admins, ou un reset d'admin.
	// Ici nous utilisons un workaround: on authentifie via un webhook/API ? Non, change password endpoint direct.)
	// Pour simplifier dans l'exemple, on force le changement via le LDClient si dispo, puis le JfClient auth en tant qu'admin
	var partialErrors []string

	// 1. LDAP (Si activé)
	if h.ldClient != nil && ldapDN.String != "" {
		if err := h.ldClient.ResetPassword(ldapDN.String, req.NewPassword); err != nil {
			partialErrors = append(partialErrors, fmt.Sprintf("LDAP: %v", err))
		}
	}

	// 2. Jellyfin (en passant par le token de l'APi admin configurée)
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

	_ = h.db.LogAction("user.password.change", sess.Username, sess.Username, "L'utilisateur a changé son mot de passe depuis le tableau de bord")

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Mot de passe modifié avec succès",
	})
}

// ── DELETE /admin/api/users/{id} ────────────────────────────────────────────

// DeleteUser supprime un utilisateur de l'AD, de Jellyfin, puis de SQLite.
// Les erreurs partielles (ex: utilisateur déjà supprimé de l'AD) ne bloquent
// pas les suppressions restantes — tout est loggé.
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
			Message: "Erreur de lecture de la base de données",
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

	msg := fmt.Sprintf("Utilisateur %q supprimé avec succès", rec.Username)
	if len(partialErrors) > 0 {
		msg = fmt.Sprintf("Utilisateur %q supprimé avec %d erreur(s) partielle(s)", rec.Username, len(partialErrors))
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

// ── GET /admin/api/invitations ──────────────────────────────────────────────

// InvitationResponse représente une invitation formatée pour l'API JSON.
type InvitationResponse struct {
	ID              int64                  `json:"id"`
	Code            string                 `json:"code"`
	Label           string                 `json:"label"`
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
	} else if r.TLS != nil {
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

// ListInvitations retourne toutes les invitations SQLite.
func (h *AdminHandler) ListInvitations(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	slog.Info("Liste des invitations demandée", "admin", sess.Username)

	var query string
	var args []interface{}

	if sess.IsAdmin {
		query = `SELECT id, code, label, max_uses, used_count, jellyfin_profile, expires_at, created_by, created_at FROM invitations ORDER BY created_at DESC`
	} else {
		query = `SELECT id, code, label, max_uses, used_count, jellyfin_profile, expires_at, created_by, created_at FROM invitations WHERE created_by = ? ORDER BY created_at DESC`
		args = append(args, sess.Username)
	}

	rows, err := h.db.Query(query, args...)
	if err != nil {
		slog.Error("Erreur lecture des invitations", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de lecture de la base de données",
		})
		return
	}
	defer rows.Close()

	var invs []InvitationResponse
	for rows.Next() {
		var i InvitationResponse
		var label, profile, createdBy sql.NullString
		var rawExpiresAt interface{}
		var rawCreatedAt interface{}

		err := rows.Scan(
			&i.ID, &i.Code, &label, &i.MaxUses, &i.UsedCount,
			&profile, &rawExpiresAt, &createdBy, &rawCreatedAt,
		)
		if err != nil {
			slog.Error("Erreur scan invitation", "error", err)
			continue
		}

		i.Label = label.String
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

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    invs,
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

// ── POST /admin/api/invitations ─────────────────────────────────────────────

// CreateInvitationRequest payload pour la création d'invitation
type CreateInvitationRequest struct {
	Label            string   `json:"label"`
	MaxUses          int      `json:"max_uses"`   // 0 = illimité
	ExpiresAt        string   `json:"expires_at"` // Date précise, exemple "2026-10-05T12:00"
	ApplyUserExpiry  *bool    `json:"apply_user_expiry"`
	UserExpiryDays   int      `json:"user_expiry_days"` // Expiration finale du compte client (jours)
	UserExpiresAt    string   `json:"user_expires_at"`
	DisableAfterDays int      `json:"disable_after_days"`
	NewUserCanInvite bool     `json:"new_user_can_invite"`
	SendToEmail      string   `json:"send_to_email"` // Si renseigné, un e-mail partira par SMTP
	EmailMessage     string   `json:"email_message"`
	Libraries        []string `json:"libraries"` // ID des bibliothèques Jellyfin
	EnableDownloads  bool     `json:"enable_downloads"`
	PolicyPresetID   string   `json:"policy_preset_id"`
	GroupName        string   `json:"group_name"`
	ForcedUsername   string   `json:"forced_username"`
	TemplateUserID   string   `json:"template_user_id"`
	UsernameMinLen   *int     `json:"username_min_length"`
	UsernameMaxLen   *int     `json:"username_max_length"`
	PasswordMinLen   *int     `json:"password_min_length"`
	PasswordMaxLen   *int     `json:"password_max_length"`
	RequireUpper     *bool    `json:"password_require_upper"`
	RequireLower     *bool    `json:"password_require_lower"`
	RequireDigit     *bool    `json:"password_require_digit"`
	RequireSpecial   *bool    `json:"password_require_special"`
	ExpiryAction     string   `json:"expiry_action"`
	DeleteAfterDays  *int     `json:"delete_after_days"`
}

// CreateInvitation crée un nouveau lien d'invitation avec un jeton robuste et logiques complexes (JFA-GO).
func (h *AdminHandler) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if !sess.IsAdmin {
		var canInvite bool
		_ = h.db.QueryRow(`SELECT can_invite FROM users WHERE jellyfin_id = ?`, sess.UserID).Scan(&canInvite)
		if !canInvite {
			writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Vous n'avez pas l'autorisation de créer des invitations"})
			return
		}
	}

	var req CreateInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}
	if req.MaxUses < 0 {
		req.MaxUses = 0
	}

	// Générer code aléatoire (ici via crypt/rand classique, 12 caractères)
	code, err := generateSecureToken(12)
	if err != nil {
		slog.Error("Erreur génération token d'invitation", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible de générer un code d'invitation"})
		return
	}

	// Calculer expiration du lien
	var expiresAt interface{}
	inviteExpiryDate := ""
	if strings.TrimSpace(req.ExpiresAt) != "" {
		parsed, parseErr := parseInvitationDateTimeInput(req.ExpiresAt)
		if parseErr != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Format d'expiration du lien invalide"})
			return
		}
		expiresAt = parsed
		inviteExpiryDate = emailTime(parsed)
	}

	inviteCfg, err := h.db.GetInvitationProfileConfig()
	if err != nil {
		slog.Error("Erreur chargement config profil invitation", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de lecture du profil d'invitation"})
		return
	}

	now := time.Now()
	var effectiveUserExpiresAt time.Time
	if req.ApplyUserExpiry != nil && *req.ApplyUserExpiry && strings.TrimSpace(req.UserExpiresAt) != "" {
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

	if !sess.IsAdmin {
		if inviteCfg.InviterQuotaDay > 0 {
			usedToday, countErr := h.countInvitationsCreatedSince(sess.Username, startOfLocalDay(now))
			if countErr != nil {
				slog.Error("Erreur calcul quota jour", "user", sess.Username, "error", countErr)
				writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible de verifier le quota journalier"})
				return
			}
			if usedToday >= inviteCfg.InviterQuotaDay {
				writeJSON(w, http.StatusTooManyRequests, APIResponse{Success: false, Message: fmt.Sprintf("Quota journalier atteint (%d/%d)", usedToday, inviteCfg.InviterQuotaDay)})
				return
			}
		}

		if inviteCfg.InviterQuotaWeek > 0 {
			usedWeek, countErr := h.countInvitationsCreatedSince(sess.Username, startOfLocalWeek(now))
			if countErr != nil {
				slog.Error("Erreur calcul quota semaine", "user", sess.Username, "error", countErr)
				writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible de verifier le quota hebdomadaire"})
				return
			}
			if usedWeek >= inviteCfg.InviterQuotaWeek {
				writeJSON(w, http.StatusTooManyRequests, APIResponse{Success: false, Message: fmt.Sprintf("Quota hebdomadaire atteint (%d/%d)", usedWeek, inviteCfg.InviterQuotaWeek)})
				return
			}
		}

		if inviteCfg.InviterQuotaMonth > 0 {
			usedMonth, countErr := h.countInvitationsCreatedSince(sess.Username, startOfLocalMonth(now))
			if countErr != nil {
				slog.Error("Erreur calcul quota mois", "user", sess.Username, "error", countErr)
				writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible de verifier le quota mensuel"})
				return
			}
			if usedMonth >= inviteCfg.InviterQuotaMonth {
				writeJSON(w, http.StatusTooManyRequests, APIResponse{Success: false, Message: fmt.Sprintf("Quota mensuel atteint (%d/%d)", usedMonth, inviteCfg.InviterQuotaMonth)})
				return
			}
		}

		if req.NewUserCanInvite && !inviteCfg.AllowInviterGrant {
			writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "La delegation du droit d'invitation est reservee par la configuration admin"})
			return
		}

		if req.ApplyUserExpiry != nil && *req.ApplyUserExpiry && !inviteCfg.AllowInviterUserExpiry {
			writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "La personnalisation de l'expiration utilisateur est desactivee pour les parrains"})
			return
		}

		if inviteCfg.InviterMaxUses > 0 {
			if req.MaxUses <= 0 || req.MaxUses > inviteCfg.InviterMaxUses {
				writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: fmt.Sprintf("Limite par lien: entre 1 et %d utilisations pour les parrains", inviteCfg.InviterMaxUses)})
				return
			}
		}

		if inviteCfg.InviterMaxLinkHours > 0 && strings.TrimSpace(req.ExpiresAt) != "" {
			maxExpiry := now.Add(time.Duration(inviteCfg.InviterMaxLinkHours) * time.Hour)
			parsedExpiry, parseErr := parseInvitationDateTimeInput(req.ExpiresAt)
			if parseErr == nil && parsedExpiry.After(maxExpiry) {
				writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: fmt.Sprintf("Expiration du lien trop lointaine (max %d heures pour les parrains)", inviteCfg.InviterMaxLinkHours)})
				return
			}
		}

		if inviteCfg.InviterMaxLinkHours > 0 && strings.TrimSpace(req.ExpiresAt) == "" {
			autoExpiry := now.Add(time.Duration(inviteCfg.InviterMaxLinkHours) * time.Hour)
			expiresAt = autoExpiry
			inviteExpiryDate = emailTime(autoExpiry)
		}
	}

	effectiveDisableAfterDays := inviteCfg.DisableAfterDays
	if req.ApplyUserExpiry != nil {
		if *req.ApplyUserExpiry {
			if effectiveUserExpiresAt.IsZero() {
				overrideDays := req.DisableAfterDays
				if overrideDays <= 0 {
					overrideDays = req.UserExpiryDays
				}
				if overrideDays <= 0 {
					writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Renseigne un nombre de jours valide ou une date absolue pour l'expiration utilisateur"})
					return
				}
				effectiveDisableAfterDays = overrideDays
			} else {
				effectiveDisableAfterDays = 0
			}
		} else {
			effectiveDisableAfterDays = 0
			effectiveUserExpiresAt = time.Time{}
		}
	}

	effectiveCanInvite := false
	if sess.IsAdmin {
		effectiveCanInvite = req.NewUserCanInvite
	} else if req.NewUserCanInvite && inviteCfg.AllowInviterGrant {
		effectiveCanInvite = true
	}

	effectiveUserExpiresAtRaw := ""
	if !effectiveUserExpiresAt.IsZero() {
		effectiveUserExpiresAtRaw = effectiveUserExpiresAt.Format(time.RFC3339)
	}

	// Construire profil Jellyfin depuis la configuration admin (paramètres globaux).
	jfProfile := jellyfin.InviteProfile{
		EnableAllFolders:       len(req.Libraries) == 0,
		EnabledFolderIDs:       req.Libraries,
		EnableDownload:         inviteCfg.EnableDownloads,
		RequireEmail:           inviteCfg.RequireEmail,
		EnableRemoteAccess:     true,
		UserExpiryDays:         effectiveDisableAfterDays,
		UserExpiresAt:          effectiveUserExpiresAtRaw,
		DisableAfterDays:       effectiveDisableAfterDays,
		GroupName:              strings.TrimSpace(req.GroupName),
		ForcedUsername:         strings.TrimSpace(req.ForcedUsername),
		CanInvite:              effectiveCanInvite,
		TemplateUserID:         strings.TrimSpace(inviteCfg.TemplateUserID),
		UsernameMinLength:      inviteCfg.UsernameMinLength,
		UsernameMaxLength:      inviteCfg.UsernameMaxLength,
		PasswordMinLength:      inviteCfg.PasswordMinLength,
		PasswordMaxLength:      inviteCfg.PasswordMaxLength,
		PasswordRequireUpper:   inviteCfg.PasswordRequireUpper,
		PasswordRequireLower:   inviteCfg.PasswordRequireLower,
		PasswordRequireDigit:   inviteCfg.PasswordRequireDigit,
		PasswordRequireSpecial: inviteCfg.PasswordRequireSpecial,
		ExpiryAction:           normalizeExpiryAction(inviteCfg.ExpiryAction),
		DeleteAfterDays:        inviteCfg.DeleteAfterDays,
	}

	if strings.TrimSpace(inviteCfg.PolicyPresetID) != "" {
		preset, err := h.getJellyfinPresetByID(inviteCfg.PolicyPresetID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Preset Jellyfin introuvable"})
			return
		}

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

	// Les options exposees dans "Profil utilisateur" sont forcees par les paramètres admin.
	jfProfile.EnableDownload = inviteCfg.EnableDownloads
	jfProfile.RequireEmail = inviteCfg.RequireEmail
	if strings.TrimSpace(inviteCfg.TemplateUserID) != "" {
		jfProfile.TemplateUserID = strings.TrimSpace(inviteCfg.TemplateUserID)
	}
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
	jfProfile.ExpiryAction = normalizeExpiryAction(inviteCfg.ExpiryAction)
	jfProfile.DeleteAfterDays = inviteCfg.DeleteAfterDays
	jfProfile.CanInvite = effectiveCanInvite

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
		`INSERT INTO invitations (code, label, max_uses, jellyfin_profile, expires_at, created_by)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		code, req.Label, req.MaxUses, string(profileJSON), expiresAt, sess.Username,
	)

	if err != nil {
		slog.Error("Erreur création invitation DB", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur d'insertion BD"})
		return
	}

	h.db.LogAction("invite.created", sess.Username, req.Label, fmt.Sprintf("Code: %s", code))

	// Envoi SMTP si demandé
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
	if sendToEmail != "" {
		if h.mailer != nil {
			customMessage := strings.TrimSpace(req.EmailMessage)
			inviteeName := strings.TrimSpace(req.ForcedUsername)
			if inviteeName == "" {
				inviteeName = "invité"
			}

			go func(recipient, username, expiryDate, customBody string) {
				emailCfg, _ := h.db.GetEmailTemplatesConfig()
				combinedTemplate := joinTemplateSections(
					emailCfg.Invitation,
					emailCfg.InviteExpiry,
					emailCfg.PreSignupHelp,
				)

				if strings.TrimSpace(customBody) != "" {
					combinedTemplate = joinTemplateSections(combinedTemplate, "{{.Message}}")
				}

				if combinedTemplate == "" {
					combinedTemplate = "Bonjour,\n\nVous êtes invité à rejoindre notre serveur. Cliquez sur ce lien pour créer votre compte : {{.InviteLink}}"
				}

				emailData := map[string]string{
					"InviteLink":    inviteURL,
					"InviteURL":     inviteURL,
					"InviteCode":    code,
					"HelpURL":       strings.TrimRight(baseURL, "/"),
					"JellyGateURL":  strings.TrimRight(baseURL, "/"),
					"Username":      username,
					"JellyfinURL":   links.JellyfinURL,
					"JellyseerrURL": links.JellyseerrURL,
					"JellyTulliURL": links.JellyTulliURL,
				}
				if expiryDate != "" {
					emailData["ExpiryDate"] = expiryDate
				} else {
					emailData["ExpiryDate"] = "Non définie"
				}
				if strings.TrimSpace(customBody) != "" {
					emailData["Message"] = customBody
				}

				errMail := sendTemplateIfConfigured(h.mailer, recipient, "Invitation à rejoindre JellyGate", combinedTemplate, emailData)
				if errMail != nil {
					slog.Error("Erreur d'envoi SMTP (Invitation)", "email", recipient, "error", errMail)
					_ = h.db.LogAction("invite.email.failed", sess.Username, code, errMail.Error())
				}
			}(sendToEmail, inviteeName, inviteExpiryDate, customMessage)
		} else {
			slog.Warn("Option e-mail cochée pour l'invitation, mais le serveur SMTP n'est pas configuré.")
		}
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Invitation générée avec succès",
		Data: map[string]interface{}{
			"code": code,
			"url":  inviteURL,
		},
	})
}

// ── DELETE /admin/api/invitations/{id} ──────────────────────────────────────

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
		Message: "Lien d'invitation détruit",
	})
}

// writeJSON écrit une réponse JSON avec le code HTTP donné.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		slog.Error("Erreur d'encodage JSON", "error", err)
	}
}
