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
	Path     string
	Shape    string
	Official bool
	Payload  any
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
		{Path: "/Users/AuthenticateByName", Shape: "Username/Pw", Official: true, Payload: authenticateByNameRequest{Username: username, Pw: password}},
		{Path: "/Users/AuthenticateByName", Shape: "Username/Password", Payload: authenticateByNamePasswordRequest{Username: username, Password: password}},
		{Path: "/Users/AuthenticateByName", Shape: "username/password", Payload: authenticateByNameLowerRequest{Username: username, Password: password}},
		{Path: "/Users/authenticatebyname", Shape: "Username/Pw", Payload: authenticateByNameRequest{Username: username, Pw: password}},
		{Path: "/Users/authenticatebyname", Shape: "Username/Password", Payload: authenticateByNamePasswordRequest{Username: username, Password: password}},
		{Path: "/Users/authenticatebyname", Shape: "username/password", Payload: authenticateByNameLowerRequest{Username: username, Password: password}},
	}

	var lastErr error
	for _, attempt := range attempts {
		user, err := c.authenticateByNameAttempt(attempt)
		if err == nil {
			return user, nil
		}
		if isAuthRejected(err) && attempt.Official {
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
		c.recordAuthAttempt(AuthAttemptSummary{
			Endpoint:     attempt.Path,
			PayloadShape: attempt.Shape,
			Error:        err.Error(),
		})
		slog.Warn("Tentative auth Jellyfin echouee",
			"endpoint", attempt.Path,
			"payload_shape", attempt.Shape,
			"error", sanitizeHTTPDetail(err.Error()),
		)
		return nil, fmt.Errorf("jellyfin.AuthenticateByName: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		detail := readHTTPDetail(resp.Body)
		err := authRejectedError{err: authRejectedStatusError(resp.StatusCode, detail)}
		c.recordAuthAttempt(AuthAttemptSummary{
			Endpoint:     attempt.Path,
			PayloadShape: attempt.Shape,
			Status:       resp.StatusCode,
			Error:        err.Error(),
			Response:     detail,
		})
		slog.Warn("Tentative auth Jellyfin refusee",
			"endpoint", attempt.Path,
			"payload_shape", attempt.Shape,
			"status", resp.StatusCode,
			"response", sanitizeHTTPDetail(detail),
		)
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		detail := readHTTPDetail(resp.Body)
		err := jellyfinHTTPStatusError("jellyfin.AuthenticateByName", resp.StatusCode, detail)
		c.recordAuthAttempt(AuthAttemptSummary{
			Endpoint:     attempt.Path,
			PayloadShape: attempt.Shape,
			Status:       resp.StatusCode,
			Error:        err.Error(),
			Response:     detail,
		})
		slog.Warn("Tentative auth Jellyfin echouee",
			"endpoint", attempt.Path,
			"payload_shape", attempt.Shape,
			"status", resp.StatusCode,
			"response", sanitizeHTTPDetail(detail),
		)
		return nil, err
	}

	var authResp authenticateByNameResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		c.recordAuthAttempt(AuthAttemptSummary{
			Endpoint:     attempt.Path,
			PayloadShape: attempt.Shape,
			Status:       resp.StatusCode,
			Error:        err.Error(),
		})
		return nil, fmt.Errorf("jellyfin.AuthenticateByName: erreur de decodage: %w", err)
	}

	userID := strings.TrimSpace(authResp.User.ID)
	if userID == "" {
		c.recordAuthAttempt(AuthAttemptSummary{
			Endpoint:     attempt.Path,
			PayloadShape: attempt.Shape,
			Status:       resp.StatusCode,
			Error:        "reponse Jellyfin sans User.Id",
		})
		return nil, fmt.Errorf("jellyfin.AuthenticateByName: reponse Jellyfin sans User.Id")
	}

	c.recordAuthAttempt(AuthAttemptSummary{
		Endpoint:     attempt.Path,
		PayloadShape: attempt.Shape,
		Status:       resp.StatusCode,
	})

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
	return jellyfinHTTPStatusError(operation, resp.StatusCode, readHTTPDetail(resp.Body))
}

func jellyfinHTTPStatusError(operation string, status int, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return fmt.Errorf("%s: HTTP %d", operation, status)
	}
	return fmt.Errorf("%s: HTTP %d: %s", operation, status, sanitizeHTTPDetail(detail))
}

func authRejectedStatusError(status int, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return fmt.Errorf("jellyfin.AuthenticateByName: identifiants refuses (HTTP %d)", status)
	}
	return fmt.Errorf("jellyfin.AuthenticateByName: identifiants refuses (HTTP %d): %s", status, sanitizeHTTPDetail(detail))
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
