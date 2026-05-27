// Package handlers contains JellyGate HTTP handlers.
package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	jgldap "github.com/maelmoreau21/JellyGate/internal/ldap"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

// AuthHandler gere les routes d'authentification admin.
type AuthHandler struct {
	cfg      *config.Config
	db       *database.DB
	jfClient *jellyfin.Client
	renderer *render.Engine
}

// NewAuthHandler cree un nouveau AuthHandler.
func NewAuthHandler(cfg *config.Config, db *database.DB, jf *jellyfin.Client, renderer *render.Engine) *AuthHandler {
	return &AuthHandler{cfg: cfg, db: db, jfClient: jf, renderer: renderer}
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
	if h.hasValidSession(r) {
		http.Redirect(w, r, "/admin/", http.StatusSeeOther)
		return
	}

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

func (h *AuthHandler) hasValidSession(r *http.Request) bool {
	if h == nil || h.cfg == nil || r == nil {
		return false
	}
	cookie, err := r.Cookie(session.CookieName)
	if err != nil {
		return false
	}
	sess, err := session.Verify(cookie.Value, h.cfg.SecretKey)
	return err == nil && h.sessionAccepted(sess)
}

func (h *AuthHandler) sessionAccepted(sess *session.Payload) bool {
	if sess == nil {
		return false
	}
	if h == nil || h.db == nil {
		return true
	}
	cfg, err := h.db.GetAuthSessionConfig()
	if err != nil {
		slog.Warn("Impossible de lire la politique de session", "error", err)
		return true
	}
	return cfg.AcceptsIssuedAt(sess.Iat)
}

func (h *AuthHandler) rememberSessionDuration() time.Duration {
	if h == nil || h.db == nil {
		return session.RememberDuration
	}
	cfg, err := h.db.GetAuthSessionConfig()
	if err != nil {
		slog.Warn("Impossible de lire la duree de session persistante", "error", err)
		return session.RememberDuration
	}
	if cfg.Remember30Days {
		return session.RememberDuration
	}
	return session.IndefiniteDuration
}

func (h *AuthHandler) redirectLoginError(w http.ResponseWriter, r *http.Request, code, username string) {
	query := url.Values{}
	query.Set("error", code)
	if trimmed := strings.TrimSpace(username); trimmed != "" {
		query.Set("username", trimmed)
	}
	http.Redirect(w, r, "/admin/login?"+query.Encode(), http.StatusSeeOther)
}

func (h *AuthHandler) logAction(action, actor, target, details string) {
	if h == nil || h.db == nil {
		return
	}
	_ = h.db.LogAction(action, actor, target, details)
}

// LoginSubmit traite la soumission du formulaire de connexion (POST /admin/login).
func (h *AuthHandler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
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

	authUser, err := h.authenticateWithJellyfin(username, password)
	if err != nil {
		slog.Warn("Echec d'authentification Jellyfin",
			"username", username,
			"remote", r.RemoteAddr,
			"error", err,
		)
		h.logAction("admin.login.failed", username, "", fmt.Sprintf("IP: %s, erreur: %s", r.RemoteAddr, err))
		logSecurityEvent(h.db, r, "admin_login", "admin.login.failed", "warning", username, "", "Echec de connexion admin", map[string]string{"error": err.Error()})
		h.redirectLoginError(w, r, "invalid", username)
		return
	}
	if authUser == nil {
		h.logAction("admin.login.failed", username, "", fmt.Sprintf("IP: %s, erreur: reponse Jellyfin vide", r.RemoteAddr))
		logSecurityEvent(h.db, r, "admin_login", "admin.login.failed", "warning", username, "", "Echec de connexion admin", map[string]string{"error": "reponse Jellyfin vide"})
		h.redirectLoginError(w, r, "invalid", username)
		return
	}

	authUsername := strings.TrimSpace(authUser.Name)
	if authUsername == "" {
		authUsername = username
	}
	authUserID := strings.TrimSpace(authUser.ID)
	isAdmin := authUser.Policy.IsAdministrator

	if h.db != nil {
		ldapCfg, ldapErr := h.db.GetLDAPConfig()
		if ldapErr != nil {
			slog.Warn("Impossible de charger la configuration LDAP pendant le login", "error", ldapErr)
		}

		if ldapErr == nil && ldapCfg.Enabled {
			lookupUsername := username
			if lookupUsername == "" {
				lookupUsername = authUsername
			}

			ldapClient := jgldap.New(ldapCfg)
			entry, ldapIsAdmin, accessErr := ldapClient.ResolveUserAccess(lookupUsername)
			if accessErr != nil {
				if isAdmin {
					slog.Warn("Verification LDAP impossible, acces accorde par fallback administrateur Jellyfin",
						"username", lookupUsername,
						"remote", r.RemoteAddr,
						"error", accessErr,
					)
					h.logAction("admin.login.ldap_fallback", lookupUsername, authUserID, fmt.Sprintf("IP: %s, controle LDAP impossible: %v", r.RemoteAddr, accessErr))
				} else {
					slog.Warn("Verification LDAP refusee pendant le login",
						"username", lookupUsername,
						"remote", r.RemoteAddr,
						"error", accessErr,
					)
					h.logAction("admin.login.failed", lookupUsername, "", fmt.Sprintf("IP: %s, controle LDAP impossible: %v", r.RemoteAddr, accessErr))
					logSecurityEvent(h.db, r, "admin_login", "admin.login.failed", "warning", lookupUsername, "", "Controle LDAP impossible", map[string]string{"error": accessErr.Error()})
					h.redirectLoginError(w, r, "invalid", lookupUsername)
					return
				}
			}

			if accessErr == nil && entry == nil {
				if !isAdmin {
					slog.Info("Acces refuse par le filtre de recherche LDAP",
						"username", lookupUsername,
						"remote", r.RemoteAddr,
					)
					h.logAction("admin.login.failed", lookupUsername, "", fmt.Sprintf("IP: %s, filtre LDAP: acces refuse", r.RemoteAddr))
					logSecurityEvent(h.db, r, "admin_login", "admin.login.failed", "warning", lookupUsername, "", "Acces refuse par le filtre LDAP", nil)
					h.redirectLoginError(w, r, "invalid", lookupUsername)
					return
				}

				slog.Warn("Utilisateur introuvable en LDAP, acces accorde par fallback administrateur Jellyfin",
					"username", lookupUsername,
					"remote", r.RemoteAddr,
				)
			}

			if accessErr == nil {
				isAdmin = isAdmin || ldapIsAdmin
			}
		}
	}

	if !isAdmin {
		slog.Info("Utilisateur standard connecte",
			"username", username,
			"jellyfin_id", authUserID,
		)
	}

	sessionDuration := session.Duration
	if rememberMe {
		sessionDuration = h.rememberSessionDuration()
	}
	now := time.Now()
	sessionExpiresAt := now.Add(sessionDuration)

	sess := session.Payload{
		UserID:   authUserID,
		Username: authUsername,
		IsAdmin:  isAdmin,
		Exp:      sessionExpiresAt.Unix(),
		Iat:      now.Unix(),
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
		HttpOnly: true,
		Secure:   jgmw.RequestIsHTTPS(r, h.cfg.BaseURL),
		SameSite: http.SameSiteLaxMode,
	})

	if preferredLang := h.resolvePreferredLang(authUserID, authUsername); preferredLang != "" {
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

	slog.Info("Connexion admin reussie",
		"username", authUsername,
		"jellyfin_id", authUserID,
		"remote", r.RemoteAddr,
	)
	h.logAction("admin.login.success", authUsername, authUserID, fmt.Sprintf("IP: %s", r.RemoteAddr))
	logSecurityEvent(h.db, r, "admin_login", "admin.login.success", "info", authUsername, authUserID, "Connexion admin reussie", nil)

	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func (h *AuthHandler) resolvePreferredLang(jellyfinID, username string) string {
	if h == nil || h.db == nil {
		return ""
	}
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

// Logout deconnecte l'utilisateur en supprimant le cookie de session.
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

	slog.Info("Deconnexion admin", "remote", r.RemoteAddr)

	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (h *AuthHandler) authenticateWithJellyfin(username, password string) (*jellyfin.User, error) {
	if h == nil || h.jfClient == nil {
		return nil, fmt.Errorf("client Jellyfin indisponible")
	}
	return h.jfClient.AuthenticateByName(username, password)
}
