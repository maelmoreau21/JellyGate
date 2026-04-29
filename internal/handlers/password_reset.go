// Package handlers — password_reset.go
//
// Gère le flux complet de réinitialisation de mot de passe :
//
//  1. Demande (POST /reset/request) :
//     - Recherche l'utilisateur par email ou username dans SQLite
//     - Génère un token sécurisé (crypto/rand, 32 bytes, hex)
//     - Insère dans la table password_resets (expiration 15 min)
//     - Envoie l'email avec le lien de réinitialisation
//
//  2. Exécution (POST /reset/{code}) :
//     - Vérifie le token (existence, expiration, non-utilisé)
//     - Applique le nouveau mot de passe simultanément :
//     • ldap.ResetPassword() — Active Directory
//     • jellyfin.ResetPassword() — Jellyfin
//     - Marque le token comme used = true
package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
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
)

// Ã¢â€�â‚¬Ã¢â€�â‚¬ Constantes Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

const (
	// resetTokenLength est la taille du token en bytes (32 bytes = 64 hex chars).
	resetTokenLength = 32

	// resetTokenExpiry est la durÃƒÂ©e de validitÃƒÂ© d'un token de rÃƒÂ©initialisation.
	resetTokenExpiry = 15 * time.Minute
)

// Ã¢â€�â‚¬Ã¢â€�â‚¬ Structures internes Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// passwordResetRecord est une ligne de la table password_resets.
type passwordResetRecord struct {
	ID        int64
	UserID    int64
	Code      string
	Used      bool
	ExpiresAt time.Time
	CreatedAt time.Time
}

