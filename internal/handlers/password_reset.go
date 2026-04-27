// Package handlers Ã¢â‚¬â€� password_reset.go
//
// GÃƒÂ¨re le flux complet de rÃƒÂ©initialisation de mot de passe :
//
//  1. Demande (POST /reset/request) :
//     - Recherche l'utilisateur par email ou username dans SQLite
//     - GÃƒÂ©nÃƒÂ¨re un token sÃƒÂ©curisÃƒÂ© (crypto/rand, 32 bytes, hex)
//     - InsÃƒÂ¨re dans la table password_resets (expiration 15 min)
//     - Envoie l'email avec le lien de rÃƒÂ©initialisation
//
//  2. ExÃƒÂ©cution (POST /reset/{code}) :
//     - VÃƒÂ©rifie le token (existence, expiration, non-utilisÃƒÂ©)
//     - Applique le nouveau mot de passe simultanÃƒÂ©ment :
//     Ã¢â‚¬Â¢ ldap.ResetPassword() Ã¢â‚¬â€� Active Directory
//     Ã¢â‚¬Â¢ jellyfin.ResetPassword() Ã¢â‚¬â€� Jellyfin
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
	ID         int64
	Username   string
	Email      string
	JellyfinID string
	LdapDN     string
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

// NewPasswordResetHandler crÃƒÂ©e un nouveau handler de rÃƒÂ©initialisation.
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

// SetLDAPClient remplace le client LDAP (rechargement ÃƒÂ  chaud).
func (h *PasswordResetHandler) SetLDAPClient(ld *jgldap.Client) { h.ldClient = ld }

// SetMailer remplace le mailer (rechargement ÃƒÂ  chaud).
func (h *PasswordResetHandler) SetMailer(m *mail.Mailer) { h.mailer = m }

// Ã¢â€�â‚¬Ã¢â€�â‚¬ GET /reset/ Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

