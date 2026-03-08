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

// ── Constantes ──────────────────────────────────────────────────────────────

const (
	// resetTokenLength est la taille du token en bytes (32 bytes = 64 hex chars).
	resetTokenLength = 32

	// resetTokenExpiry est la durée de validité d'un token de réinitialisation.
	resetTokenExpiry = 15 * time.Minute
)

// ── Structures internes ─────────────────────────────────────────────────────

// passwordResetRecord est une ligne de la table password_resets.
type passwordResetRecord struct {
	ID        int64
	UserID    int64
	Code      string
	Used      bool
	ExpiresAt time.Time
	CreatedAt time.Time
}

// userRecord contient les champs nécessaires pour le reset.
type userRecord struct {
	ID         int64
	Username   string
	Email      string
	JellyfinID string
	LdapDN     string
}

// ── Password Reset Handler ──────────────────────────────────────────────────

// PasswordResetHandler gère les routes de réinitialisation de mot de passe.
type PasswordResetHandler struct {
	cfg      *config.Config
	db       *database.DB
	jfClient *jellyfin.Client
	ldClient *jgldap.Client
	mailer   *mail.Mailer
	renderer *render.Engine
}

// NewPasswordResetHandler crée un nouveau handler de réinitialisation.
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

// SetLDAPClient remplace le client LDAP (rechargement à chaud).
func (h *PasswordResetHandler) SetLDAPClient(ld *jgldap.Client) { h.ldClient = ld }

// SetMailer remplace le mailer (rechargement à chaud).
func (h *PasswordResetHandler) SetMailer(m *mail.Mailer) { h.mailer = m }

// ── GET /reset/ ─────────────────────────────────────────────────────────────

// RequestPage affiche le formulaire de demande de réinitialisation.
func (h *PasswordResetHandler) RequestPage(w http.ResponseWriter, r *http.Request) {
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.Data["JellyTulliURL"] = links.JellyTulliURL
	td.ShowNewPasswordForm = false
	if err := h.renderer.Render(w, "reset.html", td); err != nil {
		slog.Error("Erreur rendu reset request", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
	}
}

// ── POST /reset/request ─────────────────────────────────────────────────────

// SubmitRequest traite la demande de réinitialisation.
//
// Flux :
//  1. Rechercher l'utilisateur par email ou username dans SQLite
//  2. Générer un token sécurisé (crypto/rand)
//  3. Insérer dans la table password_resets (expiration 15 min)
//  4. Envoyer l'email avec le lien
//
// IMPORTANT : Pour éviter l'énumération d'utilisateurs, on retourne
// toujours le même message de succès, que l'utilisateur existe ou non.
func (h *PasswordResetHandler) SubmitRequest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Requête invalide", http.StatusBadRequest)
		return
	}

	identifier := strings.TrimSpace(r.FormValue("identifier"))
	if identifier == "" {
		http.Error(w, "Identifiant requis", http.StatusBadRequest)
		return
	}

	slog.Info("Demande de réinitialisation MDP", "identifier", identifier, "remote", r.RemoteAddr)

	// Message générique (anti-énumération)
	successMsg := "Si un compte correspondant existe, un email de réinitialisation a été envoyé."

	// ── 1. Rechercher l'utilisateur ─────────────────────────────────────
	user, err := h.findUserByIdentifier(identifier)
	if err != nil {
		slog.Warn("Utilisateur introuvable pour reset (pas d'erreur visible)",
			"identifier", identifier,
			"error", err,
		)
		// On affiche quand même le message de succès (anti-énumération)
		h.renderSuccessPage(w, r, successMsg)
		return
	}

	// Vérifier que l'utilisateur a un email
	if user.Email == "" {
		slog.Warn("Utilisateur sans email, impossible d'envoyer le reset",
			"username", user.Username,
		)
		h.renderSuccessPage(w, r, successMsg)
		return
	}

	// ── 2. Générer un token sécurisé ────────────────────────────────────
	token, err := generateSecureToken(resetTokenLength)
	if err != nil {
		slog.Error("Erreur de génération du token", "error", err)
		http.Error(w, "Erreur interne", http.StatusInternalServerError)
		return
	}

	// ── 3. Insérer dans SQLite ──────────────────────────────────────────
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

	slog.Info("Token de reset créé", "user_id", user.ID, "expires_at", expiresAt.Format(time.RFC3339))

	// ── 4. Envoyer l'email ──────────────────────────────────────────────
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
		"JellyTulliURL": links.JellyTulliURL,
	}

	emailCfg, _ := h.db.GetEmailTemplatesConfig()
	tpl := emailCfg.PasswordReset
	if tpl == "" {
		tpl = "Bonjour {{.Username}},\n\nVoici le lien pour réinitialiser votre mot de passe : {{.ResetURL}}"
	}
	subject := firstNonEmpty(emailCfg.PasswordResetSubject, config.DefaultEmailTemplates().PasswordResetSubject)

	if err := sendTemplateIfConfigured(h.mailer, user.Email, subject, "password_reset", tpl, emailCfg, emailData); err != nil {
		slog.Error("Erreur d'envoi de l'email de reset",
			"to", user.Email,
			"error", err,
		)
		// On ne révèle pas l'erreur à l'utilisateur
		_ = h.db.LogAction("reset.email.failed", user.Username, "", err.Error())
	} else {
		slog.Info("Email de reset envoyé", "to", user.Email)
	}

	_ = h.db.LogAction("reset.requested", user.Username, "", fmt.Sprintf("IP: %s", r.RemoteAddr))

	h.renderSuccessPage(w, r, successMsg)
}

