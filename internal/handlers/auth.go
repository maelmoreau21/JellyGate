// Package handlers contient les gestionnaires HTTP de JellyGate.
//
// Ce fichier implémente l'authentification admin déléguée à Jellyfin :
//   - Login via POST /Users/AuthenticateByName sur Jellyfin
//   - Vérification que l'utilisateur est administrateur (Policy.IsAdministrator)
//   - Session maintenue via un cookie signé (HMAC-SHA256)
package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

// ── Structures de données ───────────────────────────────────────────────────

// jellyfinAuthRequest est le corps de la requête POST /Users/AuthenticateByName.
type jellyfinAuthRequest struct {
	Username string `json:"Username"`
	Pw       string `json:"Pw"`
}

// jellyfinAuthResponse contient les champs pertinents de la réponse Jellyfin.
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

// ── Auth Handler ────────────────────────────────────────────────────────────

// AuthHandler gère les routes d'authentification admin.
type AuthHandler struct {
	cfg      *config.Config
	db       *database.DB
	renderer *render.Engine
}

// NewAuthHandler crée un nouveau AuthHandler.
func NewAuthHandler(cfg *config.Config, db *database.DB, renderer *render.Engine) *AuthHandler {
	return &AuthHandler{cfg: cfg, db: db, renderer: renderer}
}

// LoginPage affiche la page de connexion admin (GET /admin/login).
func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	td := h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context()))
	td.Error = r.URL.Query().Get("error")

	if err := h.renderer.Render(w, "admin/login.html", td); err != nil {
		slog.Error("Erreur rendu login", "error", err)
		http.Error(w, "Erreur serveur : impossible de charger la page", http.StatusInternalServerError)
	}
}

// LoginSubmit traite la soumission du formulaire de connexion (POST /admin/login).
//
// Flux :
//  1. Récupérer les identifiants du formulaire
//  2. Appeler POST /Users/AuthenticateByName sur Jellyfin
//  3. Vérifier que Policy.IsAdministrator == true
//  4. Créer un cookie de session signé (HMAC-SHA256)
//  5. Rediriger vers /admin/
func (h *AuthHandler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	// ── 1. Récupérer les identifiants ───────────────────────────────────
	if err := r.ParseForm(); err != nil {
		slog.Error("Erreur parsing formulaire login", "error", err)
		http.Error(w, "Requête invalide", http.StatusBadRequest)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	if username == "" || password == "" {
		slog.Warn("Tentative de login avec champs vides", "remote", r.RemoteAddr)
		http.Error(w, "Nom d'utilisateur et mot de passe requis", http.StatusBadRequest)
		return
	}

	// ── 2. Authentifier via l'API Jellyfin ──────────────────────────────
	authResp, err := h.authenticateWithJellyfin(username, password)
	if err != nil {
		slog.Warn("Échec d'authentification Jellyfin",
			"username", username,
			"remote", r.RemoteAddr,
			"error", err,
		)
		_ = h.db.LogAction("admin.login.failed", username, "", fmt.Sprintf("IP: %s, erreur: %s", r.RemoteAddr, err))

		http.Redirect(w, r, "/admin/login?error=invalid", http.StatusSeeOther)
		return
	}

	// ── 3. Le statut d'administrateur détermine les permissions ────────────
	isAdmin := authResp.User.Policy.IsAdministrator
	if !isAdmin {
		slog.Info("Utilisateur standard connecté",
			"username", username,
			"jellyfin_id", authResp.User.ID,
		)
	}

	// ── 4. Créer le cookie de session signé ─────────────────────────────
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
		HttpOnly: true,                    // Pas accessible en JavaScript
		Secure:   r.TLS != nil,            // Secure si HTTPS
		SameSite: http.SameSiteStrictMode, // Protection CSRF
	})

	slog.Info("Connexion admin réussie",
		"username", authResp.User.Name,
		"jellyfin_id", authResp.User.ID,
		"remote", r.RemoteAddr,
	)
	_ = h.db.LogAction("admin.login.success", authResp.User.Name, authResp.User.ID, fmt.Sprintf("IP: %s", r.RemoteAddr))

	// ── 5. Rediriger vers le dashboard ──────────────────────────────────
	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

// Logout déconnecte l'utilisateur en supprimant le cookie de session (POST /admin/logout).
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

	slog.Info("Déconnexion admin", "remote", r.RemoteAddr)

	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// ── Communication avec l'API Jellyfin ───────────────────────────────────────

// authenticateWithJellyfin envoie les identifiants à l'API Jellyfin
// et retourne la réponse d'authentification.
func (h *AuthHandler) authenticateWithJellyfin(username, password string) (*jellyfinAuthResponse, error) {
	reqBody, err := json.Marshal(jellyfinAuthRequest{
		Username: username,
		Pw:       password,
	})
	if err != nil {
		return nil, fmt.Errorf("erreur de sérialisation: %w", err)
	}

	url := fmt.Sprintf("%s/Users/AuthenticateByName", strings.TrimRight(h.cfg.Jellyfin.URL, "/"))

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("erreur de création de la requête: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Emby-Authorization",
		`MediaBrowser Client="JellyGate", Device="Server", DeviceId="jellygate", Version="0.1.0"`)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erreur de connexion à Jellyfin (%s): %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("identifiants incorrects (401)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("réponse inattendue de Jellyfin: HTTP %d", resp.StatusCode)
	}

	var authResp jellyfinAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return nil, fmt.Errorf("erreur de décodage de la réponse Jellyfin: %w", err)
	}

	return &authResp, nil
}
