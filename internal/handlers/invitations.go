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
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

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
	Username    string
	Email       string
	Password    string
	DisplayName string
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

// ── GET /invite/{code} ──────────────────────────────────────────────────────

// InvitePage affiche le formulaire d'inscription pour un code d'invitation donné.
func (h *InvitationHandler) InvitePage(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	// Vérifier que l'invitation existe et est valide
	inv, err := h.getValidInvitation(code)
	if err != nil {
		slog.Warn("Invitation invalide consultée", "code", code, "error", err)
		http.Error(w, "Invitation invalide ou expirée", http.StatusNotFound)
		return
	}

	td := h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context()))
	td.Invitation = inv
	profile := jellyfin.InviteProfile{PasswordMinLength: 8}

	// Analyser le profil pour vérifier si un username est forcé (Flux B)
	if inv.JellyfinProfile != "" {
		if err := json.Unmarshal([]byte(inv.JellyfinProfile), &profile); err != nil {
			slog.Warn("Profil Jellyfin invalide dans invitation page", "code", code, "error", err)
		} else if profile.ForcedUsername != "" {
			td.Data["ForcedUsername"] = profile.ForcedUsername
		}
	}

	pwdPolicy := resolveInvitePasswordPolicy(profile)
	td.Data["PasswordMinLength"] = pwdPolicy.MinLength
	td.Data["PasswordRequireUpper"] = pwdPolicy.RequireUpper
	td.Data["PasswordRequireLower"] = pwdPolicy.RequireLower
	td.Data["PasswordRequireDigit"] = pwdPolicy.RequireDigit
	td.Data["PasswordRequireSpecial"] = pwdPolicy.RequireSpecial

	if err := h.renderer.Render(w, "invite.html", td); err != nil {
		slog.Error("Erreur rendu invitation page", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
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
		http.Error(w, "Requête invalide", http.StatusBadRequest)
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
		_ = h.db.LogAction("invite.validation.failed", targetUsername, code, err.Error())
		http.Error(w, "Invitation invalide ou expirée", http.StatusForbidden)
		return
	}

	// Décoder le profil Jellyfin de l'invitation (si défini)
	var profile jellyfin.InviteProfile
	if inv.JellyfinProfile != "" {
		if err := json.Unmarshal([]byte(inv.JellyfinProfile), &profile); err != nil {
			slog.Error("Profil Jellyfin invalide dans l'invitation", "code", code, "error", err)
			http.Error(w, "Erreur de configuration de l'invitation", http.StatusInternalServerError)
			return
		}
	} else {
		// Profil par défaut : accès à toutes les bibliothèques
		profile = jellyfin.InviteProfile{
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
		_ = h.db.LogAction("invite.validation.failed", targetUsername, code, err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// ── JFA-Go Flux B (Forced Username) ─────────────────────────
	if profile.ForcedUsername != "" {
		slog.Debug("Flux JFA-Go (Forced Username) détecté", "forced", profile.ForcedUsername, "submitted", form.Username)
		form.Username = profile.ForcedUsername
		if err := validateInviteUsername(form.Username); err != nil {
			slog.Error("Nom d'utilisateur forcé invalide", "code", code, "forced_username", profile.ForcedUsername, "error", err)
			http.Error(w, "Erreur de configuration de l'invitation", http.StatusInternalServerError)
			return
		}
	}

	slog.Info("✅ Étape 1/5 terminée", "code", code, "uses", fmt.Sprintf("%d/%d", inv.UsedCount, inv.MaxUses))

	ldapCfg, _ := h.db.GetLDAPConfig()
	ldapOnlyMode := h.ldClient != nil && ldapCfg.Enabled && strings.EqualFold(strings.TrimSpace(ldapCfg.ProvisionMode), "ldap_only")
	createJellyfinUser := !ldapOnlyMode

	isAdminProvision := shouldProvisionAsLDAPAdmin(profile, ldapCfg)
	if isAdminProvision {
		slog.Info("Provisioning LDAP admin detecte depuis le profil d'invitation",
			"group_name", strings.TrimSpace(profile.GroupName),
			"admin_group", strings.TrimSpace(ldapCfg.AdministratorsGroup),
		)
	}

	// ═══════════════════════════════════════════════════════════════════
	// ÉTAPE 2 : Création du compte dans l'Active Directory (LDAP)
	// ═══════════════════════════════════════════════════════════════════
	var userDN string
	if h.ldClient != nil {
		slog.Info("🔐 Étape 2/5 : Création du compte LDAP", "username", form.Username)

		userDN, err = h.ldClient.CreateUser(form.Username, form.DisplayName, form.Email, form.Password, isAdminProvision)
		if err != nil {
			slog.Error("❌ Étape 2/5 échouée : création LDAP",
				"username", form.Username,
				"error", err,
			)
			_ = h.db.LogAction("invite.ldap.failed", form.Username, code, err.Error())
			http.Error(w, "Erreur lors de la création du compte (LDAP)", http.StatusInternalServerError)
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
					_ = h.db.LogAction("invite.rollback.ldap.failed", form.Username, userDN, rbErr.Error())
				} else {
					slog.Info("✅ Rollback LDAP réussi", "dn", userDN)
				}
			}

			_ = h.db.LogAction("invite.jellyfin.failed", form.Username, code, err.Error())
			http.Error(w, "Erreur lors de la création du compte (Jellyfin)", http.StatusInternalServerError)
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
			_ = h.db.LogAction("invite.profile.failed", form.Username, jfUser.ID, err.Error())
		}

		if err := h.applyGroupPolicyFromProfile(profile, jfUser.ID, userDN); err != nil {
			slog.Warn("Erreur application mapping de groupe (non-bloquant)",
				"group", strings.TrimSpace(profile.GroupName),
				"jellyfin_id", jfUser.ID,
				"error", err,
			)
			_ = h.db.LogAction("invite.group_mapping.failed", form.Username, jfUser.ID, err.Error())
		}

		slog.Info("✅ Étape 3/5 terminée", "jellyfin_id", jfUser.ID)
	} else {
		slog.Info("⏭️ Étape 3/5 ignorée (mode LDAP-only)", "username", form.Username)
	}

	// ═══════════════════════════════════════════════════════════════════
	// ÉTAPE 4 : Enregistrement dans SQLite
	// ═══════════════════════════════════════════════════════════════════
	slog.Info("💾 Étape 4/5 : Enregistrement SQLite", "username", form.Username)

	if err := h.registerUser(form, inv, jellyfinID, userDN); err != nil {
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
				_ = h.db.LogAction("invite.rollback.jellyfin.failed", form.Username, jellyfinID, rbErr.Error())
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
				_ = h.db.LogAction("invite.rollback.ldap.failed", form.Username, userDN, rbErr.Error())
			} else {
				slog.Info("✅ Rollback LDAP réussi", "dn", userDN)
			}
		}

		_ = h.db.LogAction("invite.sqlite.failed", form.Username, code, err.Error())
		http.Error(w, "Erreur lors de l'enregistrement du compte", http.StatusInternalServerError)
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
		DisplayName: form.DisplayName,
		Email:       form.Email,
		InviteCode:  code,
		InvitedBy:   inv.CreatedBy,
		JellyfinID:  jellyfinID,
		LdapDN:      userDN,
	})

	if h.mailer != nil && strings.TrimSpace(form.Email) != "" {
		emailCfg, _ := h.db.GetEmailTemplatesConfig()
		links := resolvePortalLinks(h.cfg, h.db)
		combinedTemplate := joinTemplateSections(
			emailCfg.Welcome,
			emailCfg.Confirmation,
			emailCfg.PostSignupHelp,
			emailCfg.UserCreation,
		)

		if combinedTemplate != "" {
			emailData := map[string]string{
				"Username":      form.Username,
				"DisplayName":   form.DisplayName,
				"Email":         form.Email,
				"InviteCode":    code,
				"InviteLink":    strings.TrimRight(h.cfg.BaseURL, "/") + "/invite/" + code,
				"HelpURL":       h.cfg.BaseURL,
				"JellyfinURL":   links.JellyfinURL,
				"JellyseerrURL": links.JellyseerrURL,
				"JellyTulliURL": links.JellyTulliURL,
			}

			if err := sendTemplateIfConfigured(h.mailer, form.Email, "Bienvenue sur JellyGate", combinedTemplate, emailData); err != nil {
				slog.Error("Erreur envoi email post-inscription", "email", form.Email, "error", err)
				_ = h.db.LogAction("invite.welcome_email.failed", form.Username, code, err.Error())
			}
		}
	}

	if h.provisioner != nil && h.provisioner.IsEnabled() {
		if err := h.provisioner.ProvisionUser(form.Username, form.Password, form.Email); err != nil {
			slog.Warn("Provisioning compte tiers échoué", "username", form.Username, "error", err)
			_ = h.db.LogAction("invite.integration.failed", form.Username, code, err.Error())
		} else {
			_ = h.db.LogAction("invite.integration.provisioned", form.Username, code, "Jellyseerr/Ombi")
		}
	}

	_ = h.db.LogAction("invite.used", form.Username, code,
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
	td := h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context()))
	if createJellyfinUser {
		td.SuccessMessage = fmt.Sprintf("Bienvenue %s ! Votre compte a été créé avec succès dans Jellyfin et dans l'annuaire.", form.DisplayName)
	} else {
		td.SuccessMessage = fmt.Sprintf("Bienvenue %s ! Votre compte a été créé dans l'annuaire LDAP. Le compte Jellyfin sera utilisé via votre integration LDAP.", form.DisplayName)
	}
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.Data["JellyTulliURL"] = links.JellyTulliURL

	if err := h.renderer.Render(w, "invite.html", td); err != nil {
		slog.Error("Erreur rendu invite success page", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
	}
}

