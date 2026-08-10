package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

func (h *AdminHandler) ProfilesPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.AdminUsername = sess.Username
	td.IsAdmin = true
	td.CanInvite = true
	td.LDAPEnabled = h.db.IsLDAPEnabled()
	td.Section = "profiles"
	if err := h.renderer.Render(w, "admin/profiles.html", td); err != nil {
		slog.Error("Erreur rendu profiles page", "error", err)
		http.Error(w, h.tr(r, "common_server_error_page", "Erreur serveur"), http.StatusInternalServerError)
	}
}

func (h *AdminHandler) LDAPPage(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

func (h *AdminHandler) SecurityPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.AdminUsername = sess.Username
	td.IsAdmin = true
	td.CanInvite = true
	td.LDAPEnabled = h.db.IsLDAPEnabled()
	td.Section = "security"
	if err := h.renderer.Render(w, "admin/security.html", td); err != nil {
		slog.Error("Erreur rendu security page", "error", err)
		http.Error(w, h.tr(r, "common_server_error_page", "Erreur serveur"), http.StatusInternalServerError)
	}
}

func (h *AdminHandler) PendingActionsPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.AdminUsername = sess.Username
	td.IsAdmin = true
	td.CanInvite = true
	td.LDAPEnabled = h.db.IsLDAPEnabled()
	td.Section = "pending_actions"
	if err := h.renderer.Render(w, "admin/pending_actions.html", td); err != nil {
		slog.Error("Erreur rendu pending actions page", "error", err)
		http.Error(w, h.tr(r, "common_server_error_page", "Erreur serveur"), http.StatusInternalServerError)
	}
}

func (h *AdminHandler) SecurityOverview(w http.ResponseWriter, r *http.Request) {
	since := time.Now().Add(-24 * time.Hour).Format("2006-01-02 15:04:05")
	overview := map[string]int{
		"blocked_ips":          h.countSecurityDistinctIP("invite.ip.blocked", since),
		"captcha_failures":     h.countSecurityEvents("captcha", "", since),
		"invalid_invitations":  h.countSecurityEvents("invalid_invite", "", since),
		"admin_logins":         h.countSecurityEvents("admin_login", "admin.login.success", since),
		"admin_login_failures": h.countSecurityEvents("admin_login", "admin.login.failed", since),
		"smtp_errors":          h.countSecurityEvents("smtp", "", since),
		"suspicious_alerts":    h.countCriticalUnresolvedEvents(),
	}
	recent, _ := h.readSecurityEvents(1, 10, "", "", "")
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"window_hours": 24,
		"overview":     overview,
		"recent":       recent,
	}})
}

func (h *AdminHandler) SecurityEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := positiveQueryInt(q.Get("page"), 1)
	limit := positiveQueryInt(q.Get("limit"), 50)
	if limit > 200 {
		limit = 200
	}
	category := strings.TrimSpace(q.Get("category"))
	severity := strings.TrimSpace(q.Get("severity"))
	search := strings.TrimSpace(q.Get("search"))

	events, total := h.readSecurityEvents(page, limit, category, severity, search)
	totalPages := 1
	if total > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(limit)))
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"events": events,
		"meta": map[string]interface{}{
			"page":        page,
			"limit":       limit,
			"total":       total,
			"total_pages": totalPages,
		},
	}})
}

func positiveQueryInt(raw string, fallback int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && n > 0 {
		return n
	}
	return fallback
}

func (h *AdminHandler) countSecurityEvents(category, eventType, since string) int {
	where := []string{"created_at >= ?"}
	args := []interface{}{since}
	if category != "" {
		where = append(where, "category = ?")
		args = append(args, category)
	}
	if eventType != "" {
		where = append(where, "event_type = ?")
		args = append(args, eventType)
	}
	var count int
	if err := h.db.QueryRow("SELECT COUNT(1) FROM security_events WHERE "+strings.Join(where, " AND "), args...).Scan(&count); err != nil {
		slog.Error("AdminHandler: erreur comptage evenements de securite", "error", err)
	}
	return count
}

func (h *AdminHandler) countSecurityDistinctIP(eventType, since string) int {
	var count int
	if err := h.db.QueryRow(
		`SELECT COUNT(DISTINCT ip) FROM security_events WHERE event_type = ? AND created_at >= ? AND ip IS NOT NULL AND ip <> ''`,
		eventType,
		since,
	).Scan(&count); err != nil {
		slog.Error("AdminHandler: erreur comptage IP distinctes de securite", "error", err)
	}
	return count
}

