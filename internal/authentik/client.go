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

// Client interface de l'API Authentik REST.
type Client interface {
	CreateUser(ctx context.Context, payload UserCreatePayload) (*UserResponse, error)
	CreateRecoveryLink(ctx context.Context, authentikPK int64) (recoveryLink string, err error)
	AddUserToGroup(ctx context.Context, userPK int64, groupID string) error
	SetUserActiveStatus(ctx context.Context, userPK int64, active bool) error
	DeleteUser(ctx context.Context, userPK int64) error
	ListUsers(ctx context.Context) ([]UserResponse, error)

	CreateInvitationStageToken(ctx context.Context, name string, expiresAt time.Time, fixedData map[string]interface{}) (invitationID string, err error)
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

func (c *client) CreateInvitationStageToken(ctx context.Context, name string, expiresAt time.Time, fixedData map[string]interface{}) (string, error) {
	payload := map[string]interface{}{
		"name": name,
	}
	if !expiresAt.IsZero() {
		payload["expires"] = expiresAt.Format(time.RFC3339)
	}
	if fixedData != nil {
		payload["fixed_data"] = fixedData
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

// UserPKString convertit un PK entier en string pour les helpers d'URL.
func UserPKString(pk int64) string {
	return strconv.FormatInt(pk, 10)
}
