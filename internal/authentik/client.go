// Package authentik fournit un client pour l'API REST v3 d'Authentik.
package authentik

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

// UserCreatePayload payload pour la création d'un utilisateur dans Authentik.
type UserCreatePayload struct {
	Username string   `json:"username"`
	Name     string   `json:"name"`
	Email    string   `json:"email"`
	IsActive bool     `json:"is_active"`
	Groups   []string `json:"groups,omitempty"`
}

// UserResponse représente un utilisateur renvoyé par l'API Authentik.
type UserResponse struct {
	PK       int64  `json:"pk"`
	ID       string `json:"uid"` // UUID string
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	IsActive bool   `json:"is_active"`
}

// UserDetailResponse représente les détails complets d'un utilisateur Authentik incluant ses groupes.
type UserDetailResponse struct {
	PK       int64    `json:"pk"`
	ID       string   `json:"uid"`
	Username string   `json:"username"`
	Name     string   `json:"name"`
	Email    string   `json:"email"`
	IsActive bool     `json:"is_active"`
	Groups   []string `json:"groups"`
}

// InvitationTokenResponse représente une invitation stage Authentik.
type InvitationTokenResponse struct {
	PK        string                 `json:"pk"`
	Name      string                 `json:"name"`
	Expires   time.Time              `json:"expires"`
	FixedData map[string]interface{} `json:"fixed_data"`
}