func (h *PasswordResetHandler) RequestPage(w http.ResponseWriter, r *http.Request) {
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.Section = "login"
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.Data["JellyTrackURL"] = links.JellyTrackURL
	td.ShowNewPasswordForm = false
	if err := h.renderer.Render(w, "reset.html", td); err != nil {
		slog.Error("Erreur rendu reset request", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
	}
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ POST /reset/request Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// SubmitRequest traite la demande de rÃƒÂ©initialisation.
//
// Flux :
//  1. Rechercher l'utilisateur par email ou username dans SQLite
//  2. GÃƒÂ©nÃƒÂ©rer un token sÃƒÂ©curisÃƒÂ© (crypto/rand)
//  3. InsÃƒÂ©rer dans la table password_resets (expiration 15 min)
//  4. Envoyer l'email avec le lien
//
// IMPORTANT : Pour ÃƒÂ©viter l'ÃƒÂ©numÃƒÂ©ration d'utilisateurs, on retourne
// toujours le mÃƒÂªme message de succÃƒÂ¨s, que l'utilisateur existe ou non.
func (h *PasswordResetHandler) SubmitRequest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "RequÃƒÂªte invalide", http.StatusBadRequest)
		return
	}

	identifier := strings.TrimSpace(r.FormValue("identifier"))
	if identifier == "" {
		http.Error(w, "Identifiant requis", http.StatusBadRequest)
		return
	}

	slog.Info("Demande de rÃƒÂ©initialisation MDP", "identifier", identifier, "remote", r.RemoteAddr)

	// Message gÃƒÂ©nÃƒÂ©rique (anti-ÃƒÂ©numÃƒÂ©ration)
	successMsg := "Si un compte correspondant existe, un email de rÃƒÂ©initialisation a ÃƒÂ©tÃƒÂ© envoyÃƒÂ©."

	// Ã¢â€�â‚¬Ã¢â€�â‚¬ 1. Rechercher l'utilisateur Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬
	user, err := h.findUserByIdentifier(identifier)
	if err != nil {
		slog.Warn("Utilisateur introuvable pour reset (pas d'erreur visible)",
			"identifier", identifier,
			"error", err,
		)
		// On affiche quand mÃƒÂªme le message de succÃƒÂ¨s (anti-ÃƒÂ©numÃƒÂ©ration)
		h.renderSuccessPage(w, r, successMsg)
		return
	}

	// VÃƒÂ©rifier que l'utilisateur a un email
	if user.Email == "" {
		slog.Warn("Utilisateur sans email, impossible d'envoyer le reset",
			"username", user.Username,
		)
		h.renderSuccessPage(w, r, successMsg)
		return
	}

	// Ã¢â€�â‚¬Ã¢â€�â‚¬ 2. GÃƒÂ©nÃƒÂ©rer un token sÃƒÂ©curisÃƒÂ© Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬
	token, err := generateSecureToken(resetTokenLength)
	if err != nil {
		slog.Error("Erreur de gÃƒÂ©nÃƒÂ©ration du token", "error", err)
		http.Error(w, "Erreur interne", http.StatusInternalServerError)
		return
	}

	// Ã¢â€�â‚¬Ã¢â€�â‚¬ 3. InsÃƒÂ©rer dans SQLite Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬
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

	slog.Info("Token de reset crÃƒÂ©ÃƒÂ©", "user_id", user.ID, "expires_at", expiresAt.Format(time.RFC3339))

	// Ã¢â€�â‚¬Ã¢â€�â‚¬ 4. Envoyer l'email Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬
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
		"Username":      user.Username,
		"ResetLink":     resetURL,
		"ResetURL":      resetURL,
		"ResetCode":     token,
		"ExpiresIn":     "15 minutes",
		"HelpURL":       publicBaseURL,
		"JellyGateURL":  publicBaseURL,
		"JellyfinURL":   links.JellyfinURL,
		"JellyseerrURL": links.JellyseerrURL,
		"JellyTrackURL": links.JellyTrackURL,
	}

	emailCfg, _ := h.db.GetEmailTemplatesConfig()
	tpl := emailCfg.PasswordReset
	if tpl == "" {
		tpl = "Bonjour {{.Username}},\n\nVoici le lien pour rÃƒÂ©initialiser votre mot de passe : {{.ResetURL}}"
	}
	subject := firstNonEmpty(emailCfg.PasswordResetSubject, config.DefaultEmailTemplates().PasswordResetSubject)

	if err := sendTemplateIfConfigured(h.mailer, user.Email, subject, "password_reset", tpl, emailCfg, emailData); err != nil {
		slog.Error("Erreur d'envoi de l'email de reset",
			"to", user.Email,
			"error", err,
		)
		_ = h.db.LogAction("reset.email.failed", user.Username, "", err.Error())
	} else {
		slog.Info("Email de reset envoyÃƒÂ©", "to", user.Email)
		_ = h.db.LogAction("reset.email.sent", user.Username, "", "Email de reinitialisation envoye")
	}

	_ = h.db.LogAction("reset.requested", user.Username, "", fmt.Sprintf("IP: %s", r.RemoteAddr))

	h.renderSuccessPage(w, r, successMsg)
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ GET /reset/{code} Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

