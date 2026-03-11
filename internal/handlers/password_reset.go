// Package handlers â€” password_reset.go
//
// GÃ¨re le flux complet de rÃ©initialisation de mot de passe :
//
//  1. Demande (POST /reset/request) :
//     - Recherche l'utilisateur par email ou username dans SQLite
//     - GÃ©nÃ¨re un token sÃ©curisÃ© (crypto/rand, 32 bytes, hex)
//     - InsÃ¨re dans la table password_resets (expiration 15 min)
//     - Envoie l'email avec le lien de rÃ©initialisation
//
//  2. ExÃ©cution (POST /reset/{code}) :
//     - VÃ©rifie le token (existence, expiration, non-utilisÃ©)
//     - Applique le nouveau mot de passe simultanÃ©ment :
//     â€¢ ldap.ResetPassword() â€” Active Directory
//     â€¢ jellyfin.ResetPassword() â€” Jellyfin
//     - Marque le token comme used = true
package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
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

// â”€â”€ Constantes â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

const (
	// resetTokenLength est la taille du token en bytes (32 bytes = 64 hex chars).
	resetTokenLength = 32

	// resetTokenExpiry est la durÃ©e de validitÃ© d'un token de rÃ©initialisation.
	resetTokenExpiry = 15 * time.Minute
)

// â”€â”€ Structures internes â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// passwordResetRecord est une ligne de la table password_resets.
type passwordResetRecord struct {
	ID        int64
	UserID    int64
	Code      string
	Used      bool
	ExpiresAt time.Time
	CreatedAt time.Time
}

// userRecord contient les champs nÃ©cessaires pour le reset.
type userRecord struct {
	ID         int64
	Username   string
	Email      string
	JellyfinID string
	LdapDN     string
}

// â”€â”€ Password Reset Handler â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// PasswordResetHandler gÃ¨re les routes de rÃ©initialisation de mot de passe.
type PasswordResetHandler struct {
	cfg      *config.Config
	db       *database.DB
	jfClient *jellyfin.Client
	ldClient *jgldap.Client
	mailer   *mail.Mailer
	renderer *render.Engine
}

// NewPasswordResetHandler crÃ©e un nouveau handler de rÃ©initialisation.
func NewPasswordResetHandler(
	cfg *config.Config,
	db *database.DB,
	jf *jellyfin.Client,
	ld *jgldap.Client,
	m *mail.Mailer,
	renderer *render.Engine,
) *PasswordResetHandler {
	return &PasswordResetHandler{
		cfg:      cfg,
		db:       db,
		jfClient: jf,
		ldClient: ld,
		mailer:   m,
		renderer: renderer,
	}
}

// SetLDAPClient remplace le client LDAP (rechargement Ã  chaud).
func (h *PasswordResetHandler) SetLDAPClient(ld *jgldap.Client) { h.ldClient = ld }

// SetMailer remplace le mailer (rechargement Ã  chaud).
func (h *PasswordResetHandler) SetMailer(m *mail.Mailer) { h.mailer = m }