// userRecord contient les champs nÃƒÂ©cessaires pour le reset.
type userRecord struct {
	ID            int64
	Username      string
	Email         string
	JellyfinID    string
	LdapDN        string
	PreferredLang string
	GroupName     string
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ Password Reset Handler Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// PasswordResetHandler gÃƒÂ¨re les routes de rÃƒÂ©initialisation de mot de passe.
type PasswordResetHandler struct {
	cfg      *config.Config
	db       *database.DB
	jfClient *jellyfin.Client
	ldClient *jgldap.Client
	mailer   *mail.Mailer
	renderer *render.Engine
}

// NewPasswordResetHandler crée un nouveau handler de réinitialisation.
func NewPasswordResetHandler(cfg *config.Config, db *database.DB, jf *jellyfin.Client, ld *jgldap.Client, m *mail.Mailer, renderer *render.Engine) *PasswordResetHandler {
	return &PasswordResetHandler{
		cfg:      cfg,
		db:       db,
		jfClient: jf,
		ldClient: ld,
		mailer:   m,
		renderer: renderer,
	}
}

func (h *PasswordResetHandler) tr(r *http.Request, key, fallback string) string {
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

// SetLDAPClient remplace le client LDAP (rechargement à chaud).
func (h *PasswordResetHandler) SetLDAPClient(ld *jgldap.Client) { h.ldClient = ld }

// SetMailer remplace le mailer (rechargement à chaud).
func (h *PasswordResetHandler) SetMailer(m *mail.Mailer) { h.mailer = m }

// ——————————————————————————————————————————————————————————————————————————————————————————————————

func (h *PasswordResetHandler) RequestPage(w http.ResponseWriter, r *http.Request) {
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.Section = "login"
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.Data["JellyTrackURL"] = links.JellyTrackURL
	td.ShowNewPasswordForm = false
	if err := h.renderer.Render(w, "admin/reset_request.html", td); err != nil {
		slog.Error("Erreur rendu reset request", "error", err)
		http.Error(w, h.tr(r, "common_server_error", "Server error"), http.StatusInternalServerError)
	}
}

// ——————————————————————————————————————————————————————————————————————————————————————————————————

// SubmitRequest traite la demande de réinitialisation.
func (h *PasswordResetHandler) SubmitRequest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "common_invalid_request", "Invalid request")})
		return
	}

	identifier := strings.TrimSpace(r.FormValue("identifier"))
	if identifier == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "field_identifier_required", "Identifier is required")})
		return
	}

	slog.Info("Demande de réinitialisation MDP", "identifier", identifier, "remote", r.RemoteAddr)

	successMsg := h.tr(r, "reset_request_success", "If a matching account exists, a reset email has been sent.")

	// ——————————————————————————————————————————————————————————————————————————————————————————————————
	user, err := h.findUserByIdentifier(identifier)
	if err != nil {
		slog.Warn("Utilisateur introuvable pour reset (pas d'erreur visible)",
			"identifier", identifier,
			"error", err,
		)
		h.renderSuccessPage(w, r, successMsg)
		return
	}

	if user.Email == "" {
		slog.Warn("Utilisateur sans email, impossible d'envoyer le reset",
			"username", user.Username,
		)
		h.renderSuccessPage(w, r, successMsg)
		return
	}

	// ——————————————————————————————————————————————————————————————————————————————————————————————————
	token, err := generateSecureToken(resetTokenLength)
	if err != nil {
		slog.Error("Impossible de récupérer l'utilisateur", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "common_server_error", "Server error")})
		return
	}

	// ——————————————————————————————————————————————————————————————————————————————————————————————————
	expiresAt := time.Now().Add(resetTokenExpiry)
	_, err = h.db.Exec(
		`INSERT INTO password_resets (user_id, code, used, expires_at)
		 VALUES (?, ?, FALSE, ?)`,
		user.ID, token, expiresAt.Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		slog.Error("Impossible de créer la demande de reset", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "common_server_error", "Server error")})
		return
	}

	slog.Info("Token de reset créé", "user_id", user.ID, "expires_at", expiresAt.Format(time.RFC3339))

	// ——————————————————————————————————————————————————————————————————————————————————————————————————
	links := resolvePortalLinks(h.cfg, h.db)
	publicBaseURL := strings.TrimRight(strings.TrimSpace(links.JellyGateURL), "/")
	if publicBaseURL == "" && h.cfg != nil {
		publicBaseURL = strings.TrimRight(strings.TrimSpace(h.cfg.BaseURL), "/")
	}
	if publicBaseURL == "" {
		publicBaseURL = strings.TrimRight(requestBaseURL(r), "/")
	}
	resetURL := fmt.Sprintf("%s/reset/%s", publicBaseURL, token)

	emailData := map[string]string{
		"Username":           user.Username,
		"ResetLink":          resetURL,
		"ResetURL":           resetURL,
		"ResetCode":          token,
		"HelpURL":            publicBaseURL,
		"JellyGateURL":       publicBaseURL,
		"JellyfinURL":        links.JellyfinURL,
		"JellyfinServerName": links.JellyfinServerName,
		"JellyseerrURL":      links.JellyseerrURL,
		"JellyTrackURL":      links.JellyTrackURL,
	}

	emailCfg, usedLang, cfgErr := loadEmailTemplatesForLanguage(h.db, "", emailLanguageContext{
		PreferredLang: user.PreferredLang,
		GroupName:     user.GroupName,
	})
	if cfgErr != nil {
		emailCfg = config.DefaultEmailTemplatesForLanguage(usedLang)
	}
	emailData["ExpiresIn"] = config.DefaultEmailPreviewDurationForLanguage(usedLang)
	tpl := emailCfg.PasswordReset
	if tpl == "" {
		tpl = "Bonjour {{.Username}},\n\nVoici le lien pour réinitialiser votre mot de passe : {{.ResetURL}}"
	}
	subject := firstNonEmpty(emailCfg.PasswordResetSubject, h.tr(r, "reset_title", "Password Reset"))

	if err := sendTemplateIfConfigured(h.mailer, user.Email, subject, usedLang, "password_reset", tpl, emailCfg, emailData); err != nil {
		slog.Error("Erreur d'envoi de l'email de reset",
			"to", user.Email,
			"error", err,
		)
		_ = h.db.LogAction("reset.email.failed", user.Username, "", err.Error())
	} else {
		slog.Info("Email de reset envoyé", "to", user.Email)
		_ = h.db.LogAction("reset.email.sent", user.Username, "", "Email de reinitialisation envoye")
	}

	_ = h.db.LogAction("reset.requested", user.Username, "", fmt.Sprintf("IP: %s", r.RemoteAddr))

	h.renderSuccessPage(w, r, successMsg)
}

// ——————————————————————————————————————————————————————————————————————————————————————————————————

