package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/database"
	"github.com/maelmoreau21/JellyGate/internal/jellyfin"
	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/render"
	"github.com/maelmoreau21/JellyGate/internal/scheduler"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

type AutomationHandler struct {
	db        *database.DB
	jfClient  *jellyfin.Client
	renderer  *render.Engine
	scheduler *scheduler.Service
}

func NewAutomationHandler(db *database.DB, renderer *render.Engine, schedulerSvc *scheduler.Service, jf *jellyfin.Client) *AutomationHandler {
	return &AutomationHandler{db: db, jfClient: jf, renderer: renderer, scheduler: schedulerSvc}
}

func (h *AutomationHandler) tr(r *http.Request, key, fallback string) string {
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

func (h *AutomationHandler) AutomationPage(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	td := applyRequestTemplateData(r, h.renderer.NewTemplateData(jgmw.LangFromContext(r.Context())))
	td.AdminUsername = sess.Username
	td.IsAdmin = true
	td.CanInvite = true
	td.LDAPEnabled = h.db.IsLDAPEnabled()
	td.Section = "automation"
	if err := h.renderer.Render(w, "admin/automation.html", td); err != nil {
		http.Error(w, h.tr(r, "common_server_error_page", "Erreur serveur : impossible de charger la page"), http.StatusInternalServerError)
	}
}

func (h *AutomationHandler) ListPresets(w http.ResponseWriter, r *http.Request) {
	presets, err := h.db.GetJellyfinPolicyPresets()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "automation_error_presets", "Erreur lecture presets")})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: presets})
}

func (h *AutomationHandler) ListLibraries(w http.ResponseWriter, r *http.Request) {
	if h.jfClient == nil {
		writeJSON(w, http.StatusServiceUnavailable, APIResponse{Success: false, Message: h.tr(r, "admin_jf_unavailable", "Service Jellyfin indisponible")})
		return
	}

	libraries, err := h.jfClient.GetLibraries()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, APIResponse{Success: false, Message: h.tr(r, "automation_error_libraries", "Impossible de charger les mediatheques Jellyfin")})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: libraries})
}

func (h *AutomationHandler) SavePresets(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if sess == nil || !sess.IsAdmin {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: h.tr(r, "login_error_forbidden", "Acces admin requis")})
		return
	}

	var presets []config.JellyfinPolicyPreset
	if err := json.NewDecoder(r.Body).Decode(&presets); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "common_bad_request", "JSON invalide")})
		return
	}

	for i := range presets {
		presets[i].ID = strings.TrimSpace(strings.ToLower(presets[i].ID))
		if presets[i].ID == "" {
			presets[i].ID = "preset-" + strconv.Itoa(i+1)
		}
		if presets[i].Name == "" {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "automation_error_preset_name_required", "Chaque preset doit avoir un nom")})
			return
		}
	}

	if err := h.db.SaveJellyfinPolicyPresets(presets); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "automation_save_presets_failed", "Sauvegarde presets impossible")})
		return
	}

	_ = h.db.LogAction("automation.presets.saved", sess.Username, "jellyfin_presets", strconv.Itoa(len(presets)))
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: h.tr(r, "automation_presets_saved", "Presets sauvegardes")})
}

func (h *AutomationHandler) ListGroupMappings(w http.ResponseWriter, r *http.Request) {
	mappings, err := h.db.GetGroupPolicyMappings()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "automation_error_group_mappings", "Erreur lecture mappings de groupes")})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: mappings})
}

func (h *AutomationHandler) SaveGroupMappings(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if sess == nil || !sess.IsAdmin {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: h.tr(r, "login_error_forbidden", "Acces admin requis")})
		return
	}

	var mappings []config.GroupPolicyMapping
	if err := json.NewDecoder(r.Body).Decode(&mappings); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "common_bad_request", "JSON invalide")})
		return
	}

	presets, err := h.db.GetJellyfinPolicyPresets()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "automation_error_presets", "Impossible de lire les presets")})
		return
	}

	presetIndex := make(map[string]struct{}, len(presets))
	for i := range presets {
		presetIndex[strings.TrimSpace(strings.ToLower(presets[i].ID))] = struct{}{}
	}

	for i := range mappings {
		mappings[i].GroupName = strings.TrimSpace(mappings[i].GroupName)
		mappings[i].PolicyPresetID = strings.TrimSpace(strings.ToLower(mappings[i].PolicyPresetID))
		mappings[i].Source = strings.TrimSpace(strings.ToLower(mappings[i].Source))
		if mappings[i].Source != "ldap" {
			mappings[i].Source = "internal"
		}
		if mappings[i].GroupName == "" || mappings[i].PolicyPresetID == "" {
			continue
		}
		if _, ok := presetIndex[mappings[i].PolicyPresetID]; !ok {
			writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "automation_error_mapping_invalid_preset", "Un mapping rÃƒÂ©fÃƒÂ©rence un preset introuvable")})
			return
		}
	}

	if err := h.db.SaveGroupPolicyMappings(mappings); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "automation_save_mappings_failed", "Sauvegarde mappings impossible")})
		return
	}

	_ = h.db.LogAction("automation.group_mappings.saved", sess.Username, "group_mappings", strconv.Itoa(len(mappings)))
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: h.tr(r, "automation_mappings_saved", "Mappings de groupes sauvegardes")})
}