// â”€â”€ GET /reset/ â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// RequestPage affiche le formulaire de demande de rÃ©initialisation.
func (h *PasswordResetHandler) RequestPage(w http.ResponseWriter, r *http.Request) {
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.ShowNewPasswordForm = false
	if err := h.renderer.Render(w, "reset.html", td); err != nil {
		slog.Error("Erreur rendu reset request", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
	}
}

// â”€â”€ POST /reset/request â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// SubmitRequest traite la demande de rÃ©initialisation.
//
// Flux :
//  1. Rechercher l'utilisateur par email ou username dans SQLite
//  2. GÃ©nÃ©rer un token sÃ©curisÃ© (crypto/rand)
//  3. InsÃ©rer dans la table password_resets (expiration 15 min)
//  4. Envoyer l'email avec le lien
//
// IMPORTANT : Pour Ã©viter l'Ã©numÃ©ration d'utilisateurs, on retourne
// toujours le mÃªme message de succÃ¨s, que l'utilisateur existe ou non.
func (h *PasswordResetHandler) SubmitRequest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "RequÃªte invalide", http.StatusBadRequest)
		return
	}

	identifier := strings.TrimSpace(r.FormValue("identifier"))
	if identifier == "" {
		http.Error(w, "Identifiant requis", http.StatusBadRequest)
		return
	}

	slog.Info("Demande de rÃ©initialisation MDP", "identifier", identifier, "remote", r.RemoteAddr)

	// Message gÃ©nÃ©rique (anti-Ã©numÃ©ration)
	successMsg := "Si un compte correspondant existe, un email de rÃ©initialisation a Ã©tÃ© envoyÃ©."

	// â”€â”€ 1. Rechercher l'utilisateur â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	user, err := h.findUserByIdentifier(identifier)
	if err != nil {
		slog.Warn("Utilisateur introuvable pour reset (pas d'erreur visible)",
			"identifier", identifier,
			"error", err,
		)
		// On affiche quand mÃªme le message de succÃ¨s (anti-Ã©numÃ©ration)
		h.renderSuccessPage(w, r, successMsg)
		return
	}

	// VÃ©rifier que l'utilisateur a un email
	if user.Email == "" {
		slog.Warn("Utilisateur sans email, impossible d'envoyer le reset",
			"username", user.Username,
		)
		h.renderSuccessPage(w, r, successMsg)
		return
	}

	// â”€â”€ 2. GÃ©nÃ©rer un token sÃ©curisÃ© â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	token, err := generateSecureToken(resetTokenLength)
	if err != nil {
		slog.Error("Erreur de gÃ©nÃ©ration du token", "error", err)
		http.Error(w, "Erreur interne", http.StatusInternalServerError)
		return
	}

	// â”€â”€ 3. InsÃ©rer dans SQLite â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	expiresAt := time.Now().Add(resetTokenExpiry)
	_, err = h.db.Exec(
		`INSERT INTO password_resets (user_id, code, used, expires_at)
		 VALUES (?, ?, FALSE, ?)`,
		user.ID, token, expiresAt.Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		slog.Error("Erreur d'insertion du token de reset", "error", err)
		http.Error(w, "Erreur interne", http.StatusInternalServerError)
		return
	}

	slog.Info("Token de reset crÃ©Ã©", "user_id", user.ID, "expires_at", expiresAt.Format(time.RFC3339))

	// â”€â”€ 4. Envoyer l'email â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	resetURL := fmt.Sprintf("%s/reset/%s", strings.TrimRight(h.cfg.BaseURL, "/"), token)
	links := resolvePortalLinks(h.cfg, h.db)

	emailData := map[string]string{
		"Username":      user.Username,
		"ResetLink":     resetURL,
		"ResetURL":      resetURL,
		"ResetCode":     token,
		"ExpiresIn":     "15 minutes",
		"JellyfinURL":   links.JellyfinURL,
		"JellyseerrURL": links.JellyseerrURL,
	}

	emailCfg, _ := h.db.GetEmailTemplatesConfig()
	tpl := emailCfg.PasswordReset
	if tpl == "" {
		tpl = "Bonjour {{.Username}},\n\nVoici le lien pour rÃ©initialiser votre mot de passe : {{.ResetURL}}"
	}
	subject := firstNonEmpty(emailCfg.PasswordResetSubject, config.DefaultEmailTemplates().PasswordResetSubject)

	if err := sendTemplateIfConfigured(h.mailer, user.Email, subject, "password_reset", tpl, emailCfg, emailData); err != nil {
		slog.Error("Erreur d'envoi de l'email de reset",
			"to", user.Email,
			"error", err,
		)
		// On ne rÃ©vÃ¨le pas l'erreur Ã  l'utilisateur
		_ = h.db.LogAction("reset.email.failed", user.Username, "", err.Error())
	} else {
		slog.Info("Email de reset envoyÃ©", "to", user.Email)
	}

	_ = h.db.LogAction("reset.requested", user.Username, "", fmt.Sprintf("IP: %s", r.RemoteAddr))

	h.renderSuccessPage(w, r, successMsg)
}

