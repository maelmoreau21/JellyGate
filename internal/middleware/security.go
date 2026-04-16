package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
)

const csrfCookieName = "jg_csrf"
const csrfHeaderName = "X-CSRF-Token"

type scriptNonceContextKey struct{}
type csrfTokenContextKey struct{}

func CSRFCookieName() string {
	return csrfCookieName
}

func ScriptNonceFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if nonce, ok := ctx.Value(scriptNonceContextKey{}).(string); ok {
		return strings.TrimSpace(nonce)
	}
	return ""
}

func CSRFTokenFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if token, ok := ctx.Value(csrfTokenContextKey{}).(string); ok {
		return strings.TrimSpace(token)
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
			w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
			w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
			w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
			w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
			csp := "default-src 'self'; script-src 'self'"
			if strings.TrimSpace(nonce) != "" {
				csp += " 'nonce-" + nonce + "'"
			}
			csp += "; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; img-src 'self' data: https://flagcdn.com; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'"
			w.Header().Set("Content-Security-Policy", csp)

			if RequestIsHTTPS(r, baseURL) {
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
			token := ""
			if existing, err := r.Cookie(csrfCookieName); err == nil {
				token = strings.TrimSpace(existing.Value)
			}

			if token == "" {
				freshToken, tokenErr := generateCSRFToken()
				if tokenErr == nil {
					token = freshToken
					http.SetCookie(w, &http.Cookie{
						Name:     csrfCookieName,
						Value:    token,
						Path:     "/",
						MaxAge:   86400,
						HttpOnly: false,
						Secure:   RequestIsHTTPS(r, baseURL),
						SameSite: http.SameSiteLaxMode,
					})
				}
			}

			ctx := context.WithValue(r.Context(), csrfTokenContextKey{}, token)
			next.ServeHTTP(w, r.WithContext(ctx))
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

			cookieToken := strings.TrimSpace(cookie.Value)
			if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(cookieToken)) != 1 {
				http.Error(w, "CSRF token invalide", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequestIsHTTPS determines whether the incoming request should be
// considered HTTPS. It returns true if any of the following are true:
// - the configured baseURL starts with https://
// - the request has TLS information (r.TLS != nil)
// - the X-Forwarded-Proto header is set to "https"
func RequestIsHTTPS(r *http.Request, baseURL string) bool {
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
