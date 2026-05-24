// Package notify envoie des notifications multi-plateformes (Discord, Telegram, Matrix).
//
// Toutes les notifications sont envoyÃ©es de maniÃ¨re asynchrone via des
// goroutines â€” elles ne bloquent jamais le flux HTTP principal.
//
// Chaque plateforme est conditionnelle : si l'URL/token est vide dans
// la configuration, l'envoi est simplement ignorÃ©.
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

// â”€â”€ Notifier â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// Notifier gÃ¨re l'envoi asynchrone de notifications vers Discord, Telegram et Matrix.
type Notifier struct {
	cfg    config.WebhooksConfig
	client *http.Client
}

// New crÃ©e un nouveau Notifier Ã  partir de la configuration webhooks.
func New(cfg config.WebhooksConfig) *Notifier {
	return &Notifier{
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// â”€â”€ Ã‰vÃ©nements â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// UserRegisteredEvent contient les donnÃ©es d'un Ã©vÃ©nement d'inscription.
func htmlText(value string) string {
	return html.EscapeString(strings.TrimSpace(value))
}

func discordText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"*", "\\*",
		"_", "\\_",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"|", "\\|",
		"@", "@\u200b",
	)
	return replacer.Replace(value)
}

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

// â”€â”€ Envoi asynchrone â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// NotifyUserRegistered envoie des notifications d'inscription sur toutes
// les plateformes configurÃ©es. L'exÃ©cution est entiÃ¨rement asynchrone â€”
// cette mÃ©thode retourne immÃ©diatement.
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

// NotifyTaskExecuted envoie une notification sur le statut d'exÃ©cution d'une tÃ¢che.
func (n *Notifier) NotifyTaskExecuted(taskName string, success bool, err error) {
	title := "âœ… TÃ¢che terminÃ©e"
	desc := fmt.Sprintf("La tÃ¢che **%s** s'est terminÃ©e avec succÃ¨s.", taskName)
	color := 3066993 // Vert
	if !success {
		title = "â�Œ Ã‰chec de la tÃ¢che"
		desc = fmt.Sprintf("La tÃ¢che **%s** a Ã©chouÃ©.", taskName)
		color = 15158332 // Rouge
	}

	// Discord
	if n.cfg.Discord.URL != "" {
		go n.sendDiscordGeneric(title, desc, color, map[string]string{
			"TÃ¢che": taskName,
			"Status": func() string {
				if success {
					return "SuccÃ¨s"
				}
				return "Ã‰chec"
			}(),
			"Erreur": func() string {
				if err != nil {
					return err.Error()
				}
				return "Aucune"
			}(),
		})
	}

	// Telegram
	if n.cfg.Telegram.Token != "" && n.cfg.Telegram.ChatID != "" {
		statusIcon := "âœ…"
		if !success {
			statusIcon = "â�Œ"
		}
		text := fmt.Sprintf("%s <b>%s</b>\n\nTÃ¢che: <code>%s</code>\n", statusIcon, title, taskName)
		if err != nil {
			text += fmt.Sprintf("Erreur: <code>%s</code>\n", err.Error())
		}
		go n.sendTelegramGeneric(text)
	}
}

// NotifyBackupCreated alerte qu'une nouvelle sauvegarde a Ã©tÃ© gÃ©nÃ©rÃ©e.
func (n *Notifier) NotifyBackupCreated(fileName string, sizeBytes int64) {
	sizeMB := float64(sizeBytes) / (1024 * 1024)
	title := "ðŸ’¾ Sauvegarde terminÃ©e"
	desc := fmt.Sprintf("Une nouvelle sauvegarde de la base de donnÃ©es a Ã©tÃ© gÃ©nÃ©rÃ©e : **%s**", fileName)

	if n.cfg.Discord.URL != "" {
		go n.sendDiscordGeneric(title, desc, 3447003, map[string]string{
			"Version": "SQLite/Postgres",
			"Taille":  fmt.Sprintf("%.2f MB", sizeMB),
			"Fichier": fileName,
		})
	}

	if n.cfg.Telegram.Token != "" && n.cfg.Telegram.ChatID != "" {
		text := fmt.Sprintf("ðŸ’¾ <b>%s</b>\n\nFichier: <code>%s</code>\nTaille: %.2f MB", title, fileName, sizeMB)
		go n.sendTelegramGeneric(text)
	}
}

