package handlers

import (
	"net/http"
	"strings"

	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/render"
)

func applyRequestTemplateData(r *http.Request, td *render.TemplateData) *render.TemplateData {
	if td == nil || r == nil {
		return td
	}
	if td.Data == nil {
		td.Data = make(map[string]interface{})
	}
	td.ScriptNonce = jgmw.ScriptNonceFromContext(r.Context())

	csrfToken := strings.TrimSpace(jgmw.CSRFTokenFromContext(r.Context()))
	if csrfToken == "" {
		if cookie, err := r.Cookie(jgmw.CSRFCookieName()); err == nil {
			csrfToken = strings.TrimSpace(cookie.Value)
		}
	}
	if csrfToken != "" {
		td.Data["CSRFToken"] = csrfToken
	}
	return td
}