// HealthComponent représente le résultat de contrôle d'un composant Authentik.
type HealthComponent struct {
	Status  string            `json:"status"` // "ok", "error", "warning"
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

// HealthCheckResult récapitule la santé globale de l'intégration Authentik.
type HealthCheckResult struct {
	OverallStatus string                     `json:"overall_status"` // "ok", "error", "warning"
	LastChecked   time.Time                  `json:"last_checked"`
	Components    map[string]HealthComponent `json:"components"`
}

// Client interface de l'API Authentik REST.
type Client interface {
	CheckHealth(ctx context.Context, cfg config.AuthentikConfig) *HealthCheckResult
	CheckAPI(ctx context.Context) HealthComponent
	CheckOIDC(ctx context.Context, issuerURL string) HealthComponent
	CheckEnrollment(ctx context.Context, flowSlug string) HealthComponent
	CheckGroups(ctx context.Context, groups []string) HealthComponent

	ResolveGroupID(ctx context.Context, nameOrPK string) (string, error)
	CreateUser(ctx context.Context, payload UserCreatePayload) (*UserResponse, error)
	CreateRecoveryLink(ctx context.Context, authentikPK int64) (recoveryLink string, err error)
	AddUserToGroup(ctx context.Context, userPK int64, groupID string) error
	RemoveUserFromGroup(ctx context.Context, userPK int64, groupID string) error
	AddUserToGroupByString(ctx context.Context, authentikID string, groupID string) error
	RemoveUserFromGroupByString(ctx context.Context, authentikID string, groupID string) error
	SetUserActiveStatus(ctx context.Context, userPK int64, active bool) error
	SetUserActiveStatusByString(ctx context.Context, authentikID string, active bool) error
	DeleteUser(ctx context.Context, userPK int64) error
	DeleteUserByString(ctx context.Context, authentikID string) error
	ListUsers(ctx context.Context) ([]UserResponse, error)
	GetUserByUsername(ctx context.Context, username string) (*UserDetailResponse, error)

	CreateInvitationStageToken(ctx context.Context, name string, expiresAt time.Time, fixedData map[string]interface{}, singleUse bool, flow string) (invitationID string, err error)
	ListInvitationStageTokens(ctx context.Context) ([]InvitationTokenResponse, error)
	DeleteInvitationStageToken(ctx context.Context, invitationID string) error
	GetEnrollmentFlowSlug(ctx context.Context, preferred string) string
}

type client struct {
	baseURL    string
	apiToken   string
	httpClient *http.Client
}

// NewClient crée une instance de client REST Authentik.
func NewClient(cfg config.AuthentikConfig) Client {
	rawURL := strings.TrimRight(cfg.URL, "/")
	if rawURL == "" {
		rawURL = strings.TrimRight(cfg.IssuerURL, "/")
	}

	baseURL := rawURL
	if u, err := url.Parse(rawURL); err == nil && u.Scheme != "" && u.Host != "" {
		baseURL = u.Scheme + "://" + u.Host
	}

	return &client{
		baseURL:    baseURL,
		apiToken:   cfg.APIToken,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *client) doRequest(ctx context.Context, method, endpoint string, bodyObj interface{}) ([]byte, int, error) {
	if c.baseURL == "" {
		return nil, 0, fmt.Errorf("authentik base URL is not configured")
	}

	fullURL := c.baseURL + endpoint
	var reqBody io.Reader
	if bodyObj != nil {
		data, err := json.Marshal(bodyObj)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// isUUID vérifie si une chaîne correspond au format d'un UUID valide.
func isUUID(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

// ResolveGroupID résout un nom de groupe ou identifiant vers l'UUID PK du groupe Authentik.
func (c *client) ResolveGroupID(ctx context.Context, nameOrPK string) (string, error) {
	trimmed := strings.TrimSpace(nameOrPK)
	if trimmed == "" {
		return "", fmt.Errorf("nom ou identifiant de groupe vide")
	}
	if isUUID(trimmed) {
		return trimmed, nil
	}

	// 1. Recherche par nom direct dans Authentik
	endpoint := fmt.Sprintf("/api/v3/core/groups/?name=%s", url.QueryEscape(trimmed))
	respBody, statusCode, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
	if err == nil && statusCode == http.StatusOK {
		var pageResp struct {
			Results []struct {
				PK   string `json:"pk"`
				Name string `json:"name"`
			} `json:"results"`
		}
		if err := json.Unmarshal(respBody, &pageResp); err == nil {
			for _, g := range pageResp.Results {
				if strings.EqualFold(g.Name, trimmed) && g.PK != "" {
					return g.PK, nil
				}
			}
		}
	}

	// 2. Recherche dans le listing global des groupes en cas de non correspondance stricte
	respBody, statusCode, err = c.doRequest(ctx, http.MethodGet, "/api/v3/core/groups/?page_size=200", nil)
	if err == nil && statusCode == http.StatusOK {
		var pageResp struct {
			Results []struct {
				PK   string `json:"pk"`
				Name string `json:"name"`
			} `json:"results"`
		}
		if err := json.Unmarshal(respBody, &pageResp); err == nil {
			for _, g := range pageResp.Results {
				if strings.EqualFold(g.Name, trimmed) && g.PK != "" {
					return g.PK, nil
				}
			}
		}
	}

	return "", fmt.Errorf("groupe Authentik introuvable: %s", trimmed)
}

func (c *client) CreateUser(ctx context.Context, payload UserCreatePayload) (*UserResponse, error) {
	if payload.Name == "" {
		payload.Name = payload.Username
	}

	// Résolution des noms de groupes en UUIDs pour l'API Authentik v3
	if len(payload.Groups) > 0 {
		resolvedGroups := make([]string, 0, len(payload.Groups))
		for _, g := range payload.Groups {
			trimmed := strings.TrimSpace(g)
			if trimmed == "" {
				continue
			}
			pk, err := c.ResolveGroupID(ctx, trimmed)
			if err == nil && pk != "" {
				resolvedGroups = append(resolvedGroups, pk)
			} else if isUUID(trimmed) {
				resolvedGroups = append(resolvedGroups, trimmed)
			}
		}
		payload.Groups = resolvedGroups
	}

	respBody, statusCode, err := c.doRequest(ctx, http.MethodPost, "/api/v3/core/users/", payload)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusCreated && statusCode != http.StatusOK {
		return nil, fmt.Errorf("create user returned status %d: %s", statusCode, string(respBody))
	}

	var user UserResponse
	if err := json.Unmarshal(respBody, &user); err != nil {
		return nil, fmt.Errorf("failed to unmarshal user response: %w", err)
	}

	return &user, nil
}

func (c *client) CreateRecoveryLink(ctx context.Context, authentikPK int64) (string, error) {
	endpoint := fmt.Sprintf("/api/v3/core/users/%d/recovery/link/", authentikPK)
	respBody, statusCode, err := c.doRequest(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return "", err
	}
	if statusCode != http.StatusOK && statusCode != http.StatusCreated {
		return "", fmt.Errorf("create recovery link returned status %d: %s", statusCode, string(respBody))
	}

	var res struct {
		Link string `json:"link"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(respBody, &res); err != nil {
		return "", fmt.Errorf("failed to parse recovery link response: %w", err)
	}

	if res.Link != "" {
		return res.Link, nil
	}
	return res.URL, nil
}

func (c *client) AddUserToGroup(ctx context.Context, userPK int64, groupID string) error {
	resolvedID, err := c.ResolveGroupID(ctx, groupID)
	if err == nil && resolvedID != "" {
		groupID = resolvedID
	}
	endpoint := fmt.Sprintf("/api/v3/core/groups/%s/add_user/", url.PathEscape(groupID))
	payload := map[string]int64{"pk": userPK}
	respBody, statusCode, err := c.doRequest(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		return err
	}
	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return fmt.Errorf("add user to group returned status %d: %s", statusCode, string(respBody))
	}
	return nil
}

func (c *client) RemoveUserFromGroup(ctx context.Context, userPK int64, groupID string) error {
	resolvedID, err := c.ResolveGroupID(ctx, groupID)
	if err == nil && resolvedID != "" {
		groupID = resolvedID
	}
	endpoint := fmt.Sprintf("/api/v3/core/groups/%s/remove_user/", url.PathEscape(groupID))
	payload := map[string]int64{"pk": userPK}
	respBody, statusCode, err := c.doRequest(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		return err
	}
	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return fmt.Errorf("remove user from group returned status %d: %s", statusCode, string(respBody))
	}
	return nil
}

func (c *client) AddUserToGroupByString(ctx context.Context, authentikID string, groupID string) error {
	resolvedID, err := c.ResolveGroupID(ctx, groupID)
	if err == nil && resolvedID != "" {
		groupID = resolvedID
	}
	endpoint := fmt.Sprintf("/api/v3/core/groups/%s/add_user/", url.PathEscape(groupID))
	payload := map[string]string{"pk": authentikID}
	respBody, statusCode, err := c.doRequest(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		return err
	}
	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return fmt.Errorf("add user to group returned status %d: %s", statusCode, string(respBody))
	}
	return nil
}

func (c *client) RemoveUserFromGroupByString(ctx context.Context, authentikID string, groupID string) error {
	resolvedID, err := c.ResolveGroupID(ctx, groupID)
	if err == nil && resolvedID != "" {
		groupID = resolvedID
	}
	endpoint := fmt.Sprintf("/api/v3/core/groups/%s/remove_user/", url.PathEscape(groupID))
	payload := map[string]string{"pk": authentikID}
	respBody, statusCode, err := c.doRequest(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		return err
	}
	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return fmt.Errorf("remove user from group returned status %d: %s", statusCode, string(respBody))
	}
	return nil
}

func (c *client) SetUserActiveStatus(ctx context.Context, userPK int64, active bool) error {
	endpoint := fmt.Sprintf("/api/v3/core/users/%d/", userPK)
	payload := map[string]bool{"is_active": active}
	respBody, statusCode, err := c.doRequest(ctx, http.MethodPatch, endpoint, payload)
	if err != nil {
		return err
	}
	if statusCode != http.StatusOK {
		return fmt.Errorf("set user active status returned status %d: %s", statusCode, string(respBody))
	}
	return nil
}

func (c *client) SetUserActiveStatusByString(ctx context.Context, authentikID string, active bool) error {
	endpoint := fmt.Sprintf("/api/v3/core/users/%s/", url.PathEscape(authentikID))
	payload := map[string]bool{"is_active": active}
	respBody, statusCode, err := c.doRequest(ctx, http.MethodPatch, endpoint, payload)
	if err != nil {
		return err
	}
	if statusCode != http.StatusOK {
		return fmt.Errorf("set user active status returned status %d: %s", statusCode, string(respBody))
	}
	return nil
}

func (c *client) DeleteUser(ctx context.Context, userPK int64) error {
	endpoint := fmt.Sprintf("/api/v3/core/users/%d/", userPK)
	respBody, statusCode, err := c.doRequest(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	if statusCode != http.StatusNoContent && statusCode != http.StatusOK {
		return fmt.Errorf("delete user returned status %d: %s", statusCode, string(respBody))
	}
	return nil
}

func (c *client) ListUsers(ctx context.Context) ([]UserResponse, error) {
	respBody, statusCode, err := c.doRequest(ctx, http.MethodGet, "/api/v3/core/users/", nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("list users returned status %d: %s", statusCode, string(respBody))
	}

	var pageResp struct {
		Results []UserResponse `json:"results"`
	}
	if err := json.Unmarshal(respBody, &pageResp); err == nil && pageResp.Results != nil {
		return pageResp.Results, nil
	}

	var users []UserResponse
	if err := json.Unmarshal(respBody, &users); err != nil {
		return nil, fmt.Errorf("failed to unmarshal users list: %w", err)
	}

	return users, nil
}

func (c *client) GetUserByUsername(ctx context.Context, username string) (*UserDetailResponse, error) {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" {
		return nil, fmt.Errorf("username required")
	}
	endpoint := fmt.Sprintf("/api/v3/core/users/?username=%s", url.QueryEscape(trimmed))
	respBody, statusCode, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("user lookup returned status %d: %s", statusCode, string(respBody))
	}

	var pageResp struct {
		Results []struct {
			PK        int64    `json:"pk"`
			ID        string   `json:"uid"`
			Username  string   `json:"username"`
			Name      string   `json:"name"`
			Email     string   `json:"email"`
			IsActive  bool     `json:"is_active"`
			Groups    []string `json:"groups"`
			GroupsObj []struct {
				PK   string `json:"pk"`
				Name string `json:"name"`
			} `json:"groups_obj"`
		} `json:"results"`
	}

	if err := json.Unmarshal(respBody, &pageResp); err != nil {
		return nil, fmt.Errorf("failed to parse user search response: %w", err)
	}

	for _, u := range pageResp.Results {
		if strings.EqualFold(u.Username, trimmed) {
			groupNames := make([]string, 0)
			for _, g := range u.GroupsObj {
				if strings.TrimSpace(g.Name) != "" {
					groupNames = append(groupNames, g.Name)
				}
			}
			if len(groupNames) == 0 && len(u.Groups) > 0 {
				groupNames = u.Groups
			}
			return &UserDetailResponse{
				PK:       u.PK,
				ID:       u.ID,
				Username: u.Username,
				Name:     u.Name,
				Email:    u.Email,
				IsActive: u.IsActive,
				Groups:   groupNames,
			}, nil
		}
	}

	return nil, fmt.Errorf("utilisateur introuvable")
}

var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (c *client) resolveFlowInfo(ctx context.Context, flowOrSlug string) (string, string) {
	flow := strings.TrimSpace(flowOrSlug)

	// Si c'est un UUID direct
	if uuidRegex.MatchString(flow) {
		endpoint := fmt.Sprintf("/api/v3/flows/instances/%s/", url.PathEscape(flow))
		respBody, statusCode, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
		if err == nil && statusCode == http.StatusOK {
			var res struct {
				PK   string `json:"pk"`
				Slug string `json:"slug"`
			}
			if err := json.Unmarshal(respBody, &res); err == nil && res.PK != "" {
				return res.PK, res.Slug
			}
		}
		return flow, ""
	}

	// 1. Essayer /api/v3/flows/instances/<slug>/
	if flow != "" {
		endpoint := fmt.Sprintf("/api/v3/flows/instances/%s/", url.PathEscape(flow))
		respBody, statusCode, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
		if err == nil && statusCode == http.StatusOK {
			var res struct {
				PK   string `json:"pk"`
				Slug string `json:"slug"`
			}
			if err := json.Unmarshal(respBody, &res); err == nil && uuidRegex.MatchString(res.PK) {
				slug := res.Slug
				if slug == "" {
					slug = flow
				}
				return res.PK, slug
			}
		}

		// 2. Essayer /api/v3/flows/instances/?slug=<slug>
		endpointQuery := fmt.Sprintf("/api/v3/flows/instances/?slug=%s", url.QueryEscape(flow))
		respBody, statusCode, err = c.doRequest(ctx, http.MethodGet, endpointQuery, nil)
		if err == nil && statusCode == http.StatusOK {
			var listRes struct {
				Results []struct {
					PK   string `json:"pk"`
					Slug string `json:"slug"`
				} `json:"results"`
			}
			if err := json.Unmarshal(respBody, &listRes); err == nil && len(listRes.Results) > 0 && uuidRegex.MatchString(listRes.Results[0].PK) {
				slug := listRes.Results[0].Slug
				if slug == "" {
					slug = flow
				}
				return listRes.Results[0].PK, slug
			}
		}
	}

	// 3. Fallback automatique : rechercher les flux avec designation=enrollment
	respBody, statusCode, err := c.doRequest(ctx, http.MethodGet, "/api/v3/flows/instances/?designation=enrollment", nil)
	if err == nil && statusCode == http.StatusOK {
		var listRes struct {
			Results []struct {
				PK   string `json:"pk"`
				Slug string `json:"slug"`
			} `json:"results"`
		}
		if err := json.Unmarshal(respBody, &listRes); err == nil && len(listRes.Results) > 0 && uuidRegex.MatchString(listRes.Results[0].PK) {
			return listRes.Results[0].PK, listRes.Results[0].Slug
		}
	}

	// 4. Fallback général : lister les flux disponibles et chercher un flux d'inscription
	respBody, statusCode, err = c.doRequest(ctx, http.MethodGet, "/api/v3/flows/instances/", nil)
	if err == nil && statusCode == http.StatusOK {
		var listRes struct {
			Results []struct {
				PK          string `json:"pk"`
				Slug        string `json:"slug"`
				Designation string `json:"designation"`
			} `json:"results"`
		}
		if err := json.Unmarshal(respBody, &listRes); err == nil && len(listRes.Results) > 0 {
			for _, f := range listRes.Results {
				if f.Designation == "enrollment" || strings.Contains(f.Slug, "enroll") || strings.Contains(f.Slug, "invit") || strings.Contains(f.Slug, "inscri") {
					return f.PK, f.Slug
				}
			}
			return listRes.Results[0].PK, listRes.Results[0].Slug
		}
	}

	return "", flow
}

func (c *client) resolveFlowUUID(ctx context.Context, flowOrSlug string) string {
	uuid, _ := c.resolveFlowInfo(ctx, flowOrSlug)
	return uuid
}

func (c *client) GetEnrollmentFlowSlug(ctx context.Context, preferred string) string {
	_, slug := c.resolveFlowInfo(ctx, preferred)
	return slug
}

func (c *client) CreateInvitationStageToken(ctx context.Context, name string, expiresAt time.Time, fixedData map[string]interface{}, singleUse bool, flow string) (string, error) {
	payload := map[string]interface{}{
		"name":       name,
		"single_use": singleUse,
	}
	if !expiresAt.IsZero() {
		payload["expires"] = expiresAt.Format(time.RFC3339)
	}
	if fixedData != nil {
		enriched := make(map[string]interface{}, len(fixedData)+2)
		attrs := make(map[string]interface{})
		for k, v := range fixedData {
			enriched[k] = v
			if k != "attributes" && k != "groups" && k != "username" && k != "email" && k != "name" {
				attrs[k] = v
			}
		}
		if existingAttrs, ok := fixedData["attributes"].(map[string]interface{}); ok {
			for k, v := range existingAttrs {
				attrs[k] = v
			}
		}
		enriched["attributes"] = attrs
		payload["fixed_data"] = enriched
	}
	
	flowUUID, _ := c.resolveFlowInfo(ctx, flow)
	if flowUUID != "" {
		payload["flow"] = flowUUID
	}

	respBody, statusCode, err := c.doRequest(ctx, http.MethodPost, "/api/v3/stages/invitation/invitations/", payload)
	if err != nil {
		return "", err
	}
	if statusCode != http.StatusCreated && statusCode != http.StatusOK {
		return "", fmt.Errorf("create stage invitation returned status %d: %s", statusCode, string(respBody))
	}

	var res struct {
		PK string `json:"pk"`
	}
	if err := json.Unmarshal(respBody, &res); err != nil {
		return "", fmt.Errorf("failed to unmarshal invitation response: %w", err)
	}

	return res.PK, nil
}

func (c *client) ListInvitationStageTokens(ctx context.Context) ([]InvitationTokenResponse, error) {
	respBody, statusCode, err := c.doRequest(ctx, http.MethodGet, "/api/v3/stages/invitation/invitations/", nil)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("list stage invitations returned status %d: %s", statusCode, string(respBody))
	}

	var pageResp struct {
		Results []InvitationTokenResponse `json:"results"`
	}
	if err := json.Unmarshal(respBody, &pageResp); err == nil && pageResp.Results != nil {
		return pageResp.Results, nil
	}

	var res []InvitationTokenResponse
	if err := json.Unmarshal(respBody, &res); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stage invitations list: %w", err)
	}

	return res, nil
}

func (c *client) DeleteInvitationStageToken(ctx context.Context, invitationID string) error {
	endpoint := fmt.Sprintf("/api/v3/stages/invitation/invitations/%s/", invitationID)
	respBody, statusCode, err := c.doRequest(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	if statusCode != http.StatusNoContent && statusCode != http.StatusOK {
		return fmt.Errorf("delete stage invitation returned status %d: %s", statusCode, string(respBody))
	}
	return nil
}

func (c *client) DeleteUserByString(ctx context.Context, authentikID string) error {
	if strings.TrimSpace(authentikID) == "" {
		return fmt.Errorf("authentikID cannot be empty")
	}
	endpoint := fmt.Sprintf("/api/v3/core/users/%s/", url.PathEscape(authentikID))
	respBody, statusCode, err := c.doRequest(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	if statusCode != http.StatusNoContent && statusCode != http.StatusOK {
		return fmt.Errorf("delete user returned status %d: %s", statusCode, string(respBody))
	}
	return nil
}

// UserPKString convertit un PK entier en string pour les helpers d'URL.
func UserPKString(pk int64) string {
	return strconv.FormatInt(pk, 10)
}

func (c *client) CheckAPI(ctx context.Context) HealthComponent {
	if c.baseURL == "" {
		return HealthComponent{
			Status:  "error",
			Message: "URL Authentik non configurée",
		}
	}
	if c.apiToken == "" {
		return HealthComponent{
			Status:  "error",
			Message: "Token API Authentik non configuré",
		}
	}

	respBody, statusCode, err := c.doRequest(ctx, http.MethodGet, "/api/v3/core/users/me/", nil)
	if err != nil {
		return HealthComponent{
			Status:  "error",
			Message: fmt.Sprintf("Connexion API impossible: %v", err),
		}
	}

	if statusCode == http.StatusOK {
		var user UserResponse
		_ = json.Unmarshal(respBody, &user)
		return HealthComponent{
			Status:  "ok",
			Message: "Connexion API REST OK",
			Details: map[string]string{
				"authenticated_as": user.Username,
				"url":              c.baseURL,
			},
		}
	}

	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return HealthComponent{
			Status:  "error",
			Message: fmt.Sprintf("Token API invalide ou permissions insuffisantes (HTTP %d)", statusCode),
			Details: map[string]string{"http_code": strconv.Itoa(statusCode)},
		}
	}

	return HealthComponent{
		Status:  "error",
		Message: fmt.Sprintf("Erreur API Authentik HTTP %d: %s", statusCode, string(respBody)),
		Details: map[string]string{"http_code": strconv.Itoa(statusCode)},
	}
}

func (c *client) CheckOIDC(ctx context.Context, issuerURL string) HealthComponent {
	issuer := strings.TrimRight(issuerURL, "/")
	if issuer == "" && c.baseURL != "" {
		issuer = c.baseURL + "/application/o/jellygate"
	}
	if issuer == "" {
		return HealthComponent{
			Status:  "error",
			Message: "OIDC Issuer URL non configurée",
		}
	}

	discoveryURL := issuer + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return HealthComponent{
			Status:  "error",
			Message: fmt.Sprintf("Erreur préparation requête Discovery: %v", err),
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return HealthComponent{
			Status:  "error",
			Message: fmt.Sprintf("Discovery OIDC inaccessible (%s): %v", discoveryURL, err),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return HealthComponent{
			Status:  "error",
			Message: fmt.Sprintf("Discovery OIDC HTTP %d depuis %s", resp.StatusCode, discoveryURL),
		}
	}

	var meta struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
		JWKSURI               string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return HealthComponent{
			Status:  "error",
			Message: fmt.Sprintf("Erreur de decodage JSON OIDC Discovery: %v", err),
		}
	}

	return HealthComponent{
		Status:  "ok",
		Message: "OIDC Discovery OK",
		Details: map[string]string{
			"issuer":                 meta.Issuer,
			"authorization_endpoint": meta.AuthorizationEndpoint,
			"token_endpoint":         meta.TokenEndpoint,
			"jwks_uri":               meta.JWKSURI,
		},
	}
}

func (c *client) CheckEnrollment(ctx context.Context, flowSlug string) HealthComponent {
	slug := strings.TrimSpace(flowSlug)
	if slug == "" {
		slug = "default-enrollment-flow"
	}

	endpoint := fmt.Sprintf("/api/v3/flows/instances/%s/", url.PathEscape(slug))
	_, statusCode, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return HealthComponent{
			Status:  "warning",
			Message: fmt.Sprintf("Vérification du flux impossible: %v", err),
			Details: map[string]string{"flow_slug": slug},
		}
	}

	if statusCode == http.StatusOK {
		return HealthComponent{
			Status:  "ok",
			Message: fmt.Sprintf("Flux d'inscription '%s' accessible", slug),
			Details: map[string]string{"flow_slug": slug},
		}
	}

	if statusCode == http.StatusNotFound {
		return HealthComponent{
			Status:  "warning",
			Message: fmt.Sprintf("Flux d'inscription '%s' non configuré dans Authentik (optionnel)", slug),
			Details: map[string]string{"flow_slug": slug},
		}
	}

	if statusCode == http.StatusForbidden || statusCode == http.StatusUnauthorized {
		return HealthComponent{
			Status:  "warning",
			Message: "Permissions insuffisantes pour vérifier le flux d'inscription",
			Details: map[string]string{"flow_slug": slug},
		}
	}

	return HealthComponent{
		Status:  "warning",
		Message: fmt.Sprintf("Flux '%s' (HTTP %d)", slug, statusCode),
		Details: map[string]string{"flow_slug": slug, "http_code": strconv.Itoa(statusCode)},
	}
}

func (c *client) CheckGroups(ctx context.Context, groups []string) HealthComponent {
	var checkedGroups []string
	for _, g := range groups {
		g = strings.TrimSpace(g)
		if g != "" {
			checkedGroups = append(checkedGroups, g)
		}
	}
	if len(checkedGroups) == 0 {
		return HealthComponent{
			Status:  "ok",
			Message: "Aucun groupe configuré pour vérification",
		}
	}

	respBody, statusCode, err := c.doRequest(ctx, http.MethodGet, "/api/v3/core/groups/?page_size=200", nil)
	if err != nil {
		return HealthComponent{
			Status:  "warning",
			Message: fmt.Sprintf("Impossible de lister les groupes Authentik: %v", err),
		}
	}
	if statusCode == http.StatusForbidden || statusCode == http.StatusUnauthorized {
		return HealthComponent{
			Status:  "warning",
			Message: "Permissions insuffisantes pour lister les groupes via l'API",
		}
	}
	if statusCode != http.StatusOK {
		return HealthComponent{
			Status:  "warning",
			Message: fmt.Sprintf("Erreur listing groupes HTTP %d: %s", statusCode, string(respBody)),
		}
	}

	var pageResp struct {
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	}
	_ = json.Unmarshal(respBody, &pageResp)

	existingGroups := make(map[string]bool)
	for _, item := range pageResp.Results {
		existingGroups[item.Name] = true
	}

	var missing []string
	details := make(map[string]string)
	for _, g := range checkedGroups {
		if existingGroups[g] {
			details[g] = "présent"
		} else {
			details[g] = "MANQUANT"
			missing = append(missing, g)
		}
	}

	if len(missing) > 0 {
		return HealthComponent{
			Status:  "warning",
			Message: fmt.Sprintf("Groupe(s) manquant(s) dans Authentik: %s (à créer dans Authentik > Annuaire > Groupes)", strings.Join(missing, ", ")),
			Details: details,
		}
	}

	return HealthComponent{
		Status:  "ok",
		Message: "Tous les groupes requis existent dans Authentik",
		Details: details,
	}
}

func (c *client) CheckHealth(ctx context.Context, cfg config.AuthentikConfig) *HealthCheckResult {
	res := &HealthCheckResult{
		OverallStatus: "ok",
		LastChecked:   time.Now(),
		Components:    make(map[string]HealthComponent),
	}

	apiComp := c.CheckAPI(ctx)
	res.Components["api"] = apiComp

	oidcComp := c.CheckOIDC(ctx, cfg.IssuerURL)
	res.Components["oidc_discovery"] = oidcComp

	enrollComp := c.CheckEnrollment(ctx, cfg.EnrollmentFlowSlug)
	res.Components["enrollment_flow"] = enrollComp

	checkGroupsList := []string{cfg.UserGroup, cfg.AdminGroup, cfg.JellyfinUserGroup}
	if strings.TrimSpace(cfg.InvitersGroup) != "" {
		checkGroupsList = append(checkGroupsList, strings.TrimSpace(cfg.InvitersGroup))
	}
	if strings.TrimSpace(cfg.InvitersRecursiveGroup) != "" {
		checkGroupsList = append(checkGroupsList, strings.TrimSpace(cfg.InvitersRecursiveGroup))
	}
	groupsComp := c.CheckGroups(ctx, checkGroupsList)
	res.Components["groups"] = groupsComp

	if apiComp.Status == "error" && oidcComp.Status == "error" {
		res.OverallStatus = "error"
	} else if apiComp.Status == "error" || oidcComp.Status == "error" {
		res.OverallStatus = "warning"
	} else if enrollComp.Status == "warning" || enrollComp.Status == "error" || groupsComp.Status == "warning" || groupsComp.Status == "error" {
		res.OverallStatus = "warning"
	} else {
		res.OverallStatus = "ok"
	}

	return res
}
