package syslog

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSyslogManager(t *testing.T) {
	tempDir := t.TempDir()
	logsDir := filepath.Join(tempDir, "logs")

	mgr, err := NewManagerForTesting(logsDir)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Close()

	// 1. Écriture de logs
	testMsg := "time=2026-08-18T18:00:00Z level=INFO msg=\"Test log message\"\n"
	n, err := mgr.Write([]byte(testMsg))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(testMsg) {
		t.Errorf("Expected %d bytes written, got %d", len(testMsg), n)
	}

	// 2. Listing des fichiers de logs
	files, err := mgr.ListLogFiles()
	if err != nil {
		t.Fatalf("ListLogFiles failed: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("Expected at least 1 log file, got 0")
	}
	todayLog := files[0]
	if !todayLog.IsCurrent {
		t.Errorf("Expected first log file to be marked current")
	}

	// 3. Lecture des dernières lignes
	lines, err := mgr.ReadTail(todayLog.Name, 10)
	if err != nil {
		t.Fatalf("ReadTail failed: %v", err)
	}
	if len(lines) == 0 {
		t.Fatalf("Expected lines, got 0")
	}
	if !strings.Contains(lines[0], "Test log message") {
		t.Errorf("Expected line to contain test message, got: %s", lines[0])
	}

	// 4. Archive ZIP
	var buf bytes.Buffer
	if err := mgr.ZipAllLogs(&buf); err != nil {
		t.Fatalf("ZipAllLogs failed: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatalf("Expected non-empty zip buffer")
	}

	// 5. Purge des anciens logs
	// Créer un vieux fichier de log factice datant de 40 jours
	oldFile := filepath.Join(logsDir, "jellygate-2026-07-01.log")
	if err := os.WriteFile(oldFile, []byte("old log"), 0600); err != nil {
		t.Fatalf("Failed to create old file: %v", err)
	}
	oldTime := time.Now().UTC().AddDate(0, 0, -40)
	_ = os.Chtimes(oldFile, oldTime, oldTime)

	purged, err := mgr.PurgeOldLogs(30)
	if err != nil {
		t.Fatalf("PurgeOldLogs failed: %v", err)
	}
	if purged != 1 {
		t.Errorf("Expected 1 file purged, got %d", purged)
	}

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("Expected old file to be deleted")
	}
}
