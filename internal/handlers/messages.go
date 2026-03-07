package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

type CreateMessageRequest struct {
	Title         string   `json:"title"`
	Body          string   `json:"body"`
	TargetGroup   string   `json:"target_group"`
	TargetUserIDs []int64  `json:"target_user_ids"`
	Channels      []string `json:"channels"`
	IsCampaign    bool     `json:"is_campaign"`
	StartsAt      string   `json:"starts_at"`
	EndsAt        string   `json:"ends_at"`
}

type MessageResponse struct {
	ID            int64    `json:"id"`
	Title         string   `json:"title"`
	Body          string   `json:"body"`
	CreatedBy     string   `json:"created_by"`
	TargetGroup   string   `json:"target_group"`
	TargetUserIDs []int64  `json:"target_user_ids"`
	Channels      []string `json:"channels"`
	IsCampaign    bool     `json:"is_campaign"`
	StartsAt      string   `json:"starts_at"`
	EndsAt        string   `json:"ends_at"`
	SentAt        string   `json:"sent_at"`
	CreatedAt     string   `json:"created_at"`
	Read          bool     `json:"read"`
	ReadAt        string   `json:"read_at"`
	ReadCount     int      `json:"read_count"`
}

type messageEmailRecipient struct {
	ID         int64
	Username   string
	Email      string
	IsActive   bool
	CanInvite  bool
	OptInEmail bool
}

func (h *AdminHandler) MessagesPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.AdminUsername = sess.Username
	td.IsAdmin = sess.IsAdmin
	td.CanInvite = h.resolveCanInviteForSession(sess)

	if err := h.renderer.Render(w, "admin/messages.html", td); err != nil {
		slog.Error("Erreur rendu messages page", "error", err)
		http.Error(w, "Erreur serveur : impossible de charger la page", http.StatusInternalServerError)
	}
}

func csvTargetUserIDs(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	if len(parts) == 0 {
		return ""
	}
	return "," + strings.Join(parts, ",") + ","
}

func parseTargetUserIDs(raw string) []int64 {
	raw = strings.Trim(raw, ",")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if id, err := strconv.ParseInt(p, 10, 64); err == nil {
			result = append(result, id)
		}
	}
	return result
}

func normalizeChannels(channels []string) string {
	if len(channels) == 0 {
		return "in_app"
	}
	seen := map[string]bool{}
	clean := make([]string, 0, len(channels))
	for _, c := range channels {
		c = strings.TrimSpace(strings.ToLower(c))
		if c == "" {
			continue
		}
		if c != "in_app" && c != "email" && c != "discord" && c != "telegram" {
			continue
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		clean = append(clean, c)
	}
	if len(clean) == 0 {
		return "in_app"
	}
	return strings.Join(clean, ",")
}

func splitChannels(raw string) []string {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		result = append(result, p)
	}
	if len(result) == 0 {
		return []string{"in_app"}
	}
	return result
}

