// Package main est le point d'entrée de JellyGate.
//
// JellyGate est un gestionnaire d'invitations, de récupération de mots de passe
// et d'utilisateurs pour Jellyfin/Emby avec intégration Active Directory (LDAP).
package main

import (
	"context"
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

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/handlers"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	jgldap "github.com/maelmoreau21/JellyGate/internal/ldap"
	"github.com/maelmoreau21/JellyGate/internal/mail"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/notify"
	"github.com/maelmoreau21/JellyGate/internal/render"
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

	// ── 3. Initialiser la base de données SQLite ────────────────────────────
	db, err := database.New(cfg.DataDir)
	if err != nil {
		slog.Error("Erreur d'initialisation de la base de données", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("Base de données SQLite initialisée", "path", db.Path())

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

	// ── 3c. Initialiser le moteur de rendu HTML ────────────────────────────
	renderEngine, err := render.NewEngine("web/templates", "web/i18n")
	if err != nil {
		slog.Error("Erreur d'initialisation du moteur de templates", "error", err)
		os.Exit(1)
	}
	slog.Info("Moteur de rendu HTML initialisé")

	// ── 3d. Initialiser les handlers ───────────────────────────────────────
	authHandler := handlers.NewAuthHandler(cfg, db, renderEngine)
	inviteHandler := handlers.NewInvitationHandler(cfg, db, jfClient, ldClient, notifier, renderEngine)
	adminHandler := handlers.NewAdminHandler(cfg, db, jfClient, ldClient, renderEngine)
	resetHandler := handlers.NewPasswordResetHandler(cfg, db, jfClient, ldClient, mailer, renderEngine)
	settingsHandler := handlers.NewSettingsHandler(db)

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
			resetHandler.SetMailer(mailer)
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
	r.Use(chimw.RequestID)                 // ID unique par requête
	r.Use(chimw.RealIP)                    // IP réelle derrière proxy
	r.Use(chimw.Logger)                    // Log de chaque requête
	r.Use(chimw.Recoverer)                 // Récupération des panics
	r.Use(chimw.Timeout(30 * time.Second)) // Timeout global 30s
	r.Use(chimw.Compress(5))               // Compression gzip
	r.Use(jgmw.DetectLanguage(db))         // Détection de langue (cookie → Accept-Language → DB default_lang)

	// ── Routes publiques ────────────────────────────────────────────────────
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/login", http.StatusFound)
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Fichiers statiques
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	// Routes d'invitation (publiques)
	r.Route("/invite", func(r chi.Router) {
		r.Get("/{code}", inviteHandler.InvitePage)
		r.Post("/{code}", inviteHandler.InviteSubmit)
	})

	// Routes de réinitialisation de mot de passe (publiques)
	r.Route("/reset", func(r chi.Router) {
		r.Get("/", resetHandler.RequestPage)
		r.Post("/request", resetHandler.SubmitRequest)
		r.Get("/{code}", resetHandler.ResetPage)
		r.Post("/{code}", resetHandler.SubmitReset)
	})

	// ── Routes admin (authentification requise) ─────────────────────────────
	r.Route("/admin", func(r chi.Router) {
		// Routes publiques (login/logout) — pas de middleware auth
		r.Get("/login", authHandler.LoginPage)
		r.Post("/login", authHandler.LoginSubmit)
		r.Post("/logout", authHandler.Logout)

		// Routes protégées par le middleware d'authentification Jellyfin
		r.Group(func(r chi.Router) {
			r.Use(jgmw.RequireAuth(cfg.SecretKey))

			r.Get("/", adminHandler.DashboardPage)

			// Gestion des utilisateurs — pages HTML
			r.Get("/users", adminHandler.UsersPage)

			// API JSON de gestion des utilisateurs
			r.Route("/api/users", func(r chi.Router) {
				r.Get("/", adminHandler.ListUsers)
				r.Post("/{id}/toggle", adminHandler.ToggleUser)
				r.Post("/{id}/ban", handlePlaceholder("Bannir utilisateur"))
				r.Delete("/{id}", adminHandler.DeleteUser)
				r.Post("/{id}/extend", handlePlaceholder("Prolonger accès utilisateur"))
			})

			// API JSON de gestion des paramètres
			r.Route("/api/settings", func(r chi.Router) {
				r.Get("/", settingsHandler.GetAll)
				r.Post("/general", settingsHandler.SaveGeneral)
				r.Post("/ldap", settingsHandler.SaveLDAP)
				r.Post("/smtp", settingsHandler.SaveSMTP)
				r.Post("/webhooks", settingsHandler.SaveWebhooks)
			})

			// Gestion des invitations
			r.Route("/invitations", func(r chi.Router) {
				r.Get("/", adminHandler.InvitationsPage)
				r.Post("/", handlePlaceholder("Créer une invitation"))
				r.Delete("/{id}", handlePlaceholder("Supprimer une invitation"))
			})

			// Paramètres
			r.Get("/settings", adminHandler.SettingsPage)
			r.Post("/settings", handlePlaceholder("Sauvegarder les paramètres"))

			// Journal d'audit
			r.Get("/logs", adminHandler.LogsPage)
		}) // fin Group (routes protégées)
	})

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
		slog.Info("Serveur HTTP démarré", "addr", addr, "url", cfg.BaseURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Erreur du serveur HTTP", "error", err)
			os.Exit(1)
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
	fmt.Fprintf(w, `{"status":"ok","app":"JellyGate","version":"0.1.0"}`)
}

// handlePlaceholder génère un handler temporaire qui renvoie un message
// indiquant que la route existe mais n'est pas encore implémentée.
// Sera remplacé par les vrais handlers au fur et à mesure.
func handlePlaceholder(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		fmt.Fprintf(w, `{"status":"not_implemented","route":"%s","method":"%s","path":"%s"}`,
			name, r.Method, r.URL.Path)
	}
}
