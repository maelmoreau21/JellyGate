package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	jgldap "github.com/maelmoreau21/JellyGate/internal/ldap"
)

type ldapDryRunRequest struct {
	config.LDAPConfig
	LDAP          *config.LDAPConfig          `json:"ldap"`
	GroupMappings []config.GroupPolicyMapping `json:"group_mappings"`
	Limit         int                         `json:"limit"`
}

type ldapDryRunLookupItem struct {
	Mapping config.GroupPolicyMapping
	User    jgldap.UserEntry
}

type ldapDryRunUserPreview struct {
	Username       string                    `json:"username"`
	DisplayName    string                    `json:"display_name,omitempty"`
	Email          string                    `json:"email,omitempty"`
	DN             string                    `json:"dn,omitempty"`
	UID            string                    `json:"uid,omitempty"`
	IsDisabled     bool                      `json:"is_disabled"`
	PolicyPresetID string                    `json:"policy_preset_id,omitempty"`
	GroupName      string                    `json:"group_name,omitempty"`
	Mapping        config.GroupPolicyMapping `json:"mapping"`
	LocalUserID    int64                     `json:"local_user_id,omitempty"`
	JellyfinUserID string                    `json:"jellyfin_user_id,omitempty"`
	Reason         string                    `json:"reason,omitempty"`
	Blocking       bool                      `json:"blocking,omitempty"`
}

type ldapDryRunLocalUser struct {
	ID         int64
	Username   string
	JellyfinID string
	LDAPDN     string
	PresetID   string
}

var runLDAPDryRunLookup = defaultLDAPDryRunLookup

func defaultLDAPDryRunLookup(cfg config.LDAPConfig, mappings []config.GroupPolicyMapping, limit int) ([]ldapDryRunLookupItem, []string, error) {
	client := jgldap.New(cfg)
	items := make([]ldapDryRunLookupItem, 0)
	warnings := make([]string, 0)

	for _, mapping := range normalizeDryRunMappings(mappings) {
		groupRef := strings.TrimSpace(mapping.LDAPGroupDN)
		if groupRef == "" {
			groupRef = strings.TrimSpace(mapping.GroupName)
		}
		if groupRef == "" {
			continue
		}

		members, err := client.GetGroupMembers(groupRef)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %s", mapping.GroupName, err.Error()))
			continue
		}
		for _, member := range members {
			if strings.TrimSpace(member.Username) == "" {
				continue
			}
			items = append(items, ldapDryRunLookupItem{Mapping: mapping, User: member})
			if limit > 0 && len(items) >= limit {
				return items, warnings, nil
			}
		}
	}

	return items, warnings, nil
}

func normalizeDryRunMappings(mappings []config.GroupPolicyMapping) []config.GroupPolicyMapping {
	normalized := make([]config.GroupPolicyMapping, 0, len(mappings))
	for _, mapping := range mappings {
		groupName := strings.TrimSpace(mapping.GroupName)
		presetID := strings.TrimSpace(strings.ToLower(mapping.PolicyPresetID))
		if groupName == "" || presetID == "" {
			continue
		}
		source := strings.TrimSpace(strings.ToLower(mapping.Source))
		if source != "ldap" {
			continue
		}
		normalized = append(normalized, config.GroupPolicyMapping{
			GroupName:      groupName,
			Source:         "ldap",
			LDAPGroupDN:    strings.TrimSpace(mapping.LDAPGroupDN),
			PolicyPresetID: presetID,
			Priority:       mapping.Priority,
		})
	}
	return normalized
}

