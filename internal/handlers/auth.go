// Package handlers contient les gestionnaires HTTP de JellyGate.
//
// Ce fichier implÃƒÂ©mente l'authentification admin dÃƒÂ©lÃƒÂ©guÃƒÂ©e ÃƒÂ  Jellyfin :
//   - Login via POST /Users/AuthenticateByName sur Jellyfin
//   - VÃƒÂ©rification que l'utilisateur est administrateur (Policy.IsAdministrator)
//   - Session maintenue via un cookie signÃƒÂ© (HMAC-SHA256)
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
	jgldap "github.com/maelmoreau21/JellyGate/internal/ldap"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

// Ã¢â€�â‚¬Ã¢â€�â‚¬ Structures de donnÃƒÂ©es Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// jellyfinAuthRequest est le corps de la requÃƒÂªte POST /Users/AuthenticateByName.
type jellyfinAuthRequest struct {
	Username string `json:"Username"`
	Pw       string `json:"Pw"`
}

// jellyfinAuthResponse contient les champs pertinents de la rÃƒÂ©ponse Jellyfin.
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

// Ã¢â€�â‚¬Ã¢â€�â‚¬ Auth Handler Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// AuthHandler gÃƒÂ¨re les routes d'authentification admin.
type AuthHandler struct {
	cfg      *config.Config
	db       *database.DB
	renderer *render.Engine
}

// NewAuthHandler crÃƒÂ©e un nouveau AuthHandler.
func NewAuthHandler(cfg *config.Config, db *database.DB, renderer *render.Engine) *AuthHandler {
	return &AuthHandler{cfg: cfg, db: db, renderer: renderer}
}

func (h *AuthHandler) tr(r *http.Request, key, fallback string) string {
	if h.renderer == nil {
		return fallback
	}
	lang := jgmw.LangFromContext(r.Context())
	value := h.renderer.Translate(lang, key)
	if value == "["+key+"]" {
		return fallback
	}
	return value
}

