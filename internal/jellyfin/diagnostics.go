package jellyfin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// AuthAttemptSummary captures the last Jellyfin authentication attempt without secrets.
type AuthAttemptSummary struct {
	Time         time.Time `json:"time,omitempty"`
	Endpoint     string    `json:"endpoint,omitempty"`
	PayloadShape string    `json:"payload_shape,omitempty"`
	Status       int       `json:"status,omitempty"`
	Error        string    `json:"error,omitempty"`
	Response     string    `json:"response,omitempty"`
}

// AuthDiagnostics summarizes Jellyfin connectivity and API-key validity without secrets.
type AuthDiagnostics struct {
	BaseURL           string             `json:"base_url"`
	APIKeyConfigured  bool               `json:"api_key_configured"`
	APIKeyFingerprint string             `json:"api_key_fingerprint,omitempty"`
	APIKeyValid       bool               `json:"api_key_valid"`
	PublicStatus      int                `json:"public_status,omitempty"`
	AuthStatus        int                `json:"auth_status,omitempty"`
	ServerName        string             `json:"server_name,omitempty"`
	Version           string             `json:"version,omitempty"`
	PublicError       string             `json:"public_error,omitempty"`
	AuthError         string             `json:"auth_error,omitempty"`
	LastAuth          AuthAttemptSummary `json:"last_auth,omitempty"`
}

type systemInfoResponse struct {
	ID         string `json:"Id"`
	ServerName string `json:"ServerName"`
	Version    string `json:"Version"`
}

// Diagnostics probes Jellyfin public info and configured API-key auth.
func (c *Client) Diagnostics() AuthDiagnostics {
	if c == nil {
		return AuthDiagnostics{
			PublicError: "client Jellyfin nil",
			AuthError:   "client Jellyfin nil",
		}
	}
	diag := AuthDiagnostics{
		BaseURL:           c.BaseURL(),
		APIKeyConfigured:  strings.TrimSpace(c.apiKey) != "",
		APIKeyFingerprint: FingerprintSecret(c.apiKey),
		LastAuth:          c.LastAuthAttempt(),
	}

	if info, status, detail, err := c.fetchSystemInfo("/System/Info/Public", ""); err == nil {
		diag.PublicStatus = status
		diag.ServerName = info.ServerName
		diag.Version = info.Version
	} else {
		diag.PublicStatus = status
		diag.PublicError = diagnosticError(err, detail)
	}

	if !diag.APIKeyConfigured {
		diag.AuthError = "JELLYFIN_API_KEY absent"
		return diag
	}
	if info, status, detail, err := c.fetchSystemInfo("/System/Info", c.apiKey); err == nil {
		diag.AuthStatus = status
		diag.APIKeyValid = true
		if diag.ServerName == "" {
			diag.ServerName = info.ServerName
		}
		if diag.Version == "" {
			diag.Version = info.Version
		}
	} else {
		diag.AuthStatus = status
		diag.AuthError = diagnosticError(err, detail)
	}

	return diag
}

// LogDiagnostics writes a safe one-line Jellyfin diagnostic to logs.
func (c *Client) LogDiagnostics() {
	diag := c.Diagnostics()
	attrs := []any{
		"base_url", diag.BaseURL,
		"api_key_configured", diag.APIKeyConfigured,
		"api_key_fingerprint", diag.APIKeyFingerprint,
		"api_key_valid", diag.APIKeyValid,
		"public_status", diag.PublicStatus,
		"auth_status", diag.AuthStatus,
		"server_name", diag.ServerName,
		"version", diag.Version,
	}
	if diag.PublicError != "" {
		attrs = append(attrs, "public_error", diag.PublicError)
	}
	if diag.AuthError != "" {
		attrs = append(attrs, "auth_error", diag.AuthError)
	}
	if diag.PublicError != "" || (diag.APIKeyConfigured && !diag.APIKeyValid) {
		slog.Warn("Diagnostic Jellyfin", attrs...)
		return
	}
	slog.Info("Diagnostic Jellyfin", attrs...)
}

func (c *Client) fetchSystemInfo(path, token string) (systemInfoResponse, int, string, error) {
	var info systemInfoResponse
	resp, err := c.doRequestWithToken(context.Background(), http.MethodGet, path, nil, token)
	if err != nil {
		return info, 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail := readHTTPDetail(resp.Body)
		return info, resp.StatusCode, detail, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return info, resp.StatusCode, "", err
	}
	return info, resp.StatusCode, "", nil
}

// BaseURL returns the normalized Jellyfin URL without credentials.
func (c *Client) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

// LastAuthAttempt returns a copy of the latest authentication attempt summary.
func (c *Client) LastAuthAttempt() AuthAttemptSummary {
	if c == nil {
		return AuthAttemptSummary{}
	}
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	return c.lastAuth
}

func (c *Client) recordAuthAttempt(summary AuthAttemptSummary) {
	if c == nil {
		return
	}
	summary.Time = time.Now().UTC()
	summary.Error = sanitizeHTTPDetail(summary.Error)
	summary.Response = sanitizeHTTPDetail(summary.Response)
	c.authMu.Lock()
	c.lastAuth = summary
	c.authMu.Unlock()
}

// FingerprintSecret returns a short stable fingerprint for logs/debug output.
func FingerprintSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])[:12]
}

func diagnosticError(err error, detail string) string {
	if detail = strings.TrimSpace(detail); detail != "" {
		return sanitizeHTTPDetail(detail)
	}
	if err != nil {
		return sanitizeHTTPDetail(err.Error())
	}
	return ""
}

func readHTTPDetail(body io.Reader) string {
	raw, _ := io.ReadAll(io.LimitReader(body, 4096))
	return strings.TrimSpace(string(raw))
}
