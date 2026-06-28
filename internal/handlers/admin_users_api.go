package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

// SyncJellyfinUsers synchronise manuellement les utilisateurs Jellyfin dans la base locale.
func (h *AdminHandler) SyncJellyfinUsers(w http.ResponseWriter, r *http.Request) {
	jfUsers, err := h.jfClient.GetUsers()
	if err != nil {
		slog.Error("Erreur lors de la récupération des utilisateurs Jellyfin pour la sync", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: h.tr(r, "admin_jf_comm_failed", "Erreur de communication avec Jellyfin"),
		})
		return
	}

	var addedCount int
	for _, ju := range jfUsers {
		// INSERT OR IGNORE dans SQLite
		res, err := h.db.Exec(`
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
	if err := h.db.LogAction("users.sync", session.FromContext(r.Context()).Username, "all",
		fmt.Sprintf("Synchronisation manuelle déclenchée: %d nouveaux utilisateurs importés", addedCount)); err != nil {
		slog.Warn("Erreur journalisation synchronisation utilisateurs", "error", err)
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: fmt.Sprintf(h.tr(r, "admin_sync_finished", "Synchronisation terminée: %d nouveaux utilisateurs trouvés."), addedCount),
	})
}

// ListUsers retourne la liste des utilisateurs avec pagination et recherche.
func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	q := r.URL.Query()

	page := 1
	limit := 25
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		page = p
	}
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 {
		limit = l
	}
	if limit > 500 {
		limit = 500
	}

	search := strings.TrimSpace(q.Get("search"))
	status := q.Get("status")
	invite := q.Get("invite")
	extra := q.Get("extra")
	includeJellyfin := true
	if raw := strings.ToLower(strings.TrimSpace(q.Get("include_jellyfin"))); raw == "0" || raw == "false" || raw == "no" {
		includeJellyfin = false
	}

	whereParts := make([]string, 0)
	args := make([]interface{}, 0)

	if search != "" {
		term := "%" + search + "%"
		whereParts = append(whereParts, "(username LIKE ? OR email LIKE ? OR group_name LIKE ?)")
		args = append(args, term, term, term)
	}

	if status == "active" {
		whereParts = append(whereParts, "is_active = 1 AND is_banned = 0")
	} else if status == "inactive" {
		whereParts = append(whereParts, "is_active = 0 AND is_banned = 0")
	} else if status == "banned" {
		whereParts = append(whereParts, "is_banned = 1")
	}

	if invite == "enabled" {
		whereParts = append(whereParts, "can_invite = 1")
	} else if invite == "disabled" {
		whereParts = append(whereParts, "can_invite = 0")
	}

	if extra == "with-email" {
		whereParts = append(whereParts, "email IS NOT NULL AND email != ''")
	} else if extra == "without-email" {
		whereParts = append(whereParts, "(email IS NULL OR email = '')")
	} else if extra == "expiry-active" {
		whereParts = append(whereParts, "access_expires_at IS NOT NULL AND access_expires_at > CURRENT_TIMESTAMP")
	} else if extra == "expiry-expired" {
		whereParts = append(whereParts, "access_expires_at IS NOT NULL AND access_expires_at <= CURRENT_TIMESTAMP")
	} else if extra == "expiry-none" {
		whereParts = append(whereParts, "access_expires_at IS NULL")
	}

	whereClause := ""
	if len(whereParts) > 0 {
		whereClause = "WHERE " + strings.Join(whereParts, " AND ")
	}

	// 1. Compter le total (filtré)
	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM users %s", whereClause)
	if err := h.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		slog.Error("Erreur comptage des utilisateurs", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "db_error", "Database error")})
		return
	}

	// 1b. Statistiques globales pour l'aperçu
	var totalGlobal, invitersCount, expiringCount int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&totalGlobal); err != nil {
		slog.Error("AdminHandler: erreur comptage total utilisateurs", "error", err)
	}
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM users WHERE can_invite = 1`).Scan(&invitersCount); err != nil {
		slog.Error("AdminHandler: erreur comptage inviters", "error", err)
	}
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_active = 1 AND access_expires_at IS NOT NULL AND access_expires_at > CURRENT_TIMESTAMP`).Scan(&expiringCount); err != nil {
		slog.Error("AdminHandler: erreur comptage utilisateurs expirant", "error", err)
	}

	// 2. Récupérer les données paginées
	offset := (page - 1) * limit
	query := fmt.Sprintf(`SELECT id, jellyfin_id, username, email, ldap_dn, invited_by,
		        group_name, preset_id, is_active, is_banned, can_invite, access_expires_at, delete_at,
		        expiry_action, expiry_delete_after_days, expired_at,
		        profile_apply_status, profile_apply_error, profile_applied_at,
		        created_at, updated_at
		 FROM users %s ORDER BY created_at DESC LIMIT ? OFFSET ?`, whereClause)

	queryArgs := append(args, limit, offset)
	rows, err := h.db.Query(query, queryArgs...)
	if err != nil {
		slog.Error("Erreur lecture des utilisateurs", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: h.tr(r, "db_error", "Database error"),
		})
		return
	}
	defer rows.Close()

	var users []UserResponse
	for rows.Next() {
		var u UserResponse
		var jellyfinID, email, ldapDN, invitedBy, groupName, presetID, profileApplyStatus, profileApplyError sql.NullString
		var accessExpiresAt, deleteAt, expiryAction, expiredAt, createdAt, updatedAt sql.NullString
		var profileAppliedAt sql.NullString
		var deleteAfterDays sql.NullInt64

		err := rows.Scan(
			&u.ID, &jellyfinID, &u.Username, &email, &ldapDN, &invitedBy, &groupName, &presetID,
			&u.IsActive, &u.IsBanned, &u.CanInvite, &accessExpiresAt, &deleteAt,
			&expiryAction, &deleteAfterDays, &expiredAt,
			&profileApplyStatus, &profileApplyError, &profileAppliedAt,
			&createdAt, &updatedAt,
		)
		if err != nil {
			slog.Error("Erreur scan utilisateur", "error", err)
			continue
		}

		u.JellyfinID = jellyfinID.String
		u.Email = email.String
		u.LDAPDN = ldapDN.String
		u.InvitedBy = invitedBy.String
		u.GroupName = groupName.String
		u.PresetID = presetID.String
		u.AccessExpiresAt = accessExpiresAt.String
		u.DeleteAt = deleteAt.String
		u.ExpiryAction = normalizeExpiryAction(expiryAction.String)
		u.ProfileApplyStatus = profileApplyStatus.String
		u.ProfileApplyError = profileApplyError.String
		u.ProfileAppliedAt = profileAppliedAt.String
		if deleteAfterDays.Valid {
			u.DeleteAfterDays = int(deleteAfterDays.Int64)
		}
		u.ExpiredAt = expiredAt.String
		u.CreatedAt = createdAt.String
		u.UpdatedAt = updatedAt.String

		users = append(users, u)
	}

	// 3. Enrichir avec Jellyfin
	if includeJellyfin && h.jfClient != nil && len(users) > 0 {
		jfIDs := make([]string, 0, len(users))
		for _, u := range users {
			if u.JellyfinID != "" {
				jfIDs = append(jfIDs, u.JellyfinID)
			}
		}

		if len(jfIDs) > 0 {
			jfUsers, err := h.jfClient.GetUsersBatch(jfIDs)
			if err == nil {
				jfIndex := make(map[string]*jellyfin.User, len(jfUsers))
				for i := range jfUsers {
					jfIndex[jfUsers[i].ID] = &jfUsers[i]
				}
				for i := range users {
					if jfUser, ok := jfIndex[users[i].JellyfinID]; ok {
						users[i].JellyfinExists = true
						users[i].JellyfinDisabled = jfUser.Policy.IsDisabled
						users[i].JellyfinPrimaryImageTag = jfUser.PrimaryImageTag
					}
				}
			}
		}
	}

	totalPages := 1
	if total > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(limit)))
	}

	slog.Info("Liste des utilisateurs renvoyee", "admin", sess.Username, "count", len(users), "total", total)

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"users": users,
			"meta": map[string]interface{}{
				"total":          total,
				"total_global":   totalGlobal,
				"inviters_count": invitersCount,
				"expiring_count": expiringCount,
				"page":           page,
				"limit":          limit,
				"total_pages":    totalPages,
			},
		},
	})
}

// UserAvatar sert l'image de profil d'un utilisateur Jellyfin.
func (h *AdminHandler) UserAvatar(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || userID <= 0 {
		http.Error(w, h.tr(r, "admin_invalid_id", "ID invalide"), http.StatusBadRequest)
		return
	}

	// 1. Récupérer le JellyfinID depuis la base
	var jfID string
	err = h.db.QueryRow(`SELECT jellyfin_id FROM users WHERE id = ?`, userID).Scan(&jfID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, h.tr(r, "admin_user_not_found", "Utilisateur introuvable"), http.StatusNotFound)
		} else {
			http.Error(w, h.tr(r, "db_error", "Database error"), http.StatusInternalServerError)
		}
		return
	}

	if jfID == "" {
		http.Error(w, h.tr(r, "admin_no_jf_link", "Pas de lien Jellyfin"), http.StatusNotFound)
		return
	}

	// 2. Récupérer l'image via le client Jellyfin
	data, contentType, err := h.jfClient.GetUserImage(jfID)
	if err != nil {
		slog.Warn("Avatar: erreur récupération Jellyfin", "user_id", userID, "jf_id", jfID, "error", err)
		http.Error(w, h.tr(r, "admin_avatar_unavailable", "Image non disponible"), http.StatusNotFound)
		return
	}

	// 3. Servir l'image avec cache
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400") // 24h
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil {
		slog.Warn("Erreur ecriture avatar", "error", err)
	}
}

// UserTimeline retourne l'historique principal d'un utilisateur (audit + jalons internes).
func (h *AdminHandler) UserTimeline(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || userID <= 0 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "admin_invalid_id", "ID utilisateur invalide")})
		return
	}

	rec, err := h.loadAdminUserByID(userID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: h.tr(r, "admin_user_not_found", "Utilisateur introuvable")})
		return
	}
	if err != nil {
		slog.Error("Erreur chargement utilisateur timeline", "user_id", userID, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "db_error", "Database error")})
		return
	}

	idTarget := strconv.FormatInt(rec.ID, 10)
	rows, err := h.db.Query(
		`SELECT action, actor, target, details, created_at
		 FROM audit_log
		 WHERE target = ?
		    OR (? <> '' AND target = ?)
		    OR target = ?
		    OR actor = ?
		 ORDER BY created_at DESC
		 LIMIT 200`,
		rec.Username,
		rec.JellyfinID,
		rec.JellyfinID,
		idTarget,
		rec.Username,
	)
	if err != nil {
		slog.Error("Erreur lecture timeline", "user_id", userID, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "db_error", "Database error")})
		return
	}
	defer rows.Close()

	events := make([]UserTimelineEvent, 0, 32)
	for rows.Next() {
		var action, actor, target, details, createdAt sql.NullString
		if err := rows.Scan(&action, &actor, &target, &details, &createdAt); err != nil {
			continue
		}

		if !isUserTimelineAction(action.String, actor.String, target.String, rec.Username, rec.JellyfinID, idTarget) {
			continue
		}

		events = append(events, UserTimelineEvent{
			At:       normalizeTimelineAt(createdAt.String),
			Action:   action.String,
			Category: timelineCategory(action.String),
			Severity: timelineSeverity(action.String, details.String),
			Actor:    actor.String,
			Target:   target.String,
			Details:  details.String,
			Message:  describeTimelineAction(action.String, actor.String, target.String, details.String),
		})
	}

	sort.Slice(events, func(i, j int) bool {
		return timelineSortKey(events[i].At).After(timelineSortKey(events[j].At))
	})

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: events})
}

func (h *AdminHandler) loadAdminUserByID(userID int64) (*adminUserRecord, error) {
	var rec adminUserRecord
	var email, jellyfinID, ldapDN, groupName, presetID, discordContact, telegramContact, profileApplyStatus, profileApplyError sql.NullString

	err := h.db.QueryRow(
		`SELECT id, username, email, jellyfin_id, ldap_dn, group_name, preset_id, is_active, can_invite,
		        contact_discord, contact_telegram,
		        preferred_lang, notify_expiry_reminder, notify_account_events,
		        opt_in_email, opt_in_discord, opt_in_telegram,
		        expiry_action, expiry_delete_after_days, expired_at,
		        profile_apply_status, profile_apply_error, profile_applied_at,
		        access_expires_at, delete_at, created_at
		 FROM users WHERE id = ?`,
		userID,
	).Scan(
		&rec.ID,
		&rec.Username,
		&email,
		&jellyfinID,
		&ldapDN,
		&groupName,
		&presetID,
		&rec.IsActive,
		&rec.CanInvite,
		&discordContact,
		&telegramContact,
		&rec.PreferredLang,
		&rec.NotifyExpiry,
		&rec.NotifyEvents,
		&rec.OptInEmail,
		&rec.OptInDiscord,
		&rec.OptInTelegram,
		&rec.ExpiryAction,
		&rec.DeleteAfterDays,
		&rec.ExpiredAt,
		&profileApplyStatus,
		&profileApplyError,
		&rec.ProfileAppliedAt,
		&rec.AccessExpiresAt,
		&rec.DeleteAt,
		&rec.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	rec.Email = email.String
	rec.JellyfinID = jellyfinID.String
	rec.LDAPDN = ldapDN.String
	rec.GroupName = groupName.String
	rec.PresetID = presetID.String
	rec.ContactDiscord = discordContact.String
	rec.ContactTelegram = telegramContact.String
	rec.ProfileApplyStatus = profileApplyStatus.String
	rec.ProfileApplyError = profileApplyError.String

	return &rec, nil
}

func parseAccessExpiry(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("date d'expiration vide")
	}

	formats := []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04:05"}
	for _, f := range formats {
		if t, err := time.Parse(f, raw); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("format de date invalide")
}

func isUserTimelineAction(action, actor, target, username, jellyfinID, idTarget string) bool {
	action = strings.TrimSpace(action)
	if action == "" {
		return false
	}

	if strings.HasPrefix(action, "user.") || strings.HasPrefix(action, "invite.") || strings.HasPrefix(action, "reset.") {
		return true
	}

	if strings.HasPrefix(action, "admin.login.") && strings.TrimSpace(actor) == strings.TrimSpace(username) {
		return true
	}

	target = strings.TrimSpace(target)
	if target == strings.TrimSpace(username) || target == strings.TrimSpace(idTarget) {
		return true
	}
	if jellyfinID != "" && target == strings.TrimSpace(jellyfinID) {
		return true
	}

	return false
}

func describeTimelineAction(action, actor, target, details string) string {
	switch action {
	case "user.created":
		return "Compte cree"
	case "user.deleted", "user.bulk.delete":
		return "Compte supprime"
	case "user.toggled":
		return "Statut du compte modifie"
	case "user.access_extended", "user.bulk.expiry":
		return "Expiration du compte mise a jour"
	case "user.profile.updated":
		return "Profil utilisateur mis a jour"
	case "user.password.updated":
		return "Mot de passe modifie"
	case "user.avatar.updated":
		return "Photo de profil modifiee"
	case "invite.created", "invite.created.sponsor":
		return "Lien d'invitation cree"
	case "invite.deleted":
		return "Lien d'invitation supprime"
	case "invite.welcome_email.sent":
		return "Email de bienvenue envoye"
	case "invite.welcome_email.failed":
		return "Echec de l'envoi de l'email de bienvenue"
	case "reset.email.sent":
		return "Email de reinitialisation envoye"
	case "reset.email.failed":
		return "Echec de l'envoi de l'email de reinitialisation"
	case "reset.requested":
		return "Demande de reinitialisation (Email envoye)"
	case "reset.sent.admin":
		return "Lien de reinitialisation envoye par un admin"
	case "user.email_verification.sent":
		return "Email de verification envoye"
	case "user.email_verified":
		return "Email verifie avec succes"
	case "reset.completed", "reset.success":
		return "Mot de passe reinitialise"
	case "invite.used":
		return "Inscription realisee via invitation"
	}

	text := strings.TrimSpace(action)
	if strings.TrimSpace(actor) != "" {
		text = text + " par " + strings.TrimSpace(actor)
	}
	if strings.TrimSpace(target) != "" {
		text = text + " (cible: " + strings.TrimSpace(target) + ")"
	}
	if strings.TrimSpace(details) != "" {
		text = text + " - " + strings.TrimSpace(details)
	}

	return text
}

func timelineCategory(action string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	switch {
	case strings.HasPrefix(action, "invite."):
		return "invitation"
	case strings.HasPrefix(action, "reset."):
		return "password"
	case strings.Contains(action, "email") || strings.Contains(action, "verification"):
		return "email"
	case strings.HasPrefix(action, "admin.login"):
		return "security"
	case strings.HasPrefix(action, "automation."):
		return "automation"
	case strings.HasPrefix(action, "user."):
		return "account"
	default:
		return "general"
	}
}

func timelineSeverity(action, details string) string {
	text := strings.ToLower(strings.TrimSpace(action + " " + details))
	if strings.Contains(text, "failed") || strings.Contains(text, "error") || strings.Contains(text, "echec") {
		return "error"
	}
	if strings.Contains(text, "delete") || strings.Contains(text, "expired") || strings.Contains(text, "disabled") || strings.Contains(text, "banned") {
		return "warning"
	}
	if strings.Contains(text, "created") || strings.Contains(text, "success") || strings.Contains(text, "sent") || strings.Contains(text, "verified") || strings.Contains(text, "updated") {
		return "success"
	}
	return "info"
}

func normalizeTimelineAt(raw string) string {
	t := timelineSortKey(raw)
	if t.IsZero() {
		return strings.TrimSpace(raw)
	}
	return t.Format(time.RFC3339)
}

func timelineSortKey(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}

	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, raw); err == nil {
			return t
		}
	}

	return time.Time{}
}

func (h *AdminHandler) applyJellyfinPolicyPatch(jellyfinID string, patch *BulkJellyfinPolicyPatch) error {
	if patch == nil {
		return fmt.Errorf("aucun patch Jellyfin fourni")
	}
	if jellyfinID == "" {
		return fmt.Errorf("compte Jellyfin absent")
	}

	user, err := h.jfClient.GetUser(jellyfinID)
	if err != nil {
		return fmt.Errorf("impossible de lire la politique Jellyfin: %w", err)
	}

	policy := user.Policy
	if patch.EnableDownloads != nil {
		policy.EnableContentDownloading = *patch.EnableDownloads
	}
	if patch.EnableRemote != nil {
		policy.EnableRemoteAccess = *patch.EnableRemote
	}
	if patch.MaxActiveSession != nil {
		if *patch.MaxActiveSession < 0 {
			return fmt.Errorf("max_active_sessions doit être >= 0")
		}
		policy.MaxActiveSessions = *patch.MaxActiveSession
	}
	if patch.BitrateLimit != nil {
		if *patch.BitrateLimit < 0 {
			return fmt.Errorf("remote_bitrate_limit doit être >= 0")
		}
		policy.RemoteClientBitrateLimit = *patch.BitrateLimit
	}

	if err := h.jfClient.SetUserPolicy(jellyfinID, policy); err != nil {
		return fmt.Errorf("mise à jour de la politique Jellyfin: %w", err)
	}

	return nil
}

func (h *AdminHandler) getJellyfinPresetByID(presetID string) (*config.JellyfinPolicyPreset, error) {
	presetID = strings.TrimSpace(strings.ToLower(presetID))
	if presetID == "" {
		return nil, fmt.Errorf("preset manquant")
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

	return nil, fmt.Errorf("preset introuvable")
}

func inviteProfileFromPolicyPreset(preset *config.JellyfinPolicyPreset) jellyfin.InviteProfile {
	return jellyfin.InviteProfileFromPolicyPreset(preset)
}

func (h *AdminHandler) applyPresetProfileToJellyfin(userID string, preset *config.JellyfinPolicyPreset) error {
	if h.jfClient == nil {
		return fmt.Errorf("client Jellyfin indisponible")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return fmt.Errorf("identifiant Jellyfin manquant")
	}
	return h.jfClient.ApplyInviteProfile(userID, inviteProfileFromPolicyPreset(preset))
}

// applyPresetToUser force les reglages Jellyfin du preset sur un compte existant.
// L'association simple d'un preset a un utilisateur ne doit pas appeler ce helper.
func (h *AdminHandler) applyPresetToUser(rec *adminUserRecord, presetID string) error {
	if rec == nil {
		return nil
	}
	presetID = strings.TrimSpace(presetID)
	if presetID == "" {
		return nil
	}

	preset, err := h.getJellyfinPresetByID(presetID)
	if err != nil {
		return fmt.Errorf("lecture preset %q: %w", presetID, err)
	}

	if rec.JellyfinID != "" {
		if err := h.applyPresetProfileToJellyfin(rec.JellyfinID, preset); err != nil {
			return fmt.Errorf("application preset Jellyfin: %w", err)
		}
	}

	status := "pending"
	appliedAtExpr := "NULL"
	if strings.TrimSpace(rec.JellyfinID) != "" {
		status = "applied"
		appliedAtExpr = "CURRENT_TIMESTAMP"
	}

	// Persister le choix du preset dans SQLite.
	_, err = h.db.Exec(fmt.Sprintf(`UPDATE users
		SET preset_id = ?, profile_apply_status = ?, profile_apply_error = '', profile_applied_at = %s
		WHERE id = ?`, appliedAtExpr), preset.ID, status, rec.ID)
	if err != nil {
		return fmt.Errorf("maj preset_id sqlite: %w", err)
	}

	return nil
}

func (h *AdminHandler) applyGroupMappingToUser(rec *adminUserRecord, groupName string) error {
	if rec == nil {
		return nil
	}
	groupName = strings.TrimSpace(groupName)
	if groupName == "" {
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

	// Le mapping de groupe associe le preset localement. Il ne force pas les
	// droits Jellyfin d'un compte existant; l'action bulk "Forcer un preset"
	// reste le bouton explicite pour cela.
	if strings.TrimSpace(mapping.PolicyPresetID) != "" {
		_, err = h.db.Exec(`UPDATE users SET preset_id = ?, updated_at = datetime('now') WHERE id = ?`, strings.TrimSpace(mapping.PolicyPresetID), rec.ID)
		if err != nil {
			return fmt.Errorf("maj preset_id via mapping: %w", err)
		}
	}

	// Si c'est un groupe LDAP et que LDAP est activé, on ajoute l'utilisateur au groupe LDAP s'il en manque
	if mapping.Source == "ldap" && h.ldClient != nil && strings.TrimSpace(rec.LDAPDN) != "" && strings.TrimSpace(mapping.LDAPGroupDN) != "" {
		if err := h.ldClient.AddUserToGroup(rec.LDAPDN, mapping.LDAPGroupDN); err != nil {
			return fmt.Errorf("assignation groupe ldap: %w", err)
		}
	}

	return nil
}

func (h *AdminHandler) setUserActiveState(rec *adminUserRecord, newActive bool, actor string) ([]string, error) {
	var partialErrors []string

	if h.ldClient != nil && rec.LDAPDN != "" {
		var err error
		if newActive {
			err = h.ldClient.EnableUser(rec.LDAPDN)
		} else {
			err = h.ldClient.DisableUser(rec.LDAPDN)
		}
		if err != nil {
			partialErrors = append(partialErrors, fmt.Sprintf("LDAP: %s", err.Error()))
		}
	}

	if rec.JellyfinID != "" {
		var err error
		if newActive {
			err = h.jfClient.EnableUser(rec.JellyfinID)
		} else {
			err = h.jfClient.DisableUser(rec.JellyfinID)
		}
		if err != nil {
			partialErrors = append(partialErrors, fmt.Sprintf("Jellyfin: %s", err.Error()))
		}
	}

	_, err := h.db.Exec(
		`UPDATE users SET is_active = ?, updated_at = datetime('now') WHERE id = ?`,
		newActive,
		rec.ID,
	)
	if err != nil {
		partialErrors = append(partialErrors, fmt.Sprintf("SQLite: %s", err.Error()))
		return partialErrors, err
	}

	rec.IsActive = newActive
	action := "user.enabled"
	if !newActive {
		action = "user.disabled"
	}
	_ = h.db.LogAction(action, actor, rec.Username, fmt.Sprintf(`{"user_id":%d,"errors":%d}`,
		rec.ID, len(partialErrors)))

	tplKey := "user_enabled"
	if !newActive {
		tplKey = "user_disabled"
	}
	if err := h.sendUserTemplateByKey(rec, tplKey, nil); err != nil {
		partialErrors = append(partialErrors, fmt.Sprintf("Email: %s", err.Error()))
	}

	return partialErrors, nil
}

func (h *AdminHandler) deleteUserRecord(rec *adminUserRecord, actor string) ([]string, error) {
	var partialErrors []string

	if h.ldClient != nil && rec.LDAPDN != "" {
		if err := h.ldClient.DeleteUser(rec.LDAPDN); err != nil {
			partialErrors = append(partialErrors, fmt.Sprintf("LDAP: %s", err.Error()))
		}
	}

	if rec.JellyfinID != "" {
		if err := h.jfClient.DeleteUser(rec.JellyfinID); err != nil {
			partialErrors = append(partialErrors, fmt.Sprintf("Jellyfin: %s", err.Error()))
		}
	}

	if err := h.sendUserTemplateByKey(rec, "user_deleted", nil); err != nil {
		partialErrors = append(partialErrors, fmt.Sprintf("Email: %s", err.Error()))
	}

	_, err := h.db.Exec(`DELETE FROM users WHERE id = ?`, rec.ID)
	if err != nil {
		partialErrors = append(partialErrors, fmt.Sprintf("SQLite: %s", err.Error()))
		return partialErrors, err
	}

	_ = h.db.LogAction("user.deleted", actor, rec.Username, fmt.Sprintf(`{"user_id":%d,"errors":%d}`,
		rec.ID, len(partialErrors)))

	return partialErrors, nil
}

func (h *AdminHandler) sendPasswordResetForUser(rec *adminUserRecord, actor string) error {
	if h.mailer == nil {
		return fmt.Errorf("SMTP non configuré")
	}
	if strings.TrimSpace(rec.Email) == "" {
		return fmt.Errorf("utilisateur sans email")
	}

	token, err := generateSecureToken(resetTokenLength)
	if err != nil {
		return fmt.Errorf("génération du token: %w", err)
	}

	expiresAt := time.Now().Add(resetTokenExpiry)
	_, err = h.db.Exec(
		`INSERT INTO password_resets (user_id, code, used, expires_at)
		 VALUES (?, ?, FALSE, ?)`,
		rec.ID,
		token,
		expiresAt.Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return fmt.Errorf("insertion du token en base: %w", err)
	}

	links := resolvePortalLinks(h.cfg, h.db)
	publicBaseURL := strings.TrimRight(strings.TrimSpace(links.JellyGateURL), "/")
	if publicBaseURL == "" && h.cfg != nil {
		publicBaseURL = strings.TrimRight(strings.TrimSpace(h.cfg.BaseURL), "/")
	}
	resetURL := fmt.Sprintf("%s/reset/%s", publicBaseURL, token)
	mailCfg, usedLang, cfgErr := loadEmailTemplatesForLanguage(h.db, "", emailLanguageContext{
		PreferredLang: rec.PreferredLang,
		GroupName:     rec.GroupName,
	})
	if cfgErr != nil {
		mailCfg = config.DefaultEmailTemplatesForLanguage(usedLang)
	}
	tpl := mailCfg.PasswordReset
	if tpl == "" {
		tpl = "Bonjour {{.Username}},\n\nVoici votre lien de réinitialisation de mot de passe : {{.ResetLink}}"
	}
	subject := firstNonEmpty(mailCfg.PasswordResetSubject, config.DefaultEmailTemplatesForLanguage(usedLang).PasswordResetSubject)

	data := map[string]string{
		"Username":           rec.Username,
		"ResetLink":          resetURL,
		"ResetURL":           resetURL,
		"ResetCode":          token,
		"ExpiresIn":          config.DefaultEmailPreviewDurationForLanguage(usedLang),
		"HelpURL":            publicBaseURL,
		"JellyGateURL":       publicBaseURL,
		"JellyfinURL":        links.JellyfinURL,
		"JellyfinServerName": links.JellyfinServerName,
		"JellyseerrURL":      links.JellyseerrURL,
		"JellyTrackURL":      links.JellyTrackURL,
	}

	if err := sendTemplateIfConfigured(h.mailer, rec.Email, subject, usedLang, "password_reset", tpl, mailCfg, data); err != nil {
		return fmt.Errorf("envoi de l'email: %w", err)
	}

	_ = h.db.LogAction("reset.sent.admin", actor, rec.Username, fmt.Sprintf(`{"user_id":%d}`, rec.ID))
	return nil
}

// CreateUser cree directement un compte utilisateur depuis l'admin.
func (h *AdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if sess == nil || !sess.IsAdmin {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Acces admin requis"})
		return
	}
	if h.jfClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, APIResponse{Success: false, Message: "Jellyfin indisponible"})
		return
	}

	var req CreateAdminUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)
	req.PolicyPresetID = strings.TrimSpace(req.PolicyPresetID)

	if req.Username == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Nom d'utilisateur requis"})
		return
	}
	if req.DisableAfterDays < 0 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Duree d'expiration invalide"})
		return
	}

	var alreadyExists int
	_ = h.db.QueryRow(`SELECT COUNT(1) FROM users WHERE lower(username) = lower(?)`, req.Username).Scan(&alreadyExists)
	if alreadyExists > 0 {
		writeJSON(w, http.StatusConflict, APIResponse{Success: false, Message: "Un utilisateur avec ce nom existe deja"})
		return
	}

	password := strings.TrimSpace(req.Password)
	generatedPassword := ""
	if password == "" {
		token, err := generateSecureToken(18)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible de generer un mot de passe temporaire"})
			return
		}
		password = token
		generatedPassword = token
	}

	inviteCfg, _ := h.db.GetInvitationProfileConfig()
	if req.PolicyPresetID == "" {
		req.PolicyPresetID = strings.TrimSpace(inviteCfg.PolicyPresetID)
	}

	var preset *config.JellyfinPolicyPreset
	if req.PolicyPresetID != "" {
		resolvedPreset, err := h.getJellyfinPresetByID(req.PolicyPresetID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Preset Jellyfin introuvable"})
			return
		}
		preset = resolvedPreset
		req.PolicyPresetID = resolvedPreset.ID
	}

	effectiveDisableAfterDays := req.DisableAfterDays
	if effectiveDisableAfterDays <= 0 && preset != nil && preset.DisableAfterDays > 0 {
		effectiveDisableAfterDays = preset.DisableAfterDays
	}

	var expiryAt time.Time
	if strings.TrimSpace(req.AccessExpiresAt) != "" {
		parsedExpiry, err := parseAccessExpiry(strings.TrimSpace(req.AccessExpiresAt))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Date d'expiration invalide"})
			return
		}
		expiryAt = parsedExpiry
	} else if effectiveDisableAfterDays > 0 {
		expiryAt = time.Now().AddDate(0, 0, effectiveDisableAfterDays)
	}

	created, err := h.jfClient.CreateUser(req.Username, password)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Creation Jellyfin echouee: " + err.Error()})
		return
	}

	if preset != nil {
		if err := h.applyPresetProfileToJellyfin(created.ID, preset); err != nil {
			_ = h.jfClient.DeleteUser(created.ID)
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Application du preset impossible: " + err.Error()})
			return
		}
	}

	effectiveCanInvite := req.CanInvite
	if preset != nil && preset.CanInvite {
		effectiveCanInvite = true
	}

	storedExpiry := ""
	var expiryValue interface{}
	if !expiryAt.IsZero() {
		storedExpiry = expiryAt.Format("2006-01-02 15:04:05")
		expiryValue = storedExpiry
	}

	expiryAction := normalizeExpiryAction(inviteCfg.ExpiryAction)
	deleteAfterDays := inviteCfg.DeleteAfterDays
	if preset != nil {
		expiryAction = normalizeExpiryAction(preset.ExpiryAction)
		if preset.DeleteAfterDays >= 0 {
			deleteAfterDays = preset.DeleteAfterDays
		}
	}

	emailVerified := strings.TrimSpace(req.Email) == ""
	if _, err := h.db.Exec(
		`INSERT INTO users
			(jellyfin_id, username, email, email_verified, invited_by, is_active, can_invite, access_expires_at, preset_id, expiry_action, expiry_delete_after_days, profile_apply_status, profile_apply_error, profile_applied_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, '', ?, datetime('now'), datetime('now'))`,
		created.ID,
		req.Username,
		req.Email,
		emailVerified,
		"admin:"+sess.Username,
		effectiveCanInvite,
		expiryValue,
		req.PolicyPresetID,
		expiryAction,
		deleteAfterDays,
		map[bool]string{true: "applied", false: "pending"}[preset != nil],
		map[bool]interface{}{true: time.Now(), false: nil}[preset != nil],
	); err != nil {
		_ = h.jfClient.DeleteUser(created.ID)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible d'enregistrer l'utilisateur"})
		return
	}

	var createdID int64
	_ = h.db.QueryRow(`SELECT id FROM users WHERE jellyfin_id = ?`, created.ID).Scan(&createdID)
	rec := &adminUserRecord{ID: createdID, Username: req.Username, Email: req.Email, JellyfinID: created.ID, CanInvite: effectiveCanInvite}
	if storedExpiry != "" {
		rec.AccessExpiresAt = sql.NullString{String: storedExpiry, Valid: true}
	}

	welcomeSent := false
	if req.SendWelcomeEmail && strings.TrimSpace(req.Email) != "" && h.mailer != nil {
		usedLang := h.db.GetDefaultLang()
		emailCfg, _, cfgErr := h.db.GetEmailTemplatesConfigForLang(usedLang)
		if cfgErr == nil && !emailCfg.DisableWelcomeEmail {
			defaults := config.DefaultEmailTemplatesForLanguage(usedLang)
			subject := firstNonEmpty(emailCfg.WelcomeSubject, defaults.WelcomeSubject)
			body := emailCfg.Welcome
			if strings.TrimSpace(body) == "" {
				body = defaults.Welcome
			}
			extra := map[string]string{}
			if !expiryAt.IsZero() {
				extra["ExpiryDate"] = emailTime(expiryAt)
			}
			if err := h.sendUserEventEmail(rec, subject, usedLang, "welcome", body, emailCfg, extra); err == nil {
				welcomeSent = true
			}
		}
	}

	_ = h.db.LogAction("user.created.admin", sess.Username, req.Username, fmt.Sprintf(`{"user_id":%d,"preset_id":"%s","can_invite":%t}`, createdID, req.PolicyPresetID, effectiveCanInvite))

	respData := map[string]interface{}{
		"id":                createdID,
		"username":          req.Username,
		"email":             req.Email,
		"jellyfin_id":       created.ID,
		"preset_id":         req.PolicyPresetID,
		"can_invite":        effectiveCanInvite,
		"access_expires_at": storedExpiry,
		"welcome_sent":      welcomeSent,
	}
	if generatedPassword != "" {
		respData["temporary_password"] = generatedPassword
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Utilisateur cree", Data: respData})
}

// UpdateUser met à jour les informations éditables d'un utilisateur (email, parrainage, expiration).
func (h *AdminHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID utilisateur invalide"})
		return
	}

	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}

	rec, err := h.loadAdminUserByID(userID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: "Utilisateur introuvable"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de lecture utilisateur"})
		return
	}

	email := rec.Email
	if req.Email != nil {
		email = strings.TrimSpace(*req.Email)
	}

	canInvite := rec.CanInvite
	if req.CanInvite != nil {
		canInvite = *req.CanInvite
	}

	groupName := strings.TrimSpace(rec.GroupName)
	if req.GroupName != nil {
		groupName = strings.TrimSpace(*req.GroupName)
	}

	presetID := strings.TrimSpace(rec.PresetID)
	if req.PresetID != nil {
		presetID = strings.TrimSpace(*req.PresetID)
	}

	oldExpiry := ""
	if rec.AccessExpiresAt.Valid {
		oldExpiry = strings.TrimSpace(rec.AccessExpiresAt.String)
	}

	newExpiry := oldExpiry
	var accessExpiresAt interface{}
	if req.ClearExpiry {
		accessExpiresAt = nil
		newExpiry = ""
	} else if req.AccessExpiresAt != nil {
		raw := strings.TrimSpace(*req.AccessExpiresAt)
		if raw == "" {
			accessExpiresAt = nil
			newExpiry = ""
		} else {
			exp, err := parseAccessExpiry(raw)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Date d'expiration invalide"})
				return
			}
			accessExpiresAt = exp
			newExpiry = exp.Format("2006-01-02 15:04:05")
		}
	} else if rec.AccessExpiresAt.Valid {
		accessExpiresAt = rec.AccessExpiresAt.String
		newExpiry = strings.TrimSpace(rec.AccessExpiresAt.String)
	}

	_, err = h.db.Exec(
		`UPDATE users
		 SET email = ?, group_name = ?, preset_id = ?, can_invite = ?, access_expires_at = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		email,
		groupName,
		presetID,
		canInvite,
		accessExpiresAt,
		userID,
	)
	if err != nil {
		slog.Error("Erreur mise à jour utilisateur", "id", userID, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de mise à jour"})
		return
	}

	_ = h.db.LogAction("user.updated", sess.Username, rec.Username,
		fmt.Sprintf(`{"user_id":%d,"email":"%s","group_name":"%s","preset_id":"%s","can_invite":%t}`, userID, email, groupName, presetID, canInvite))

	// Le preset_id est une association locale. Pour forcer Jellyfin, utiliser
	// l'action explicite "Forcer un preset Jellyfin" depuis les actions bulk.
	if req.GroupName != nil && groupName != "" {
		rec.GroupName = groupName
		if err := h.applyGroupMappingToUser(rec, groupName); err != nil {
			slog.Warn("Application mapping groupe echouee", "user", rec.Username, "group", groupName, "error", err)
			_ = h.db.LogAction("user.group_mapping.failed", sess.Username, rec.Username, err.Error())
		}
	}

	if oldExpiry != newExpiry {
		rec.Email = email
		rec.AccessExpiresAt = sql.NullString{String: newExpiry, Valid: newExpiry != ""}
		if newExpiry != "" {
			if err := h.sendUserTemplateByKey(rec, "expiry_adjusted", map[string]string{"ExpiryDate": newExpiry}); err != nil {
				slog.Error("Erreur envoi email expiry_adjusted", "user", rec.Username, "error", err)
			}
		}
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Utilisateur mis à jour",
		Data: map[string]interface{}{
			"id":                userID,
			"email":             email,
			"group_name":        groupName,
			"can_invite":        canInvite,
			"access_expires_at": accessExpiresAt,
		},
	})
}

