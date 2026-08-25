package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/maelmoreau21/JellyGate/internal/authentik"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

// InvitationResponse représente une invitation formatée pour l'API JSON.
type InvitationResponse struct {
	ID                     int64                  `json:"id"`
	Code                   string                 `json:"code"`
	Label                  string                 `json:"label"`
	PreferredLang          string                 `json:"preferred_lang"`
	MaxUses                int                    `json:"max_uses"`
	UsedCount              int                    `json:"used_count"`
	JellyfinProfile        map[string]interface{} `json:"jellyfin_profile"`
	ProfileID              string                 `json:"profile_id"`
	ProfileSnapshot        map[string]interface{} `json:"profile_snapshot,omitempty"`
	IsTemporary            bool                   `json:"is_temporary"`
	AccountDurationDays    int                    `json:"account_duration_days"`
	ExpiresAt              string                 `json:"expires_at,omitempty"`
	CreatedBy              string                 `json:"created_by"`
	CreatedAt              string                 `json:"created_at"`
	AuthentikInvitationID  string                 `json:"authentik_invitation_id,omitempty"`
	AuthentikEnrollmentURL string                 `json:"authentik_enrollment_url,omitempty"`
	InviteURL              string                 `json:"invite_url,omitempty"`
}

type InvitationSponsorStats struct {
	Sponsor        string  `json:"sponsor"`
	CreatedLinks   int     `json:"created_links"`
	ActiveLinks    int     `json:"active_links"`
	ClosedLinks    int     `json:"closed_links"`
	TotalUses      int     `json:"total_uses"`
	Conversions    int     `json:"conversions"`
	ConversionRate float64 `json:"conversion_rate"`
}

// CreateInvitationRequest payload pour la création d'invitation
type CreateInvitationRequest struct {
	Label                  string   `json:"label"`
	PreferredLang          string   `json:"preferred_lang"`
	MaxUses                int      `json:"max_uses"`   // 0 = illimité
	ExpiresAt              string   `json:"expires_at"` // Legacy: date précise, exemple "2026-10-05T12:00"
	ExpiresInDays          int      `json:"expires_in_days"`
	IgnorePresetLinkExpiry bool     `json:"ignore_preset_link_expiry"`
	ApplyUserExpiry        *bool    `json:"apply_user_expiry"`
	UserExpiryDays         int      `json:"user_expiry_days"` // Expiration finale du compte client (jours)
	UserExpiresAt          string   `json:"user_expires_at"`
	IgnorePresetUserExpiry bool     `json:"ignore_preset_user_expiry"`
	DisableAfterDays       int      `json:"disable_after_days"`
	IsTemporary            bool     `json:"is_temporary"`
	AccountDurationDays    int      `json:"account_duration_days"`
	NewUserCanInvite       bool     `json:"new_user_can_invite"`
	SendToEmail            string   `json:"send_to_email"` // Si renseigné, un e-mail partira par SMTP
	Email                  string   `json:"email"`         // Legacy frontend key
	EmailMessage           string   `json:"email_message"`
	Libraries              []string `json:"libraries"` // ID des bibliothèques Jellyfin
	EnableDownloads        bool     `json:"enable_downloads"`
	PolicyPresetID         string   `json:"policy_preset_id"`
	GroupName              string   `json:"group_name"`
	ForcedName             string   `json:"forced_name"`
	ForcedUsername         string   `json:"forced_username"`
	TemplateUserID         string   `json:"template_user_id"`
	UsernameMinLen         *int     `json:"username_min_length"`
	UsernameMaxLen         *int     `json:"username_max_length"`
	PasswordMinLen         *int     `json:"password_min_length"`
	PasswordMaxLen         *int     `json:"password_max_length"`
	RequireUpper           *bool    `json:"password_require_upper"`
	RequireLower           *bool    `json:"password_require_lower"`
	RequireDigit           *bool    `json:"password_require_digit"`
	RequireSpecial         *bool    `json:"password_require_special"`
	ExpiryAction           string   `json:"expiry_action"`
	DeleteAfterDays        *int     `json:"delete_after_days"`
}

type invitationCreatorLimits struct {
	CanInvite         bool
	AllowGrant        bool
	AllowUserExpiry   bool
	AllowIgnoreLimits bool
	AllowLanguage     bool

	MaxUses          int
	LinkValidityDays int
	UserExpiryDays   int
	QuotaDay         int
	QuotaMonth       int

	SourcePreset                  *config.JellyfinPolicyPreset
	TargetPresetID                string
	AllowedTargetPresetIDs        []string
	CanCreateTemporaryInvitations bool
	AllowedTemporaryPresetIDs     []string
	DefaultTemporaryDurationDays  int
	MaxTemporaryDurationDays      int
}

// GetMyInvitations retourne les invitations créées par l'utilisateur connecté.
func (h *AdminHandler) GetMyInvitations(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	now := time.Now()

	inviteCfg, err := h.db.GetInvitationProfileConfig()
	if err != nil {
		inviteCfg = config.DefaultInvitationProfileConfig()
	}
	limits, err := h.resolveInvitationCreatorLimits(sess, inviteCfg)
	if err != nil {
		slog.Error("Erreur calcul limites de parrainage", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "db_error", "Database error")})
		return
	}

	links := resolvePortalLinks(h.cfg, h.db)
	baseURL := strings.TrimSpace(links.JellyGateURL)
	if baseURL == "" {
		baseURL = requestBaseURL(r)
	}

	authCfg, _ := h.db.GetAuthentikConfig()
	authentikEnabled := (h.cfg != nil && h.cfg.Authentik.Enabled) || authCfg.Enabled
	var authBaseURL string
	var flowSlug string
	if authentikEnabled {
		rawAuthURL := authCfg.URL
		if rawAuthURL == "" && h.cfg != nil {
			rawAuthURL = h.cfg.Authentik.URL
		}
		if rawAuthURL == "" && authCfg.IssuerURL != "" {
			rawAuthURL = authCfg.IssuerURL
		}
		authBaseURL = authentik.ResolveBaseURL(rawAuthURL)
		flowSlug = strings.TrimSpace(authCfg.EnrollmentFlowSlug)
		if flowSlug == "" && h.cfg != nil {
			flowSlug = strings.TrimSpace(h.cfg.Authentik.EnrollmentFlowSlug)
		}
		if flowSlug == "" {
			flowSlug = "default-enrollment-flow"
		}
	}

	rows, err := h.db.Query(`
		SELECT id, code, max_uses, used_count, expires_at, created_at, COALESCE(authentik_invitation_id, '')
		FROM invitations 
		WHERE created_by = ? 
		ORDER BY created_at DESC`, sess.Username)
	if err != nil {
		slog.Error("Erreur lecture mes invitations", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "db_error", "Database error")})
		return
	}
	defer rows.Close()

	var invs []InvitationResponse
	activeLinks := 0
	for rows.Next() {
		var i InvitationResponse
		var rawExpiresAt, rawCreatedAt interface{}
		var authInvID sql.NullString
		if err := rows.Scan(&i.ID, &i.Code, &i.MaxUses, &i.UsedCount, &rawExpiresAt, &rawCreatedAt, &authInvID); err != nil {
			continue
		}
		i.ExpiresAt = anyToDateString(rawExpiresAt)
		i.CreatedAt = anyToDateString(rawCreatedAt)
		i.AuthentikInvitationID = strings.TrimSpace(authInvID.String)
		i.InviteURL = strings.TrimRight(baseURL, "/") + "/invite/" + i.Code
		if authentikEnabled && i.AuthentikInvitationID != "" && authBaseURL != "" {
			i.AuthentikEnrollmentURL = fmt.Sprintf("%s/if/flow/%s/?itoken=%s", authBaseURL, flowSlug, url.QueryEscape(i.AuthentikInvitationID))
		}
		isExpired := false
		if strings.TrimSpace(i.ExpiresAt) != "" {
			if exp, parseErr := parseAccessExpiry(i.ExpiresAt); parseErr == nil {
				isExpired = !exp.After(now)
			}
		}
		if !isExpired && (i.MaxUses <= 0 || i.UsedCount < i.MaxUses) {
			activeLinks++
		}
		invs = append(invs, i)
	}

	todayCount, _ := h.countInvitationsCreatedSince(sess.Username, startOfLocalDay(now))
	monthCount, _ := h.countInvitationsCreatedSince(sess.Username, startOfLocalMonth(now))

	conversions := 0
	_ = h.db.QueryRow(`
		SELECT COUNT(u.id)
		FROM invitations i
		LEFT JOIN users u ON u.invited_by = i.code
		WHERE i.created_by = ?`, sess.Username,
	).Scan(&conversions)

	targetPresetName := ""
	if strings.TrimSpace(limits.TargetPresetID) != "" {
		if targetPreset, presetErr := h.getJellyfinPresetByID(strings.TrimSpace(limits.TargetPresetID)); presetErr == nil && targetPreset != nil {
			targetPresetName = strings.TrimSpace(targetPreset.Name)
		}
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"links": invs,
		"limits": map[string]interface{}{
			"can_invite":                       limits.CanInvite,
			"max_uses":                         limits.MaxUses,
			"link_validity_days":               limits.LinkValidityDays,
			"quota_day":                        limits.QuotaDay,
			"quota_month":                      limits.QuotaMonth,
			"target_preset_id":                 strings.TrimSpace(limits.TargetPresetID),
			"target_preset_name":               targetPresetName,
			"allowed_target_preset_ids":        limits.AllowedTargetPresetIDs,
			"can_create_temporary_invitations": limits.CanCreateTemporaryInvitations,
			"allowed_temporary_preset_ids":     limits.AllowedTemporaryPresetIDs,
			"default_temporary_duration_days":  limits.DefaultTemporaryDurationDays,
			"max_temporary_duration_days":      limits.MaxTemporaryDurationDays,
		},
		"usage": map[string]interface{}{
			"today": todayCount,
			"month": monthCount,
		},
		"stats": map[string]interface{}{
			"total_links":  len(invs),
			"active_links": activeLinks,
			"conversions":  conversions,
		},
	}})
}

