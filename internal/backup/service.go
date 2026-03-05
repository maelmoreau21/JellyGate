package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/database"
)

var ErrSQLiteOnly = errors.New("fonction backup disponible uniquement en mode sqlite")

type BackupInfo struct {
	Name      string    `json:"name"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

type Service struct {
	dataDir    string
	backupDir  string
	restoreDir string
	db         *database.DB
	mu         sync.Mutex
}

func NewService(dataDir string, db *database.DB) *Service {
	return &Service{
		dataDir:    dataDir,
		backupDir:  filepath.Join(dataDir, "backups"),
		restoreDir: filepath.Join(dataDir, "restore"),
		db:         db,
	}
}

func (s *Service) SupportsSQLiteArchive() bool {
	return s != nil && s.db != nil && s.db.IsSQLite()
}

func (s *Service) requireSQLiteArchive(action string) error {
	if s.SupportsSQLiteArchive() {
		return nil
	}
	if s == nil || s.db == nil {
		return fmt.Errorf("%w: %s impossible (service indisponible)", ErrSQLiteOnly, action)
	}
	return fmt.Errorf("%w: %s non supporte avec %q (utiliser pg_dump/pg_restore)", ErrSQLiteOnly, action, s.db.Driver())
}

func (s *Service) ensureDirs() error {
	if err := os.MkdirAll(s.backupDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(s.restoreDir, 0755); err != nil {
		return err
	}
	return nil
}

func sanitizeReason(reason string) string {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return "manual"
	}
	reason = strings.ReplaceAll(reason, " ", "-")
	reason = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return -1
	}, reason)
	if reason == "" {
		return "manual"
	}
	return reason
}

func safeArchiveName(name string) (string, error) {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." || base == ".." {
		return "", fmt.Errorf("nom d'archive invalide")
	}
	if !strings.HasSuffix(strings.ToLower(base), ".zip") {
		return "", fmt.Errorf("archive invalide (extension .zip requise)")
	}
	if base != name && strings.TrimSpace(name) != base {
		return "", fmt.Errorf("nom d'archive invalide")
	}
	return base, nil
}

func (s *Service) CreateBackup(reason string) (BackupInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var info BackupInfo
	if err := s.ensureDirs(); err != nil {
		return info, fmt.Errorf("création des dossiers backup: %w", err)
	}

	now := time.Now()
	stamp := now.Format("20060102-150405")
	name := fmt.Sprintf("jellygate-%s-%s.zip", sanitizeReason(reason), stamp)
	path := filepath.Join(s.backupDir, name)
	snapshotPath := filepath.Join(s.restoreDir, fmt.Sprintf("snapshot-%s.db", stamp))
	defer os.Remove(snapshotPath)

	if err := s.requireSQLiteArchive("creation de sauvegarde"); err != nil {
		return info, err
	}

	quotedSnapshot := strings.ReplaceAll(snapshotPath, "'", "''")
	if _, err := s.db.Exec("VACUUM INTO '" + quotedSnapshot + "'"); err != nil {
		return info, fmt.Errorf("snapshot sqlite (VACUUM INTO): %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return info, fmt.Errorf("création archive: %w", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)

	dbBytes, err := os.ReadFile(snapshotPath)
	if err != nil {
		_ = zw.Close()
		return info, fmt.Errorf("lecture sqlite: %w", err)
	}
	if err := writeZipEntry(zw, "jellygate.db", dbBytes); err != nil {
		_ = zw.Close()
		return info, fmt.Errorf("ajout jellygate.db: %w", err)
	}

	settingsMap, err := s.db.GetAllSettings()
	if err != nil {
		_ = zw.Close()
		return info, fmt.Errorf("lecture settings: %w", err)
	}
	settingsJSON, _ := json.MarshalIndent(settingsMap, "", "  ")
	if err := writeZipEntry(zw, "settings.json", settingsJSON); err != nil {
		_ = zw.Close()
		return info, fmt.Errorf("ajout settings.json: %w", err)
	}

	meta := map[string]string{
		"created_at": now.Format(time.RFC3339),
		"reason":     sanitizeReason(reason),
		"version":    "0.1.0-alpha",
	}
	metaJSON, _ := json.MarshalIndent(meta, "", "  ")
	if err := writeZipEntry(zw, "metadata.json", metaJSON); err != nil {
		_ = zw.Close()
		return info, fmt.Errorf("ajout metadata.json: %w", err)
	}

	if err := zw.Close(); err != nil {
		return info, fmt.Errorf("finalisation archive: %w", err)
	}

	st, err := os.Stat(path)
	if err != nil {
		return info, fmt.Errorf("stat archive: %w", err)
	}

	return BackupInfo{Name: name, SizeBytes: st.Size(), CreatedAt: st.ModTime()}, nil
}

func writeZipEntry(zw *zip.Writer, name string, payload []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, bytes.NewReader(payload))
	return err
}

func (s *Service) ListBackups() ([]BackupInfo, error) {
	if err := s.ensureDirs(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(s.backupDir)
	if err != nil {
		return nil, err
	}

	list := make([]BackupInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".zip") {
			continue
		}
		st, err := entry.Info()
		if err != nil {
			continue
		}
		list = append(list, BackupInfo{Name: name, SizeBytes: st.Size(), CreatedAt: st.ModTime()})
	}

	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	return list, nil
}

func (s *Service) BackupPath(name string) (string, error) {
	safe, err := safeArchiveName(name)
	if err != nil {
		return "", err
	}
	path := filepath.Join(s.backupDir, safe)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("archive introuvable")
	}
	return path, nil
}

func (s *Service) DeleteBackup(name string) error {
	safe, err := safeArchiveName(name)
	if err != nil {
		return err
	}
	path := filepath.Join(s.backupDir, safe)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("archive introuvable")
		}
		return err
	}
	return nil
}

func (s *Service) ImportBackup(filename string, r io.Reader) (BackupInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var info BackupInfo
	if err := s.requireSQLiteArchive("import de sauvegarde"); err != nil {
		return info, err
	}
	if err := s.ensureDirs(); err != nil {
		return info, err
	}

	now := time.Now()
	ext := ".zip"
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(filename)), ".zip") {
		ext = ".zip"
	}
	name := fmt.Sprintf("jellygate-imported-%s%s", now.Format("20060102-150405"), ext)
	path := filepath.Join(s.backupDir, name)

	f, err := os.Create(path)
	if err != nil {
		return info, fmt.Errorf("création archive importée: %w", err)
	}
	if _, err := io.Copy(f, io.LimitReader(r, 512*1024*1024)); err != nil {
		f.Close()
		_ = os.Remove(path)
		return info, fmt.Errorf("écriture archive importée: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return info, fmt.Errorf("fermeture archive importée: %w", err)
	}

	if err := validateBackupArchive(path); err != nil {
		_ = os.Remove(path)
		return info, err
	}

	st, err := os.Stat(path)
	if err != nil {
		_ = os.Remove(path)
		return info, err
	}

	return BackupInfo{Name: name, SizeBytes: st.Size(), CreatedAt: st.ModTime()}, nil
}

func validateBackupArchive(path string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("archive invalide: %w", err)
	}
	defer zr.Close()

	hasDB := false
	for _, f := range zr.File {
		if strings.EqualFold(filepath.Base(f.Name), "jellygate.db") {
			hasDB = true
			break
		}
	}
	if !hasDB {
		return fmt.Errorf("archive invalide: fichier jellygate.db manquant")
	}
	return nil
}

func (s *Service) PrepareRestore(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.requireSQLiteArchive("restauration"); err != nil {
		return err
	}

	if err := s.ensureDirs(); err != nil {
		return err
	}

	archivePath, err := s.BackupPath(name)
	if err != nil {
		return err
	}

	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("ouverture archive: %w", err)
	}
	defer zr.Close()

	pendingDB := filepath.Join(s.restoreDir, "jellygate.db.pending")
	pendingMeta := filepath.Join(s.restoreDir, "restore.pending")
	_ = os.Remove(pendingDB)
	_ = os.Remove(pendingMeta)

	found := false
	for _, file := range zr.File {
		if !strings.EqualFold(filepath.Base(file.Name), "jellygate.db") {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return fmt.Errorf("lecture jellygate.db depuis archive: %w", err)
		}
		out, err := os.Create(pendingDB)
		if err != nil {
			rc.Close()
			return fmt.Errorf("création pending db: %w", err)
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return fmt.Errorf("copie pending db: %w", err)
		}
		out.Close()
		rc.Close()
		found = true
		break
	}

	if !found {
		return fmt.Errorf("archive invalide: fichier jellygate.db manquant")
	}

	marker := map[string]string{
		"backup_name": name,
		"prepared_at": time.Now().Format(time.RFC3339),
	}
	payload, _ := json.Marshal(marker)
	if err := os.WriteFile(pendingMeta, payload, 0644); err != nil {
		return fmt.Errorf("écriture restore marker: %w", err)
	}

	return nil
}

func ApplyPendingRestore(dataDir, dbType string) error {
	if strings.TrimSpace(strings.ToLower(dbType)) != database.DialectSQLite {
		return nil
	}

	restoreDir := filepath.Join(dataDir, "restore")
	pendingDB := filepath.Join(restoreDir, "jellygate.db.pending")
	if _, err := os.Stat(pendingDB); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	dbPath := filepath.Join(dataDir, "jellygate.db")
	if _, err := os.Stat(dbPath); err == nil {
		backupPath := filepath.Join(restoreDir, fmt.Sprintf("pre-restore-%s.db", time.Now().Format("20060102-150405")))
		if err := copyFile(dbPath, backupPath); err != nil {
			return fmt.Errorf("backup pré-restore: %w", err)
		}
		if err := os.Remove(dbPath); err != nil {
			return fmt.Errorf("suppression ancienne base: %w", err)
		}
	}

	if err := os.Rename(pendingDB, dbPath); err != nil {
		return fmt.Errorf("application restore pending: %w", err)
	}

	_ = os.Remove(filepath.Join(restoreDir, "restore.pending"))
	slog.Info("Restauration appliquée au démarrage", "db", dbPath)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func (s *Service) ApplyRetention(keep int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if keep < 1 {
		keep = 1
	}
	list, err := s.ListBackups()
	if err != nil {
		return err
	}
	if len(list) <= keep {
		return nil
	}

	for _, item := range list[keep:] {
		path := filepath.Join(s.backupDir, item.Name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.Warn("Impossible de supprimer une archive excédentaire", "name", item.Name, "error", err)
		}
	}
	return nil
}

func (s *Service) StartScheduler(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runScheduledTick()
			}
		}
	}()
}

func (s *Service) runScheduledTick() {
	if !s.SupportsSQLiteArchive() {
		return
	}

	cfg, err := s.db.GetBackupConfig()
	if err != nil {
		slog.Warn("Lecture config backup impossible", "error", err)
		return
	}
	if !cfg.Enabled {
		return
	}

	now := time.Now()
	if now.Hour() != cfg.Hour || now.Minute() != cfg.Minute {
		return
	}

	day := now.Format("2006-01-02")
	if s.db.GetBackupLastRun() == day {
		return
	}

	if _, err := s.CreateBackup("auto"); err != nil {
		slog.Error("Échec backup planifié", "error", err)
		return
	}
	if err := s.ApplyRetention(cfg.RetentionCount); err != nil {
		slog.Warn("Échec rétention backups", "error", err)
	}
	if err := s.db.SetBackupLastRun(day); err != nil {
		slog.Warn("Impossible d'enregistrer backup_last_run", "error", err)
	}
	_ = s.db.LogAction("backup.auto.created", "system", "", day)
	slog.Info("Backup planifié exécuté", "day", day)
}
