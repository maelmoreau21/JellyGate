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

// RequireAuth est un middleware Chi qui vérifie que l'utilisateur est connecté
// en tant qu'administrateur Jellyfin.
//
// Fonctionnement :
//  1. Lit le cookie de session "jellygate_session"
//  2. Vérifie la signature HMAC-SHA256 et l'expiration
//  3. Si valide → injecte la session dans le contexte et passe au handler suivant
//  4. Si invalide → redirige vers /admin/login
//
// La session est récupérable dans les handlers via session.FromContext(r.Context()).
func RequireAuth(secretKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Récupérer le cookie de session
			cookie, err := r.Cookie(session.CookieName)
			if err != nil {
				slog.Debug("Pas de cookie de session, redirection vers login",
					"path", r.URL.Path,
					"remote", r.RemoteAddr,
				)
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
				return
			}

			// Vérifier la signature et l'expiration
			sess, err := session.Verify(cookie.Value, secretKey)
			if err != nil {
				slog.Warn("Cookie de session invalide",
					"error", err,
					"remote", r.RemoteAddr,
					"path", r.URL.Path,
				)

				// Supprimer le cookie invalide
				http.SetCookie(w, &http.Cookie{
					Name:     session.CookieName,
					Value:    "",
					Path:     "/",
					MaxAge:   -1,
					HttpOnly: true,
				})

				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
				return
			}

			// Injecter la session dans le contexte de la requête
			ctx := session.NewContext(r.Context(), sess)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
