package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/maelmoreau21/JellyGate/internal/authentik"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/handlers"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/oidc"
	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

// RouterParams regroupe les dépendances nécessaires à l'initialisation du routeur.
type RouterParams struct {
	Cfg                  *config.Config
	DB                   *database.DB
	RenderEngine         *render.Engine
	JFClient             *jellyfin.Client
	OIDCClient           oidc.Client
	AuthentikClient      authentik.Client
	AuthHandler          *handlers.AuthHandler
	InviteHandler        *handlers.InvitationHandler
	AdminHandler         *handlers.AdminHandler
	SettingsHandler      *handlers.SettingsHandler
	BackupHandler        *handlers.BackupHandler
	AutomationHandler    *handlers.AutomationHandler
	AuthSessionValidator jgmw.SessionValidator
}

// setupRouter initialise le routeur Chi avec ses middlewares globaux et toutes les routes de l'application.
func setupRouter(p RouterParams) *chi.Mux {
	cfg := p.Cfg
	r := chi.NewRouter()

	// Middlewares globaux
	r.Use(jgmw.SecurityHeaders(cfg.BaseURL))       // Headers de sécurité HTTP
	r.Use(jgmw.LimitRequestBody(10 * 1024 * 1024)) // Protection DoS: limite le corps des requêtes à 10 Mo
	r.Use(chimw.RequestID)                         // ID unique par requête
	if cfg.TrustProxyHeaders {
		for _, cidr := range cfg.TrustedProxies {
			trimmed := strings.TrimSpace(cidr)
			if trimmed == "0.0.0.0/0" || trimmed == "::/0" || trimmed == "0.0.0.0" {
				slog.Warn("[SEC] TRUSTED_PROXIES contient un CIDR trop large (" + trimmed + "). Tout attaquant peut usurper son IP via X-Forwarded-For. Restreignez la liste aux seuls IPs de vos reverse proxies.")
			}
		}
		if len(cfg.TrustedProxies) == 0 {
			slog.Warn("[SEC] TRUST_PROXY_HEADERS=true mais TRUSTED_PROXIES est vide. Aucun en-tête proxy ne sera lu. Spécifiez les IPs/CIDRs de vos proxies de confiance.")
		}
		r.Use(jgmw.TrustedProxyRealIP(cfg.TrustedProxies))
	}
	r.Use(chimw.Logger)                    // Log de chaque requête
	r.Use(jgmw.LogPanics())                // Dev: log panics with stack trace
	r.Use(chimw.Recoverer)                 // Récupération des panics
	r.Use(chimw.Timeout(30 * time.Second)) // Timeout global 30s
	r.Use(chimw.Compress(5))               // Compression gzip
	r.Use(jgmw.DetectLanguage(p.DB))       // Détection de langue (cookie → Accept-Language → DB default_lang)
	r.NotFound(handleNotFound(p.RenderEngine))
	r.MethodNotAllowed(handleMethodNotAllowed(p.RenderEngine))

	// ── Routes publiques ────────────────────────────────────────────────────
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, adminLandingPath(r, cfg.SecretKey, p.AuthSessionValidator), http.StatusFound)
	})
	// Répondre aux requêtes HEAD sur la racine pour satisfaire les healthchecks
	r.Head("/", handleHealthCheck)

	// Endpoints de santé
	r.Get("/health", handleHealthCheck)
	r.Head("/health", handleHealthCheck)
	r.Get("/health/jellyfin", handleJellyfinHealthCheck(p.JFClient))

	r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "web/static/favicon.svg")
	})

	r.Get("/manifest.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
		http.ServeFile(w, r, "web/static/manifest.webmanifest")
	})

	r.Get("/service-worker.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Service-Worker-Allowed", "/")
		http.ServeFile(w, r, "web/static/service-worker.js")
	})

	// Fichiers statiques protégés contre toute fuite d'informations sensibles
	r.With(jgmw.StaticFileFilter()).Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	// Routes d'invitation (publiques)
	r.Route("/invite", func(r chi.Router) {
		r.Get("/{code}", p.InviteHandler.InvitePage)
		r.With(jgmw.RateLimitByIP(15, 5*time.Minute)).Post("/{code}", p.InviteHandler.InviteSubmit)
	})

	// Redirection de la réinitialisation de mot de passe vers Authentik
	r.Get("/reset/*", func(w http.ResponseWriter, r *http.Request) {
		if cfg.Authentik.URL != "" {
			http.Redirect(w, r, strings.TrimRight(cfg.Authentik.URL, "/")+"/flow/initial-setup/", http.StatusFound)
		} else {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
		}
	})

	// Routes d'authentification OIDC & locale de secours (publiques)
	r.Route("/auth", func(r chi.Router) {
		r.Use(jgmw.EnsureCSRFCookie(cfg.SecretKey, cfg.BaseURL))
		r.Get("/login", p.AuthHandler.LoginRedirect)
		r.Get("/callback", p.AuthHandler.Callback)
		r.Get("/local", p.AuthHandler.LocalLoginPage)
		r.With(jgmw.RateLimitByIP(6, 5*time.Minute), jgmw.RequireCSRF(cfg.SecretKey)).Post("/local", p.AuthHandler.LocalLoginSubmit)
		r.Get("/logout", p.AuthHandler.Logout)
		r.Post("/logout", p.AuthHandler.Logout)
	})

	// Accès direct /login, /local pour la connexion d'urgence et /logout
	r.With(jgmw.EnsureCSRFCookie(cfg.SecretKey, cfg.BaseURL)).Get("/login", p.AuthHandler.LoginPage)
	r.With(jgmw.EnsureCSRFCookie(cfg.SecretKey, cfg.BaseURL)).Get("/local", p.AuthHandler.LocalLoginPage)
	r.With(jgmw.EnsureCSRFCookie(cfg.SecretKey, cfg.BaseURL), jgmw.RateLimitByIP(6, 5*time.Minute), jgmw.RequireCSRF(cfg.SecretKey)).Post("/local", p.AuthHandler.LocalLoginSubmit)
	r.Get("/logout", p.AuthHandler.Logout)
	r.Post("/logout", p.AuthHandler.Logout)

	// ── Routes admin (authentification requise) ─────────────────────────────
	r.Route("/admin", func(r chi.Router) {
		r.Use(jgmw.EnsureCSRFCookie(cfg.SecretKey, cfg.BaseURL))
		// Routes publiques (login/logout) — pas de middleware auth
		r.Get("/login", p.AuthHandler.LoginPage)
		r.Get("/login/local", p.AuthHandler.LocalLoginPage)
		r.With(jgmw.RateLimitByIP(6, 5*time.Minute), jgmw.RequireCSRF(cfg.SecretKey)).Post("/login/local", p.AuthHandler.LocalLoginSubmit)
		r.With(jgmw.RateLimitByIP(12, 10*time.Minute), jgmw.RequireCSRF(cfg.SecretKey)).Post("/login", p.AuthHandler.LoginSubmit)
		r.Get("/logout", p.AuthHandler.Logout)
		r.Post("/logout", p.AuthHandler.Logout)

		if cfg.EnableDebugRoutes {
			slog.Warn("Routes debug admin activées: à ne jamais utiliser en production")

			r.Group(func(r chi.Router) {
				r.Use(jgmw.RequireAuth(cfg.SecretKey, cfg.BaseURL, p.AuthSessionValidator))
				r.Use(jgmw.RequireAdminAuth())

				// DEBUG route: verify jellygate_session cookie using server secret.
				r.Get("/debug/verify-session", func(w http.ResponseWriter, r *http.Request) {
					cookie, err := r.Cookie(session.CookieName)
					w.Header().Set("Content-Type", "application/json")
					if err != nil {
						w.WriteHeader(http.StatusUnauthorized)
						_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "cookie missing"})
						return
					}
					payload, err := session.Verify(cookie.Value, cfg.SecretKey)
					if err != nil {
						w.WriteHeader(http.StatusUnauthorized)
						_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "user": payload.Username, "is_admin": payload.IsAdmin})
				})

				// DEBUG route: inspect Jellyfin auth/config without exposing secrets.
				r.Get("/debug/jellyfin-auth-config", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					var diag interface{}
					if p.JFClient != nil {
						diag = p.JFClient.Diagnostics()
					}
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"success": true,
						"data":    diag,
					})
				})
			})
		}

		// Routes protégées par le middleware d'authentification global (standard + admin)
		r.Group(func(r chi.Router) {
			r.Use(jgmw.RequireAuth(cfg.SecretKey, cfg.BaseURL, p.AuthSessionValidator))

			// Le tableau de bord est commun
			r.Get("/", p.AdminHandler.DashboardPage)
			r.Get("/my-account", p.AdminHandler.MyAccountPage)

			// ── User Self-Service API ──────────────────────────────────────
			r.Route("/api/users/me", func(r chi.Router) {
				r.Use(jgmw.RequireCSRF(cfg.SecretKey))
				r.Get("/", p.AdminHandler.GetMyAccount)
				r.Patch("/", p.AdminHandler.UpdateMyAccount)
				r.Post("/password", p.AdminHandler.UpdateMyPassword)
				r.Post("/avatar", p.AdminHandler.UpdateMyAccountAvatar)
				r.Get("/invitations", p.AdminHandler.GetMyInvitations)
				r.Post("/invitations", p.AdminHandler.CreateMyInvitation)
			})

			// ── Routes limitées aux administrateurs purs ────────────────────
			r.Group(func(r chi.Router) {
				r.Use(jgmw.RequireAdminAuth())

				r.Get("/users", p.AdminHandler.UsersPage)
				r.Get("/profiles", p.AdminHandler.ProfilesPage)
				r.Get("/authentik", p.AdminHandler.AuthentikPage)
				r.Get("/sso", p.AdminHandler.AuthentikPage)
				r.Get("/security", p.AdminHandler.SecurityPage)
				r.Get("/pending-actions", p.AdminHandler.PendingActionsPage)
				r.Get("/automation", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/admin/settings#scheduler", http.StatusSeeOther)
				})
				r.Route("/api/users", func(r chi.Router) {
					r.Use(jgmw.RequireCSRF(cfg.SecretKey))
					r.Get("/", p.AdminHandler.ListUsers)
					r.Post("/", p.AdminHandler.CreateUser)
					r.Post("/sync", p.AdminHandler.SyncJellyfinUsers)
					r.Get("/dashboard/stats", p.AdminHandler.DashboardStats)
					r.Get("/invitations", p.AdminHandler.ListInvitations)
					r.Get("/{id}/avatar", p.AdminHandler.UserAvatar)
					r.Get("/{id}/timeline", p.AdminHandler.UserTimeline)
					r.Post("/bulk", p.AdminHandler.BulkUsersAction)
					r.Patch("/{id}", p.AdminHandler.UpdateUser)
					r.Post("/{id}/toggle", p.AdminHandler.ToggleUser)
					r.Post("/{id}/invite-toggle", p.AdminHandler.ToggleUserInvite)
					r.Post("/{id}/ban", p.AdminHandler.BanUser)
					r.Delete("/{id}", p.AdminHandler.DeleteUser)
					r.Post("/{id}/quota", p.AdminHandler.SetUserQuota)
					r.Get("/referrals", p.AdminHandler.GetReferrals)
					r.Post("/{id}/extend", p.AdminHandler.ExtendAccess)
				})

				r.Route("/api/settings", func(r chi.Router) {
					r.Use(jgmw.RequireCSRF(cfg.SecretKey))
					r.Get("/", p.SettingsHandler.GetAll)
					r.Post("/general", p.SettingsHandler.SaveGeneral)
					r.Post("/general/fetch-server-name", p.SettingsHandler.FetchJellyfinServerName)
					r.Post("/auth-session", p.SettingsHandler.SaveAuthSession)
					r.Post("/auth-session/revoke", p.SettingsHandler.RevokeAuthSessions)
					r.Post("/authentik", p.SettingsHandler.SaveAuthentik)
					r.Get("/authentik/health", p.SettingsHandler.GetAuthentikHealth)
					r.Post("/authentik/test", p.SettingsHandler.GetAuthentikHealth)
					r.Post("/authentik/reload-env", p.SettingsHandler.ReloadAuthentikFromEnv)
					r.Post("/authentik/test-user", p.SettingsHandler.TestAuthentikUser)
					r.Post("/jellyfin", p.SettingsHandler.SaveJellyfin)
					r.Get("/jellyfin/health", p.SettingsHandler.TestJellyfin)
					r.Post("/jellyfin/test", p.SettingsHandler.TestJellyfin)
					r.Post("/smtp", p.SettingsHandler.SaveSMTP)
					r.Post("/smtp/test", p.SettingsHandler.TestSMTP)
					r.Post("/webhooks", p.SettingsHandler.SaveWebhooks)
					r.Post("/backup", p.SettingsHandler.SaveBackup)
					r.Get("/email-templates/export", p.SettingsHandler.ExportEmailTemplates)
					r.Post("/email-templates", p.SettingsHandler.SaveEmailTemplates)
					r.Post("/email-templates/import", p.SettingsHandler.ImportEmailTemplates)
					r.Post("/email-templates/preview", p.SettingsHandler.PreviewEmailTemplate)
					r.Post("/invitation-profile", p.SettingsHandler.SaveInvitationProfile)
				})

				r.Route("/api/backups", func(r chi.Router) {
					r.Use(jgmw.RequireCSRF(cfg.SecretKey))
					r.Get("/", p.BackupHandler.ListBackups)
					r.Post("/create", p.BackupHandler.CreateBackup)
					r.Post("/import", p.BackupHandler.ImportBackup)
					r.Get("/{name}/download", p.BackupHandler.DownloadBackup)
					r.Post("/{name}/restore", p.BackupHandler.RestoreBackup)
					r.Delete("/{name}", p.BackupHandler.DeleteBackup)
				})

				r.Route("/api/logs", func(r chi.Router) {
					r.Use(jgmw.RequireCSRF(cfg.SecretKey))
					r.Get("/", p.AdminHandler.LogsAPI)
					r.Get("/system", p.AdminHandler.SystemLogsAPI)
					r.Get("/system/download", p.AdminHandler.DownloadSystemLog)
					r.Get("/system/download-all", p.AdminHandler.DownloadAllSystemLogs)
				})

				r.Route("/api/security", func(r chi.Router) {
					r.Use(jgmw.RequireCSRF(cfg.SecretKey))
					r.Get("/overview", p.AdminHandler.SecurityOverview)
					r.Get("/events", p.AdminHandler.SecurityEvents)
				})

				r.Route("/api/pending-actions", func(r chi.Router) {
					r.Use(jgmw.RequireCSRF(cfg.SecretKey))
					r.Get("/", p.AdminHandler.PendingActions)
				})

				r.Route("/api/automation", func(r chi.Router) {
					r.Use(jgmw.RequireCSRF(cfg.SecretKey))
					r.Get("/libraries", p.AutomationHandler.ListLibraries)
					r.Route("/presets", func(r chi.Router) {
						r.Get("/", p.AutomationHandler.ListPresets)
						r.Post("/", p.AutomationHandler.SavePresets)
					})
					r.Route("/group-mappings", func(r chi.Router) {
						r.Get("/", p.AutomationHandler.ListGroupMappings)
						r.Post("/", p.AutomationHandler.SaveGroupMappings)
					})
					r.Route("/tasks", func(r chi.Router) {
						r.Get("/", p.AutomationHandler.ListTasks)
						r.Post("/", p.AutomationHandler.CreateTask)
						r.Patch("/{id}", p.AutomationHandler.UpdateTask)
						r.Delete("/{id}", p.AutomationHandler.DeleteTask)
						r.Post("/{id}/run", p.AutomationHandler.RunTaskNow)
					})
				})

				r.Get("/settings", p.AdminHandler.SettingsPage)
				r.Get("/email-templates", p.AdminHandler.EmailTemplatesPage)

				r.Get("/logs", p.AdminHandler.LogsPage)
			})

			// ── Routes d'invitations (Filtrées en interne selon IsAdmin) ────
			r.Route("/invitations", func(r chi.Router) {
				r.Get("/", p.AdminHandler.InvitationsPage)
			})
			r.Route("/api/invitations", func(r chi.Router) {
				r.Use(jgmw.RequireCSRF(cfg.SecretKey))
				r.Get("/", p.AdminHandler.ListInvitations)
				r.Get("/stats", p.AdminHandler.InvitationStats)
				r.Post("/sync-authentik", p.AdminHandler.SyncAuthentikInvitations)
				r.Get("/security", p.AdminHandler.InvitationSecurityConfig)
				r.Post("/security", p.AdminHandler.SaveInvitationSecurityConfig)
				r.Post("/preview", p.AdminHandler.PreviewInvitation)
				r.Post("/", p.AdminHandler.CreateInvitation)
				r.Delete("/{id}", p.AdminHandler.DeleteInvitation)
			})
		}) // fin Group RequireAuth
	})

	return r
}

