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
	"context"
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
	JellyfinID     string
	UserDN         string
	LDAPMirrorMode bool
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
	LDAPGroups       []string
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ Invitation Handler Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// InvitationHandler gère les routes liées aux invitations.
type InvitationHandler struct {
	cfg         *config.Config
	db          *database.DB
	jfClient    *jellyfin.Client
	authClient  authentik.Client
	provisioner *integrations.Client
	mailer      *mail.Mailer
	notifier    *notify.Notifier
	renderer    *render.Engine
	abuse       *inviteAbuseTracker
}

// NewInvitationHandler crée un nouveau handler d'invitations.
func NewInvitationHandler(cfg *config.Config, db *database.DB, jf *jellyfin.Client, provisioner *integrations.Client, m *mail.Mailer, n *notify.Notifier, renderer *render.Engine) *InvitationHandler {
	return &InvitationHandler{
		cfg:         cfg,
		db:          db,
		jfClient:    jf,
		provisioner: provisioner,
		mailer:      m,
		notifier:    n,
		renderer:    renderer,
		abuse:       newInviteAbuseTracker(),
	}
}

// SetAuthentikClient définit le client Authentik.
func (h *InvitationHandler) SetAuthentikClient(auth authentik.Client) { h.authClient = auth }

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
		slog.Warn("Invitation invalide consultÃƒÂ©e", "code_fingerprint", tokenLogFingerprint(code), "error", err)
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
	productCfg, _ := h.db.GetProductFeaturesConfig()
	td.Data["InviteIntroHTML"] = renderProductMarkdownHTML(productCfg.Content.InviteIntroMarkdown)
	if productCfg.AntiAbuse.Enabled && productCfg.AntiAbuse.Captcha {
		question, token := h.newInviteCaptchaChallenge()
		td.Data["CaptchaEnabled"] = true
		td.Data["CaptchaQuestion"] = question
		td.Data["CaptchaToken"] = token
	}
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
	td.Data["RequireEmailVerification"] = profile.RequireEmailVerification

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

	// Ã¢â€�â‚¬Ã¢â€�â‚¬ Parsing du formulaire Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â�	inv, err := h.getValidInvitation(code)
	if err != nil {
		slog.Warn("Invitation invalide", "code_fingerprint", tokenLogFingerprint(code), "error", err)
		targetUsername := strings.TrimSpace(submittedUsername)
		if targetUsername == "" {
			targetUsername = "unknown"
		}
		h.recordInviteFailure(r, antiAbuseCfg)
		h.logInviteAction(r, "invite.validation.failed", targetUsername, code, err.Error())
		logSecurityEvent(h.db, r, "invalid_invite", "invite.invalid", "warning", targetUsername, tokenLogFingerprint(code), "Invitation invalide ou expirée", map[string]string{"error": err.Error()})
		http.Error(w, h.tr(r, "invite_error_invalid_or_expired", "Invitation invalide ou expirée"), http.StatusForbidden)
		return
	}

	profile := jellyfin.InviteProfile{RequireEmail: true, RequireEmailVerification: true}
	if inv.JellyfinProfile != "" {
		_ = json.Unmarshal([]byte(inv.JellyfinProfile), &profile)
	}

	form, err := h.validateForm(r, &profile)
	if err != nil {
		h.recordInviteFailure(r, antiAbuseCfg)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.reserveInvitationUse(inv); err != nil {
		h.recordInviteFailure(r, antiAbuseCfg)
		http.Error(w, h.tr(r, "invite_error_invalid_or_expired", "Invitation invalide ou expirée"), http.StatusForbidden)
		return
	}

	_, err = h.completeInviteSignup(r, inv, form, profile, strings.TrimSpace(form.Email) != "")
	if err != nil {
		h.releaseInvitationUse(inv)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.recordInviteSuccess(r)
}eur dans SQLite et incrémente le compteurturn nil
}


				slog.Error("Ã¢Å¡Â Ã¯Â¸Â� ROLLBACK LDAP Ãƒâ€°CHOUÃƒâ€° Ã¢â‚¬â€� intervention manuelle requise", "dn", userDN, "rollback_error", rbErr)


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
			map[bool]string{true: "ldap_mirror", false: "local"}[ldapMirrorMode],
		))

	slog.Info("Ã°Å¸Å½â€° Inscription terminÃƒÂ©e avec succÃƒÂ¨s", "username", form.Username, "jellyfin_id", jellyfinID, "ldap_dn", userDN, "invitation_fingerprint", tokenLogFingerprint(inv.Code))

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
		JellyfinID:     jellyfinID,
		UserDN:         userDN,
		LDAPMirrorMode: ldapMirrorMode,
	}, nil
}

// registerUser insère l'utilisateur dans SQLite et incrémente le compteur
// d'utilisation de l'invitation. Les deux opérations sont dans une transaction.
func (h *InvitationHandler) registerUser(ctx context.Context, form *inviteFormData, inv *invitation, profile jellyfin.InviteProfile, jellyfinID, ldapDN, ldapRole string, emailVerified bool) error {
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

	canInvite := roleAllowsInvites(ldapRole) || canInviteFromProfile
	preferredLang := normalizeSupportedEmailLang(inv.PreferredLang)
	profileApplyStatus := "pending"
	var profileAppliedAt interface{}
	if strings.TrimSpace(jellyfinID) != "" {
		profileApplyStatus = "applied"
		profileAppliedAt = time.Now()
	}

	// INSERT de l'utilisateur
	_, err = tx.Exec(
		`INSERT INTO users (jellyfin_id, username, email, email_verified, ldap_dn, group_name, invited_by, preferred_lang, is_active, is_banned, can_invite, access_expires_at, delete_at, expiry_action, expiry_delete_after_days, expired_at, preset_id, profile_apply_status, profile_apply_error, profile_applied_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, TRUE, FALSE, ?, ?, ?, ?, ?, NULL, ?, ?, '', ?)`,
		jellyfinIDValue, form.Username, form.Email, emailVerified, ldapDN, groupName, inv.Code, preferredLang, canInvite, accessExpiresAt, deleteAt, expiryAction, deleteAfterDays, presetID, profileApplyStatus, profileAppliedAt,
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
		"ldap_dn", ldapDN,
		"invitation_id", inv.ID,
	)

	// Post-registration: link Authentik identity and referral tree
	var newUserID int64
	_ = h.db.QueryRow(`SELECT id FROM users WHERE username = ?`, form.Username).Scan(&newUserID)

	var sponsorUserID int64
	_ = h.db.QueryRow(`SELECT id FROM users WHERE username = ?`, inv.CreatedBy).Scan(&sponsorUserID)

	var authentikID string
	if h.authClient != nil && h.cfg != nil && h.cfg.Authentik.Enabled {
		authResp, authErr := h.authClient.CreateUser(ctx, authentik.UserCreatePayload{
			Username: form.Username,
			Email:    form.Email,
			IsActive: true,
		})
		if authErr == nil && authResp != nil {
			authentikID = authResp.ID
			if sponsorUserID > 0 {
				_, _ = h.db.Exec(`UPDATE users SET authentik_id = ?, invited_by_id = ? WHERE id = ?`, authentikID, sponsorUserID, newUserID)
			} else {
				_, _ = h.db.Exec(`UPDATE users SET authentik_id = ? WHERE id = ?`, authentikID, newUserID)
			}
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

	plan.LDAPGroups = append(resolveLDAPGroupsFromMappings(mappings, presetID, groupName), plan.EffectiveProfile.LDAPGroups...)
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
	merged.LDAPGroups = profile.LDAPGroups
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
