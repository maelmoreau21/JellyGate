package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
)

type pendingInviteSignupRecord struct {
	ID                 int64
	Code               string
	InvitationCode     string
	Username           string
	Email              string
	PasswordCiphertext string
	Used               bool
	ExpiresAt          time.Time
}

func loadPendingInviteSignup(db *database.DB, code string) (*pendingInviteSignupRecord, error) {
	var record pendingInviteSignupRecord
	var expiresAtRaw string
	err := db.QueryRow(
		`SELECT id, code, invitation_code, username, email, password_ciphertext, used, expires_at
		 FROM pending_invite_signups
		 WHERE code = ?`,
		code,
	).Scan(
		&record.ID,
		&record.Code,
		&record.InvitationCode,
		&record.Username,
		&record.Email,
		&record.PasswordCiphertext,
		&record.Used,
		&expiresAtRaw,
	)
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

func (h *InvitationHandler) createPendingInviteSignup(r *http.Request, inv *invitation, form *inviteFormData) error {
	if h.mailer == nil {
		return errors.New(h.tr(r, "settings_save_smtp", "SMTP not configured"))
	}
	if err := h.ensureInviteUsernameAvailable(r, form.Username); err != nil {
		return err
	}

	token, err := generateSecureToken(emailVerificationTokenLength)
	if err != nil {
		return fmt.Errorf("generation du token d'invitation: %w", err)
	}

	expiresAt := time.Now().Add(emailVerificationExpiry)
	tx, err := h.db.Begin()
	if err != nil {
		return fmt.Errorf("transaction invitation en attente: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM pending_invite_signups WHERE lower(username) = lower(?)`, form.Username); err != nil {
		return fmt.Errorf("nettoyage invitations en attente: %w", err)
	}

	if _, err := tx.Exec(
		`INSERT INTO pending_invite_signups (code, invitation_code, username, email, password_ciphertext, expires_at, used)
		 VALUES (?, ?, ?, ?, ?, ?, FALSE)`,
		token,
		inv.Code,
		form.Username,
		form.Email,
		"",
		expiresAt.Format("2006-01-02 15:04:05"),
	); err != nil {
		return fmt.Errorf("creation invitation en attente: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("validation invitation en attente: %w", err)
	}

	langCtx := emailLanguageContext{}
	if strings.TrimSpace(inv.JellyfinProfile) != "" {
		var inviteProfile jellyfin.InviteProfile
		if err := json.Unmarshal([]byte(inv.JellyfinProfile), &inviteProfile); err == nil {
			langCtx.GroupName = strings.TrimSpace(inviteProfile.GroupName)
		}
	}
	if err := sendVerificationEmailTemplate(r, h.cfg, h.db, h.mailer, form.Username, form.Email, token, strings.TrimSpace(inv.PreferredLang), langCtx); err != nil {
		_, _ = h.db.Exec(`DELETE FROM pending_invite_signups WHERE code = ?`, token)
		return err
	}

	h.logInviteAction(r, "invite.email_verification.sent", form.Username, inv.Code, form.Email)
	return nil
}

func (h *InvitationHandler) loadPendingInviteContext(code string) (*pendingInviteSignupRecord, *invitation, jellyfin.InviteProfile, string, bool, error) {
	record, err := loadPendingInviteSignup(h.db, code)
	if err == sql.ErrNoRows {
		return nil, nil, jellyfin.InviteProfile{}, "", false, nil
	}
	if err != nil {
		return nil, nil, jellyfin.InviteProfile{}, "invalid", true, fmt.Errorf("lecture invitation en attente: %w", err)
	}
	if record.Used {
		return nil, nil, jellyfin.InviteProfile{}, "used", true, fmt.Errorf("invitation deja utilisee")
	}
	if time.Now().After(record.ExpiresAt) {
		return nil, nil, jellyfin.InviteProfile{}, "expired", true, fmt.Errorf("invitation en attente expiree")
	}

	inv, err := h.getValidInvitation(record.InvitationCode)
	if err != nil {
		return nil, nil, jellyfin.InviteProfile{}, "failed", true, err
	}

	profile := jellyfin.InviteProfile{RequireEmail: true, RequireEmailVerification: true}
	if inv.JellyfinProfile != "" {
		if err := json.Unmarshal([]byte(inv.JellyfinProfile), &profile); err != nil {
			return nil, nil, jellyfin.InviteProfile{}, "failed", true, fmt.Errorf("profil d'invitation invalide: %w", err)
		}
	}

	return record, inv, profile, "pending", true, nil
}

func (h *InvitationHandler) markPendingInviteSignupUsed(code string) error {
	result, err := h.db.Exec(
		`UPDATE pending_invite_signups
		 SET used = TRUE
		 WHERE code = ? AND used = FALSE AND expires_at > ?`,
		code,
		time.Now().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return err
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("verification link already used or expired")
	}
	return nil
}

func (h *InvitationHandler) completePendingInviteSignup(r *http.Request, code string) (string, bool, error) {
	record, inv, profile, status, handled, err := h.loadPendingInviteContext(code)
	if !handled || err != nil {
		return status, handled, err
	}

	if err := r.ParseForm(); err != nil {
		return "failed", true, fmt.Errorf("requete invalide")
	}
	password := r.FormValue("password")
	confirm := r.FormValue("password_confirm")
	if password == "" {
		return "failed", true, fmt.Errorf("le mot de passe est requis")
	}
	if password != confirm {
		return "failed", true, fmt.Errorf("les mots de passe ne correspondent pas")
	}
	if err := h.validateInvitePassword(r, password, &profile); err != nil {
		return "failed", true, err
	}

	form := &inviteFormData{
		Username: record.Username,
		Email:    record.Email,
		Password: password,
	}
	if strings.TrimSpace(profile.ForcedUsername) != "" {
		form.Username = strings.TrimSpace(profile.ForcedUsername)
	}

	if err := h.ensureInviteUsernameAvailable(r, form.Username); err != nil {
		return "failed", true, err
	}
	if err := h.reserveInvitationUse(inv); err != nil {
		return "failed", true, err
	}
	if err := h.markPendingInviteSignupUsed(code); err != nil {
		h.releaseInvitationUse(inv)
		return "used", true, err
	}

	if _, err := h.completeInviteSignup(r, inv, form, profile, true); err != nil {
		if shouldReleaseInvitationReservation(err) {
			h.releaseInvitationUse(inv)
		}
		return "failed", true, err
	}

	if _, err := h.db.Exec(`DELETE FROM pending_invite_signups WHERE lower(username) = lower(?) AND code <> ?`, form.Username, code); err != nil {
		slog.Warn("Impossible de nettoyer les anciennes invitations en attente", "username", form.Username, "error", err)
	}

	h.logInviteAction(r, "invite.email_verification.consumed", form.Username, inv.Code, form.Email)
	return "success", true, nil
}

func (h *InvitationHandler) renderPendingInvitePasswordPage(w http.ResponseWriter, r *http.Request, code string, record *pendingInviteSignupRecord, profile jellyfin.InviteProfile, statusCode int, message string) {
	if h.renderer == nil {
		http.Error(w, message, statusCode)
		return
	}

	links := resolvePortalLinks(h.cfg, h.db)
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.Section = "login"
	td.SuccessMessage = message
	td.Data["ResultTitle"] = h.tr(r, "verify_email_title", "Email verification")
	td.Data["ResultHeading"] = h.tr(r, "verify_email_invite_password_heading", "Choose your password")
	td.Data["LoginLabel"] = h.tr(r, "back_to_login", "Back to login")
	td.Data["ShowInvitePasswordForm"] = true
	td.Data["VerificationCode"] = code
	td.Data["PendingUsername"] = record.Username
	td.Data["PendingEmail"] = record.Email
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.Data["JellyTrackURL"] = links.JellyTrackURL

	policy := resolveInvitePasswordPolicy(profile)
	td.Data["PasswordMinLength"] = policy.MinLength
	td.Data["PasswordMaxLength"] = policy.MaxLength
	td.Data["PasswordRequireUpper"] = policy.RequireUpper
	td.Data["PasswordRequireLower"] = policy.RequireLower
	td.Data["PasswordRequireDigit"] = policy.RequireDigit
	td.Data["PasswordRequireSpecial"] = policy.RequireSpecial

	w.WriteHeader(statusCode)
	if err := h.renderer.Render(w, "verify_email.html", td); err != nil {
		http.Error(w, message, statusCode)
	}
}

func (h *InvitationHandler) VerifyEmailPage(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	title := h.tr(r, "verify_email_title", "Email verification")
	statusCode := http.StatusOK
	var heading, message string

	record, _, profile, status, handled, err := h.loadPendingInviteContext(code)
	if handled {
		if err != nil {
			slog.Warn("Validation email invitation echouee", "code_fingerprint", tokenLogFingerprint(code), "status", status, "error", err)
			switch status {
			case "expired":
				statusCode = http.StatusGone
				heading = h.tr(r, "verify_email_invite_expired_heading", "Verification link expired")
				message = h.tr(r, "verify_email_invite_expired_message", "This signup confirmation link has expired. Submit the invitation form again to receive a new email.")
			case "used":
				statusCode = http.StatusGone
				heading = h.tr(r, "verify_email_used_heading", "Link already used")
				message = h.tr(r, "verify_email_used_message", "This verification link has already been used. Your email may already be confirmed.")
			default:
				statusCode = http.StatusConflict
				heading = h.tr(r, "verify_email_invite_failed_heading", "Account creation failed")
				message = h.tr(r, "verify_email_invite_failed_message", "We could not finish creating your account from this invitation. Submit the invitation form again or ask for a new invitation.")
			}
			renderEmailVerificationPage(r, w, h.renderer, jgmw.LangFromContext(r.Context()), statusCode, title, heading, message, h.tr(r, "back_to_login", "Back to login"), resolvePortalLinks(h.cfg, h.db))
			return
		}

		h.renderPendingInvitePasswordPage(
			w,
			r,
			code,
			record,
			profile,
			http.StatusOK,
			h.tr(r, "verify_email_invite_password_message", "Your email is confirmed. Choose a password to finish creating your account."),
		)
		return
	}

	_, status, err = validateEmailVerification(h.db, code)
	if err != nil {
		slog.Warn("Verification email echouee", "code_fingerprint", tokenLogFingerprint(code), "status", status, "error", err)
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
	} else {
		heading = h.tr(r, "verify_email_confirm_heading", "Confirm email address")
		message = h.tr(r, "verify_email_confirm_message", "Confirm this email address to finish securing your account.")
	}

	extraData := map[string]interface{}{}
	if err == nil {
		extraData["ShowEmailVerificationConfirm"] = true
		extraData["VerificationCode"] = code
		extraData["ConfirmLabel"] = h.tr(r, "verify_email_confirm_button", "Confirm email")
	}
	renderEmailVerificationPage(r, w, h.renderer, jgmw.LangFromContext(r.Context()), statusCode, title, heading, message, h.tr(r, "back_to_login", "Back to login"), resolvePortalLinks(h.cfg, h.db), extraData)
}

func (h *InvitationHandler) VerifyEmailSubmit(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	title := h.tr(r, "verify_email_title", "Email verification")
	statusCode := http.StatusOK
	heading := h.tr(r, "verify_email_invite_success_heading", "Email verified, account created")
	message := h.tr(r, "verify_email_invite_success_message", "Your email address has been confirmed and your account is now ready. You can sign in to Jellyfin.")

	status, handled, err := h.completePendingInviteSignup(r, code)
	if !handled {
		_, status, err = consumeEmailVerification(h.db, code)
		if err != nil {
			slog.Warn("Verification email echouee", "code_fingerprint", tokenLogFingerprint(code), "status", status, "error", err)
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
		} else {
			heading = h.tr(r, "verify_email_success_heading", "Email verified")
			message = h.tr(r, "verify_email_success_message", "Your email address has been confirmed. You can now sign in normally.")
		}
	} else if err != nil {
		slog.Warn("Validation email invitation echouee", "code_fingerprint", tokenLogFingerprint(code), "status", status, "error", err)
		switch status {
		case "expired":
			statusCode = http.StatusGone
			heading = h.tr(r, "verify_email_invite_expired_heading", "Verification link expired")
			message = h.tr(r, "verify_email_invite_expired_message", "This signup confirmation link has expired. Submit the invitation form again to receive a new email.")
		case "used":
			statusCode = http.StatusGone
			heading = h.tr(r, "verify_email_used_heading", "Link already used")
			message = h.tr(r, "verify_email_used_message", "This verification link has already been used. Your email may already be confirmed.")
		default:
			statusCode = http.StatusConflict
			heading = h.tr(r, "verify_email_invite_failed_heading", "Account creation failed")
			message = err.Error()
		}
	}

	renderEmailVerificationPage(r, w, h.renderer, jgmw.LangFromContext(r.Context()), statusCode, title, heading, message, h.tr(r, "back_to_login", "Back to login"), resolvePortalLinks(h.cfg, h.db))
}
