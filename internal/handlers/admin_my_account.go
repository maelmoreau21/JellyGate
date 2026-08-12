package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	netmail "net/mail"
	"strings"

	"github.com/maelmoreau21/JellyGate/internal/config"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

// GetMyAccount retourne les infos de l'utilisateur connecté.
func (h *AdminHandler) GetMyAccount(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if err := h.ensureUserRowForSession(sess); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "admin_profile_load_failed", "Impossible de préparer le profil utilisateur")})
		return
	}

	var (
		id              int64
		email           sql.NullString
		pendingEmail    sql.NullString
		emailVerified   bool
		contactDiscord  sql.NullString
		contactTelegram sql.NullString
		contactMatrix   sql.NullString
		preferredLang   string
		notifyExpiry    bool
		notifyEvents    bool
		optInEmail      bool
		optInDiscord    bool
		optInTelegram   bool
		optInMatrix     bool
		accessExpiresAt sql.NullString
		createdAt       sql.NullString
	)

	err := h.db.QueryRow(
		`SELECT id, email, contact_discord, contact_telegram, contact_matrix,
		        pending_email, email_verified,
		        preferred_lang, notify_expiry_reminder, notify_account_events,
		        opt_in_email, opt_in_discord, opt_in_telegram, opt_in_matrix,
		        access_expires_at, created_at
		 FROM users WHERE authentik_id = ? OR username = ? OR id = ?`,
		sess.AuthentikID, sess.Username, sess.UserID,
	).Scan(
		&id,
		&email,
		&contactDiscord,
		&contactTelegram,
		&contactMatrix,
		&pendingEmail,
		&emailVerified,
		&preferredLang,
		&notifyExpiry,
		&notifyEvents,
		&optInEmail,
		&optInDiscord,
		&optInTelegram,
		&optInMatrix,
		&accessExpiresAt,
		&createdAt,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "admin_profile_read_failed", "Erreur de lecture du profil")})
		return
	}

	if preferredLang == "" {
		preferredLang = h.db.GetDefaultLang()
	}

	var jfPrimaryImageTag string
	if h.jfClient != nil {
		if jfUser, err := h.jfClient.GetUser(sess.UserID); err == nil && jfUser != nil {
			jfPrimaryImageTag = jfUser.PrimaryImageTag
		}
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"id":                         id,
			"username":                   sess.Username,
			"jellyfin_primary_image_tag": jfPrimaryImageTag,
			"email":                      email.String,
			"pending_email":              pendingEmail.String,
			"email_verified":             emailVerified,
			"contact_discord":            contactDiscord.String,
			"contact_telegram":           contactTelegram.String,
			"contact_matrix":             contactMatrix.String,
			"preferred_lang":             preferredLang,
			"notify_expiry_reminder":     notifyExpiry,
			"notify_account_events":      notifyEvents,
			"opt_in_email":               optInEmail,
			"opt_in_discord":             optInDiscord,
			"opt_in_telegram":            optInTelegram,
			"opt_in_matrix":              optInMatrix,
			"is_admin":                   sess.IsAdmin,
			"access_expires_at":          accessExpiresAt.String,
			"can_invite":                 h.resolveCanInviteForSession(sess),
			"created_at":                 createdAt.String,
		},
	})
}

