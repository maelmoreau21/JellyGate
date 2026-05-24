package jellyfin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

type authenticateByNameResponse struct {
	User        User   `json:"User"`
	AccessToken string `json:"AccessToken"`
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

	reqBody, err := json.Marshal(authenticateByNameRequest{ // #nosec G117 -- password is sent directly to Jellyfin over the configured API client.
		Username: username,
		Pw:       password,
	})
	if err != nil {
		return nil, fmt.Errorf("jellyfin.AuthenticateByName: erreur de serialisation: %w", err)
	}

	resp, err := c.doRequestWithToken(http.MethodPost, "/Users/AuthenticateByName", reqBody, "")
	if err != nil {
		return nil, fmt.Errorf("jellyfin.AuthenticateByName: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, jellyfinHTTPError("jellyfin.AuthenticateByName", resp)
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
	accessToken := strings.TrimSpace(authResp.AccessToken)
	if accessToken == "" {
		return nil, fmt.Errorf("jellyfin.AuthenticateByName: reponse Jellyfin sans AccessToken")
	}

	user, err := c.getUserWithToken(userID, accessToken)
	if err != nil {
		return nil, fmt.Errorf("jellyfin.AuthenticateByName: confirmation policy impossible: %w", err)
	}
	if strings.TrimSpace(user.ID) == "" {
		user.ID = userID
	}
	if strings.TrimSpace(user.Name) == "" {
		user.Name = authResp.User.Name
	}

	return user, nil
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
