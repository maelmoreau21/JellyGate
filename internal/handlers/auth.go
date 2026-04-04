// Package handlers contient les gestionnaires HTTP de JellyGate.
//
// Ce fichier implÃ©mente l'authentification admin dÃ©lÃ©guÃ©e Ã  Jellyfin :
//   - Login via POST /Users/AuthenticateByName sur Jellyfin
//   - VÃ©rification que l'utilisateur est administrateur (Policy.IsAdministrator)
//   - Session maintenue via un cookie signÃ© (HMAC-SHA256)
package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

// â”€â”€ Structures de donnÃ©es â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// jellyfinAuthRequest est le corps de la requÃªte POST /Users/AuthenticateByName.
type jellyfinAuthRequest struct {
	Username string `json:"Username"`
	Pw       string `json:"Pw"`
}

// jellyfinAuthResponse contient les champs pertinents de la rÃ©ponse Jellyfin.
type jellyfinAuthResponse struct {
	User struct {
		ID     string `json:"Id"`
		Name   string `json:"Name"`
		Policy struct {
			IsAdministrator bool `json:"IsAdministrator"`
		} `json:"Policy"`
	} `json:"User"`
	AccessToken string `json:"AccessToken"`
}

// â”€â”€ Auth Handler â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// AuthHandler gÃ¨re les routes d'authentification admin.
type AuthHandler struct {
	cfg      *config.Config
	db       *database.DB
	renderer *render.Engine
}

// NewAuthHandler crÃ©e un nouveau AuthHandler.
func NewAuthHandler(cfg *config.Config, db *database.DB, renderer *render.Engine) *AuthHandler {
	return &AuthHandler{cfg: cfg, db: db, renderer: renderer}
}

// LoginPage affiche la page de connexion admin (GET /admin/login).
func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.Error = r.URL.Query().Get("error")
	td.Data["SubmittedUsername"] = strings.TrimSpace(r.URL.Query().Get("username"))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.Section = "login"

	if err := h.renderer.Render(w, "admin/login.html", td); err != nil {
		slog.Error("Erreur rendu login", "error", err)
		http.Error(w, "Erreur serveur : impossible de charger la page", http.StatusInternalServerError)
	}
}

func (h *AuthHandler) redirectLoginError(w http.ResponseWriter, r *http.Request, code, username string) {
	query := url.Values{}
	query.Set("error", code)
	if trimmed := strings.TrimSpace(username); trimmed != "" {
		query.Set("username", trimmed)
	}
	http.Redirect(w, r, "/admin/login?"+query.Encode(), http.StatusSeeOther)
}

