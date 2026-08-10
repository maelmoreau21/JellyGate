package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DiscoveryMetadata représente le document .well-known/openid-configuration.
type DiscoveryMetadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	EndSessionEndpoint    string   `json:"end_session_endpoint"`
	UserinfoEndpoint      string   `json:"userinfo_endpoint"`
	SupportedAlgs         []string `json:"id_token_signing_alg_values_supported"`
}

type discoveryCache struct {
	metadata *DiscoveryMetadata
	lastRun  time.Time
	mu       sync.RWMutex
}

func (c *oidcClient) getDiscoveryMetadata(ctx context.Context) (*DiscoveryMetadata, error) {
	c.discMu.RLock()
	if c.discCache != nil && time.Since(c.discLast) < 12*time.Hour {
		meta := c.discCache
		c.discMu.RUnlock()
		return meta, nil
	}
	c.discMu.RUnlock()

	c.discMu.Lock()
	defer c.discMu.Unlock()

	// Vérification à nouveau sous verrou exclusif
	if c.discCache != nil && time.Since(c.discLast) < 12*time.Hour {
		return c.discCache, nil
	}

	issuerURL := strings.TrimRight(c.cfg.IssuerURL, "/")
	if issuerURL == "" && c.cfg.URL != "" {
		issuerURL = strings.TrimRight(c.cfg.URL, "/") + "/application/o/jellygate"
	}
	if issuerURL == "" {
		return nil, fmt.Errorf("OIDC issuer_url is not configured")
	}

	discoveryURL := issuerURL + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discovery request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC discovery returned status %d from %s", resp.StatusCode, discoveryURL)
	}

	var meta DiscoveryMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("failed to decode OIDC discovery JSON: %w", err)
	}

	c.discCache = &meta
	c.discLast = time.Now()

	return &meta, nil
}