// handleHealthCheck renvoie un statut 200 pour les healthchecks Docker.
func handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok","app":"JellyGate","version":"%s"}`,
		config.AppVersion)
}

func handleJellyfinHealthCheck(jfClient *jellyfin.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		status := "disabled"
		if jfClient != nil && jfClient.IsConfigured() {
			status = string(jfClient.Status())
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": status,
			"app":    "JellyGate",
		})
	}
}

// authSessionAllowed applique la politique de révocation globale stockée en base
// ainsi que l'invalidation immédiate des sessions d'utilisateurs désactivés ou bannis.
func authSessionAllowed(db *database.DB, sess *session.Payload) bool {
	if sess == nil {
		return false
	}
	if db == nil {
		return true
	}
	cfg, err := db.GetAuthSessionConfig()
	if err != nil {
		slog.Warn("Impossible de lire la politique de session", "error", err)
		return true
	}
	if !cfg.AcceptsIssuedAt(sess.Iat) {
		return false
	}

	if sess.AuthentikID == "local_admin" {
		return true
	}

	var isActive, isBanned bool
	err = db.QueryRow(
		`SELECT is_active, is_banned FROM users WHERE authentik_id = ? OR username = ? LIMIT 1`,
		sess.AuthentikID, sess.Username,
	).Scan(&isActive, &isBanned)
	if err == nil {
		if isBanned || !isActive {
			return false
		}
	} else if errors.Is(err, sql.ErrNoRows) && !sess.IsAdmin {
		return false
	}

	return true
}

