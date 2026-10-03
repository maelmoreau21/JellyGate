package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/syslog"
)

func TestSystemLogsAPIAndDownload(t *testing.T) {
	tempDir := t.TempDir()
	logsDir := filepath.Join(tempDir, "logs")
	mgr, err := syslog.NewManagerForTesting(logsDir)
	if err != nil {
		t.Fatalf("Failed to create test syslog manager: %v", err)
	}
	defer mgr.Close()
	syslog.SetDefaultManagerForTesting(mgr)

	// Écrire un log
	_, _ = mgr.Write([]byte("time=2026-08-18T18:30:00Z level=INFO msg=\"System log line for testing\"\n"))

	db := newAuthTestDB(t)
	cfg := &config.Config{
		BaseURL:   "http://localhost:8097",
		SecretKey: testAuthSecret,
		DataDir:   tempDir,
	}

	renderEngine, _ := newTestRenderEngine(t)
	adminHandler := NewAdminHandler(cfg, db, nil, nil, nil, renderEngine)

	// 1. Appel SystemLogsAPI
	req := httptest.NewRequest(http.MethodGet, "/admin/api/logs/system", nil)
	rec := httptest.NewRecorder()
	adminHandler.SystemLogsAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("SystemLogsAPI returned %d: %s", rec.Code, rec.Body.String())
	}

	var res struct {
		Success      bool                 `json:"success"`
		Files        []syslog.LogFileInfo `json:"files"`
		SelectedFile string               `json:"selected_file"`
		Lines        []string             `json:"lines"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if !res.Success {
		t.Fatalf("Expected success true")
	}

	// 2. Téléchargement d'un fichier de log
	if res.SelectedFile != "" {
		reqDownload := httptest.NewRequest(http.MethodGet, "/admin/api/logs/system/download?file="+res.SelectedFile, nil)
		recDownload := httptest.NewRecorder()
		adminHandler.DownloadSystemLog(recDownload, reqDownload)

		if recDownload.Code != http.StatusOK {
			t.Fatalf("DownloadSystemLog returned %d: %s", recDownload.Code, recDownload.Body.String())
		}
		if !strings.Contains(recDownload.Body.String(), "System log line for testing") {
			t.Errorf("Expected downloaded log to contain test line, got: %s", recDownload.Body.String())
		}
	}

	// 3. Téléchargement de l'archive ZIP
	reqZip := httptest.NewRequest(http.MethodGet, "/admin/api/logs/system/download-all", nil)
	recZip := httptest.NewRecorder()
	adminHandler.DownloadAllSystemLogs(recZip, reqZip)

	if recZip.Code != http.StatusOK {
		t.Fatalf("DownloadAllSystemLogs returned %d: %s", recZip.Code, recZip.Body.String())
	}
	if recZip.Header().Get("Content-Type") != "application/zip" {
		t.Errorf("Expected application/zip, got: %s", recZip.Header().Get("Content-Type"))
	}
}
