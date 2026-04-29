package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

func TestInvitationSecurityConfigRoundTrip(t *testing.T) {
	_, db := newTestSettingsHandler(t)
	handler := NewAdminHandler(&config.Config{}, db, nil, nil, nil, nil)

	payload := config.AntiAbuseConfig{
		Enabled:       true,
		Captcha:       false,
		MaxFailures:   -3,
		WindowMinutes: 0,
		BlockMinutes:  -1,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	saveRec := httptest.NewRecorder()
	handler.SaveInvitationSecurityConfig(saveRec, newAdminRequest(http.MethodPost, "/admin/api/invitations/security", body))
	if saveRec.Code != http.StatusOK {
		t.Fatalf("SaveInvitationSecurityConfig status = %d, want %d; body=%s", saveRec.Code, http.StatusOK, saveRec.Body.String())
	}

	var saveResp struct {
		Success bool                   `json:"success"`
		Data    config.AntiAbuseConfig `json:"data"`
	}
	if err := json.NewDecoder(saveRec.Body).Decode(&saveResp); err != nil {
		t.Fatalf("decode save response: %v", err)
	}
	if !saveResp.Success {
		t.Fatalf("SaveInvitationSecurityConfig success = false")
	}
	if !saveResp.Data.Enabled || saveResp.Data.Captcha {
		t.Fatalf("anti-abuse booleans not preserved: %+v", saveResp.Data)
	}
	if saveResp.Data.MaxFailures != 5 || saveResp.Data.WindowMinutes != 15 || saveResp.Data.BlockMinutes != 20 {
		t.Fatalf("anti-abuse numeric defaults not normalized: %+v", saveResp.Data)
	}

	getRec := httptest.NewRecorder()
	handler.InvitationSecurityConfig(getRec, newAdminRequest(http.MethodGet, "/admin/api/invitations/security", nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("InvitationSecurityConfig status = %d, want %d", getRec.Code, http.StatusOK)
	}

	var getResp struct {
		Success bool                   `json:"success"`
		Data    config.AntiAbuseConfig `json:"data"`
	}
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if getResp.Data != saveResp.Data {
		t.Fatalf("GET data = %+v, want %+v", getResp.Data, saveResp.Data)
	}
}