// LoginSubmit traite la soumission du formulaire de connexion (POST /admin/login).
//
// Flux :
//  1. RÃ©cupÃ©rer les identifiants du formulaire
//  2. Appeler POST /Users/AuthenticateByName sur Jellyfin
//  3. VÃ©rifier que Policy.IsAdministrator == true
//  4. CrÃ©er un cookie de session signÃ© (HMAC-SHA256)
//  5. Rediriger vers /admin/
func (h *AuthHandler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	// â”€â”€ 1. RÃ©cupÃ©rer les identifiants â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	if err := r.ParseForm(); err != nil {
		slog.Error("Erreur parsing formulaire login", "error", err)
		h.redirectLoginError(w, r, "invalid", "")
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	if username == "" || password == "" {
		slog.Warn("Tentative de login avec champs vides", "remote", r.RemoteAddr)
		h.redirectLoginError(w, r, "required", username)
		return
	}

	// â”€â”€ 2. Authentifier via l'API Jellyfin â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	authResp, err := h.authenticateWithJellyfin(username, password)
	if err != nil {
		slog.Warn("Ã‰chec d'authentification Jellyfin",
			"username", username,
			"remote", r.RemoteAddr,
			"error", err,
		)
		_ = h.db.LogAction("admin.login.failed", username, "", fmt.Sprintf("IP: %s, erreur: %s", r.RemoteAddr, err))

		h.redirectLoginError(w, r, "invalid", username)
		return
	}

	// â”€â”€ 3. Le statut d'administrateur dÃ©termine les permissions â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	isAdmin := authResp.User.Policy.IsAdministrator
	if !isAdmin {
		slog.Info("Utilisateur standard connectÃ©",
			"username", username,
			"jellyfin_id", authResp.User.ID,
		)
	}

	// â”€â”€ 4. CrÃ©er le cookie de session signÃ© â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	sess := session.Payload{
		UserID:   authResp.User.ID,
		Username: authResp.User.Name,
		IsAdmin:  isAdmin,
		Exp:      time.Now().Add(session.Duration).Unix(),
	}

	cookieValue, err := session.Sign(sess, h.cfg.SecretKey)
	if err != nil {
		slog.Error("Erreur lors de la signature de la session", "error", err)
		http.Error(w, "Erreur interne", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     session.CookieName,
		Value:    cookieValue,
		Path:     "/",
		MaxAge:   int(session.Duration.Seconds()),
		HttpOnly: true,                 // Pas accessible en JavaScript
		Secure:   r.TLS != nil,         // Secure si HTTPS
		SameSite: http.SameSiteLaxMode, // Plus compatible que Strict pour le dev/local
	})

	if preferredLang := h.resolvePreferredLang(authResp.User.ID, authResp.User.Name); preferredLang != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     "lang",
			Value:    preferredLang,
			Path:     "/",
			MaxAge:   31536000,
			HttpOnly: false,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteLaxMode,
		})
	}

	slog.Info("Connexion admin rÃ©ussie",
		"username", authResp.User.Name,
		"jellyfin_id", authResp.User.ID,
		"remote", r.RemoteAddr,
	)
	_ = h.db.LogAction("admin.login.success", authResp.User.Name, authResp.User.ID, fmt.Sprintf("IP: %s", r.RemoteAddr))

	// â”€â”€ 5. Rediriger vers le dashboard â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func (h *AuthHandler) resolvePreferredLang(jellyfinID, username string) string {
	var preferred string
	err := h.db.QueryRow(
		`SELECT preferred_lang FROM users WHERE jellyfin_id = ? OR username = ? LIMIT 1`,
		strings.TrimSpace(jellyfinID),
		strings.TrimSpace(username),
	).Scan(&preferred)
	if err != nil {
		return ""
	}
	lang := config.NormalizeLanguageTag(preferred)
	if !config.IsSupportedLanguage(lang) {
		return ""
	}
	return lang
}

// Logout dÃ©connecte l'utilisateur en supprimant le cookie de session (POST /admin/logout).
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     session.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})

	slog.Info("DÃ©connexion admin", "remote", r.RemoteAddr)

	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// â”€â”€ Communication avec l'API Jellyfin â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// authenticateWithJellyfin envoie les identifiants Ã  l'API Jellyfin
// et retourne la rÃ©ponse d'authentification.
func (h *AuthHandler) authenticateWithJellyfin(username, password string) (*jellyfinAuthResponse, error) {
	reqBody, err := json.Marshal(jellyfinAuthRequest{
		Username: username,
		Pw:       password,
	})
	if err != nil {
		return nil, fmt.Errorf("erreur de sÃ©rialisation: %w", err)
	}

	url := fmt.Sprintf("%s/Users/AuthenticateByName", strings.TrimRight(h.cfg.Jellyfin.URL, "/"))

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("erreur de crÃ©ation de la requÃªte: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	embyAuth := fmt.Sprintf(`MediaBrowser Client="JellyGate", Device="Server", DeviceId="jellygate", Version="%s"`, config.AppVersion)
	req.Header.Set("Authorization", embyAuth)
	req.Header.Set("X-Emby-Authorization", embyAuth)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erreur de connexion Ã  Jellyfin (%s): %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("identifiants incorrects (401)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rÃ©ponse inattendue de Jellyfin: HTTP %d", resp.StatusCode)
	}

	var authResp jellyfinAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return nil, fmt.Errorf("erreur de dÃ©codage de la rÃ©ponse Jellyfin: %w", err)
	}

	return &authResp, nil
}
