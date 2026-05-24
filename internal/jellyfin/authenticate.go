package jellyfin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var sensitiveHTTPDetailPattern = regexp.MustCompile(`(?i)(access[_-]?token|token|password|pw)(["']?\s*[:=]\s*["']?)[^"',\s}]+`)

type authenticateByNameRequest struct {
	Username string `json:"Username"`
	Pw       string `json:"Pw"`
}

type authenticateByNamePasswordRequest struct {
	Username string `json:"Username"`
	Password string `json:"Password"`
}

type authenticateByNameLowerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authenticateByNameResponse struct {
	User        User   `json:"User"`
	AccessToken string `json:"AccessToken"`
}

type authenticateByNameAttempt struct {
	Path    string
	Payload any
}

// AuthenticateByName authenticates a Jellyfin user and reloads their full
// profile with the temporary user token so Policy is confirmed from Jellyfin.
func (c *Client) AuthenticateByName(username, password string) (*User, error) {
	if c == nil {
		return nil, fmt.Errorf("jellyfin.AuthenticateByName: client nil")
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, fmt.Errorf("jellyfin.AuthenticateByName: username vide")
	}
	if password == "" {
		return nil, fmt.Errorf("jellyfin.AuthenticateByName: mot de passe vide")
	}

	attempts := []authenticateByNameAttempt{
		{Path: "/Users/AuthenticateByName", Payload: authenticateByNameRequest{Username: username, Pw: password}},
		{Path: "/Users/AuthenticateByName", Payload: authenticateByNamePasswordRequest{Username: username, Password: password}},
		{Path: "/Users/AuthenticateByName", Payload: authenticateByNameLowerRequest{Username: username, Password: password}},
		{Path: "/Users/authenticatebyname", Payload: authenticateByNameRequest{Username: username, Pw: password}},
		{Path: "/Users/authenticatebyname", Payload: authenticateByNamePasswordRequest{Username: username, Password: password}},
		{Path: "/Users/authenticatebyname", Payload: authenticateByNameLowerRequest{Username: username, Password: password}},
	}

	var lastErr error
	for _, attempt := range attempts {
		user, err := c.authenticateByNameAttempt(attempt)
		if err == nil {
			return user, nil
		}
		if isAuthRejected(err) {
			return nil, err
		}
		lastErr = err
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("jellyfin.AuthenticateByName: authentification impossible")
}

func (c *Client) authenticateByNameAttempt(attempt authenticateByNameAttempt) (*User, error) {
	reqBody, err := json.Marshal(attempt.Payload) // #nosec G117 -- password is sent directly to Jellyfin over the configured API client.
	if err != nil {
		return nil, fmt.Errorf("jellyfin.AuthenticateByName: erreur de serialisation: %w", err)
	}

	resp, err := c.doRequestWithToken(http.MethodPost, attempt.Path, reqBody, "")
	if err != nil {
		return nil, fmt.Errorf("jellyfin.AuthenticateByName: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, authRejectedError{err: jellyfinHTTPError("jellyfin.AuthenticateByName", resp)}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, jellyfinHTTPError("jellyfin.AuthenticateByName", resp)
	}

	var authResp authenticateByNameResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return nil, fmt.Errorf("jellyfin.AuthenticateByName: erreur de decodage: %w", err)
	}

	userID := strings.TrimSpace(authResp.User.ID)
	if userID == "" {
		return nil, fmt.Errorf("jellyfin.AuthenticateByName: reponse Jellyfin sans User.Id")
	}

	return c.confirmAuthenticatedUser(authResp)
}

func (c *Client) confirmAuthenticatedUser(authResp authenticateByNameResponse) (*User, error) {
	userID := strings.TrimSpace(authResp.User.ID)
	if userID == "" {
		return nil, fmt.Errorf("jellyfin.AuthenticateByName: reponse Jellyfin sans User.Id")
	}
	accessToken := strings.TrimSpace(authResp.AccessToken)

	var confirmErr error
	if accessToken != "" {
		user, err := c.getUserWithToken(userID, accessToken)
		if err == nil {
			mergeAuthenticatedUserDefaults(user, authResp.User)
			return user, nil
		}
		confirmErr = err
	}

	if strings.TrimSpace(c.apiKey) != "" {
		user, err := c.GetUser(userID)
		if err == nil {
			mergeAuthenticatedUserDefaults(user, authResp.User)
			return user, nil
		}
		if confirmErr == nil {
			confirmErr = err
		}
	}

	user := authResp.User
	user.Policy.IsAdministrator = false
	var safeErr any
	if confirmErr != nil {
		safeErr = sanitizeHTTPDetail(confirmErr.Error())
	}
	slog.Warn("Policy Jellyfin impossible a confirmer apres authentification; session accordee sans droits admin",
		"jellyfin_id", userID,
		"error", safeErr,
	)
	return &user, nil
}

func mergeAuthenticatedUserDefaults(user *User, fallback User) {
	if user == nil {
		return
	}
	userID := strings.TrimSpace(fallback.ID)
	if strings.TrimSpace(user.ID) == "" {
		user.ID = userID
	}
	if strings.TrimSpace(user.Name) == "" {
		user.Name = fallback.Name
	}
}

func (c *Client) getUserWithToken(userID, token string) (*User, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("userID vide")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("token vide")
	}

	resp, err := c.doRequestWithToken(http.MethodGet, fmt.Sprintf("/Users/%s", url.PathEscape(userID)), nil, token)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, jellyfinHTTPError("jellyfin.GetUserWithToken", resp)
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("erreur de decodage: %w", err)
	}
	return &user, nil
}

func (c *Client) doRequestWithToken(method, path string, body []byte, token string) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("erreur de creation de la requete %s %s: %w", method, path, err)
	}

	req.Header.Set("Authorization", AuthorizationHeader(token))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := c.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erreur de connexion a Jellyfin %s %s: %w", method, path, err)
	}
	return resp, nil
}

func jellyfinHTTPError(operation string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		return fmt.Errorf("%s: HTTP %d", operation, resp.StatusCode)
	}
	return fmt.Errorf("%s: HTTP %d: %s", operation, resp.StatusCode, sanitizeHTTPDetail(detail))
}

func sanitizeHTTPDetail(detail string) string {
	return sensitiveHTTPDetailPattern.ReplaceAllString(detail, `${1}${2}[REDACTED]`)
}

type authRejectedError struct {
	err error
}

func (e authRejectedError) Error() string {
	return e.err.Error()
}

func (e authRejectedError) Unwrap() error {
	return e.err
}

func isAuthRejected(err error) bool {
	var rejected authRejectedError
	return errors.As(err, &rejected)
}
