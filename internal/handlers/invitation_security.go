package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/maelmoreau21/JellyGate/internal/config"
	"github.com/maelmoreau21/JellyGate/internal/session"
)

func (h *AdminHandler) InvitationSecurityConfig(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if sess == nil || !sess.IsAdmin {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: h.tr(r, "admin_forbidden", "Acces interdit")})
		return
	}

	cfg, err := h.db.GetProductFeaturesConfig()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur lecture securite invitations"})
		return
	}

	antiAbuse := config.NormalizeProductFeaturesConfig(cfg).AntiAbuse
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: antiAbuse})
}

func (h *AdminHandler) SaveInvitationSecurityConfig(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r.Context())
	if sess == nil || !sess.IsAdmin {
		writeJSON(w, http.StatusForbidden, APIResponse{Success: false, Message: h.tr(r, "admin_forbidden", "Acces interdit")})
		return
	}

	var input config.AntiAbuseConfig
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Success: false, Message: h.tr(r, "common_bad_request", "Requete invalide")})
		return
	}

	cfg, err := h.db.GetProductFeaturesConfig()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur lecture securite invitations"})
		return
	}
	cfg.AntiAbuse = input
	cfg = config.NormalizeProductFeaturesConfig(cfg)

	if err := h.db.SaveProductFeaturesConfig(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, APIResponse{Success: false, Message: "Erreur sauvegarde securite invitations"})
		return
	}

	_ = h.db.LogAction("settings.invitation_security.updated", sess.Username, "", "")
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Securite des invitations sauvegardee", Data: cfg.AntiAbuse})
}
