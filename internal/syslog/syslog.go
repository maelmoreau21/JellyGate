// Package syslog gère les journaux système réels de JellyGate sur le disque,
// incluant la rotation quotidienne, la lecture en temps réel, le téléchargement
// direct et la purge automatique selon la durée de rétention.
package syslog

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// LogFileInfo décrit un fichier de journal système stocké sur le disque.
type LogFileInfo struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"mod_time"`
	IsCurrent bool      `json:"is_current"`
}

// Manager gère l'écriture, la lecture et la maintenance des fichiers de logs.
type Manager struct {
	mu         sync.RWMutex
	logsDir    string
	currentDay string
	file       *os.File
	writer     io.Writer
}

var (
	defaultManager *Manager
	initOnce       sync.Once
)

// Init initialise le gestionnaire de logs système global et configure le slog handler.
func Init(dataDir string) (*Manager, error) {
	var initErr error
	initOnce.Do(func() {
		logsDir := filepath.Join(dataDir, "logs")
		if err := os.MkdirAll(logsDir, 0750); err != nil {
			initErr = fmt.Errorf("impossible de créer le dossier de logs %s: %w", logsDir, err)
			return
		}

		mgr := &Manager{
			logsDir: logsDir,
		}

		if err := mgr.rotateIfNeeded(); err != nil {
			initErr = fmt.Errorf("impossible d'initialiser le fichier de log: %w", err)
			return
		}

		defaultManager = mgr

		// Configurer slog pour écrire à la fois sur stdout et dans le fichier de log actif
		dualWriter := io.MultiWriter(os.Stdout, mgr)
		handler := slog.NewTextHandler(dualWriter, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
		slog.SetDefault(slog.New(handler))
	})

	if initErr != nil {
		return nil, initErr
	}
	return defaultManager, nil
}

// GetManager retourne l'instance globale du gestionnaire de logs.
func GetManager() *Manager {
	return defaultManager
}

// NewManagerForTesting crée une nouvelle instance de Manager pour les tests.
func NewManagerForTesting(logsDir string) (*Manager, error) {
	if err := os.MkdirAll(logsDir, 0750); err != nil {
		return nil, err
	}
	mgr := &Manager{logsDir: logsDir}
	if err := mgr.rotateIfNeeded(); err != nil {
		return nil, err
	}
	return mgr, nil
}

// SetDefaultManagerForTesting configure l'instance globale de logs pour les tests.
func SetDefaultManagerForTesting(m *Manager) {
	defaultManager = m
}

// Close ferme proprement le fichier de log actif.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.file != nil {
		_ = m.file.Sync()
		err := m.file.Close()
		m.file = nil
		return err
	}
	return nil
}

// Write implémente io.Writer de façon thread-safe avec rotation journalière automatique.
func (m *Manager) Write(p []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	today := time.Now().UTC().Format("2006-01-02")
	if today != m.currentDay || m.file == nil {
		if err := m.rotateLocked(); err != nil {
			return 0, err
		}
	}

	if m.file == nil {
		return 0, fmt.Errorf("fichier de log non disponible")
	}

	return m.file.Write(p)
}

func (m *Manager) rotateIfNeeded() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rotateLocked()
}

func (m *Manager) rotateLocked() error {
	today := time.Now().UTC().Format("2006-01-02")
	if today == m.currentDay && m.file != nil {
		return nil
	}

	if m.file != nil {
		_ = m.file.Sync()
		_ = m.file.Close()
		m.file = nil
	}

	filename := fmt.Sprintf("jellygate-%s.log", today)
	filePath := filepath.Join(m.logsDir, filename)

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("impossible d'ouvrir le fichier de log %s: %w", filePath, err)
	}

	m.file = f
	m.currentDay = today
	return nil
}

// ListLogFiles liste tous les fichiers .log du dossier logs, triés du plus récent au plus ancien.
func (m *Manager) ListLogFiles() ([]LogFileInfo, error) {
	m.mu.RLock()
	dir := m.logsDir
	today := m.currentDay
	m.mu.RUnlock()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("erreur lecture dossier logs: %w", err)
	}

	var results []LogFileInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		isCurrent := strings.Contains(entry.Name(), today)
		results = append(results, LogFileInfo{
			Name:      entry.Name(),
			Size:      info.Size(),
			ModTime:   info.ModTime().UTC(),
			IsCurrent: isCurrent,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].ModTime.After(results[j].ModTime)
	})

	return results, nil
}

// GetLogFilePath valide et retourne le chemin absolu sécurisé d'un fichier de log.
func (m *Manager) GetLogFilePath(filename string) (string, error) {
	cleaned := filepath.Base(filepath.Clean(filename))
	if cleaned == "." || cleaned == ".." || !strings.HasSuffix(cleaned, ".log") {
		return "", fmt.Errorf("nom de fichier de log invalide")
	}

	m.mu.RLock()
	fullPath := filepath.Join(m.logsDir, cleaned)
	m.mu.RUnlock()

	if _, err := os.Stat(fullPath); err != nil {
		return "", fmt.Errorf("fichier de log introuvable: %w", err)
	}

	return fullPath, nil
}

// ReadTail lit les dernières lignes d'un fichier de log (par défaut 200 lignes).
func (m *Manager) ReadTail(filename string, maxLines int) ([]string, error) {
	if maxLines <= 0 {
		maxLines = 200
	}
	if maxLines > 2000 {
		maxLines = 2000
	}

	filePath, err := m.GetLogFilePath(filename)
	if err != nil {
		return nil, err
	}

	// Flush current file before reading if it's active
	m.mu.RLock()
	if m.file != nil && filepath.Base(filePath) == fmt.Sprintf("jellygate-%s.log", m.currentDay) {
		_ = m.file.Sync()
	}
	m.mu.RUnlock()

	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	return lines, nil
}

// PurgeOldLogs supprime les fichiers .log datant de plus de retentionDays jours.
func (m *Manager) PurgeOldLogs(retentionDays int) (int, error) {
	if retentionDays <= 0 {
		retentionDays = 30
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)

	m.mu.RLock()
	dir := m.logsDir
	m.mu.RUnlock()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("erreur lecture dossier logs pour purge: %w", err)
	}

	purgedCount := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().UTC().Before(cutoff) {
			filePath := filepath.Join(dir, entry.Name())
			if err := os.Remove(filePath); err == nil {
				purgedCount++
				slog.Info("Fichier de log ancien purgé", "file", entry.Name(), "retention_days", retentionDays)
			}
		}
	}

	return purgedCount, nil
}

// ZipAllLogs génère une archive ZIP contenant tous les fichiers .log vers le writer w.
func (m *Manager) ZipAllLogs(w io.Writer) error {
	m.mu.RLock()
	if m.file != nil {
		_ = m.file.Sync()
	}
	dir := m.logsDir
	m.mu.RUnlock()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	zw := zip.NewWriter(w)
	defer zw.Close()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}

		filePath := filepath.Join(dir, entry.Name())
		f, err := os.Open(filePath)
		if err != nil {
			continue
		}

		zf, err := zw.Create(entry.Name())
		if err != nil {
			f.Close()
			return err
		}

		_, _ = io.Copy(zf, f)
		f.Close()
	}

	return nil
}