func (h *AdminHandler) countCriticalUnresolvedEvents() int {
	var count int
	if err := h.db.QueryRow(`SELECT COUNT(1) FROM security_events WHERE severity = ? AND resolved = ?`, "critical", false).Scan(&count); err != nil {
		slog.Error("AdminHandler: erreur comptage evenements critiques non resolus", "error", err)
	}
	return count
}

func (h *AdminHandler) readSecurityEvents(page, limit int, category, severity, search string) ([]database.SecurityEvent, int) {
	where := []string{}
	args := []interface{}{}
	if category != "" {
		where = append(where, "category = ?")
		args = append(args, category)
	}
	if severity != "" {
		where = append(where, "severity = ?")
		args = append(args, severity)
	}
	if search != "" {
		term := "%" + search + "%"
		where = append(where, "(actor LIKE ? OR target LIKE ? OR ip LIKE ? OR message LIKE ? OR metadata LIKE ?)")
		args = append(args, term, term, term, term, term)
	}
	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := h.db.QueryRow("SELECT COUNT(1) FROM security_events "+whereClause, args...).Scan(&total); err != nil {
		slog.Error("AdminHandler: erreur comptage total evenements de securite", "error", err)
	}

	offset := (page - 1) * limit
	queryArgs := append(append([]interface{}{}, args...), limit, offset)
	rows, err := h.db.Query(
		`SELECT id, category, event_type, severity, actor, target, ip, message, metadata, resolved, created_at
		 FROM security_events `+whereClause+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		queryArgs...,
	)
	if err != nil {
		slog.Warn("Lecture security_events impossible", "error", err)
		return []database.SecurityEvent{}, total
	}
	defer rows.Close()

	events := make([]database.SecurityEvent, 0)
	for rows.Next() {
		var ev database.SecurityEvent
		var actor, target, ip, message, metadata sql.NullString
		if err := rows.Scan(&ev.ID, &ev.Category, &ev.EventType, &ev.Severity, &actor, &target, &ip, &message, &metadata, &ev.Resolved, &ev.CreatedAt); err != nil {
			continue
		}
		ev.Actor = actor.String
		ev.Target = target.String
		ev.IP = ip.String
		ev.Message = message.String
		ev.Metadata = metadata.String
		events = append(events, ev)
	}
	return events, total
}

func (h *AdminHandler) PendingActions(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	userHorizon := now.AddDate(0, 0, 14).Format("2006-01-02 15:04:05")
	inviteHorizon := now.AddDate(0, 0, 7).Format("2006-01-02 15:04:05")
	smtpSince := now.AddDate(0, 0, -7).Format("2006-01-02 15:04:05")

	expiring := h.pendingExpiringUsers(userHorizon)
	unverified := h.pendingUnverifiedEmails()
	invites := h.pendingExpiringInvitations(inviteHorizon)
	smtpErrors := h.pendingSMTPErrors(smtpSince)

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"summary": map[string]int{
			"expiring_accounts":    len(expiring),
			"unverified_emails":    len(unverified),
			"expiring_invitations": len(invites),
			"smtp_errors":          len(smtpErrors),
		},
		"expiring_accounts":    expiring,
		"unverified_emails":    unverified,
		"expiring_invitations": invites,
		"smtp_errors":          smtpErrors,
	}})
}

func (h *AdminHandler) pendingExpiringUsers(horizon string) []map[string]interface{} {
	rows, err := h.db.Query(
		`SELECT id, username, email, access_expires_at, expiry_action
		 FROM users
		 WHERE is_active = ? AND access_expires_at IS NOT NULL AND access_expires_at > CURRENT_TIMESTAMP AND access_expires_at <= ?
		 ORDER BY access_expires_at ASC LIMIT 50`,
		true,
		horizon,
	)
	if err != nil {
		return []map[string]interface{}{}
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var id int64
		var username string
		var email, expiresAt, action sql.NullString
		if err := rows.Scan(&id, &username, &email, &expiresAt, &action); err == nil {
			items = append(items, map[string]interface{}{"id": id, "username": username, "email": email.String, "expires_at": expiresAt.String, "action": action.String})
		}
	}
	return items
}