// BanUser banni définitvement un utilisateur (désactivation + flag banni).
func (h *AdminHandler) BanUser(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID utilisateur invalide"})
		return
	}

	rec, err := h.loadAdminUserByID(userID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: "Utilisateur introuvable"})
		return
	}

	_, err = h.db.Exec(`UPDATE users SET is_active = 0, is_banned = 1, updated_at = datetime('now') WHERE id = ?`, userID)
	if err != nil {
		slog.Error("Erreur bannissement utilisateur", "id", userID, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de mise à jour"})
		return
	}

	_ = h.db.LogAction("user.banned", sess.Username, rec.Username, fmt.Sprintf(`{"user_id":%d}`, userID))

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: fmt.Sprintf("Utilisateur %s banni", rec.Username)})
}

// ExtendAccess ajoute une durée d'accès par défaut (30 jours) à l'utilisateur.
func (h *AdminHandler) ExtendAccess(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID utilisateur invalide"})
		return
	}

	rec, err := h.loadAdminUserByID(userID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: "Utilisateur introuvable"})
		return
	}

	currentExpiry := time.Now()
	if rec.AccessExpiresAt.Valid {
		if t, err := time.Parse("2006-01-02 15:04:05", rec.AccessExpiresAt.String); err == nil {
			if t.After(currentExpiry) {
				currentExpiry = t
			}
		}
	}

	newExpiry := currentExpiry.AddDate(0, 0, 30)
	newExpiryStr := newExpiry.Format("2006-01-02 15:04:05")

	_, err = h.db.Exec(`UPDATE users SET access_expires_at = ?, updated_at = datetime('now') WHERE id = ?`, newExpiryStr, userID)
	if err != nil {
		slog.Error("Erreur prolongation utilisateur", "id", userID, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de mise à jour"})
		return
	}

	_ = h.db.LogAction("user.access_extended", sess.Username, rec.Username, fmt.Sprintf(`{"user_id":%d,"new_expiry":"%s"}`, userID, newExpiryStr))

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: fmt.Sprintf("Accès prolongé pour %s jusqu'au %s", rec.Username, newExpiry.Format("02/01/2006")),
		Data: map[string]interface{}{
			"id":                userID,
			"access_expires_at": newExpiryStr,
		},
	})
}

