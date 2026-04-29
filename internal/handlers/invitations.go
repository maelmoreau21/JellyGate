// Package handlers — invitations.go
//
// Gère le système d'invitations de JellyGate.
// La route POST /invite/{code} implémente un flux de création atomique :
//
//  1. Validation SQLite (code, expiration, quota)
//  2. Création LDAP (Active Directory)
//  3. Création Jellyfin + application du profil
//     → Rollback LDAP si échec
//  4. Enregistrement SQLite (user + incrément used_count)
//     → Rollback Jellyfin + LDAP si échec
//  5. Notifications (email + webhooks) — pas de rollback
package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	netmail "net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/integrations"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	jgldap "github.com/maelmoreau21/JellyGate/internal/ldap"
	"github.com/maelmoreau21/JellyGate/internal/mail"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/notify"
	"github.com/maelmoreau21/JellyGate/internal/render"
)

// Ã¢â€�â‚¬Ã¢â€�â‚¬ Structures internes Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// invitation reprÃƒÂ©sente une ligne de la table invitations.
type invitation struct {
	ID              int64
	Code            string
	Label           string
	MaxUses         int
	UsedCount       int
	JellyfinProfile string // JSON brut du profil
	PreferredLang   string
	ExpiresAt       sql.NullTime
	CreatedBy       string
	CreatedAt       time.Time
}

// inviteFormData contient les donnÃƒÂ©es soumises par le formulaire d'inscription.
type inviteFormData struct {
	Username string
	Email    string
	Password string
}

type inviteSignupResult struct {
	JellyfinID   string
	UserDN       string
	LDAPOnlyMode bool
}