// UpdateMyAccount met à jour les préférences et l'email de l'utilisateur connecté.
func (h *AdminHandler) UpdateMyAccount(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if err := h.ensureUserRowForSession(sess); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "admin_profile_load_failed", "Impossible de préparer le profil utilisateur")})
		return
	}

	var req UpdateMyAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "admin_invalid_json", "Payload JSON invalide")})
		return
	}

	var (
		userID          int64
		currentEmail    sql.NullString
		currentPending  sql.NullString
		emailVerified   bool
		currentDiscord  sql.NullString
		currentTelegram sql.NullString
		currentMatrix   sql.NullString
		preferredLang   string
		notifyExpiry    bool
		notifyEvents    bool
		optInEmail      bool
		optInDiscord    bool
		optInTelegram   bool
		optInMatrix     bool
	)
	err := h.db.QueryRow(
		`SELECT id, email, pending_email, email_verified, contact_discord, contact_telegram, contact_matrix,
		        preferred_lang, notify_expiry_reminder, notify_account_events,
		        opt_in_email, opt_in_discord, opt_in_telegram, opt_in_matrix
		 FROM users WHERE authentik_id = ? OR username = ? OR id = ?`,
		sess.AuthentikID, sess.Username, sess.UserID,
	).Scan(
		&userID,
		&currentEmail,
		&currentPending,
		&emailVerified,
		&currentDiscord,
		&currentTelegram,
		&currentMatrix,
		&preferredLang,
		&notifyExpiry,
		&notifyEvents,
		&optInEmail,
		&optInDiscord,
		&optInTelegram,
		&optInMatrix,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "admin_profile_read_failed", "Erreur de lecture des préférences")})
		return
	}

	newEmail := strings.TrimSpace(currentEmail.String)
	newPendingEmail := strings.TrimSpace(currentPending.String)
	newEmailVerified := emailVerified
	if req.Email != nil {
		requestedEmail := strings.TrimSpace(*req.Email)
		if requestedEmail != "" {
			if _, err := netmail.ParseAddress(requestedEmail); err != nil {
				writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "admin_invalid_email", "Adresse email invalide")})
				return
			}
		}

		switch {
		case requestedEmail == "":
			newEmail = ""
			newPendingEmail = ""
			newEmailVerified = false
		case strings.EqualFold(requestedEmail, newEmail):
			newPendingEmail = ""
		default:
			newPendingEmail = requestedEmail
		}
	}
	newDiscord := strings.TrimSpace(currentDiscord.String)
	if req.ContactDiscord != nil {
		newDiscord = strings.TrimSpace(*req.ContactDiscord)
	}
	newTelegram := strings.TrimSpace(currentTelegram.String)
	if req.ContactTelegram != nil {
		newTelegram = strings.TrimSpace(*req.ContactTelegram)
	}
	newMatrix := strings.TrimSpace(currentMatrix.String)
	if req.ContactMatrix != nil {
		newMatrix = strings.TrimSpace(*req.ContactMatrix)
	}

	newPreferredLang := strings.TrimSpace(preferredLang)
	if req.PreferredLang != nil {
		candidate := config.NormalizeLanguageTag(*req.PreferredLang)
		if candidate != "" && !config.IsSupportedLanguage(candidate) {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "admin_invalid_lang", "Langue invalide")})
			return
		}
		newPreferredLang = candidate
	}

	newNotifyExpiry := notifyExpiry
	if req.NotifyExpiryReminder != nil {
		newNotifyExpiry = *req.NotifyExpiryReminder
	}

	newNotifyEvents := notifyEvents
	if req.NotifyAccountEvents != nil {
		newNotifyEvents = *req.NotifyAccountEvents
	}

	newOptInEmail := optInEmail
	if req.OptInEmail != nil {
		newOptInEmail = *req.OptInEmail
	}
	newOptInDiscord := optInDiscord
	if req.OptInDiscord != nil {
		newOptInDiscord = *req.OptInDiscord
	}
	newOptInTelegram := optInTelegram
	if req.OptInTelegram != nil {
		newOptInTelegram = *req.OptInTelegram
	}
	newOptInMatrix := optInMatrix
	if req.OptInMatrix != nil {
		newOptInMatrix = *req.OptInMatrix
	}

	_, err = h.db.Exec(
		`UPDATE users
		 SET email = ?, pending_email = ?, email_verified = ?, contact_discord = ?, contact_telegram = ?, contact_matrix = ?,
		     preferred_lang = ?, notify_expiry_reminder = ?, notify_account_events = ?,
		     opt_in_email = ?, opt_in_discord = ?, opt_in_telegram = ?, opt_in_matrix = ?,
		     email_verification_sent_at = CASE WHEN ? THEN NULL ELSE email_verification_sent_at END,
		     updated_at = datetime('now')
		 WHERE id = ? OR authentik_id = ? OR username = ?`,
		newEmail,
		newPendingEmail,
		newEmailVerified,
		newDiscord,
		newTelegram,
		newMatrix,
		newPreferredLang,
		newNotifyExpiry,
		newNotifyEvents,
		newOptInEmail,
		newOptInDiscord,
		newOptInTelegram,
		newOptInMatrix,
		req.Email != nil,
		userID, sess.AuthentikID, sess.Username,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "admin_update_failed", "Erreur de mise à jour des préférences")})
		return
	}

	message := h.tr(r, "admin_profile_updated", "Profile updated")

	if req.PreferredLang != nil {
		if strings.TrimSpace(newPreferredLang) == "" {
			// #nosec G124 -- language preference is intentionally readable by frontend language switching code.
			http.SetCookie(w, &http.Cookie{
				Name:     "lang",
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: false,
				Secure:   jgmw.RequestIsHTTPS(r, h.cfg.BaseURL),
				SameSite: http.SameSiteLaxMode,
			})
		} else {
			// #nosec G124 -- language preference is intentionally readable by frontend language switching code.
			http.SetCookie(w, &http.Cookie{
				Name:     "lang",
				Value:    newPreferredLang,
				Path:     "/",
				MaxAge:   31536000,
				HttpOnly: false,
				Secure:   jgmw.RequestIsHTTPS(r, h.cfg.BaseURL),
				SameSite: http.SameSiteLaxMode,
			})
		}
	}

	_ = h.db.LogAction(
		"user.profile.updated",
		sess.Username,
		sess.Username,
		fmt.Sprintf(`{"preferred_lang":"%s","notify_expiry":%t,"notify_events":%t,"opt_in_email":%t,"opt_in_discord":%t,"opt_in_telegram":%t}`,
			newPreferredLang,
			newNotifyExpiry,
			newNotifyEvents,
			newOptInEmail,
			newOptInDiscord,
			newOptInTelegram,
		),
	)

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: message,
		Data: map[string]interface{}{
			"email":                  newEmail,
			"pending_email":          newPendingEmail,
			"email_verified":         newEmailVerified && newPendingEmail == "",
			"contact_discord":        newDiscord,
			"contact_telegram":       newTelegram,
			"preferred_lang":         newPreferredLang,
			"notify_expiry_reminder": newNotifyExpiry,
			"notify_account_events":  newNotifyEvents,
			"opt_in_email":           newOptInEmail,
			"opt_in_discord":         newOptInDiscord,
			"opt_in_telegram":        newOptInTelegram,
		},
	})
}

