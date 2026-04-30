package backup

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeTestBackupArchive(t *testing.T, dir, name, metadata string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create() error = %v", err)
	}
	zw := zip.NewWriter(f)
	if metadata != "" {
		w, err := zw.Create("metadata.json")
		if err != nil {
			t.Fatalf("zip.Create(metadata) error = %v", err)
		}
		if _, err := w.Write([]byte(metadata)); err != nil {
			t.Fatalf("metadata write error = %v", err)
		}
	}
	w, err := zw.Create("jellygate.db")
	if err != nil {
		t.Fatalf("zip.Create(db) error = %v", err)
	}
	if _, err := w.Write([]byte("db")); err != nil {
		t.Fatalf("db write error = %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip.Close() error = %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("file.Close() error = %v", err)
	}
	return path
}

func TestBackupInfoFromFileClassifiesKnownSources(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name       string
		metadata   string
		wantReason string
		wantSource string
		wantLabel  string
		wantLegacy bool
	}{
		{
			name:       "jellygate-auto-20260429-030047.zip",
			metadata:   `{"reason":"auto"}`,
			wantReason: "auto",
			wantSource: "settings",
			wantLabel:  "Daily automatic",
		},
		{
			name:       "jellygate-scheduled-task-20260428-050048.zip",
			metadata:   "",
			wantReason: "scheduled-task",
			wantSource: "automation",
			wantLabel:  "Advanced automation",
			wantLegacy: true,
		},
		{
			name:       "jellygate-automation-20260430-120000.zip",
			metadata:   `{"reason":"automation"}`,
			wantReason: "automation",
			wantSource: "automation",
			wantLabel:  "Advanced automation",
		},
		{
			name:       "jellygate-imported-20260430-120100.zip",
			metadata:   `{"reason":"manual"}`,
			wantReason: "imported",
			wantSource: "imported",
			wantLabel:  "Imported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTestBackupArchive(t, dir, tt.name, tt.metadata)
			st, err := os.Stat(path)
			if err != nil {
				t.Fatalf("os.Stat() error = %v", err)
			}

			got := backupInfoFromFile(path, tt.name, st)
			if got.Reason != tt.wantReason || got.Source != tt.wantSource || got.DisplayLabel != tt.wantLabel || got.IsLegacyName != tt.wantLegacy {
				t.Fatalf("backupInfoFromFile() = %+v, want reason=%q source=%q label=%q legacy=%v", got, tt.wantReason, tt.wantSource, tt.wantLabel, tt.wantLegacy)
			}
		})
	}
}
