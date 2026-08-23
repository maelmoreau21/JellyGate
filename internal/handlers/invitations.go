// Package handlers — invitations.go
//
// Gère le système d'invitations de JellyGate.
// La route GET/POST /invite/{code} génère un jeton Stage Invitation Authentik
// et redirige l'utilisateur vers le flux d'inscription (Enrollment Flow) Authentik.
package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	netmail "net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/maelmoreau21/JellyGate/internal/authentik"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/integrations"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	"github.com/maelmoreau21/JellyGate/internal/mail"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/notify"
	"github.com/maelmoreau21/JellyGate/internal/render"
)

// Ã¢â€�â‚¬Ã¢â€�â‚¬ Structures internes Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// invitation reprÃƒÂ©sente une ligne de la table invitations.
type invitation struct {
	ID                  int64
	Code                string
	Label               string
	MaxUses             int
	UsedCount           int
	JellyfinProfile     string // JSON brut du profil
	ProfileID           string
	ProfileSnapshot     string
	IsTemporary         bool
	AccountDurationDays int
	PreferredLang       string
	ExpiresAt           sql.NullTime
	CreatedBy           string
	CreatedAt           time.Time
}

// inviteFormData contient les donnÃƒÂ©es soumises par le formulaire d'inscription.
type inviteFormData struct {
	Username string
	Email    string
	Password string
}

type inviteSignupResult struct {
	JellyfinID string
}

type inviteSignupError struct {
	err                error
	releaseReservation bool
}

func (e *inviteSignupError) Error() string {
	return e.err.Error()
}

func (e *inviteSignupError) Unwrap() error {
	return e.err
}

func inviteSignupFailure(err error, releaseReservation bool) error {
	return &inviteSignupError{err: err, releaseReservation: releaseReservation}
}

func shouldReleaseInvitationReservation(err error) bool {
	var signupErr *inviteSignupError
	return errors.As(err, &signupErr) && signupErr.releaseReservation
}

type inviteProvisionPlan struct {
	EffectiveProfile jellyfin.InviteProfile
	MappingPresetID  string
}

// Ã¢â€â‚¬Ã¢â€â‚¬ Invitation Handler Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬Ã¢â€â‚¬

// InvitationHandler gÃƒÂ¨re les routes liÃƒÂ©es aux invitations.
type InvitationHandler struct {
	cfg         *config.Config
	db          *database.DB
	authClient  authentik.Client
	provisioner *integrations.Client
	mailer      *mail.Mailer
	notifier    *notify.Notifier
	renderer    *render.Engine
	abuse       *inviteAbuseTracker
}

// NewInvitationHandler crée un nouveau handler d'invitations.
func NewInvitationHandler(cfg *config.Config, db *database.DB, provisioner *integrations.Client, m *mail.Mailer, n *notify.Notifier, renderer *render.Engine) *InvitationHandler {
	return &InvitationHandler{
		cfg:         cfg,
		db:          db,
		provisioner: provisioner,
		mailer:      m,
		notifier:    n,
		renderer:    renderer,
		abuse:       newInviteAbuseTracker(),
	}
}

// SetAuthentikClient définit le client Authentik.
func (h *InvitationHandler) SetAuthentikClient(auth authentik.Client) { h.authClient = auth }

func (h *InvitationHandler) getEffectiveAuthentikClient() authentik.Client {
	if h.authClient != nil {
		return h.authClient
	}
	if h.db != nil {
		if dbCfg, err := h.db.GetAuthentikConfig(); err == nil && (dbCfg.URL != "" || dbCfg.IssuerURL != "") {
			return authentik.NewClient(dbCfg)
		}
	}
	if h.cfg != nil && (h.cfg.Authentik.URL != "" || h.cfg.Authentik.IssuerURL != "") {
		return authentik.NewClient(h.cfg.Authentik)
	}
	return nil
}

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

