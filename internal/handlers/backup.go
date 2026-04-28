package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/maelmoreau21/JellyGate/internal/backup"
	"github.com/maelmoreau21/JellyGate/internal/database"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

type BackupHandler struct {
	db       *database.DB
	service  *backup.Service
	renderer *render.Engine
}

func NewBackupHandler(db *database.DB, service *backup.Service, renderer *render.Engine) *BackupHandler {
	return &BackupHandler{db: db, service: service, renderer: renderer}
}

func (h *BackupHandler) tr(r *http.Request, key, fallback string) string {
	if h.renderer == nil {
		return fallback
	}
	lang := jgmw.LangFromContext(r.Context())
	value := h.renderer.Translate(lang, key)
	if value == "["+key+"]" {
		return fallback
	}
	return value
}

func (h *BackupHandler) ListBackups(w http.ResponseWriter, r *http.Request) {
	list, err := h.service.ListBackups()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "backup_error_list", "Impossible de lister les sauvegardes")})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: list})
}

func (h *BackupHandler) CreateBackup(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	info, err := h.service.CreateBackup("manual")
	if err != nil {
		if errors.Is(err, backup.ErrSQLiteOnly) {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "backup_error_create", "Ã‰chec de crÃ©ation de la sauvegarde")})
		return
	}

	if cfg, cfgErr := h.db.GetBackupConfig(); cfgErr == nil {
		_ = h.service.ApplyRetention(cfg.RetentionCount)
	}
	_ = h.db.LogAction("backup.manual.created", sess.Username, info.Name, "")

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: h.tr(r, "backup_created", "Sauvegarde crÃ©Ã©e"), Data: info})
}

func (h *BackupHandler) DownloadBackup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	path, err := h.service.BackupPath(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: h.tr(r, "backup_error_not_found", "Sauvegarde introuvable")})
		return
	}

	f, err := os.Open(path)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "backup_error_read", "Impossible de lire la sauvegarde")})
		return
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "backup_error_read", "Impossible de lire la sauvegarde")})
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", st.Name()))
	w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

func (h *BackupHandler) ImportBackup(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())

	if err := r.ParseMultipartForm(512 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Formulaire invalide"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Fichier manquant"})
		return
	}
	defer file.Close()

	info, err := h.service.ImportBackup(header.Filename, file)
	if err != nil {
		if errors.Is(err, backup.ErrSQLiteOnly) {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}

	if cfg, cfgErr := h.db.GetBackupConfig(); cfgErr == nil {
		_ = h.service.ApplyRetention(cfg.RetentionCount)
	}

	_ = h.db.LogAction("backup.imported", sess.Username, info.Name, "")
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: h.tr(r, "backup_imported", "Sauvegarde importÃ©e"), Data: info})
}

func (h *BackupHandler) RestoreBackup(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Nom de sauvegarde manquant"})
		return
	}

	if h.db.IsPostgres() {
		if err := h.service.RestorePostgresBackup(name); err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
			return
		}

		_ = h.db.LogAction("backup.restore.applied", sess.Username, name, time.Now().Format(time.RFC3339))
		writeJSON(w, http.StatusOK, APIResponse{
			Success: true,
			Message: h.tr(r, "backup_restore_applied", "Restauration PostgreSQL appliquÃ©e."),
			Data: map[string]interface{}{
				"restart_required": false,
				"backup":           name,
			},
		})
		return
	}

	if err := h.service.PrepareRestore(name); err != nil {
		if errors.Is(err, backup.ErrSQLiteOnly) {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}

	_ = h.db.LogAction("backup.restore.prepared", sess.Username, name, time.Now().Format(time.RFC3339))
	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: h.tr(r, "backup_restore_prepared", "Restauration prÃ©parÃ©e. RedÃ©marre JellyGate pour appliquer la sauvegarde."),
		Data: map[string]interface{}{
			"restart_required": true,
			"backup":           name,
		},
	})
}

func (h *BackupHandler) DeleteBackup(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	name := chi.URLParam(r, "name")
	if err := h.service.DeleteBackup(name); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: err.Error()})
		return
	}
	_ = h.db.LogAction("backup.deleted", sess.Username, name, "")
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: h.tr(r, "backup_deleted", "Sauvegarde supprimÃ©e")})
}
