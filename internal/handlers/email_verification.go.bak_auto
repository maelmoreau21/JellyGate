package handlers

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/mail"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

const (
	emailVerificationTokenLength    = 32
	emailVerificationExpiry         = 24 * time.Hour
	emailVerificationResendCooldown = 2 * time.Minute
)

type emailVerificationTarget struct {
	UserID             int64
	Username           string
	Email              string
	PendingEmail       string
	EmailVerified      bool
	VerificationSentAt sql.NullString
}

type emailVerificationRecord struct {
	ID        int64
	UserID    int64
	Email     string
	Code      string
	Used      bool
	ExpiresAt time.Time
}

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

func defaultEmailVerificationTemplate() string {
	return `<h2 style="margin:0 0 14px 0;font-size:22px;color:#0f172a;">Verify your email address</h2>
<p>Hello <strong>{{.Username}}</strong>,</p>
<p>Please confirm your email address to finish securing your Jellyfin access.</p>
<p style="margin:20px 0;"><a href="{{.VerificationLink}}" style="display:inline-block;background:#0ea5e9;color:#ffffff;text-decoration:none;padding:12px 18px;border-radius:8px;font-weight:600;">Verify my email</a></p>
<p style="font-size:13px;color:#475569;">Verification link: {{.VerificationURL}}</p>
<p style="font-size:13px;color:#475569;">Code: <strong>{{.VerificationCode}}</strong> Â· Expires in {{.ExpiresIn}}</p>`
}

func defaultEmailVerificationSubject() string {
	return "Verify your email address for Jellyfin"
}

func loadEmailVerificationTarget(db *database.DB, userID int64) (*emailVerificationTarget, error) {
	var target emailVerificationTarget
	var email, pendingEmail sql.NullString
	err := db.QueryRow(
		`SELECT id, username, email, pending_email, email_verified, email_verification_sent_at
		 FROM users
		 WHERE id = ?`,
		userID,
	).Scan(&target.UserID, &target.Username, &email, &pendingEmail, &target.EmailVerified, &target.VerificationSentAt)
	if err != nil {
		return nil, err
	}
	target.Email = strings.TrimSpace(email.String)
	target.PendingEmail = strings.TrimSpace(pendingEmail.String)
	return &target, nil
}

func effectiveVerificationEmail(target *emailVerificationTarget) string {
	if target == nil {
		return ""
	}
	if strings.TrimSpace(target.PendingEmail) != "" {
		return strings.TrimSpace(target.PendingEmail)
	}
	return strings.TrimSpace(target.Email)
}

func requiresEmailVerification(target *emailVerificationTarget) bool {
	if target == nil {
		return false
	}
	if strings.TrimSpace(target.PendingEmail) != "" {
		return true
	}
	return strings.TrimSpace(target.Email) != "" && !target.EmailVerified
}

func canResendVerification(target *emailVerificationTarget) (bool, time.Duration) {
	if target == nil || !target.VerificationSentAt.Valid || strings.TrimSpace(target.VerificationSentAt.String) == "" {
		return true, 0
	}
	sentAt, err := parseAccessExpiry(target.VerificationSentAt.String)
	if err != nil {
		return true, 0
	}
	remaining := emailVerificationResendCooldown - time.Since(sentAt)
	if remaining <= 0 {
		return true, 0
	}
	return false, remaining
}

func sendVerificationEmailTemplate(cfg *config.Config, db *database.DB, mailer *mail.Mailer, username, address, token string) error {
	if mailer == nil {
		return fmt.Errorf("SMTP non configurÃ©")
	}

	links := resolvePortalLinks(cfg, db)
	publicBaseURL := strings.TrimRight(strings.TrimSpace(links.JellyGateURL), "/")
	if publicBaseURL == "" && cfg != nil {
		publicBaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	}
	verificationURL := publicBaseURL + "/verify-email/" + token

	emailData := map[string]string{
		"Username":         username,
		"DisplayName":      username,
		"Email":            address,
		"VerificationLink": verificationURL,
		"VerificationURL":  verificationURL,
		"VerificationCode": token,
		"ExpiresIn":        "24 hours",
		"HelpURL":          publicBaseURL,
		"JellyGateURL":     publicBaseURL,
		"JellyfinURL":      links.JellyfinURL,
		"JellyseerrURL":    links.JellyseerrURL,
	}

	templateBody := defaultEmailVerificationTemplate()
	templateSubject := defaultEmailVerificationSubject()
	emailCfg := config.DefaultEmailTemplates()
	if savedEmailCfg, cfgErr := db.GetEmailTemplatesConfig(); cfgErr == nil {
		emailCfg = savedEmailCfg
		if strings.TrimSpace(emailCfg.EmailVerification) != "" {
			templateBody = emailCfg.EmailVerification
		}
		if strings.TrimSpace(emailCfg.EmailVerificationSubject) != "" {
			templateSubject = emailCfg.EmailVerificationSubject
		}
	}

	if err := sendTemplateIfConfigured(mailer, address, templateSubject, "email_verification", templateBody, emailCfg, emailData); err != nil {
		return fmt.Errorf("envoi email verification: %w", err)
	}

	return nil
}