// â”€â”€ GET /reset/{code} â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// ResetPage affiche le formulaire de saisie du nouveau mot de passe.
func (h *PasswordResetHandler) ResetPage(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	// VÃ©rifier que le token est valide avant d'afficher le formulaire
	_, _, err := h.getValidResetToken(code)
	if err != nil {
		slog.Warn("Token de reset invalide consultÃ©", "code", code, "error", err)
		http.Error(w, "Lien de rÃ©initialisation invalide ou expirÃ©", http.StatusNotFound)
		return
	}

	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.ShowNewPasswordForm = true
	td.ResetCode = code

	if err := h.renderer.Render(w, "reset.html", td); err != nil {
		slog.Error("Erreur rendu reset logic", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
	}
}

// â”€â”€ POST /reset/{code} â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// SubmitReset traite la soumission du nouveau mot de passe.
//
// Flux :
//  1. Valider le token (existence, expiration, non-utilisÃ©)
//  2. Valider le nouveau mot de passe
//  3. Appliquer le reset simultanÃ©ment :
//     â€¢ ldap.ResetPassword() â€” Active Directory
//     â€¢ jellyfin.ResetPassword() â€” Jellyfin
//  4. Marquer le token comme used = true
func (h *PasswordResetHandler) SubmitReset(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	if err := r.ParseForm(); err != nil {
		http.Error(w, "RequÃªte invalide", http.StatusBadRequest)
		return
	}

	password := r.FormValue("password")
	passwordConfirm := r.FormValue("password_confirm")

	// â”€â”€ 1. Valider le token â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	resetRecord, user, err := h.getValidResetToken(code)
	if err != nil {
		slog.Warn("Token de reset invalide", "code", code, "error", err)
		http.Error(w, "Lien de rÃ©initialisation invalide ou expirÃ©", http.StatusForbidden)
		return
	}

	// â”€â”€ 2. Valider le mot de passe â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	if password == "" {
		http.Error(w, "Le mot de passe est requis", http.StatusBadRequest)
		return
	}
	if len(password) < 8 {
		http.Error(w, "Le mot de passe doit faire au minimum 8 caractÃ¨res", http.StatusBadRequest)
		return
	}
	if password != passwordConfirm {
		http.Error(w, "Les mots de passe ne correspondent pas", http.StatusBadRequest)
		return
	}

	slog.Info("RÃ©initialisation MDP en cours",
		"username", user.Username,
		"user_id", user.ID,
		"remote", r.RemoteAddr,
	)

	// â”€â”€ 3. Appliquer le reset (LDAP + Jellyfin) â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	var errors []string

	// Reset LDAP (Active Directory)
	if h.ldClient != nil && user.LdapDN != "" {
		if err := h.ldClient.ResetPassword(user.LdapDN, password); err != nil {
			slog.Error("Erreur reset LDAP",
				"dn", user.LdapDN,
				"error", err,
			)
			errors = append(errors, fmt.Sprintf("AD: %s", err.Error()))
		} else {
			slog.Info("Reset LDAP rÃ©ussi", "dn", user.LdapDN)
		}
	}

	// Reset Jellyfin
	if user.JellyfinID != "" {
		if err := h.jfClient.ResetPassword(user.JellyfinID, password); err != nil {
			slog.Error("Erreur reset Jellyfin",
				"jellyfin_id", user.JellyfinID,
				"error", err,
			)
			errors = append(errors, fmt.Sprintf("Jellyfin: %s", err.Error()))
		} else {
			slog.Info("Reset Jellyfin rÃ©ussi", "id", user.JellyfinID)
		}
	}

	// Si les DEUX ont Ã©chouÃ©, c'est une erreur critique
	if user.LdapDN != "" && user.JellyfinID != "" && len(errors) == 2 {
		slog.Error("Reset MDP Ã©chouÃ© sur TOUS les services",
			"username", user.Username,
			"errors", errors,
		)
		_ = h.db.LogAction("reset.failed.total", user.Username, "", fmt.Sprintf("%v", errors))
		http.Error(w, "Erreur lors de la rÃ©initialisation. Veuillez rÃ©essayer ou contacter l'administrateur.", http.StatusInternalServerError)
		return
	}

	// â”€â”€ 4. Marquer le token comme utilisÃ© â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	_, err = h.db.Exec(
		`UPDATE password_resets SET used = TRUE WHERE id = ?`,
		resetRecord.ID,
	)
	if err != nil {
		slog.Error("Erreur de mise Ã  jour du token de reset", "id", resetRecord.ID, "error", err)
		// Non-bloquant : le reset a dÃ©jÃ  Ã©tÃ© appliquÃ©
	}

	// â”€â”€ 5. Audit log â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	status := "success"
	if len(errors) > 0 {
		status = "partial"
	}
	_ = h.db.LogAction(
		fmt.Sprintf("reset.%s", status),
		user.Username,
		"",
		fmt.Sprintf(`{"user_id":%d,"jellyfin_id":"%s","ldap_dn":"%s","errors":%d}`,
			user.ID, user.JellyfinID, user.LdapDN, len(errors)),
	)

	slog.Info("RÃ©initialisation MDP terminÃ©e",
		"username", user.Username,
		"status", status,
		"partial_errors", len(errors),
	)

	// â”€â”€ RÃ©ponse â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	msg := "Votre mot de passe a Ã©tÃ© rÃ©initialisÃ© avec succÃ¨s."
	if len(errors) > 0 {
		msg = "Votre mot de passe a Ã©tÃ© partiellement rÃ©initialisÃ©. Contactez l'administrateur si le problÃ¨me persiste."
	}

	h.renderSuccessPage(w, r, msg)
}

