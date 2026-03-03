// Package mail fournit un client SMTP pour l'envoi d'emails dans JellyGate.
//
// Utilise github.com/wneessen/go-mail pour une gestion robuste de :
//   - Authentification SMTP (PLAIN, LOGIN, CRAM-MD5)
//   - STARTTLS / TLS direct
//   - Templates HTML avec html/template
//   - Test de connexion au démarrage (Ping)
//
// Les templates sont chargés depuis web/templates/emails/.
package mail

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	gomail "github.com/wneessen/go-mail"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

// ── Mailer ──────────────────────────────────────────────────────────────────

// Mailer encapsule l'envoi d'emails via SMTP.
type Mailer struct {
	cfg       config.SMTPConfig
	from      string
	templates *template.Template // Templates HTML préchargés
}

// New crée un nouveau Mailer à partir de la configuration SMTP.
//
// Charge tous les templates depuis web/templates/emails/*.html.
// Si le dossier de templates n'existe pas, le mailer fonctionne quand même
// mais SendMail retournera une erreur si un template est requis.
func New(cfg config.SMTPConfig) (*Mailer, error) {
	m := &Mailer{
		cfg:  cfg,
		from: cfg.From,
	}

	// Charger les templates HTML
	templatesDir := filepath.Join("web", "templates", "emails")
	pattern := filepath.Join(templatesDir, "*.html")

	// Vérifier si le dossier de templates existe
	if _, err := os.Stat(templatesDir); err == nil {
		tmpl, err := template.ParseGlob(pattern)
		if err != nil {
			slog.Warn("Erreur de chargement des templates email (non-bloquant)",
				"pattern", pattern,
				"error", err,
			)
		} else {
			m.templates = tmpl
			slog.Info("Templates email chargés",
				"count", len(tmpl.Templates()),
				"dir", templatesDir,
			)
		}
	} else {
		slog.Warn("Dossier de templates email introuvable (non-bloquant)",
			"dir", templatesDir,
		)
	}

	return m, nil
}

// ── Ping ────────────────────────────────────────────────────────────────────

// Ping teste la connexion au serveur SMTP.
// Doit être appelé au démarrage de l'application pour vérifier la
// configuration SMTP avant d'accepter des requêtes.
//
// Ouvre une connexion, effectue le handshake EHLO/HELO, puis ferme.
func (m *Mailer) Ping() error {
	client, err := m.newClient()
	if err != nil {
		return fmt.Errorf("mail.Ping: %w", err)
	}

	// Ouvrir la connexion avec un timeout court
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.DialWithContext(ctx); err != nil {
		return fmt.Errorf("mail.Ping: impossible de joindre le serveur SMTP %s:%d — %w",
			m.cfg.Host, m.cfg.Port, err)
	}
	defer client.Close()

	slog.Info("Connexion SMTP vérifiée",
		"host", m.cfg.Host,
		"port", m.cfg.Port,
		"tls", m.cfg.UseTLS,
	)

	return nil
}

// ── SendMail ────────────────────────────────────────────────────────────────

