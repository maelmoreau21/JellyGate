// Package main est le point d'entrée de JellyGate.
//
// JellyGate est un gestionnaire d'invitations, de parrainage
// et d'utilisateurs pour Jellyfin avec intégration Authentik (OIDC).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/authentik"
	"github.com/maelmoreau21/JellyGate/internal/backup"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/handlers"
	"github.com/maelmoreau21/JellyGate/internal/integrations"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	"github.com/maelmoreau21/JellyGate/internal/mail"
	"github.com/maelmoreau21/JellyGate/internal/notify"
	"github.com/maelmoreau21/JellyGate/internal/oidc"
	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/scheduler"
	"github.com/maelmoreau21/JellyGate/internal/session"
	"github.com/maelmoreau21/JellyGate/internal/syslog"
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

	// ── 2b. Initialiser le gestionnaire de logs système (disque + console) ──
	if _, err := syslog.Init(cfg.DataDir); err != nil {
		slog.Warn("⚠️ Impossible d'initialiser la journalisation sur disque", "error", err)
	} else {
		slog.Info("📁 Journalisation système activée", "dir", cfg.DataDir+"/logs")
	}

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

	// ── 3b. Initialiser les clients de service à partir des settings DB & Env ──
	jellyfinCfg := cfg.Jellyfin
	if db != nil {
		if dbJfCfg, err := db.GetJellyfinConfig(); err == nil {
			if strings.TrimSpace(jellyfinCfg.URL) == "" {
				jellyfinCfg.URL = dbJfCfg.URL
			}
			if strings.TrimSpace(jellyfinCfg.APIKey) == "" {
				jellyfinCfg.APIKey = dbJfCfg.APIKey
			}
		}
	}
	jfClient := jellyfin.New(jellyfinCfg)
	if jfClient.IsConfigured() {
		slog.Info("Client Jellyfin initialisé", "url", jellyfinCfg.URL)
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
		if dbAuthCfg, err := db.GetAuthentikConfig(); err == nil {
			if dbAuthCfg.Enabled || dbAuthCfg.URL != "" || dbAuthCfg.IssuerURL != "" {
				if strings.TrimSpace(dbAuthCfg.URL) != "" {
					authentikCfg.URL = dbAuthCfg.URL
				}
				if strings.TrimSpace(dbAuthCfg.IssuerURL) != "" {
					authentikCfg.IssuerURL = dbAuthCfg.IssuerURL
				}
				if strings.TrimSpace(dbAuthCfg.ClientID) != "" {
					authentikCfg.ClientID = dbAuthCfg.ClientID
				}
				if strings.TrimSpace(dbAuthCfg.ClientSecret) != "" {
					authentikCfg.ClientSecret = dbAuthCfg.ClientSecret
				}
				if strings.TrimSpace(dbAuthCfg.RedirectURL) != "" {
					authentikCfg.RedirectURL = dbAuthCfg.RedirectURL
				}
				if strings.TrimSpace(dbAuthCfg.APIToken) != "" {
					authentikCfg.APIToken = dbAuthCfg.APIToken
				}
				if strings.TrimSpace(dbAuthCfg.UserGroup) != "" {
					authentikCfg.UserGroup = dbAuthCfg.UserGroup
				}
				if strings.TrimSpace(dbAuthCfg.AdminGroup) != "" {
					authentikCfg.AdminGroup = dbAuthCfg.AdminGroup
				}
				if strings.TrimSpace(dbAuthCfg.JellyfinUserGroup) != "" {
					authentikCfg.JellyfinUserGroup = dbAuthCfg.JellyfinUserGroup
				}
				if strings.TrimSpace(dbAuthCfg.EnrollmentFlowSlug) != "" {
					authentikCfg.EnrollmentFlowSlug = dbAuthCfg.EnrollmentFlowSlug
				}
				if strings.TrimSpace(dbAuthCfg.InvitersGroup) != "" {
					authentikCfg.InvitersGroup = dbAuthCfg.InvitersGroup
				}
				if strings.TrimSpace(dbAuthCfg.InvitersRecursiveGroup) != "" {
					authentikCfg.InvitersRecursiveGroup = dbAuthCfg.InvitersRecursiveGroup
				}
				authentikCfg.Enabled = dbAuthCfg.Enabled
			}
		}
	}
	// Surcharge par les variables d'environnement si définies explicitement
	if strings.TrimSpace(cfg.Authentik.URL) != "" {
		authentikCfg.URL = strings.TrimSpace(cfg.Authentik.URL)
	}
	if strings.TrimSpace(cfg.Authentik.IssuerURL) != "" {
		authentikCfg.IssuerURL = strings.TrimSpace(cfg.Authentik.IssuerURL)
	}
	if strings.TrimSpace(cfg.Authentik.ClientID) != "" {
		authentikCfg.ClientID = strings.TrimSpace(cfg.Authentik.ClientID)
	}
	if strings.TrimSpace(cfg.Authentik.ClientSecret) != "" {
		authentikCfg.ClientSecret = strings.TrimSpace(cfg.Authentik.ClientSecret)
	}
	if strings.TrimSpace(cfg.Authentik.RedirectURL) != "" {
		authentikCfg.RedirectURL = strings.TrimSpace(cfg.Authentik.RedirectURL)
	}
	if strings.TrimSpace(cfg.Authentik.APIToken) != "" {
		authentikCfg.APIToken = strings.TrimSpace(cfg.Authentik.APIToken)
	}
	if cfg.Authentik.Enabled {
		authentikCfg.Enabled = true
	}
	if authentikCfg.RedirectURL == "" && strings.TrimSpace(cfg.BaseURL) != "" {
		authentikCfg.RedirectURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/") + "/auth/callback"
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
	schedulerService.SetAuthentikClient(authentikClient)
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
		schedulerService.SetAuthentikClient(newAuthClient)
		slog.Info("🔄 Clients OIDC & Authentik rechargés", "enabled", c.Enabled, "url", c.URL)
	}
	settingsHandler.OnJellyfinReload = func(c config.JellyfinConfig) {
		jfClient.UpdateConfig(c)
		if jfClient.IsConfigured() {
			go jfClient.LogDiagnostics()
		}
		slog.Info("🔄 Client Jellyfin rechargé", "url", c.URL)
	}

	// ── 4. Configurer le routeur Chi ────────────────────────────────────────
	r := setupRouter(RouterParams{
		Cfg:                  cfg,
		DB:                   db,
		RenderEngine:         renderEngine,
		JFClient:             jfClient,
		OIDCClient:           oidcClient,
		AuthentikClient:      authentikClient,
		AuthHandler:          authHandler,
		InviteHandler:        inviteHandler,
		AdminHandler:         adminHandler,
		SettingsHandler:      settingsHandler,
		BackupHandler:        backupHandler,
		AutomationHandler:    automationHandler,
		AuthSessionValidator: authSessionValidator,
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