// UpdateMyAccountAvatar change la photo de profil Jellyfin de l'utilisateur connecté.
func (h *AdminHandler) UpdateMyAccountAvatar(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if h.jfClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, APIResponse{Success: false, Message: h.tr(r, "admin_jf_unavailable", "Service Jellyfin indisponible")})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarUploadBytes+(1024*1024))
	if err := r.ParseMultipartForm(1 << 20); err != nil { // #nosec G120 -- request body is capped with http.MaxBytesReader above.
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "admin_file_too_large", "Fichier trop lourd ou invalide")})
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "admin_image_missing", "Image manquante")})
		return
	}
	defer file.Close()

	if header.Size > maxAvatarUploadBytes {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "admin_image_too_large", "Image trop lourde")})
		return
	}

	data, err := io.ReadAll(io.LimitReader(file, maxAvatarUploadBytes+1))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "admin_image_read_failed", "Erreur lecture image")})
		return
	}
	if int64(len(data)) > maxAvatarUploadBytes {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "admin_image_too_large", "Image trop lourde")})
		return
	}
	if len(data) == 0 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "admin_image_empty", "Image vide")})
		return
	}

	contentType := http.DetectContentType(data)
	if !isAllowedAvatarContentType(contentType) {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "admin_image_format_not_allowed", "Format image non autorise")})
		return
	}

	if err := h.jfClient.SetUserImage(sess.UserID, contentType, data); err != nil {
		slog.Error("Erreur mise a jour avatar Jellyfin", "username", sess.Username, "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur Jellyfin : " + err.Error()})
		return
	}

	_ = h.db.LogAction("user.avatar.updated", sess.Username, sess.Username, "Changement de photo de profil")

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: h.tr(r, "admin_avatar_updated", "Photo de profil mise à jour")})
}

func isAllowedAvatarContentType(contentType string) bool {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

// UpdateMyPassword informe l'utilisateur que la gestion du mot de passe est déléguée à Authentik SSO.
func (h *AdminHandler) UpdateMyPassword(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusBadRequest, APIResponse{
		Success: false,
		Message: h.tr(r, "admin_password_managed_by_authentik", "La gestion du mot de passe s'effectue directement sur le portail Authentik."),
	})
}

// ResendEmailVerification renvoie un code de vérification à l'utilisateur connecté.
func (h *AdminHandler) ResendEmailVerification(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if h.mailer == nil {
		writeJSON(w, http.StatusServiceUnavailable, APIResponse{Success: false, Message: h.tr(r, "admin_mail_service_unavailable", "Service mail non configuré")})
		return
	}

	var email, pendingEmail string
	var emailVerified bool
	var id int64
	err := h.db.QueryRow(`SELECT id, email, pending_email, email_verified FROM users WHERE authentik_id = ? OR jellyfin_id = ? OR username = ? OR id = ?`, sess.UserID, sess.UserID, sess.Username, sess.UserID).Scan(&id, &email, &pendingEmail, &emailVerified)
	if err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: h.tr(r, "admin_user_not_found", "Utilisateur introuvable")})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: h.tr(r, "admin_email_managed_by_authentik", "La vérification d'email s'effectue directement sur le portail Authentik."),
	})
}