// ── Méthodes internes ───────────────────────────────────────────────────────

// validateForm valide et extrait les données du formulaire d'inscription.
func (h *InvitationHandler) validateForm(r *http.Request, profile *jellyfin.InviteProfile) (*inviteFormData, error) {
	username := strings.TrimSpace(r.FormValue("username"))
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	passwordConfirm := r.FormValue("password_confirm")
	displayName := strings.TrimSpace(r.FormValue("display_name"))

	// Validations
	if err := validateInviteUsername(username); err != nil {
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

	if displayName == "" {
		displayName = username // Fallback : utiliser le username
	}

	return &inviteFormData{
		Username:    username,
		Email:       email,
		Password:    password,
		DisplayName: displayName,
	}, nil
}

type invitePasswordPolicy struct {
	MinLength      int
	RequireUpper   bool
	RequireLower   bool
	RequireDigit   bool
	RequireSpecial bool
}

func shouldProvisionAsLDAPAdmin(profile jellyfin.InviteProfile, ldapCfg config.LDAPConfig) bool {
	groupName := strings.ToLower(strings.TrimSpace(profile.GroupName))
	if groupName == "" {
		return false
	}

	adminGroup := strings.ToLower(strings.TrimSpace(ldapCfg.AdministratorsGroup))
	if adminGroup != "" && groupName == adminGroup {
		return true
	}

	return groupName == "admin" || groupName == "admins" || groupName == "administrator" || groupName == "administrators"
}

func resolveInvitePasswordPolicy(profile jellyfin.InviteProfile) invitePasswordPolicy {
	minLength := profile.PasswordMinLength
	if minLength <= 0 {
		minLength = 8
	}

	return invitePasswordPolicy{
		MinLength:      minLength,
		RequireUpper:   profile.PasswordRequireUpper,
		RequireLower:   profile.PasswordRequireLower,
		RequireDigit:   profile.PasswordRequireDigit,
		RequireSpecial: profile.PasswordRequireSpecial,
	}
}

func validateInviteUsername(username string) error {
	if username == "" {
		return fmt.Errorf("le nom d'utilisateur est requis")
	}
	if len(username) < 3 || len(username) > 32 {
		return fmt.Errorf("le nom d'utilisateur doit faire entre 3 et 32 caractères")
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
func (h *InvitationHandler) registerUser(form *inviteFormData, inv *invitation, jellyfinID, ldapDN string) error {
	tx, err := h.db.Begin()
	if err != nil {
		return fmt.Errorf("impossible de démarrer la transaction: %w", err)
	}
	defer tx.Rollback() // No-op si Commit() a été appelé

	// Parsing du profil JSON pour récupérer les politiques d'expiration et groupe.
	var disableAfterDays int
	expiryAction := "disable"
	deleteAfterDays := 0
	groupName := ""
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
			groupName = strings.TrimSpace(pf.GroupName)
		}
	}

	var accessExpiresAt interface{}
	if disableAfterDays > 0 {
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

	// INSERT de l'utilisateur
	_, err = tx.Exec(
		`INSERT INTO users (jellyfin_id, username, email, ldap_dn, group_name, invited_by, is_active, is_banned, access_expires_at, delete_at, expiry_action, expiry_delete_after_days, expired_at)
		 VALUES (?, ?, ?, ?, ?, ?, TRUE, FALSE, ?, ?, ?, ?, NULL)`,
		jellyfinIDValue, form.Username, form.Email, ldapDN, groupName, inv.Code, accessExpiresAt, deleteAt, expiryAction, deleteAfterDays,
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
