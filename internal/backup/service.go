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
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/notify"
)

var ErrSQLiteOnly = errors.New("fonction backup disponible uniquement en mode sqlite")

const (
	MaxArchiveBytes             int64  = 512 * 1024 * 1024
	maxArchiveEntries                  = 20
	maxArchiveUncompressedBytes uint64 = 2 * 1024 * 1024 * 1024
)

var backupStampPattern = regexp.MustCompile(`^\d{8}-\d{6}$`)

type BackupInfo struct {
	Name         string    `json:"name"`
	SizeBytes    int64     `json:"size_bytes"`
	CreatedAt    time.Time `json:"created_at"`
	Reason       string    `json:"reason"`
	Source       string    `json:"source"`
	DisplayLabel string    `json:"display_label"`
	IsLegacyName bool      `json:"is_legacy_name"`
}

type Service struct {
	dataDir    string
	backupDir  string
	restoreDir string
	db         *database.DB
	notifier   *notify.Notifier
	mu         sync.Mutex
}

func (s *Service) SetNotifier(n *notify.Notifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifier = n
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
	return fmt.Errorf("%w: %s non supporte avec %q", ErrSQLiteOnly, action, s.db.Driver())
}

func (s *Service) ensureDirs() error {
	if err := os.MkdirAll(s.backupDir, 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(s.restoreDir, 0700); err != nil {
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

func classifyBackupReason(reason string) (source, label string, legacy bool) {
	switch sanitizeReason(reason) {
	case "auto":
		return "settings", "Daily automatic", false
	case "automation":
		return "automation", "Advanced automation", false
	case "scheduled-task":
		return "automation", "Advanced automation", true
	case "manual":
		return "manual", "Manual", false
	case "imported", "import":
		return "imported", "Imported", false
	default:
		return "unknown", "Backup", false
	}
}

func inferBackupReasonFromName(name string) (string, bool) {
	lower := strings.TrimSuffix(strings.ToLower(filepath.Base(strings.TrimSpace(name))), ".zip")
	if strings.HasPrefix(lower, "jellygate-imported-") {
		return "imported", false
	}
	if !strings.HasPrefix(lower, "jellygate-") || len(lower) <= len("jellygate-")+16 {
		return "manual", false
	}

	stamp := lower[len(lower)-15:]
	if !backupStampPattern.MatchString(stamp) || lower[len(lower)-16] != '-' {
		return "manual", false
	}

	reason := lower[len("jellygate-") : len(lower)-16]
	if reason == "" {
		return "manual", false
	}
	return sanitizeReason(reason), reason == "scheduled-task"
}

func readBackupMetadata(path string) map[string]string {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil
	}
	defer zr.Close()

	for _, file := range zr.File {
		if !strings.EqualFold(filepath.Base(file.Name), "metadata.json") || file.FileInfo().IsDir() {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil
		}
		defer rc.Close()

		payload, err := io.ReadAll(io.LimitReader(rc, 64*1024))
		if err != nil {
			return nil
		}
		var meta map[string]string
		if err := json.Unmarshal(payload, &meta); err != nil {
			return nil
		}
		return meta
	}
	return nil
}

func backupInfoFromFile(path, name string, st os.FileInfo) BackupInfo {
	reason, legacyFromName := inferBackupReasonFromName(name)
	if strings.HasPrefix(strings.ToLower(name), "jellygate-imported-") {
		reason = "imported"
	} else if meta := readBackupMetadata(path); meta != nil {
		if metaReason := strings.TrimSpace(meta["reason"]); metaReason != "" {
			reason = sanitizeReason(metaReason)
		}
	}

	source, label, legacyFromReason := classifyBackupReason(reason)
	return BackupInfo{
		Name:         name,
		SizeBytes:    st.Size(),
		CreatedAt:    st.ModTime(),
		Reason:       sanitizeReason(reason),
		Source:       source,
		DisplayLabel: label,
		IsLegacyName: legacyFromName || legacyFromReason,
	}
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
	if s == nil || s.db == nil {
		return info, fmt.Errorf("service backup indisponible")
	}
	if err := s.ensureDirs(); err != nil {
		return info, fmt.Errorf("création des dossiers backup: %w", err)
	}

	now := time.Now()
	stamp := now.Format("20060102-150405")
	name := fmt.Sprintf("jellygate-%s-%s.zip", sanitizeReason(reason), stamp)
	path := filepath.Join(s.backupDir, name)

	var dumpPath string
	var zipFileName string

	if s.db.IsSQLite() {
		dumpPath = filepath.Join(s.restoreDir, fmt.Sprintf("snapshot-%s.db", stamp))
		defer os.Remove(dumpPath)

		quotedSnapshot := strings.ReplaceAll(dumpPath, "'", "''")
		if _, err := s.db.Exec("VACUUM INTO '" + quotedSnapshot + "'"); err != nil {
			return info, fmt.Errorf("snapshot sqlite (VACUUM INTO): %w", err)
		}
		zipFileName = "jellygate.db"
	} else if s.db.IsPostgres() {
		dumpPath = filepath.Join(s.restoreDir, fmt.Sprintf("snapshot-%s.sql", stamp))
		defer os.Remove(dumpPath)

		cfg := s.db.GetConfig()
		port := cfg.Port
		if port <= 0 {
			port = 5432
		}

		_ = os.Setenv("PGPASSWORD", cfg.Password)
		defer os.Unsetenv("PGPASSWORD")

		cmd := exec.Command( // #nosec G204 -- arguments are passed without a shell and come from database configuration.
			"pg_dump",
			"-h", cfg.Host,
			"-p", strconv.Itoa(port),
			"-U", cfg.User,
			"-d", cfg.Name,
			"--clean",
			"--if-exists",
			"--no-owner",
			"--no-privileges",
			"-F", "p",
			"-f", dumpPath,
		)
		cmdOutput, err := cmd.CombinedOutput()
		if err != nil {
			return info, fmt.Errorf("dump postgresql echoue: %w. En mode Docker, 'pg_dump' est inclus dans l'image JellyGate (reconstruisez l'image si la version majeure PostgreSQL ne correspond pas). Hors Docker, verifiez que 'pg_dump' est installe et present dans votre PATH. (output: %s)", err, string(cmdOutput))
		}
		zipFileName = "jellygate.sql"
	} else {
		return info, fmt.Errorf("driver de base non supporte pour backup: %q", s.db.Driver())
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return info, fmt.Errorf("création archive: %w", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)

	dbBytes, err := os.ReadFile(dumpPath)
	if err != nil {
		_ = zw.Close()
		return info, fmt.Errorf("lecture base de versement: %w", err)
	}
	if err := writeZipEntry(zw, zipFileName, dbBytes); err != nil {
		_ = zw.Close()
		return info, fmt.Errorf("ajout fichier db compressé: %w", err)
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
		"created_at":      now.Format(time.RFC3339),
		"reason":          sanitizeReason(reason),
		"version":         config.AppVersion,
		"database_driver": s.db.Driver(),
		"archive_entry":   zipFileName,
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

	info = backupInfoFromFile(path, name, st)
	if s.notifier != nil {
		s.notifier.NotifyBackupCreated(info.Name, info.SizeBytes)
	}

	return info, nil
}

func writeZipEntry(zw *zip.Writer, name string, payload []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, bytes.NewReader(payload))
	return err
}

func extractBackupEntry(archivePath, entryName, outputPath string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("ouverture archive: %w", err)
	}
	defer zr.Close()

	for _, file := range zr.File {
		if !strings.EqualFold(filepath.Base(file.Name), entryName) {
			continue
		}
		if file.FileInfo().IsDir() {
			return fmt.Errorf("archive invalide: %s est un dossier", entryName)
		}
		if file.UncompressedSize64 > maxArchiveUncompressedBytes {
			return fmt.Errorf("archive invalide: fichier %s trop volumineux", entryName)
		}

		rc, err := file.Open()
		if err != nil {
			return fmt.Errorf("lecture %s depuis archive: %w", entryName, err)
		}
		defer rc.Close()

		out, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return fmt.Errorf("création %s: %w", outputPath, err)
		}
		if _, err := copyLimited(out, rc, int64(maxArchiveUncompressedBytes)); err != nil {
			out.Close()
			return fmt.Errorf("copie %s: %w", entryName, err)
		}
		if err := out.Close(); err != nil {
			return fmt.Errorf("fermeture %s: %w", outputPath, err)
		}
		return nil
	}

	return fmt.Errorf("archive invalide: fichier %s manquant", entryName)
}

func (s *Service) RestorePostgresBackup(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s == nil || s.db == nil {
		return fmt.Errorf("service backup indisponible")
	}
	if !s.db.IsPostgres() {
		return fmt.Errorf("restauration postgresql non supportee avec %q", s.db.Driver())
	}

	if err := s.ensureDirs(); err != nil {
		return err
	}

	archivePath, err := s.BackupPath(name)
	if err != nil {
		return err
	}

	stamp := time.Now().Format("20060102-150405")
	dumpPath := filepath.Join(s.restoreDir, fmt.Sprintf("restore-%s.sql", stamp))
	defer os.Remove(dumpPath)

	if err := extractBackupEntry(archivePath, "jellygate.sql", dumpPath); err != nil {
		return err
	}

	cfg := s.db.GetConfig()
	port := cfg.Port
	if port <= 0 {
		port = 5432
	}

	_ = os.Setenv("PGPASSWORD", cfg.Password)
	defer os.Unsetenv("PGPASSWORD")

	cmd := exec.Command( // #nosec G204 -- arguments are passed without a shell and come from database configuration.
		"psql",
		"-h", cfg.Host,
		"-p", strconv.Itoa(port),
		"-U", cfg.User,
		"-d", cfg.Name,
		"-v", "ON_ERROR_STOP=1",
		"-f", dumpPath,
	)
	cmdOutput, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restauration postgresql echouee: %w. En mode Docker, 'psql' est inclus dans l'image JellyGate (reconstruisez l'image si la version majeure PostgreSQL ne correspond pas). Hors Docker, verifiez que 'psql' est installe et present dans votre PATH. (output: %s)", err, string(cmdOutput))
	}

	return nil
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
		list = append(list, backupInfoFromFile(filepath.Join(s.backupDir, name), name, st))
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

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return info, fmt.Errorf("création archive importée: %w", err)
	}
	if _, err := copyLimited(f, r, MaxArchiveBytes); err != nil {
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

	requiredEntry := "jellygate.db"
	if s.db != nil && s.db.IsPostgres() {
		requiredEntry = "jellygate.sql"
	}
	hasRequiredEntry, err := backupArchiveContains(path, requiredEntry)
	if err != nil {
		_ = os.Remove(path)
		return info, err
	}
	if !hasRequiredEntry {
		_ = os.Remove(path)
		return info, fmt.Errorf("archive incompatible avec %q: fichier %s manquant", s.db.Driver(), requiredEntry)
	}

	st, err := os.Stat(path)
	if err != nil {
		_ = os.Remove(path)
		return info, err
	}

	return backupInfoFromFile(path, name, st), nil
}

