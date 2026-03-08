package handlers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
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

func encryptPendingInvitePassword(secretKey, password string) (string, error) {
	key := sha256.Sum256([]byte(secretKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("initialisation chiffrement invitation: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("initialisation GCM invitation: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generation nonce invitation: %w", err)
	}

	sealed := gcm.Seal(nil, nonce, []byte(password), nil)
	payload := append(nonce, sealed...)
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decryptPendingInvitePassword(secretKey, ciphertext string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(ciphertext))
	if err != nil {
		return "", fmt.Errorf("decodage mot de passe invitation: %w", err)
	}

	key := sha256.Sum256([]byte(secretKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("initialisation dechiffrement invitation: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("initialisation GCM invitation: %w", err)
	}

	if len(decoded) < gcm.NonceSize() {
		return "", fmt.Errorf("contenu chiffre invitation invalide")
	}

	nonce := decoded[:gcm.NonceSize()]
	message := decoded[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, message, nil)
	if err != nil {
		return "", fmt.Errorf("dechiffrement mot de passe invitation: %w", err)
	}

	return string(plaintext), nil
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
		return fmt.Errorf("SMTP non configuré")
	}
	if err := h.ensureInviteUsernameAvailable(form.Username); err != nil {
		return err
	}

	token, err := generateSecureToken(emailVerificationTokenLength)
	if err != nil {
		return fmt.Errorf("génération du token d'invitation: %w", err)
	}

	passwordCiphertext, err := encryptPendingInvitePassword(h.cfg.SecretKey, form.Password)
	if err != nil {
		return err
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
		passwordCiphertext,
		expiresAt.Format("2006-01-02 15:04:05"),
	); err != nil {
		return fmt.Errorf("création invitation en attente: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("validation invitation en attente: %w", err)
	}

	if err := sendVerificationEmailTemplate(h.cfg, h.db, h.mailer, form.Username, form.Email, token); err != nil {
		_, _ = h.db.Exec(`DELETE FROM pending_invite_signups WHERE code = ?`, token)
		return err
	}

	h.logInviteAction(r, "invite.email_verification.sent", form.Username, inv.Code, form.Email)
	return nil
}

func (h *InvitationHandler) completePendingInviteSignup(r *http.Request, code string) (string, bool, error) {
	record, err := loadPendingInviteSignup(h.db, code)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "invalid", true, fmt.Errorf("lecture invitation en attente: %w", err)
	}

	if record.Used {
		return "used", true, fmt.Errorf("invitation déjà utilisée")
	}
	if time.Now().After(record.ExpiresAt) {
		return "expired", true, fmt.Errorf("invitation en attente expirée")
	}

	password, err := decryptPendingInvitePassword(h.cfg.SecretKey, record.PasswordCiphertext)
	if err != nil {
		return "failed", true, err
	}

	inv, err := h.getValidInvitation(record.InvitationCode)
	if err != nil {
		return "failed", true, err
	}

	profile := jellyfin.InviteProfile{RequireEmail: true, RequireEmailVerification: true}
	if inv.JellyfinProfile != "" {
		if err := json.Unmarshal([]byte(inv.JellyfinProfile), &profile); err != nil {
			return "failed", true, fmt.Errorf("profil d'invitation invalide: %w", err)
		}
	}

	form := &inviteFormData{
		Username: record.Username,
		Email:    record.Email,
		Password: password,
	}
	if strings.TrimSpace(profile.ForcedUsername) != "" {
		form.Username = strings.TrimSpace(profile.ForcedUsername)
	}

	if err := h.ensureInviteUsernameAvailable(form.Username); err != nil {
		return "failed", true, err
	}

	if _, err := h.completeInviteSignup(r, inv, form, profile, true); err != nil {
		return "failed", true, err
	}

	if _, err := h.db.Exec(`UPDATE pending_invite_signups SET used = TRUE WHERE code = ?`, code); err != nil {
		slog.Warn("Impossible de marquer l'invitation en attente comme utilisée", "code", code, "error", err)
	}
	if _, err := h.db.Exec(`DELETE FROM pending_invite_signups WHERE lower(username) = lower(?) AND code <> ?`, form.Username, code); err != nil {
		slog.Warn("Impossible de nettoyer les anciennes invitations en attente", "username", form.Username, "error", err)
	}

	h.logInviteAction(r, "invite.email_verification.consumed", form.Username, inv.Code, form.Email)
	return "success", true, nil
}

func (h *InvitationHandler) VerifyEmailPage(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	title := h.tr(r, "verify_email_title", "Email verification")
	statusCode := http.StatusOK
	heading := h.tr(r, "verify_email_success_heading", "Email verified")
	message := h.tr(r, "verify_email_success_message", "Your email address has been confirmed. You can now sign in normally.")

	status, handled, err := h.completePendingInviteSignup(r, code)
	if handled {
		if err != nil {
			slog.Warn("Validation email invitation échouée", "code", code, "status", status, "error", err)
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
		} else {
			heading = h.tr(r, "verify_email_invite_success_heading", "Email verified, account created")
			message = h.tr(r, "verify_email_invite_success_message", "Your email address has been confirmed and your account is now ready. You can sign in to Jellyfin.")
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
		return
	}

	_, status, err = consumeEmailVerification(h.db, code)
	if err != nil {
		slog.Warn("Verification email échouée", "code", code, "status", status, "error", err)
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