// NotifyAccessExpiry signale Ã  l'admin qu'un utilisateur va bientÃ´t expirer.
func (n *Notifier) NotifyAccessExpiry(username string, daysLeft int) {
	title := "â�³ Expiration imminente"
	desc := fmt.Sprintf("L'accÃ¨s de l'utilisateur **%s** arrive Ã  expiration dans **%d jours**.", username, daysLeft)

	if n.cfg.Discord.URL != "" {
		go n.sendDiscordGeneric(title, desc, 15105570, map[string]string{
			"Utilisateur": username,
			"Ã‰chÃ©ance":  fmt.Sprintf("%d jours", daysLeft),
		})
	}

	if n.cfg.Telegram.Token != "" && n.cfg.Telegram.ChatID != "" {
		text := fmt.Sprintf("â�³ <b>%s</b>\n\nUtilisateur: <code>%s</code>\nExpire dans %d jours.", title, username, daysLeft)
		go n.sendTelegramGeneric(text)
	}
}

// â”€â”€ Discord â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// sendDiscord envoie une notification via un webhook Discord.
//
// Format : Embed riche avec couleur verte et champs structurÃ©s.
// API : POST <webhook_url> avec Content-Type: application/json
func (n *Notifier) sendDiscord(event UserRegisteredEvent) {
	// Construire l'embed Discord
	payload := map[string]interface{}{
		"username":   "JellyGate",
		"avatar_url": "",
		"allowed_mentions": map[string]interface{}{
			"parse": []string{},
		},
		"embeds": []map[string]interface{}{
			{
				"title":       "ðŸŽ‰ Nouvel utilisateur inscrit",
				"description": fmt.Sprintf("**%s** vient de crÃ©er un compte via invitation.", discordText(event.DisplayName)),
				"color":       3066993, // Vert (#2ECC71)
				"fields": []map[string]interface{}{
					{"name": "ðŸ‘¤ Username", "value": fmt.Sprintf("`%s`", discordText(event.Username)), "inline": true},
					{"name": "ðŸ“§ Email", "value": discordText(n.maskOrEmpty(event.Email)), "inline": true},
					{"name": "ðŸŽ« Invitation", "value": fmt.Sprintf("`%s`", discordText(event.InviteCode)), "inline": true},
					{"name": "ðŸ‘¥ InvitÃ© par", "value": discordText(n.valueOrNA(event.InvitedBy)), "inline": true},
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
		slog.Error("Discord: erreur de sÃ©rialisation", "error", err)
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
		slog.Error("Discord: rÃ©ponse HTTP inattendue",
			"status", resp.StatusCode,
			"body", string(respBody),
		)
		return
	}

	slog.Info("Discord: notification envoyÃ©e", "username", event.Username)
}

// â”€â”€ Telegram â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// sendTelegram envoie une notification via l'API Bot Telegram.
//
// API : POST https://api.telegram.org/bot<token>/sendMessage
// Format : Message HTML avec mise en forme.
func (n *Notifier) sendTelegram(event UserRegisteredEvent) {
	text := fmt.Sprintf(
		"ðŸŽ‰ <b>Nouvel utilisateur inscrit</b>\n\n"+
			"ðŸ‘¤ Username: <code>%s</code>\n"+
			"ðŸ“� Nom: %s\n"+
			"ðŸ“§ Email: %s\n"+
			"ðŸŽ« Invitation: <code>%s</code>\n"+
			"ðŸ‘¥ InvitÃ© par: %s\n"+
			"ðŸ•� %s",
		htmlText(event.Username),
		htmlText(event.DisplayName),
		htmlText(n.maskOrEmpty(event.Email)),
		htmlText(event.InviteCode),
		htmlText(n.valueOrNA(event.InvitedBy)),
		event.Timestamp.Format("02/01/2006 15:04"),
	)

	payload := map[string]interface{}{
		"chat_id":    n.cfg.Telegram.ChatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("Telegram: erreur de sÃ©rialisation", "error", err)
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
		slog.Error("Telegram: rÃ©ponse HTTP inattendue",
			"status", resp.StatusCode,
			"body", string(respBody),
		)
		return
	}

	slog.Info("Telegram: notification envoyÃ©e", "username", event.Username, "chat_id", n.cfg.Telegram.ChatID)
}

// â”€â”€ Matrix â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// sendMatrix envoie une notification via l'API client-server Matrix.
//
// API : PUT /_matrix/client/v3/rooms/{roomId}/send/m.room.message/{txnId}
// Auth : Bearer token dans le header Authorization.
func (n *Notifier) sendMatrix(event UserRegisteredEvent) {
	// Construire le corps du message (format HTML)
	htmlBody := fmt.Sprintf(
		"<h3>ðŸŽ‰ Nouvel utilisateur inscrit</h3>"+
			"<ul>"+
			"<li><b>Username:</b> <code>%s</code></li>"+
			"<li><b>Nom:</b> %s</li>"+
			"<li><b>Email:</b> %s</li>"+
			"<li><b>Invitation:</b> <code>%s</code></li>"+
			"<li><b>InvitÃ© par:</b> %s</li>"+
			"</ul>",
		htmlText(event.Username),
		htmlText(event.DisplayName),
		htmlText(n.maskOrEmpty(event.Email)),
		htmlText(event.InviteCode),
		htmlText(n.valueOrNA(event.InvitedBy)),
	)

	plainBody := fmt.Sprintf(
		"ðŸŽ‰ Nouvel utilisateur inscrit\n"+
			"Username: %s\nNom: %s\nEmail: %s\nInvitation: %s\nInvitÃ© par: %s",
		discordText(event.Username), discordText(event.DisplayName),
		discordText(n.maskOrEmpty(event.Email)), discordText(event.InviteCode),
		discordText(n.valueOrNA(event.InvitedBy)),
	)

	payload := map[string]interface{}{
		"msgtype":        "m.text",
		"body":           plainBody,
		"format":         "org.matrix.custom.html",
		"formatted_body": htmlBody,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("Matrix: erreur de sÃ©rialisation", "error", err)
		return
	}

	// Transaction ID unique basÃ© sur le timestamp
	txnID := fmt.Sprintf("jg_%d", time.Now().UnixNano())

	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s",
		n.cfg.Matrix.URL, n.cfg.Matrix.RoomID, txnID)

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		slog.Error("Matrix: erreur de crÃ©ation de la requÃªte", "error", err)
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
		slog.Error("Matrix: rÃ©ponse HTTP inattendue",
			"status", resp.StatusCode,
			"body", string(respBody),
		)
		return
	}

	slog.Info("Matrix: notification envoyÃ©e", "username", event.Username, "room", n.cfg.Matrix.RoomID)
}

// â”€â”€ Generic Send Helpers â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

func (n *Notifier) sendDiscordGeneric(title, description string, color int, fields map[string]string) {
	discordFields := make([]map[string]interface{}, 0, len(fields))
	for k, v := range fields {
		discordFields = append(discordFields, map[string]interface{}{
			"name": k, "value": v, "inline": true,
		})
	}

	payload := map[string]interface{}{
		"username": "JellyGate",
		"embeds": []map[string]interface{}{
			{
				"title":       title,
				"description": description,
				"color":       color,
				"fields":      discordFields,
				"timestamp":   time.Now().Format(time.RFC3339),
				"footer":      map[string]string{"text": "JellyGate"},
			},
		},
	}

	body, _ := json.Marshal(payload)
	_, _ = n.client.Post(n.cfg.Discord.URL, "application/json", bytes.NewReader(body))
}

func (n *Notifier) sendTelegramGeneric(text string) {
	payload := map[string]interface{}{
		"chat_id":    n.cfg.Telegram.ChatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.cfg.Telegram.Token)
	_, _ = n.client.Post(url, "application/json", bytes.NewReader(body))
}

// â”€â”€ Utilitaires â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

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

// splitEmail sÃ©pare un email en [local, domain].
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