func (h *PasswordResetHandler) ResetPage(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.Section = "login"
	_, _, err := h.getValidResetToken(code)
	if err != nil {
		slog.Warn("Token de reset invalide consulté", "code", code, "error", err)
		http.Error(w, "Lien de réinitialisation invalide ou expiré", http.StatusNotFound)
		return
	}
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.Data["JellyTrackURL"] = links.JellyTrackURL
	td.ShowNewPasswordForm = true
	td.ResetCode = code
	if err := h.renderer.Render(w, "admin/reset_new.html", td); err != nil {
		slog.Error("Erreur rendu reset new", "error", err)
		http.Error(w, h.tr(r, "common_server_error", "Server error"), http.StatusInternalServerError)
	}
}

// ——————————————————————————————————————————————————————————————————————————————————————————————————

// SubmitReset traite la soumission du nouveau mot de passe.
func (h *PasswordResetHandler) SubmitReset(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Requête invalide", http.StatusBadRequest)
		return
	}

	password := r.FormValue("password")
	confirm := r.FormValue("password_confirm")

	// ——————————————————————————————————————————————————————————————————————————————————————————————————
	resetRecord, user, err := h.getValidResetToken(code)
	if err != nil {
		slog.Warn("Demande de reset invalide", "token", code, "error", err)
		http.Redirect(w, r, "/admin/login?error="+url.QueryEscape(h.tr(r, "reset_error_invalid_or_expired", "Invalid or expired reset link")), http.StatusSeeOther)
		return
	}

	// ——————————————————————————————————————————————————————————————————————————————————————————————————
	if password == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "field_password_required", "Password is required")})
		return
	}
	if len(password) < 8 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "field_password_min_length", "Password must be at least 8 characters")})
		return
	}
	if password != confirm {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "field_password_mismatch", "Passwords do not match")})
		return
	}

	slog.Info("Réinitialisation MDP en cours",
		"username", user.Username,
		"user_id", user.ID,
		"remote", r.RemoteAddr,
	)

	// ——————————————————————————————————————————————————————————————————————————————————————————————————
	var errors []string

	if h.ldClient != nil && user.LdapDN != "" {
		if err := h.ldClient.ResetPassword(user.LdapDN, password); err != nil {
			slog.Error("Erreur reset LDAP", "dn", user.LdapDN, "error", err)
			errors = append(errors, fmt.Sprintf("AD: %s", err.Error()))
		}
	}

	if user.JellyfinID != "" {
		if err := h.jfClient.ResetPassword(user.JellyfinID, password); err != nil {
			slog.Error("Erreur reset Jellyfin", "jellyfin_id", user.JellyfinID, "error", err)
			errors = append(errors, fmt.Sprintf("Jellyfin: %s", err.Error()))
		}
	}

	if user.LdapDN != "" && user.JellyfinID != "" && len(errors) == 2 {
		slog.Error("Reset MDP échoué sur TOUS les services", "username", user.Username, "errors", errors)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "reset_error_generic", "Error during reset. Please try again or contact the administrator.")})
		return
	}

	// ——————————————————————————————————————————————————————————————————————————————————————————————————
	_, err = h.db.Exec(`UPDATE password_resets SET used = TRUE WHERE id = ?`, resetRecord.ID)
	if err != nil {
		slog.Error("Erreur de mise à jour du token de reset", "id", resetRecord.ID, "error", err)
	}

	partial := len(errors) > 0
	_ = h.db.LogAction(
		fmt.Sprintf("reset.%s", map[bool]string{true: "partial", false: "success"}[partial]),
		user.Username,
		"",
		fmt.Sprintf(`{"user_id":%d,"jellyfin_id":"%s","ldap_dn":"%s","errors":%d}`,
			user.ID, user.JellyfinID, user.LdapDN, len(errors)),
	)

	slog.Info("Réinitialisation MDP terminée", "username", user.Username, "partial", partial)

	if partial {
		writeJSON(w, http.StatusOK, APIResponse{
			Success: true,
			Message: h.tr(r, "reset_success_partial", "Your password has been partially reset. Contact the administrator if the problem persists."),
		})
	} else {
		writeJSON(w, http.StatusOK, APIResponse{
			Success: true,
			Message: h.tr(r, "reset_success", "Your password has been reset successfully."),
		})
	}
}

// ——————————————————————————————————————————————————————————————————————————————————————————————————

