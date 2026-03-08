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

// ── Structures internes ─────────────────────────────────────────────────────

// invitation représente une ligne de la table invitations.
type invitation struct {
	ID              int64
	Code            string
	Label           string
	MaxUses         int
	UsedCount       int
	JellyfinProfile string // JSON brut du profil
	ExpiresAt       sql.NullTime
	CreatedBy       string
	CreatedAt       time.Time
}

// inviteFormData contient les données soumises par le formulaire d'inscription.
type inviteFormData struct {
	Username string
	Email    string
	Password string
}

// ── Invitation Handler ──────────────────────────────────────────────────────

// InvitationHandler gère les routes liées aux invitations.
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

// NewInvitationHandler crée un nouveau handler d'invitations.
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

// SetLDAPClient remplace le client LDAP (rechargement à chaud).
func (h *InvitationHandler) SetLDAPClient(ld *jgldap.Client) { h.ldClient = ld }

// SetMailer remplace le mailer SMTP (rechargement à chaud).
func (h *InvitationHandler) SetMailer(m *mail.Mailer) { h.mailer = m }

// SetNotifier remplace le notifier (rechargement à chaud).
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

// ── GET /invite/{code} ──────────────────────────────────────────────────────

// InvitePage affiche le formulaire d'inscription pour un code d'invitation donné.
func (h *InvitationHandler) InvitePage(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	// Vérifier que l'invitation existe et est valide
	inv, err := h.getValidInvitation(code)
	if err != nil {
		slog.Warn("Invitation invalide consultée", "code", code, "error", err)
		http.Error(w, h.tr(r, "invite_error_invalid_or_expired", "Invitation invalide ou expirée"), http.StatusNotFound)
		return
	}

	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.Invitation = inv
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.Data["JellyTulliURL"] = links.JellyTulliURL
	profile := jellyfin.InviteProfile{UsernameMinLength: 3, UsernameMaxLength: 32, PasswordMinLength: 8, PasswordMaxLength: 128, RequireEmail: true}

	// Analyser le profil pour vérifier si un username est forcé (Flux B)
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

// ── POST /invite/{code} — FLUX ATOMIQUE ─────────────────────────────────────

// InviteSubmit traite la soumission du formulaire d'inscription.
//
// Flux atomique avec rollback strict :
//
//	Étape 1 : Validation SQLite      → erreur = stop (rien à nettoyer)
//	Étape 2 : Création LDAP          → erreur = stop (rien à nettoyer)
//	Étape 3 : Création Jellyfin      → erreur = rollback LDAP
//	Étape 4 : Enregistrement SQLite   → erreur = rollback Jellyfin + LDAP
//	Étape 5 : Notifications           → erreur = log seulement (pas de rollback)
func (h *InvitationHandler) InviteSubmit(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	remoteAddr := r.RemoteAddr

	slog.Info("⚡ Début du flux d'inscription",
		"code", code,
		"remote", remoteAddr,
	)

	// ── Parsing du formulaire ───────────────────────────────────────────
	if err := r.ParseForm(); err != nil {
		slog.Error("Erreur parsing formulaire inscription", "error", err)
		http.Error(w, h.tr(r, "common_bad_request", "Requête invalide"), http.StatusBadRequest)
		return
	}

	submittedUsername := strings.TrimSpace(r.FormValue("username"))

	// ═══════════════════════════════════════════════════════════════════
	// ÉTAPE 1 : Validation SQLite
	// ═══════════════════════════════════════════════════════════════════
	slog.Info("📋 Étape 1/5 : Validation de l'invitation", "code", code)

	inv, err := h.getValidInvitation(code)
	if err != nil {
		slog.Warn("Invitation invalide", "code", code, "error", err)
		targetUsername := strings.TrimSpace(submittedUsername)
		if targetUsername == "" {
			targetUsername = "unknown"
		}
		h.logInviteAction(r, "invite.validation.failed", targetUsername, code, err.Error())
		http.Error(w, h.tr(r, "invite_error_invalid_or_expired", "Invitation invalide ou expirée"), http.StatusForbidden)
		return
	}

	// Décoder le profil Jellyfin de l'invitation (si défini)
	profile := jellyfin.InviteProfile{RequireEmail: true}
	if inv.JellyfinProfile != "" {
		if err := json.Unmarshal([]byte(inv.JellyfinProfile), &profile); err != nil {
			slog.Error("Profil Jellyfin invalide dans l'invitation", "code", code, "error", err)
			http.Error(w, h.tr(r, "invite_error_config", "Erreur de configuration de l'invitation"), http.StatusInternalServerError)
			return
		}
	} else {
		// Profil par défaut : accès à toutes les bibliothèques
		profile = jellyfin.InviteProfile{
			RequireEmail:       true,
			EnableAllFolders:   true,
			EnableDownload:     true,
			EnableRemoteAccess: true,
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

	// ── JFA-Go Flux B (Forced Username) ─────────────────────────
	if profile.ForcedUsername != "" {
		slog.Debug("Flux JFA-Go (Forced Username) détecté", "forced", profile.ForcedUsername, "submitted", form.Username)
		form.Username = profile.ForcedUsername
		if err := validateInviteUsername(form.Username, &profile); err != nil {
			slog.Error("Nom d'utilisateur forcé invalide", "code", code, "forced_username", profile.ForcedUsername, "error", err)
			http.Error(w, h.tr(r, "invite_error_config", "Erreur de configuration de l'invitation"), http.StatusInternalServerError)
			return
		}
	}

	slog.Info("✅ Étape 1/5 terminée", "code", code, "uses", fmt.Sprintf("%d/%d", inv.UsedCount, inv.MaxUses))

	ldapCfg, _ := h.db.GetLDAPConfig()
	ldapOnlyMode := h.ldClient != nil && ldapCfg.Enabled && strings.EqualFold(strings.TrimSpace(ldapCfg.ProvisionMode), "ldap_only")
	createJellyfinUser := !ldapOnlyMode

	ldapProvisionRole := resolveLDAPProvisionRole(profile, ldapCfg)
	if ldapProvisionRole != jgldap.ProvisionRoleUser {
		slog.Info("Provisioning LDAP role detecte depuis le profil d'invitation",
			"role", ldapProvisionRole,
			"group_name", strings.TrimSpace(profile.GroupName),
			"inviter_group", strings.TrimSpace(ldapCfg.InviterGroup),
			"admin_group", strings.TrimSpace(ldapCfg.AdministratorsGroup),
		)
	}

	// ═══════════════════════════════════════════════════════════════════
	// ÉTAPE 2 : Création du compte dans l'Active Directory (LDAP)
	// ═══════════════════════════════════════════════════════════════════
	var userDN string
	if h.ldClient != nil {
		slog.Info("🔐 Étape 2/5 : Création du compte LDAP", "username", form.Username)

		userDN, err = h.ldClient.CreateUser(form.Username, form.Username, form.Email, form.Password, ldapProvisionRole)
		if err != nil {
			slog.Error("❌ Étape 2/5 échouée : création LDAP",
				"username", form.Username,
				"error", err,
			)
			h.logInviteAction(r, "invite.ldap.failed", form.Username, code, err.Error())
			http.Error(w, h.tr(r, "invite_error_ldap_create", "Erreur lors de la création du compte (LDAP)"), http.StatusInternalServerError)
			return
		}

		slog.Info("✅ Étape 2/5 terminée", "dn", userDN)
	} else {
		slog.Info("⏭️ Étape 2/5 ignorée (LDAP désactivé)")
	}

	var jellyfinID string

	// ═══════════════════════════════════════════════════════════════════
	// ÉTAPE 3 : Création du compte dans Jellyfin + profil
	// ═══════════════════════════════════════════════════════════════════
	if createJellyfinUser {
		slog.Info("🎬 Étape 3/5 : Création du compte Jellyfin", "username", form.Username)

		jfUser, err := h.jfClient.CreateUser(form.Username, form.Password)
		if err != nil {
			slog.Error("❌ Étape 3/5 échouée : création Jellyfin",
				"username", form.Username,
				"error", err,
			)

			// ── ROLLBACK : Supprimer le compte LDAP créé à l'étape 2 ────
			if h.ldClient != nil && userDN != "" {
				slog.Warn("🔄 Rollback : suppression du compte LDAP", "dn", userDN)
				if rbErr := h.ldClient.DeleteUser(userDN); rbErr != nil {
					slog.Error("⚠️ ROLLBACK LDAP ÉCHOUÉ — intervention manuelle requise",
						"dn", userDN,
						"rollback_error", rbErr,
						"original_error", err,
					)
					h.logInviteAction(r, "invite.rollback.ldap.failed", form.Username, userDN, rbErr.Error())
				} else {
					slog.Info("✅ Rollback LDAP réussi", "dn", userDN)
				}
			}

			h.logInviteAction(r, "invite.jellyfin.failed", form.Username, code, err.Error())
			http.Error(w, h.tr(r, "invite_error_jellyfin_create", "Erreur lors de la création du compte (Jellyfin)"), http.StatusInternalServerError)
			return
		}

		jellyfinID = jfUser.ID

		// Appliquer le profil d'invitation (bibliothèques, droits)
		if err := h.jfClient.ApplyInviteProfile(jfUser.ID, profile); err != nil {
			slog.Warn("Erreur lors de l'application du profil Jellyfin (non-bloquant)",
				"jellyfin_id", jfUser.ID,
				"error", err,
			)
			// Le profil n'est pas critique — on continue mais on log
			h.logInviteAction(r, "invite.profile.failed", form.Username, jfUser.ID, err.Error())
		}

		if err := h.applyGroupPolicyFromProfile(profile, jfUser.ID, userDN); err != nil {
			slog.Warn("Erreur application mapping de groupe (non-bloquant)",
				"group", strings.TrimSpace(profile.GroupName),
				"jellyfin_id", jfUser.ID,
				"error", err,
			)
			h.logInviteAction(r, "invite.group_mapping.failed", form.Username, jfUser.ID, err.Error())
		}

		slog.Info("✅ Étape 3/5 terminée", "jellyfin_id", jfUser.ID)
	} else {
		slog.Info("⏭️ Étape 3/5 ignorée (mode LDAP-only)", "username", form.Username)
	}

	// ═══════════════════════════════════════════════════════════════════
	// ÉTAPE 4 : Enregistrement dans SQLite
	// ═══════════════════════════════════════════════════════════════════
	slog.Info("💾 Étape 4/5 : Enregistrement SQLite", "username", form.Username)

	if err := h.registerUser(form, inv, jellyfinID, userDN, ldapProvisionRole); err != nil {
		slog.Error("❌ Étape 4/5 échouée : enregistrement SQLite",
			"username", form.Username,
			"error", err,
		)

		// ── ROLLBACK : Supprimer Jellyfin ET LDAP ───────────────────
		slog.Warn("🔄 Rollback : suppression Jellyfin + LDAP")

		// Rollback Jellyfin (si mode hybride)
		if createJellyfinUser && strings.TrimSpace(jellyfinID) != "" {
			if rbErr := h.jfClient.DeleteUser(jellyfinID); rbErr != nil {
				slog.Error("⚠️ ROLLBACK JELLYFIN ÉCHOUÉ — intervention manuelle requise",
					"jellyfin_id", jellyfinID,
					"rollback_error", rbErr,
				)
				h.logInviteAction(r, "invite.rollback.jellyfin.failed", form.Username, jellyfinID, rbErr.Error())
			} else {
				slog.Info("✅ Rollback Jellyfin réussi", "id", jellyfinID)
			}
		}

		// Rollback LDAP
		if h.ldClient != nil && userDN != "" {
			if rbErr := h.ldClient.DeleteUser(userDN); rbErr != nil {
				slog.Error("⚠️ ROLLBACK LDAP ÉCHOUÉ — intervention manuelle requise",
					"dn", userDN,
					"rollback_error", rbErr,
				)
				h.logInviteAction(r, "invite.rollback.ldap.failed", form.Username, userDN, rbErr.Error())
			} else {
				slog.Info("✅ Rollback LDAP réussi", "dn", userDN)
			}
		}

		h.logInviteAction(r, "invite.sqlite.failed", form.Username, code, err.Error())
		http.Error(w, h.tr(r, "invite_error_persist", "Erreur lors de l'enregistrement du compte"), http.StatusInternalServerError)
		return
	}

	slog.Info("✅ Étape 4/5 terminée", "username", form.Username)

	// ═══════════════════════════════════════════════════════════════════
	// ÉTAPE 5 : Notifications (pas de rollback si échec)
	// ═══════════════════════════════════════════════════════════════════
	slog.Info("📨 Étape 5/5 : Notifications", "username", form.Username)

	// Envoyer les webhooks (Discord, Telegram, Matrix) — asynchrone
	h.notifier.NotifyUserRegistered(notify.UserRegisteredEvent{
		Username:    form.Username,
		DisplayName: form.Username,
		Email:       form.Email,
		InviteCode:  code,
		InvitedBy:   inv.CreatedBy,
		JellyfinID:  jellyfinID,
		LdapDN:      userDN,
	})

	if h.mailer != nil && strings.TrimSpace(form.Email) != "" {
		emailCfg, _ := h.db.GetEmailTemplatesConfig()
		defaults := config.DefaultEmailTemplates()
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
				"Username":      form.Username,
				"DisplayName":   form.Username,
				"Email":         form.Email,
				"InviteCode":    code,
				"InviteLink":    publicBaseURL + "/invite/" + code,
				"HelpURL":       publicBaseURL,
				"JellyGateURL":  publicBaseURL,
				"JellyfinURL":   links.JellyfinURL,
				"JellyseerrURL": links.JellyseerrURL,
				"JellyTulliURL": links.JellyTulliURL,
			}

			subject := firstNonEmpty(append(subjectCandidates, defaults.WelcomeSubject)...)
			if err := sendTemplateIfConfigured(h.mailer, form.Email, subject, "welcome", combinedTemplate, emailCfg, emailData); err != nil {
				slog.Error("Erreur envoi email post-inscription", "email", form.Email, "error", err)
				h.logInviteAction(r, "invite.welcome_email.failed", form.Username, code, err.Error())
			}
		}
	}

	if strings.TrimSpace(form.Email) != "" {
		var userID int64
		err := h.db.QueryRow(`SELECT id FROM users WHERE username = ? LIMIT 1`, form.Username).Scan(&userID)
		if err != nil {
			slog.Error("Impossible de retrouver l'utilisateur pour la verification email", "username", form.Username, "error", err)
			h.logInviteAction(r, "invite.email_verification.lookup_failed", form.Username, code, err.Error())
		} else if err := sendEmailVerification(h.cfg, h.db, h.mailer, userID, true); err != nil {
			slog.Error("Erreur envoi email de verification", "username", form.Username, "email", form.Email, "error", err)
			h.logInviteAction(r, "invite.email_verification.failed", form.Username, code, err.Error())
		} else {
			h.logInviteAction(r, "invite.email_verification.sent", form.Username, code, form.Email)
		}
	}

	if h.provisioner != nil && h.provisioner.IsEnabled() {
		if err := h.provisioner.ProvisionUser(form.Username, form.Password, form.Email); err != nil {
			slog.Warn("Provisioning compte tiers échoué", "username", form.Username, "error", err)
			h.logInviteAction(r, "invite.integration.failed", form.Username, code, err.Error())
		} else {
			h.logInviteAction(r, "invite.integration.provisioned", form.Username, code, "Jellyseerr/Ombi")
		}
	}

	h.logInviteAction(r, "invite.used", form.Username, code,
		fmt.Sprintf(`{"jellyfin_id":"%s","ldap_dn":"%s","email":"%s","mode":"%s"}`,
			jellyfinID,
			userDN,
			form.Email,
			map[bool]string{true: "ldap_only", false: "hybrid"}[ldapOnlyMode],
		))

	slog.Info("🎉 Inscription terminée avec succès",
		"username", form.Username,
		"jellyfin_id", jellyfinID,
		"ldap_dn", userDN,
		"invitation", code,
	)

	// ── Réponse de succès ────────────────────────────────────────────────
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	if createJellyfinUser {
		td.SuccessMessage = strings.ReplaceAll(
			h.tr(r, "invite_success_hybrid", "Bienvenue {username} ! Votre compte a été créé avec succès dans Jellyfin et dans l'annuaire. Vérifiez maintenant votre email pour finaliser votre contact."),
			"{username}",
			form.Username,
		)
	} else {
		td.SuccessMessage = strings.ReplaceAll(
			h.tr(r, "invite_success_ldap_only", "Bienvenue {username} ! Votre compte a été créé dans l'annuaire LDAP. Le compte Jellyfin sera utilisé via votre integration LDAP. Vérifiez maintenant votre email pour finaliser votre contact."),
			"{username}",
			form.Username,
		)
	}
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.Data["JellyTulliURL"] = links.JellyTulliURL

	if err := h.renderer.Render(w, "invite.html", td); err != nil {
		slog.Error("Erreur rendu invite success page", "error", err)
		http.Error(w, h.tr(r, "common_server_error", "Erreur serveur"), http.StatusInternalServerError)
	}
}

// ── Méthodes internes ───────────────────────────────────────────────────────

// validateForm valide et extrait les données du formulaire d'inscription.
func (h *InvitationHandler) validateForm(r *http.Request, profile *jellyfin.InviteProfile) (*inviteFormData, error) {
	username := strings.TrimSpace(r.FormValue("username"))
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	passwordConfirm := r.FormValue("password_confirm")

	// Validations
	if err := validateInviteUsername(username, profile); err != nil {
		return nil, err
	}

	if password == "" {
		return nil, fmt.Errorf("le mot de passe est requis")
	}

	if err := validateInvitePassword(password, profile); err != nil {
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

func resolveLDAPProvisionRole(profile jellyfin.InviteProfile, ldapCfg config.LDAPConfig) string {
	groupName := strings.ToLower(strings.TrimSpace(profile.GroupName))
	if groupName == "" {
		return jgldap.ProvisionRoleUser
	}

	adminGroup := strings.ToLower(strings.TrimSpace(ldapCfg.AdministratorsGroup))
	if adminGroup != "" && groupName == adminGroup {
		return jgldap.ProvisionRoleAdmin
	}

	inviterGroup := strings.ToLower(strings.TrimSpace(ldapCfg.InviterGroup))
	if inviterGroup != "" && groupName == inviterGroup {
		return jgldap.ProvisionRoleInviter
	}

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

func validateInviteUsername(username string, profile *jellyfin.InviteProfile) error {
	usernamePolicy := jellyfin.InviteProfile{}
	if profile != nil {
		usernamePolicy = *profile
	}
	minLength, maxLength := resolveInviteUsernamePolicy(usernamePolicy)

	if username == "" {
		return fmt.Errorf("le nom d'utilisateur est requis")
	}
	if len(username) < minLength || len(username) > maxLength {
		return fmt.Errorf("le nom d'utilisateur doit faire entre %d et %d caractères", minLength, maxLength)
	}

	for _, c := range username {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return fmt.Errorf("le nom d'utilisateur ne peut contenir que des lettres, chiffres, tirets et underscores")
		}
	}

	return nil
}

func validateInvitePassword(password string, profile *jellyfin.InviteProfile) error {
	policy := resolveInvitePasswordPolicy(jellyfin.InviteProfile{})
	if profile != nil {
		policy = resolveInvitePasswordPolicy(*profile)
	}

	if len(password) < policy.MinLength {
		return fmt.Errorf("le mot de passe doit faire au minimum %d caractères", policy.MinLength)
	}
	if len(password) > policy.MaxLength {
		return fmt.Errorf("le mot de passe doit faire au maximum %d caractères", policy.MaxLength)
	}
	if policy.RequireUpper && !strings.ContainsAny(password, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		return fmt.Errorf("le mot de passe doit contenir au moins une lettre majuscule")
	}
	if policy.RequireLower && !strings.ContainsAny(password, "abcdefghijklmnopqrstuvwxyz") {
		return fmt.Errorf("le mot de passe doit contenir au moins une lettre minuscule")
	}
	if policy.RequireDigit && !strings.ContainsAny(password, "0123456789") {
		return fmt.Errorf("le mot de passe doit contenir au moins un chiffre")
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
			return fmt.Errorf("le mot de passe doit contenir au moins un caractère spécial")
		}
	}

	return nil
}

// getValidInvitation récupère et valide une invitation depuis SQLite.
// Vérifie : existence, expiration, et quota d'utilisation.
func (h *InvitationHandler) getValidInvitation(code string) (*invitation, error) {
	if code == "" {
		return nil, fmt.Errorf("code d'invitation vide")
	}

	cleanupClosedInvitationsIfEnabled(h.db)

	row := h.db.QueryRow(
		`SELECT id, code, label, max_uses, used_count, jellyfin_profile, expires_at, created_by, created_at
		 FROM invitations WHERE code = ?`, code)

	var inv invitation
	var jellyfinProfile sql.NullString
	var label sql.NullString
	var createdBy sql.NullString

	err := row.Scan(
		&inv.ID, &inv.Code, &label, &inv.MaxUses, &inv.UsedCount,
		&jellyfinProfile, &inv.ExpiresAt, &createdBy, &inv.CreatedAt,
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
	inv.CreatedBy = createdBy.String

	// Vérifier l'expiration
	if inv.ExpiresAt.Valid && time.Now().After(inv.ExpiresAt.Time) {
		return nil, fmt.Errorf("invitation %q expirée depuis %s", code, inv.ExpiresAt.Time.Format("02/01/2006 15:04"))
	}

	// Vérifier le quota d'utilisation (0 = illimité)
	if inv.MaxUses > 0 && inv.UsedCount >= inv.MaxUses {
		return nil, fmt.Errorf("invitation %q a atteint sa limite d'utilisation (%d/%d)", code, inv.UsedCount, inv.MaxUses)
	}

	return &inv, nil
}

// registerUser insère l'utilisateur dans SQLite et incrémente le compteur
// d'utilisation de l'invitation. Les deux opérations sont dans une transaction.
func (h *InvitationHandler) registerUser(form *inviteFormData, inv *invitation, jellyfinID, ldapDN, ldapRole string) error {
	tx, err := h.db.Begin()
	if err != nil {
		return fmt.Errorf("impossible de démarrer la transaction: %w", err)
	}
	defer tx.Rollback() // No-op si Commit() a été appelé

	// Parsing du profil JSON pour récupérer les politiques d'expiration et groupe.
	var disableAfterDays int
	var absoluteUserExpiryAt time.Time
	expiryAction := "disable"
	deleteAfterDays := 0
	groupName := ""
	canInviteFromProfile := false
	if inv.JellyfinProfile != "" {
		var pf jellyfin.InviteProfile
		if err := json.Unmarshal([]byte(inv.JellyfinProfile), &pf); err == nil {
			disableAfterDays = pf.DisableAfterDays
			if disableAfterDays <= 0 {
				disableAfterDays = pf.UserExpiryDays
			}
			expiryAction = normalizeExpiryAction(pf.ExpiryAction)
			if pf.DeleteAfterDays > 0 {
				deleteAfterDays = pf.DeleteAfterDays
			}
			if strings.TrimSpace(pf.UserExpiresAt) != "" {
				if parsed, err := parseAccessExpiry(pf.UserExpiresAt); err == nil {
					absoluteUserExpiryAt = parsed
				}
			}
			groupName = strings.TrimSpace(pf.GroupName)
			canInviteFromProfile = pf.CanInvite
		}
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

	// INSERT de l'utilisateur
	_, err = tx.Exec(
		`INSERT INTO users (jellyfin_id, username, email, ldap_dn, group_name, invited_by, is_active, is_banned, can_invite, access_expires_at, delete_at, expiry_action, expiry_delete_after_days, expired_at)
		 VALUES (?, ?, ?, ?, ?, ?, TRUE, FALSE, ?, ?, ?, ?, ?, NULL)`,
		jellyfinIDValue, form.Username, form.Email, ldapDN, groupName, inv.Code, canInvite, accessExpiresAt, deleteAt, expiryAction, deleteAfterDays,
	)
	if err != nil {
		return fmt.Errorf("impossible d'insérer l'utilisateur %q: %w", form.Username, err)
	}

	// INCREMENT du compteur d'utilisation
	result, err := tx.Exec(
		`UPDATE invitations SET used_count = used_count + 1 WHERE id = ?`,
		inv.ID,
	)
	if err != nil {
		return fmt.Errorf("impossible d'incrémenter le compteur de l'invitation %d: %w", inv.ID, err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("invitation %d non trouvée lors de l'incrémentation", inv.ID)
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

	return nil
}

func (h *InvitationHandler) applyGroupPolicyFromProfile(profile jellyfin.InviteProfile, jellyfinID, userDN string) error {
	groupName := strings.TrimSpace(profile.GroupName)
	if groupName == "" || strings.TrimSpace(jellyfinID) == "" {
		return nil
	}

	mappings, err := h.db.GetGroupPolicyMappings()
	if err != nil {
		return err
	}

	var mapping *config.GroupPolicyMapping
	for i := range mappings {
		if strings.EqualFold(strings.TrimSpace(mappings[i].GroupName), groupName) {
			mapping = &mappings[i]
			break
		}
	}
	if mapping == nil {
		return nil
	}

	presetID := strings.TrimSpace(strings.ToLower(mapping.PolicyPresetID))
	if presetID == "" {
		return nil
	}

	presets, err := h.db.GetJellyfinPolicyPresets()
	if err != nil {
		return err
	}

	var preset *config.JellyfinPolicyPreset
	for i := range presets {
		if strings.TrimSpace(strings.ToLower(presets[i].ID)) == presetID {
			preset = &presets[i]
			break
		}
	}
	if preset == nil {
		return fmt.Errorf("preset %q introuvable pour groupe %q", presetID, groupName)
	}

	user, err := h.jfClient.GetUser(jellyfinID)
	if err != nil {
		return fmt.Errorf("lecture utilisateur jellyfin: %w", err)
	}

	policy := user.Policy
	policy.EnableAllFolders = preset.EnableAllFolders
	policy.EnabledFolders = preset.EnabledFolderIDs
	policy.EnableContentDownloading = preset.EnableDownload
	policy.EnableRemoteAccess = preset.EnableRemoteAccess
	policy.MaxActiveSessions = preset.MaxSessions
	policy.RemoteClientBitrateLimit = preset.BitrateLimit

	if err := h.jfClient.SetUserPolicy(jellyfinID, policy); err != nil {
		return fmt.Errorf("application policy jellyfin: %w", err)
	}

	if mapping.Source == "ldap" && h.ldClient != nil && strings.TrimSpace(userDN) != "" && strings.TrimSpace(mapping.LDAPGroupDN) != "" {
		if err := h.ldClient.AddUserToGroup(userDN, mapping.LDAPGroupDN); err != nil {
			return fmt.Errorf("assignation groupe ldap: %w", err)
		}
	}

	return nil
}