// LDAPDryRun simule les mappings LDAP sans ecriture LDAP, Jellyfin ou base locale.
func (h *SettingsHandler) LDAPDryRun(w http.ResponseWriter, r *http.Request) {
	if !h.ensureAdmin(w, r) {
		return
	}

	var input ldapDryRunRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "JSON invalide : " + err.Error()})
		return
	}

	ldapCfg := input.LDAPConfig
	if input.LDAP != nil {
		ldapCfg = *input.LDAP
	}
	h.normalizeLDAPInput(&ldapCfg)
	if err := validateLDAPMinimalConfig(ldapCfg); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}

	mappings := input.GroupMappings
	if len(mappings) == 0 {
		var err error
		mappings, err = h.db.GetGroupPolicyMappings()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur lecture mappings LDAP"})
			return
		}
	}
	mappings = normalizeDryRunMappings(mappings)
	if len(mappings) == 0 {
		writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Dry-run LDAP pret", Data: map[string]interface{}{
			"summary":      map[string]int{"mappings": 0, "would_create": 0, "would_sync": 0, "conflicts": 0, "warnings": 0},
			"mappings":     []config.GroupPolicyMapping{},
			"would_create": []ldapDryRunUserPreview{},
			"would_sync":   []ldapDryRunUserPreview{},
			"conflicts":    []ldapDryRunUserPreview{},
			"warnings":     []string{"Aucun mapping LDAP actif"},
		}})
		return
	}

	items, warnings, err := runLDAPDryRunLookup(ldapCfg, mappings, input.Limit)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Dry-run LDAP impossible: " + err.Error()})
		return
	}

	localUsers := h.indexLocalUsersForLDAPDryRun()
	jellyfinUsers := h.indexJellyfinUsersForLDAPDryRun()

	wouldCreate := make([]ldapDryRunUserPreview, 0)
	wouldSync := make([]ldapDryRunUserPreview, 0)
	conflicts := make([]ldapDryRunUserPreview, 0)
	seenLDAPUsers := map[string]ldapDryRunUserPreview{}

	for _, item := range items {
		preview := ldapDryRunPreviewFromLookup(item)
		key := strings.ToLower(preview.Username)
		if key == "" {
			continue
		}

		if previous, exists := seenLDAPUsers[key]; exists {
			preview.Reason = fmt.Sprintf("Utilisateur present dans plusieurs mappings: %s et %s", previous.Mapping.GroupName, preview.Mapping.GroupName)
			preview.Blocking = true
			conflicts = append(conflicts, preview)
			continue
		}
		seenLDAPUsers[key] = preview

		if preview.IsDisabled {
			preview.Reason = "Compte LDAP desactive"
			warnings = append(warnings, fmt.Sprintf("%s: compte LDAP desactive", preview.Username))
		}

		if local, ok := localUsers[key]; ok {
			preview.LocalUserID = local.ID
			preview.JellyfinUserID = local.JellyfinID
			if strings.TrimSpace(local.LDAPDN) != "" && !strings.EqualFold(strings.TrimSpace(local.LDAPDN), strings.TrimSpace(preview.DN)) {
				preview.Reason = "Un utilisateur JellyGate existe deja avec un DN LDAP different"
				preview.Blocking = true
				conflicts = append(conflicts, preview)
				continue
			}
			preview.Reason = "Utilisateur existant a synchroniser"
			wouldSync = append(wouldSync, preview)
			continue
		}

		if jfUser, ok := jellyfinUsers[key]; ok {
			preview.JellyfinUserID = jfUser.ID
			preview.Reason = "Un compte Jellyfin existe deja sans correspondance JellyGate"
			preview.Blocking = true
			conflicts = append(conflicts, preview)
			continue
		}

		preview.Reason = "Utilisateur pret a creer"
		wouldCreate = append(wouldCreate, preview)
	}

	blocking := 0
	for _, conflict := range conflicts {
		if conflict.Blocking {
			blocking++
		}
	}

	if blocking > 0 {
		_ = h.db.LogSecurityEvent(databaseSecurityDryRunEvent("ldap.dry_run.conflicts", "warning", fmt.Sprintf("%d conflit(s) bloquant(s)", blocking)))
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Dry-run LDAP pret", Data: map[string]interface{}{
		"config": map[string]interface{}{
			"host":    ldapCfg.Host,
			"base_dn": ldapCfg.BaseDN,
			"enabled": ldapCfg.Enabled,
		},
		"summary": map[string]int{
			"mappings":           len(mappings),
			"ldap_users":         len(items),
			"would_create":       len(wouldCreate),
			"would_sync":         len(wouldSync),
			"conflicts":          len(conflicts),
			"blocking_conflicts": blocking,
			"warnings":           len(warnings),
		},
		"mappings":     mappings,
		"would_create": wouldCreate,
		"would_sync":   wouldSync,
		"conflicts":    conflicts,
		"warnings":     warnings,
	}})
}

func ldapDryRunPreviewFromLookup(item ldapDryRunLookupItem) ldapDryRunUserPreview {
	return ldapDryRunUserPreview{
		Username:       strings.TrimSpace(item.User.Username),
		DisplayName:    strings.TrimSpace(item.User.DisplayName),
		Email:          strings.TrimSpace(item.User.Email),
		DN:             strings.TrimSpace(item.User.DN),
		UID:            strings.TrimSpace(item.User.UID),
		IsDisabled:     item.User.IsDisabled,
		PolicyPresetID: strings.TrimSpace(item.Mapping.PolicyPresetID),
		GroupName:      strings.TrimSpace(item.Mapping.GroupName),
		Mapping:        item.Mapping,
	}
}

func (h *SettingsHandler) indexLocalUsersForLDAPDryRun() map[string]ldapDryRunLocalUser {
	users := map[string]ldapDryRunLocalUser{}
	rows, err := h.db.Query(`SELECT id, username, jellyfin_id, ldap_dn, preset_id FROM users`)
	if err != nil {
		return users
	}
	defer rows.Close()

	for rows.Next() {
		var user ldapDryRunLocalUser
		var jellyfinID, ldapDN, presetID sql.NullString
		if err := rows.Scan(&user.ID, &user.Username, &jellyfinID, &ldapDN, &presetID); err != nil {
			continue
		}
		user.JellyfinID = strings.TrimSpace(jellyfinID.String)
		user.LDAPDN = strings.TrimSpace(ldapDN.String)
		user.PresetID = strings.TrimSpace(presetID.String)
		users[strings.ToLower(strings.TrimSpace(user.Username))] = user
	}
	return users
}

func (h *SettingsHandler) indexJellyfinUsersForLDAPDryRun() map[string]jellyfin.User {
	users := map[string]jellyfin.User{}
	if h.jfClient == nil {
		return users
	}
	jfUsers, err := h.jfClient.GetUsers()
	if err != nil {
		slog.Warn("Dry-run LDAP: lecture utilisateurs Jellyfin impossible", "error", err)
		return users
	}
	for _, user := range jfUsers {
		name := strings.TrimSpace(user.Name)
		if name == "" {
			continue
		}
		users[strings.ToLower(name)] = user
	}
	return users
}

func databaseSecurityDryRunEvent(eventType, severity, message string) database.SecurityEvent {
	return database.SecurityEvent{
		Category:  "ldap",
		EventType: eventType,
		Severity:  severity,
		Actor:     "admin",
		Message:   message,
	}
}