func (h *AutomationHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(
		`SELECT id, name, task_type, enabled, hour, minute, payload, last_run_at, created_by, created_at, updated_at
		 FROM scheduled_tasks ORDER BY created_at DESC`,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "automation_error_tasks", "Erreur lecture taches")})
		return
	}
	defer rows.Close()

	tasks := make([]scheduler.TaskRecord, 0)
	for rows.Next() {
		var t scheduler.TaskRecord
		if err := rows.Scan(&t.ID, &t.Name, &t.TaskType, &t.Enabled, &t.Hour, &t.Minute, &t.Payload, &t.LastRunAt, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt); err == nil {
			tasks = append(tasks, t)
		}
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: tasks})
}

func (h *AutomationHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if sess == nil || !sess.IsAdmin {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: h.tr(r, "login_error_forbidden", "Acces admin requis")})
		return
	}

	var input scheduler.TaskRecord
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "common_bad_request", "Payload JSON invalide")})
		return
	}

	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.TaskType) == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "automation_error_task_name_type_required", "Nom et type requis")})
		return
	}
	if input.Hour < 0 || input.Hour > 23 || input.Minute < 0 || input.Minute > 59 {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "automation_error_task_schedule_invalid", "Horaire invalide")})
		return
	}

	_, err := h.db.Exec(
		`INSERT INTO scheduled_tasks (name, task_type, enabled, hour, minute, payload, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(input.Name),
		strings.TrimSpace(input.TaskType),
		input.Enabled,
		input.Hour,
		input.Minute,
		strings.TrimSpace(input.Payload),
		sess.Username,
		time.Now().Format("2006-01-02 15:04:05"),
		time.Now().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "automation_task_create_failed", "Creation tache impossible")})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: h.tr(r, "automation_task_created", "Tache planifiee creee")})
}

func (h *AutomationHandler) UpdateTask(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if sess == nil || !sess.IsAdmin {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: h.tr(r, "login_error_forbidden", "Acces admin requis")})
		return
	}

	taskID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID tache invalide"})
		return
	}

	var input scheduler.TaskRecord
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "Payload JSON invalide"})
		return
	}

	_, err = h.db.Exec(
		`UPDATE scheduled_tasks
		 SET name = ?, task_type = ?, enabled = ?, hour = ?, minute = ?, payload = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		strings.TrimSpace(input.Name),
		strings.TrimSpace(input.TaskType),
		input.Enabled,
		input.Hour,
		input.Minute,
		strings.TrimSpace(input.Payload),
		taskID,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "automation_task_update_failed", "Mise a jour impossible")})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: h.tr(r, "automation_task_updated", "Tache mise a jour")})
}

func (h *AutomationHandler) DeleteTask(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if sess == nil || !sess.IsAdmin {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: h.tr(r, "login_error_forbidden", "Acces admin requis")})
		return
	}

	taskID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID tache invalide"})
		return
	}

	res, err := h.db.Exec(`DELETE FROM scheduled_tasks WHERE id = ?`, taskID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: h.tr(r, "automation_task_delete_failed", "Suppression impossible")})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeJSON(w, http.StatusNotFound, APIResponse{Success: false, Message: h.tr(r, "automation_manual_task_missing", "Tache introuvable")})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: h.tr(r, "automation_task_deleted", "Tache supprimee")})
}

func (h *AutomationHandler) RunTaskNow(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if sess == nil || !sess.IsAdmin {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: "Acces admin requis"})
		return
	}

	taskID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: "ID tache invalide"})
		return
	}

	if err := h.scheduler.RunTaskNow(taskID); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "automation_task_run_failed", "Execution echouee") + ": " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: h.tr(r, "automation_task_run_success", "Tache executee")})
}

func (h *AutomationHandler) GetPresetByID(id string) (*config.JellyfinPolicyPreset, error) {
	presets, err := h.db.GetJellyfinPolicyPresets()
	if err != nil {
		return nil, err
	}
	id = strings.TrimSpace(strings.ToLower(id))
	for i := range presets {
		if strings.TrimSpace(strings.ToLower(presets[i].ID)) == id {
			return &presets[i], nil
		}
	}
	return nil, sql.ErrNoRows
}
