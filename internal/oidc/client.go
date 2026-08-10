// Package oidc gère le flux OAuth2 Authorization Code + PKCE et la validation d'ID Token pour Authentik OIDC.
package oidc

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

const (
	CookieState    = "jellygate_oidc_state"
	CookieNonce    = "jellygate_oidc_nonce"
	CookieVerifier = "jellygate_oidc_verifier"
	CookiePath     = "/"
	CookieTTL      = 10 * time.Minute
	ClockSkew      = 60 * time.Second
)

// Claims représente les informations extraites de l'ID Token / UserInfo OIDC.
type Claims struct {
	Sub               string   `json:"sub"`
	PreferredUsername string   `json:"preferred_username"`
	Name              string   `json:"name"`
	Email             string   `json:"email"`
	EmailVerified     bool     `json:"email_verified"`
	Groups            []string `json:"groups"`
	Issuer            string   `json:"iss"`
	Audience          any      `json:"aud"`
	Expiration        int64    `json:"exp"`
	IssuedAt          int64    `json:"iat"`
	Nonce             string   `json:"nonce"`
}

// Client est l'interface du client OIDC.
type Client interface {
	GenerateAuthURL(w http.ResponseWriter, r *http.Request) (authURL string, err error)
	HandleCallback(r *http.Request) (*Claims, error)
	ValidateIDToken(ctx context.Context, rawIDToken string, expectedNonce string) (*Claims, error)
	DetermineUserRole(groups []string) (isAdmin bool, hasAccess bool)
	GetEndSessionURL(ctx context.Context) string
}

// JSONWebKey représente une clé JWKS.
type JSONWebKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// JWKSResponse représente le document JWKS.
type JWKSResponse struct {
	Keys []JSONWebKey `json:"keys"`
}

type oidcClient struct {
	cfg        config.AuthentikConfig
	httpClient *http.Client
	jwksCache  map[string]*rsa.PublicKey
	jwksMu     sync.RWMutex
	jwksLast   time.Time
	discCache  *DiscoveryMetadata
	discMu     sync.RWMutex
	discLast   time.Time
}

// NewClient crée une nouvelle instance du client OIDC.
func NewClient(cfg config.AuthentikConfig) Client {
	return &oidcClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		jwksCache:  make(map[string]*rsa.PublicKey),
	}
}

func (c *oidcClient) GenerateAuthURL(w http.ResponseWriter, r *http.Request) (string, error) {
	state, err := generateRandomString(32)
	if err != nil {
		return "", fmt.Errorf("failed to generate state: %w", err)
	}

	nonce, err := generateRandomString(32)
	if err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	codeVerifier, err := generateRandomString(64)
	if err != nil {
		return "", fmt.Errorf("failed to generate code verifier: %w", err)
	}

	codeChallenge := calculateS256Challenge(codeVerifier)

	isHTTPS := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")

	setTempCookie(w, CookieState, state, isHTTPS)
	setTempCookie(w, CookieNonce, nonce, isHTTPS)
	setTempCookie(w, CookieVerifier, codeVerifier, isHTTPS)

	authEndpoint, err := c.getAuthEndpoint(r.Context())
	if err != nil {
		return "", fmt.Errorf("failed to resolve auth endpoint: %w", err)
	}
	u, err := url.Parse(authEndpoint)
	if err != nil {
		return "", fmt.Errorf("invalid authorization endpoint: %w", err)
	}

	q := u.Query()
	q.Set("client_id", c.cfg.ClientID)
	q.Set("response_type", "code")
	q.Set("scope", "openid profile email groups")
	q.Set("redirect_uri", c.cfg.RedirectURL)
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()

	return u.String(), nil
}