func (h *AdminHandler) pendingUnverifiedEmails() []map[string]interface{} {
	rows, err := h.db.Query(
		`SELECT id, username, email, pending_email, email_verification_sent_at
		 FROM users
		 WHERE email_verified = ? AND ((email IS NOT NULL AND email <> '') OR pending_email <> '')
		 ORDER BY updated_at DESC LIMIT 50`,
		false,
	)
	if err != nil {
		return []map[string]interface{}{}
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var id int64
		var username string
		var email, pendingEmail, sentAt sql.NullString
		if err := rows.Scan(&id, &username, &email, &pendingEmail, &sentAt); err == nil {
			items = append(items, map[string]interface{}{"id": id, "username": username, "email": email.String, "pending_email": pendingEmail.String, "sent_at": sentAt.String})
		}
	}
	return items
}

func (h *AdminHandler) pendingExpiringInvitations(horizon string) []map[string]interface{} {
	rows, err := h.db.Query(
		`SELECT id, code, label, max_uses, used_count, expires_at, created_by
		 FROM invitations
		 WHERE expires_at IS NOT NULL AND expires_at > CURRENT_TIMESTAMP AND expires_at <= ?
		   AND (max_uses <= 0 OR used_count < max_uses)
		 ORDER BY expires_at ASC LIMIT 50`,
		horizon,
	)
	if err != nil {
		return []map[string]interface{}{}
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var id int64
		var code string
		var label, expiresAt, createdBy sql.NullString
		var maxUses, usedCount int
		if err := rows.Scan(&id, &code, &label, &maxUses, &usedCount, &expiresAt, &createdBy); err == nil {
			items = append(items, map[string]interface{}{"id": id, "code": code, "label": label.String, "max_uses": maxUses, "used_count": usedCount, "expires_at": expiresAt.String, "created_by": createdBy.String})
		}
	}
	return items
}

func (h *AdminHandler) pendingSMTPErrors(since string) []map[string]interface{} {
	items := []map[string]interface{}{}
	rows, err := h.db.Query(
		`SELECT event_type, actor, target, message, metadata, created_at
		 FROM security_events
		 WHERE category = 'smtp' AND created_at >= ?
		 ORDER BY created_at DESC LIMIT 25`,
		since,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var eventType string
			var actor, target, message, metadata, createdAt sql.NullString
			if err := rows.Scan(&eventType, &actor, &target, &message, &metadata, &createdAt); err == nil {
				items = append(items, map[string]interface{}{"source": "security", "action": eventType, "actor": actor.String, "target": target.String, "message": message.String, "details": metadata.String, "created_at": createdAt.String})
			}
		}
	}
	rows, err = h.db.Query(
		`SELECT action, actor, target, details, created_at
		 FROM audit_log
		 WHERE action LIKE ? AND created_at >= ?
		 ORDER BY created_at DESC LIMIT 25`,
		"%email.failed%",
		since,
	)
	if err != nil {
		return items
	}
	defer rows.Close()
	for rows.Next() {
		var action string
		var actor, target, details, createdAt sql.NullString
		if err := rows.Scan(&action, &actor, &target, &details, &createdAt); err == nil {
			items = append(items, map[string]interface{}{"source": "audit", "action": action, "actor": actor.String, "target": target.String, "details": details.String, "created_at": createdAt.String})
		}
	}
	return items
}

func (h *AdminHandler) PreviewInvitation(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	var req CreateInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}
	preview, err := h.buildInvitationPreview(r, sess, &req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Apercu pret", Data: preview})
}