// adminLandingPath conserve l'ouverture de l'app sur le dashboard quand une
// session persistante est encore valide.
func adminLandingPath(r *http.Request, secretKey string, validators ...jgmw.SessionValidator) string {
	if r == nil {
		return "/admin/login"
	}
	cookie, err := r.Cookie(session.CookieName)
	if err != nil {
		return "/admin/login"
	}
	sess, err := session.Verify(cookie.Value, secretKey)
	if err != nil || !jgmw.SessionAllowed(sess, validators...) {
		return "/admin/login"
	}
	return "/admin/"
}

func handleNotFound(renderEngine *render.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.Contains(r.URL.Path, "/api") || strings.Contains(r.Header.Get("Accept"), "application/json") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Resource not found",
				"code":    404,
			})
			return
		}

		lang := jgmw.LangFromContext(r.Context())
		td := renderEngine.NewTemplateData(lang)
		td.ScriptNonce = jgmw.ScriptNonceFromContext(r.Context())
		td.Data["StatusCode"] = 404
		td.Data["Title"] = "Page introuvable"
		td.Data["Message"] = "La page demandée n'existe pas, a été déplacée ou est temporairement inaccessible."

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		if err := renderEngine.Render(w, "error.html", td); err != nil {
			http.Error(w, "404 Not Found", http.StatusNotFound)
		}
	}
}

func handleMethodNotAllowed(renderEngine *render.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.Contains(r.URL.Path, "/api") || strings.Contains(r.Header.Get("Accept"), "application/json") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Method not allowed",
				"code":    405,
			})
			return
		}

		lang := jgmw.LangFromContext(r.Context())
		td := renderEngine.NewTemplateData(lang)
		td.ScriptNonce = jgmw.ScriptNonceFromContext(r.Context())
		td.Data["StatusCode"] = 405
		td.Data["Title"] = "Méthode non autorisée"
		td.Data["Message"] = "La méthode HTTP demandée n'est pas prise en charge pour cette page."

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		if err := renderEngine.Render(w, "error.html", td); err != nil {
			http.Error(w, "405 Method Not Allowed", http.StatusMethodNotAllowed)
		}
	}
}
