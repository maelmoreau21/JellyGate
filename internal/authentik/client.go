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

	CreateUser(ctx context.Context, payload UserCreatePayload) (*UserResponse, error)
	CreateRecoveryLink(ctx context.Context, authentikPK int64) (recoveryLink string, err error)
	AddUserToGroup(ctx context.Context, userPK int64, groupID string) error
	SetUserActiveStatus(ctx context.Context, userPK int64, active bool) error
	SetUserActiveStatusByString(ctx context.Context, authentikID string, active bool) error
	DeleteUser(ctx context.Context, userPK int64) error
	DeleteUserByString(ctx context.Context, authentikID string) error
	ListUsers(ctx context.Context) ([]UserResponse, error)

	CreateInvitationStageToken(ctx context.Context, name string, expiresAt time.Time, fixedData map[string]interface{}, singleUse bool, flow string) (invitationID string, err error)
	ListInvitationStageTokens(ctx context.Context) ([]InvitationTokenResponse, error)
	DeleteInvitationStageToken(ctx context.Context, invitationID string) error
}

type client struct {
	baseURL    string
	apiToken   string
	httpClient *http.Client
}

// NewClient crée une instance de client REST Authentik.
func NewClient(cfg config.AuthentikConfig) Client {
	baseURL := strings.TrimRight(cfg.URL, "/")
	if baseURL == "" && cfg.IssuerURL != "" {
		u, err := url.Parse(cfg.IssuerURL)
		if err == nil {
			baseURL = u.Scheme + "://" + u.Host
		}
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

func (c *client) CreateUser(ctx context.Context, payload UserCreatePayload) (*UserResponse, error) {
	if payload.Name == "" {
		payload.Name = payload.Username
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
	endpoint := fmt.Sprintf("/api/v3/core/groups/%s/add_user/", groupID)
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

func (c *client) CreateInvitationStageToken(ctx context.Context, name string, expiresAt time.Time, fixedData map[string]interface{}, singleUse bool, flow string) (string, error) {
	payload := map[string]interface{}{
		"name":       name,
		"single_use": singleUse,
	}
	if !expiresAt.IsZero() {
		payload["expires"] = expiresAt.Format(time.RFC3339)
	}
	if fixedData != nil {
		payload["fixed_data"] = fixedData
	}
	if strings.TrimSpace(flow) != "" {
		payload["flow"] = strings.TrimSpace(flow)
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
	respBody, statusCode, err := c.doRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return HealthComponent{
			Status:  "warning",
			Message: fmt.Sprintf("Vérification du flow d'enrollment impossible: %v", err),
			Details: map[string]string{"flow_slug": slug},
		}
	}

	if statusCode == http.StatusOK {
		return HealthComponent{
			Status:  "ok",
			Message: fmt.Sprintf("Flow d'enrollment '%s' accessible", slug),
			Details: map[string]string{"flow_slug": slug},
		}
	}

	if statusCode == http.StatusNotFound {
		return HealthComponent{
			Status:  "error",
			Message: fmt.Sprintf("Flow d'enrollment '%s' introuvable dans Authentik", slug),
			Details: map[string]string{"flow_slug": slug},
		}
	}

	return HealthComponent{
		Status:  "warning",
		Message: fmt.Sprintf("Réponse inattendue lors de la vérification du flow '%s' (HTTP %d): %s", slug, statusCode, string(respBody)),
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
			Status:  "warning",
			Message: "Aucun groupe configuré pour vérification",
		}
	}

	respBody, statusCode, err := c.doRequest(ctx, http.MethodGet, "/api/v3/core/groups/", nil)
	if err != nil {
		return HealthComponent{
			Status:  "error",
			Message: fmt.Sprintf("Impossible de lister les groupes Authentik: %v", err),
		}
	}
	if statusCode != http.StatusOK {
		return HealthComponent{
			Status:  "error",
			Message: fmt.Sprintf("Erreur listing groupes Authentik HTTP %d: %s", statusCode, string(respBody)),
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
			Status:  "error",
			Message: fmt.Sprintf("Groupe(s) Authentik manquant(s): %s", strings.Join(missing, ", ")),
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

	groupsComp := c.CheckGroups(ctx, []string{cfg.UserGroup, cfg.AdminGroup, cfg.JellyfinUserGroup})
	res.Components["groups"] = groupsComp

	for _, comp := range res.Components {
		if comp.Status == "error" {
			res.OverallStatus = "error"
			break
		} else if comp.Status == "warning" && res.OverallStatus != "error" {
			res.OverallStatus = "warning"
		}
	}

	return res
}
