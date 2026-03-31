package handlers

import (
	"net/http"

	jgmw "github.com/maelmoreau21/JellyGate/internal/middleware"
	"github.com/maelmoreau21/JellyGate/internal/render"
)

func applyRequestTemplateData(r *http.Request, td *render.TemplateData) *render.TemplateData {
	if td == nil || r == nil {
		return td
	}
	td.ScriptNonce = jgmw.ScriptNonceFromContext(r.Context())
	return td
}