func sendEmailVerification(cfg *config.Config, db *database.DB, mailer *mail.Mailer, userID int64, force bool) error {
	if mailer == nil {
		return fmt.Errorf("SMTP non configurÃ©")
	}

	target, err := loadEmailVerificationTarget(db, userID)
	if err != nil {
		return fmt.Errorf("lecture utilisateur: %w", err)
	}

	address := effectiveVerificationEmail(target)
	if address == "" {
		return fmt.Errorf("aucune adresse email Ã  vÃ©rifier")
	}
	if !requiresEmailVerification(target) {
		return fmt.Errorf("adresse email dÃ©jÃ  vÃ©rifiÃ©e")
	}
	if !force {
		if ok, remaining := canResendVerification(target); !ok {
			seconds := int(remaining.Seconds())
			if seconds < 1 {
				seconds = 1
			}
			return fmt.Errorf("merci d'attendre %d secondes avant un nouvel envoi", seconds)
		}
	}

	token, err := generateSecureToken(emailVerificationTokenLength)
	if err != nil {
		return fmt.Errorf("gÃ©nÃ©ration du token: %w", err)
	}

	now := time.Now()
	expiresAt := now.Add(emailVerificationExpiry)
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("transaction verification email: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`UPDATE email_verifications SET used = TRUE WHERE user_id = ? AND email = ? AND used = FALSE`,
		userID,
		address,
	); err != nil {
		return fmt.Errorf("dÃ©sactivation anciens tokens: %w", err)
	}

	if _, err := tx.Exec(
		`INSERT INTO email_verifications (user_id, email, code, used, expires_at)
		 VALUES (?, ?, ?, FALSE, ?)`,
		userID,
		address,
		token,
		expiresAt.Format("2006-01-02 15:04:05"),
	); err != nil {
		return fmt.Errorf("crÃ©ation token verification: %w", err)
	}

	if _, err := tx.Exec(
		`UPDATE users SET email_verification_sent_at = ?, updated_at = datetime('now') WHERE id = ?`,
		now.Format("2006-01-02 15:04:05"),
		userID,
	); err != nil {
		return fmt.Errorf("mise Ã  jour envoi verification: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("validation verification email: %w", err)
	}

	if err := sendVerificationEmailTemplate(cfg, db, mailer, target.Username, address, token); err != nil {
		return err
	}

	_ = db.LogAction("user.email_verification.sent", target.Username, address, fmt.Sprintf("user_id=%d", userID))
	return nil
}

func loadEmailVerificationRecord(db *database.DB, code string) (*emailVerificationRecord, error) {
	var record emailVerificationRecord
	var expiresAtRaw string
	err := db.QueryRow(
		`SELECT id, user_id, email, code, used, expires_at
		 FROM email_verifications
		 WHERE code = ?`,
		code,
	).Scan(&record.ID, &record.UserID, &record.Email, &record.Code, &record.Used, &expiresAtRaw)
	if err != nil {
		return nil, err
	}
	expiresAt, err := parseAccessExpiry(expiresAtRaw)
	if err != nil {
		return nil, err
	}
	record.ExpiresAt = expiresAt
	return &record, nil
}

func consumeEmailVerification(db *database.DB, code string) (*emailVerificationTarget, string, error) {
	if strings.TrimSpace(code) == "" {
		return nil, "invalid", fmt.Errorf("code vide")
	}

	record, err := loadEmailVerificationRecord(db, code)
	if err == sql.ErrNoRows {
		return nil, "invalid", fmt.Errorf("token introuvable")
	}
	if err != nil {
		return nil, "invalid", fmt.Errorf("lecture token: %w", err)
	}
	if record.Used {
		return nil, "used", fmt.Errorf("token dÃ©jÃ  utilisÃ©")
	}
	if time.Now().After(record.ExpiresAt) {
		return nil, "expired", fmt.Errorf("token expirÃ©")
	}

	target, err := loadEmailVerificationTarget(db, record.UserID)
	if err != nil {
		return nil, "invalid", fmt.Errorf("lecture utilisateur: %w", err)
	}

	resolvedPending := strings.EqualFold(strings.TrimSpace(target.PendingEmail), strings.TrimSpace(record.Email))
	resolvedCurrent := strings.EqualFold(strings.TrimSpace(target.Email), strings.TrimSpace(record.Email))
	if !resolvedPending && !resolvedCurrent {
		return nil, "obsolete", fmt.Errorf("token obsolÃ¨te")
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, "invalid", fmt.Errorf("transaction verification: %w", err)
	}
	defer tx.Rollback()

	if resolvedPending {
		if _, err := tx.Exec(
			`UPDATE users
			 SET email = ?, pending_email = '', email_verified = TRUE, email_verification_sent_at = NULL, updated_at = datetime('now')
			 WHERE id = ?`,
			record.Email,
			record.UserID,
		); err != nil {
			return nil, "invalid", fmt.Errorf("validation email pending: %w", err)
		}
	} else {
		if _, err := tx.Exec(
			`UPDATE users
			 SET email_verified = TRUE, pending_email = '', email_verification_sent_at = NULL, updated_at = datetime('now')
			 WHERE id = ?`,
			record.UserID,
		); err != nil {
			return nil, "invalid", fmt.Errorf("validation email courant: %w", err)
		}
	}

	if _, err := tx.Exec(`UPDATE email_verifications SET used = TRUE WHERE id = ?`, record.ID); err != nil {
		return nil, "invalid", fmt.Errorf("consommation token: %w", err)
	}
	if _, err := tx.Exec(`UPDATE email_verifications SET used = TRUE WHERE user_id = ? AND email = ? AND used = FALSE`, record.UserID, record.Email); err != nil {
		return nil, "invalid", fmt.Errorf("nettoyage tokens: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, "invalid", fmt.Errorf("validation transaction: %w", err)
	}

	updated, err := loadEmailVerificationTarget(db, record.UserID)
	if err != nil {
		return nil, "invalid", fmt.Errorf("relecture utilisateur: %w", err)
	}
	_ = db.LogAction("user.email_verified", updated.Username, record.Email, fmt.Sprintf("user_id=%d", updated.UserID))
	return updated, "success", nil
}

func renderEmailVerificationPage(r *http.Request, w http.ResponseWriter, renderer *render.Engine, lang string, statusCode int, title, heading, message, loginLabel string, links config.PortalLinksConfig) {
	if renderer == nil {
		http.Error(w, message, statusCode)
		return
	}
	td := applyRequestTemplateData(r, renderer.NewTemplateData(lang))
	td.Section = "login"
	td.SuccessMessage = message
	td.Data["ResultTitle"] = title
	td.Data["ResultHeading"] = heading
	td.Data["LoginLabel"] = loginLabel
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	w.WriteHeader(statusCode)
	if err := renderer.Render(w, "verify_email.html", td); err != nil {
		http.Error(w, message, statusCode)
	}
}

func (h *AdminHandler) VerifyEmailPage(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	statusCode := http.StatusOK
	title := h.tr(r, "verify_email_title", "Email verification")
	heading := h.tr(r, "verify_email_success_heading", "Email verified")
	message := h.tr(r, "verify_email_success_message", "Your email address has been confirmed. You can now sign in normally.")

	target, status, err := consumeEmailVerification(h.db, code)
	if err != nil {
		slog.Warn("Verification email Ã©chouÃ©e", "code", code, "status", status, "error", err)
		switch status {
		case "expired":
			statusCode = http.StatusGone
			heading = h.tr(r, "verify_email_expired_heading", "Verification link expired")
			message = h.tr(r, "verify_email_expired_message", "This verification link has expired. Request a new email from your account page.")
		case "used":
			statusCode = http.StatusGone
			heading = h.tr(r, "verify_email_used_heading", "Link already used")
			message = h.tr(r, "verify_email_used_message", "This verification link has already been used. Your email may already be confirmed.")
		case "obsolete":
			statusCode = http.StatusGone
			heading = h.tr(r, "verify_email_obsolete_heading", "Verification link outdated")
			message = h.tr(r, "verify_email_obsolete_message", "This verification link is no longer valid because a newer email address is pending.")
		default:
			statusCode = http.StatusNotFound
			heading = h.tr(r, "verify_email_invalid_heading", "Invalid verification link")
			message = h.tr(r, "verify_email_invalid_message", "This verification link is invalid or no longer available.")
		}
	} else if target != nil {
		if syncErr := h.syncUserContactToLDAP(target.UserID); syncErr != nil {
			slog.Warn("Synchronisation LDAP apres verification email partielle", "user_id", target.UserID, "error", syncErr)
		}
	}

	renderEmailVerificationPage(
		r,
		w,
		h.renderer,
		jgmw.LangFromContext(r.Context()),
		statusCode,
		title,
		heading,
		message,
		h.tr(r, "back_to_login", "Back to login"),
		resolvePortalLinks(h.cfg, h.db),
	)
}

func (h *AdminHandler) ResendMyEmailVerification(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if err := h.ensureUserRowForSession(sess); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible de prÃ©parer le profil utilisateur"})
		return
	}

	var userID int64
	err := h.db.QueryRow(`SELECT id FROM users WHERE jellyfin_id = ?`, sess.UserID).Scan(&userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Utilisateur introuvable"})
		return
	}

	if err := sendEmailVerification(h.cfg, h.db, h.mailer, userID, false); err != nil {
		message := err.Error()
		statusCode := http.StatusBadRequest
		if strings.Contains(strings.ToLower(message), "attendre") {
			statusCode = http.StatusTooManyRequests
		}
		writeJSON(w, statusCode, APIResponse{Success: false, Message: message})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: h.tr(r, "my_account_email_verification_sent", "A verification email has been sent.")})
}