func (h *AdminHandler) loadMessageEmailRecipients(targetGroup string, targetUserIDs []int64) ([]messageEmailRecipient, error) {
	targetUsersCSV := csvTargetUserIDs(targetUserIDs)
	rows, err := h.db.Query(`SELECT id, username, email, is_active, can_invite, opt_in_email FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	recipients := make([]messageEmailRecipient, 0)
	for rows.Next() {
		var rec messageEmailRecipient
		if err := rows.Scan(&rec.ID, &rec.Username, &rec.Email, &rec.IsActive, &rec.CanInvite, &rec.OptInEmail); err != nil {
			continue
		}
		if !rec.OptInEmail || strings.TrimSpace(rec.Email) == "" {
			continue
		}
		if !messageTargetsUser(targetGroup, targetUsersCSV, rec.ID, false, rec.CanInvite, rec.IsActive) {
			continue
		}
		recipients = append(recipients, rec)
	}

	return recipients, nil
}

func (h *AdminHandler) sendImmediateMessageEmails(recipients []messageEmailRecipient, title, body, actor string) int {
	if h.mailer == nil {
		return 0
	}

	sentCount := 0
	for _, rec := range recipients {
		err := h.mailer.SendTemplateString(rec.Email, title, body, map[string]string{
			"Username": rec.Username,
			"Email":    rec.Email,
			"Actor":    actor,
		})
		if err != nil {
			slog.Warn("envoi message utilisateur impossible", "user_id", rec.ID, "email", rec.Email, "error", err)
			continue
		}
		sentCount++
	}

	return sentCount
}

func (h *AdminHandler) CreateMessage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if sess == nil || !sess.IsAdmin {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Acces admin requis"})
		return
	}

	var req CreateMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}

	title := strings.TrimSpace(req.Title)
	body := strings.TrimSpace(req.Body)
	if title == "" || body == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Titre et contenu requis"})
		return
	}

	targetGroup := strings.TrimSpace(strings.ToLower(req.TargetGroup))
	if targetGroup == "" {
		targetGroup = "all"
	}

	var startsAt interface{}
	var endsAt interface{}
	if strings.TrimSpace(req.StartsAt) != "" {
		parsed, err := parseOptionalDateTime(req.StartsAt)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Date de debut invalide"})
			return
		}
		startsAt = parsed
	}
	if strings.TrimSpace(req.EndsAt) != "" {
		parsed, err := parseOptionalDateTime(req.EndsAt)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Date de fin invalide"})
			return
		}
		endsAt = parsed
	}

	channels := normalizeChannels(req.Channels)
	if !strings.Contains(channels, "email") {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Le canal email est requis"})
		return
	}
	if h.mailer == nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "SMTP non configuré"})
		return
	}

	var recipients []messageEmailRecipient
	if !req.IsCampaign {
		resolvedRecipients, resolveErr := h.loadMessageEmailRecipients(targetGroup, req.TargetUserIDs)
		if resolveErr != nil {
			writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur résolution destinataires"})
			return
		}
		recipients = resolvedRecipients
		if len(recipients) == 0 {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Aucun destinataire email valide"})
			return
		}
	}

	targetUsers := csvTargetUserIDs(req.TargetUserIDs)

	res, err := h.db.Exec(
		`INSERT INTO user_messages (title, body, created_by, target_group, target_user_ids, channels, is_campaign, starts_at, ends_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		title,
		body,
		sess.Username,
		targetGroup,
		targetUsers,
		channels,
		req.IsCampaign,
		startsAt,
		endsAt,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur creation message"})
		return
	}

	msgID, _ := res.LastInsertId()
	if !req.IsCampaign {
		sentCount := h.sendImmediateMessageEmails(recipients, title, body, sess.Username)
		if sentCount == 0 {
			_, _ = h.db.Exec(`DELETE FROM user_messages WHERE id = ?`, msgID)
			writeJSON(w, http.StatusBadGateway, APIResponse{Success: false, Message: "Aucun email n'a pu être envoyé"})
			return
		}
		_, _ = h.db.Exec(`UPDATE user_messages SET sent_at = datetime('now') WHERE id = ?`, msgID)
		_ = h.db.LogAction("message.sent", sess.Username, strconv.FormatInt(msgID, 10), fmt.Sprintf("%d emails envoyes", sentCount))
		writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Message envoyé"})
		return
	}

	_ = h.db.LogAction("message.created", sess.Username, strconv.FormatInt(msgID, 10), title)
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Message cree"})
}

func parseOptionalDateTime(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	formats := []string{"2006-01-02T15:04", time.RFC3339, "2006-01-02 15:04:05"}
	for _, f := range formats {
		if t, err := time.Parse(f, raw); err == nil {
			return t.Format("2006-01-02 15:04:05"), nil
		}
	}
	return "", fmt.Errorf("invalid datetime")
}

func (h *AdminHandler) DeleteMessage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if sess == nil || !sess.IsAdmin {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Acces admin requis"})
		return
	}

	msgID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID message invalide"})
		return
	}

	res, err := h.db.Exec(`DELETE FROM user_messages WHERE id = ?`, msgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Suppression impossible"})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: "Message introuvable"})
		return
	}

	_ = h.db.LogAction("message.deleted", sess.Username, strconv.FormatInt(msgID, 10), "")
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Message supprime"})
}

