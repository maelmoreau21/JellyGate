package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

const csrfCookieName = "jg_csrf"
const csrfHeaderName = "X-CSRF-Token"

type scriptNonceContextKey struct{}

func ScriptNonceFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if nonce, ok := ctx.Value(scriptNonceContextKey{}).(string); ok {
		return strings.TrimSpace(nonce)
	}
	return ""
}

// SecurityHeaders ajoute un socle de headers de securite pour toutes les reponses.
func SecurityHeaders(baseURL string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nonce, _ := generateCSRFToken()
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			csp := "default-src 'self'; script-src 'self'"
			if strings.TrimSpace(nonce) != "" {
				csp += " 'nonce-" + nonce + "'"
			}
			csp += "; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; img-src 'self' data: https://flagcdn.com; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'"
			w.Header().Set("Content-Security-Policy", csp)

			if requestIsHTTPS(r, baseURL) {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			ctx := context.WithValue(r.Context(), scriptNonceContextKey{}, strings.TrimSpace(nonce))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// EnsureCSRFCookie cree un cookie CSRF si absent.
func EnsureCSRFCookie(baseURL string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := r.Cookie(csrfCookieName); err != nil {
				token, tokenErr := generateCSRFToken()
				if tokenErr == nil {
					http.SetCookie(w, &http.Cookie{
						Name:     csrfCookieName,
						Value:    token,
						Path:     "/",
						MaxAge:   86400,
						HttpOnly: false,
						Secure:   requestIsHTTPS(r, baseURL),
						SameSite: http.SameSiteLaxMode,
					})
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireCSRF verifie le token sur les methodes mutables.
func RequireCSRF() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isUnsafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			cookie, err := r.Cookie(csrfCookieName)
			if err != nil || strings.TrimSpace(cookie.Value) == "" {
				http.Error(w, "CSRF token manquant", http.StatusForbidden)
				return
			}

			token := strings.TrimSpace(r.Header.Get(csrfHeaderName))
			if token == "" {
				token = strings.TrimSpace(r.FormValue("_csrf"))
			}

			if token == "" || token != strings.TrimSpace(cookie.Value) {
				http.Error(w, "CSRF token invalide", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func requestIsHTTPS(r *http.Request, baseURL string) bool {
	if r == nil {
		return false
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(baseURL)), "https://") {
		return true
	}
	if r.TLS != nil {
		return true
	}
	proto := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")))
	return proto == "https"
}

func generateCSRFToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func isUnsafeMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
