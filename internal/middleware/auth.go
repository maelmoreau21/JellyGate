// Package middleware contient les middlewares HTTP de JellyGate.
//
// Ce fichier implémente le middleware d'authentification admin qui protège
// les routes /admin/* en vérifiant le cookie de session signé.
package middleware

import (
	"log/slog"
	"net/http"

	"github.com/maelmoreau21/JellyGate/internal/session"
)

// ── Middleware d'authentification ────────────────────────────────────────────

// SessionValidator peut refuser une session pourtant correctement signee
// (par exemple apres une revocation globale).
type SessionValidator func(*session.Payload) bool

// SessionAllowed applique les validateurs optionnels a une session.
func SessionAllowed(sess *session.Payload, validators ...SessionValidator) bool {
	if sess == nil {
		return false
	}
	for _, validator := range validators {
		if validator != nil && !validator(sess) {
			return false
		}
	}
	return true
}

// RequireAuth est un middleware Chi qui verifie que l'utilisateur est connecte
// en tant qu'utilisateur legitime (Admin ou Standard).
func RequireAuth(secretKey, baseURL string, validators ...SessionValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(session.CookieName)
			if err != nil {
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
				return
			}

			sess, err := session.Verify(cookie.Value, secretKey)
			if err != nil || !SessionAllowed(sess, validators...) {
				// Supprimer le cookie invalide
				// #nosec G124 -- clearing uses the same Secure policy as the session cookie.
				http.SetCookie(w, &http.Cookie{
					Name:     session.CookieName,
					Value:    "",
					Path:     "/",
					MaxAge:   -1,
					HttpOnly: true,
					Secure:   RequestIsHTTPS(r, baseURL),
					SameSite: http.SameSiteStrictMode,
				})
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
				return
			}

			// Le flag "isAdmin" n'est pas vérifié ici (User vs Admin), c'est une zone commune.
			ctx := session.NewContext(r.Context(), sess)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdminAuth est une surcouche qui exige spécifiquement que la session
// contienne IsAdmin == true. Doit être appelé APRÈS RequireAuth (ou englober la route).
func RequireAdminAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess := session.FromContext(r.Context())
			if sess == nil || !sess.IsAdmin {
				slog.Warn("Accès interdit: RequireAdminAuth demandé", "path", r.URL.Path)
				http.Error(w, "Accès interdit (Administrateurs uniquement)", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