func (h *PasswordResetHandler) ResetPage(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.Section = "login"
	// VÃƒÂ©rifier que le token est valide avant d'afficher le formulaire
	_, _, err := h.getValidResetToken(code)
	if err != nil {
		slog.Warn("Token de reset invalide consultÃƒÂ©", "code", code, "error", err)
		http.Error(w, "Lien de rÃƒÂ©initialisation invalide ou expirÃƒÂ©", http.StatusNotFound)
		return
	}
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.Data["JellyTrackURL"] = links.JellyTrackURL
	td.ShowNewPasswordForm = true
	td.ResetCode = code
	if err := h.renderer.Render(w, "reset.html", td); err != nil {
		slog.Error("Erreur rendu reset logic", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
	}
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ POST /reset/{code} Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// SubmitReset traite la soumission du nouveau mot de passe.
//
// Flux :
//  1. Valider le token (existence, expiration, non-utilisÃƒÂ©)
//  2. Valider le nouveau mot de passe
//  3. Appliquer le reset simultanÃƒÂ©ment :
//     Ã¢â‚¬Â¢ ldap.ResetPassword() Ã¢â‚¬â€� Active Directory
//     Ã¢â‚¬Â¢ jellyfin.ResetPassword() Ã¢â‚¬â€� Jellyfin
//  4. Marquer le token comme used = true
func (h *PasswordResetHandler) SubmitReset(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	if err := r.ParseForm(); err != nil {
		http.Error(w, "RequÃƒÂªte invalide", http.StatusBadRequest)
		return
	}

	password := r.FormValue("password")
	passwordConfirm := r.FormValue("password_confirm")

	// Ã¢â€�â‚¬Ã¢â€�â‚¬ 1. Valider le token Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬
	resetRecord, user, err := h.getValidResetToken(code)
	if err != nil {
		slog.Warn("Token de reset invalide", "code", code, "error", err)
		http.Error(w, "Lien de rÃƒÂ©initialisation invalide ou expirÃƒÂ©", http.StatusForbidden)
		return
	}

	// Ã¢â€�â‚¬Ã¢â€�â‚¬ 2. Valider le mot de passe Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬
	if password == "" {
		http.Error(w, "Le mot de passe est requis", http.StatusBadRequest)
		return
	}
	if len(password) < 8 {
		http.Error(w, "Le mot de passe doit faire au minimum 8 caractÃƒÂ¨res", http.StatusBadRequest)
		return
	}
	if password != passwordConfirm {
		http.Error(w, "Les mots de passe ne correspondent pas", http.StatusBadRequest)
		return
	}

	slog.Info("RÃƒÂ©initialisation MDP en cours",
		"username", user.Username,
		"user_id", user.ID,
		"remote", r.RemoteAddr,
	)

	// Ã¢â€�â‚¬Ã¢â€�â‚¬ 3. Appliquer le reset (LDAP + Jellyfin) Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬
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
			slog.Info("Reset LDAP rÃƒÂ©ussi", "dn", user.LdapDN)
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
			slog.Info("Reset Jellyfin rÃƒÂ©ussi", "id", user.JellyfinID)
		}
	}

	// Si les DEUX ont ÃƒÂ©chouÃƒÂ©, c'est une erreur critique
	if user.LdapDN != "" && user.JellyfinID != "" && len(errors) == 2 {
		slog.Error("Reset MDP ÃƒÂ©chouÃƒÂ© sur TOUS les services",
			"username", user.Username,
			"errors", errors,
		)
		_ = h.db.LogAction("reset.failed.total", user.Username, "", fmt.Sprintf("%v", errors))
		http.Error(w, "Erreur lors de la rÃƒÂ©initialisation. Veuillez rÃƒÂ©essayer ou contacter l'administrateur.", http.StatusInternalServerError)
		return
	}

	// Ã¢â€�â‚¬Ã¢â€�â‚¬ 4. Marquer le token comme utilisÃƒÂ© Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬
	_, err = h.db.Exec(
		`UPDATE password_resets SET used = TRUE WHERE id = ?`,
		resetRecord.ID,
	)
	if err != nil {
		slog.Error("Erreur de mise ÃƒÂ  jour du token de reset", "id", resetRecord.ID, "error", err)
		// Non-bloquant : le reset a dÃƒÂ©jÃƒÂ  ÃƒÂ©tÃƒÂ© appliquÃƒÂ©
	}

	// Ã¢â€�â‚¬Ã¢â€�â‚¬ 5. Audit log Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬
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

	slog.Info("RÃƒÂ©initialisation MDP terminÃƒÂ©e",
		"username", user.Username,
		"status", status,
		"partial_errors", len(errors),
	)

	// Ã¢â€�â‚¬Ã¢â€�â‚¬ RÃƒÂ©ponse Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬
	msg := "Votre mot de passe a ÃƒÂ©tÃƒÂ© rÃƒÂ©initialisÃƒÂ© avec succÃƒÂ¨s."
	if len(errors) > 0 {
		msg = "Votre mot de passe a ÃƒÂ©tÃƒÂ© partiellement rÃƒÂ©initialisÃƒÂ©. Contactez l'administrateur si le problÃƒÂ¨me persiste."
	}

	h.renderSuccessPage(w, r, msg)
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ MÃƒÂ©thodes internes Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

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
		return nil, fmt.Errorf("aucun utilisateur trouvÃƒÂ© pour %q", identifier)
	}
	if err != nil {
		return nil, fmt.Errorf("erreur de recherche: %w", err)
	}

	user.Email = email.String
	user.JellyfinID = jellyfinID.String
	user.LdapDN = ldapDN.String

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
	var jellyfinID, email, ldapDN sql.NullString

	err = h.db.QueryRow(
		`SELECT id, username, email, jellyfin_id, ldap_dn FROM users WHERE id = ?`,
		rec.UserID,
	).Scan(&user.ID, &user.Username, &email, &jellyfinID, &ldapDN)

	if err != nil {
		return nil, nil, fmt.Errorf("utilisateur associÃƒÂ© au token introuvable: %w", err)
	}

	user.Email = email.String
	user.JellyfinID = jellyfinID.String
	user.LdapDN = ldapDN.String

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
