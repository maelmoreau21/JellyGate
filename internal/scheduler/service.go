package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/backup"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	"github.com/maelmoreau21/JellyGate/internal/mail"
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
	db     *database.DB
	jf     *jellyfin.Client
	backup *backup.Service
	mailer *mail.Mailer
	mu     sync.Mutex
}

func NewService(db *database.DB, jf *jellyfin.Client, backupSvc *backup.Service, mailer *mail.Mailer) *Service {
	return &Service{db: db, jf: jf, backup: backupSvc, mailer: mailer}
}

func (s *Service) SetMailer(m *mail.Mailer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mailer = m
}

func (s *Service) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		defer ticker.Stop()
		time.Sleep(7 * time.Second)
		s.runDueTasks()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runDueTasks()
			}
		}
	}()
}

func (s *Service) RunTaskNow(taskID int64) error {
	task, err := s.loadTask(taskID)
	if err != nil {
		return err
	}
	return s.executeTask(task)
}

func (s *Service) runDueTasks() {
	now := time.Now()
	rows, err := s.db.Query(
		`SELECT id, name, task_type, enabled, hour, minute, payload, last_run_at, created_by, created_at, updated_at
		 FROM scheduled_tasks
		 WHERE enabled = TRUE AND hour = ? AND minute = ?`,
		now.Hour(),
		now.Minute(),
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

		if task.LastRunAt != "" {
			if t, err := parseDateTime(task.LastRunAt); err == nil {
				if sameLocalDay(t, now) {
					continue
				}
			}
		}

		if err := s.executeTask(task); err != nil {
			slog.Error("Scheduler: execution tache echouee", "task", task.Name, "type", task.TaskType, "error", err)
			continue
		}
	}
}

func (s *Service) executeTask(task TaskRecord) error {
	now := time.Now().Format("2006-01-02 15:04:05")

	switch strings.TrimSpace(task.TaskType) {
	case "sync_users":
		if s.jf == nil {
			return fmt.Errorf("client Jellyfin indisponible")
		}
		jfUsers, err := s.jf.GetUsers()
		if err != nil {
			return err
		}
		added := 0
		for _, ju := range jfUsers {
			res, err := s.db.Exec(`INSERT OR IGNORE INTO users (jellyfin_id, username, is_active) VALUES (?, ?, ?)`, ju.ID, ju.Name, !ju.Policy.IsDisabled)
			if err != nil {
				continue
			}
			if n, _ := res.RowsAffected(); n > 0 {
				added++
			}
		}
		_ = s.db.LogAction("task.sync_users", "scheduler", task.Name, fmt.Sprintf("%d nouveaux utilisateurs", added))

	case "cleanup_resets":
		res, err := s.db.Exec(`DELETE FROM password_resets WHERE used = TRUE OR expires_at < datetime('now', '-24 hours')`)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		_ = s.db.LogAction("task.cleanup_resets", "scheduler", task.Name, fmt.Sprintf("%d tokens nettoyes", n))

	case "dispatch_campaigns":
		if err := s.dispatchCampaignMessages(); err != nil {
			return err
		}

	case "create_backup":
		if s.backup == nil {
			return fmt.Errorf("service backup indisponible")
		}
		if _, err := s.backup.CreateBackup("scheduled-task"); err != nil {
			return err
		}
		backupCfg, _ := s.db.GetBackupConfig()
		_ = s.backup.ApplyRetention(backupCfg.RetentionCount)
		_ = s.db.LogAction("task.create_backup", "scheduler", task.Name, "Sauvegarde executee")

	default:
		return fmt.Errorf("type de tache non supporte: %s", task.TaskType)
	}

	_, err := s.db.Exec(`UPDATE scheduled_tasks SET last_run_at = ?, updated_at = datetime('now') WHERE id = ?`, now, task.ID)
	return err
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
			_, _ = s.db.Exec(`UPDATE user_messages SET sent_at = datetime('now') WHERE id = ?`, c.id)
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

		_, _ = s.db.Exec(`UPDATE user_messages SET sent_at = datetime('now') WHERE id = ?`, c.id)
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
		if t, err := time.Parse(f, raw); err == nil {
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
