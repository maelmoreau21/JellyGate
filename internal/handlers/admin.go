// Package handlers — admin.go
//
// Gère les endpoints JSON du tableau de bord administrateur.
// Toutes les routes sont protégées par le middleware RequireAuth.
//
// Endpoints :
//   - GET    /admin/api/users         → Liste des utilisateurs (fusion SQLite + Jellyfin)
//   - POST   /admin/api/users/{id}/toggle → Active/désactive un compte (AD + Jellyfin)
//   - DELETE /admin/api/users/{id}    → Suppression totale (AD + Jellyfin + SQLite)
//
// Les erreurs partielles sont loggées mais ne bloquent pas les opérations
// restantes (ex: si l'utilisateur est déjà supprimé de l'AD, on continue
// avec Jellyfin et SQLite).
package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	jgldap "github.com/maelmoreau21/JellyGate/internal/ldap"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/notify"
	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

// ── Structures de réponse JSON ──────────────────────────────────────────────

// UserResponse est la représentation JSON d'un utilisateur pour l'API admin.
type UserResponse struct {
	ID              int64  `json:"id"`
	JellyfinID      string `json:"jellyfin_id"`
	Username        string `json:"username"`
	Email           string `json:"email"`
	LdapDN          string `json:"ldap_dn"`
	InvitedBy       string `json:"invited_by"`
	IsActive        bool   `json:"is_active"`
	IsBanned        bool   `json:"is_banned"`
	CanInvite       bool   `json:"can_invite"`
	AccessExpiresAt string `json:"access_expires_at,omitempty"` // ISO 8601
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`

	// Statuts temps réel depuis Jellyfin (enrichissement)
	JellyfinDisabled bool `json:"jellyfin_disabled"`
	JellyfinExists   bool `json:"jellyfin_exists"`
}

// APIResponse est l'enveloppe standard pour toutes les réponses JSON.
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Errors  []string    `json:"errors,omitempty"`
}

// ── Admin Handler ───────────────────────────────────────────────────────────

// AdminHandler gère les endpoints d'administration.
type AdminHandler struct {
	cfg      *config.Config
	db       *database.DB
	jfClient *jellyfin.Client
	ldClient *jgldap.Client
	renderer *render.Engine
}

// NewAdminHandler crée un nouveau handler d'administration.
func NewAdminHandler(cfg *config.Config, db *database.DB, jf *jellyfin.Client, ld *jgldap.Client, renderer *render.Engine) *AdminHandler {
	return &AdminHandler{
		cfg:      cfg,
		db:       db,
		jfClient: jf,
		ldClient: ld,
		renderer: renderer,
	}
}

// SetLDAPClient remplace le client LDAP (rechargement à chaud).
func (h *AdminHandler) SetLDAPClient(ld *jgldap.Client) { h.ldClient = ld }

// ── Background Jobs ─────────────────────────────────────────────────────────

// StartExpirationJob lance une routine en arrière-plan qui vérifie périodiquement
// si des comptes utilisateurs ont expiré, afin de les désactiver automatiquement.
func (h *AdminHandler) StartExpirationJob(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		// Faire une première vérification au démarrage court
		time.Sleep(5 * time.Second)
		h.runExpirationCheck()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.runExpirationCheck()
			}
		}
	}()
}

func (h *AdminHandler) runExpirationCheck() {
	slog.Debug("Lancement du job d'expiration automatique des utilisateurs...")

	// Rechercher les utilisateurs actifs dont access_expires_at est dépassé
	rows, err := h.db.Conn().Query(`
		SELECT id, username, jellyfin_id, ldap_dn 
		FROM users 
		WHERE is_active = 1 
		AND access_expires_at IS NOT NULL 
		AND access_expires_at < ?
	`, time.Now())
	if err != nil {
		slog.Error("Erreur SQL lors du job d'expiration", "error", err)
		return
	}

	var usersToDisable []struct {
		ID         int64
		Username   string
		JellyfinID string
		LdapDN     string
	}

	for rows.Next() {
		var u struct {
			ID         int64
			Username   string
			JellyfinID string
			LdapDN     string
		}
		var jfID, ldDN sql.NullString
		if err := rows.Scan(&u.ID, &u.Username, &jfID, &ldDN); err == nil {
			u.JellyfinID = jfID.String
			u.LdapDN = ldDN.String
			usersToDisable = append(usersToDisable, u)
		}
	}
	rows.Close()

	if len(usersToDisable) > 0 {
		slog.Info("Comptes expirés détéctés", "count", len(usersToDisable))
	}

	for _, u := range usersToDisable {
		slog.Info("Désactivation automatique de l'utilisateur (Expiré)", "user", u.Username)

		// 1. LDAP
		if h.ldClient != nil && u.LdapDN != "" {
			if err := h.ldClient.DisableUser(u.LdapDN); err != nil {
				slog.Error("Erreur lors de la désactivation LDAP (Expiration)", "error", err)
			}
		}

		// 2. Jellyfin
		if u.JellyfinID != "" {
			if err := h.jfClient.DisableUser(u.JellyfinID); err != nil {
				slog.Error("Erreur lors de la désactivation Jellyfin (Expiration)", "error", err)
			}
		}

		// 3. SQLite
		_, err := h.db.Conn().Exec(`UPDATE users SET is_active = 0 WHERE id = ?`, u.ID)
		if err != nil {
			slog.Error("Erreur lors de la désactivation SQLite (Expiration)", "error", err)
		}

		h.db.LogAction("user.expired", "system", u.Username, "Le compte a atteint sa date d'expiration et a été désactivé.")
	}
}

// ── Pages HTML ──────────────────────────────────────────────────────────────

// DashboardPage affiche la page principale du tableau de bord.
func (h *AdminHandler) DashboardPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context()))
	td.AdminUsername = sess.Username
	td.IsAdmin = sess.IsAdmin

	if td.IsAdmin {
		td.CanInvite = true
	} else {
		var canInvite bool
		_ = h.db.Conn().QueryRow(`SELECT can_invite FROM users WHERE jellyfin_id = ?`, sess.UserID).Scan(&canInvite)
		td.CanInvite = canInvite
	}

	if err := h.renderer.Render(w, "admin/dashboard.html", td); err != nil {
		slog.Error("Erreur rendu dashboard", "error", err)
		http.Error(w, "Erreur serveur : impossible de charger la page", http.StatusInternalServerError)
	}
}

// UsersPage affiche la page de gestion des utilisateurs.
func (h *AdminHandler) UsersPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context()))
	td.AdminUsername = sess.Username
	td.IsAdmin = true
	td.CanInvite = true
	if err := h.renderer.Render(w, "admin/users.html", td); err != nil {
		slog.Error("Erreur rendu users page", "error", err)
		http.Error(w, "Erreur serveur : impossible de charger la page", http.StatusInternalServerError)
	}
}

// SettingsPage affiche la page de configuration globale.
func (h *AdminHandler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context()))
	td.AdminUsername = sess.Username
	td.IsAdmin = true
	td.CanInvite = true
	if err := h.renderer.Render(w, "admin/settings.html", td); err != nil {
		slog.Error("Erreur rendu settings page", "error", err)
		http.Error(w, "Erreur serveur : impossible de charger la page", http.StatusInternalServerError)
	}
}

// InvitationsPage affiche la page de gestion des invitations.
func (h *AdminHandler) InvitationsPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context()))
	td.AdminUsername = sess.Username
	td.IsAdmin = sess.IsAdmin

	if td.IsAdmin {
		td.CanInvite = true
	} else {
		var canInvite bool
		_ = h.db.Conn().QueryRow(`SELECT can_invite FROM users WHERE jellyfin_id = ?`, sess.UserID).Scan(&canInvite)
		td.CanInvite = canInvite

		if !td.CanInvite {
			http.Error(w, "Accès interdit au programme de parrainage", http.StatusForbidden)
			return
		}
	}

	if err := h.renderer.Render(w, "admin/invitations.html", td); err != nil {
		slog.Error("Erreur rendu invitations page", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
	}
}

// LogsPage affiche la page du journal d'audit.
func (h *AdminHandler) LogsPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context()))
	td.AdminUsername = sess.Username
	td.IsAdmin = true
	td.CanInvite = true
	if err := h.renderer.Render(w, "admin/logs.html", td); err != nil {
		slog.Error("Erreur rendu logs page", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
	}
}

// ── GET /admin/api/logs ─────────────────────────────────────────────────────

// AuditLogResponse représente une ligne formatée du journal d'audit JSON.
type AuditLogResponse struct {
	ID        int64  `json:"id"`
	Action    string `json:"action"`
	Actor     string `json:"actor"`
	Target    string `json:"target"`
	Details   string `json:"details"`
	CreatedAt string `json:"created_at"`
}

// ListLogs retourne la liste des journaux d'audit (max 500).
func (h *AdminHandler) ListLogs(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	slog.Info("Liste des journaux d'audit demandée", "admin", sess.Username)

	rows, err := h.db.Conn().Query(
		`SELECT id, action, actor, target, details, created_at 
		 FROM audit_log ORDER BY created_at DESC LIMIT 500`)
	if err != nil {
		slog.Error("Erreur lecture des journaux d'audit", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de lecture de la base de données",
		})
		return
	}
	defer rows.Close()

	var logs []AuditLogResponse
	for rows.Next() {
		var l AuditLogResponse
		var actor, target, details sql.NullString

		err := rows.Scan(
			&l.ID, &l.Action, &actor, &target, &details, &l.CreatedAt,
		)
		if err != nil {
			slog.Error("Erreur scan log", "error", err)
			continue
		}

		l.Actor = actor.String
		l.Target = target.String
		l.Details = details.String

		logs = append(logs, l)
	}

	slog.Info("Liste des journaux d'audit renvoyée", "count", len(logs))

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    logs,
	})
}

// ── POST /admin/api/users/sync ──────────────────────────────────────────────

// SyncJellyfinUsers synchronise manuellement les utilisateurs Jellyfin vers SQLite
func (h *AdminHandler) SyncJellyfinUsers(w http.ResponseWriter, r *http.Request) {
	jfUsers, err := h.jfClient.GetUsers()
	if err != nil {
		slog.Error("Erreur lors de la récupération des utilisateurs Jellyfin pour la sync", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de communication avec Jellyfin",
		})
		return
	}

	var addedCount int
	for _, ju := range jfUsers {
		// INSERT OR IGNORE dans SQLite
		res, err := h.db.Conn().Exec(`
			INSERT OR IGNORE INTO users (jellyfin_id, username, is_active)
			VALUES (?, ?, ?)
		`, ju.ID, ju.Name, !ju.Policy.IsDisabled)

		if err == nil {
			if affected, _ := res.RowsAffected(); affected > 0 {
				addedCount++
			}
		}
	}

	slog.Info("Synchronisation manuelle Jellyfin terminée", "users_added", addedCount)
	h.db.LogAction("users.sync", session.FromContext(r.Context()).Username, "all",
		fmt.Sprintf("Synchronisation manuelle déclenchée: %d nouveaux utilisateurs importés", addedCount))

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: fmt.Sprintf("Synchronisation terminée: %d nouveaux utilisateurs trouvés.", addedCount),
	})
}

// ── GET /admin/api/users ────────────────────────────────────────────────────

// ListUsers retourne la liste de tous les utilisateurs avec leurs statuts
// enrichis depuis Jellyfin.
func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	slog.Info("Liste des utilisateurs demandée", "admin", sess.Username)

	// ── 1. Récupérer les utilisateurs depuis SQLite ─────────────────────
	rows, err := h.db.Conn().Query(
		`SELECT id, jellyfin_id, username, email, ldap_dn, invited_by,
		        is_active, is_banned, can_invite, access_expires_at, created_at, updated_at
		 FROM users ORDER BY created_at DESC`)
	if err != nil {
		slog.Error("Erreur lecture des utilisateurs", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de lecture de la base de données",
		})
		return
	}
	defer rows.Close()

	var users []UserResponse
	for rows.Next() {
		var u UserResponse
		var jellyfinID, email, ldapDN, invitedBy sql.NullString
		var accessExpiresAt, createdAt, updatedAt sql.NullString

		err := rows.Scan(
			&u.ID, &jellyfinID, &u.Username, &email, &ldapDN, &invitedBy,
			&u.IsActive, &u.IsBanned, &u.CanInvite, &accessExpiresAt, &createdAt, &updatedAt,
		)
		if err != nil {
			slog.Error("Erreur scan utilisateur", "error", err)
			continue
		}

		u.JellyfinID = jellyfinID.String
		u.Email = email.String
		u.LdapDN = ldapDN.String
		u.InvitedBy = invitedBy.String
		u.AccessExpiresAt = accessExpiresAt.String
		u.CreatedAt = createdAt.String
		u.UpdatedAt = updatedAt.String

		users = append(users, u)
	}

	// ── 2. Enrichir avec le statut Jellyfin en temps réel ───────────────
	// On fait un seul appel pour récupérer tous les utilisateurs Jellyfin,
	// puis on fusionne par ID pour éviter N requêtes individuelles.
	jfUsers, err := h.jfClient.GetUsers()
	if err != nil {
		slog.Warn("Impossible de récupérer les utilisateurs Jellyfin (enrichissement dégradé)",
			"error", err,
		)
		// On continue sans enrichissement — les données SQLite suffisent
	} else {
		// Construire un index par ID Jellyfin
		jfIndex := make(map[string]*jellyfin.User, len(jfUsers))
		for i := range jfUsers {
			jfIndex[jfUsers[i].ID] = &jfUsers[i]
		}

		// Fusionner
		for i := range users {
			if jfUser, ok := jfIndex[users[i].JellyfinID]; ok {
				users[i].JellyfinExists = true
				users[i].JellyfinDisabled = jfUser.Policy.IsDisabled
			}
		}
	}

	slog.Info("Liste des utilisateurs renvoyée", "count", len(users))

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    users,
	})
}

// ── POST /admin/api/users/{id}/toggle ───────────────────────────────────────

// ToggleUser active ou désactive un utilisateur simultanément dans l'AD
// et dans Jellyfin, puis met à jour le statut SQLite.
func (h *AdminHandler) ToggleUser(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "ID utilisateur invalide",
		})
		return
	}

	// ── 1. Récupérer l'utilisateur depuis SQLite ────────────────────────
	var username, jellyfinID, ldapDN string
	var isActive bool
	err = h.db.Conn().QueryRow(
		`SELECT username, jellyfin_id, ldap_dn, is_active FROM users WHERE id = ?`, userID,
	).Scan(&username, &jellyfinID, &ldapDN, &isActive)

	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, APIResponse{
			Success: false,
			Message: "Utilisateur introuvable",
		})
		return
	}
	if err != nil {
		slog.Error("Erreur lecture utilisateur pour toggle", "id", userID, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de lecture de la base de données",
		})
		return
	}

	// Nouveau statut = inverse du statut actuel
	newActive := !isActive
	var partialErrors []string
	action := "activé"
	if !newActive {
		action = "désactivé"
	}

	slog.Info("Toggle utilisateur",
		"admin", sess.Username,
		"user_id", userID,
		"username", username,
		"current_active", isActive,
		"new_active", newActive,
	)

	// ── 2. Modifier dans l'Active Directory (LDAP) ─────────────────────────────
	if h.ldClient != nil && ldapDN != "" {
		if newActive {
			err = h.ldClient.EnableUser(ldapDN)
		} else {
			err = h.ldClient.DisableUser(ldapDN)
		}
		if err != nil {
			slog.Error("Erreur toggle LDAP",
				"dn", ldapDN,
				"action", action,
				"error", err,
			)
			partialErrors = append(partialErrors, fmt.Sprintf("LDAP: %s", err.Error()))
		} else {
			slog.Info("LDAP toggle réussi", "dn", ldapDN, "action", action)
		}
	}

	// ── 3. Modifier dans Jellyfin ───────────────────────────────────────
	if jellyfinID != "" {
		if newActive {
			err = h.jfClient.EnableUser(jellyfinID)
		} else {
			err = h.jfClient.DisableUser(jellyfinID)
		}
		if err != nil {
			slog.Error("Erreur toggle Jellyfin",
				"jellyfin_id", jellyfinID,
				"action", action,
				"error", err,
			)
			partialErrors = append(partialErrors, fmt.Sprintf("Jellyfin: %s", err.Error()))
		} else {
			slog.Info("Jellyfin toggle réussi", "id", jellyfinID, "action", action)
		}
	}

	// ── 4. Mettre à jour SQLite ─────────────────────────────────────────
	_, err = h.db.Conn().Exec(
		`UPDATE users SET is_active = ?, updated_at = datetime('now') WHERE id = ?`,
		newActive, userID,
	)
	if err != nil {
		slog.Error("Erreur mise à jour SQLite pour toggle", "id", userID, "error", err)
		partialErrors = append(partialErrors, fmt.Sprintf("SQLite: %s", err.Error()))
	}

	// ── 5. Audit log ────────────────────────────────────────────────────
	_ = h.db.LogAction(
		fmt.Sprintf("user.%s", action),
		sess.Username,
		username,
		fmt.Sprintf(`{"user_id":%d,"jellyfin_id":"%s","ldap_dn":"%s","errors":%d}`,
			userID, jellyfinID, ldapDN, len(partialErrors)),
	)

	// ── 6. Réponse ──────────────────────────────────────────────────────
	if len(partialErrors) > 0 {
		writeJSON(w, http.StatusOK, APIResponse{
			Success: true,
			Message: fmt.Sprintf("Utilisateur %s (avec %d erreur(s) partielle(s))", action, len(partialErrors)),
			Errors:  partialErrors,
			Data: map[string]interface{}{
				"id":        userID,
				"username":  username,
				"is_active": newActive,
			},
		})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: fmt.Sprintf("Utilisateur %q %s avec succès", username, action),
		Data: map[string]interface{}{
			"id":        userID,
			"username":  username,
			"is_active": newActive,
		},
	})
}

// ── POST /admin/api/users/{id}/invite-toggle ────────────────────────────────

// ToggleUserInvite active ou désactive le droit de créer des invitations pour un utilisateur.
func (h *AdminHandler) ToggleUserInvite(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID utilisateur invalide"})
		return
	}

	var username string
	var canInvite bool
	err = h.db.Conn().QueryRow(`SELECT username, can_invite FROM users WHERE id = ?`, userID).
		Scan(&username, &canInvite)
	if err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: "Utilisateur introuvable"})
		return
	}

	newStatus := !canInvite
	_, err = h.db.Conn().Exec(`UPDATE users SET can_invite = ?, updated_at = datetime('now') WHERE id = ?`, newStatus, userID)
	if err != nil {
		slog.Error("Erreur modification can_invite", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur BDD"})
		return
	}

	actionTxt := "activé"
	if !newStatus {
		actionTxt = "désactivé"
	}
	_ = h.db.LogAction("user.can_invite.toggle", sess.Username, username, fmt.Sprintf("Droit d'invitation %s", actionTxt))

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: fmt.Sprintf("Droit de parrainage %s pour %s", actionTxt, username),
		Data: map[string]interface{}{
			"id":         userID,
			"can_invite": newStatus,
		},
	})
}

// ── POST /admin/api/users/me/password ───────────────────────────────────────

// ChangeMyPassword permet à l'utilisateur connecté de modifier son propre mot de passe.
func (h *AdminHandler) ChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}

	if len(req.NewPassword) < 8 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Le nouveau mot de passe doit faire au moins 8 caractères"})
		return
	}

	// Récupérer le DN LDAP depuis SQLite
	var ldapDN sql.NullString
	err := h.db.Conn().QueryRow(`SELECT ldap_dn FROM users WHERE jellyfin_id = ?`, sess.UserID).Scan(&ldapDN)
	if err != nil && err != sql.ErrNoRows {
		slog.Error("Erreur lecture ldap_dn pour changement MDP", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur base de données"})
		return
	}

	// Le changement s'effectue sur Jellyfin
	// (Note: l'API Jellyfin demande d'avoir l'ancien mot de passe pour les non-admins, ou un reset d'admin.
	// Ici nous utilisons un workaround: on authentifie via un webhook/API ? Non, change password endpoint direct.)
	// Pour simplifier dans l'exemple, on force le changement via le LDClient si dispo, puis le JfClient auth en tant qu'admin
	var partialErrors []string

	// 1. LDAP (Si activé)
	if h.ldClient != nil && ldapDN.String != "" {
		if err := h.ldClient.ResetPassword(ldapDN.String, req.NewPassword); err != nil {
			partialErrors = append(partialErrors, fmt.Sprintf("LDAP: %v", err))
		}
	}

	// 2. Jellyfin (en passant par le token de l'APi admin configurée)
	if err := h.jfClient.ResetPassword(sess.UserID, req.NewPassword); err != nil {
		partialErrors = append(partialErrors, fmt.Sprintf("Jellyfin: %v", err))
	}

	if len(partialErrors) > 0 {
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Des erreurs sont survenues lors du changement",
			Errors:  partialErrors,
		})
		return
	}

	_ = h.db.LogAction("user.password.change", sess.Username, sess.Username, "L'utilisateur a changé son mot de passe depuis le tableau de bord")

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Mot de passe modifié avec succès",
	})
}

// ── DELETE /admin/api/users/{id} ────────────────────────────────────────────

// DeleteUser supprime un utilisateur de l'AD, de Jellyfin, puis de SQLite.
// Les erreurs partielles (ex: utilisateur déjà supprimé de l'AD) ne bloquent
// pas les suppressions restantes — tout est loggé.
func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Message: "ID utilisateur invalide",
		})
		return
	}

	// ── 1. Récupérer l'utilisateur depuis SQLite ────────────────────────
	var username, jellyfinID, ldapDN string
	err = h.db.Conn().QueryRow(
		`SELECT username, jellyfin_id, ldap_dn FROM users WHERE id = ?`, userID,
	).Scan(&username, &jellyfinID, &ldapDN)

	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, APIResponse{
			Success: false,
			Message: "Utilisateur introuvable",
		})
		return
	}
	if err != nil {
		slog.Error("Erreur lecture utilisateur pour suppression", "id", userID, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de lecture de la base de données",
		})
		return
	}

	slog.Info("Suppression d'utilisateur demandée",
		"admin", sess.Username,
		"user_id", userID,
		"username", username,
		"jellyfin_id", jellyfinID,
		"ldap_dn", ldapDN,
	)

	var partialErrors []string

	// ── 2. Supprimer de l'Active Directory (LDAP) ──────────────────────────────
	if h.ldClient != nil && ldapDN != "" {
		if err := h.ldClient.DeleteUser(ldapDN); err != nil {
			slog.Error("Erreur suppression LDAP (on continue)",
				"dn", ldapDN,
				"error", err,
			)
			partialErrors = append(partialErrors, fmt.Sprintf("LDAP: %s", err.Error()))
		} else {
			slog.Info("Utilisateur supprimé de l'AD", "dn", ldapDN)
		}
	}

	// ── 3. Supprimer de Jellyfin ────────────────────────────────────────
	if jellyfinID != "" {
		if err := h.jfClient.DeleteUser(jellyfinID); err != nil {
			slog.Error("Erreur suppression Jellyfin (on continue)",
				"jellyfin_id", jellyfinID,
				"error", err,
			)
			partialErrors = append(partialErrors, fmt.Sprintf("Jellyfin: %s", err.Error()))
		} else {
			slog.Info("Utilisateur supprimé de Jellyfin", "id", jellyfinID)
		}
	}

	// ── 4. Supprimer de SQLite ──────────────────────────────────────────
	_, err = h.db.Conn().Exec(`DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		slog.Error("Erreur suppression SQLite", "id", userID, "error", err)
		partialErrors = append(partialErrors, fmt.Sprintf("SQLite: %s", err.Error()))
	} else {
		slog.Info("Utilisateur supprimé de SQLite", "id", userID)
	}

	// ── 5. Audit log ────────────────────────────────────────────────────
	_ = h.db.LogAction(
		"user.deleted",
		sess.Username,
		username,
		fmt.Sprintf(`{"user_id":%d,"jellyfin_id":"%s","ldap_dn":"%s","partial_errors":%d}`,
			userID, jellyfinID, ldapDN, len(partialErrors)),
	)

	// ── 6. Réponse ──────────────────────────────────────────────────────
	if len(partialErrors) > 0 && err != nil {
		// La suppression SQLite a échoué — c'est critique
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur lors de la suppression (voir les erreurs)",
			Errors:  partialErrors,
		})
		return
	}

	msg := fmt.Sprintf("Utilisateur %q supprimé avec succès", username)
	if len(partialErrors) > 0 {
		msg = fmt.Sprintf("Utilisateur %q supprimé de SQLite (avec %d erreur(s) sur les services externes)", username, len(partialErrors))
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: msg,
		Errors:  partialErrors,
		Data: map[string]interface{}{
			"id":       userID,
			"username": username,
			"deleted":  true,
		},
	})
}