func (h *AdminHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	view := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("view")))
	if view == "" {
		view = "inbox"
	}

	if err := h.ensureUserRowForSession(sess); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Profil utilisateur indisponible"})
		return
	}

	userID, isActive, canInvite, err := h.loadLocalUserFlags(sess.UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur lecture utilisateur"})
		return
	}

	readMap := map[int64]string{}
	rowsRead, err := h.db.Query(`SELECT message_id, read_at FROM user_message_reads WHERE user_id = ?`, userID)
	if err == nil {
		for rowsRead.Next() {
			var msgID int64
			var readAt string
			if err := rowsRead.Scan(&msgID, &readAt); err == nil {
				readMap[msgID] = readAt
			}
		}
		rowsRead.Close()
	}

	readCountMap := map[int64]int{}
	if sess.IsAdmin && view == "admin" {
		rowsAgg, err := h.db.Query(`SELECT message_id, COUNT(*) FROM user_message_reads GROUP BY message_id`)
		if err == nil {
			for rowsAgg.Next() {
				var msgID int64
				var count int
				if err := rowsAgg.Scan(&msgID, &count); err == nil {
					readCountMap[msgID] = count
				}
			}
			rowsAgg.Close()
		}
	}

	rows, err := h.db.Query(
		`SELECT id, title, body, created_by, target_group, target_user_ids, channels, is_campaign,
		        starts_at, ends_at, sent_at, created_at
		 FROM user_messages
		 WHERE (starts_at IS NULL OR starts_at <= datetime('now'))
		   AND (ends_at IS NULL OR ends_at >= datetime('now'))
		 ORDER BY created_at DESC
		 LIMIT 300`,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur lecture messages"})
		return
	}
	defer rows.Close()

	messages := make([]MessageResponse, 0)
	for rows.Next() {
		var msg MessageResponse
		var targetIDs, channels string
		if err := rows.Scan(
			&msg.ID,
			&msg.Title,
			&msg.Body,
			&msg.CreatedBy,
			&msg.TargetGroup,
			&targetIDs,
			&channels,
			&msg.IsCampaign,
			&msg.StartsAt,
			&msg.EndsAt,
			&msg.SentAt,
			&msg.CreatedAt,
		); err != nil {
			continue
		}

		msg.TargetUserIDs = parseTargetUserIDs(targetIDs)
		msg.Channels = splitChannels(channels)

		if view != "admin" || !sess.IsAdmin {
			if !messageTargetsUser(msg.TargetGroup, targetIDs, userID, sess.IsAdmin, canInvite, isActive) {
				continue
			}
		}

		if readAt, ok := readMap[msg.ID]; ok {
			msg.Read = true
			msg.ReadAt = readAt
		}
		msg.ReadCount = readCountMap[msg.ID]
		messages = append(messages, msg)
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: messages})
}

func messageTargetsUser(targetGroup, targetUserIDs string, userID int64, isAdmin, canInvite, isActive bool) bool {
	if strings.Contains(targetUserIDs, fmt.Sprintf(",%d,", userID)) {
		return true
	}

	switch strings.TrimSpace(strings.ToLower(targetGroup)) {
	case "", "all":
		return true
	case "admins":
		return isAdmin
	case "inviters":
		return canInvite
	case "active":
		return isActive
	case "inactive":
		return !isActive
	default:
		return false
	}
}

func (h *AdminHandler) loadLocalUserFlags(jellyfinID string) (int64, bool, bool, error) {
	var (
		userID    int64
		isActive  bool
		canInvite bool
	)
	err := h.db.QueryRow(
		`SELECT id, is_active, can_invite FROM users WHERE jellyfin_id = ?`,
		jellyfinID,
	).Scan(&userID, &isActive, &canInvite)
	if err != nil {
		return 0, false, false, err
	}
	return userID, isActive, canInvite, nil
}

func (h *AdminHandler) MarkMessageRead(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	msgID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID message invalide"})
		return
	}

	if err := h.ensureUserRowForSession(sess); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Profil utilisateur indisponible"})
		return
	}

	userID, isActive, canInvite, err := h.loadLocalUserFlags(sess.UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Profil local introuvable"})
		return
	}

	var targetGroup, targetUserIDs string
	err = h.db.QueryRow(`SELECT target_group, target_user_ids FROM user_messages WHERE id = ?`, msgID).Scan(&targetGroup, &targetUserIDs)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: "Message introuvable"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur lecture message"})
		return
	}

	if !messageTargetsUser(targetGroup, targetUserIDs, userID, sess.IsAdmin, canInvite, isActive) {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Message non accessible"})
		return
	}

	_, err = h.db.Exec(
		`INSERT INTO user_message_reads (message_id, user_id, read_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(message_id, user_id) DO UPDATE SET read_at = excluded.read_at`,
		msgID,
		userID,
		time.Now().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Impossible de marquer le message"})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Message marque comme lu"})
}
