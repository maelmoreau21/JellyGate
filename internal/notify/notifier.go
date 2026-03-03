// Package notify envoie des notifications multi-plateformes (Discord, Telegram, Matrix).
//
// Toutes les notifications sont envoyées de manière asynchrone via des
// goroutines — elles ne bloquent jamais le flux HTTP principal.
//
// Chaque plateforme est conditionnelle : si l'URL/token est vide dans
// la configuration, l'envoi est simplement ignoré.
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

// ── Notifier ────────────────────────────────────────────────────────────────

// Notifier gère l'envoi asynchrone de notifications vers Discord, Telegram et Matrix.
type Notifier struct {
	cfg    config.WebhooksConfig
	client *http.Client
}

// New crée un nouveau Notifier à partir de la configuration webhooks.
func New(cfg config.WebhooksConfig) *Notifier {
	return &Notifier{
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ── Événements ──────────────────────────────────────────────────────────────

// UserRegisteredEvent contient les données d'un événement d'inscription.
type UserRegisteredEvent struct {
	Username    string
	DisplayName string
	Email       string
	InviteCode  string
	InvitedBy   string
	JellyfinID  string
	LdapDN      string
	Timestamp   time.Time
}

// ── Envoi asynchrone ────────────────────────────────────────────────────────

// NotifyUserRegistered envoie des notifications d'inscription sur toutes
// les plateformes configurées. L'exécution est entièrement asynchrone —
// cette méthode retourne immédiatement.
func (n *Notifier) NotifyUserRegistered(event UserRegisteredEvent) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Discord
	if n.cfg.Discord.URL != "" {
		go n.sendDiscord(event)
	}

	// Telegram
	if n.cfg.Telegram.Token != "" && n.cfg.Telegram.ChatID != "" {
		go n.sendTelegram(event)
	}

	// Matrix
	if n.cfg.Matrix.URL != "" && n.cfg.Matrix.RoomID != "" && n.cfg.Matrix.Token != "" {
		go n.sendMatrix(event)
	}
}

// ── Discord ─────────────────────────────────────────────────────────────────

// sendDiscord envoie une notification via un webhook Discord.
//
// Format : Embed riche avec couleur verte et champs structurés.
// API : POST <webhook_url> avec Content-Type: application/json
func (n *Notifier) sendDiscord(event UserRegisteredEvent) {
	// Construire l'embed Discord
	payload := map[string]interface{}{
		"username":   "JellyGate",
		"avatar_url": "",
		"embeds": []map[string]interface{}{
			{
				"title":       "🎉 Nouvel utilisateur inscrit",
				"description": fmt.Sprintf("**%s** vient de créer un compte via invitation.", event.DisplayName),
				"color":       3066993, // Vert (#2ECC71)
				"fields": []map[string]interface{}{
					{"name": "👤 Username", "value": fmt.Sprintf("`%s`", event.Username), "inline": true},
					{"name": "📧 Email", "value": n.maskOrEmpty(event.Email), "inline": true},
					{"name": "🎫 Invitation", "value": fmt.Sprintf("`%s`", event.InviteCode), "inline": true},
					{"name": "👥 Invité par", "value": n.valueOrNA(event.InvitedBy), "inline": true},
				},
				"timestamp": event.Timestamp.Format(time.RFC3339),
				"footer": map[string]string{
					"text": "JellyGate",
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("Discord: erreur de sérialisation", "error", err)
		return
	}

	resp, err := n.client.Post(n.cfg.Discord.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		slog.Error("Discord: erreur d'envoi", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		slog.Error("Discord: réponse HTTP inattendue",
			"status", resp.StatusCode,
			"body", string(respBody),
		)
		return
	}

	slog.Info("Discord: notification envoyée", "username", event.Username)
}

// ── Telegram ────────────────────────────────────────────────────────────────

// sendTelegram envoie une notification via l'API Bot Telegram.
//
// API : POST https://api.telegram.org/bot<token>/sendMessage
// Format : Message HTML avec mise en forme.
func (n *Notifier) sendTelegram(event UserRegisteredEvent) {
	text := fmt.Sprintf(
		"🎉 <b>Nouvel utilisateur inscrit</b>\n\n"+
			"👤 Username: <code>%s</code>\n"+
			"📝 Nom: %s\n"+
			"📧 Email: %s\n"+
			"🎫 Invitation: <code>%s</code>\n"+
			"👥 Invité par: %s\n"+
			"🕐 %s",
		event.Username,
		event.DisplayName,
		n.maskOrEmpty(event.Email),
		event.InviteCode,
		n.valueOrNA(event.InvitedBy),
		event.Timestamp.Format("02/01/2006 15:04"),
	)

	payload := map[string]interface{}{
		"chat_id":    n.cfg.Telegram.ChatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("Telegram: erreur de sérialisation", "error", err)
		return
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.cfg.Telegram.Token)

	resp, err := n.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		slog.Error("Telegram: erreur d'envoi", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		slog.Error("Telegram: réponse HTTP inattendue",
			"status", resp.StatusCode,
			"body", string(respBody),
		)
		return
	}

	slog.Info("Telegram: notification envoyée", "username", event.Username, "chat_id", n.cfg.Telegram.ChatID)
}

// ── Matrix ──────────────────────────────────────────────────────────────────

// sendMatrix envoie une notification via l'API client-server Matrix.
//
// API : PUT /_matrix/client/v3/rooms/{roomId}/send/m.room.message/{txnId}
// Auth : Bearer token dans le header Authorization.
func (n *Notifier) sendMatrix(event UserRegisteredEvent) {
	// Construire le corps du message (format HTML)
	htmlBody := fmt.Sprintf(
		"<h3>🎉 Nouvel utilisateur inscrit</h3>"+
			"<ul>"+
			"<li><b>Username:</b> <code>%s</code></li>"+
			"<li><b>Nom:</b> %s</li>"+
			"<li><b>Email:</b> %s</li>"+
			"<li><b>Invitation:</b> <code>%s</code></li>"+
			"<li><b>Invité par:</b> %s</li>"+
			"</ul>",
		event.Username,
		event.DisplayName,
		n.maskOrEmpty(event.Email),
		event.InviteCode,
		n.valueOrNA(event.InvitedBy),
	)

	plainBody := fmt.Sprintf(
		"🎉 Nouvel utilisateur inscrit\n"+
			"Username: %s\nNom: %s\nEmail: %s\nInvitation: %s\nInvité par: %s",
		event.Username, event.DisplayName,
		n.maskOrEmpty(event.Email), event.InviteCode,
		n.valueOrNA(event.InvitedBy),
	)

	payload := map[string]interface{}{
		"msgtype":        "m.text",
		"body":           plainBody,
		"format":         "org.matrix.custom.html",
		"formatted_body": htmlBody,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("Matrix: erreur de sérialisation", "error", err)
		return
	}

	// Transaction ID unique basé sur le timestamp
	txnID := fmt.Sprintf("jg_%d", time.Now().UnixNano())

	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s",
		n.cfg.Matrix.URL, n.cfg.Matrix.RoomID, txnID)

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		slog.Error("Matrix: erreur de création de la requête", "error", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", n.cfg.Matrix.Token))

	resp, err := n.client.Do(req)
	if err != nil {
		slog.Error("Matrix: erreur d'envoi", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		slog.Error("Matrix: réponse HTTP inattendue",
			"status", resp.StatusCode,
			"body", string(respBody),
		)
		return
	}

	slog.Info("Matrix: notification envoyée", "username", event.Username, "room", n.cfg.Matrix.RoomID)
}

// ── Utilitaires ─────────────────────────────────────────────────────────────

// maskOrEmpty masque partiellement un email ou retourne "N/A" si vide.
func (n *Notifier) maskOrEmpty(email string) string {
	if email == "" {
		return "N/A"
	}
	// Masquer : afficher 2 premiers chars + *** + domaine
	parts := splitEmail(email)
	if len(parts) != 2 {
		return "***"
	}
	local := parts[0]
	if len(local) > 2 {
		local = local[:2] + "***"
	}
	return local + "@" + parts[1]
}

// valueOrNA retourne la valeur ou "N/A" si vide.
func (n *Notifier) valueOrNA(value string) string {
	if value == "" {
		return "N/A"
	}
	return value
}

// splitEmail sépare un email en [local, domain].
func splitEmail(email string) []string {
	at := -1
	for i, c := range email {
		if c == '@' {
			at = i
			break
		}
	}
	if at < 0 {
		return nil
	}
	return []string{email[:at], email[at+1:]}
}