// â”€â”€ MÃ©thodes internes â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// findUserByIdentifier recherche un utilisateur par email ou username dans SQLite.
func (h *PasswordResetHandler) findUserByIdentifier(identifier string) (*userRecord, error) {
	var user userRecord
	var jellyfinID, email, ldapDN sql.NullString

	err := h.db.QueryRow(
		`SELECT id, username, email, jellyfin_id, ldap_dn
		 FROM users
		 WHERE username = ? OR email = ?
		 LIMIT 1`,
		identifier, identifier,
	).Scan(&user.ID, &user.Username, &email, &jellyfinID, &ldapDN)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("aucun utilisateur trouvÃ© pour %q", identifier)
	}
	if err != nil {
		return nil, fmt.Errorf("erreur de recherche: %w", err)
	}

	user.Email = email.String
	user.JellyfinID = jellyfinID.String
	user.LdapDN = ldapDN.String

	return &user, nil
}

// getValidResetToken rÃ©cupÃ¨re et valide un token de rÃ©initialisation.
// VÃ©rifie : existence, expiration (15 min), non-utilisÃ©.
// Retourne le token ET l'utilisateur associÃ©.
func (h *PasswordResetHandler) getValidResetToken(code string) (*passwordResetRecord, *userRecord, error) {
	if code == "" {
		return nil, nil, fmt.Errorf("code de reset vide")
	}

	// RÃ©cupÃ©rer le token
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

	// VÃ©rifier non-utilisÃ©
	if rec.Used {
		return nil, nil, fmt.Errorf("token %q dÃ©jÃ  utilisÃ©", code)
	}

	// VÃ©rifier expiration
	if time.Now().After(rec.ExpiresAt) {
		return nil, nil, fmt.Errorf("token %q expirÃ© depuis %s",
			code, rec.ExpiresAt.Format("02/01/2006 15:04"))
	}

	// RÃ©cupÃ©rer l'utilisateur associÃ©
	var user userRecord
	var jellyfinID, email, ldapDN sql.NullString

	err = h.db.QueryRow(
		`SELECT id, username, email, jellyfin_id, ldap_dn FROM users WHERE id = ?`,
		rec.UserID,
	).Scan(&user.ID, &user.Username, &email, &jellyfinID, &ldapDN)

	if err != nil {
		return nil, nil, fmt.Errorf("utilisateur associÃ© au token introuvable: %w", err)
	}

	user.Email = email.String
	user.JellyfinID = jellyfinID.String
	user.LdapDN = ldapDN.String

	return &rec, &user, nil
}

// generateSecureToken gÃ©nÃ¨re un token cryptographiquement sÃ»r.
// Utilise crypto/rand pour une entropie maximale.
// Retourne une chaÃ®ne hexadÃ©cimale de 2*length caractÃ¨res.
func generateSecureToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("erreur de gÃ©nÃ©ration du token: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// renderSuccessPage affiche une page de succÃ¨s gÃ©nÃ©rique.
func (h *PasswordResetHandler) renderSuccessPage(w http.ResponseWriter, r *http.Request, message string) {
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.SuccessMessage = message
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	if err := h.renderer.Render(w, "reset.html", td); err != nil {
		slog.Error("Erreur rendu success page", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
	}
}