func validateBackupArchive(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("archive invalide: %w", err)
	}
	if st.Size() > MaxArchiveBytes {
		return fmt.Errorf("archive invalide: taille maximale %d MiB depassee", MaxArchiveBytes/(1024*1024))
	}

	zr, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("archive invalide: %w", err)
	}
	defer zr.Close()

	hasSQLiteDump := false
	hasPostgresDump := false
	var totalUncompressed uint64
	for _, f := range zr.File {
		if len(zr.File) > maxArchiveEntries {
			return fmt.Errorf("archive invalide: trop de fichiers")
		}
		if f.FileInfo().IsDir() {
			continue
		}
		totalUncompressed += f.UncompressedSize64
		if totalUncompressed > maxArchiveUncompressedBytes {
			return fmt.Errorf("archive invalide: contenu decompresse trop volumineux")
		}

		base := strings.ToLower(filepath.Base(f.Name))
		if base == "jellygate.db" {
			hasSQLiteDump = true
		}
		if base == "jellygate.sql" {
			hasPostgresDump = true
		}
	}
	if !hasSQLiteDump && !hasPostgresDump {
		return fmt.Errorf("archive invalide: fichier jellygate.db ou jellygate.sql manquant")
	}
	return nil
}

func backupArchiveContains(path, entryName string) (bool, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false, fmt.Errorf("archive invalide: %w", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if strings.EqualFold(filepath.Base(f.Name), entryName) {
			return true, nil
		}
	}

	return false, nil
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

	pendingDB := filepath.Join(s.restoreDir, "jellygate.db.pending")
	pendingMeta := filepath.Join(s.restoreDir, "restore.pending")
	_ = os.Remove(pendingDB)
	_ = os.Remove(pendingMeta)

	if err := extractBackupEntry(archivePath, "jellygate.db", pendingDB); err != nil {
		return err
	}

	marker := map[string]string{
		"backup_name": name,
		"prepared_at": time.Now().Format(time.RFC3339),
	}
	payload, _ := json.Marshal(marker)
	if err := os.WriteFile(pendingMeta, payload, 0600); err != nil {
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

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func copyLimited(dst io.Writer, src io.Reader, maxBytes int64) (int64, error) {
	if maxBytes <= 0 {
		return 0, fmt.Errorf("limite de taille invalide")
	}

	lr := &io.LimitedReader{R: src, N: maxBytes + 1}
	n, err := io.Copy(dst, lr)
	if err != nil {
		return n, err
	}
	if n > maxBytes {
		return n, fmt.Errorf("taille maximale %d MiB depassee", maxBytes/(1024*1024))
	}
	return n, nil
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
	if !s.SupportsSQLiteArchive() && s.db.Driver() != database.DialectPostgres {
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