// SendMail envoie un email à partir d'un template HTML.
//
// Paramètres :
//   - to           : adresse email du destinataire
//   - subject      : sujet de l'email
//   - templateName : nom du template (sans extension, ex: "welcome")
//   - data         : données injectées dans le template
//
// Le template est recherché dans web/templates/emails/{templateName}.html.
// Si le template n'existe pas dans les templates préchargés, il est chargé
// dynamiquement depuis le disque.
func (m *Mailer) SendMail(to, subject, templateName string, data interface{}) error {
	if to == "" {
		return fmt.Errorf("mail.SendMail: adresse destinataire vide")
	}

	// ── 1. Rendre le template HTML ──────────────────────────────────────
	htmlBody, err := m.renderTemplate(templateName, data)
	if err != nil {
		return fmt.Errorf("mail.SendMail: %w", err)
	}

	// ── 2. Construire le message ────────────────────────────────────────
	msg := gomail.NewMsg()

	if err := msg.From(m.from); err != nil {
		return fmt.Errorf("mail.SendMail: adresse expéditeur invalide %q: %w", m.from, err)
	}
	if err := msg.To(to); err != nil {
		return fmt.Errorf("mail.SendMail: adresse destinataire invalide %q: %w", to, err)
	}

	msg.Subject(subject)
	msg.SetMessageID()
	msg.SetDate()
	msg.SetBodyString(gomail.TypeTextHTML, htmlBody)

	// Ajouter une version texte brut (fallback)
	// TODO: Générer un texte brut à partir du HTML (strip tags)

	// ── 3. Envoyer via le client SMTP ───────────────────────────────────
	client, err := m.newClient()
	if err != nil {
		return fmt.Errorf("mail.SendMail: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.DialAndSendWithContext(ctx, msg); err != nil {
		return fmt.Errorf("mail.SendMail: échec d'envoi à %q — %w", to, err)
	}

	slog.Info("Email envoyé",
		"to", to,
		"subject", subject,
		"template", templateName,
	)

	return nil
}

// ── SendRawHTML ──────────────────────────────────────────────────────────────

// SendRawHTML envoie un email avec un corps HTML fourni directement
// (sans passer par un template). Utile pour les cas simples ou les tests.
func (m *Mailer) SendRawHTML(to, subject, htmlBody string) error {
	if to == "" {
		return fmt.Errorf("mail.SendRawHTML: adresse destinataire vide")
	}

	msg := gomail.NewMsg()

	if err := msg.From(m.from); err != nil {
		return fmt.Errorf("mail.SendRawHTML: adresse expéditeur invalide: %w", err)
	}
	if err := msg.To(to); err != nil {
		return fmt.Errorf("mail.SendRawHTML: adresse destinataire invalide: %w", err)
	}

	msg.Subject(subject)
	msg.SetMessageID()
	msg.SetDate()
	msg.SetBodyString(gomail.TypeTextHTML, htmlBody)

	client, err := m.newClient()
	if err != nil {
		return fmt.Errorf("mail.SendRawHTML: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.DialAndSendWithContext(ctx, msg); err != nil {
		return fmt.Errorf("mail.SendRawHTML: échec d'envoi à %q — %w", to, err)
	}

	slog.Info("Email brut envoyé", "to", to, "subject", subject)
	return nil
}

// ── Méthodes internes ───────────────────────────────────────────────────────

// newClient crée un nouveau client go-mail configuré avec les paramètres SMTP.
func (m *Mailer) newClient() (*gomail.Client, error) {
	// Options de base
	opts := []gomail.Option{
		gomail.WithPort(m.cfg.Port),
		gomail.WithTimeout(10 * time.Second),
		gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
		gomail.WithUsername(m.cfg.Username),
		gomail.WithPassword(m.cfg.Password),
	}

	// Configuration TLS
	if m.cfg.UseTLS {
		if m.cfg.Port == 465 {
			// Port 465 : TLS implicite (connexion directe en TLS)
			opts = append(opts, gomail.WithSSLPort(false))
		} else {
			// Port 587 ou autre : STARTTLS (upgrade de la connexion)
			opts = append(opts, gomail.WithTLSPolicy(gomail.TLSMandatory))
		}
	} else {
		// Pas de TLS (déconseillé en production)
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS))
	}

	client, err := gomail.NewClient(m.cfg.Host, opts...)
	if err != nil {
		return nil, fmt.Errorf("erreur de création du client SMTP: %w", err)
	}

	return client, nil
}

// renderTemplate rend un template HTML avec les données fournies.
func (m *Mailer) renderTemplate(templateName string, data interface{}) (string, error) {
	fullName := templateName + ".html"

	// Essayer les templates préchargés d'abord
	if m.templates != nil {
		tmpl := m.templates.Lookup(fullName)
		if tmpl != nil {
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, data); err != nil {
				return "", fmt.Errorf("erreur de rendu du template %q: %w", fullName, err)
			}
			return buf.String(), nil
		}
	}

	// Fallback : charger directement depuis le disque
	path := filepath.Join("web", "templates", "emails", fullName)
	tmpl, err := template.ParseFiles(path)
	if err != nil {
		return "", fmt.Errorf("template %q introuvable (%s): %w", templateName, path, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("erreur de rendu du template %q: %w", templateName, err)
	}

	return buf.String(), nil
}
