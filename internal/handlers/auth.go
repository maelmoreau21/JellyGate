// Package handlers contains JellyGate HTTP handlers.
package handlers

import (
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/authentik"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/oidc"
	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

// AuthHandler gere les routes d'authentification admin et OIDC.
type AuthHandler struct {
	cfg        *config.Config
	db         *database.DB
	oidcClient oidc.Client
	authClient authentik.Client
	renderer   *render.Engine
}

// NewAuthHandler cree un nouveau AuthHandler.
func NewAuthHandler(cfg *config.Config, db *database.DB, oidcClient oidc.Client, authClient authentik.Client, renderer *render.Engine) *AuthHandler {
	return &AuthHandler{
		cfg:        cfg,
		db:         db,
		oidcClient: oidcClient,
		authClient: authClient,
		renderer:   renderer,
	}
}

// SetOIDCClient met à jour le client OIDC.
func (h *AuthHandler) SetOIDCClient(oidcClient oidc.Client) {
	h.oidcClient = oidcClient
}

// SetAuthentikClient met à jour le client Authentik.
func (h *AuthHandler) SetAuthentikClient(authClient authentik.Client) {
	h.authClient = authClient
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

func (h *AuthHandler) resolveEffectiveAuthentikConfig() config.AuthentikConfig {
	cfg := config.AuthentikConfig{
		Enabled:            false,
		UserGroup:          "jellygate-users",
		AdminGroup:         "jellygate-admins",
		JellyfinUserGroup:  "jellyfin-users",
		EnrollmentFlowSlug: "default-enrollment-flow",
	}

	if h.db != nil {
		if dbCfg, err := h.db.GetAuthentikConfig(); err == nil {
			if dbCfg.URL != "" || dbCfg.IssuerURL != "" || dbCfg.ClientID != "" || dbCfg.APIToken != "" || dbCfg.Enabled {
				cfg = dbCfg
			}
		}
	}

	if h.cfg != nil {
		env := h.cfg.Authentik

		if strings.TrimSpace(env.URL) != "" {
			cfg.URL = strings.TrimSpace(env.URL)
		}
		if strings.TrimSpace(env.IssuerURL) != "" {
			cfg.IssuerURL = strings.TrimSpace(env.IssuerURL)
		}
		if strings.TrimSpace(env.ClientID) != "" {
			cfg.ClientID = strings.TrimSpace(env.ClientID)
		}
		if strings.TrimSpace(env.ClientSecret) != "" {
			cfg.ClientSecret = strings.TrimSpace(env.ClientSecret)
		}
		if strings.TrimSpace(env.RedirectURL) != "" {
			cfg.RedirectURL = strings.TrimSpace(env.RedirectURL)
		}
		if strings.TrimSpace(env.APIToken) != "" {
			cfg.APIToken = strings.TrimSpace(env.APIToken)
		}
		if strings.TrimSpace(env.UserGroup) != "" {
			cfg.UserGroup = strings.TrimSpace(env.UserGroup)
		}
		if strings.TrimSpace(env.AdminGroup) != "" {
			cfg.AdminGroup = strings.TrimSpace(env.AdminGroup)
		}
		if strings.TrimSpace(env.JellyfinUserGroup) != "" {
			cfg.JellyfinUserGroup = strings.TrimSpace(env.JellyfinUserGroup)
		}
		if strings.TrimSpace(env.InvitersGroup) != "" {
			cfg.InvitersGroup = strings.TrimSpace(env.InvitersGroup)
		}
		if strings.TrimSpace(env.InvitersRecursiveGroup) != "" {
			cfg.InvitersRecursiveGroup = strings.TrimSpace(env.InvitersRecursiveGroup)
		}
		if strings.TrimSpace(env.EnrollmentFlowSlug) != "" {
			cfg.EnrollmentFlowSlug = strings.TrimSpace(env.EnrollmentFlowSlug)
		}
		if env.Enabled {
			cfg.Enabled = true
		}
	}

	if cfg.RedirectURL == "" && h.cfg != nil && h.cfg.BaseURL != "" {
		cfg.RedirectURL = strings.TrimRight(h.cfg.BaseURL, "/") + "/auth/callback"
	}

	return cfg
}

func (h *AuthHandler) getEffectiveAuthentikClient() authentik.Client {
	if h.authClient != nil {
		return h.authClient
	}
	cfg := h.resolveEffectiveAuthentikConfig()
	if cfg.URL != "" || cfg.IssuerURL != "" {
		return authentik.NewClient(cfg)
	}
	return nil
}

func (h *AuthHandler) getEffectiveOIDCClient() oidc.Client {
	if h.oidcClient != nil {
		return h.oidcClient
	}
	cfg := h.resolveEffectiveAuthentikConfig()
	if cfg.Enabled || cfg.URL != "" || cfg.IssuerURL != "" {
		return oidc.NewClient(cfg)
	}
	return nil
}

// LoginPage affiche la page de connexion JellyGate avec le bouton de connexion SSO et l'accès de secours (GET /admin/login ou GET /login).
func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if h.hasValidSession(r) {
		http.Redirect(w, r, "/admin/", http.StatusSeeOther)
		return
	}

	if h.renderer != nil {
		td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
		td.Error = r.URL.Query().Get("error")
		td.Data["SubmittedUsername"] = strings.TrimSpace(r.URL.Query().Get("username"))
		links := resolvePortalLinks(h.cfg, h.db)
		td.Data["JellyfinURL"] = links.JellyfinURL
		td.Data["JellyseerrURL"] = links.JellyseerrURL
		td.Data["JellyTrackURL"] = links.JellyTrackURL
		td.Data["OIDCEnabled"] = true
		td.Data["OIDCLoginURL"] = "/auth/login"
		td.Data["LocalAdminEnabled"] = h.cfg != nil && h.cfg.LocalAdmin.Enabled
		td.Section = "login"
		if err := h.renderer.Render(w, "admin/login.html", td); err == nil {
			return
		}
	}

	http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
}

// LoginRedirect redirige l'utilisateur vers Authentik pour le SSO OIDC (GET /auth/login).
func (h *AuthHandler) LoginRedirect(w http.ResponseWriter, r *http.Request) {
	if h.hasValidSession(r) {
		http.Redirect(w, r, "/admin/", http.StatusSeeOther)
		return
	}

	oidcCli := h.getEffectiveOIDCClient()
	if oidcCli == nil {
		slog.Error("Client OIDC non configuré")
		http.Error(w, h.tr(r, "auth_oidc_disabled", "Authentification OIDC non configurée"), http.StatusServiceUnavailable)
		return
	}

	authURL, err := oidcCli.GenerateAuthURL(w, r)
	if err != nil {
		slog.Error("Erreur lors de la génération de l'URL OIDC", "error", err)
		http.Error(w, h.tr(r, "auth_oidc_failed", "Impossible d'initier la connexion OIDC"), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

// Callback traite le retour OIDC depuis Authentik (GET /auth/callback).
func (h *AuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	oidcCli := h.getEffectiveOIDCClient()
	if oidcCli == nil {
		slog.Error("Client OIDC indisponible pendant le callback")
		h.redirectLoginError(w, r, "oidc_unavailable", "")
		return
	}

	claims, err := oidcCli.HandleCallback(r)
	if err != nil {
		slog.Warn("Échec du callback OIDC Authentik", "error", err, "remote", r.RemoteAddr)
		errStr := err.Error()
		code := "oidc_failed"
		if strings.Contains(errStr, "state") {
			code = "bad_state"
		} else if strings.Contains(errStr, "nonce") {
			code = "bad_nonce"
		} else if strings.Contains(errStr, "expired") {
			code = "token_expired"
		}
		h.logAction("auth.oidc.failed", "", "", fmt.Sprintf("IP: %s, erreur: %s", r.RemoteAddr, errStr))
		logSecurityEvent(h.db, r, "oidc_login", "auth.oidc.failed", "warning", "", "", "Échec callback OIDC", map[string]string{"error": errStr})
		h.redirectLoginError(w, r, code, "")
		return
	}

	isAdmin, hasAccess := oidcCli.DetermineUserRole(claims.Groups)
	canInvite, canInviteRecursive := oidcCli.DetermineInviterRole(claims.Groups)

	// Fallback Authentik API si les groupes ne sont pas présents dans le jeton ID OIDC
	if !hasAccess || !isAdmin {
		if authCli := h.getEffectiveAuthentikClient(); authCli != nil {
			if authUser, err := authCli.GetUserByUsername(r.Context(), claims.PreferredUsername); err == nil && authUser != nil {
				if len(authUser.Groups) > 0 {
					claims.Groups = append(claims.Groups, authUser.Groups...)
					adm, acc := oidcCli.DetermineUserRole(claims.Groups)
					if adm {
						isAdmin = true
					}
					if acc {
						hasAccess = true
					}
					inv, invRec := oidcCli.DetermineInviterRole(claims.Groups)
					if inv {
						canInvite = true
					}
					if invRec {
						canInviteRecursive = true
					}
				}
				if authUser.PK == 1 || strings.EqualFold(claims.PreferredUsername, "akadmin") {
					isAdmin = true
					hasAccess = true
				}
			}
		}
	}

	if isAdmin {
		canInvite = true
		canInviteRecursive = true
		hasAccess = true
	}

	if !hasAccess {
		slog.Warn("Accès OIDC refusé : aucun groupe autorisé", "username", claims.PreferredUsername, "sub", claims.Sub, "groups", claims.Groups)
		h.logAction("auth.oidc.unauthorized", claims.PreferredUsername, claims.Sub, fmt.Sprintf("IP: %s, groupes non autorisés: %v", r.RemoteAddr, claims.Groups))
		logSecurityEvent(h.db, r, "oidc_login", "auth.oidc.unauthorized", "warning", claims.PreferredUsername, claims.Sub, "Groupes OIDC insuffisants", map[string]string{"groups": strings.Join(claims.Groups, ",")})

		w.WriteHeader(http.StatusForbidden)
		if h.renderer != nil {
			td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
			td.Error = h.tr(r, "auth_oidc_forbidden", "Accès non autorisé au portail JellyGate (groupe OIDC manquant)")
			td.Section = "login"
			_ = h.renderer.Render(w, "admin/login.html", td)
		} else {
			_, _ = w.Write([]byte(h.tr(r, "auth_oidc_forbidden", "Accès non autorisé au portail JellyGate (groupe OIDC manquant)")))
		}
		return
	}

	// Synchronisation JIT ou rapprochement de compte existant
	var user *database.User
	if h.db != nil {
		user, err = h.db.SyncOIDCUser(r.Context(), claims.Sub, claims.PreferredUsername, claims.Email)
		if err != nil {
			slog.Error("Erreur lors de la synchronisation JIT OIDC", "error", err, "sub", claims.Sub)
			h.redirectLoginError(w, r, "sync_failed", claims.PreferredUsername)
			return
		}
		if user != nil && canInvite {
			_, _ = h.db.Exec(`UPDATE users SET can_invite = TRUE, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, user.ID)
		}
	}

	userIDStr := claims.Sub
	if user != nil {
		userIDStr = fmt.Sprintf("%d", user.ID)
	}

	sessionDuration := h.rememberSessionDuration()
	now := time.Now()
	sessionExpiresAt := now.Add(sessionDuration)

	sess := session.Payload{
		UserID:             userIDStr,
		AuthentikID:        claims.Sub,
		Username:           claims.PreferredUsername,
		Email:              claims.Email,
		IsAdmin:            isAdmin,
		CanInvite:          canInvite,
		CanInviteRecursive: canInviteRecursive,
		Groups:             claims.Groups,
		Exp:                sessionExpiresAt.Unix(),
		Iat:                now.Unix(),
	}

	cookieValue, err := session.Sign(sess, h.cfg.SecretKey)
	if err != nil {
		slog.Error("Erreur signature session OIDC", "error", err)
		http.Error(w, h.tr(r, "common_server_error", "Server error"), http.StatusInternalServerError)
		return
	}

	clearTempCookie(w, oidc.CookieState)
	clearTempCookie(w, oidc.CookieNonce)
	clearTempCookie(w, oidc.CookieVerifier)
	clearTempCookie(w, oidc.CookieRedirectURI)

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

	if preferredLang := h.resolvePreferredLang(claims.Sub, claims.PreferredUsername); preferredLang != "" {
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

	slog.Info("Connexion OIDC réussie",
		"username", claims.PreferredUsername,
		"sub", claims.Sub,
		"is_admin", isAdmin,
		"remote", r.RemoteAddr,
	)
	h.logAction("admin.login.oidc_success", claims.PreferredUsername, claims.Sub, fmt.Sprintf("IP: %s, isAdmin: %v", r.RemoteAddr, isAdmin))
	logSecurityEvent(h.db, r, "admin_login", "admin.login.oidc_success", "info", claims.PreferredUsername, claims.Sub, "Connexion OIDC réussie", nil)

	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func clearTempCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     oidc.CookiePath,
		MaxAge:   -1,
		HttpOnly: true,
	})
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

// LocalLoginPage affiche le formulaire de connexion locale de secours (GET /local ou GET /auth/local).
func (h *AuthHandler) LocalLoginPage(w http.ResponseWriter, r *http.Request) {
	if h.hasValidSession(r) {
		http.Redirect(w, r, "/admin/", http.StatusSeeOther)
		return
	}

	if h.renderer == nil {
		http.Error(w, "Moteur de template indisponible", http.StatusInternalServerError)
		return
	}

	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.Data["LocalAdminEnabled"] = h.cfg != nil && h.cfg.LocalAdmin.Enabled
	if h.cfg != nil && h.cfg.LocalAdmin.Enabled && h.cfg.LocalAdmin.Username != "" {
		td.Data["DefaultUsername"] = h.cfg.LocalAdmin.Username
	}
	td.Section = "login"
	_ = h.renderer.Render(w, "admin/login_local.html", td)
}

// LocalLoginSubmit traite l'authentification de secours avec le mot de passe local (POST /local ou POST /auth/local).
func (h *AuthHandler) LocalLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Requête invalide", http.StatusBadRequest)
		return
	}

	if h.cfg == nil || !h.cfg.LocalAdmin.Enabled || h.cfg.LocalAdmin.Password == "" {
		slog.Warn("Tentative de connexion locale alors que le compte local est désactivé", "ip", r.RemoteAddr)
		w.WriteHeader(http.StatusForbidden)
		if h.renderer != nil {
			td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
			td.Error = "Le compte administrateur local est désactivé sur cette instance."
			td.Data["LocalAdminEnabled"] = false
			td.Section = "login"
			_ = h.renderer.Render(w, "admin/login_local.html", td)
			return
		}
		http.Error(w, "Compte local désactivé", http.StatusForbidden)
		return
	}

	submittedUser := strings.TrimSpace(r.FormValue("username"))
	submittedPass := strings.TrimSpace(r.FormValue("password"))
	if submittedPass == "" {
		submittedPass = strings.TrimSpace(r.FormValue("secret"))
	}

	expectedUser := strings.TrimSpace(h.cfg.LocalAdmin.Username)
	if expectedUser == "" {
		expectedUser = "admin"
	}
	expectedPass := strings.TrimSpace(h.cfg.LocalAdmin.Password)

	userValid := submittedUser == "" || subtle.ConstantTimeCompare([]byte(submittedUser), []byte(expectedUser)) == 1
	passValid := subtle.ConstantTimeCompare([]byte(submittedPass), []byte(expectedPass)) == 1

	if !userValid || !passValid {
		slog.Warn("Échec de la connexion locale de secours (identifiants invalides)", "ip", r.RemoteAddr, "user", submittedUser)
		h.logAction("auth.local.failed", submittedUser, "", fmt.Sprintf("IP: %s - Identifiants invalides", r.RemoteAddr))
		logSecurityEvent(h.db, r, "admin_login", "auth.local.failed", "warning", submittedUser, "local_admin", "Échec connexion de secours", map[string]string{"ip": r.RemoteAddr})

		w.WriteHeader(http.StatusUnauthorized)
		if h.renderer != nil {
			td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
			td.Error = "Identifiants administrateur invalides"
			td.Data["LocalAdminEnabled"] = true
			td.Data["SubmittedUsername"] = submittedUser
			td.Section = "login"
			_ = h.renderer.Render(w, "admin/login_local.html", td)
			return
		}
		http.Error(w, "Identifiants invalides", http.StatusUnauthorized)
		return
	}

	// Authentification réussie -> Création de session Administrateur local
	now := time.Now()
	sessionDuration := h.rememberSessionDuration()
	sessionExpiresAt := now.Add(sessionDuration)

	sess := session.Payload{
		UserID:      "1",
		AuthentikID: "local_admin",
		Username:    expectedUser,
		Email:       expectedUser + "@jellygate.local",
		IsAdmin:     true,
		Exp:         sessionExpiresAt.Unix(),
		Iat:         now.Unix(),
	}

	cookieValue, err := session.Sign(sess, h.cfg.SecretKey)
	if err != nil {
		slog.Error("Erreur signature session locale", "error", err)
		http.Error(w, "Erreur serveur", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     session.CookieName,
		Value:    cookieValue,
		Path:     "/",
		Expires:  sessionExpiresAt,
		MaxAge:   int(sessionDuration.Seconds()),
		HttpOnly: true,
		Secure:   jgmw.RequestIsHTTPS(r, h.cfg.BaseURL),
		SameSite: http.SameSiteStrictMode,
	})

	slog.Info("Connexion locale de secours réussie", "ip", r.RemoteAddr, "user", expectedUser)
	h.logAction("auth.local.success", expectedUser, "local_admin", fmt.Sprintf("IP: %s", r.RemoteAddr))
	logSecurityEvent(h.db, r, "admin_login", "auth.local.success", "info", expectedUser, "local_admin", "Connexion de secours réussie", nil)

	// Redirige vers la configuration SSO dans les paramètres
	http.Redirect(w, r, "/admin/settings#authentik", http.StatusSeeOther)
}

// LoginSubmit redirige les requêtes de formulaire obsolètes vers Authentik OIDC (POST /admin/login).
func (h *AuthHandler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
}

func (h *AuthHandler) resolvePreferredLang(authentikID, username string) string {
	if h == nil || h.db == nil {
		return ""
	}
	var preferred string
	err := h.db.QueryRow(
		`SELECT preferred_lang FROM users WHERE authentik_id = ? OR username = ? LIMIT 1`,
		strings.TrimSpace(authentikID),
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

// Logout deconnecte l'utilisateur en supprimant le cookie de session et redirige vers la page de connexion JellyGate.
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

	clearTempCookie(w, oidc.CookieState)
	clearTempCookie(w, oidc.CookieNonce)
	clearTempCookie(w, oidc.CookieVerifier)

	slog.Info("Deconnexion utilisateur", "remote", r.RemoteAddr)
	h.logAction("admin.logout", "", "", fmt.Sprintf("IP: %s", r.RemoteAddr))

	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}