// CreateMyInvitation génère une invitation automatique (parrainage) basée sur le preset de l'utilisateur.
func (h *AdminHandler) CreateMyInvitation(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	now := time.Now()

	inviteCfg, err := h.db.GetInvitationProfileConfig()
	if err != nil {
		inviteCfg = config.DefaultInvitationProfileConfig()
	}

	limits, err := h.resolveInvitationCreatorLimits(sess, inviteCfg)
	if err != nil {
		slog.Error("Erreur calcul limites invitation utilisateur", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "admin_limits_read_failed", "Erreur de lecture des limites")})
		return
	}

	if !limits.CanInvite {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: h.tr(r, "admin_invite_forbidden", "Vous n'avez pas l'autorisation de parrainer")})
		return
	}

	if !limits.AllowIgnoreLimits {
		if limits.QuotaDay > 0 {
			todayCount, countErr := h.countInvitationsCreatedSince(sess.Username, startOfLocalDay(now))
			if countErr != nil {
				writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "admin_quota_check_failed", "Erreur verification quota")})
				return
			}
			if todayCount >= limits.QuotaDay {
				writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: fmt.Sprintf(h.tr(r, "admin_quota_day_reached", "Quota journalier atteint (%d/%d)"), todayCount, limits.QuotaDay)})
				return
			}
		}

		if limits.QuotaMonth > 0 {
			monthCount, countErr := h.countInvitationsCreatedSince(sess.Username, startOfLocalMonth(now))
			if countErr != nil {
				writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "admin_quota_check_failed", "Erreur verification quota")})
				return
			}
			if monthCount >= limits.QuotaMonth {
				writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: fmt.Sprintf(h.tr(r, "admin_quota_month_reached", "Quota mensuel atteint (%d/%d)"), monthCount, limits.QuotaMonth)})
				return
			}
		}
	}

	targetPresetID := strings.TrimSpace(limits.TargetPresetID)
	if targetPresetID == "" && limits.SourcePreset != nil {
		targetPresetID = strings.TrimSpace(limits.SourcePreset.ID)
	}
	if targetPresetID == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "admin_no_preset_configured", "Aucun profil de parrainage configuré sur le serveur")})
		return
	}

	targetPreset, err := h.getJellyfinPresetByID(targetPresetID)
	if err != nil || targetPreset == nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "admin_preset_read_failed", "Erreur lecture profil de droit")})
		return
	}
	if targetPreset.IsTemporary {
		if !limits.CanCreateTemporaryInvitations {
			writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: h.tr(r, "admin_temp_invite_forbidden", "Votre profil ne permet pas de creer des invitations temporaires")})
			return
		}
		if !presetIDAllowed(targetPreset.ID, limits.AllowedTemporaryPresetIDs) {
			writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: h.tr(r, "admin_temp_profile_forbidden", "Ce profil temporaire n'est pas autorise")})
			return
		}
	}

	// Verification quota parrainage JellyGate
	var sponsorUserID int64
	_ = h.db.QueryRow(`SELECT id FROM users WHERE username = ?`, sess.Username).Scan(&sponsorUserID)
	if sponsorUserID > 0 {
		calc, qErr := h.db.CalculateUserQuota(r.Context(), sponsorUserID)
		if qErr == nil && calc != nil && calc.RemainingQuota <= 0 {
			writeJSON(w, http.StatusBadRequest, APIResponse{
				Success: false,
				Message: fmt.Sprintf(h.tr(r, "admin_quota_reached", "Quota d'invitations épuisé (%d/%d)"), calc.UsedQuota, calc.TotalQuota),
			})
			return
		}
	}

	code, err := generateSecureToken(12)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "admin_invite_gen_failed", "Impossible de generer un code d'invitation")})
		return
	}

	maxUses := limits.MaxUses
	if maxUses <= 0 && limits.SourcePreset == nil {
		maxUses = targetPreset.InviteMaxUses
	}

	validityDays := limits.LinkValidityDays
	if validityDays <= 0 && limits.SourcePreset == nil {
		validityDays = presetInviteLinkValidityDays(targetPreset)
	}

	var expiresAt interface{}
	var expiresAtResponse interface{}
	var resolvedExpiry time.Time
	if validityDays > 0 {
		resolvedExpiry = now.AddDate(0, 0, validityDays)
		expiresAt = resolvedExpiry
		expiresAtResponse = resolvedExpiry.Format(time.RFC3339)
	}

	profile := inviteProfileFromPolicyPreset(targetPreset)
	profile.CanInvite = false
	profile.RequireEmail = inviteCfg.RequireEmail
	if profile.IsTemporary {
		duration := profile.AccountDurationDays
		if duration <= 0 {
			duration = limits.DefaultTemporaryDurationDays
		}
		if duration <= 0 {
			duration = targetPreset.DisableAfterDays
		}
		if duration > 0 {
			if limits.MaxTemporaryDurationDays > 0 && duration > limits.MaxTemporaryDurationDays {
				duration = limits.MaxTemporaryDurationDays
			}
			profile.AccountDurationDays = duration
			profile.DisableAfterDays = duration
			profile.UserExpiryDays = duration
		}
	}
	profileJSON, _ := json.Marshal(profile)

	// Invoquer l'API Authentik pour créer une invitation Stage si Authentik est configuré
	var authentikInvID string
	authCfg, _ := h.db.GetAuthentikConfig()
	authentikEnabled := (h.cfg != nil && h.cfg.Authentik.Enabled) || authCfg.Enabled
	effectiveAuth := h.getEffectiveAuthentikClient()
	if effectiveAuth != nil && authentikEnabled {
		flowSlug := strings.TrimSpace(authCfg.EnrollmentFlowSlug)
		if flowSlug == "" && h.cfg != nil {
			flowSlug = strings.TrimSpace(h.cfg.Authentik.EnrollmentFlowSlug)
		}
		if flowSlug == "" {
			flowSlug = "default-enrollment-flow"
		}

		var targetGroups []string
		jellyfinGroup := strings.TrimSpace(authCfg.JellyfinUserGroup)
		if jellyfinGroup == "" && h.cfg != nil {
			jellyfinGroup = strings.TrimSpace(h.cfg.Authentik.JellyfinUserGroup)
		}
		if jellyfinGroup == "" {
			jellyfinGroup = "jellyfin-users"
		}
		targetGroups = append(targetGroups, jellyfinGroup)

		isRecursive := sess.CanInviteRecursive
		if !isRecursive {
			invRecGroup := strings.TrimSpace(authCfg.InvitersRecursiveGroup)
			if invRecGroup == "" {
				invRecGroup = "jellygate-inviters-recursive"
			}
			for _, g := range sess.Groups {
				if strings.EqualFold(g, invRecGroup) || strings.EqualFold(g, "jellygate-inviters-recursive") {
					isRecursive = true
					break
				}
			}
		}

		if isRecursive {
			invGroup := strings.TrimSpace(authCfg.InvitersGroup)
			if invGroup == "" {
				invGroup = "jellygate-inviters"
			}
			targetGroups = append(targetGroups, invGroup)
		}

		fixedData := map[string]interface{}{
			"source":                "JellyGate",
			"created_by":            "JellyGate",
			"created_by_app":        "JellyGate",
			"sponsor":               sess.Username,
			"sponsor_user_id":       sponsorUserID,
			"code":                  code,
			"invitation_code":       code,
			"groups":                targetGroups,
			"preset_id":             targetPreset.ID,
			"target_preset_name":    targetPreset.Name,
			"is_temporary":          profile.IsTemporary,
			"account_duration_days": profile.AccountDurationDays,
		}

		tokenName := fmt.Sprintf("jellygate-sponsor-%s-%s", sess.Username, code)
		invID, authErr := effectiveAuth.CreateInvitationStageToken(r.Context(), tokenName, resolvedExpiry, fixedData, maxUses == 1, flowSlug)
		if authErr == nil && strings.TrimSpace(invID) != "" {
			authentikInvID = strings.TrimSpace(invID)
			slog.Info("Jeton invitation Authentik créé avec succès pour parrainage", "code", code, "sponsor", sess.Username, "authentik_invitation_id", authentikInvID, "expires", resolvedExpiry)
		} else if authErr != nil {
			slog.Warn("Création token invitation Authentik échouée pour parrainage (fallback local)", "code", code, "sponsor", sess.Username, "error", authErr)
		}
	}

	res, err := h.db.Exec(`
		INSERT INTO invitations (code, label, max_uses, used_count, jellyfin_profile, expires_at, created_by, created_by_user_id, authentik_invitation_id, profile_id, profile_snapshot, is_temporary, account_duration_days)
		VALUES (?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		code, "Parrainage de "+sess.Username, maxUses, string(profileJSON), expiresAt, sess.Username, sponsorUserID, authentikInvID,
		strings.TrimSpace(strings.ToLower(profile.PresetID)), string(profileJSON), profile.IsTemporary, profile.AccountDurationDays)

	if err != nil {
		slog.Error("Erreur creation invitation parrainage", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "db_error", "Database error")})
		return
	}

	invitationID, _ := res.LastInsertId()
	if sponsorUserID > 0 {
		_, _ = h.db.CreateReferral(r.Context(), sponsorUserID, invitationID, "")
	}

	_ = h.db.LogAction("invite.created.sponsor", sess.Username, code, fmt.Sprintf(`{"target_preset":"%s","max_uses":%d,"validity_days":%d}`, targetPreset.ID, maxUses, validityDays))

	var authentikEnrollmentURL string
	if authentikEnabled && authentikInvID != "" {
		rawAuthURL := authCfg.URL
		if rawAuthURL == "" && h.cfg != nil {
			rawAuthURL = h.cfg.Authentik.URL
		}
		if rawAuthURL == "" && authCfg.IssuerURL != "" {
			rawAuthURL = authCfg.IssuerURL
		}
		authBaseURL := authentik.ResolveBaseURL(rawAuthURL)
		flowSlug := strings.TrimSpace(authCfg.EnrollmentFlowSlug)
		if flowSlug == "" && h.cfg != nil {
			flowSlug = strings.TrimSpace(h.cfg.Authentik.EnrollmentFlowSlug)
		}
		if effectiveAuth != nil {
			if discovered := effectiveAuth.GetEnrollmentFlowSlug(r.Context(), flowSlug); discovered != "" {
				flowSlug = discovered
			}
		}
		if flowSlug == "" {
			flowSlug = "default-enrollment-flow"
		}
		if authBaseURL != "" {
			authentikEnrollmentURL = fmt.Sprintf("%s/if/flow/%s/?itoken=%s", authBaseURL, flowSlug, url.QueryEscape(authentikInvID))
		}
	}

	inviteURL := strings.TrimRight(requestBaseURL(r), "/") + "/invite/" + code

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: h.tr(r, "admin_invite_created", "Lien de parrainage créé"), Data: map[string]interface{}{
		"code":                     code,
		"max_uses":                 maxUses,
		"expires_at":               expiresAtResponse,
		"authentik_invitation_id":  authentikInvID,
		"authentik_enrollment_url": authentikEnrollmentURL,
		"authentik_enabled":        authentikEnabled,
		"target_preset_id":         targetPreset.ID,
		"target_preset_name":       targetPreset.Name,
		"link_validity_days":       validityDays,
		"invite_url":               inviteURL,
		"url":                      inviteURL,
	}})
}

func (h *AdminHandler) resolveInvitationCreatorLimits(sess *session.Payload, inviteCfg config.InvitationProfileConfig) (invitationCreatorLimits, error) {
	limits := invitationCreatorLimits{
		AllowGrant:      inviteCfg.AllowInviterGrant,
		AllowUserExpiry: inviteCfg.AllowInviterUserExpiry,
		MaxUses:         inviteCfg.InviterMaxUses,
		UserExpiryDays:  inviteCfg.DisableAfterDays,
		QuotaDay:        inviteCfg.InviterQuotaDay,
		QuotaMonth:      inviteCfg.InviterQuotaMonth,
		TargetPresetID:  strings.TrimSpace(inviteCfg.PolicyPresetID),
	}

	if inviteCfg.InviterMaxLinkHours > 0 {
		limits.LinkValidityDays = (inviteCfg.InviterMaxLinkHours + 23) / 24
	}

	if sess == nil {
		return limits, nil
	}

	if sess.IsAdmin {
		limits.CanInvite = true
		limits.AllowGrant = true
		limits.AllowUserExpiry = true
		limits.AllowIgnoreLimits = true
		limits.AllowLanguage = true
		limits.CanCreateTemporaryInvitations = true

		if limits.TargetPresetID != "" {
			if preset, err := h.getJellyfinPresetByID(limits.TargetPresetID); err == nil {
				if days := presetInviteLinkValidityDays(preset); days > 0 {
					limits.LinkValidityDays = days
				}
				if preset.DisableAfterDays > 0 {
					limits.UserExpiryDays = preset.DisableAfterDays
				}
			}
		}

		return limits, nil
	}

	if sess.CanInvite || sess.CanInviteRecursive {
		limits.CanInvite = true
		if sess.CanInviteRecursive {
			limits.AllowGrant = true
		}
	}

	authCfg, _ := h.db.GetAuthentikConfig()
	invGroup := strings.TrimSpace(authCfg.InvitersGroup)
	if invGroup == "" {
		invGroup = "jellygate-inviters"
	}
	invRecGroup := strings.TrimSpace(authCfg.InvitersRecursiveGroup)
	if invRecGroup == "" {
		invRecGroup = "jellygate-inviters-recursive"
	}

	for _, g := range sess.Groups {
		if strings.EqualFold(g, invRecGroup) || strings.EqualFold(g, "jellygate-inviters-recursive") {
			limits.CanInvite = true
			limits.AllowGrant = true
			break
		}
		if strings.EqualFold(g, invGroup) || strings.EqualFold(g, "jellygate-inviters") {
			limits.CanInvite = true
		}
	}

	var (
		canInvite bool
		presetID  sql.NullString
	)
	err := h.db.QueryRow(
		`SELECT can_invite, preset_id FROM users WHERE (jellyfin_id = ? AND jellyfin_id != '') OR CAST(id AS TEXT) = ? OR username = ? LIMIT 1`,
		sess.UserID, sess.UserID, sess.Username,
	).Scan(&canInvite, &presetID)
	if err != nil && err != sql.ErrNoRows {
		return limits, err
	}
	if canInvite {
		limits.CanInvite = true
	}

	presetIDStr := strings.TrimSpace(presetID.String)
	if presetIDStr != "" {
		preset, err := h.getJellyfinPresetByID(presetIDStr)
		if err == nil && preset != nil {
			limits.SourcePreset = preset
			if preset.CanInvite || preset.CanCreateInvitations {
				limits.CanInvite = true
			}
			if preset.InviteAllowLanguage {
				limits.AllowLanguage = true
			}
			limits.AllowedTargetPresetIDs = normalizedPresetIDs(preset.AllowedTargetPresetIDs)
			limits.MaxUses = preset.InviteMaxUses
			limits.LinkValidityDays = presetInviteLinkValidityDays(preset)
			limits.QuotaDay = preset.InviteQuotaDay
			limits.QuotaMonth = presetInviteQuotaMonth(preset)
			if strings.TrimSpace(preset.TargetPresetID) != "" {
				limits.TargetPresetID = strings.TrimSpace(preset.TargetPresetID)
			}
			if limits.TargetPresetID != "" && !presetIDAllowed(limits.TargetPresetID, limits.AllowedTargetPresetIDs) {
				limits.AllowedTargetPresetIDs = append(limits.AllowedTargetPresetIDs, strings.TrimSpace(strings.ToLower(limits.TargetPresetID)))
			}
			limits.CanCreateTemporaryInvitations = preset.CanCreateTemporaryInvitations
			limits.AllowedTemporaryPresetIDs = normalizedPresetIDs(preset.AllowedTemporaryPresetIDs)
			limits.DefaultTemporaryDurationDays = preset.DefaultTemporaryDurationDays
			limits.MaxTemporaryDurationDays = preset.MaxTemporaryDurationDays
			if preset.DisableAfterDays > 0 {
				limits.UserExpiryDays = preset.DisableAfterDays
			}
		}
	}

	if limits.TargetPresetID != "" {
		if targetPreset, err := h.getJellyfinPresetByID(limits.TargetPresetID); err == nil && targetPreset != nil {
			if targetPreset.DisableAfterDays > 0 {
				limits.UserExpiryDays = targetPreset.DisableAfterDays
			}
		}
	}

	return limits, nil
}

func (h *AdminHandler) countInvitationsCreatedSince(creator string, since time.Time) (int, error) {
	creator = strings.TrimSpace(creator)
	if creator == "" {
		return 0, fmt.Errorf("creator vide")
	}

	var count int
	err := h.db.QueryRow(
		`SELECT COUNT(1) FROM invitations WHERE created_by = ? AND created_at >= ?`,
		creator,
		since,
	).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// ListInvitations retourne les invitations SQLite avec pagination et recherche.
func (h *AdminHandler) ListInvitations(w http.ResponseWriter, r *http.Request) {
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

	whereParts := make([]string, 0)
	args := make([]interface{}, 0)

	if !sess.IsAdmin {
		whereParts = append(whereParts, "created_by = ?")
		args = append(args, sess.Username)
	}

	if search != "" {
		term := "%" + search + "%"
		whereParts = append(whereParts, "(code LIKE ? OR label LIKE ? OR created_by LIKE ?)")
		args = append(args, term, term, term)
	}

	whereClause := ""
	if len(whereParts) > 0 {
		whereClause = "WHERE " + strings.Join(whereParts, " AND ")
	}

	// 1. Compter le total
	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM invitations %s", whereClause)
	if err := h.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		slog.Error("Erreur comptage des invitations", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur lecture base de donnees"})
		return
	}

	links := resolvePortalLinks(h.cfg, h.db)
	baseURL := strings.TrimSpace(links.JellyGateURL)
	if baseURL == "" {
		baseURL = requestBaseURL(r)
	}

	authCfg, _ := h.db.GetAuthentikConfig()
	authentikEnabled := (h.cfg != nil && h.cfg.Authentik.Enabled) || authCfg.Enabled
	var authBaseURL string
	var flowSlug string
	if authentikEnabled {
		rawAuthURL := authCfg.URL
		if rawAuthURL == "" && h.cfg != nil {
			rawAuthURL = h.cfg.Authentik.URL
		}
		if rawAuthURL == "" && authCfg.IssuerURL != "" {
			rawAuthURL = authCfg.IssuerURL
		}
		authBaseURL = authentik.ResolveBaseURL(rawAuthURL)
		flowSlug = strings.TrimSpace(authCfg.EnrollmentFlowSlug)
		if flowSlug == "" && h.cfg != nil {
			flowSlug = strings.TrimSpace(h.cfg.Authentik.EnrollmentFlowSlug)
		}
		if flowSlug == "" {
			flowSlug = "default-enrollment-flow"
		}
	}

	// 2. Récupérer les données paginées
	offset := (page - 1) * limit
	query := fmt.Sprintf(`SELECT id, code, label, preferred_lang, max_uses, used_count, jellyfin_profile, profile_id, profile_snapshot, is_temporary, account_duration_days, expires_at, created_by, created_at, COALESCE(authentik_invitation_id, '') FROM invitations %s ORDER BY created_at DESC LIMIT ? OFFSET ?`, whereClause)

	queryArgs := append(args, limit, offset)
	rows, err := h.db.Query(query, queryArgs...)
	if err != nil {
		slog.Error("Erreur lecture des invitations", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de lecture de la base de donnees"})
		return
	}
	defer rows.Close()

	var invs []InvitationResponse
	for rows.Next() {
		var i InvitationResponse
		var label, profile, profileID, profileSnapshot, createdBy, preferredLang sql.NullString
		var rawExpiresAt interface{}
		var rawCreatedAt interface{}
		var authInvID sql.NullString

		err := rows.Scan(
			&i.ID, &i.Code, &label, &preferredLang, &i.MaxUses, &i.UsedCount,
			&profile, &profileID, &profileSnapshot, &i.IsTemporary, &i.AccountDurationDays,
			&rawExpiresAt, &createdBy, &rawCreatedAt, &authInvID,
		)
		if err != nil {
			slog.Error("Erreur scan invitation", "error", err)
			continue
		}

		i.Label = label.String
		i.PreferredLang = normalizeSupportedEmailLang(preferredLang.String)
		i.ProfileID = strings.TrimSpace(profileID.String)
		i.ExpiresAt = anyToDateString(rawExpiresAt)
		i.CreatedBy = createdBy.String
		i.CreatedAt = anyToDateString(rawCreatedAt)
		i.AuthentikInvitationID = strings.TrimSpace(authInvID.String)
		i.InviteURL = strings.TrimRight(baseURL, "/") + "/invite/" + i.Code
		if authentikEnabled && i.AuthentikInvitationID != "" && authBaseURL != "" {
			i.AuthentikEnrollmentURL = fmt.Sprintf("%s/if/flow/%s/?itoken=%s", authBaseURL, flowSlug, url.QueryEscape(i.AuthentikInvitationID))
		}

		if profile.String != "" {
			var p map[string]interface{}
			_ = json.Unmarshal([]byte(profile.String), &p)
			i.JellyfinProfile = p
		}
		if profileSnapshot.String != "" {
			var p map[string]interface{}
			_ = json.Unmarshal([]byte(profileSnapshot.String), &p)
			i.ProfileSnapshot = p
		}

		invs = append(invs, i)
	}

	totalPages := 1
	if total > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(limit)))
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"invitations": invs,
			"meta": map[string]interface{}{
				"total":       total,
				"page":        page,
				"limit":       limit,
				"total_pages": totalPages,
			},
		},
	})
}

// InvitationStats retourne des statistiques de parrainage par createur d'invitations.
func (h *AdminHandler) InvitationStats(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	cleanupClosedInvitationsIfEnabled(h.db)

	scope := "all"
	filterByCreator := ""
	if !sess.IsAdmin {
		var canInvite bool
		_ = h.db.QueryRow(`SELECT can_invite FROM users WHERE jellyfin_id = ?`, sess.UserID).Scan(&canInvite)
		if !canInvite {
			writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Vous n'avez pas l'autorisation d'acceder aux statistiques de parrainage"})
			return
		}
		scope = "mine"
		filterByCreator = sess.Username
	}

	now := time.Now()
	statsQuery := `
		SELECT created_by,
		       COUNT(1) AS created_links,
		       SUM(CASE WHEN (expires_at IS NULL OR expires_at > ?) AND (max_uses = 0 OR used_count < max_uses) THEN 1 ELSE 0 END) AS active_links,
		       SUM(CASE WHEN NOT ((expires_at IS NULL OR expires_at > ?) AND (max_uses = 0 OR used_count < max_uses)) THEN 1 ELSE 0 END) AS closed_links,
		       SUM(used_count) AS total_uses
		FROM invitations`
	statsArgs := []interface{}{now, now}
	if filterByCreator != "" {
		statsQuery += ` WHERE created_by = ?`
		statsArgs = append(statsArgs, filterByCreator)
	}
	statsQuery += ` GROUP BY created_by ORDER BY total_uses DESC, created_by ASC`

	rows, err := h.db.Query(statsQuery, statsArgs...)
	if err != nil {
		slog.Error("Erreur lecture statistiques invitations", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de lecture des statistiques"})
		return
	}
	defer rows.Close()

	statsBySponsor := make(map[string]*InvitationSponsorStats)
	for rows.Next() {
		var creator sql.NullString
		var createdLinks, activeLinks, closedLinks, totalUses sql.NullInt64
		if scanErr := rows.Scan(&creator, &createdLinks, &activeLinks, &closedLinks, &totalUses); scanErr != nil {
			continue
		}

		sponsorKey := strings.TrimSpace(creator.String)
		if sponsorKey == "" {
			sponsorKey = "(inconnu)"
		}

		statsBySponsor[sponsorKey] = &InvitationSponsorStats{
			Sponsor:      sponsorKey,
			CreatedLinks: int(createdLinks.Int64),
			ActiveLinks:  int(activeLinks.Int64),
			ClosedLinks:  int(closedLinks.Int64),
			TotalUses:    int(totalUses.Int64),
		}
	}

	convQuery := `
		SELECT i.created_by, COUNT(u.id) AS conversions
		FROM invitations i
		LEFT JOIN users u ON u.invited_by = i.code`
	convArgs := []interface{}{}
	if filterByCreator != "" {
		convQuery += ` WHERE i.created_by = ?`
		convArgs = append(convArgs, filterByCreator)
	}
	convQuery += ` GROUP BY i.created_by`

	convRows, err := h.db.Query(convQuery, convArgs...)
	if err == nil {
		defer convRows.Close()
		for convRows.Next() {
			var creator sql.NullString
			var conversions sql.NullInt64
			if scanErr := convRows.Scan(&creator, &conversions); scanErr != nil {
				continue
			}

			sponsorKey := strings.TrimSpace(creator.String)
			if sponsorKey == "" {
				sponsorKey = "(inconnu)"
			}
			if item, ok := statsBySponsor[sponsorKey]; ok {
				item.Conversions = int(conversions.Int64)
			}
		}
	}

	stats := make([]InvitationSponsorStats, 0, len(statsBySponsor))
	totalLinks := 0
	totalActive := 0
	totalClosed := 0
	totalUses := 0
	totalConversions := 0

	for _, item := range statsBySponsor {
		if item.CreatedLinks > 0 {
			item.ConversionRate = (float64(item.Conversions) / float64(item.CreatedLinks)) * 100
		}
		stats = append(stats, *item)

		totalLinks += item.CreatedLinks
		totalActive += item.ActiveLinks
		totalClosed += item.ClosedLinks
		totalUses += item.TotalUses
		totalConversions += item.Conversions
	}

	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Conversions == stats[j].Conversions {
			return strings.ToLower(stats[i].Sponsor) < strings.ToLower(stats[j].Sponsor)
		}
		return stats[i].Conversions > stats[j].Conversions
	})

	globalRate := 0.0
	if totalLinks > 0 {
		globalRate = (float64(totalConversions) / float64(totalLinks)) * 100
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"scope":           scope,
			"total_links":     totalLinks,
			"active_links":    totalActive,
			"closed_links":    totalClosed,
			"total_uses":      totalUses,
			"conversions":     totalConversions,
			"conversion_rate": globalRate,
			"by_sponsor":      stats,
			"generated_at":    now.Format(time.RFC3339),
		},
	})
}

// CreateInvitation crée un nouveau lien d'invitation avec un jeton robuste et logiques complexes (JFA-GO).
func (h *AdminHandler) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())

	var req CreateInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}

	if req.MaxUses < 0 {
		req.MaxUses = 0
	}
	if req.ExpiresInDays < 0 {
		req.ExpiresInDays = 0
	}
	if req.UserExpiryDays < 0 {
		req.UserExpiryDays = 0
	}
	if req.DisableAfterDays < 0 {
		req.DisableAfterDays = 0
	}
	if req.AccountDurationDays < 0 {
		req.AccountDurationDays = 0
	}
	req.PreferredLang = normalizeSupportedEmailLang(req.PreferredLang)

	inviteCfg, err := h.db.GetInvitationProfileConfig()
	if err != nil {
		slog.Error("Erreur chargement config profil invitation", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de lecture du profil d'invitation"})
		return
	}

	limits, err := h.resolveInvitationCreatorLimits(sess, inviteCfg)
	if err != nil {
		slog.Error("Erreur resolution limites invitations", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible de charger les limites de creation"})
		return
	}

	if !sess.IsAdmin && !limits.CanInvite {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Vous n'avez pas l'autorisation de créer des invitations"})
		return
	}

	if (req.IgnorePresetLinkExpiry || req.IgnorePresetUserExpiry) && !limits.AllowIgnoreLimits {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Le contournement des limites preset est reserve aux administrateurs"})
		return
	}

	if req.NewUserCanInvite && !sess.IsAdmin && !limits.AllowGrant {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "La delegation du droit d'invitation est reservee par la configuration admin"})
		return
	}

	targetPresetID := strings.TrimSpace(limits.TargetPresetID)
	if sess.IsAdmin && strings.TrimSpace(req.PolicyPresetID) != "" {
		targetPresetID = strings.TrimSpace(req.PolicyPresetID)
	} else if !sess.IsAdmin && strings.TrimSpace(req.PolicyPresetID) != "" {
		requestedPresetID := strings.TrimSpace(strings.ToLower(req.PolicyPresetID))
		if !presetIDAllowed(requestedPresetID, limits.AllowedTargetPresetIDs) {
			writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Ce profil cible n'est pas autorise par votre profil"})
			return
		}
		targetPresetID = requestedPresetID
	}

	if !sess.IsAdmin {
		if strings.TrimSpace(req.GroupName) != "" ||
			strings.TrimSpace(req.ForcedUsername) != "" ||
			strings.TrimSpace(req.TemplateUserID) != "" ||
			req.UsernameMinLen != nil || req.UsernameMaxLen != nil ||
			req.PasswordMinLen != nil || req.PasswordMaxLen != nil ||
			req.RequireUpper != nil || req.RequireLower != nil ||
			req.RequireDigit != nil || req.RequireSpecial != nil ||
			strings.TrimSpace(req.ExpiryAction) != "" || req.DeleteAfterDays != nil {
			writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Les parametres de securite avancés sont reserves aux administrateurs"})
			return
		}
	}

	var preset *config.JellyfinPolicyPreset
	if targetPresetID != "" {
		resolvedPreset, err := h.getJellyfinPresetByID(targetPresetID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Preset cible Jellyfin introuvable"})
			return
		}
		preset = resolvedPreset
	}

	targetPresetName := ""
	if preset != nil {
		targetPresetName = preset.Name
	}

	code, err := generateSecureToken(16)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de generation du jeton"})
		return
	}

	now := time.Now()
	var expiresAt interface{}
	var expiresAtResponse interface{}

	effectiveLinkValidityDays := req.ExpiresInDays
	if effectiveLinkValidityDays <= 0 && !req.IgnorePresetLinkExpiry {
		effectiveLinkValidityDays = limits.LinkValidityDays
	}

	if strings.TrimSpace(req.ExpiresAt) != "" {
		preciseExpiry, err := parseInvitationDateTimeInput(strings.TrimSpace(req.ExpiresAt))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Date d'expiration invalide"})
			return
		}
		if preciseExpiry.Before(now) {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "La date d'expiration doit etre dans le futur"})
			return
		}
		expiresAt = preciseExpiry
		expiresAtResponse = preciseExpiry.Format(time.RFC3339)
	} else if effectiveLinkValidityDays > 0 {
		resolvedExpiry := now.AddDate(0, 0, effectiveLinkValidityDays)
		expiresAt = resolvedExpiry
		expiresAtResponse = resolvedExpiry.Format(time.RFC3339)
	}

	profile := inviteProfileFromPolicyPreset(preset)
	if sess.IsAdmin {
		profile.GroupName = strings.TrimSpace(req.GroupName)
		profile.ForcedName = strings.TrimSpace(req.ForcedName)
		profile.ForcedUsername = strings.TrimSpace(req.ForcedUsername)
		profile.TemplateUserID = strings.TrimSpace(req.TemplateUserID)
		if req.UsernameMinLen != nil {
			profile.UsernameMinLength = *req.UsernameMinLen
		}
		if req.UsernameMaxLen != nil {
			profile.UsernameMaxLength = *req.UsernameMaxLen
		}
		if req.PasswordMinLen != nil {
			profile.PasswordMinLength = *req.PasswordMinLen
		}
		if req.PasswordMaxLen != nil {
			profile.PasswordMaxLength = *req.PasswordMaxLen
		}
		if req.RequireUpper != nil {
			profile.PasswordRequireUpper = *req.RequireUpper
		}
		if req.RequireLower != nil {
			profile.PasswordRequireLower = *req.RequireLower
		}
		if req.RequireDigit != nil {
			profile.PasswordRequireDigit = *req.RequireDigit
		}
		if req.RequireSpecial != nil {
			profile.PasswordRequireSpecial = *req.RequireSpecial
		}
	}

	if req.Libraries != nil && sess.IsAdmin {
		profile.EnabledFolderIDs = req.Libraries
		profile.EnableAllFolders = len(req.Libraries) == 0
	}
	if req.EnableDownloads && sess.IsAdmin {
		profile.EnableDownload = true
	}

	// Le droit de parrainage est strictement déterminé par le profil / groupe sélectionné
	if preset != nil {
		profile.CanInvite = preset.CanInvite || preset.CanCreateInvitations
	} else {
		profile.CanInvite = false
	}
	profile.RequireEmail = inviteCfg.RequireEmail

	profile.IsTemporary = req.IsTemporary
	if preset != nil && preset.IsTemporary {
		profile.IsTemporary = true
	}

	effectiveDuration := req.AccountDurationDays
	if preset != nil && preset.IsTemporary {
		effectiveDuration = preset.DisableAfterDays
		if effectiveDuration <= 0 {
			effectiveDuration = preset.DefaultAccountDurationDays
		}
	}
	if profile.IsTemporary {
		if !sess.IsAdmin && !limits.CanCreateTemporaryInvitations {
			writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Votre profil ne permet pas de generer des invitations temporaires"})
			return
		}
		if !sess.IsAdmin && preset != nil && !presetIDAllowed(preset.ID, limits.AllowedTemporaryPresetIDs) {
			writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Ce profil temporaire n'est pas autorise"})
			return
		}
		if effectiveDuration <= 0 {
			effectiveDuration = limits.DefaultTemporaryDurationDays
		}
		if !sess.IsAdmin && limits.MaxTemporaryDurationDays > 0 && effectiveDuration > limits.MaxTemporaryDurationDays {
			effectiveDuration = limits.MaxTemporaryDurationDays
		}
		if effectiveDuration <= 0 {
			effectiveDuration = 7 // valeur par défaut
		}
		profile.AccountDurationDays = effectiveDuration
		profile.DisableAfterDays = effectiveDuration
		profile.UserExpiryDays = effectiveDuration
	}

	if sess.IsAdmin {
		if strings.TrimSpace(req.ExpiryAction) != "" {
			profile.ExpiryAction = normalizeExpiryAction(req.ExpiryAction)
		}
		if req.DeleteAfterDays != nil {
			profile.DeleteAfterDays = *req.DeleteAfterDays
		}
	}

	effectiveUserExpiryDays := req.UserExpiryDays
	if effectiveUserExpiryDays <= 0 && !req.IgnorePresetUserExpiry {
		effectiveUserExpiryDays = limits.UserExpiryDays
	}

	if sess.IsAdmin && strings.TrimSpace(req.UserExpiresAt) != "" {
		preciseUserExpiry, err := parseInvitationDateTimeInput(strings.TrimSpace(req.UserExpiresAt))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Date d'expiration finale utilisateur invalide"})
			return
		}
		if preciseUserExpiry.Before(now) {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "La date d'expiration finale doit etre dans le futur"})
			return
		}
		profile.UserExpiresAt = preciseUserExpiry.Format("2006-01-02 15:04:05")
	} else if effectiveUserExpiryDays > 0 {
		profile.UserExpiryDays = effectiveUserExpiryDays
		profile.DisableAfterDays = effectiveUserExpiryDays
	}

	if profile.ExpiryAction == "" {
		profile.ExpiryAction = normalizeExpiryAction(inviteCfg.ExpiryAction)
	}
	if profile.DeleteAfterDays < 0 {
		profile.DeleteAfterDays = inviteCfg.DeleteAfterDays
	}

	profileJSON, err := json.Marshal(profile)
	if err != nil {
		slog.Error("Erreur serialisation snapshot invitation", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur serveur"})
		return
	}

	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = "Invitation cree par " + sess.Username
	}

	maxUses := req.MaxUses
	if !sess.IsAdmin && limits.MaxUses > 0 && (maxUses == 0 || maxUses > limits.MaxUses) {
		maxUses = limits.MaxUses
	}

	// Invoquer l'API Authentik pour créer une invitation Stage si Authentik est configuré
	var authentikInvID string
	authCfg, _ := h.db.GetAuthentikConfig()
	authentikEnabled := (h.cfg != nil && h.cfg.Authentik.Enabled) || authCfg.Enabled
	effectiveAuth := h.getEffectiveAuthentikClient()

	if effectiveAuth != nil && authentikEnabled {
		flowSlug := strings.TrimSpace(authCfg.EnrollmentFlowSlug)
		if flowSlug == "" && h.cfg != nil {
			flowSlug = strings.TrimSpace(h.cfg.Authentik.EnrollmentFlowSlug)
		}
		if flowSlug == "" {
			flowSlug = "default-enrollment-flow"
		}

		var targetGroups []string
		groupName := strings.TrimSpace(profile.GroupName)
		if groupName != "" {
			targetGroups = append(targetGroups, groupName)
		} else {
			jfGroup := strings.TrimSpace(authCfg.JellyfinUserGroup)
			if jfGroup == "" && h.cfg != nil {
				jfGroup = strings.TrimSpace(h.cfg.Authentik.JellyfinUserGroup)
			}
			if jfGroup == "" {
				jfGroup = "jellyfin-users"
			}
			targetGroups = append(targetGroups, jfGroup)
		}

		isRecursive := sess.CanInviteRecursive
		if !isRecursive {
			invRecGroup := strings.TrimSpace(authCfg.InvitersRecursiveGroup)
			if invRecGroup == "" {
				invRecGroup = "jellygate-inviters-recursive"
			}
			for _, g := range sess.Groups {
				if strings.EqualFold(g, invRecGroup) || strings.EqualFold(g, "jellygate-inviters-recursive") {
					isRecursive = true
					break
				}
			}
		}

		if req.NewUserCanInvite || isRecursive {
			invGroup := strings.TrimSpace(authCfg.InvitersGroup)
			if invGroup == "" {
				invGroup = "jellygate-inviters"
			}
			targetGroups = append(targetGroups, invGroup)
		}

		fixedData := map[string]interface{}{
			"source":                "JellyGate",
			"created_by":            "JellyGate",
			"sponsor":               sess.Username,
			"code":                  code,
			"invitation_code":       code,
			"groups":                targetGroups,
			"preset_id":             targetPresetID,
			"target_preset_name":    targetPresetName,
			"is_temporary":          profile.IsTemporary,
			"account_duration_days": profile.AccountDurationDays,
		}
		if strings.TrimSpace(req.ForcedUsername) != "" {
			fixedData["username"] = strings.TrimSpace(req.ForcedUsername)
		}
		if strings.TrimSpace(req.ForcedName) != "" {
			fixedData["name"] = strings.TrimSpace(req.ForcedName)
		}
		sendToEmailCandidate := strings.TrimSpace(req.SendToEmail)
		if sendToEmailCandidate == "" {
			sendToEmailCandidate = strings.TrimSpace(req.Email)
		}
		if sendToEmailCandidate != "" {
			fixedData["email"] = sendToEmailCandidate
		}

		var stageExpiry time.Time
		if t, ok := expiresAt.(time.Time); ok {
			stageExpiry = t
		} else if expiresAtResponse != nil {
			if t, tErr := time.Parse(time.RFC3339, fmt.Sprint(expiresAtResponse)); tErr == nil {
				stageExpiry = t
			}
		}

		tokenName := fmt.Sprintf("jellygate-invite-%s", code)
		invID, authErr := effectiveAuth.CreateInvitationStageToken(r.Context(), tokenName, stageExpiry, fixedData, maxUses == 1, flowSlug)
		if authErr == nil && strings.TrimSpace(invID) != "" {
			authentikInvID = strings.TrimSpace(invID)
			slog.Info("Jeton invitation Authentik créé avec succès", "code", code, "creator", sess.Username, "authentik_invitation_id", authentikInvID, "expires", stageExpiry)
		} else if authErr != nil {
			slog.Warn("Création token invitation Authentik échouée (fallback local)", "code", code, "creator", sess.Username, "error", authErr)
		}
	}

	_, err = h.db.Exec(`
		INSERT INTO invitations (code, label, preferred_lang, max_uses, used_count, jellyfin_profile, expires_at, created_by, authentik_invitation_id, profile_id, profile_snapshot, is_temporary, account_duration_days)
		VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?)`,
		code, label, req.PreferredLang, maxUses, string(profileJSON), expiresAt, sess.Username, authentikInvID,
		strings.TrimSpace(strings.ToLower(profile.PresetID)), string(profileJSON), profile.IsTemporary, profile.AccountDurationDays)

	if err != nil {
		slog.Error("Erreur insertion invitation SQLite", "error", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur de base de donnees"})
		return
	}

	_ = h.db.LogAction("invite.created", sess.Username, code, fmt.Sprintf(`{"preset_id":"%s","max_uses":%d}`, targetPresetID, maxUses))

	links := resolvePortalLinks(h.cfg, h.db)
	baseURL := strings.TrimSpace(links.JellyGateURL)
	if baseURL == "" {
		baseURL = requestBaseURL(r)
	}
	inviteURL := strings.TrimRight(baseURL, "/") + "/invite/" + code

	var inviteExpiryDate string
	if expiresAtResponse != nil {
		if t, tErr := time.Parse(time.RFC3339, fmt.Sprint(expiresAtResponse)); tErr == nil {
			inviteExpiryDate = emailTime(t)
		}
	}

	sendToEmail := strings.TrimSpace(req.SendToEmail)
	if sendToEmail == "" {
		sendToEmail = strings.TrimSpace(req.Email)
	}
	if sendToEmail != "" {
		if h.mailer != nil {
			customMessage := strings.TrimSpace(req.EmailMessage)
			inviteeName := strings.TrimSpace(req.ForcedUsername)
			if inviteeName == "" {
				inviteeName = "invité"
			}

			go func(recipient, username, expiryDate, customBody, invitationLang string) {
				emailCfg, usedLang, cfgErr := loadEmailTemplatesForLanguage(h.db, invitationLang, emailLanguageContext{})
				if cfgErr != nil {
					emailCfg = config.DefaultEmailTemplatesForLanguage(usedLang)
				}
				sections := []string{emailCfg.Invitation}
				if expiryDate != "" && !emailCfg.DisableInviteExpiryEmail {
					sections = append(sections, emailCfg.InviteExpiry)
				}
				if !emailCfg.DisablePreSignupHelpEmail {
					sections = append(sections, emailCfg.PreSignupHelp)
				}
				combinedTemplate := joinTemplateSections(sections...)

				if strings.TrimSpace(customBody) != "" {
					combinedTemplate = joinTemplateSections(combinedTemplate, "{{.Message}}")
				}

				if combinedTemplate == "" {
					combinedTemplate = "Bonjour,\n\nVous êtes invité à rejoindre notre serveur. Cliquez sur ce lien pour créer votre compte : {{.InviteLink}}"
				}

				emailData := map[string]string{
					"InviteLink":         inviteURL,
					"InviteURL":          inviteURL,
					"InviteCode":         code,
					"HelpURL":            strings.TrimRight(baseURL, "/"),
					"JellyGateURL":       strings.TrimRight(baseURL, "/"),
					"Username":           username,
					"JellyfinURL":        links.JellyfinURL,
					"JellyfinServerName": links.JellyfinServerName,
					"JellyseerrURL":      links.JellyseerrURL,
					"JellyTrackURL":      links.JellyTrackURL,
				}
				if expiryDate != "" {
					emailData["ExpiryDate"] = expiryDate
				}
				if strings.TrimSpace(customBody) != "" {
					emailData["Message"] = customBody
				}

				subject := firstNonEmpty(emailCfg.InvitationSubject, emailCfg.InviteExpirySubject, config.DefaultEmailTemplatesForLanguage(usedLang).InvitationSubject)
				errMail := sendTemplateIfConfigured(h.mailer, recipient, subject, usedLang, "invitation", combinedTemplate, emailCfg, emailData)
				if errMail != nil {
					slog.Error("Erreur d'envoi SMTP (Invitation)", "email", recipient, "error", errMail)
					_ = h.db.LogAction("invite.email.failed", sess.Username, code, errMail.Error())
					_ = h.db.LogSecurityEvent(database.SecurityEvent{
						Category:  "smtp",
						EventType: "invite.email.failed",
						Severity:  "warning",
						Actor:     sess.Username,
						Target:    code,
						Message:   "Erreur SMTP invitation",
						Metadata:  errMail.Error(),
					})
				}
			}(sendToEmail, inviteeName, inviteExpiryDate, customMessage, req.PreferredLang)
		} else {
			slog.Warn("Option e-mail cochée pour l'invitation, mais le serveur SMTP n'est pas configuré.")
		}
	}

	var authentikEnrollmentURL string
	if authentikEnabled && authentikInvID != "" {
		rawAuthURL := authCfg.URL
		if rawAuthURL == "" && h.cfg != nil {
			rawAuthURL = h.cfg.Authentik.URL
		}
		if rawAuthURL == "" && authCfg.IssuerURL != "" {
			rawAuthURL = authCfg.IssuerURL
		}
		authBaseURL := authentik.ResolveBaseURL(rawAuthURL)
		flowSlug := strings.TrimSpace(authCfg.EnrollmentFlowSlug)
		if flowSlug == "" && h.cfg != nil {
			flowSlug = strings.TrimSpace(h.cfg.Authentik.EnrollmentFlowSlug)
		}
		if effectiveAuth != nil {
			if discovered := effectiveAuth.GetEnrollmentFlowSlug(r.Context(), flowSlug); discovered != "" {
				flowSlug = discovered
			}
		}
		if flowSlug == "" {
			flowSlug = "default-enrollment-flow"
		}
		if authBaseURL != "" {
			authentikEnrollmentURL = fmt.Sprintf("%s/if/flow/%s/?itoken=%s", authBaseURL, flowSlug, url.QueryEscape(authentikInvID))
		}
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Invitation générée avec succès",
		Data: map[string]interface{}{
			"code":                     code,
			"url":                      inviteURL,
			"invite_url":               inviteURL,
			"authentik_invitation_id":  authentikInvID,
			"authentik_enrollment_url": authentikEnrollmentURL,
			"authentik_enabled":        authentikEnabled,
			"target_preset_id":         targetPresetID,
			"target_preset_name":       targetPresetName,
			"max_uses":                 maxUses,
			"expires_at":               expiresAtResponse,
			"is_temporary":             profile.IsTemporary,
			"account_duration_days":    profile.AccountDurationDays,
		},
	})
}

// DeleteInvitation supprime brutalement l'invitation SQLite
func (h *AdminHandler) DeleteInvitation(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	invID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID invalide"})
		return
	}

	var authInvID sql.NullString
	_ = h.db.QueryRow(`SELECT authentik_invitation_id FROM invitations WHERE id = ?`, invID).Scan(&authInvID)
	if authInvID.Valid && authInvID.String != "" && h.authClient != nil {
		if errDel := h.authClient.DeleteInvitationStageToken(r.Context(), authInvID.String); errDel != nil {
			slog.Warn("Suppression du token invitation Authentik échouée", "token_id", authInvID.String, "error", errDel)
		}
	}

	var errDB error
	if sess.IsAdmin {
		_, errDB = h.db.Exec(`DELETE FROM invitations WHERE id = ?`, invID)
	} else {
		// Security: Le standard user ne supprime que ses propres liens
		result, errDBQuery := h.db.Exec(`DELETE FROM invitations WHERE id = ? AND created_by = ?`, invID, sess.Username)
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
		slog.Error("Erreur suppression invitation", "id", invID, "error", errDB)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur DB"})
		return
	}

	if err := h.db.LogAction("invite.deleted", sess.Username, fmt.Sprintf("%d", invID), ""); err != nil {
		slog.Warn("Erreur journalisation suppression invitation", "error", err)
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Lien d'invitation détruit",
	})
}

func anyToDateString(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return ""
	case time.Time:
		return val.Format(time.RFC3339)
	case []byte:
		return strings.TrimSpace(string(val))
	case string:
		return strings.TrimSpace(val)
	default:
		return strings.TrimSpace(fmt.Sprint(val))
	}
}

func requestBaseURL(r *http.Request) string {
	if r == nil {
		return ""
	}

	scheme := "http"
	if jgmw.RequestIsHTTPS(r, "") {
		scheme = "https"
	}

	host := strings.TrimSpace(r.Host)
	if host == "" {
		return ""
	}

	return fmt.Sprintf("%s://%s", scheme, host)
}

func parseInvitationDateTimeInput(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("date vide")
	}

	if parsed, err := time.Parse("2006-01-02T15:04", trimmed); err == nil {
		return parsed, nil
	}
	if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return parsed, nil
	}

	return time.Time{}, fmt.Errorf("format de date invalide")
}

func startOfLocalDay(t time.Time) time.Time {
	lt := t.Local()
	y, m, d := lt.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, lt.Location())
}

func startOfLocalWeek(t time.Time) time.Time {
	day := startOfLocalDay(t)
	offset := (int(day.Weekday()) + 6) % 7 // Monday=0 ... Sunday=6
	return day.AddDate(0, 0, -offset)
}

func startOfLocalMonth(t time.Time) time.Time {
	lt := t.Local()
	y, m, _ := lt.Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, lt.Location())
}

func normalizedPresetIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		normalized := strings.TrimSpace(strings.ToLower(id))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func presetIDAllowed(id string, allowed []string) bool {
	id = strings.TrimSpace(strings.ToLower(id))
	if id == "" {
		return false
	}
	for _, allowedID := range allowed {
		if strings.EqualFold(strings.TrimSpace(allowedID), id) {
			return true
		}
	}
	return false
}

func presetInviteQuotaMonth(preset *config.JellyfinPolicyPreset) int {
	if preset == nil {
		return 0
	}
	if preset.InviteQuotaMonth > 0 {
		return preset.InviteQuotaMonth
	}
	if preset.InviteQuota > 0 {
		return preset.InviteQuota
	}
	return 0
}

func presetInviteLinkValidityDays(preset *config.JellyfinPolicyPreset) int {
	if preset == nil {
		return 0
	}
	if preset.InviteLinkValidityDays > 0 {
		return preset.InviteLinkValidityDays
	}
	if preset.InviteMaxLinkHours > 0 {
		return (preset.InviteMaxLinkHours + 23) / 24
	}
	return 0
}

// SyncAuthentikInvitations reconciles invitations between JellyGate and Authentik.
func (h *AdminHandler) SyncAuthentikInvitations(w http.ResponseWriter, r *http.Request) {
	client := h.getEffectiveAuthentikClient()
	if client == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Authentik n'est pas configuré",
		})
		return
	}

	authCfg, _ := h.db.GetAuthentikConfig()
	flowSlug := strings.TrimSpace(authCfg.EnrollmentFlowSlug)
	if flowSlug == "" {
		flowSlug = "default-enrollment-flow"
	}

	// 1. Lister les invitations Authentik actuelles
	tokens, err := client.ListInvitationStageTokens(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Impossible de lister les tokens Authentik: %v", err),
		})
		return
	}

	tokenByPK := make(map[string]authentik.InvitationTokenResponse, len(tokens))
	tokenByCode := make(map[string]authentik.InvitationTokenResponse, len(tokens))
	for _, tok := range tokens {
		if tok.PK != "" {
			tokenByPK[tok.PK] = tok
		}
		if tok.FixedData != nil {
			if c, ok := tok.FixedData["code"].(string); ok && strings.TrimSpace(c) != "" {
				tokenByCode[strings.TrimSpace(c)] = tok
			} else if c, ok := tok.FixedData["invitation_code"].(string); ok && strings.TrimSpace(c) != "" {
				tokenByCode[strings.TrimSpace(c)] = tok
			}
		}
	}

	rows, err := h.db.Query(`
		SELECT id, code, label, max_uses, used_count, jellyfin_profile, expires_at, created_by, authentik_invitation_id, profile_id, is_temporary, account_duration_days
		FROM invitations
	`)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}
	defer rows.Close()

	now := time.Now()
	recreated := 0
	cleaned := 0
	totalChecked := 0

	for rows.Next() {
		totalChecked++
		var id int64
		var code, label, jfProfile, createdBy, profileID string
		var maxUses, usedCount, accountDurationDays int
		var expiresAt sql.NullTime
		var authentikID sql.NullString
		var isTemporary bool

		if err := rows.Scan(&id, &code, &label, &maxUses, &usedCount, &jfProfile, &expiresAt, &createdBy, &authentikID, &profileID, &isTemporary, &accountDurationDays); err != nil {
			continue
		}

		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}

		isExpired := expiresAt.Valid && expiresAt.Time.Before(now)
		isExhausted := maxUses > 0 && usedCount >= maxUses
		isActive := !isExpired && !isExhausted

		curAuthID := strings.TrimSpace(authentikID.String)

		if isActive {
			tokenExists := false
			if curAuthID != "" {
				if _, ok := tokenByPK[curAuthID]; ok {
					tokenExists = true
				}
			}
			if !tokenExists {
				if tok, ok := tokenByCode[code]; ok && tok.PK != "" {
					tokenExists = true
					_, _ = h.db.Exec(`UPDATE invitations SET authentik_invitation_id = ? WHERE id = ?`, tok.PK, id)
				}
			}

			// Si le token n'existe pas dans Authentik, le recréer automatiquement
			if !tokenExists {
				var targetGroups []string
				jfGroup := strings.TrimSpace(authCfg.JellyfinUserGroup)
				if jfGroup == "" {
					jfGroup = "jellyfin-users"
				}
				targetGroups = append(targetGroups, jfGroup)

				fixedData := map[string]interface{}{
					"source":                "JellyGate",
					"created_by":            "JellyGate",
					"created_by_app":        "JellyGate",
					"invitation_code":       code,
					"code":                  code,
					"sponsor":               createdBy,
					"groups":                targetGroups,
					"preset_id":             profileID,
					"is_temporary":          isTemporary,
					"account_duration_days": accountDurationDays,
				}

				var stageExpiry time.Time
				if expiresAt.Valid {
					stageExpiry = expiresAt.Time
				}

				tokenName := fmt.Sprintf("jellygate-%s", code)
				newTokPK, tokErr := client.CreateInvitationStageToken(r.Context(), tokenName, stageExpiry, fixedData, maxUses == 1, flowSlug)
				if tokErr == nil && strings.TrimSpace(newTokPK) != "" {
					newTokPK = strings.TrimSpace(newTokPK)
					_, _ = h.db.Exec(`UPDATE invitations SET authentik_invitation_id = ? WHERE id = ?`, newTokPK, id)
					_ = h.db.LogAction("invite.reconciled", "admin", code, fmt.Sprintf("Jeton Authentik recréé avec succès (PK: %s)", newTokPK))
					recreated++
				} else {
					slog.Warn("Admin: échec recréation token Authentik", "code", code, "error", tokErr)
				}
			}
		} else {
			// L'invitation est expirée ou épuisée : nettoyer dans Authentik
			if curAuthID != "" {
				if _, ok := tokenByPK[curAuthID]; ok {
					if delErr := client.DeleteInvitationStageToken(r.Context(), curAuthID); delErr == nil {
						_ = h.db.LogAction("invite.authentik_cleanup", "admin", code, fmt.Sprintf("Jeton Authentik expiré supprimé (PK: %s)", curAuthID))
						cleaned++
					}
				}
			}
		}
	}

	actor := "admin"
	if sess := session.FromContext(r.Context()); sess != nil && sess.Username != "" {
		actor = sess.Username
	}
	_ = h.db.LogAction("invite.sync_authentik", actor, "invitations", fmt.Sprintf("Synchronisation Authentik manuelle : %d vérifiées, %d recréées, %d nettoyées", totalChecked, recreated, cleaned))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"total_checked": totalChecked,
		"recreated":     recreated,
		"cleaned":       cleaned,
		"message":       fmt.Sprintf("Synchronisation Authentik terminée : %d vérifiées, %d recréées, %d nettoyées", totalChecked, recreated, cleaned),
	})
}
