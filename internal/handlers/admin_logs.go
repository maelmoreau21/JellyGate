package handlers

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/maelmoreau21/JellyGate/internal/syslog"
)

// SystemLogsAPI retourne la liste des fichiers de logs système et les lignes récentes du log sélectionné.
func (h *AdminHandler) SystemLogsAPI(w http.ResponseWriter, r *http.Request) {
	mgr := syslog.GetManager()
	if mgr == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"files":   []syslog.LogFileInfo{},
			"lines":   []string{"Le gestionnaire de logs système n'est pas initialisé."},
		})
		return
	}

	files, err := mgr.ListLogFiles()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Message: "Erreur lecture des fichiers de logs: " + err.Error(),
		})
		return
	}

	selectedFile := strings.TrimSpace(r.URL.Query().Get("file"))
	if selectedFile == "" && len(files) > 0 {
		selectedFile = files[0].Name
	}

	maxLines := 200
	if l, err := strconv.Atoi(r.URL.Query().Get("lines")); err == nil && l > 0 {
		maxLines = l
	}

	var lines []string
	if selectedFile != "" {
		lines, err = mgr.ReadTail(selectedFile, maxLines)
		if err != nil {
			lines = []string{fmt.Sprintf("Erreur lecture du fichier %s: %v", selectedFile, err)}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"files":         files,
		"selected_file": selectedFile,
		"lines":         lines,
	})
}

// DownloadSystemLog permet le téléchargement direct d'un fichier .log spécifique.
func (h *AdminHandler) DownloadSystemLog(w http.ResponseWriter, r *http.Request) {
	fileName := strings.TrimSpace(r.URL.Query().Get("file"))
	if fileName == "" {
		http.Error(w, "Paramètre 'file' requis", http.StatusBadRequest)
		return
	}

	mgr := syslog.GetManager()
	if mgr == nil {
		http.Error(w, "Gestionnaire de logs non initialisé", http.StatusInternalServerError)
		return
	}

	f, info, err := mgr.OpenLogFile(fileName)
	if err != nil {
		http.Error(w, "Fichier introuvable ou nom invalide", http.StatusNotFound)
		return
	}
	defer f.Close()

	cleanName := filepath.Base(fileName)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, cleanName))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	http.ServeContent(w, r, cleanName, info.ModTime(), f)
}

// DownloadAllSystemLogs génère et télécharge une archive zip de tous les logs système.
func (h *AdminHandler) DownloadAllSystemLogs(w http.ResponseWriter, r *http.Request) {
	mgr := syslog.GetManager()
	if mgr == nil {
		http.Error(w, "Gestionnaire de logs non initialisé", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Disposition", `attachment; filename="jellygate-logs.zip"`)
	w.Header().Set("Content-Type", "application/zip")

	if err := mgr.ZipAllLogs(w); err != nil {
		http.Error(w, "Erreur génération zip: "+err.Error(), http.StatusInternalServerError)
	}
}