// ── GET /reset/{code} ───────────────────────────────────────────────────────

// ResetPage affiche le formulaire de saisie du nouveau mot de passe.
func (h *PasswordResetHandler) ResetPage(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	// Vérifier que le token est valide avant d'afficher le formulaire
	_, _, err := h.getValidResetToken(code)
	if err != nil {
		slog.Warn("Token de reset invalide consulté", "code", code, "error", err)
		http.Error(w, "Lien de réinitialisation invalide ou expiré", http.StatusNotFound)
		return
	}

	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.Data["JellyTulliURL"] = links.JellyTulliURL
	td.ShowNewPasswordForm = true
	td.ResetCode = code

	if err := h.renderer.Render(w, "reset.html", td); err != nil {
		slog.Error("Erreur rendu reset logic", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
	}
}

// ── POST /reset/{code} ──────────────────────────────────────────────────────

// SubmitReset traite la soumission du nouveau mot de passe.
//
// Flux :
//  1. Valider le token (existence, expiration, non-utilisé)
//  2. Valider le nouveau mot de passe
//  3. Appliquer le reset simultanément :
//     • ldap.ResetPassword() — Active Directory
//     • jellyfin.ResetPassword() — Jellyfin
//  4. Marquer le token comme used = true
func (h *PasswordResetHandler) SubmitReset(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Requête invalide", http.StatusBadRequest)
		return
	}

	password := r.FormValue("password")
	passwordConfirm := r.FormValue("password_confirm")

	// ── 1. Valider le token ─────────────────────────────────────────────
	resetRecord, user, err := h.getValidResetToken(code)
	if err != nil {
		slog.Warn("Token de reset invalide", "code", code, "error", err)
		http.Error(w, "Lien de réinitialisation invalide ou expiré", http.StatusForbidden)
		return
	}

	// ── 2. Valider le mot de passe ──────────────────────────────────────
	if password == "" {
		http.Error(w, "Le mot de passe est requis", http.StatusBadRequest)
		return
	}
	if len(password) < 8 {
		http.Error(w, "Le mot de passe doit faire au minimum 8 caractères", http.StatusBadRequest)
		return
	}
	if password != passwordConfirm {
		http.Error(w, "Les mots de passe ne correspondent pas", http.StatusBadRequest)
		return
	}

	slog.Info("Réinitialisation MDP en cours",
		"username", user.Username,
		"user_id", user.ID,
		"remote", r.RemoteAddr,
	)

	// ── 3. Appliquer le reset (LDAP + Jellyfin) ─────────────────────────
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
			slog.Info("Reset LDAP réussi", "dn", user.LdapDN)
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
			slog.Info("Reset Jellyfin réussi", "id", user.JellyfinID)
		}
	}

	// Si les DEUX ont échoué, c'est une erreur critique
	if user.LdapDN != "" && user.JellyfinID != "" && len(errors) == 2 {
		slog.Error("Reset MDP échoué sur TOUS les services",
			"username", user.Username,
			"errors", errors,
		)
		_ = h.db.LogAction("reset.failed.total", user.Username, "", fmt.Sprintf("%v", errors))
		http.Error(w, "Erreur lors de la réinitialisation. Veuillez réessayer ou contacter l'administrateur.", http.StatusInternalServerError)
		return
	}

	// ── 4. Marquer le token comme utilisé ───────────────────────────────
	_, err = h.db.Exec(
		`UPDATE password_resets SET used = TRUE WHERE id = ?`,
		resetRecord.ID,
	)
	if err != nil {
		slog.Error("Erreur de mise à jour du token de reset", "id", resetRecord.ID, "error", err)
		// Non-bloquant : le reset a déjà été appliqué
	}

	// ── 5. Audit log ────────────────────────────────────────────────────
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

	slog.Info("Réinitialisation MDP terminée",
		"username", user.Username,
		"status", status,
		"partial_errors", len(errors),
	)

	// ── Réponse ─────────────────────────────────────────────────────────
	msg := "Votre mot de passe a été réinitialisé avec succès."
	if len(errors) > 0 {
		msg = "Votre mot de passe a été partiellement réinitialisé. Contactez l'administrateur si le problème persiste."
	}

	h.renderSuccessPage(w, r, msg)
}