func (c *oidcClient) HandleCallback(r *http.Request) (*Claims, error) {
	code := r.URL.Query().Get("code")
	stateParam := r.URL.Query().Get("state")

	if code == "" {
		return nil, errors.New("missing code parameter in OIDC callback")
	}
	if stateParam == "" {
		return nil, errors.New("missing state parameter in OIDC callback")
	}

	stateCookie, err := r.Cookie(CookieState)
	if err != nil || stateCookie.Value == "" {
		return nil, errors.New("missing state cookie in OIDC callback")
	}
	if !hmac.Equal([]byte(stateCookie.Value), []byte(stateParam)) {
		return nil, errors.New("invalid or mismatched OIDC state parameter")
	}

	verifierCookie, err := r.Cookie(CookieVerifier)
	if err != nil || verifierCookie.Value == "" {
		return nil, errors.New("missing code_verifier cookie in OIDC callback")
	}

	nonceCookie, err := r.Cookie(CookieNonce)
	if err != nil || nonceCookie.Value == "" {
		return nil, errors.New("missing nonce cookie in OIDC callback")
	}
	expectedNonce := nonceCookie.Value

	// Échange du code d'autorisation contre des jetons
	tokenEndpoint, err := c.getTokenEndpoint(r.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to resolve token endpoint: %w", err)
	}

	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", c.cfg.RedirectURL)
	data.Set("client_id", c.cfg.ClientID)
	if c.cfg.ClientSecret != "" {
		data.Set("client_secret", c.cfg.ClientSecret)
	}
	data.Set("code_verifier", verifierCookie.Value)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		IDToken     string `json:"id_token"`
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token JSON response: %w", err)
	}

	if tokenResp.IDToken == "" {
		return nil, errors.New("id_token missing in token response")
	}

	claims, err := c.ValidateIDToken(r.Context(), tokenResp.IDToken, expectedNonce)
	if err != nil {
		return nil, fmt.Errorf("id_token validation failed: %w", err)
	}

	return claims, nil
}

func (c *oidcClient) ValidateIDToken(ctx context.Context, rawIDToken string, expectedNonce string) (*Claims, error) {
	parts := strings.Split(rawIDToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed JWT ID token structure")
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid JWT header base64: %w", err)
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("invalid JWT header JSON: %w", err)
	}

	// Rejet strict des algorithmes non supportés
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("unsupported JWT algorithm %q: only RS256 is accepted", header.Alg)
	}
	if header.Kid == "" {
		return nil, errors.New("missing 'kid' header in JWT ID token")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid JWT payload base64: %w", err)
	}

	var claims Claims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("invalid JWT payload JSON: %w", err)
	}

	// 1. Validation Expiration (avec tolérance Clock Skew)
	now := time.Now()
	if claims.Expiration > 0 && now.After(time.Unix(claims.Expiration, 0).Add(ClockSkew)) {
		return nil, errors.New("ID token has expired")
	}
	if claims.IssuedAt > 0 && now.Before(time.Unix(claims.IssuedAt, 0).Add(-ClockSkew)) {
		return nil, errors.New("ID token issued in the future")
	}

	// 2. Validation Issuer
	if c.cfg.IssuerURL != "" {
		expectedIssuer := strings.TrimRight(c.cfg.IssuerURL, "/")
		actualIssuer := strings.TrimRight(claims.Issuer, "/")
		if actualIssuer != expectedIssuer {
			return nil, fmt.Errorf("invalid token issuer: expected %s, got %s", expectedIssuer, actualIssuer)
		}
	}

	// 3. Validation Audience / Client ID
	if c.cfg.ClientID != "" {
		if !checkAudience(claims.Audience, c.cfg.ClientID) {
			return nil, fmt.Errorf("invalid token audience: client_id %s not in token audience", c.cfg.ClientID)
		}
	}

	// 4. Validation Nonce obligatoire
	if expectedNonce != "" {
		if claims.Nonce == "" {
			return nil, errors.New("missing nonce claim in ID token when expected")
		}
		if !hmac.Equal([]byte(claims.Nonce), []byte(expectedNonce)) {
			return nil, errors.New("mismatched nonce in ID token")
		}
	}

	// 5. Validation Signature via JWKS (OBLIGATOIRE - pas de fallback silencieux)
	pubKey, err := c.getJWKSKey(ctx, header.Kid)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS key kid %s: %w", header.Kid, err)
	}
	if pubKey == nil {
		return nil, fmt.Errorf("public key for kid %s is nil", header.Kid)
	}

	hashed := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("invalid JWT signature base64: %w", err)
	}
	if err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hashed[:], sig); err != nil {
		return nil, fmt.Errorf("JWT RSA signature verification failed: %w", err)
	}

	if claims.Sub == "" {
		return nil, errors.New("ID token missing 'sub' claim")
	}

	if claims.PreferredUsername == "" {
		if claims.Name != "" {
			claims.PreferredUsername = claims.Name
		} else {
			claims.PreferredUsername = claims.Sub
		}
	}

	return &claims, nil
}

func (c *oidcClient) DetermineUserRole(groups []string) (isAdmin bool, hasAccess bool) {
	adminGroup := c.cfg.AdminGroup
	if adminGroup == "" {
		adminGroup = "jellygate-admins"
	}
	userGroup := c.cfg.UserGroup
	if userGroup == "" {
		userGroup = "jellygate-users"
	}

	for _, g := range groups {
		if g == adminGroup {
			return true, true
		}
		if g == userGroup {
			hasAccess = true
		}
	}

	// SÉCURITÉ : Aucun groupe autorisé présent -> Accès strictement refusé
	return false, hasAccess
}