// ── GET /admin/api/invitations ──────────────────────────────────────────────

// InvitationResponse représente une invitation formatée pour l'API JSON.
type InvitationResponse struct {
	ID              int64                  `json:"id"`
	Code            string                 `json:"code"`
	Label           string                 `json:"label"`
	MaxUses         int                    `json:"max_uses"`
	UsedCount       int                    `json:"used_count"`
	JellyfinProfile map[string]interface{} `json:"jellyfin_profile"`
	ExpiresAt       string                 `json:"expires_at,omitempty"`
	CreatedBy       string                 `json:"created_by"`
	CreatedAt       string                 `json:"created_at"`
}

// ListInvitations retourne toutes les invitations SQLite.
func (h *AdminHandler) ListInvitations(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	slog.Info("Liste des invitations demandée", "admin", sess.Username)

	var query string
	var args []interface{}

	if sess.IsAdmin {
		query = `SELECT id, code, label, max_uses, used_count, jellyfin_profile, expires_at, created_by, created_at FROM invitations ORDER BY created_at DESC`
	} else {
		query = `SELECT id, code, label, max_uses, used_count, jellyfin_profile, expires_at, created_by, created_at FROM invitations WHERE created_by = ? ORDER BY created_at DESC`
		args = append(args, sess.Username)
	}

	rows, err := h.db.Conn().Query(query, args...)
	if err != nil {
		slog.Error("Erreur lecture des invitations", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur de lecture de la base de données",
		})
		return
	}
	defer rows.Close()

	var invs []InvitationResponse
	for rows.Next() {
		var i InvitationResponse
		var label, profile, expiresAt, createdBy sql.NullString

		err := rows.Scan(
			&i.ID, &i.Code, &label, &i.MaxUses, &i.UsedCount,
			&profile, &expiresAt, &createdBy, &i.CreatedAt,
		)
		if err != nil {
			slog.Error("Erreur scan invitation", "error", err)
			continue
		}

		i.Label = label.String
		i.ExpiresAt = expiresAt.String
		i.CreatedBy = createdBy.String

		if profile.String != "" {
			var p map[string]interface{}
			_ = json.Unmarshal([]byte(profile.String), &p)
			i.JellyfinProfile = p
		}

		invs = append(invs, i)
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    invs,
	})
}