// InvitePage affiche le formulaire d'inscription pour un code d'invitation donné.
func (h *InvitationHandler) InvitePage(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	// Vérifier que l'invitation existe et est valide
	inv, err := h.getValidInvitation(code)
	if err != nil {
		slog.Warn("Invitation invalide consultée", "code_fingerprint", tokenLogFingerprint(code), "error", err)
		http.Error(w, h.tr(r, "invite_error_invalid_or_expired", "Invitation invalide ou expirée"), http.StatusNotFound)
		return
	}

	var td *render.TemplateData
	if h.renderer != nil {
		td = applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	} else {
		td = &render.TemplateData{Data: make(map[string]interface{})}
	}
	td.Section = "login"
	td.Invitation = inv
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.Data["JellyTrackURL"] = links.JellyTrackURL
	td.Data["JellyfinServerName"] = links.JellyfinServerName
	productCfg, _ := h.db.GetProductFeaturesConfig()
	td.Data["InviteIntroHTML"] = renderProductMarkdownHTML(productCfg.Content.InviteIntroMarkdown)
	if productCfg.AntiAbuse.Enabled && productCfg.AntiAbuse.Captcha {
		question, token := h.newInviteCaptchaChallenge()
		td.Data["CaptchaEnabled"] = true
		td.Data["CaptchaQuestion"] = question
		td.Data["CaptchaToken"] = token
	}

	var profile jellyfin.InviteProfile
	if strings.TrimSpace(inv.JellyfinProfile) != "" {
		_ = json.Unmarshal([]byte(inv.JellyfinProfile), &profile)
	}
	minLen, maxLen := resolveInviteUsernamePolicy(profile)
	td.Data["UsernameMinLength"] = minLen
	td.Data["UsernameMaxLength"] = maxLen
	td.Data["RequireEmail"] = profile.RequireEmail
	if strings.TrimSpace(profile.ForcedUsername) != "" {
		td.Data["SubmittedUsername"] = strings.TrimSpace(profile.ForcedUsername)
		td.Data["ForcedUsername"] = strings.TrimSpace(profile.ForcedUsername)
	}
	if strings.TrimSpace(profile.ForcedName) != "" {
		td.Data["SubmittedName"] = strings.TrimSpace(profile.ForcedName)
		td.Data["ForcedName"] = strings.TrimSpace(profile.ForcedName)
	}

	authCfg, _ := h.db.GetAuthentikConfig()
	authentikEnabled := (h.cfg != nil && h.cfg.Authentik.Enabled) || authCfg.Enabled
	effectiveAuth := h.getEffectiveAuthentikClient()
	if effectiveAuth != nil && authentikEnabled {
		rawAuthURL := authCfg.URL
		if rawAuthURL == "" && h.cfg != nil {
			rawAuthURL = h.cfg.Authentik.URL
		}
		if rawAuthURL == "" && authCfg.IssuerURL != "" {
			rawAuthURL = authCfg.IssuerURL
		}
		if rawAuthURL == "" && h.cfg != nil && h.cfg.Authentik.IssuerURL != "" {
			rawAuthURL = h.cfg.Authentik.IssuerURL
		}

		authURL := authentik.ResolveBaseURL(rawAuthURL)
		if authURL == "" && effectiveAuth.GetBaseURL() != "" {
			authURL = authentik.ResolveBaseURL(effectiveAuth.GetBaseURL())
		}
		if authURL == "" && h.cfg != nil && h.cfg.BaseURL != "" {
			authURL = authentik.ResolveBaseURL(h.cfg.BaseURL)
		}
		if authURL == "" {
			authURL = authentik.ResolveBaseURL(requestBaseURL(r))
		}

		flowSlug := strings.TrimSpace(authCfg.EnrollmentFlowSlug)
		if flowSlug == "" && h.cfg != nil {
			flowSlug = strings.TrimSpace(h.cfg.Authentik.EnrollmentFlowSlug)
		}
		if discovered := effectiveAuth.GetEnrollmentFlowSlug(r.Context(), flowSlug); discovered != "" {
			flowSlug = discovered
		}
		if flowSlug == "" {
			flowSlug = "default-enrollment-flow"
		}

		if authURL != "" {
			var stageToken sql.NullString
			_ = h.db.QueryRow(`SELECT authentik_invitation_id FROM invitations WHERE code = ? OR authentik_invitation_id = ?`, inv.Code, inv.Code).Scan(&stageToken)
			invToken := strings.TrimSpace(stageToken.String)

			if invToken == "" {
				// Créer à la volée le token Stage Authentik si inexistant avec l'ensemble des métadonnées
				var targetGroups []string
				jellyfinGroup := strings.TrimSpace(authCfg.JellyfinUserGroup)
				if jellyfinGroup == "" && h.cfg != nil {
					jellyfinGroup = strings.TrimSpace(h.cfg.Authentik.JellyfinUserGroup)
				}
				if jellyfinGroup == "" {
					jellyfinGroup = "jellyfin-users"
				}
				targetGroups = append(targetGroups, jellyfinGroup)

				fixedData := map[string]interface{}{
					"source":                "JellyGate",
					"created_by":            "JellyGate",
					"created_by_app":        "JellyGate",
					"invitation_code":       inv.Code,
					"code":                  inv.Code,
					"sponsor":               inv.CreatedBy,
					"groups":                targetGroups,
					"preset_id":             profile.PresetID,
					"is_temporary":          profile.IsTemporary,
					"account_duration_days": profile.AccountDurationDays,
				}
				if strings.TrimSpace(profile.ForcedUsername) != "" {
					fixedData["username"] = strings.TrimSpace(profile.ForcedUsername)
				}
				if strings.TrimSpace(profile.ForcedName) != "" {
					fixedData["name"] = strings.TrimSpace(profile.ForcedName)
				}
				var stageExpiry time.Time
				if inv.ExpiresAt.Valid {
					stageExpiry = inv.ExpiresAt.Time
				}
				tokenName := fmt.Sprintf("jellygate-%s", inv.Code)
				if tokenID, authErr := effectiveAuth.CreateInvitationStageToken(r.Context(), tokenName, stageExpiry, fixedData, inv.MaxUses == 1, flowSlug); authErr == nil && strings.TrimSpace(tokenID) != "" {
					invToken = strings.TrimSpace(tokenID)
					_, _ = h.db.Exec(`UPDATE invitations SET authentik_invitation_id = ? WHERE id = ?`, invToken, inv.ID)
					slog.Info("Token Authentik régénéré à la volée pour l'invitation", "code", inv.Code, "token_id", invToken)
				} else {
					slog.Warn("Échec régénération token Authentik pour l'invitation (fallback formulaire JellyGate)", "code", inv.Code, "error", authErr)
				}
			}

			// Ne rediriger et ne proposer le bouton SSO Authentik QUE si un véritable jeton Authentik valide est présent
			if invToken != "" {
				authentikEnrollmentURL := fmt.Sprintf("%s/if/flow/%s/?itoken=%s", authURL, flowSlug, url.QueryEscape(invToken))
				td.Data["AuthentikEnrollmentURL"] = authentikEnrollmentURL

				// Redirection directe vers le flux d'inscription Authentik (sauf mode prévisualisation explicite)
				if r.URL.Query().Get("preview") != "1" {
					_ = h.db.LogAction("invite.redirect_authentik", inv.CreatedBy, inv.Code, fmt.Sprintf("redirected to authentik flow %s from IP %s", flowSlug, r.RemoteAddr))
					http.Redirect(w, r, authentikEnrollmentURL, http.StatusTemporaryRedirect)
					return
				}
			}
		}
	}

	if h.renderer != nil {
		if err := h.renderer.Render(w, "invite.html", td); err != nil {
			slog.Error("Erreur rendu invitation page", "error", err)
			http.Error(w, h.tr(r, "common_server_error", "Erreur serveur"), http.StatusInternalServerError)
		}
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "<html><body><a id=\"authentik-enroll-btn\" href=\"%s\">Authentik</a> %s</body></html>", td.Data["AuthentikEnrollmentURL"], td.Data["JellyfinServerName"])
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ POST /invite/{code} Ã¢â‚¬â€� FLUX ATOMIQUE Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// InviteSubmit traite la soumission du formulaire d'inscription.
//
// Flux d'inscription Authentik :
//
//	Étape 1 : Validation de l'invitation et des quotas en base de données.
//	Étape 2 : Génération / récupération du jeton d'invitation Stage Authentik.
//	Étape 3 : Redirection vers le flux d'Enrollment Authentik.
func (h *InvitationHandler) InviteSubmit(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	remoteAddr := r.RemoteAddr

	slog.Info("⚡ Début du flux d'inscription",
		"code", code,
		"remote", remoteAddr,
	)

	// Parsing du formulaire
	if err := r.ParseForm(); err != nil {
		slog.Error("Erreur parsing formulaire inscription", "error", err)
		http.Error(w, h.tr(r, "common_bad_request", "Requête invalide"), http.StatusBadRequest)
		return
	}

	submittedUsername := strings.TrimSpace(r.FormValue("username"))
	submittedEmail := strings.TrimSpace(r.FormValue("email"))

	antiAbuseCfg := h.inviteAntiAbuseConfig()
	if blocked, retryAfter := h.isInviteBlocked(r, antiAbuseCfg); blocked {
		h.logInviteAction(r, "invite.anti_abuse.blocked", submittedUsername, code, fmt.Sprintf("retry_after=%s", retryAfter.Round(time.Second)))
		logSecurityEvent(h.db, r, "invite_abuse", "invite.ip.blocked", "critical", submittedUsername, tokenLogFingerprint(code), "IP bloquee par la protection invitation", map[string]string{"retry_after": retryAfter.Round(time.Second).String()})
		http.Error(w, h.tr(r, "invite_error_too_many_attempts", "Trop de tentatives. Reessayez plus tard."), http.StatusTooManyRequests)
		return
	}
	if err := h.verifyInviteCaptcha(r, antiAbuseCfg); err != nil {
		h.recordInviteFailure(r, antiAbuseCfg)
		h.logInviteAction(r, "invite.captcha.failed", submittedUsername, code, err.Error())
		logSecurityEvent(h.db, r, "captcha", "invite.captcha.failed", "warning", submittedUsername, tokenLogFingerprint(code), "Echec CAPTCHA invitation", map[string]string{"error": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// ÉTAPE 1 : Validation invitation
	slog.Info("📋 Validation de l'invitation", "code", code)

	inv, err := h.getValidInvitation(code)
	if err != nil {
		slog.Warn("Invitation invalide", "code_fingerprint", tokenLogFingerprint(code), "error", err)
		targetUsername := strings.TrimSpace(submittedUsername)
		if targetUsername == "" {
			targetUsername = "unknown"
		}
		h.recordInviteFailure(r, antiAbuseCfg)
		h.logInviteAction(r, "invite.validation.failed", targetUsername, code, err.Error())
		logSecurityEvent(h.db, r, "invalid_invite", "invite.invalid", "warning", targetUsername, tokenLogFingerprint(code), "Invitation invalide ou expiree", map[string]string{"error": err.Error()})
		http.Error(w, h.tr(r, "invite_error_invalid_or_expired", "Invitation invalide ou expirée"), http.StatusForbidden)
		return
	}

	var profile jellyfin.InviteProfile
	if strings.TrimSpace(inv.JellyfinProfile) != "" {
		_ = json.Unmarshal([]byte(inv.JellyfinProfile), &profile)
	}

	var form *inviteFormData
	if strings.TrimSpace(r.FormValue("password")) != "" {
		form, err = h.validateForm(r, &profile)
	} else {
		form, err = h.validatePendingInviteForm(r, &profile)
	}
	if err != nil {
		h.recordInviteFailure(r, antiAbuseCfg)
		td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
		td.Section = "login"
		td.Invitation = inv
		td.Error = err.Error()
		links := resolvePortalLinks(h.cfg, h.db)
		td.Data["JellyfinURL"] = links.JellyfinURL
		td.Data["JellyseerrURL"] = links.JellyseerrURL
		td.Data["JellyTrackURL"] = links.JellyTrackURL
		td.Data["JellyfinServerName"] = links.JellyfinServerName
		td.Data["SubmittedUsername"] = submittedUsername
		td.Data["SubmittedEmail"] = submittedEmail
		minLen, maxLen := resolveInviteUsernamePolicy(profile)
		td.Data["UsernameMinLength"] = minLen
		td.Data["UsernameMaxLength"] = maxLen
		td.Data["RequireEmail"] = profile.RequireEmail
		_ = h.renderer.Render(w, "invite.html", td)
		return
	}

	// ÉTAPE 2 : Réservation de l'utilisation de l'invitation
	if err := h.reserveInvitationUse(inv); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// ÉTAPE 3 : Provisioning automatique
	authCfg, _ := h.db.GetAuthentikConfig()
	authentikEnabled := (h.cfg != nil && h.cfg.Authentik.Enabled) || authCfg.Enabled

	var recoveryURL string
	var authentikID string

	if h.authClient != nil && authentikEnabled {
		var targetGroups []string
		if profile.GroupName != "" {
			targetGroups = append(targetGroups, profile.GroupName)
		} else {
			userGroup := strings.TrimSpace(authCfg.JellyfinUserGroup)
			if userGroup == "" && h.cfg != nil {
				userGroup = strings.TrimSpace(h.cfg.Authentik.JellyfinUserGroup)
			}
			if userGroup == "" {
				userGroup = "jellyfin-users"
			}
			targetGroups = append(targetGroups, userGroup)
		}

		if profile.CanInvite {
			invGroup := strings.TrimSpace(authCfg.InvitersGroup)
			if invGroup == "" {
				invGroup = "jellygate-inviters"
			}
			targetGroups = append(targetGroups, invGroup)
		}

		authName := form.Username
		if strings.TrimSpace(profile.ForcedName) != "" {
			authName = strings.TrimSpace(profile.ForcedName)
		}

		authResp, authErr := h.authClient.CreateUser(r.Context(), authentik.UserCreatePayload{
			Username: form.Username,
			Name:     authName,
			Email:    form.Email,
			IsActive: true,
			Groups:   targetGroups,
		})
		if authErr != nil || authResp == nil {
			h.releaseInvitationUse(inv)
			slog.Error("Échec création utilisateur Authentik via invitation", "username", form.Username, "error", authErr)
			td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
			td.Section = "login"
			td.Invitation = inv
			td.Error = h.tr(r, "invite_error_authentik_create", "Erreur lors de la création du compte dans Authentik") + ": " + fmt.Sprintf("%v", authErr)
			links := resolvePortalLinks(h.cfg, h.db)
			td.Data["JellyfinURL"] = links.JellyfinURL
			td.Data["JellyseerrURL"] = links.JellyseerrURL
			td.Data["JellyTrackURL"] = links.JellyTrackURL
			td.Data["JellyfinServerName"] = links.JellyfinServerName
			td.Data["SubmittedUsername"] = form.Username
			td.Data["SubmittedEmail"] = form.Email
			minLen, maxLen := resolveInviteUsernamePolicy(profile)
			td.Data["UsernameMinLength"] = minLen
			td.Data["UsernameMaxLength"] = maxLen
			td.Data["RequireEmail"] = profile.RequireEmail
			_ = h.renderer.Render(w, "invite.html", td)
			return
		}

		if authResp.ID != "" {
			authentikID = authResp.ID
		} else if authResp.PK > 0 {
			authentikID = fmt.Sprintf("%d", authResp.PK)
		}

		if authResp.PK > 0 {
			if link, errLink := h.authClient.CreateRecoveryLink(r.Context(), authResp.PK); errLink == nil && strings.TrimSpace(link) != "" {
				recoveryURL = strings.TrimSpace(link)
			}
		}
	}

	provisionPlan := inviteProvisionPlan{EffectiveProfile: profile}
	if resolvedPlan, err := h.resolveInviteProvisionPlan(profile); err == nil {
		provisionPlan = resolvedPlan
	}

	if err := h.registerUser(r.Context(), form, inv, provisionPlan.EffectiveProfile, "", authentikID, true); err != nil {
		h.releaseInvitationUse(inv)
		slog.Error("Échec enregistrement utilisateur JellyGate", "username", form.Username, "error", err)
		h.logInviteAction(r, "invite.sqlite.failed", form.Username, inv.Code, err.Error())
		http.Error(w, h.tr(r, "invite_error_persist", "Erreur lors de l'enregistrement du compte"), http.StatusInternalServerError)
		return
	}

	h.recordInviteSuccess(r)
	h.logInviteAction(r, "invite.signup.completed", form.Username, inv.Code, fmt.Sprintf("authentik_id=%s; recovery=%t", authentikID, recoveryURL != ""))

	// Envoi de l'email de bienvenue uniquement après la création effective du compte
	h.sendInviteWelcomeEmail(r, form.Username, form.Email, provisionPlan.EffectiveProfile)

	if recoveryURL != "" {
		http.Redirect(w, r, recoveryURL, http.StatusSeeOther)
		return
	}

	successMsg := fmt.Sprintf(h.tr(r, "invite_success_created", "Bienvenue %s ! Votre compte a été créé avec succès."), form.Username)
	h.renderInviteSuccessPage(
		w,
		r,
		inv,
		successMsg,
		true,
	)
}

func (h *InvitationHandler) sendInviteWelcomeEmail(r *http.Request, username, email string, profile jellyfin.InviteProfile) {
	if h.mailer == nil || strings.TrimSpace(email) == "" {
		return
	}
	preferredLang := jgmw.LangFromContext(r.Context())
	emailCfg, usedLang, err := loadEmailTemplatesForLanguage(h.db, preferredLang, emailLanguageContext{
		PreferredLang: preferredLang,
		GroupName:     profile.GroupName,
	})
	if err != nil || emailCfg.DisableWelcomeEmail {
		return
	}
	defaults := config.DefaultEmailTemplatesForLanguage(usedLang)
	subject := firstNonEmpty(emailCfg.WelcomeSubject, defaults.WelcomeSubject)
	body := emailCfg.Welcome
	if strings.TrimSpace(body) == "" {
		body = defaults.Welcome
	}
	links := resolvePortalLinks(h.cfg, h.db)
	helpURL := firstNonEmpty(links.JellyGateURL, h.cfg.BaseURL)
	extra := map[string]string{
		"Username":           username,
		"Email":              email,
		"JellyfinURL":        links.JellyfinURL,
		"JellyfinServerName": links.JellyfinServerName,
		"JellyseerrURL":      links.JellyseerrURL,
		"JellyTrackURL":      links.JellyTrackURL,
		"JellyGateURL":       helpURL,
		"HelpURL":            helpURL,
	}
	if err := sendTemplateIfConfigured(h.mailer, email, subject, usedLang, "welcome", body, emailCfg, extra); err != nil {
		slog.Warn("Échec envoi email de bienvenue post-inscription", "username", username, "email", email, "error", err)
	} else {
		slog.Info("Email de bienvenue envoyé après création de compte", "username", username, "email", email)
	}
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
	td.Data["JellyfinServerName"] = links.JellyfinServerName
	productCfg, _ := h.db.GetProductFeaturesConfig()
	td.Data["InviteSuccessHTML"] = renderProductMarkdownHTML(productCfg.Content.InviteSuccessMarkdown)

	if err := h.renderer.Render(w, "invite.html", td); err != nil {
		slog.Error("Erreur rendu invite success page", "error", err)
		http.Error(w, h.tr(r, "common_server_error", "Erreur serveur"), http.StatusInternalServerError)
	}
}

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

func (h *InvitationHandler) validatePendingInviteForm(r *http.Request, profile *jellyfin.InviteProfile) (*inviteFormData, error) {
	username := strings.TrimSpace(r.FormValue("username"))
	email := strings.TrimSpace(r.FormValue("email"))

	if err := h.validateInviteUsername(r, username, profile); err != nil {
		return nil, err
	}
	if email == "" {
		return nil, fmt.Errorf("l'adresse email est obligatoire")
	}
	if _, err := netmail.ParseAddress(email); err != nil {
		return nil, fmt.Errorf("adresse email invalide")
	}

	return &inviteFormData{
		Username: username,
		Email:    email,
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

const (
	ProvisionRoleUser    = "user"
	ProvisionRoleInviter = "inviter"
	ProvisionRoleAdmin   = "admin"
)

func resolveProvisionRole(profile jellyfin.InviteProfile) string {
	if profile.CanInvite {
		return ProvisionRoleInviter
	}

	groupName := strings.ToLower(strings.TrimSpace(profile.GroupName))
	switch groupName {
	case "admin", "admins", "administrator", "administrators":
		return ProvisionRoleAdmin
	case "inviter", "inviters", "parrainage", "sponsor", "sponsors":
		return ProvisionRoleInviter
	default:
		return ProvisionRoleUser
	}
}

func roleAllowsInvites(role string) bool {
	normalized := strings.ToLower(strings.TrimSpace(role))
	return normalized == ProvisionRoleInviter || normalized == ProvisionRoleAdmin
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

	if forced := strings.TrimSpace(usernamePolicy.ForcedUsername); forced != "" {
		if !strings.EqualFold(strings.TrimSpace(username), forced) {
			return fmt.Errorf("le nom d'utilisateur est verrouillé à « %s » pour cette invitation", forced)
		}
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
		`SELECT id, code, label, max_uses, used_count, jellyfin_profile, profile_id, profile_snapshot, is_temporary, account_duration_days, preferred_lang, expires_at, created_by, created_at
		 FROM invitations WHERE code = ? OR authentik_invitation_id = ?`, code, code)

	var inv invitation
	var jellyfinProfile, profileID, profileSnapshot sql.NullString
	var label sql.NullString
	var createdBy, preferredLang sql.NullString

	err := row.Scan(
		&inv.ID, &inv.Code, &label, &inv.MaxUses, &inv.UsedCount,
		&jellyfinProfile, &profileID, &profileSnapshot, &inv.IsTemporary, &inv.AccountDurationDays,
		&preferredLang, &inv.ExpiresAt, &createdBy, &inv.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invitation introuvable")
	}
	if err != nil {
		return nil, fmt.Errorf("erreur de lecture de l'invitation: %w", err)
	}

	// Reconstituer les champs nullable
	inv.Label = label.String
	inv.JellyfinProfile = jellyfinProfile.String
	inv.ProfileID = profileID.String
	inv.ProfileSnapshot = profileSnapshot.String
	inv.PreferredLang = strings.TrimSpace(preferredLang.String)
	inv.CreatedBy = createdBy.String

	// VÃƒÂ©rifier l'expiration
	if inv.ExpiresAt.Valid && time.Now().After(inv.ExpiresAt.Time) {
		return nil, fmt.Errorf("invitation expiree depuis %s", inv.ExpiresAt.Time.Format("02/01/2006 15:04"))
	}

	// VÃƒÂ©rifier le quota d'utilisation (0 = illimitÃƒÂ©)
	if inv.MaxUses > 0 && inv.UsedCount >= inv.MaxUses {
		return nil, fmt.Errorf("invitation a atteint sa limite d'utilisation (%d/%d)", inv.UsedCount, inv.MaxUses)
	}

	return &inv, nil
}

func (h *InvitationHandler) reserveInvitationUse(inv *invitation) error {
	if inv == nil {
		return fmt.Errorf("invitation indisponible")
	}
	result, err := h.db.Exec(
		`UPDATE invitations
		 SET used_count = used_count + 1
		 WHERE id = ? AND (max_uses <= 0 OR used_count < max_uses)`,
		inv.ID,
	)
	if err != nil {
		return fmt.Errorf("impossible de reserver l'invitation %d: %w", inv.ID, err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("invitation a atteint sa limite d'utilisation")
	}
	inv.UsedCount++
	return nil
}

func (h *InvitationHandler) releaseInvitationUse(inv *invitation) {
	if inv == nil {
		return
	}
	_, err := h.db.Exec(
		`UPDATE invitations
		 SET used_count = CASE WHEN used_count > 0 THEN used_count - 1 ELSE 0 END
		 WHERE id = ?`,
		inv.ID,
	)
	if err != nil {
		slog.Warn("Impossible de liberer une reservation d'invitation", "invitation_id", inv.ID, "error", err)
	}
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
	if h.authClient != nil {
		cfg, _ := h.db.GetAuthentikConfig()
		if cfg.Enabled {
			expiresAt := time.Now().Add(24 * time.Hour)
			if inv.ExpiresAt.Valid {
				expiresAt = inv.ExpiresAt.Time
			}
			fixedData := map[string]interface{}{
				"email": form.Email,
			}
			if _, err := h.authClient.CreateInvitationStageToken(r.Context(), form.Username, expiresAt, fixedData, true, cfg.EnrollmentFlowSlug); err != nil {
				slog.Error("Erreur création token invitation Authentik", "error", err)
			}
		}
	}

	provisionPlan := inviteProvisionPlan{EffectiveProfile: profile}
	if resolvedPlan, err := h.resolveInviteProvisionPlan(profile); err == nil {
		provisionPlan = resolvedPlan
	}

	slog.Info("Enregistrement utilisateur JellyGate (Identité via Authentik)", "username", form.Username)
	if err := h.registerUser(r.Context(), form, inv, provisionPlan.EffectiveProfile, "", "", emailVerified); err != nil {
		slog.Error("Échec enregistrement utilisateur JellyGate", "username", form.Username, "error", err)
		h.logInviteAction(r, "invite.sqlite.failed", form.Username, inv.Code, err.Error())
		return nil, inviteSignupFailure(fmt.Errorf("%s", h.tr(r, "invite_error_persist", "Erreur lors de l'enregistrement du compte")), true)
	}

	return &inviteSignupResult{
		JellyfinID: "",
	}, nil
}

// registerUser insère l'utilisateur dans SQLite et incrémente le compteur
// d'utilisation de l'invitation. Les deux opérations sont dans une transaction.
func (h *InvitationHandler) registerUser(ctx context.Context, form *inviteFormData, inv *invitation, profile jellyfin.InviteProfile, jellyfinID string, authentikID string, emailVerified bool) error {
	tx, err := h.db.Begin()
	if err != nil {
		return fmt.Errorf("impossible de démarrer la transaction: %w", err)
	}
	defer tx.Rollback() // No-op si Commit() a été appelé

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

	canInvite := canInviteFromProfile
	preferredLang := normalizeSupportedEmailLang(inv.PreferredLang)
	profileApplyStatus := "pending"
	var profileAppliedAt interface{}
	if strings.TrimSpace(jellyfinID) != "" {
		profileApplyStatus = "applied"
		profileAppliedAt = time.Now()
	}

	// INSERT de l'utilisateur
	_, err = tx.Exec(
		`INSERT INTO users (jellyfin_id, username, email, email_verified, group_name, invited_by, preferred_lang, is_active, is_banned, can_invite, access_expires_at, delete_at, expiry_action, expiry_delete_after_days, expired_at, preset_id, profile_apply_status, profile_apply_error, profile_applied_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, TRUE, FALSE, ?, ?, ?, ?, ?, NULL, ?, ?, '', ?)`,
		jellyfinIDValue, form.Username, form.Email, emailVerified, groupName, inv.Code, preferredLang, canInvite, accessExpiresAt, deleteAt, expiryAction, deleteAfterDays, presetID, profileApplyStatus, profileAppliedAt,
	)
	if err != nil {
		return fmt.Errorf("impossible d'insérer l'utilisateur %q: %w", form.Username, err)
	}

	// Commit de la transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("impossible de valider la transaction: %w", err)
	}

	slog.Info("Utilisateur enregistré dans SQLite",
		"username", form.Username,
		"jellyfin_id", jellyfinID,
		"invitation_id", inv.ID,
	)

	// Post-registration: link Authentik identity and referral tree
	var newUserID int64
	_ = h.db.QueryRow(`SELECT id FROM users WHERE username = ?`, form.Username).Scan(&newUserID)

	var sponsorUserID int64
	_ = h.db.QueryRow(`SELECT id FROM users WHERE username = ?`, inv.CreatedBy).Scan(&sponsorUserID)

	if authentikID == "" && h.authClient != nil {
		authCfg, _ := h.db.GetAuthentikConfig()
		if (h.cfg != nil && h.cfg.Authentik.Enabled) || authCfg.Enabled {
			userGroup := authCfg.JellyfinUserGroup
			if userGroup == "" && h.cfg != nil {
				userGroup = h.cfg.Authentik.JellyfinUserGroup
			}
			if userGroup == "" {
				userGroup = "jellyfin-users"
			}
			authResp, authErr := h.authClient.CreateUser(ctx, authentik.UserCreatePayload{
				Username: form.Username,
				Name:     form.Username,
				Email:    form.Email,
				IsActive: true,
				Groups:   []string{userGroup},
			})
			if authErr == nil && authResp != nil {
				if authResp.ID != "" {
					authentikID = authResp.ID
				} else if authResp.PK > 0 {
					authentikID = fmt.Sprintf("%d", authResp.PK)
				}
			}
		}
	}
	if authentikID != "" {
		if sponsorUserID > 0 {
			_, _ = h.db.Exec(`UPDATE users SET authentik_id = ?, invited_by_id = ? WHERE id = ?`, authentikID, sponsorUserID, newUserID)
		} else {
			_, _ = h.db.Exec(`UPDATE users SET authentik_id = ? WHERE id = ?`, authentikID, newUserID)
		}
	} else if sponsorUserID > 0 {
		_, _ = h.db.Exec(`UPDATE users SET invited_by_id = ? WHERE id = ?`, sponsorUserID, newUserID)
	}

	// Link referral record
	var referralID int64
	errRef := h.db.QueryRow(`SELECT id FROM referrals WHERE invitation_id = ? AND status = 'pending' LIMIT 1`, inv.ID).Scan(&referralID)
	if errRef == nil && referralID > 0 {
		_ = h.db.UpdateReferralStatus(ctx, referralID, "accepted", &newUserID, authentikID)
	} else if sponsorUserID > 0 {
		ref, errCreate := h.db.CreateReferral(ctx, sponsorUserID, inv.ID, form.Email)
		if errCreate == nil && ref != nil {
			_ = h.db.UpdateReferralStatus(ctx, ref.ID, "accepted", &newUserID, authentikID)
		}
	}

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
	profile := jellyfin.InviteProfileFromPolicyPreset(&preset)
	merged.PresetID = profile.PresetID
	merged.IsAdministrator = profile.IsAdministrator
	merged.IsHidden = profile.IsHidden
	merged.IsDisabled = profile.IsDisabled
	merged.EnableAllFolders = profile.EnableAllFolders
	merged.EnabledFolderIDs = profile.EnabledFolderIDs
	merged.BlockedMediaFolders = profile.BlockedMediaFolders
	merged.EnableAllDevices = profile.EnableAllDevices
	merged.EnabledDevices = profile.EnabledDevices
	merged.EnableAllChannels = profile.EnableAllChannels
	merged.EnabledChannels = profile.EnabledChannels
	merged.BlockedChannels = profile.BlockedChannels
	merged.EnableDownload = profile.EnableDownload
	merged.EnableMediaPlayback = profile.EnableMediaPlayback
	merged.EnableAudioPlaybackTranscoding = profile.EnableAudioPlaybackTranscoding
	merged.EnableVideoPlaybackTranscoding = profile.EnableVideoPlaybackTranscoding
	merged.EnablePlaybackRemuxing = profile.EnablePlaybackRemuxing
	merged.EnableRemoteAccess = profile.EnableRemoteAccess
	merged.EnableLiveTvAccess = profile.EnableLiveTvAccess
	merged.EnableLiveTvManagement = profile.EnableLiveTvManagement
	merged.EnableSharedDeviceControl = profile.EnableSharedDeviceControl
	merged.EnableContentDeletion = profile.EnableContentDeletion
	merged.EnableContentDeletionFromFolders = profile.EnableContentDeletionFromFolders
	merged.EnablePublicSharing = profile.EnablePublicSharing
	merged.EnableSyncTranscoding = profile.EnableSyncTranscoding
	merged.EnableMediaConversion = profile.EnableMediaConversion
	merged.ForceRemoteSourceTranscoding = profile.ForceRemoteSourceTranscoding
	merged.SyncPlayAccess = profile.SyncPlayAccess
	merged.InvalidLoginAttemptCount = profile.InvalidLoginAttemptCount
	merged.LoginAttemptsBeforeLockout = profile.LoginAttemptsBeforeLockout
	merged.MaxSessions = profile.MaxSessions
	merged.BitrateLimit = profile.BitrateLimit
	merged.UserConfiguration = profile.UserConfiguration
	merged.DisplayPreferences = profile.DisplayPreferences
	merged.UsernameMinLength = profile.UsernameMinLength
	merged.UsernameMaxLength = profile.UsernameMaxLength
	merged.PasswordMinLength = profile.PasswordMinLength
	merged.PasswordMaxLength = profile.PasswordMaxLength
	merged.PasswordRequireUpper = profile.PasswordRequireUpper
	merged.PasswordRequireLower = profile.PasswordRequireLower
	merged.PasswordRequireDigit = profile.PasswordRequireDigit
	merged.PasswordRequireSpecial = profile.PasswordRequireSpecial
	merged.DisableAfterDays = profile.DisableAfterDays
	merged.UserExpiryDays = profile.UserExpiryDays
	merged.ExpiryAction = profile.ExpiryAction
	merged.DeleteAfterDays = profile.DeleteAfterDays
	merged.AllowedTags = profile.AllowedTags
	merged.BlockedTags = profile.BlockedTags
	merged.MaxParentalRating = profile.MaxParentalRating
	merged.BlockUnratedItems = profile.BlockUnratedItems
	merged.AccessSchedules = profile.AccessSchedules
	merged.CanInvite = profile.CanInvite || merged.CanInvite
	merged.IsTemporary = profile.IsTemporary
	merged.AccountDurationDays = profile.AccountDurationDays
	return merged
}
