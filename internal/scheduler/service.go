package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/authentik"
	"github.com/maelmoreau21/JellyGate/internal/backup"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/mail"
	"github.com/maelmoreau21/JellyGate/internal/notify"
	"github.com/maelmoreau21/JellyGate/internal/syslog"
)

type TaskRecord struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	TaskType  string `json:"task_type"`
	Enabled   bool   `json:"enabled"`
	Hour      int    `json:"hour"`
	Minute    int    `json:"minute"`
	Payload   string `json:"payload"`
	LastRunAt string `json:"last_run_at"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Service struct {
	db         *database.DB
	backup     *backup.Service
	mailer     *mail.Mailer
	notifier   *notify.Notifier
	authClient authentik.Client
	mu         sync.Mutex
}

func NewService(db *database.DB, backupSvc *backup.Service, mailer *mail.Mailer, notifier *notify.Notifier) *Service {
	return &Service{db: db, backup: backupSvc, mailer: mailer, notifier: notifier}
}

func (s *Service) SetMailer(m *mail.Mailer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mailer = m
}

func (s *Service) SetAuthentikClient(auth authentik.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authClient = auth
}

func (s *Service) getEffectiveAuthentikClient() authentik.Client {
	s.mu.Lock()
	client := s.authClient
	s.mu.Unlock()
	if client != nil {
		return client
	}
	if s.db != nil {
		if dbCfg, err := s.db.GetAuthentikConfig(); err == nil && (dbCfg.URL != "" || dbCfg.IssuerURL != "") {
			return authentik.NewClient(dbCfg)
		}
	}
	return nil
}

func (s *Service) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		defer ticker.Stop()
		time.Sleep(7 * time.Second)
		s.runDueTasks()
		s.runDailyInternalCleanup(time.Now())

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runDueTasks()
				s.runDailyInternalCleanup(time.Now())
			}
		}
	}()
}

func (s *Service) runDailyInternalCleanup(now time.Time) {
	todayStr := now.Format("2006-01-02")
	lastRun, err := s.db.GetSetting("daily_check_last_run")
	if err == nil && lastRun == todayStr {
		return
	}

	slog.Info("Scheduler: execution de checkExpiringAccounts quotidien", "date", todayStr)
	s.checkExpiringAccounts()

	// Réconciliation quotidienne Authentik <-> JellyGate
	if client := s.getEffectiveAuthentikClient(); client != nil {
		recreated, cleaned, rErr := s.ReconcileAuthentik(context.Background())
		if rErr != nil {
			slog.Warn("Scheduler: réconciliation Authentik quotidienne", "error", rErr)
		} else if recreated > 0 || cleaned > 0 {
			slog.Info("Scheduler: réconciliation Authentik quotidienne terminée", "recreated", recreated, "cleaned", cleaned)
		}
	}

	// Purge automatique des anciens logs système
	if mgr := syslog.GetManager(); mgr != nil {
		retentionDays := s.db.GetLogRetentionDays()
		purged, err := mgr.PurgeOldLogs(retentionDays)
		if err != nil {
			slog.Warn("Scheduler: erreur lors de la purge des anciens logs", "error", err)
		} else if purged > 0 {
			slog.Info("Scheduler: purge des anciens fichiers de log terminée", "purged", purged, "retention_days", retentionDays)
		}
	}

	_ = s.db.SetSetting("daily_check_last_run", todayStr)
}

func (s *Service) RunTaskNow(taskID int64) error {
	task, err := s.loadTask(taskID)
	if err != nil {
		return err
	}
	return s.executeTask(task)
}

func (s *Service) runDueTasks() {
	if err := s.cleanupClosedInvitations(); err != nil {
		slog.Warn("Scheduler: nettoyage des invitations fermees impossible", "error", err)
	}

	now := time.Now()
	rows, err := s.db.Query(
		`SELECT id, name, task_type, enabled, hour, minute, payload, last_run_at, created_by, created_at, updated_at
		 FROM scheduled_tasks
		 WHERE enabled = TRUE`,
	)
	if err != nil {
		slog.Error("Scheduler: lecture des taches impossible", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			continue
		}

		scheduledToday := time.Date(now.Year(), now.Month(), now.Day(), task.Hour, task.Minute, 0, 0, now.Location())
		if now.Before(scheduledToday) {
			continue
		}

		if task.LastRunAt != "" {
			if t, err := parseDateTime(task.LastRunAt); err == nil {
				if sameLocalDay(t, now) || t.After(scheduledToday) {
					continue
				}
			}
		}

		if err := s.executeTask(task); err != nil {
			slog.Error("Scheduler: execution tache echouee", "task", task.Name, "type", task.TaskType, "error", err)
			if s.notifier != nil {
				s.notifier.NotifyTaskExecuted(task.Name, false, err)
			}
			continue
		} else {
			if s.notifier != nil {
				s.notifier.NotifyTaskExecuted(task.Name, true, nil)
			}
		}
	}
}

func (s *Service) cleanupClosedInvitations() error {
	inviteCfg, err := s.db.GetInvitationProfileConfig()
	if err != nil {
		return err
	}
	if !inviteCfg.AutoDeleteClosedLinks {
		return nil
	}

	deleted, err := s.db.DeleteClosedInvitations(time.Now())
	if err != nil {
		return err
	}
	if deleted > 0 {
		_ = s.db.LogAction("invite.cleanup", "scheduler", "invitations", fmt.Sprintf("%d lien(s) fermes supprimes", deleted))
	}
	return nil
}

// ReconcileAuthentik vérifie et synchronise l'état des invitations entre JellyGate et Authentik.
// Si une invitation active dans JellyGate n'a pas de stage token sur Authentik (ou si celui-ci a été supprimé),
// elle est automatiquement recréée avec la même date d'expiration et les métadonnées de parrainage.
// Les invitations expirées ou épuisées sont nettoyées dans Authentik.
func (s *Service) ReconcileAuthentik(ctx context.Context) (recreated int, cleaned int, err error) {
	client := s.getEffectiveAuthentikClient()
	if client == nil {
		return 0, 0, fmt.Errorf("client Authentik non configuré")
	}

	authCfg, _ := s.db.GetAuthentikConfig()
	flowSlug := strings.TrimSpace(authCfg.EnrollmentFlowSlug)
	if flowSlug == "" {
		flowSlug = "default-enrollment-flow"
	}

	// 1. Lister les invitations Authentik actuelles
	tokens, err := client.ListInvitationStageTokens(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("impossible de lister les tokens Authentik: %w", err)
	}

	tokenByPK := make(map[string]authentik.InvitationTokenResponse, len(tokens))
	tokenByCode := make(map[string]authentik.InvitationTokenResponse, len(tokens))
	for _, tok := range tokens {
		if tok.PK != "" {
			tokenByPK[tok.PK] = tok
		}
		if tok.FixedData != nil {
			if c, ok := tok.FixedData["code"].(string); ok && strings.TrimSpace(c) != "" {
				tokenByCode[strings.TrimSpace(c)] = tok
			} else if c, ok := tok.FixedData["invitation_code"].(string); ok && strings.TrimSpace(c) != "" {
				tokenByCode[strings.TrimSpace(c)] = tok
			}
		}
	}

	// 2. Charger toutes les invitations de JellyGate
	rows, err := s.db.Query(`
		SELECT id, code, label, max_uses, used_count, jellyfin_profile, expires_at, created_by, authentik_invitation_id, profile_id, is_temporary, account_duration_days
		FROM invitations
	`)
	if err != nil {
		return 0, 0, fmt.Errorf("lecture invitations JellyGate: %w", err)
	}
	defer rows.Close()

	now := time.Now()

	for rows.Next() {
		var id int64
		var code, label, jfProfile, createdBy, profileID string
		var maxUses, usedCount, accountDurationDays int
		var expiresAt sql.NullTime
		var authentikID sql.NullString
		var isTemporary bool

		if err := rows.Scan(&id, &code, &label, &maxUses, &usedCount, &jfProfile, &expiresAt, &createdBy, &authentikID, &profileID, &isTemporary, &accountDurationDays); err != nil {
			continue
		}

		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}

		isExpired := expiresAt.Valid && expiresAt.Time.Before(now)
		isExhausted := maxUses > 0 && usedCount >= maxUses
		isActive := !isExpired && !isExhausted

		curAuthID := strings.TrimSpace(authentikID.String)

		if isActive {
			tokenExists := false
			if curAuthID != "" {
				if _, ok := tokenByPK[curAuthID]; ok {
					tokenExists = true
				}
			}
			if !tokenExists {
				if tok, ok := tokenByCode[code]; ok && tok.PK != "" {
					tokenExists = true
					_, _ = s.db.Exec(`UPDATE invitations SET authentik_invitation_id = ? WHERE id = ?`, tok.PK, id)
				}
			}

			// Si le token n'existe pas dans Authentik, le recréer automatiquement
			if !tokenExists {
				var targetGroups []string
				jfGroup := strings.TrimSpace(authCfg.JellyfinUserGroup)
				if jfGroup == "" {
					jfGroup = "jellyfin-users"
				}
				targetGroups = append(targetGroups, jfGroup)

				fixedData := map[string]interface{}{
					"source":                "JellyGate",
					"created_by":            "JellyGate",
					"invitation_code":       code,
					"code":                  code,
					"sponsor":               createdBy,
					"groups":                targetGroups,
					"preset_id":             profileID,
					"is_temporary":          isTemporary,
					"account_duration_days": accountDurationDays,
				}

				var stageExpiry time.Time
				if expiresAt.Valid {
					stageExpiry = expiresAt.Time
				}

				tokenName := fmt.Sprintf("jellygate-%s", code)
				newTokPK, tokErr := client.CreateInvitationStageToken(ctx, tokenName, stageExpiry, fixedData, maxUses == 1, flowSlug)
				if tokErr == nil && newTokPK != "" {
					_, _ = s.db.Exec(`UPDATE invitations SET authentik_invitation_id = ? WHERE id = ?`, newTokPK, id)
					_ = s.db.LogAction("invite.reconciled", "scheduler", code, fmt.Sprintf("Jeton Authentik recréé avec succès (PK: %s)", newTokPK))
					slog.Info("Scheduler: invitation Authentik recréée avec succès", "code", code, "pk", newTokPK)
					recreated++
				} else {
					slog.Warn("Scheduler: échec recréation token Authentik", "code", code, "error", tokErr)
				}
			}
		} else {
			// L'invitation est expirée ou épuisée : nettoyer dans Authentik
			if curAuthID != "" {
				if _, ok := tokenByPK[curAuthID]; ok {
					if delErr := client.DeleteInvitationStageToken(ctx, curAuthID); delErr == nil {
						_ = s.db.LogAction("invite.authentik_cleanup", "scheduler", code, fmt.Sprintf("Jeton Authentik expiré supprimé (PK: %s)", curAuthID))
						cleaned++
					}
				}
			}
		}
	}

	return recreated, cleaned, nil
}

func (s *Service) executeTask(task TaskRecord) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	_ = now

	switch strings.TrimSpace(task.TaskType) {
	case "sync_authentik":
		recreated, cleaned, syncErr := s.ReconcileAuthentik(context.Background())
		if syncErr != nil {
			return syncErr
		}
		_ = s.db.LogAction("task.sync_authentik", "scheduler", task.Name, fmt.Sprintf("Réconciliation Authentik terminée (%d recréée(s), %d nettoyée(s))", recreated, cleaned))

	case "sync_users":
		if client := s.getEffectiveAuthentikClient(); client != nil {
			_, _, _ = s.ReconcileAuthentik(context.Background())
		}
		_ = s.db.LogAction("task.sync_users", "scheduler", task.Name, "Synchronisation des utilisateurs exécutée")

	case "cleanup_resets":
		_ = s.db.LogAction("task.cleanup_resets", "scheduler", task.Name, "Nettoyage jetons exécuté")

	case "create_backup":
		if s.backup == nil {
			return fmt.Errorf("service de backup indisponible")
		}
		info, err := s.backup.CreateBackup("scheduled")
		if err != nil {
			return err
		}
		_ = s.db.LogAction("task.create_backup", "scheduler", task.Name, fmt.Sprintf("Sauvegarde créée: %s", info.Name))

	case "send_broadcast":
		payload := strings.TrimSpace(task.Payload)
		if payload == "" {
			return fmt.Errorf("payload de message vide pour broadcast")
		}
		var msg struct {
			Title          string   `json:"title"`
			Content        string   `json:"content"`
			Type           string   `json:"type"`
			TargetAudience string   `json:"target_audience"`
			TargetPresetID string   `json:"target_preset_id"`
			Channels       []string `json:"channels"`
		}
		if err := json.Unmarshal([]byte(payload), &msg); err != nil {
			return fmt.Errorf("payload JSON invalide: %w", err)
		}
		targetPresetID := strings.TrimSpace(strings.ToLower(msg.TargetPresetID))

		rows, err := s.db.Query(`SELECT username, email, is_active, can_invite, COALESCE(preset_id, '') FROM users`)
		if err != nil {
			return err
		}
		defer rows.Close()

		sentCount := 0
		for rows.Next() {
			var username, email, userPresetID string
			var isActive, canInvite bool
			if err := rows.Scan(&username, &email, &isActive, &canInvite, &userPresetID); err != nil {
				continue
			}
			if !matchesAudience(msg.TargetAudience, isActive, canInvite) {
				continue
			}
			if targetPresetID != "" && !strings.EqualFold(strings.TrimSpace(userPresetID), targetPresetID) {
				continue
			}

			_, _ = s.db.Exec(`INSERT INTO user_messages (username, title, content, type, target_preset_id, created_at) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`, username, msg.Title, msg.Content, msg.Type, targetPresetID)
			sentCount++
		}
		_ = s.db.LogAction("task.send_broadcast", "scheduler", task.Name, fmt.Sprintf("Message diffusé à %d utilisateur(s)", sentCount))

	default:
		slog.Warn("Scheduler: type de tâche inconnu ou désactivé", "type", task.TaskType)
	}

	_, _ = s.db.Exec(`UPDATE scheduled_tasks SET last_run_at = ?, updated_at = ? WHERE id = ?`, now, now, task.ID)
	return nil
}

func matchesAudience(targetAudience string, isActive, canInvite bool) bool {
	switch strings.ToLower(strings.TrimSpace(targetAudience)) {
	case "active", "active_users":
		return isActive
	case "inactive", "inactive_users":
		return !isActive
	case "sponsors", "inviters":
		return canInvite
	default:
		return true
	}
}

func (s *Service) dispatchCampaignMessages() error {
	rows, err := s.db.Query(
		`SELECT id, title, body, target_group, target_user_ids, channels
		 FROM user_messages
		 WHERE is_campaign = TRUE
		   AND sent_at IS NULL
		   AND (starts_at IS NULL OR starts_at <= datetime('now'))
		 ORDER BY created_at ASC`,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	type campaign struct {
		id            int64
		title         string
		body          string
		targetGroup   string
		targetUserIDs string
		channels      string
	}

	campaigns := make([]campaign, 0)
	for rows.Next() {
		var c campaign
		if err := rows.Scan(&c.id, &c.title, &c.body, &c.targetGroup, &c.targetUserIDs, &c.channels); err == nil {
			campaigns = append(campaigns, c)
		}
	}

	for _, c := range campaigns {
		if !strings.Contains(strings.ToLower(c.channels), "email") || s.mailer == nil {
			_, _ = s.db.Exec(`UPDATE user_messages SET sent_at = CURRENT_TIMESTAMP WHERE id = ?`, c.id)
			continue
		}

		users, err := s.loadUsersForCampaign()
		if err != nil {
			continue
		}

		sentCount := 0
		for _, u := range users {
			if !matchTarget(c.targetGroup, c.targetUserIDs, u.id, u.isAdmin, u.canInvite, u.isActive) {
				continue
			}
			if !u.optInEmail || strings.TrimSpace(u.email) == "" {
				continue
			}

			err := s.mailer.SendTemplateString(u.email, c.title, c.body, map[string]string{
				"Username": u.username,
				"Email":    u.email,
			})
			if err != nil {
				continue
			}
			sentCount++
		}

		_, _ = s.db.Exec(`UPDATE user_messages SET sent_at = CURRENT_TIMESTAMP WHERE id = ?`, c.id)
		_ = s.db.LogAction("task.dispatch_campaigns", "scheduler", strconv.FormatInt(c.id, 10), fmt.Sprintf("%d emails envoyes", sentCount))
	}

	return nil
}

type campaignUser struct {
	id         int64
	username   string
	email      string
	isActive   bool
	isAdmin    bool
	canInvite  bool
	optInEmail bool
}

func (s *Service) loadUsersForCampaign() ([]campaignUser, error) {
	rows, err := s.db.Query(`SELECT id, username, email, is_active, can_invite, opt_in_email FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]campaignUser, 0)
	for rows.Next() {
		var u campaignUser
		var email sql.NullString
		if err := rows.Scan(&u.id, &u.username, &email, &u.isActive, &u.canInvite, &u.optInEmail); err != nil {
			continue
		}
		u.email = email.String
		u.isAdmin = strings.EqualFold(strings.TrimSpace(u.username), "admin")
		list = append(list, u)
	}
	return list, nil
}

