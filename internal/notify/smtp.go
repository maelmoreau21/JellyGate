package notify

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/smtp"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

// SendInvitationEmail envoie une invitation par e-mail en utilisant la configuration SMTP
// stockée dans la base de données.
func SendInvitationEmail(cfg *config.SMTPConfig, toEmail, inviteURL, message string) error {
	if cfg == nil || cfg.Host == "" {
		return fmt.Errorf("configuration SMTP manquante ou incomplète")
	}

	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)

	// Construction du message (MIME standard HTML)
	subject := "Invitation à rejoindre JellyGate"
	body := fmt.Sprintf(`
		<html>
		<body style="font-family: sans-serif; color: #333; line-height: 1.6;">
			<div style="max-width: 600px; margin: 0 auto; padding: 20px; border: 1px solid #ddd; border-radius: 8px;">
				<h2 style="color: #8b5cf6;">Bienvenue !</h2>
				<p>Vous avez été invité(e) pour créer un compte.</p>
				<p><em>%s</em></p>
				<div style="text-align: center; margin: 30px 0;">
					<a href="%s" style="background: #8b5cf6; color: white; padding: 12px 24px; text-decoration: none; border-radius: 6px; font-weight: bold; display: inline-block;">Créer mon compte</a>
				</div>
				<p>Ou copiez ce lien dans votre navigateur :</p>
				<p style="word-break: break-all; color: #666;"><a href="%s">%s</a></p>
			</div>
		</body>
		</html>
	`, message, inviteURL, inviteURL, inviteURL)

	msg := []byte(fmt.Sprintf("To: %s\r\n"+
		"From: %s\r\n"+
		"Subject: %s\r\n"+
		"MIME-Version: 1.0\r\n"+
		"Content-Type: text/html; charset=UTF-8\r\n"+
		"\r\n"+
		"%s\r\n", toEmail, cfg.From, subject, body))

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	// Connexion et chiffrement
	// go-smtp tente `STARTTLS` automatiquement si le serveur l'annonce.
	// Si tls est forcé (port 465) on utilise Dial TLS natif.
	if cfg.Port == 465 || cfg.UseTLS {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: false,
			ServerName:         cfg.Host,
		}
		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return fmt.Errorf("erreur de connexion SMTP TLS: %w", err)
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			return fmt.Errorf("erreur de client SMTP: %w", err)
		}

		if err = client.Auth(auth); err != nil {
			return fmt.Errorf("erreur d'authentification SMTP: %w", err)
		}

		if err = client.Mail(cfg.From); err != nil {
			return err
		}
		if err = client.Rcpt(toEmail); err != nil {
			return err
		}

		w, err := client.Data()
		if err != nil {
			return err
		}

		_, err = w.Write(msg)
		if err != nil {
			return err
		}

		err = w.Close()
		if err != nil {
			return err
		}

		return client.Quit()
	}

	// Port 587 / 25 classsique avec détection STARTTLS automatique dans SendMail
	err := smtp.SendMail(addr, auth, cfg.From, []string{toEmail}, msg)
	if err != nil {
		slog.Error("Erreur SendMail SMTP", "error", err)
		return fmt.Errorf("erreur lors de l'envoi de l'email: %w", err)
	}

	slog.Info("E-mail d'invitation envoyé avec succès", "to", toEmail)
	return nil
}
