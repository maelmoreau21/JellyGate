// Package main est le point d'entrée de JellyGate.
//
// JellyGate est un gestionnaire d'invitations, de récupération de mots de passe
// et d'utilisateurs pour Jellyfin/Emby avec intégration Active Directory (LDAP).
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
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/maelmoreau21/JellyGate/internal/backup"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/handlers"
	"github.com/maelmoreau21/JellyGate/internal/integrations"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	jgldap "github.com/maelmoreau21/JellyGate/internal/ldap"
	"github.com/maelmoreau21/JellyGate/internal/mail"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/notify"
	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/scheduler"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

func main() {
	// ── 1. Initialiser le logger structuré ──────────────────────────────────
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("🚀 Démarrage de JellyGate...")

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
	slog.Info("Client Jellyfin initialisé")

	// LDAP (optionnel — chargé depuis la base)
	ldapCfg, _ := db.GetLDAPConfig()
	var ldClient *jgldap.Client
	if ldapCfg.Enabled {
		ldClient = jgldap.New(ldapCfg)
		slog.Info("Client LDAP initialisé", "host", ldapCfg.Host)
	} else {
		slog.Info("Intégration LDAP désactivée")
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
	authHandler := handlers.NewAuthHandler(cfg, db, renderEngine)
	inviteHandler := handlers.NewInvitationHandler(cfg, db, jfClient, ldClient, provisioner, mailer, notifier, renderEngine)
	adminHandler := handlers.NewAdminHandler(cfg, db, jfClient, ldClient, mailer, renderEngine)
	resetHandler := handlers.NewPasswordResetHandler(cfg, db, jfClient, ldClient, mailer, renderEngine)
	settingsHandler := handlers.NewSettingsHandler(db, jfClient, renderEngine)
	backupService := backup.NewService(cfg.DataDir, db)
	backupHandler := handlers.NewBackupHandler(db, backupService, renderEngine)
	schedulerService := scheduler.NewService(db, jfClient, backupService, mailer, notifier)
	automationHandler := handlers.NewAutomationHandler(db, renderEngine, schedulerService, jfClient)

	// Callbacks de rechargement à chaud
	settingsHandler.OnLDAPReload = func(c config.LDAPConfig) {
		if c.Enabled {
			ldClient = jgldap.New(c)
			slog.Info("🔄 Client LDAP rechargé", "host", c.Host)
		} else {
			ldClient = nil
			slog.Info("🔄 Intégration LDAP désactivée")
		}
		inviteHandler.SetLDAPClient(ldClient)
		adminHandler.SetLDAPClient(ldClient)
		resetHandler.SetLDAPClient(ldClient)
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
			resetHandler.SetMailer(mailer)
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
		http.Redirect(w, r, "/admin/login", http.StatusFound)
	})
	// Répondre aux requêtes HEAD sur la racine pour satisfaire les healthchecks
	r.Head("/", handleHealthCheck)

	// Endpoint de santé
	r.Get("/health", handleHealthCheck)
	r.Head("/health", handleHealthCheck)

	r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "web/static/favicon.svg")
	})

	// Fichiers statiques
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	// Routes d'invitation (publiques)
	r.Route("/invite", func(r chi.Router) {
		r.Get("/{code}", inviteHandler.InvitePage)
		r.With(jgmw.RateLimitByIP(15, 5*time.Minute)).Post("/{code}", inviteHandler.InviteSubmit)
	})

	// Routes de réinitialisation de mot de passe (publiques)
	r.Route("/reset", func(r chi.Router) {
		r.Get("/", resetHandler.RequestPage)
		r.With(jgmw.RateLimitByIP(10, 10*time.Minute)).Post("/request", resetHandler.SubmitRequest)
		r.Get("/{code}", resetHandler.ResetPage)
		r.With(jgmw.RateLimitByIP(12, 10*time.Minute)).Post("/{code}", resetHandler.SubmitReset)
	})

	r.Route("/verify-email", func(r chi.Router) {
		r.Get("/{code}", inviteHandler.VerifyEmailPage)
	})

	// ── Routes admin (authentification requise) ─────────────────────────────
	r.Route("/admin", func(r chi.Router) {
		r.Use(jgmw.EnsureCSRFCookie(cfg.BaseURL))
		// Routes publiques (login/logout) — pas de middleware auth
		r.Get("/login", authHandler.LoginPage)
		r.With(jgmw.RateLimitByIP(12, 10*time.Minute), jgmw.RequireCSRF()).Post("/login", authHandler.LoginSubmit)
		r.With(jgmw.RequireCSRF()).Post("/logout", authHandler.Logout)

		if cfg.EnableDebugRoutes {
			slog.Warn("Routes debug admin activées: à ne jamais utiliser en production")

			// DEBUG ROUTE (local only): bypass auth and call ListInvitations with a fake admin session
			// Use only for local debugging to reproduce API errors.
			r.Get("/debug/invitations-bypass", func(w http.ResponseWriter, r *http.Request) {
				sess := &session.Payload{UserID: "1", Username: "debug-admin", IsAdmin: true, Exp: time.Now().Add(24 * time.Hour).Unix()}
				r = r.WithContext(session.NewContext(r.Context(), sess))
				adminHandler.ListInvitations(w, r)
			})

			// DEBUG route for InvitationStats
			r.Get("/debug/invitations-stats-bypass", func(w http.ResponseWriter, r *http.Request) {
				sess := &session.Payload{UserID: "1", Username: "debug-admin", IsAdmin: true, Exp: time.Now().Add(24 * time.Hour).Unix()}
				r = r.WithContext(session.NewContext(r.Context(), sess))
				adminHandler.InvitationStats(w, r)
			})

			// DEBUG route: verify jellygate_session cookie using server secret and return error (local only)
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
		}

		// Routes protégées par le middleware d'authentification global (standard + admin)
		r.Group(func(r chi.Router) {
			r.Use(jgmw.RequireAuth(cfg.SecretKey, cfg.BaseURL))

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
				r.Get("/automation", automationHandler.AutomationPage)
				r.Get("/product", adminHandler.ProductPage)
				r.Route("/api/users", func(r chi.Router) {
					r.Use(jgmw.RequireCSRF())
					r.Get("/", adminHandler.ListUsers)
					r.Post("/", adminHandler.CreateUser)
					r.Get("/dashboard/stats", adminHandler.DashboardStats)
					r.Get("/invitations", adminHandler.ListInvitations)
					r.Get("/{id}/avatar", adminHandler.UserAvatar)
					r.Get("/{id}/timeline", adminHandler.UserTimeline)
					r.Post("/bulk", adminHandler.BulkUsersAction)
					r.Post("/sync", adminHandler.SyncJellyfinUsers)
					r.Patch("/{id}", adminHandler.UpdateUser)
					r.Post("/{id}/toggle", adminHandler.ToggleUser)
					r.Post("/{id}/password-reset/send", adminHandler.SendUserPasswordReset)
					r.Post("/{id}/invite-toggle", adminHandler.ToggleUserInvite)
					r.Post("/{id}/ban", adminHandler.BanUser)
					r.Delete("/{id}", adminHandler.DeleteUser)
					r.Post("/{id}/extend", adminHandler.ExtendAccess)
				})

				r.Route("/api/settings", func(r chi.Router) {
					r.Use(jgmw.RequireCSRF())
					r.Get("/", settingsHandler.GetAll)
					r.Post("/general", settingsHandler.SaveGeneral)
					r.Post("/general/fetch-server-name", settingsHandler.FetchJellyfinServerName)
					r.Post("/ldap", settingsHandler.SaveLDAP)
					r.Post("/ldap/test-connection", settingsHandler.TestLDAPConnection)
					r.Post("/ldap/test-user", settingsHandler.TestLDAPUserLookup)
					r.Post("/ldap/test-jellyfin-auth", settingsHandler.TestJellyfinLDAPAuth)
					r.Post("/smtp", settingsHandler.SaveSMTP)
					r.Post("/webhooks", settingsHandler.SaveWebhooks)
					r.Post("/backup", settingsHandler.SaveBackup)
					r.Post("/email-templates", settingsHandler.SaveEmailTemplates)
					r.Post("/email-templates/preview", settingsHandler.PreviewEmailTemplate)
					r.Post("/invitation-profile", settingsHandler.SaveInvitationProfile)
				})

				r.Route("/api/product", func(r chi.Router) {
					r.Use(jgmw.RequireCSRF())
					r.Get("/config", adminHandler.ProductConfig)
					r.Post("/config", adminHandler.SaveProductConfig)
					r.Get("/health", adminHandler.ProductHealth)
					r.Get("/timeline", adminHandler.ProductTimeline)
					r.Get("/lifecycle", adminHandler.ProductLifecyclePreview)
					r.Post("/markdown-preview", adminHandler.ProductMarkdownPreview)
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

	// Laisser 10 secondes pour terminer les requêtes en cours
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("Erreur lors de l'arrêt du serveur", "error", err)
		os.Exit(1)
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