func (h *AdminHandler) buildInvitationPreview(r *http.Request, sess *session.Payload, req *CreateInvitationRequest) (map[string]interface{}, error) {
	if sess == nil {
		return nil, fmt.Errorf("session absente")
	}
	if req.MaxUses < 0 {
		return nil, fmt.Errorf("la limite d'utilisations ne peut pas etre negative")
	}
	if req.ExpiresInDays < 0 || req.UserExpiryDays < 0 || req.DisableAfterDays < 0 {
		return nil, fmt.Errorf("les durees ne peuvent pas etre negatives")
	}

	inviteCfg, err := h.db.GetInvitationProfileConfig()
	if err != nil {
		return nil, fmt.Errorf("erreur de lecture du profil d'invitation")
	}
	limits, err := h.resolveInvitationCreatorLimits(sess, inviteCfg)
	if err != nil {
		return nil, fmt.Errorf("impossible de charger les limites de creation")
	}
	if !sess.IsAdmin && !limits.CanInvite {
		return nil, fmt.Errorf("vous n'avez pas l'autorisation de creer des invitations")
	}

	targetPresetID := strings.TrimSpace(limits.TargetPresetID)
	if sess.IsAdmin && strings.TrimSpace(req.PolicyPresetID) != "" {
		targetPresetID = strings.TrimSpace(req.PolicyPresetID)
	} else if !sess.IsAdmin && strings.TrimSpace(req.PolicyPresetID) != "" {
		requestedPresetID := strings.TrimSpace(strings.ToLower(req.PolicyPresetID))
		if !presetIDAllowed(requestedPresetID, limits.AllowedTargetPresetIDs) {
			return nil, fmt.Errorf("ce profil cible n'est pas autorise par votre profil")
		}
		targetPresetID = requestedPresetID
	}

	var preset *config.JellyfinPolicyPreset
	if targetPresetID != "" {
		preset, err = h.getJellyfinPresetByID(targetPresetID)
		if err != nil {
			return nil, fmt.Errorf("preset Jellyfin introuvable")
		}
		if preset.IsAdministrator && !sess.IsAdmin {
			return nil, fmt.Errorf("le preset administrateur est reserve aux administrateurs JellyGate")
		}
	}

	maxUses := req.MaxUses
	if maxUses == 0 && !sess.IsAdmin && limits.MaxUses > 0 {
		maxUses = limits.MaxUses
	}
	if !sess.IsAdmin && limits.MaxUses > 0 && (maxUses <= 0 || maxUses > limits.MaxUses) {
		return nil, fmt.Errorf("limite par lien: entre 1 et %d utilisations pour les parrains", limits.MaxUses)
	}

	linkDays := req.ExpiresInDays
	if limits.LinkValidityDays > 0 && !req.IgnorePresetLinkExpiry {
		if linkDays <= 0 || linkDays > limits.LinkValidityDays {
			linkDays = limits.LinkValidityDays
		}
	}

	applyUserExpiry := req.ApplyUserExpiry != nil && *req.ApplyUserExpiry
	userDays := req.UserExpiryDays
	if limits.UserExpiryDays > 0 && !req.IgnorePresetUserExpiry {
		applyUserExpiry = true
		if userDays <= 0 || userDays > limits.UserExpiryDays {
			userDays = limits.UserExpiryDays
		}
	}
	if applyUserExpiry && userDays <= 0 && strings.TrimSpace(req.UserExpiresAt) == "" {
		return nil, fmt.Errorf("renseigne un nombre de jours valide pour l'expiration utilisateur")
	}

	profile := jellyfin.InviteProfile{
		EnableAllFolders:         len(req.Libraries) == 0,
		EnabledFolderIDs:         req.Libraries,
		EnableDownload:           inviteCfg.EnableDownloads,
		RequireEmail:             inviteCfg.RequireEmail,
		RequireEmailVerification: resolveInviteEmailVerificationRequirement(inviteCfg.EmailVerificationPolicy, inviteCfg.RequireEmailVerification, sess.IsAdmin, maxUses),
		EnableRemoteAccess:       true,
		UserExpiryDays:           userDays,
		DisableAfterDays:         userDays,
		ForcedUsername:           strings.TrimSpace(req.ForcedUsername),
		CanInvite:                req.NewUserCanInvite && (sess.IsAdmin || limits.AllowGrant),
		UsernameMinLength:        inviteCfg.UsernameMinLength,
		UsernameMaxLength:        inviteCfg.UsernameMaxLength,
		PasswordMinLength:        inviteCfg.PasswordMinLength,
		PasswordMaxLength:        inviteCfg.PasswordMaxLength,
		PasswordRequireUpper:     inviteCfg.PasswordRequireUpper,
		PasswordRequireLower:     inviteCfg.PasswordRequireLower,
		PasswordRequireDigit:     inviteCfg.PasswordRequireDigit,
		PasswordRequireSpecial:   inviteCfg.PasswordRequireSpecial,
		ExpiryAction:             normalizeExpiryAction(inviteCfg.ExpiryAction),
		DeleteAfterDays:          inviteCfg.DeleteAfterDays,
	}
	if preset != nil {
		profile = inviteProfileFromPolicyPreset(preset)
		profile.RequireEmail = inviteCfg.RequireEmail
		profile.RequireEmailVerification = resolveInviteEmailVerificationRequirement(inviteCfg.EmailVerificationPolicy, inviteCfg.RequireEmailVerification, sess.IsAdmin, maxUses)
		profile.UserExpiryDays = userDays
		profile.DisableAfterDays = userDays
		profile.ForcedUsername = strings.TrimSpace(req.ForcedUsername)
		profile.CanInvite = preset.CanInvite || preset.CanCreateInvitations || (req.NewUserCanInvite && (sess.IsAdmin || limits.AllowGrant))
		if !applyUserExpiry {
			defaultAccountDuration := preset.DefaultAccountDurationDays
			if defaultAccountDuration <= 0 {
				defaultAccountDuration = preset.DisableAfterDays
			}
			if defaultAccountDuration > 0 {
				applyUserExpiry = true
				userDays = defaultAccountDuration
				profile.UserExpiryDays = userDays
				profile.DisableAfterDays = userDays
			}
		}
	}
	isTemporary := req.IsTemporary || (preset != nil && preset.IsTemporary)
	accountDurationDays := 0
	if isTemporary {
		if !sess.IsAdmin {
			if !limits.CanCreateTemporaryInvitations {
				return nil, fmt.Errorf("votre profil ne permet pas de creer des invitations temporaires")
			}
			if !presetIDAllowed(targetPresetID, limits.AllowedTemporaryPresetIDs) {
				return nil, fmt.Errorf("ce profil temporaire n'est pas autorise par votre profil")
			}
		}
		defaultDuration := limits.DefaultTemporaryDurationDays
		maxDuration := limits.MaxTemporaryDurationDays
		if preset != nil {
			if preset.DefaultAccountDurationDays > 0 {
				defaultDuration = preset.DefaultAccountDurationDays
			} else if preset.DisableAfterDays > 0 {
				defaultDuration = preset.DisableAfterDays
			}
			if preset.MaxAccountDurationDays > 0 {
				maxDuration = preset.MaxAccountDurationDays
			}
		}
		accountDurationDays = req.AccountDurationDays
		if accountDurationDays <= 0 {
			accountDurationDays = defaultDuration
		}
		if accountDurationDays <= 0 {
			return nil, fmt.Errorf("renseigne une duree de compte temporaire valide")
		}
		if maxDuration > 0 && accountDurationDays > maxDuration {
			if !sess.IsAdmin {
				return nil, fmt.Errorf("duree temporaire limitee a %d jour(s)", maxDuration)
			}
			accountDurationDays = maxDuration
		}
		profile.IsTemporary = true
		profile.AccountDurationDays = accountDurationDays
		profile.UserExpiryDays = accountDurationDays
		profile.DisableAfterDays = accountDurationDays
	}
	links := resolvePortalLinks(h.cfg, h.db)
	baseURL := strings.TrimSpace(links.JellyGateURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(h.cfg.BaseURL)
	}
	if baseURL == "" {
		baseURL = requestBaseURL(r)
	}
	pseudoURL := strings.TrimRight(baseURL, "/") + "/invite/ABCD12"
	preferredLang := normalizeSupportedEmailLang(req.PreferredLang)
	if preferredLang == "" {
		preferredLang = h.db.GetDefaultLang()
	}

	return map[string]interface{}{
		"public_url":          pseudoURL,
		"max_uses":            maxUses,
		"link_validity_days":  linkDays,
		"user_expiry_enabled": applyUserExpiry,
		"user_expiry_days":    userDays,
		"preferred_lang":      preferredLang,
		"send_to_email":       strings.TrimSpace(firstNonEmpty(req.SendToEmail, req.Email)),
		"email_message":       strings.TrimSpace(req.EmailMessage),
		"profile":             profile,
		"preset":              preset,
		"public_preview_html": fmt.Sprintf(`<div class="jg-public-preview"><strong>Invitation JellyGate</strong><span>%s</span><small>%d utilisation(s), lien %d jour(s)</small></div>`, pseudoURL, maxUses, linkDays),
	}, nil
}
