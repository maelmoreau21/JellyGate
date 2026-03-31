package integrations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

// Client provisionne des comptes utilisateur sur des services tiers (optionnels).
type Client struct {
	cfg        config.ThirdPartyConfig
	httpClient *http.Client
}

// New crée un nouveau client d'intégrations tierces.
func New(cfg config.ThirdPartyConfig) *Client {
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// IsEnabled indique si au moins une intégration tierce est configurée.
func (c *Client) IsEnabled() bool {
	if c == nil {
		return false
	}
	jellyseerr := strings.TrimSpace(c.cfg.JellyseerrURL) != "" && strings.TrimSpace(c.cfg.JellyseerrAPIKey) != ""
	return jellyseerr
}

// ProvisionUser crée un utilisateur dans Jellyseerr selon la configuration active.
func (c *Client) ProvisionUser(username, password, email string) error {
	if c == nil || !c.IsEnabled() {
		return nil
	}

	errs := make([]string, 0, 2)
	if strings.TrimSpace(c.cfg.JellyseerrURL) != "" && strings.TrimSpace(c.cfg.JellyseerrAPIKey) != "" {
		if err := c.createJellyseerrUser(username, password, email); err != nil {
			errs = append(errs, "jellyseerr: "+err.Error())
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, " | "))
	}

	return nil
}

func (c *Client) createJellyseerrUser(username, password, email string) error {
	baseURL := strings.TrimRight(strings.TrimSpace(c.cfg.JellyseerrURL), "/")
	if baseURL == "" {
		return nil
	}

	mail := strings.TrimSpace(email)
	if mail == "" {
		mail = username + "@local.invalid"
	}

	payload := map[string]interface{}{
		"email":           mail,
		"username":        username,
		"password":        password,
		"confirmPassword": password,
	}

	return c.doJSONRequest(
		http.MethodPost,
		baseURL+"/api/v1/user",
		"X-Api-Key",
		c.cfg.JellyseerrAPIKey,
		payload,
	)
}

func (c *Client) doJSONRequest(method, url, authHeader, authValue string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set(authHeader, strings.TrimSpace(authValue))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		// Utilisateur déjà présent : comportement idempotent.
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	return nil
}