func (c *oidcClient) getAuthEndpoint(ctx context.Context) (string, error) {
	meta, err := c.getDiscoveryMetadata(ctx)
	if err == nil && meta.AuthorizationEndpoint != "" {
		return meta.AuthorizationEndpoint, nil
	}
	if c.cfg.URL != "" {
		return strings.TrimRight(c.cfg.URL, "/") + "/application/o/authorize/", nil
	}
	if c.cfg.IssuerURL != "" {
		return strings.TrimRight(c.cfg.IssuerURL, "/") + "/application/o/authorize/", nil
	}
	return "", errors.New("unable to resolve OIDC authorization endpoint")
}

func (c *oidcClient) getTokenEndpoint(ctx context.Context) (string, error) {
	meta, err := c.getDiscoveryMetadata(ctx)
	if err == nil && meta.TokenEndpoint != "" {
		return meta.TokenEndpoint, nil
	}
	if c.cfg.URL != "" {
		return strings.TrimRight(c.cfg.URL, "/") + "/application/o/token/", nil
	}
	if c.cfg.IssuerURL != "" {
		return strings.TrimRight(c.cfg.IssuerURL, "/") + "/application/o/token/", nil
	}
	return "", errors.New("unable to resolve OIDC token endpoint")
}

func (c *oidcClient) getJWKSKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	c.jwksMu.RLock()
	key, ok := c.jwksCache[kid]
	last := c.jwksLast
	c.jwksMu.RUnlock()

	if ok && time.Since(last) < 24*time.Hour {
		return key, nil
	}

	// Tentative de résolution de l'URL JWKS via Auto-Discovery
	jwksURL := ""
	if meta, err := c.getDiscoveryMetadata(ctx); err == nil && meta.JWKSURI != "" {
		jwksURL = meta.JWKSURI
	} else if c.cfg.IssuerURL != "" {
		jwksURL = strings.TrimRight(c.cfg.IssuerURL, "/") + "/jwks/"
	} else if c.cfg.URL != "" {
		jwksURL = strings.TrimRight(c.cfg.URL, "/") + "/application/o/jellygate/jwks/"
	}

	if jwksURL == "" {
		return nil, errors.New("JWKS URL could not be resolved")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	var jwks JWKSResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, err
	}

	c.jwksMu.Lock()
	defer c.jwksMu.Unlock()

	for _, k := range jwks.Keys {
		if k.Kty == "RSA" && k.Kid != "" && k.N != "" && k.E != "" {
			pubKey, err := parseRSAPublicKey(k.N, k.E)
			if err == nil {
				c.jwksCache[k.Kid] = pubKey
			}
		}
	}
	c.jwksLast = time.Now()

	key, ok = c.jwksCache[kid]
	if !ok {
		return nil, fmt.Errorf("key kid %s not found in JWKS from %s", kid, jwksURL)
	}
	return key, nil
}

func parseRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, err
	}

	n := new(big.Int).SetBytes(nBytes)
	var e int
	for _, b := range eBytes {
		e = (e << 8) | int(b)
	}

	return &rsa.PublicKey{N: n, E: e}, nil
}

func checkAudience(aud interface{}, clientID string) bool {
	switch v := aud.(type) {
	case string:
		return v == clientID
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && s == clientID {
				return true
			}
		}
	}
	return false
}

func calculateS256Challenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func generateRandomString(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to read cryptographically secure random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b)[:length], nil
}

func setTempCookie(w http.ResponseWriter, name, value string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     CookiePath,
		Expires:  time.Now().Add(CookieTTL),
		MaxAge:   int(CookieTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (c *oidcClient) GetEndSessionURL(ctx context.Context) string {
	meta, err := c.getDiscoveryMetadata(ctx)
	if err == nil && meta != nil && strings.TrimSpace(meta.EndSessionEndpoint) != "" {
		return meta.EndSessionEndpoint
	}
	if strings.TrimSpace(c.cfg.IssuerURL) != "" {
		return strings.TrimRight(strings.TrimSpace(c.cfg.IssuerURL), "/") + "/end-session/"
	}
	if strings.TrimSpace(c.cfg.URL) != "" {
		return strings.TrimRight(strings.TrimSpace(c.cfg.URL), "/") + "/application/o/jellygate/end-session/"
	}
	return "/auth/login"
}