// findUserByIdentifier recherche un utilisateur par email ou username dans SQLite.
func (h *PasswordResetHandler) findUserByIdentifier(identifier string) (*userRecord, error) {
	var user userRecord
	var jellyfinID, email, ldapDN, preferredLang, groupName sql.NullString

	err := h.db.QueryRow(
		`SELECT id, username, email, jellyfin_id, ldap_dn, preferred_lang, group_name
		 FROM users
		 WHERE username = ? OR email = ?
		 LIMIT 1`,
		identifier, identifier,
	).Scan(&user.ID, &user.Username, &email, &jellyfinID, &ldapDN, &preferredLang, &groupName)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("aucun utilisateur trouvÃƒÂ© pour %q", identifier)
	}
	if err != nil {
		return nil, fmt.Errorf("erreur de recherche: %w", err)
	}

	user.Email = email.String
	user.JellyfinID = jellyfinID.String
	user.LdapDN = ldapDN.String
	user.PreferredLang = strings.TrimSpace(preferredLang.String)
	user.GroupName = strings.TrimSpace(groupName.String)

	return &user, nil
}

// getValidResetToken rÃƒÂ©cupÃƒÂ¨re et valide un token de rÃƒÂ©initialisation.
// VÃƒÂ©rifie : existence, expiration (15 min), non-utilisÃƒÂ©.
// Retourne le token ET l'utilisateur associÃƒÂ©.
func (h *PasswordResetHandler) getValidResetToken(code string) (*passwordResetRecord, *userRecord, error) {
	if code == "" {
		return nil, nil, fmt.Errorf("code de reset vide")
	}

	// RÃƒÂ©cupÃƒÂ©rer le token
	var rec passwordResetRecord
	var expiresAtStr string

	err := h.db.QueryRow(
		`SELECT id, user_id, code, used, expires_at, created_at
		 FROM password_resets WHERE code = ?`, code,
	).Scan(&rec.ID, &rec.UserID, &rec.Code, &rec.Used, &expiresAtStr, &rec.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil, fmt.Errorf("token %q introuvable", code)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("erreur de lecture du token: %w", err)
	}

	// Parser l'expiration
	rec.ExpiresAt, err = time.Parse("2006-01-02 15:04:05", expiresAtStr)
	if err != nil {
		return nil, nil, fmt.Errorf("format d'expiration invalide: %w", err)
	}

	// VÃƒÂ©rifier non-utilisÃƒÂ©
	if rec.Used {
		return nil, nil, fmt.Errorf("token %q dÃƒÂ©jÃƒÂ  utilisÃƒÂ©", code)
	}

	// VÃƒÂ©rifier expiration
	if time.Now().After(rec.ExpiresAt) {
		return nil, nil, fmt.Errorf("token %q expirÃƒÂ© depuis %s",
			code, rec.ExpiresAt.Format("02/01/2006 15:04"))
	}

	// RÃƒÂ©cupÃƒÂ©rer l'utilisateur associÃƒÂ©
	var user userRecord
	var jellyfinID, email, ldapDN, preferredLang, groupName sql.NullString

	err = h.db.QueryRow(
		`SELECT id, username, email, jellyfin_id, ldap_dn, preferred_lang, group_name FROM users WHERE id = ?`,
		rec.UserID,
	).Scan(&user.ID, &user.Username, &email, &jellyfinID, &ldapDN, &preferredLang, &groupName)

	if err != nil {
		return nil, nil, fmt.Errorf("utilisateur associÃƒÂ© au token introuvable: %w", err)
	}

	user.Email = email.String
	user.JellyfinID = jellyfinID.String
	user.LdapDN = ldapDN.String
	user.PreferredLang = strings.TrimSpace(preferredLang.String)
	user.GroupName = strings.TrimSpace(groupName.String)

	return &rec, &user, nil
}

// generateSecureToken gÃƒÂ©nÃƒÂ¨re un token cryptographiquement sÃƒÂ»r.
// Utilise crypto/rand pour une entropie maximale.
// Retourne une chaÃƒÂ®ne hexadÃƒÂ©cimale de 2*length caractÃƒÂ¨res.
func generateSecureToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("erreur de gÃƒÂ©nÃƒÂ©ration du token: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func (h *PasswordResetHandler) renderSuccessPage(w http.ResponseWriter, r *http.Request, message string) {
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.Section = "login"
	td.SuccessMessage = message
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.Data["JellyTrackURL"] = links.JellyTrackURL
	if err := h.renderer.Render(w, "reset.html", td); err != nil {
		slog.Error("Erreur rendu success page", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
	}
}
