// Package main est le point d'entrée de JellyGate.
//
// JellyGate est un gestionnaire d'invitations, de parrainage
// et d'utilisateurs pour Jellyfin avec intégration Authentik (OIDC).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/maelmoreau21/JellyGate/internal/authentik"
	"github.com/maelmoreau21/JellyGate/internal/backup"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/handlers"
	"github.com/maelmoreau21/JellyGate/internal/integrations"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	"github.com/maelmoreau21/JellyGate/internal/mail"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/notify"
	"github.com/maelmoreau21/JellyGate/internal/oidc"
	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/scheduler"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

func main() {
	// ── 0. Fuseau horaire global en UTC ─────────────────────────────────────
	time.Local = time.UTC

	// ── 1. Initialiser le logger structuré ──────────────────────────────────
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("🚀 Démarrage de JellyGate (timezone: UTC)...")

	// ── 2. Charger la configuration ─────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Erreur de configuration", "error", err)
		os.Exit(1)
	}
	slog.Info("Configuration chargée",
		"port", cfg.Port,
		"base_url", cfg.BaseURL,
		"jellyfin_url", cfg.Jellyfin.URL,
	)

	if err := backup.ApplyPendingRestore(cfg.DataDir, cfg.Database.Type); err != nil {
		slog.Error("Erreur application restauration en attente", "error", err)
	}

	// ── 3. Initialiser la base de données (SQLite/PostgreSQL) ──────────────
	db, err := database.New(cfg.Database, cfg.DataDir, cfg.SecretKey)
	if err != nil {
		slog.Error("Erreur d'initialisation de la base de données", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if db.IsSQLite() {
		slog.Info("Base de données SQLite initialisée", "path", db.Path())
	} else {
		slog.Info("Base de données PostgreSQL initialisée", "driver", db.Driver())
	}

	// ── 3c. Optionnel : Appliquer la langue par défaut depuis l'environnement ──
	if cfg.DefaultLang != "" {
		if err := db.SetSetting(database.SettingDefaultLang, cfg.DefaultLang); err != nil {
			slog.Warn("⚠️ Impossible d'appliquer JELLYGATE_DEFAULT_LANG", "error", err)
		} else {
			slog.Info("🌐 Langue par défaut forcée via configuration", "lang", cfg.DefaultLang)
		}
	}

	// ── 3b. Initialiser les clients de service à partir des settings DB ──
	jfClient := jellyfin.New(cfg.Jellyfin)
	if jfClient.IsConfigured() {
		slog.Info("Client Jellyfin initialisé", "url", cfg.Jellyfin.URL)
		go jfClient.LogDiagnostics()
	} else {
		slog.Info("Intégration Jellyfin non configurée (démarrage en mode pur Authentik)")
	}

	// SMTP (optionnel — chargé depuis la base)
	smtpCfg, _ := db.GetSMTPConfig()
	var mailer *mail.Mailer
	if smtpCfg.Host != "" {
		mailer, err = mail.New(smtpCfg)
		if err != nil {
			slog.Warn("⚠️ Erreur d'initialisation du mailer", "error", err)
		} else if err := mailer.Ping(); err != nil {
			slog.Warn("⚠️ Serveur SMTP injoignable", "error", err)
		} else {
			slog.Info("✅ Connexion SMTP vérifiée")
		}
	} else {
		slog.Info("SMTP non configuré (emails désactivés)")
	}

	// Webhooks (optionnel — chargé depuis la base)
	webhooksCfg, _ := db.GetWebhooksConfig()
	notifier := notify.New(webhooksCfg)
	provisioner := integrations.New(cfg.ThirdParty)

	// ── 3c. Initialiser le moteur de rendu HTML ────────────────────────────
	renderEngine, err := render.NewEngine("web/templates", "web/i18n")
	if err != nil {
		slog.Error("Erreur d'initialisation du moteur de templates", "error", err)
		os.Exit(1)
	}
	slog.Info("Moteur de rendu HTML initialisé")

	// ── 3d. Initialiser les handlers ───────────────────────────────────────
	authentikCfg := cfg.Authentik
	if db != nil {
		if dbAuthCfg, err := db.GetAuthentikConfig(); err == nil && (dbAuthCfg.URL != "" || dbAuthCfg.IssuerURL != "" || dbAuthCfg.ClientID != "" || dbAuthCfg.APIToken != "" || dbAuthCfg.Enabled) {
			authentikCfg = dbAuthCfg
		} else {
			// Si la base n'a pas encore de configuration enregistrée mais que des variables d'environnement sont présentes dans Docker Compose
			if cfg.Authentik.URL != "" || cfg.Authentik.IssuerURL != "" || cfg.Authentik.ClientID != "" || cfg.Authentik.APIToken != "" {
				if err := db.SaveAuthentikConfig(cfg.Authentik); err != nil {
					slog.Warn("Erreur persistance initiale Authentik config en base", "error", err)
				} else {
					slog.Info("Configuration SSO/Authentik importée de l'environnement Docker vers la base de données")
					authentikCfg = cfg.Authentik
				}
			}
		}
	}
	oidcClient := oidc.NewClient(authentikCfg)
	authentikClient := authentik.NewClient(authentikCfg)

	authHandler := handlers.NewAuthHandler(cfg, db, oidcClient, authentikClient, renderEngine)
	inviteHandler := handlers.NewInvitationHandler(cfg, db, provisioner, mailer, notifier, renderEngine)
	inviteHandler.SetAuthentikClient(authentikClient)
	adminHandler := handlers.NewAdminHandler(cfg, db, jfClient, authentikClient, mailer, renderEngine)
	settingsHandler := handlers.NewSettingsHandler(cfg, db, jfClient, authentikClient, renderEngine)
	backupService := backup.NewService(cfg.DataDir, db)
	backupHandler := handlers.NewBackupHandler(db, backupService, renderEngine)
	schedulerService := scheduler.NewService(db, backupService, mailer, notifier)
	automationHandler := handlers.NewAutomationHandler(db, renderEngine, schedulerService, jfClient)
	authSessionValidator := func(sess *session.Payload) bool {
		return authSessionAllowed(db, sess)
	}
	settingsHandler.OnSMTPReload = func(c config.SMTPConfig) {
		if c.Host != "" {
			newMailer, err := mail.New(c)
			if err != nil {
				slog.Warn("🔄 Erreur rechargement SMTP", "error", err)
				return
			}
			mailer = newMailer
			inviteHandler.SetMailer(mailer)
			adminHandler.SetMailer(mailer)
			schedulerService.SetMailer(mailer)
			slog.Info("🔄 Client SMTP rechargé", "host", c.Host)
		}
	}
	settingsHandler.OnWebhooksReload = func(c config.WebhooksConfig) {
		newNotifier := notify.New(c)
		inviteHandler.SetNotifier(newNotifier)
		slog.Info("🔄 Webhooks rechargés")
	}
	settingsHandler.OnAuthentikReload = func(c config.AuthentikConfig) {
		newOIDC := oidc.NewClient(c)
		newAuthClient := authentik.NewClient(c)
		authHandler.SetOIDCClient(newOIDC)
		authHandler.SetAuthentikClient(newAuthClient)
		inviteHandler.SetAuthentikClient(newAuthClient)
		adminHandler.SetAuthentikClient(newAuthClient)
		settingsHandler.SetAuthentikClient(newAuthClient)
		slog.Info("🔄 Clients OIDC & Authentik rechargés", "enabled", c.Enabled, "url", c.URL)
	}

	// ── 4. Configurer le routeur Chi ────────────────────────────────────────
	r := chi.NewRouter()

	// Middlewares globaux
	r.Use(jgmw.SecurityHeaders(cfg.BaseURL)) // Headers de securite HTTP
	r.Use(chimw.RequestID)                   // ID unique par requête
	if cfg.TrustProxyHeaders {
		r.Use(chimw.RealIP)
	}
	r.Use(chimw.Logger)                    // Log de chaque requête
	r.Use(jgmw.LogPanics())                // Dev: log panics with stack trace
	r.Use(chimw.Recoverer)                 // Récupération des panics
	r.Use(chimw.Timeout(30 * time.Second)) // Timeout global 30s
	r.Use(chimw.Compress(5))               // Compression gzip
	r.Use(jgmw.DetectLanguage(db))         // Détection de langue (cookie → Accept-Language → DB default_lang)

	// ── Routes publiques ────────────────────────────────────────────────────
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, adminLandingPath(r, cfg.SecretKey, authSessionValidator), http.StatusFound)
	})
	// Répondre aux requêtes HEAD sur la racine pour satisfaire les healthchecks
	r.Head("/", handleHealthCheck)

	// Endpoint de santé
	r.Get("/health", handleHealthCheck)
	r.Head("/health", handleHealthCheck)
	r.Get("/health/jellyfin", handleJellyfinHealthCheck(jfClient))

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

	// Fichiers statiques
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	// Routes d'invitation (publiques)
	r.Route("/invite", func(r chi.Router) {
		r.Get("/{code}", inviteHandler.InvitePage)
		r.With(jgmw.RateLimitByIP(15, 5*time.Minute)).Post("/{code}", inviteHandler.InviteSubmit)
	})

	// Redirection de la réinitialisation de mot de passe vers Authentik
	r.Get("/reset/*", func(w http.ResponseWriter, r *http.Request) {
		if authentikCfg.URL != "" {
			http.Redirect(w, r, strings.TrimRight(authentikCfg.URL, "/")+"/flow/initial-setup/", http.StatusFound)
		} else {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
		}
	})

	r.Route("/verify-email", func(r chi.Router) {
		r.Get("/{code}", inviteHandler.VerifyEmailPage)
		r.With(jgmw.RateLimitByIP(12, 10*time.Minute)).Post("/{code}", inviteHandler.VerifyEmailSubmit)
	})

	// Routes d'authentification OIDC & locale de secours (publiques)
	r.Route("/auth", func(r chi.Router) {
		r.Get("/login", authHandler.LoginRedirect)
		r.Get("/callback", authHandler.Callback)
		r.Get("/local", authHandler.LocalLoginPage)
		r.With(jgmw.RateLimitByIP(6, 5*time.Minute)).Post("/local", authHandler.LocalLoginSubmit)
		r.Get("/logout", authHandler.Logout)
		r.Post("/logout", authHandler.Logout)
	})

	// Accès direct /login, /local pour la connexion d'urgence et /logout
	r.Get("/login", authHandler.LoginPage)
	r.Get("/local", authHandler.LocalLoginPage)
	r.With(jgmw.RateLimitByIP(6, 5*time.Minute)).Post("/local", authHandler.LocalLoginSubmit)
	r.Get("/logout", authHandler.Logout)
	r.Post("/logout", authHandler.Logout)

	// ── Routes admin (authentification requise) ─────────────────────────────
	r.Route("/admin", func(r chi.Router) {
		r.Use(jgmw.EnsureCSRFCookie(cfg.BaseURL))
		// Routes publiques (login/logout) — pas de middleware auth
		r.Get("/login", authHandler.LoginPage)
		r.Get("/login/local", authHandler.LocalLoginPage)
		r.With(jgmw.RateLimitByIP(6, 5*time.Minute)).Post("/login/local", authHandler.LocalLoginSubmit)
		r.With(jgmw.RateLimitByIP(12, 10*time.Minute), jgmw.RequireCSRF()).Post("/login", authHandler.LoginSubmit)
		r.Get("/logout", authHandler.Logout)
		r.Post("/logout", authHandler.Logout)

		if cfg.EnableDebugRoutes {
			slog.Warn("Routes debug admin activées: à ne jamais utiliser en production")

			r.Group(func(r chi.Router) {
				r.Use(jgmw.RequireAuth(cfg.SecretKey, cfg.BaseURL, authSessionValidator))
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
					p, err := session.Verify(cookie.Value, cfg.SecretKey)
					if err != nil {
						w.WriteHeader(http.StatusUnauthorized)
						_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "user": p.Username, "is_admin": p.IsAdmin})
				})

				// DEBUG route: inspect Jellyfin auth/config without exposing secrets.
				r.Get("/debug/jellyfin-auth-config", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"success": true,
						"data":    jfClient.Diagnostics(),
					})
				})
			})
		}

		// Routes protégées par le middleware d'authentification global (standard + admin)
		r.Group(func(r chi.Router) {
			r.Use(jgmw.RequireAuth(cfg.SecretKey, cfg.BaseURL, authSessionValidator))

			// Le tableau de bord est commun
			r.Get("/", adminHandler.DashboardPage)
			r.Get("/my-account", adminHandler.MyAccountPage)

			// ── User Self-Service API ──────────────────────────────────────
			r.Route("/api/users/me", func(r chi.Router) {
				r.Use(jgmw.RequireCSRF())
				r.Get("/", adminHandler.GetMyAccount)
				r.Patch("/", adminHandler.UpdateMyAccount)
				r.Post("/password", adminHandler.UpdateMyPassword)
				r.Post("/avatar", adminHandler.UpdateMyAccountAvatar)
				r.Get("/invitations", adminHandler.GetMyInvitations)
				r.Post("/invitations", adminHandler.CreateMyInvitation)
				r.Post("/email-verification/resend", adminHandler.ResendEmailVerification)
			})

			// ── Routes limitées aux administrateurs purs ────────────────────
			r.Group(func(r chi.Router) {
				r.Use(jgmw.RequireAdminAuth())

				r.Get("/users", adminHandler.UsersPage)
				r.Get("/profiles", adminHandler.ProfilesPage)
				r.Get("/authentik", adminHandler.AuthentikPage)
				r.Get("/sso", adminHandler.AuthentikPage)
				r.Get("/security", adminHandler.SecurityPage)
				r.Get("/pending-actions", adminHandler.PendingActionsPage)
				r.Get("/automation", func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/admin/settings#scheduler", http.StatusSeeOther)
				})
				r.Route("/api/users", func(r chi.Router) {
					r.Use(jgmw.RequireCSRF())
					r.Get("/", adminHandler.ListUsers)
					r.Post("/", adminHandler.CreateUser)
					r.Get("/dashboard/stats", adminHandler.DashboardStats)
					r.Get("/invitations", adminHandler.ListInvitations)
					r.Get("/{id}/avatar", adminHandler.UserAvatar)
					r.Get("/{id}/timeline", adminHandler.UserTimeline)
					r.Post("/bulk", adminHandler.BulkUsersAction)
					r.Patch("/{id}", adminHandler.UpdateUser)
					r.Post("/{id}/toggle", adminHandler.ToggleUser)
					r.Post("/{id}/invite-toggle", adminHandler.ToggleUserInvite)
					r.Post("/{id}/ban", adminHandler.BanUser)
					r.Delete("/{id}", adminHandler.DeleteUser)
					r.Post("/{id}/quota", adminHandler.SetUserQuota)
					r.Get("/referrals", adminHandler.GetReferrals)
					r.Post("/{id}/extend", adminHandler.ExtendAccess)
				})

				r.Route("/api/settings", func(r chi.Router) {
					r.Use(jgmw.RequireCSRF())
					r.Get("/", settingsHandler.GetAll)
					r.Post("/general", settingsHandler.SaveGeneral)
					r.Post("/general/fetch-server-name", settingsHandler.FetchJellyfinServerName)
					r.Post("/auth-session", settingsHandler.SaveAuthSession)
					r.Post("/auth-session/revoke", settingsHandler.RevokeAuthSessions)
					r.Post("/authentik", settingsHandler.SaveAuthentik)
					r.Get("/authentik/health", settingsHandler.GetAuthentikHealth)
					r.Post("/authentik/test", settingsHandler.GetAuthentikHealth)
					r.Post("/authentik/reload-env", settingsHandler.ReloadAuthentikFromEnv)
					r.Post("/authentik/test-user", settingsHandler.TestAuthentikUser)
					r.Post("/smtp", settingsHandler.SaveSMTP)
					r.Post("/webhooks", settingsHandler.SaveWebhooks)
					r.Post("/backup", settingsHandler.SaveBackup)
					r.Get("/email-templates/export", settingsHandler.ExportEmailTemplates)
					r.Post("/email-templates", settingsHandler.SaveEmailTemplates)
					r.Post("/email-templates/import", settingsHandler.ImportEmailTemplates)
					r.Post("/email-templates/preview", settingsHandler.PreviewEmailTemplate)
					r.Post("/invitation-profile", settingsHandler.SaveInvitationProfile)
				})

				r.Route("/api/backups", func(r chi.Router) {
					r.Use(jgmw.RequireCSRF())
					r.Get("/", backupHandler.ListBackups)
					r.Post("/create", backupHandler.CreateBackup)
					r.Post("/import", backupHandler.ImportBackup)
					r.Get("/{name}/download", backupHandler.DownloadBackup)
					r.Post("/{name}/restore", backupHandler.RestoreBackup)
					r.Delete("/{name}", backupHandler.DeleteBackup)
				})

				r.Route("/api/logs", func(r chi.Router) {
					r.Use(jgmw.RequireCSRF())
					r.Get("/", adminHandler.LogsAPI)
				})

				r.Route("/api/security", func(r chi.Router) {
					r.Use(jgmw.RequireCSRF())
					r.Get("/overview", adminHandler.SecurityOverview)
					r.Get("/events", adminHandler.SecurityEvents)
				})

				r.Route("/api/pending-actions", func(r chi.Router) {
					r.Use(jgmw.RequireCSRF())
					r.Get("/", adminHandler.PendingActions)
				})

				r.Route("/api/automation", func(r chi.Router) {
					r.Use(jgmw.RequireCSRF())
					r.Get("/libraries", automationHandler.ListLibraries)
					r.Route("/presets", func(r chi.Router) {
						r.Get("/", automationHandler.ListPresets)
						r.Post("/", automationHandler.SavePresets)
					})
					r.Route("/group-mappings", func(r chi.Router) {
						r.Get("/", automationHandler.ListGroupMappings)
						r.Post("/", automationHandler.SaveGroupMappings)
					})
					r.Route("/tasks", func(r chi.Router) {
						r.Get("/", automationHandler.ListTasks)
						r.Post("/", automationHandler.CreateTask)
						r.Patch("/{id}", automationHandler.UpdateTask)
						r.Delete("/{id}", automationHandler.DeleteTask)
						r.Post("/{id}/run", automationHandler.RunTaskNow)
					})
				})

				r.Get("/settings", adminHandler.SettingsPage)
				r.Get("/email-templates", adminHandler.EmailTemplatesPage)
				r.Post("/settings", handlePlaceholder("Sauvegarder les paramètres"))

				r.Get("/logs", adminHandler.LogsPage)
			})

			// ── Routes d'invitations (Filtées en interne selon IsAdmin) ─────
			r.Route("/invitations", func(r chi.Router) {
				r.Get("/", adminHandler.InvitationsPage)
			})
			r.Route("/api/invitations", func(r chi.Router) {
				r.Use(jgmw.RequireCSRF())
				r.Get("/", adminHandler.ListInvitations)
				r.Get("/stats", adminHandler.InvitationStats)
				r.Get("/security", adminHandler.InvitationSecurityConfig)
				r.Post("/security", adminHandler.SaveInvitationSecurityConfig)
				r.Post("/preview", adminHandler.PreviewInvitation)
				r.Post("/", adminHandler.CreateInvitation)
				r.Delete("/{id}", adminHandler.DeleteInvitation)
			})

			// ── Route de profil (Changement MDP, par tout le monde) ─────────
			// (Supprimé car doublon avec le bloc défini plus haut)

		}) // fin Group RequireAuth
	})

	// ── Lancer la Job d'expiration Automatique ──────────────────────────────
	ctx, cancelMain := context.WithCancel(context.Background())
	defer cancelMain()
	adminHandler.StartExpirationJob(ctx)
	backupService.StartScheduler(ctx)
	schedulerService.Start(ctx)

	// ── 5. Démarrer le serveur HTTP ─────────────────────────────────────────
	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Démarrage non-bloquant dans une goroutine
	go func() {
		if cfg.TLSCert != "" && cfg.TLSKey != "" {
			slog.Info("Serveur HTTPS démarré", "addr", addr, "url", cfg.BaseURL)
			if err := srv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("Erreur du serveur HTTPS", "error", err)
				os.Exit(1)
			}
		} else {
			slog.Info("Serveur HTTP démarré", "addr", addr, "url", cfg.BaseURL)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("Erreur du serveur HTTP", "error", err)
				os.Exit(1)
			}
		}
	}()

	// ── 6. Arrêt gracieux (graceful shutdown) ───────────────────────────────
	// Écouter les signaux d'arrêt (SIGINT = Ctrl+C, SIGTERM = Docker stop)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	slog.Info("Signal d'arrêt reçu, arrêt gracieux...", "signal", sig)

	// Annuler le contexte global pour arrêter les routines d'arrière-plan (scheduler, etc.)
	cancelMain()

	// Laisser 10 secondes pour terminer les requêtes en cours
	ctxShutdown, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()

	if err := srv.Shutdown(ctxShutdown); err != nil {
		slog.Error("Erreur lors de l'arrêt du serveur", "error", err)
	}

	// Fermer proprement la base de données
	if err := db.Close(); err != nil {
		slog.Error("Erreur lors de la fermeture de la base de données", "error", err)
	} else {
		slog.Info("Base de données fermée proprement")
	}

	slog.Info("✅ JellyGate arrêté proprement")
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
		if jfClient.IsConfigured() {
			status = string(jfClient.Status())
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": status,
			"app":    "JellyGate",
		})
	}
}

// authSessionAllowed applique la politique de revocation globale stockee en base.
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
	return cfg.AcceptsIssuedAt(sess.Iat)
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

// handlePlaceholder génère un handler temporaire qui renvoie un message
// indiquant que la route existe mais n'est pas encore implémentée.
// Sera remplacé par les vrais handlers au fur et à mesure.
func handlePlaceholder(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "not_implemented", "route": name, "method": r.Method, "path": r.URL.Path})
	}
}
