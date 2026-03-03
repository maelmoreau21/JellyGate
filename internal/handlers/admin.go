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
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	jgldap "github.com/maelmoreau21/JellyGate/internal/ldap"
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
}

// NewAdminHandler crée un nouveau handler d'administration.
func NewAdminHandler(cfg *config.Config, db *database.DB, jf *jellyfin.Client, ld *jgldap.Client) *AdminHandler {
	return &AdminHandler{
		cfg:      cfg,
		db:       db,
		jfClient: jf,
		ldClient: ld,
	}
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
		        is_active, is_banned, access_expires_at, created_at, updated_at
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
			&u.IsActive, &u.IsBanned, &accessExpiresAt, &createdAt, &updatedAt,
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

	// ── 2. Modifier dans Synology AD (LDAP) ─────────────────────────────
	if ldapDN != "" {
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

	// ── 2. Supprimer de Synology AD (LDAP) ──────────────────────────────
	if ldapDN != "" {
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

// ── Utilitaires JSON ────────────────────────────────────────────────────────

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