// SendUserPasswordReset crée et envoie un lien de réinitialisation à l'utilisateur ciblé.
func (h *AdminHandler) SendUserPasswordReset(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID utilisateur invalide"})
		return
	}

	rec, err := h.loadAdminUserByID(userID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: "Utilisateur introuvable"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de lecture utilisateur"})
		return
	}

	if err := h.sendPasswordResetForUser(rec, sess.Username); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: h.tr(r, "admin_password_reset_sent", "Password reset link sent")})
}

// BulkUsersAction applique une action de masse sur les utilisateurs sélectionnés.
func (h *AdminHandler) BulkUsersAction(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())

	var req BulkUsersActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}

	if len(req.UserIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Aucun utilisateur sélectionné"})
		return
	}

	action := strings.TrimSpace(req.Action)
	if action == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Action manquante"})
		return
	}
	previewOnly := req.Preview

	results := make([]map[string]interface{}, 0, len(req.UserIDs))
	successCount := 0

	for _, userID := range req.UserIDs {
		rec, err := h.loadAdminUserByID(userID)
		if err != nil {
			results = append(results, map[string]interface{}{
				"id":      userID,
				"success": false,
				"message": "Utilisateur introuvable",
			})
			continue
		}

		entry := map[string]interface{}{
			"id":       rec.ID,
			"username": rec.Username,
		}

		switch action {
		case "send_email":
			if previewOnly {
				subject := strings.TrimSpace(req.EmailSubject)
				body := strings.TrimSpace(req.EmailBody)
				if h.mailer == nil {
					entry["success"] = false
					entry["message"] = "SMTP non configure"
					break
				}
				if subject == "" || body == "" {
					entry["success"] = false
					entry["message"] = "Sujet/corps email requis"
					break
				}
				if strings.TrimSpace(rec.Email) == "" {
					entry["success"] = false
					entry["message"] = "Utilisateur sans email"
					break
				}
				entry["success"] = true
				entry["message"] = "Email sera envoye"
				entry["preview"] = true
				entry["impact"] = map[string]interface{}{"channel": "email", "subject": subject}
				break
			}

			if h.mailer == nil {
				entry["success"] = false
				entry["message"] = "SMTP non configuré"
				break
			}
			subject := strings.TrimSpace(req.EmailSubject)
			body := strings.TrimSpace(req.EmailBody)
			if subject == "" || body == "" {
				entry["success"] = false
				entry["message"] = "Sujet/corps email requis"
				break
			}
			if strings.TrimSpace(rec.Email) == "" {
				entry["success"] = false
				entry["message"] = "Utilisateur sans email"
				break
			}

			err := h.mailer.SendTemplateString(rec.Email, subject, body, map[string]string{
				"Username": rec.Username,
				"Email":    rec.Email,
				"Actor":    sess.Username,
			})
			if err != nil {
				entry["success"] = false
				entry["message"] = err.Error()
				break
			}

			_ = h.db.LogAction("user.bulk.email", sess.Username, rec.Username, subject)
			entry["success"] = true
			entry["message"] = "Email envoyé"

		case "jellyfin_policy":
			if previewOnly {
				if req.JellyfinPolicy == nil {
					entry["success"] = false
					entry["message"] = "Parametres Jellyfin manquants"
					break
				}
				entry["success"] = true
				entry["message"] = "Parametres Jellyfin seront mis a jour"
				entry["preview"] = true
				entry["impact"] = map[string]interface{}{"jellyfin_id": rec.JellyfinID}
				break
			}

			err := h.applyJellyfinPolicyPatch(rec.JellyfinID, req.JellyfinPolicy)
			if err != nil {
				entry["success"] = false
				entry["message"] = err.Error()
				break
			}

			_ = h.db.LogAction("user.bulk.jellyfin_policy", sess.Username, rec.Username, fmt.Sprintf(`{"user_id":%d}`, rec.ID))
			entry["success"] = true
			entry["message"] = "Paramètres Jellyfin mis à jour"

		case "apply_preset":
			preset, err := h.getJellyfinPresetByID(req.PolicyPresetID)
			if err != nil {
				entry["success"] = false
				entry["message"] = err.Error()
				break
			}

			if previewOnly {
				entry["success"] = true
				entry["message"] = "Preset Jellyfin sera force avec droits, accueil et affichage"
				entry["preview"] = true
				entry["impact"] = map[string]interface{}{"preset_id": preset.ID, "preset_name": preset.Name}
				break
			}

			err = h.applyPresetToUser(rec, preset.ID)
			if err != nil {
				entry["success"] = false
				entry["message"] = err.Error()
				break
			}

			_ = h.db.LogAction("user.bulk.apply_preset", sess.Username, rec.Username, preset.ID)
			entry["success"] = true
			entry["message"] = "Preset Jellyfin force avec droits, accueil et affichage"

		case "set_parrainage":
			if req.CanInvite == nil {
				entry["success"] = false
				entry["message"] = "can_invite manquant"
				break
			}

			if previewOnly {
				entry["success"] = true
				entry["message"] = "Parrainage sera mis a jour"
				entry["preview"] = true
				entry["impact"] = map[string]interface{}{"can_invite": *req.CanInvite}
				break
			}

			_, err := h.db.Exec(`UPDATE users SET can_invite = ?, updated_at = datetime('now') WHERE id = ?`, *req.CanInvite, rec.ID)
			if err != nil {
				entry["success"] = false
				entry["message"] = "Erreur SQLite"
				break
			}

			_ = h.db.LogAction("user.bulk.can_invite", sess.Username, rec.Username, fmt.Sprintf(`{"can_invite":%t}`, *req.CanInvite))
			entry["success"] = true
			entry["message"] = "Parrainage mis à jour"

		case "set_expiry":
			var expiry interface{}
			if req.ClearExpiry {
				expiry = nil
			} else {
				if req.AccessExpiresAt == nil || strings.TrimSpace(*req.AccessExpiresAt) == "" {
					entry["success"] = false
					entry["message"] = "Date d'expiration manquante"
					break
				}
				exp, err := parseAccessExpiry(*req.AccessExpiresAt)
				if err != nil {
					entry["success"] = false
					entry["message"] = "Date d'expiration invalide"
					break
				}
				expiry = exp
			}

			if previewOnly {
				entry["success"] = true
				if req.ClearExpiry {
					entry["message"] = "Expiration sera supprimee"
					entry["impact"] = map[string]interface{}{"clear_expiry": true}
				} else {
					displayExpiry := ""
					if req.AccessExpiresAt != nil {
						displayExpiry = strings.TrimSpace(*req.AccessExpiresAt)
					}
					entry["message"] = "Expiration sera mise a jour"
					entry["impact"] = map[string]interface{}{"clear_expiry": false, "access_expires_at": displayExpiry}
				}
				entry["preview"] = true
				break
			}

			_, err := h.db.Exec(`UPDATE users SET access_expires_at = ?, updated_at = datetime('now') WHERE id = ?`, expiry, rec.ID)
			if err != nil {
				entry["success"] = false
				entry["message"] = "Erreur SQLite"
				break
			}

			_ = h.db.LogAction("user.bulk.expiry", sess.Username, rec.Username, "")
			if !req.ClearExpiry && req.AccessExpiresAt != nil && strings.TrimSpace(*req.AccessExpiresAt) != "" {
				if err := h.sendUserTemplateByKey(rec, "expiry_adjusted", map[string]string{"ExpiryDate": strings.TrimSpace(*req.AccessExpiresAt)}); err != nil {
					slog.Error("Erreur envoi email bulk expiry_adjusted", "user", rec.Username, "error", err)
					entry["success"] = true
					entry["message"] = "Expiration mise à jour (email non envoyé)"
					break
				}
			}
			entry["success"] = true
			entry["message"] = "Expiration mise à jour"

		case "activate", "deactivate":
			newState := action == "activate"
			if previewOnly {
				entry["success"] = true
				if newState {
					entry["message"] = "Utilisateur sera active"
				} else {
					entry["message"] = "Utilisateur sera desactive"
				}
				entry["preview"] = true
				entry["impact"] = map[string]interface{}{"is_active": newState}
				break
			}

			partials, err := h.setUserActiveState(rec, newState, sess.Username)
			if err != nil {
				entry["success"] = false
				entry["message"] = strings.Join(partials, " | ")
				break
			}

			entry["success"] = true
			if len(partials) > 0 {
				entry["message"] = "Action appliquée avec erreurs partielles: " + strings.Join(partials, " | ")
			} else if newState {
				entry["message"] = "Utilisateur activé"
			} else {
				entry["message"] = "Utilisateur désactivé"
			}

		case "send_password_reset":
			if previewOnly {
				if strings.TrimSpace(rec.Email) == "" {
					entry["success"] = false
					entry["message"] = "Utilisateur sans email"
					break
				}
				entry["success"] = true
				entry["message"] = "Lien de reinitialisation sera envoye"
				entry["preview"] = true
				entry["impact"] = map[string]interface{}{"channel": "email"}
				break
			}

			err := h.sendPasswordResetForUser(rec, sess.Username)
			if err != nil {
				entry["success"] = false
				entry["message"] = err.Error()
				break
			}

			entry["success"] = true
			entry["message"] = "Lien de réinitialisation envoyé"

		case "delete":
			if previewOnly {
				entry["success"] = true
				entry["message"] = "Utilisateur sera supprime"
				entry["preview"] = true
				entry["impact"] = map[string]interface{}{"delete": true}
				break
			}

			partials, err := h.deleteUserRecord(rec, sess.Username)
			if err != nil {
				entry["success"] = false
				entry["message"] = strings.Join(partials, " | ")
				break
			}

			entry["success"] = true
			if len(partials) > 0 {
				entry["message"] = "Supprimé avec erreurs partielles: " + strings.Join(partials, " | ")
			} else {
				entry["message"] = "Utilisateur supprimé"
			}

		default:
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Action de masse non supportée"})
			return
		}

		if ok, _ := entry["success"].(bool); ok {
			successCount++
		}
		results = append(results, entry)
	}

	message := fmt.Sprintf("Action de masse terminée: %d/%d succès", successCount, len(req.UserIDs))
	if previewOnly {
		message = fmt.Sprintf("Preview action de masse: %d/%d impact(s) valides", successCount, len(req.UserIDs))
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: message,
		Data: map[string]interface{}{
			"total":   len(req.UserIDs),
			"success": successCount,
			"preview": previewOnly,
			"results": results,
		},
	})
}

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

	rec, err := h.loadAdminUserByID(userID)
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

	newActive := !rec.IsActive
	partialErrors, err := h.setUserActiveState(rec, newActive, sess.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur lors de la mise à jour du statut utilisateur",
			Errors:  partialErrors,
			Data: map[string]interface{}{
				"id":        rec.ID,
				"username":  rec.Username,
				"is_active": rec.IsActive,
			},
		})
		return
	}

	action := "activé"
	if !newActive {
		action = "désactivé"
	}
	if len(partialErrors) > 0 {
		writeJSON(w, http.StatusOK, APIResponse{
			Success: true,
			Message: fmt.Sprintf("Utilisateur %s (avec %d erreur(s) partielle(s))", action, len(partialErrors)),
			Errors:  partialErrors,
			Data: map[string]interface{}{
				"id":        rec.ID,
				"username":  rec.Username,
				"is_active": newActive,
			},
		})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: fmt.Sprintf("Utilisateur %q %s avec succès", rec.Username, action),
		Data: map[string]interface{}{
			"id":        rec.ID,
			"username":  rec.Username,
			"is_active": newActive,
		},
	})
}

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
	err = h.db.QueryRow(`SELECT username, can_invite FROM users WHERE id = ?`, userID).
		Scan(&username, &canInvite)
	if err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: "Utilisateur introuvable"})
		return
	}

	newStatus := !canInvite
	_, err = h.db.Exec(`UPDATE users SET can_invite = ?, updated_at = datetime('now') WHERE id = ?`, newStatus, userID)
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

	rec, err := h.loadAdminUserByID(userID)
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

	partialErrors, err := h.deleteUserRecord(rec, sess.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur lors de la suppression de l'utilisateur",
			Errors:  partialErrors,
			Data: map[string]interface{}{
				"id":       rec.ID,
				"username": rec.Username,
				"deleted":  false,
			},
		})
		return
	}

	msg := fmt.Sprintf("Utilisateur %q supprimé avec succès", rec.Username)
	if len(partialErrors) > 0 {
		msg = fmt.Sprintf("Utilisateur %q supprimé avec %d erreur(s) partielle(s)", rec.Username, len(partialErrors))
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: msg,
		Errors:  partialErrors,
		Data: map[string]interface{}{
			"id":       rec.ID,
			"username": rec.Username,
			"deleted":  true,
		},
	})
}