// ── Méthodes internes ───────────────────────────────────────────────────────

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
		return nil, fmt.Errorf("aucun utilisateur trouvé pour %q", identifier)
	}
	if err != nil {
		return nil, fmt.Errorf("erreur de recherche: %w", err)
	}

	user.Email = email.String
	user.JellyfinID = jellyfinID.String
	user.LdapDN = ldapDN.String

	return &user, nil
}

// getValidResetToken récupère et valide un token de réinitialisation.
// Vérifie : existence, expiration (15 min), non-utilisé.
// Retourne le token ET l'utilisateur associé.
func (h *PasswordResetHandler) getValidResetToken(code string) (*passwordResetRecord, *userRecord, error) {
	if code == "" {
		return nil, nil, fmt.Errorf("code de reset vide")
	}

	// Récupérer le token
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

	// Vérifier non-utilisé
	if rec.Used {
		return nil, nil, fmt.Errorf("token %q déjà utilisé", code)
	}

	// Vérifier expiration
	if time.Now().After(rec.ExpiresAt) {
		return nil, nil, fmt.Errorf("token %q expiré depuis %s",
			code, rec.ExpiresAt.Format("02/01/2006 15:04"))
	}

	// Récupérer l'utilisateur associé
	var user userRecord
	var jellyfinID, email, ldapDN sql.NullString

	err = h.db.QueryRow(
		`SELECT id, username, email, jellyfin_id, ldap_dn FROM users WHERE id = ?`,
		rec.UserID,
	).Scan(&user.ID, &user.Username, &email, &jellyfinID, &ldapDN)

	if err != nil {
		return nil, nil, fmt.Errorf("utilisateur associé au token introuvable: %w", err)
	}

	user.Email = email.String
	user.JellyfinID = jellyfinID.String
	user.LdapDN = ldapDN.String

	return &rec, &user, nil
}

// generateSecureToken génère un token cryptographiquement sûr.
// Utilise crypto/rand pour une entropie maximale.
// Retourne une chaîne hexadécimale de 2*length caractères.
func generateSecureToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("erreur de génération du token: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// renderSuccessPage affiche une page de succès générique.
func (h *PasswordResetHandler) renderSuccessPage(w http.ResponseWriter, r *http.Request, message string) {
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.SuccessMessage = message
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.Data["JellyTulliURL"] = links.JellyTulliURL
	if err := h.renderer.Render(w, "reset.html", td); err != nil {
		slog.Error("Erreur rendu success page", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
	}
}