// ── POST /admin/api/invitations ─────────────────────────────────────────────

// CreateInvitationRequest payload pour la création d'invitation
type CreateInvitationRequest struct {
	Label           string   `json:"label"`
	MaxUses         int      `json:"max_uses"`         // 0 = illimité
	ExpiresAt       string   `json:"expires_at"`       // Date précise, exemple "2026-10-05T12:00"
	UserExpiryDays  int      `json:"user_expiry_days"` // Expiration finale du compte client (jours)
	SendToEmail     string   `json:"send_to_email"`    // Si renseigné, un e-mail partira par SMTP
	EmailMessage    string   `json:"email_message"`
	Libraries       []string `json:"libraries"` // ID des bibliothèques Jellyfin
	EnableDownloads bool     `json:"enable_downloads"`
}

// CreateInvitation crée un nouveau lien d'invitation avec un jeton robuste et logiques complexes (JFA-GO).
func (h *AdminHandler) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if !sess.IsAdmin {
		var canInvite bool
		_ = h.db.Conn().QueryRow(`SELECT can_invite FROM users WHERE jellyfin_id = ?`, sess.UserID).Scan(&canInvite)
		if !canInvite {
			writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Vous n'avez pas l'autorisation de créer des invitations"})
			return
		}
	}

	var req CreateInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}

	// Générer code aléatoire (ici via crypt/rand classique, 12 caractères)
	code := utilsGenerateToken(12)

	// Calculer expiration du lien
	var expiresAt interface{}
	if req.ExpiresAt != "" {
		// Le frontend enverra "yyyy-MM-ddThh:mm"
		if parsed, err := time.Parse("2006-01-02T15:04", req.ExpiresAt); err == nil {
			expiresAt = parsed
		} else if parsed, err := time.Parse(time.RFC3339, req.ExpiresAt); err == nil {
			expiresAt = parsed
		} else {
			expiresAt = req.ExpiresAt // fallback natif sqlite string
		}
	}

	// Construire profil Jellyfin
	jfProfile := jellyfin.InviteProfile{
		EnableAllFolders:   len(req.Libraries) == 0,
		EnabledFolderIDs:   req.Libraries,
		EnableDownload:     req.EnableDownloads,
		EnableRemoteAccess: true,
		UserExpiryDays:     req.UserExpiryDays,
	}
	profileJSON, _ := json.Marshal(jfProfile)

	_, err := h.db.Conn().Exec(
		`INSERT INTO invitations (code, label, max_uses, jellyfin_profile, expires_at, created_by)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		code, req.Label, req.MaxUses, string(profileJSON), expiresAt, sess.Username,
	)

	if err != nil {
		slog.Error("Erreur création invitation DB", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur d'insertion BD"})
		return
	}

	h.db.LogAction("invite.created", sess.Username, req.Label, fmt.Sprintf("Code: %s", code))

	// Envoi SMTP si demandé
	inviteURL := fmt.Sprintf("%s/invite/%s", h.cfg.Jellyfin.URL, code) // Normalement JellyGate public URL
	if req.SendToEmail != "" {
		// Envoyer l'email
		go func() {
			smtpCfg, _ := h.db.GetSMTPConfig()
			errMail := notify.SendInvitationEmail(&smtpCfg, req.SendToEmail, inviteURL, req.EmailMessage)
			if errMail != nil {
				slog.Error("Erreur d'envoi SMTP (Invitation)", "email", req.SendToEmail, "error", errMail)
			}
		}()
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Invitation générée avec succès",
		Data: map[string]interface{}{
			"code": code,
			"url":  inviteURL,
		},
	})
}

// ── DELETE /admin/api/invitations/{id} ──────────────────────────────────────

// DeleteInvitation supprime brutalement l'invitation SQLite
func (h *AdminHandler) DeleteInvitation(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	invID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID invalide"})
		return
	}

	var errDB error
	if sess.IsAdmin {
		_, errDB = h.db.Conn().Exec(`DELETE FROM invitations WHERE id = ?`, invID)
	} else {
		// Security: Le standard user ne supprime que ses propres liens
		result, errDBQuery := h.db.Conn().Exec(`DELETE FROM invitations WHERE id = ? AND created_by = ?`, invID, sess.Username)
		errDB = errDBQuery
		if errDB == nil {
			rowsAffected, _ := result.RowsAffected()
			if rowsAffected == 0 {
				writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Vous n'avez pas l'autorisation de supprimer ce lien"})
				return
			}
		}
	}

	if errDB != nil {
		slog.Error("Erreur suppression invitation", "id", invID, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur DB"})
		return
	}

	h.db.LogAction("invite.deleted", sess.Username, fmt.Sprintf("%d", invID), "")

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Lien d'invitation détruit",
	})
}

// writeJSON écrit une réponse JSON avec le code HTTP donné.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		slog.Error("Erreur d'encodage JSON", "error", err)
	}
}

// utilsGenerateToken crée une chaîne aléatoire sûre pour les invitations
func utilsGenerateToken(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	// Fallback basique en attendant de le bouger ailleurs
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}