func matchTarget(group, targetUserIDs string, userID int64, isAdmin, canInvite, isActive bool) bool {
	group = strings.TrimSpace(strings.ToLower(group))
	if group == "" || group == "all" {
		return true
	}

	if strings.Contains(targetUserIDs, fmt.Sprintf(",%d,", userID)) {
		return true
	}

	switch group {
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

func scanTask(scanner interface {
	Scan(dest ...interface{}) error
}) (TaskRecord, error) {
	var t TaskRecord
	err := scanner.Scan(&t.ID, &t.Name, &t.TaskType, &t.Enabled, &t.Hour, &t.Minute, &t.Payload, &t.LastRunAt, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

func parseDateTime(raw string) (time.Time, error) {
	formats := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"}
	for _, f := range formats {
		if t, err := time.ParseInLocation(f, raw, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("format invalide")
}

func sameLocalDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func ParseTaskPayloadDelayMinutes(payload string) int {
	v := strings.TrimSpace(payload)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return int(math.Max(float64(n), 0))
}

func (s *Service) loadTask(taskID int64) (TaskRecord, error) {
	row := s.db.QueryRow(
		`SELECT id, name, task_type, enabled, hour, minute, payload, last_run_at, created_by, created_at, updated_at
		 FROM scheduled_tasks WHERE id = ?`,
		taskID,
	)
	t, err := scanTask(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return t, fmt.Errorf("tache introuvable")
		}
		return t, err
	}
	return t, nil
}

func (s *Service) checkExpiringAccounts() {
	if s.notifier == nil {
		return
	}

	targetDate := time.Now().AddDate(0, 0, 2).Format("2006-01-02")

	rows, err := s.db.Query(`
		SELECT username, access_expires_at 
		FROM users 
		WHERE is_active = TRUE 
		  AND access_expires_at IS NOT NULL
	`)
	if err != nil {
		slog.Error("Scheduler: erreur checkExpiringAccounts", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var username, expiryStr string
		if err := rows.Scan(&username, &expiryStr); err != nil {
			continue
		}
		expiryStr = strings.TrimSpace(expiryStr)
		if strings.HasPrefix(expiryStr, targetDate) {
			slog.Info("Scheduler: envoi notification expiration", "user", username, "expiry", expiryStr)
			s.notifier.NotifyAccessExpiry(username, 2)
		}
	}
}