type inviteProvisionPlan struct {
	EffectiveProfile jellyfin.InviteProfile
	MappingPresetID  string
	LDAPGroups       []string
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ Invitation Handler Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// InvitationHandler gÃƒÂ¨re les routes liÃƒÂ©es aux invitations.
type InvitationHandler struct {
	cfg         *config.Config
	db          *database.DB
	jfClient    *jellyfin.Client
	ldClient    *jgldap.Client
	provisioner *integrations.Client
	mailer      *mail.Mailer
	notifier    *notify.Notifier
	renderer    *render.Engine
}

// NewInvitationHandler crÃƒÂ©e un nouveau handler d'invitations.
func NewInvitationHandler(cfg *config.Config, db *database.DB, jf *jellyfin.Client, ld *jgldap.Client, provisioner *integrations.Client, m *mail.Mailer, n *notify.Notifier, renderer *render.Engine) *InvitationHandler {
	return &InvitationHandler{
		cfg:         cfg,
		db:          db,
		jfClient:    jf,
		ldClient:    ld,
		provisioner: provisioner,
		mailer:      m,
		notifier:    n,
		renderer:    renderer,
	}
}

// SetLDAPClient remplace le client LDAP (rechargement ÃƒÂ  chaud).
func (h *InvitationHandler) SetLDAPClient(ld *jgldap.Client) { h.ldClient = ld }

// SetMailer remplace le mailer SMTP (rechargement ÃƒÂ  chaud).
func (h *InvitationHandler) SetMailer(m *mail.Mailer) { h.mailer = m }

// SetNotifier remplace le notifier (rechargement ÃƒÂ  chaud).
func (h *InvitationHandler) SetNotifier(n *notify.Notifier) { h.notifier = n }

func (h *InvitationHandler) tr(r *http.Request, key, fallback string) string {
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

func (h *InvitationHandler) logInviteAction(r *http.Request, action, actor, target, details string) {
	reqID := strings.TrimSpace(chimw.GetReqID(r.Context()))
	if reqID != "" {
		if strings.TrimSpace(details) == "" {
			details = "request_id=" + reqID
		} else {
			details = details + "; request_id=" + reqID
		}
	}
	_ = h.db.LogAction(action, actor, target, details)
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ GET /invite/{code} Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// InvitePage affiche le formulaire d'inscription pour un code d'invitation donnÃƒÂ©.
func (h *InvitationHandler) InvitePage(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	// VÃƒÂ©rifier que l'invitation existe et est valide
	inv, err := h.getValidInvitation(code)
	if err != nil {
		slog.Warn("Invitation invalide consultÃƒÂ©e", "code", code, "error", err)
		http.Error(w, h.tr(r, "invite_error_invalid_or_expired", "Invitation invalide ou expirÃƒÂ©e"), http.StatusNotFound)
		return
	}

	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.Section = "login"
	td.Invitation = inv
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.Data["JellyTrackURL"] = links.JellyTrackURL
	profile := jellyfin.InviteProfile{UsernameMinLength: 3, UsernameMaxLength: 32, PasswordMinLength: 8, PasswordMaxLength: 128, RequireEmail: true, RequireEmailVerification: true}

	// Analyser le profil pour vÃƒÂ©rifier si un username est forcÃƒÂ© (Flux B)
	if inv.JellyfinProfile != "" {
		if err := json.Unmarshal([]byte(inv.JellyfinProfile), &profile); err != nil {
			slog.Warn("Profil Jellyfin invalide dans invitation page", "code", code, "error", err)
		} else if profile.ForcedUsername != "" {
			td.Data["ForcedUsername"] = profile.ForcedUsername
		}
	}

	td.Data["RequireEmail"] = profile.RequireEmail

	pwdPolicy := resolveInvitePasswordPolicy(profile)
	usernameMin, usernameMax := resolveInviteUsernamePolicy(profile)
	td.Data["UsernameMinLength"] = usernameMin
	td.Data["UsernameMaxLength"] = usernameMax
	td.Data["PasswordMinLength"] = pwdPolicy.MinLength
	td.Data["PasswordMaxLength"] = pwdPolicy.MaxLength
	td.Data["PasswordRequireUpper"] = pwdPolicy.RequireUpper
	td.Data["PasswordRequireLower"] = pwdPolicy.RequireLower
	td.Data["PasswordRequireDigit"] = pwdPolicy.RequireDigit
	td.Data["PasswordRequireSpecial"] = pwdPolicy.RequireSpecial

	if err := h.renderer.Render(w, "invite.html", td); err != nil {
		slog.Error("Erreur rendu invitation page", "error", err)
		http.Error(w, h.tr(r, "common_server_error", "Erreur serveur"), http.StatusInternalServerError)
	}
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ POST /invite/{code} Ã¢â‚¬â€� FLUX ATOMIQUE Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// InviteSubmit traite la soumission du formulaire d'inscription.
//
// Flux atomique avec rollback strict :
//
//	Ãƒâ€°tape 1 : Validation SQLite      Ã¢â€ â€™ erreur = stop (rien ÃƒÂ  nettoyer)
//	Ãƒâ€°tape 2 : CrÃƒÂ©ation LDAP          Ã¢â€ â€™ erreur = stop (rien ÃƒÂ  nettoyer)
//	Ãƒâ€°tape 3 : CrÃƒÂ©ation Jellyfin      Ã¢â€ â€™ erreur = rollback LDAP
//	Ãƒâ€°tape 4 : Enregistrement SQLite   Ã¢â€ â€™ erreur = rollback Jellyfin + LDAP
//	Ãƒâ€°tape 5 : Notifications           Ã¢â€ â€™ erreur = log seulement (pas de rollback)
func (h *InvitationHandler) InviteSubmit(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	remoteAddr := r.RemoteAddr

	slog.Info("Ã¢Å¡Â¡ DÃƒÂ©but du flux d'inscription",
		"code", code,
		"remote", remoteAddr,
	)

	// Ã¢â€�â‚¬Ã¢â€�â‚¬ Parsing du formulaire Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬
	if err := r.ParseForm(); err != nil {
		slog.Error("Erreur parsing formulaire inscription", "error", err)
		http.Error(w, h.tr(r, "common_bad_request", "RequÃƒÂªte invalide"), http.StatusBadRequest)
		return
	}

	submittedUsername := strings.TrimSpace(r.FormValue("username"))

	// Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�
	// Ãƒâ€°TAPE 1 : Validation SQLite
	// Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�Ã¢â€¢Â�
	slog.Info("Ã°Å¸â€œâ€¹ Ãƒâ€°tape 1/5 : Validation de l'invitation", "code", code)

	inv, err := h.getValidInvitation(code)
	if err != nil {
		slog.Warn("Invitation invalide", "code", code, "error", err)
		targetUsername := strings.TrimSpace(submittedUsername)
		if targetUsername == "" {
			targetUsername = "unknown"
		}
		h.logInviteAction(r, "invite.validation.failed", targetUsername, code, err.Error())
		http.Error(w, h.tr(r, "invite_error_invalid_or_expired", "Invitation invalide ou expirÃƒÂ©e"), http.StatusForbidden)
		return
	}

	// DÃƒÂ©coder le profil Jellyfin de l'invitation (si dÃƒÂ©fini)
	profile := jellyfin.InviteProfile{RequireEmail: true, RequireEmailVerification: true}
	if inv.JellyfinProfile != "" {
		if err := json.Unmarshal([]byte(inv.JellyfinProfile), &profile); err != nil {
			slog.Error("Profil Jellyfin invalide dans l'invitation", "code", code, "error", err)
			http.Error(w, h.tr(r, "invite_error_config", "Erreur de configuration de l'invitation"), http.StatusInternalServerError)
			return
		}
	} else {
		// Profil par dÃƒÂ©faut : accÃƒÂ¨s ÃƒÂ  toutes les bibliothÃƒÂ¨ques
		profile = jellyfin.InviteProfile{
			RequireEmail:             true,
			RequireEmailVerification: true,
			EnableAllFolders:         true,
			EnableDownload:           true,
			EnableRemoteAccess:       true,
		}
	}

	form, err := h.validateForm(r, &profile)
	if err != nil {
		slog.Warn("Formulaire d'inscription invalide", "code", code, "error", err)
		targetUsername := strings.TrimSpace(submittedUsername)
		if targetUsername == "" {
			targetUsername = "unknown"
		}
		h.logInviteAction(r, "invite.validation.failed", targetUsername, code, err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if profile.ForcedUsername != "" {
		slog.Debug("Flux JFA-Go (Forced Username) détecté", "forced", profile.ForcedUsername, "submitted", form.Username)
		form.Username = profile.ForcedUsername
		if err := h.validateInviteUsername(r, form.Username, &profile); err != nil {
			slog.Error("Nom d'utilisateur forcé invalide", "code", code, "forced_username", profile.ForcedUsername, "error", err)
			http.Error(w, h.tr(r, "invite_error_config", "Erreur de configuration de l'invitation"), http.StatusInternalServerError)
			return
		}
	}

	if err := h.ensureInviteUsernameAvailable(r, form.Username); err != nil {
		slog.Warn("Nom d'utilisateur indisponible pour invitation", "code", code, "username", form.Username, "error", err)
		h.logInviteAction(r, "invite.validation.failed", form.Username, code, err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	slog.Info("✅ Étape 1/5 terminée", "code", code, "uses", fmt.Sprintf("%d/%d", inv.UsedCount, inv.MaxUses))

	if profile.RequireEmailVerification {
		if err := h.createPendingInviteSignup(r, inv, form); err != nil {
			slog.Error("Impossible de préparer la vérification email avant création", "username", form.Username, "email", form.Email, "error", err)
			h.logInviteAction(r, "invite.email_verification.failed", form.Username, code, err.Error())
			statusCode := http.StatusInternalServerError
			message := err.Error()
			if strings.Contains(strings.ToLower(err.Error()), "smtp") {
				statusCode = http.StatusServiceUnavailable
				message = h.tr(r, "invite_error_email_verification_unavailable", "La vÃƒÂ©rification par email est activÃƒÂ©e, mais l'envoi d'emails n'est pas disponible actuellement.")
			} else if strings.Contains(strings.ToLower(err.Error()), "dÃƒÂ©jÃƒÂ  utilisÃƒÂ©") {
				statusCode = http.StatusConflict
			}
			http.Error(w, message, statusCode)
			return
		}

		h.renderInviteSuccessPage(
			w,
			r,
			inv,
			strings.ReplaceAll(
				h.tr(r, "invite_success_pending_verification", "VÃƒÂ©rifiez maintenant votre email pour confirmer la crÃƒÂ©ation de votre compte {username}. Le compte sera crÃƒÂ©ÃƒÂ© uniquement aprÃƒÂ¨s cette confirmation."),
				"{username}",
				form.Username,
			),
			false,
		)
		return
	}

	result, err := h.completeInviteSignup(r, inv, form, profile, strings.TrimSpace(form.Email) != "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if result.LDAPOnlyMode {
		h.renderInviteSuccessPage(
			w,
			r,
			inv,
			strings.ReplaceAll(
				h.tr(r, "invite_success_ldap_only", "Bienvenue {username} ! Votre compte a ÃƒÂ©tÃƒÂ© crÃƒÂ©ÃƒÂ© dans l'annuaire LDAP. L'accÃƒÂ¨s Jellyfin utilisera votre integration LDAP."),
				"{username}",
				form.Username,
			),
			true,
		)
		return
	}

	h.renderInviteSuccessPage(
		w,
		r,
		inv,
		strings.ReplaceAll(
			h.tr(r, "invite_success_hybrid", "Bienvenue {username} ! Votre compte a ete cree dans Jellyfin et dans l'annuaire LDAP."),
			"{username}",
			form.Username,
		),
		true,
	)
}

func (h *InvitationHandler) renderInviteSuccessPage(w http.ResponseWriter, r *http.Request, inv *invitation, message string, accountCreated bool) {
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.Section = "login"
	td.Invitation = inv
	td.SuccessMessage = message
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["AccountCreated"] = accountCreated
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.Data["JellyTrackURL"] = links.JellyTrackURL

	if err := h.renderer.Render(w, "invite.html", td); err != nil {
		slog.Error("Erreur rendu invite success page", "error", err)
		http.Error(w, h.tr(r, "common_server_error", "Erreur serveur"), http.StatusInternalServerError)
	}
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ MÃƒÂ©thodes internes Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// validateForm valide et extrait les donnÃƒÂ©es du formulaire d'inscription.
func (h *InvitationHandler) validateForm(r *http.Request, profile *jellyfin.InviteProfile) (*inviteFormData, error) {
	username := strings.TrimSpace(r.FormValue("username"))
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	passwordConfirm := r.FormValue("password_confirm")

	// Validations
	if err := h.validateInviteUsername(r, username, profile); err != nil {
		return nil, err
	}

	if password == "" {
		return nil, fmt.Errorf("le mot de passe est requis")
	}

	if err := h.validateInvitePassword(r, password, profile); err != nil {
		return nil, err
	}

	if password != passwordConfirm {
		return nil, fmt.Errorf("les mots de passe ne correspondent pas")
	}

	requireEmail := true
	if profile != nil {
		requireEmail = profile.RequireEmail
	}
	if requireEmail && email == "" {
		return nil, fmt.Errorf("l'adresse email est obligatoire")
	}
	if email != "" {
		if _, err := netmail.ParseAddress(email); err != nil {
			return nil, fmt.Errorf("adresse email invalide")
		}
	}

	return &inviteFormData{
		Username: username,
		Email:    email,
		Password: password,
	}, nil
}

type invitePasswordPolicy struct {
	MinLength      int
	MaxLength      int
	RequireUpper   bool
	RequireLower   bool
	RequireDigit   bool
	RequireSpecial bool
}

func resolveInviteUsernamePolicy(profile jellyfin.InviteProfile) (int, int) {
	minLength := profile.UsernameMinLength
	maxLength := profile.UsernameMaxLength

	if minLength <= 0 {
		minLength = 3
	}
	if maxLength <= 0 {
		maxLength = 32
	}
	if maxLength < minLength {
		maxLength = minLength
	}

	return minLength, maxLength
}

func resolveLDAPProvisionRole(profile jellyfin.InviteProfile) string {
	if profile.CanInvite {
		return jgldap.ProvisionRoleInviter
	}

	groupName := strings.ToLower(strings.TrimSpace(profile.GroupName))
	switch groupName {
	case "admin", "admins", "administrator", "administrators":
		return jgldap.ProvisionRoleAdmin
	case "inviter", "inviters", "parrainage", "sponsor", "sponsors":
		return jgldap.ProvisionRoleInviter
	default:
		return jgldap.ProvisionRoleUser
	}
}

func roleAllowsInvites(role string) bool {
	normalized := strings.ToLower(strings.TrimSpace(role))
	return normalized == jgldap.ProvisionRoleInviter || normalized == jgldap.ProvisionRoleAdmin
}

func resolveInvitePasswordPolicy(profile jellyfin.InviteProfile) invitePasswordPolicy {
	minLength := profile.PasswordMinLength
	maxLength := profile.PasswordMaxLength
	if minLength <= 0 {
		minLength = 8
	}
	if maxLength <= 0 {
		maxLength = 128
	}
	if maxLength < minLength {
		maxLength = minLength
	}

	return invitePasswordPolicy{
		MinLength:      minLength,
		MaxLength:      maxLength,
		RequireUpper:   profile.PasswordRequireUpper,
		RequireLower:   profile.PasswordRequireLower,
		RequireDigit:   profile.PasswordRequireDigit,
		RequireSpecial: profile.PasswordRequireSpecial,
	}
}

func (h *InvitationHandler) validateInviteUsername(r *http.Request, username string, profile *jellyfin.InviteProfile) error {
	usernamePolicy := jellyfin.InviteProfile{}
	if profile != nil {
		usernamePolicy = *profile
	}
	minLength, maxLength := resolveInviteUsernamePolicy(usernamePolicy)

	if username == "" {
		return errors.New(h.tr(r, "field_username_required", "Username is required"))
	}
	if len(username) < minLength || len(username) > maxLength {
		msg := h.tr(r, "field_username_min_max", "Username must be between {min} and {max} characters")
		msg = strings.ReplaceAll(msg, "{min}", fmt.Sprintf("%d", minLength))
		msg = strings.ReplaceAll(msg, "{max}", fmt.Sprintf("%d", maxLength))
		return errors.New(msg)
	}

	for _, c := range username {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return errors.New(h.tr(r, "field_username_invalid_chars", "Username can only contain letters, numbers, dashes, and underscores"))
		}
	}

	return h.ensureInviteUsernameAvailable(r, username)
}

func (h *InvitationHandler) validateInvitePassword(r *http.Request, password string, profile *jellyfin.InviteProfile) error {
	policy := resolveInvitePasswordPolicy(jellyfin.InviteProfile{})
	if profile != nil {
		policy = resolveInvitePasswordPolicy(*profile)
	}

	if len(password) < policy.MinLength {
		msg := h.tr(r, "password_rule_at_least", "at least {n} characters")
		msg = strings.ReplaceAll(msg, "{n}", fmt.Sprintf("%d", policy.MinLength))
		return errors.New(msg)
	}
	if len(password) > policy.MaxLength {
		msg := h.tr(r, "password_rule_at_most", "at most {n} characters")
		msg = strings.ReplaceAll(msg, "{n}", fmt.Sprintf("%d", policy.MaxLength))
		return errors.New(msg)
	}
	if policy.RequireUpper && !strings.ContainsAny(password, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		return errors.New(h.tr(r, "password_rule_upper", "one uppercase letter"))
	}
	if policy.RequireLower && !strings.ContainsAny(password, "abcdefghijklmnopqrstuvwxyz") {
		return errors.New(h.tr(r, "password_rule_lower", "one lowercase letter"))
	}
	if policy.RequireDigit && !strings.ContainsAny(password, "0123456789") {
		return errors.New(h.tr(r, "password_rule_digit", "one digit"))
	}
	if policy.RequireSpecial {
		hasSpecial := false
		for _, c := range password {
			isLower := c >= 'a' && c <= 'z'
			isUpper := c >= 'A' && c <= 'Z'
			isDigit := c >= '0' && c <= '9'
			if !isLower && !isUpper && !isDigit {
				hasSpecial = true
				break
			}
		}
		if !hasSpecial {
			return errors.New(h.tr(r, "password_rule_special", "one special character"))
		}
	}

	return nil
}

// getValidInvitation rÃƒÂ©cupÃƒÂ¨re et valide une invitation depuis SQLite.
// VÃƒÂ©rifie : existence, expiration, et quota d'utilisation.
func (h *InvitationHandler) getValidInvitation(code string) (*invitation, error) {
	if code == "" {
		return nil, fmt.Errorf("code d'invitation vide")
	}

	cleanupClosedInvitationsIfEnabled(h.db)

	row := h.db.QueryRow(
		`SELECT id, code, label, max_uses, used_count, jellyfin_profile, preferred_lang, expires_at, created_by, created_at
		 FROM invitations WHERE code = ?`, code)

	var inv invitation
	var jellyfinProfile sql.NullString
	var label sql.NullString
	var createdBy, preferredLang sql.NullString

	err := row.Scan(
		&inv.ID, &inv.Code, &label, &inv.MaxUses, &inv.UsedCount,
		&jellyfinProfile, &preferredLang, &inv.ExpiresAt, &createdBy, &inv.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invitation %q introuvable", code)
	}
	if err != nil {
		return nil, fmt.Errorf("erreur de lecture de l'invitation: %w", err)
	}

	// Reconstituer les champs nullable
	inv.Label = label.String
	inv.JellyfinProfile = jellyfinProfile.String
	inv.PreferredLang = strings.TrimSpace(preferredLang.String)
	inv.CreatedBy = createdBy.String

	// VÃƒÂ©rifier l'expiration
	if inv.ExpiresAt.Valid && time.Now().After(inv.ExpiresAt.Time) {
		return nil, fmt.Errorf("invitation %q expirÃƒÂ©e depuis %s", code, inv.ExpiresAt.Time.Format("02/01/2006 15:04"))
	}

	// VÃƒÂ©rifier le quota d'utilisation (0 = illimitÃƒÂ©)
	if inv.MaxUses > 0 && inv.UsedCount >= inv.MaxUses {
		return nil, fmt.Errorf("invitation %q a atteint sa limite d'utilisation (%d/%d)", code, inv.UsedCount, inv.MaxUses)
	}

	return &inv, nil
}

func (h *InvitationHandler) ensureInviteUsernameAvailable(r *http.Request, username string) error {
	if strings.TrimSpace(username) == "" {
		return errors.New(h.tr(r, "field_username_required", "Username is required"))
	}

	var existingUserID int64
	err := h.db.QueryRow(`SELECT id FROM users WHERE lower(username) = lower(?) LIMIT 1`, username).Scan(&existingUserID)
	if err == nil {
		return errors.New(h.tr(r, "field_username_taken", "This username is already taken"))
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("impossible de vÃƒÂ©rifier la disponibilitÃƒÂ© du nom d'utilisateur: %w", err)
	}

	return nil
}

func (h *InvitationHandler) completeInviteSignup(r *http.Request, inv *invitation, form *inviteFormData, profile jellyfin.InviteProfile, emailVerified bool) (*inviteSignupResult, error) {
	ldapCfg, _ := h.db.GetLDAPConfig()
	ldapOnlyMode := h.ldClient != nil && ldapCfg.Enabled && strings.EqualFold(strings.TrimSpace(ldapCfg.ProvisionMode), "ldap_only")
	createJellyfinUser := !ldapOnlyMode

	provisionPlan := inviteProvisionPlan{EffectiveProfile: profile}
	if resolvedPlan, err := h.resolveInviteProvisionPlan(profile); err != nil {
		slog.Warn(
			"Impossible de resoudre le mapping LDAP -> preset pour l'invitation (fallback sur profil invitation)",
			"group", strings.TrimSpace(profile.GroupName),
			"preset_id", strings.TrimSpace(profile.PresetID),
			"error", err,
		)
	} else {
		provisionPlan = resolvedPlan
		if strings.TrimSpace(provisionPlan.MappingPresetID) != "" {
			slog.Info(
				"Mapping LDAP -> preset resolu pour l'invitation",
				"group", strings.TrimSpace(profile.GroupName),
				"mapping_preset_id", provisionPlan.MappingPresetID,
			)
		}
	}

	ldapProvisionRole := resolveLDAPProvisionRole(profile)
	if ldapProvisionRole != jgldap.ProvisionRoleUser {
		slog.Info("Provisioning LDAP role detecte depuis le profil d'invitation",
			"role", ldapProvisionRole,
			"can_invite", profile.CanInvite,
			"group_name", strings.TrimSpace(profile.GroupName),
			"preset_id", strings.TrimSpace(profile.PresetID),
		)
	}

	var userDN string
	if h.ldClient != nil {
		slog.Info("Ã°Å¸â€�Â� Ãƒâ€°tape 2/5 : CrÃƒÂ©ation du compte LDAP", "username", form.Username)

		createdDN, err := h.ldClient.CreateUser(form.Username, form.Username, form.Email, form.Password, ldapProvisionRole)
		if err != nil {
			slog.Error("Ã¢Â�Å’ Ãƒâ€°tape 2/5 ÃƒÂ©chouÃƒÂ©e : crÃƒÂ©ation LDAP", "username", form.Username, "error", err)
			h.logInviteAction(r, "invite.ldap.failed", form.Username, inv.Code, err.Error())
			return nil, fmt.Errorf("%s", h.tr(r, "invite_error_ldap_create", "Erreur lors de la crÃƒÂ©ation du compte (LDAP)"))
		}

		userDN = createdDN
		slog.Info("Ã¢Å“â€¦ Ãƒâ€°tape 2/5 terminÃƒÂ©e", "dn", userDN)
	} else {
		slog.Info("Ã¢Â�Â­Ã¯Â¸Â� Ãƒâ€°tape 2/5 ignorÃƒÂ©e (LDAP dÃƒÂ©sactivÃƒÂ©)")
	}

	if h.ldClient != nil && strings.TrimSpace(userDN) != "" {
		targetGroups := resolveLDAPProvisionGroups(ldapCfg, provisionPlan.LDAPGroups)
		for _, groupRef := range targetGroups {
			if err := h.ldClient.AddUserToGroup(userDN, groupRef); err != nil {
				slog.Warn(
					"Assignation groupe LDAP echouee pendant provisioning invitation",
					"username", form.Username,
					"dn", userDN,
					"group_ref", groupRef,
					"error", err,
				)
				h.logInviteAction(r, "invite.group_mapping.failed", form.Username, userDN, fmt.Sprintf("%s: %v", groupRef, err))
			}
		}
	}

	var jellyfinID string
	if createJellyfinUser {
		slog.Info("Ã°Å¸Å½Â¬ Ãƒâ€°tape 3/5 : CrÃƒÂ©ation du compte Jellyfin", "username", form.Username)

		jfUser, err := h.jfClient.CreateUser(form.Username, form.Password)
		if err != nil {
			slog.Error("Ã¢Â�Å’ Ãƒâ€°tape 3/5 ÃƒÂ©chouÃƒÂ©e : crÃƒÂ©ation Jellyfin", "username", form.Username, "error", err)
			if h.ldClient != nil && userDN != "" {
				slog.Warn("Ã°Å¸â€�â€ž Rollback : suppression du compte LDAP", "dn", userDN)
				if rbErr := h.ldClient.DeleteUser(userDN); rbErr != nil {
					slog.Error("Ã¢Å¡Â Ã¯Â¸Â� ROLLBACK LDAP Ãƒâ€°CHOUÃƒâ€° Ã¢â‚¬â€� intervention manuelle requise", "dn", userDN, "rollback_error", rbErr, "original_error", err)
					h.logInviteAction(r, "invite.rollback.ldap.failed", form.Username, userDN, rbErr.Error())
				} else {
					slog.Info("Ã¢Å“â€¦ Rollback LDAP rÃƒÂ©ussi", "dn", userDN)
				}
			}

			h.logInviteAction(r, "invite.jellyfin.failed", form.Username, inv.Code, err.Error())
			return nil, fmt.Errorf("%s", h.tr(r, "invite_error_jellyfin_create", "Erreur lors de la crÃƒÂ©ation du compte (Jellyfin)"))
		}

		jellyfinID = jfUser.ID

		if err := h.jfClient.ApplyInviteProfile(jfUser.ID, provisionPlan.EffectiveProfile); err != nil {
			slog.Warn("Erreur lors de l'application du profil Jellyfin (non-bloquant)", "jellyfin_id", jfUser.ID, "error", err)
			h.logInviteAction(r, "invite.profile.failed", form.Username, jfUser.ID, err.Error())
		}

		slog.Info("Ã¢Å“â€¦ Ãƒâ€°tape 3/5 terminÃƒÂ©e", "jellyfin_id", jfUser.ID)
	} else {
		slog.Info("Ã¢Â�Â­Ã¯Â¸Â� Ãƒâ€°tape 3/5 ignorÃƒÂ©e (mode LDAP-only)", "username", form.Username)
	}

	slog.Info("Ã°Å¸â€™Â¾ Ãƒâ€°tape 4/5 : Enregistrement SQLite", "username", form.Username)
	if err := h.registerUser(form, inv, provisionPlan.EffectiveProfile, jellyfinID, userDN, ldapProvisionRole, emailVerified); err != nil {
		slog.Error("Ã¢Â�Å’ Ãƒâ€°tape 4/5 ÃƒÂ©chouÃƒÂ©e : enregistrement SQLite", "username", form.Username, "error", err)
		slog.Warn("Ã°Å¸â€�â€ž Rollback : suppression Jellyfin + LDAP")

		if createJellyfinUser && strings.TrimSpace(jellyfinID) != "" {
			if rbErr := h.jfClient.DeleteUser(jellyfinID); rbErr != nil {
				slog.Error("Ã¢Å¡Â Ã¯Â¸Â� ROLLBACK JELLYFIN Ãƒâ€°CHOUÃƒâ€° Ã¢â‚¬â€� intervention manuelle requise", "jellyfin_id", jellyfinID, "rollback_error", rbErr)
				h.logInviteAction(r, "invite.rollback.jellyfin.failed", form.Username, jellyfinID, rbErr.Error())
			} else {
				slog.Info("Ã¢Å“â€¦ Rollback Jellyfin rÃƒÂ©ussi", "id", jellyfinID)
			}
		}

		if h.ldClient != nil && userDN != "" {
			if rbErr := h.ldClient.DeleteUser(userDN); rbErr != nil {
				slog.Error("Ã¢Å¡Â Ã¯Â¸Â� ROLLBACK LDAP Ãƒâ€°CHOUÃƒâ€° Ã¢â‚¬â€� intervention manuelle requise", "dn", userDN, "rollback_error", rbErr)
				h.logInviteAction(r, "invite.rollback.ldap.failed", form.Username, userDN, rbErr.Error())
			} else {
				slog.Info("Ã¢Å“â€¦ Rollback LDAP rÃƒÂ©ussi", "dn", userDN)
			}
		}

		h.logInviteAction(r, "invite.sqlite.failed", form.Username, inv.Code, err.Error())
		return nil, fmt.Errorf("%s", h.tr(r, "invite_error_persist", "Erreur lors de l'enregistrement du compte"))
	}

	slog.Info("Ã¢Å“â€¦ Ãƒâ€°tape 4/5 terminÃƒÂ©e", "username", form.Username)
	slog.Info("Ã°Å¸â€œÂ¨ Ãƒâ€°tape 5/5 : Notifications", "username", form.Username)

	h.notifier.NotifyUserRegistered(notify.UserRegisteredEvent{
		Username:    form.Username,
		DisplayName: form.Username,
		Email:       form.Email,
		InviteCode:  inv.Code,
		InvitedBy:   inv.CreatedBy,
		JellyfinID:  jellyfinID,
		LdapDN:      userDN,
	})

	if h.mailer != nil && strings.TrimSpace(form.Email) != "" {
		emailCfg, usedLang, cfgErr := loadEmailTemplatesForLanguage(h.db, strings.TrimSpace(inv.PreferredLang), emailLanguageContext{
			GroupName: strings.TrimSpace(provisionPlan.EffectiveProfile.GroupName),
		})
		if cfgErr != nil {
			emailCfg = config.DefaultEmailTemplatesForLanguage(usedLang)
		}
		defaults := config.DefaultEmailTemplatesForLanguage(usedLang)
		links := resolvePortalLinks(h.cfg, h.db)
		publicBaseURL := strings.TrimRight(strings.TrimSpace(links.JellyGateURL), "/")
		if publicBaseURL == "" {
			publicBaseURL = strings.TrimRight(strings.TrimSpace(h.cfg.BaseURL), "/")
		}
		sections := make([]string, 0, 4)
		subjectCandidates := make([]string, 0, 3)
		if !emailCfg.DisableWelcomeEmail {
			sections = append(sections, emailCfg.Welcome)
			subjectCandidates = append(subjectCandidates, emailCfg.WelcomeSubject)
		}
		if !emailCfg.DisableConfirmationEmail {
			sections = append(sections, emailCfg.Confirmation)
			subjectCandidates = append(subjectCandidates, emailCfg.ConfirmationSubject)
		}
		if !emailCfg.DisablePostSignupHelpEmail {
			sections = append(sections, emailCfg.PostSignupHelp)
		}
		if !emailCfg.DisableUserCreationEmail {
			sections = append(sections, emailCfg.UserCreation)
			subjectCandidates = append(subjectCandidates, emailCfg.UserCreationSubject)
		}
		combinedTemplate := joinTemplateSections(sections...)

		if combinedTemplate != "" {
			emailData := map[string]string{
				"Username":           form.Username,
				"DisplayName":        form.Username,
				"Email":              form.Email,
				"InviteCode":         inv.Code,
				"InviteLink":         publicBaseURL + "/invite/" + inv.Code,
				"HelpURL":            publicBaseURL,
				"JellyGateURL":       publicBaseURL,
				"JellyfinURL":        links.JellyfinURL,
				"JellyfinServerName": links.JellyfinServerName,
				"JellyseerrURL":      links.JellyseerrURL,
				"JellyTrackURL":      links.JellyTrackURL,
			}
			subject := firstNonEmpty(append(subjectCandidates, defaults.WelcomeSubject)...)
			if err := sendTemplateIfConfigured(h.mailer, form.Email, subject, usedLang, "welcome", combinedTemplate, emailCfg, emailData); err != nil {
				slog.Error("Erreur envoi email post-inscription", "email", form.Email, "error", err)
				h.logInviteAction(r, "invite.welcome_email.failed", form.Username, inv.Code, err.Error())
			} else {
				h.logInviteAction(r, "invite.welcome_email.sent", form.Username, inv.Code, "Email de bienvenue envoye")
			}
		}
	}

	if h.provisioner != nil && h.provisioner.IsEnabled() {
		if err := h.provisioner.ProvisionUser(form.Username, form.Password, form.Email); err != nil {
			slog.Warn("Provisioning compte tiers ÃƒÂ©chouÃƒÂ©", "username", form.Username, "error", err)
			h.logInviteAction(r, "invite.integration.failed", form.Username, inv.Code, err.Error())
		} else {
			h.logInviteAction(r, "invite.integration.provisioned", form.Username, inv.Code, "Jellyseerr/Ombi")
		}
	}

	h.logInviteAction(r, "invite.used", form.Username, inv.Code,
		fmt.Sprintf(`{"jellyfin_id":"%s","ldap_dn":"%s","email":"%s","mode":"%s"}`,
			jellyfinID,
			userDN,
			form.Email,
			map[bool]string{true: "ldap_only", false: "hybrid"}[ldapOnlyMode],
		))

	slog.Info("Ã°Å¸Å½â€° Inscription terminÃƒÂ©e avec succÃƒÂ¨s", "username", form.Username, "jellyfin_id", jellyfinID, "ldap_dn", userDN, "invitation", inv.Code)

	if h.notifier != nil {
		h.notifier.NotifyUserRegistered(notify.UserRegisteredEvent{
			Username:    form.Username,
			DisplayName: form.Username,
			Email:       form.Email,
			InviteCode:  inv.Code,
			InvitedBy:   inv.CreatedBy,
			JellyfinID:  jellyfinID,
			LdapDN:      userDN,
			Timestamp:   time.Now(),
		})
	}

	return &inviteSignupResult{
		JellyfinID:   jellyfinID,
		UserDN:       userDN,
		LDAPOnlyMode: ldapOnlyMode,
	}, nil
}

// registerUser insÃƒÂ¨re l'utilisateur dans SQLite et incrÃƒÂ©mente le compteur
// d'utilisation de l'invitation. Les deux opÃƒÂ©rations sont dans une transaction.
func (h *InvitationHandler) registerUser(form *inviteFormData, inv *invitation, profile jellyfin.InviteProfile, jellyfinID, ldapDN, ldapRole string, emailVerified bool) error {
	tx, err := h.db.Begin()
	if err != nil {
		return fmt.Errorf("impossible de dÃƒÂ©marrer la transaction: %w", err)
	}
	defer tx.Rollback() // No-op si Commit() a ÃƒÂ©tÃƒÂ© appelÃƒÂ©

	disableAfterDays := profile.DisableAfterDays
	if disableAfterDays <= 0 {
		disableAfterDays = profile.UserExpiryDays
	}

	var absoluteUserExpiryAt time.Time
	expiryAction := normalizeExpiryAction(profile.ExpiryAction)
	deleteAfterDays := 0
	groupName := strings.TrimSpace(profile.GroupName)
	canInviteFromProfile := profile.CanInvite
	var presetID interface{}

	if profile.DeleteAfterDays > 0 {
		deleteAfterDays = profile.DeleteAfterDays
	}
	if strings.TrimSpace(profile.UserExpiresAt) != "" {
		if parsed, err := parseAccessExpiry(profile.UserExpiresAt); err == nil {
			absoluteUserExpiryAt = parsed
		}
	}
	if strings.TrimSpace(profile.PresetID) != "" {
		presetID = strings.TrimSpace(strings.ToLower(profile.PresetID))
	}

	var accessExpiresAt interface{}
	if !absoluteUserExpiryAt.IsZero() {
		accessExpiresAt = absoluteUserExpiryAt
	} else if disableAfterDays > 0 {
		accessExpiresAt = time.Now().AddDate(0, 0, disableAfterDays)
	}

	var deleteAt interface{}
	if deleteAfterDays > 0 {
		deleteAt = time.Now().AddDate(0, 0, deleteAfterDays)
	}

	var jellyfinIDValue interface{}
	if strings.TrimSpace(jellyfinID) == "" {
		jellyfinIDValue = nil
	} else {
		jellyfinIDValue = jellyfinID
	}

	canInvite := roleAllowsInvites(ldapRole) || canInviteFromProfile
	preferredLang := normalizeSupportedEmailLang(inv.PreferredLang)

	// INSERT de l'utilisateur
	_, err = tx.Exec(
		`INSERT INTO users (jellyfin_id, username, email, email_verified, ldap_dn, group_name, invited_by, preferred_lang, is_active, is_banned, can_invite, access_expires_at, delete_at, expiry_action, expiry_delete_after_days, expired_at, preset_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, TRUE, FALSE, ?, ?, ?, ?, ?, NULL, ?)`,
		jellyfinIDValue, form.Username, form.Email, emailVerified, ldapDN, groupName, inv.Code, preferredLang, canInvite, accessExpiresAt, deleteAt, expiryAction, deleteAfterDays, presetID,
	)
	if err != nil {
		return fmt.Errorf("impossible d'insÃƒÂ©rer l'utilisateur %q: %w", form.Username, err)
	}

	// INCREMENT du compteur d'utilisation
	result, err := tx.Exec(
		`UPDATE invitations SET used_count = used_count + 1 WHERE id = ?`,
		inv.ID,
	)
	if err != nil {
		return fmt.Errorf("impossible d'incrÃƒÂ©menter le compteur de l'invitation %d: %w", inv.ID, err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("invitation %d non trouvÃƒÂ©e lors de l'incrÃƒÂ©mentation", inv.ID)
	}

	// Commit de la transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("impossible de valider la transaction: %w", err)
	}

	slog.Info("Utilisateur enregistrÃƒÂ© dans SQLite",
		"username", form.Username,
		"jellyfin_id", jellyfinID,
		"ldap_dn", ldapDN,
		"invitation_id", inv.ID,
	)

	return nil
}

func (h *InvitationHandler) resolveInviteProvisionPlan(profile jellyfin.InviteProfile) (inviteProvisionPlan, error) {
	plan := inviteProvisionPlan{EffectiveProfile: profile}

	mappings, err := h.db.GetGroupPolicyMappings()
	if err != nil {
		return plan, err
	}

	groupName := strings.TrimSpace(profile.GroupName)
	presetID := strings.TrimSpace(strings.ToLower(profile.PresetID))

	if presetID == "" && groupName != "" {
		for i := range mappings {
			if strings.EqualFold(strings.TrimSpace(mappings[i].GroupName), groupName) {
				presetID = strings.TrimSpace(strings.ToLower(mappings[i].PolicyPresetID))
				if presetID != "" {
					break
				}
			}
		}
	}

	if presetID != "" {
		preset, err := h.getInvitePolicyPresetByID(presetID)
		if err != nil {
			return plan, err
		}
		plan.MappingPresetID = strings.TrimSpace(preset.ID)
		plan.EffectiveProfile = mergeInviteProfileWithPreset(profile, *preset)
	}

	plan.LDAPGroups = resolveLDAPGroupsFromMappings(mappings, presetID, groupName)
	return plan, nil
}

func (h *InvitationHandler) getInvitePolicyPresetByID(presetID string) (*config.JellyfinPolicyPreset, error) {
	presetID = strings.TrimSpace(strings.ToLower(presetID))
	if presetID == "" {
		return nil, fmt.Errorf("preset vide")
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

	return nil, fmt.Errorf("preset %q introuvable pour profil d'invitation", presetID)
}

func mergeInviteProfileWithPreset(base jellyfin.InviteProfile, preset config.JellyfinPolicyPreset) jellyfin.InviteProfile {
	merged := base
	merged.PresetID = strings.TrimSpace(strings.ToLower(preset.ID))
	merged.TemplateUserID = strings.TrimSpace(preset.TemplateUserID)
	merged.EnableAllFolders = preset.EnableAllFolders
	merged.EnabledFolderIDs = append([]string(nil), preset.EnabledFolderIDs...)
	merged.EnableDownload = preset.EnableDownload
	merged.EnableRemoteAccess = preset.EnableRemoteAccess
	merged.MaxSessions = preset.MaxSessions
	merged.BitrateLimit = preset.BitrateLimit
	merged.UsernameMinLength = preset.UsernameMinLength
	merged.UsernameMaxLength = preset.UsernameMaxLength
	merged.PasswordMinLength = preset.PasswordMinLength
	merged.PasswordMaxLength = preset.PasswordMaxLength
	merged.PasswordRequireUpper = preset.RequireUpper
	merged.PasswordRequireLower = preset.RequireLower
	merged.PasswordRequireDigit = preset.RequireDigit
	merged.PasswordRequireSpecial = preset.RequireSpecial
	merged.DisableAfterDays = preset.DisableAfterDays
	merged.UserExpiryDays = preset.DisableAfterDays
	merged.ExpiryAction = normalizeExpiryAction(preset.ExpiryAction)
	merged.DeleteAfterDays = preset.DeleteAfterDays
	merged.CanInvite = preset.CanInvite || merged.CanInvite
	return merged
}

func resolveLDAPProvisionGroups(ldapCfg config.LDAPConfig, mappedGroups []string) []string {
	groups := make([]string, 0, len(mappedGroups)+1)
	seen := map[string]struct{}{}

	appendUnique := func(groupRef string) {
		trimmed := strings.TrimSpace(groupRef)
		if trimmed == "" {
			return
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		groups = append(groups, trimmed)
	}

	baseGroup := strings.TrimSpace(ldapCfg.JellyfinGroup)
	if baseGroup == "" {
		baseGroup = strings.TrimSpace(ldapCfg.UserGroup)
	}
	if baseGroup == "" {
		baseGroup = "jellyfin"
	}

	appendUnique(baseGroup)
	for _, groupRef := range mappedGroups {
		appendUnique(groupRef)
	}

	return groups
}

func resolveLDAPGroupsFromMappings(mappings []config.GroupPolicyMapping, presetID, groupName string) []string {
	result := make([]string, 0, 2)
	seen := map[string]struct{}{}

	appendUnique := func(groupRef string) {
		trimmed := strings.TrimSpace(groupRef)
		if trimmed == "" {
			return
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}

	for i := range mappings {
		if strings.TrimSpace(strings.ToLower(mappings[i].Source)) != "ldap" {
			continue
		}

		mappingPresetID := strings.TrimSpace(strings.ToLower(mappings[i].PolicyPresetID))
		mappingGroupName := strings.TrimSpace(mappings[i].GroupName)

		if presetID != "" && mappingPresetID == presetID {
			appendUnique(mappings[i].LDAPGroupDN)
			continue
		}

		if groupName != "" && strings.EqualFold(mappingGroupName, groupName) {
			appendUnique(mappings[i].LDAPGroupDN)
		}
	}

	return result
}
