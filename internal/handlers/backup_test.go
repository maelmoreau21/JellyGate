package handlers

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	backupsvc "github.com/maelmoreau21/JellyGate/internal/backup"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
)

func writeHandlerTestBackup(t *testing.T, dataDir, name, metadata string) {
	t.Helper()
	backupDir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		t.Fatalf("MkdirAll(backups) error = %v", err)
	}
	f, err := os.Create(filepath.Join(backupDir, name))
	if err != nil {
		t.Fatalf("os.Create(backup) error = %v", err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("metadata.json")
	if err != nil {
		t.Fatalf("zip.Create(metadata) error = %v", err)
	}
	if _, err := w.Write([]byte(metadata)); err != nil {
		t.Fatalf("metadata write error = %v", err)
	}
	dbEntry, err := zw.Create("jellygate.db")
	if err != nil {
		t.Fatalf("zip.Create(db) error = %v", err)
	}
	if _, err := dbEntry.Write([]byte("db")); err != nil {
		t.Fatalf("db write error = %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip.Close() error = %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("file.Close() error = %v", err)
	}
}

func TestBackupHandlerListBackupsReturnsDisplayMetadata(t *testing.T) {
	dataDir := t.TempDir()
	db, err := database.New(config.DatabaseConfig{Type: "sqlite"}, dataDir, "test-secret-key-0123456789012345")
	if err != nil {
		t.Fatalf("database.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	writeHandlerTestBackup(t, dataDir, "jellygate-automation-20260430-120000.zip", `{"reason":"automation"}`)
	handler := NewBackupHandler(db, backupsvc.NewService(dataDir, db), nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/backups", nil)
	handler.ListBackups(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ListBackups status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Success bool `json:"success"`
		Data    []struct {
			Name         string `json:"name"`
			Reason       string `json:"reason"`
			Source       string `json:"source"`
			DisplayLabel string `json:"display_label"`
			IsLegacyName bool   `json:"is_legacy_name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Success || len(resp.Data) != 1 {
		t.Fatalf("response = %+v, want one successful backup", resp)
	}
	got := resp.Data[0]
	if got.Name == "" || got.Reason != "automation" || got.Source != "automation" || got.DisplayLabel != "Advanced automation" || got.IsLegacyName {
		t.Fatalf("backup metadata = %+v", got)
	}
}