// LoginPage affiche la page de connexion admin (GET /admin/login).
func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.Error = r.URL.Query().Get("error")
	td.Data["SubmittedUsername"] = strings.TrimSpace(r.URL.Query().Get("username"))
	links := resolvePortalLinks(h.cfg, h.db)
	td.Data["JellyfinURL"] = links.JellyfinURL
	td.Data["JellyseerrURL"] = links.JellyseerrURL
	td.Data["JellyTrackURL"] = links.JellyTrackURL
	td.Section = "login"

	if err := h.renderer.Render(w, "admin/login.html", td); err != nil {
		slog.Error("Erreur rendu login", "error", err)
		http.Error(w, h.tr(r, "common_server_error_page", "Server error: unable to load page"), http.StatusInternalServerError)
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
//  1. RÃƒÂ©cupÃƒÂ©rer les identifiants du formulaire
//  2. Appeler POST /Users/AuthenticateByName sur Jellyfin
//  3. VÃƒÂ©rifier que Policy.IsAdministrator == true
//  4. CrÃƒÂ©er un cookie de session signÃƒÂ© (HMAC-SHA256)
//  5. Rediriger vers /admin/
func (h *AuthHandler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	// Ã¢â€�â‚¬Ã¢â€�â‚¬ 1. RÃƒÂ©cupÃƒÂ©rer les identifiants Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬
	if err := r.ParseForm(); err != nil {
		slog.Error("Erreur parsing formulaire login", "error", err)
		h.redirectLoginError(w, r, "invalid", "")
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	rememberMe := r.FormValue("remember_me") == "1"

	if username == "" || password == "" {
		slog.Warn("Tentative de login avec champs vides", "remote", r.RemoteAddr)
		h.redirectLoginError(w, r, "required", username)
		return
	}

	// Ã¢â€�â‚¬Ã¢â€�â‚¬ 2. Authentifier via l'API Jellyfin Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬
	authResp, err := h.authenticateWithJellyfin(username, password)
	if err != nil {
		slog.Warn("Ãƒâ€°chec d'authentification Jellyfin",
			"username", username,
			"remote", r.RemoteAddr,
			"error", err,
		)
		_ = h.db.LogAction("admin.login.failed", username, "", fmt.Sprintf("IP: %s, erreur: %s", r.RemoteAddr, err))

		h.redirectLoginError(w, r, "invalid", username)
		return
	}

	// Ã¢â€�â‚¬Ã¢â€�â‚¬ 3. Le statut d'administrateur dÃƒÂ©termine les permissions Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬
	isAdmin := authResp.User.Policy.IsAdministrator

	ldapCfg, ldapErr := h.db.GetLDAPConfig()
	if ldapErr != nil {
		slog.Warn("Impossible de charger la configuration LDAP pendant le login", "error", ldapErr)
	}

	if ldapErr == nil && ldapCfg.Enabled {
		lookupUsername := strings.TrimSpace(username)
		if lookupUsername == "" {
			lookupUsername = strings.TrimSpace(authResp.User.Name)
		}

		ldapClient := jgldap.New(ldapCfg)
		entry, ldapIsAdmin, accessErr := ldapClient.ResolveUserAccess(lookupUsername)
		if accessErr != nil {
			slog.Warn("Verification LDAP refusee pendant le login",
				"username", lookupUsername,
				"remote", r.RemoteAddr,
				"error", accessErr,
			)
			_ = h.db.LogAction("admin.login.failed", lookupUsername, "", fmt.Sprintf("IP: %s, controle LDAP impossible: %v", r.RemoteAddr, accessErr))
			h.redirectLoginError(w, r, "invalid", lookupUsername)
			return
		}

		if entry == nil {
			// If the user is not found in LDAP but is an administrator in Jellyfin,
			// allow the login and keep a warning. Otherwise deny access.
			if !isAdmin {
				slog.Info("Acces refuse par le filtre de recherche LDAP",
					"username", lookupUsername,
					"remote", r.RemoteAddr,
				)
				_ = h.db.LogAction("admin.login.failed", lookupUsername, "", fmt.Sprintf("IP: %s, filtre LDAP: acces refuse", r.RemoteAddr))
				h.redirectLoginError(w, r, "invalid", lookupUsername)
				return
			}

			slog.Warn("Utilisateur introuvable en LDAP mais administrateur Jellyfin : accès accordé (mode fallback)",
				"username", lookupUsername,
				"remote", r.RemoteAddr,
			)
		}

		// When LDAP is active, consider the user an admin if they are either
		// an admin in Jellyfin or match the LDAP admin filter.
		isAdmin = isAdmin || ldapIsAdmin
	}

	if !isAdmin {
		slog.Info("Utilisateur standard connectÃƒÂ©",
			"username", username,
			"jellyfin_id", authResp.User.ID,
		)
	}

	// Ã¢â€�â‚¬Ã¢â€�â‚¬ 4. CrÃƒÂ©er le cookie de session signÃƒÂ© Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬
	sessionDuration := session.Duration
	if rememberMe {
		sessionDuration = session.RememberDuration
	}
	sessionExpiresAt := time.Now().Add(sessionDuration)

	sess := session.Payload{
		UserID:   authResp.User.ID,
		Username: authResp.User.Name,
		IsAdmin:  isAdmin,
		Exp:      sessionExpiresAt.Unix(),
	}

	cookieValue, err := session.Sign(sess, h.cfg.SecretKey)
	if err != nil {
		slog.Error("Erreur lors de la signature de la session", "error", err)
		http.Error(w, h.tr(r, "common_server_error", "Server error"), http.StatusInternalServerError)
		return
	}

	// #nosec G124 -- Secure is enabled whenever the configured public URL or request is HTTPS.
	http.SetCookie(w, &http.Cookie{
		Name:     session.CookieName,
		Value:    cookieValue,
		Path:     "/",
		MaxAge:   int(sessionDuration.Seconds()),
		Expires:  sessionExpiresAt,
		HttpOnly: true,                                  // Pas accessible en JavaScript
		Secure:   jgmw.RequestIsHTTPS(r, h.cfg.BaseURL), // Secure si HTTPS
		SameSite: http.SameSiteLaxMode,                  // Plus compatible que Strict pour le dev/local
	})

	if preferredLang := h.resolvePreferredLang(authResp.User.ID, authResp.User.Name); preferredLang != "" {
		// #nosec G124 -- language preference is intentionally readable by frontend language switching code.
		http.SetCookie(w, &http.Cookie{
			Name:     "lang",
			Value:    preferredLang,
			Path:     "/",
			MaxAge:   31536000,
			HttpOnly: false,
			Secure:   jgmw.RequestIsHTTPS(r, h.cfg.BaseURL),
			SameSite: http.SameSiteLaxMode,
		})
	}

	slog.Info("Connexion admin rÃƒÂ©ussie",
		"username", authResp.User.Name,
		"jellyfin_id", authResp.User.ID,
		"remote", r.RemoteAddr,
	)
	_ = h.db.LogAction("admin.login.success", authResp.User.Name, authResp.User.ID, fmt.Sprintf("IP: %s", r.RemoteAddr))

	// Ã¢â€�â‚¬Ã¢â€�â‚¬ 5. Rediriger vers le dashboard Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬
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

// Logout dÃƒÂ©connecte l'utilisateur en supprimant le cookie de session (POST /admin/logout).
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// #nosec G124 -- clearing uses the same Secure policy as the session cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     session.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   jgmw.RequestIsHTTPS(r, h.cfg.BaseURL),
		SameSite: http.SameSiteStrictMode,
	})

	slog.Info("DÃƒÂ©connexion admin", "remote", r.RemoteAddr)

	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// Ã¢â€�â‚¬Ã¢â€�â‚¬ Communication avec l'API Jellyfin Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬Ã¢â€�â‚¬

// authenticateWithJellyfin envoie les identifiants ÃƒÂ  l'API Jellyfin
// et retourne la rÃƒÂ©ponse d'authentification.
func (h *AuthHandler) authenticateWithJellyfin(username, password string) (*jellyfinAuthResponse, error) {
	reqBody, err := json.Marshal(jellyfinAuthRequest{
		Username: username,
		Pw:       password,
	})
	if err != nil {
		return nil, fmt.Errorf("erreur de sÃƒÂ©rialisation: %w", err)
	}

	url := fmt.Sprintf("%s/Users/AuthenticateByName", strings.TrimRight(h.cfg.Jellyfin.URL, "/"))

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("erreur de crÃƒÂ©ation de la requÃƒÂªte: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	embyAuth := fmt.Sprintf(`MediaBrowser Client="JellyGate", Device="Server", DeviceId="jellygate", Version="%s"`, config.AppVersion)
	req.Header.Set("Authorization", embyAuth)
	req.Header.Set("X-Emby-Authorization", embyAuth)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erreur de connexion ÃƒÂ  Jellyfin (%s): %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("identifiants incorrects (401)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rÃƒÂ©ponse inattendue de Jellyfin: HTTP %d", resp.StatusCode)
	}

	var authResp jellyfinAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return nil, fmt.Errorf("erreur de dÃƒÂ©codage de la rÃƒÂ©ponse Jellyfin: %w", err)
	}

	return &authResp, nil
}
