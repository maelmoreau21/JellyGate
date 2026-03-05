// Package middleware — i18n.go
//
// Middleware de détection de langue pour JellyGate.
//
// Ordre de priorité :
//  1. Cookie "lang" (défini par le sélecteur UI)
//  2. En-tête HTTP Accept-Language
//  3. Valeur default_lang lue depuis la table settings (via DefaultLangProvider)
//
// La langue détectée est injectée dans le contexte de la requête
// et accessible via middleware.LangFromContext(ctx).
package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

// ── Context key ─────────────────────────────────────────────────────────────

type langContextKey struct{}

// LangFromContext extrait la langue du contexte de requête.
// Retourne "fr" si aucune langue n'est définie.
func LangFromContext(ctx context.Context) string {
	if lang, ok := ctx.Value(langContextKey{}).(string); ok && lang != "" {
		return lang
	}
	return "fr"
}

// ── Provider ────────────────────────────────────────────────────────────────

// DefaultLangProvider fournit la langue par défaut du serveur.
// Implémenté par database.DB via GetDefaultLang().
type DefaultLangProvider interface {
	GetDefaultLang() string
}

// ── Middleware ───────────────────────────────────────────────────────────────

// DetectLanguage détermine la langue de l'utilisateur et l'injecte
// dans le contexte de la requête.
//
// Priorité : cookie "lang" → Accept-Language → provider.GetDefaultLang().
func DetectLanguage(provider DefaultLangProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lang := ""

			// 1. Cookie "lang"
			if cookie, err := r.Cookie("lang"); err == nil {
				candidate := config.NormalizeLanguageTag(cookie.Value)
				if config.IsSupportedLanguage(candidate) {
					lang = candidate
				}
			}

			// 2. Accept-Language header
			if lang == "" {
				lang = parseAcceptLanguage(r.Header.Get("Accept-Language"))
			}

			// 3. Default (depuis la base de données)
			if lang == "" {
				lang = config.NormalizeLanguageTag(provider.GetDefaultLang())
				if !config.IsSupportedLanguage(lang) {
					lang = "fr"
				}
			}

			// Injecter dans le contexte
			ctx := context.WithValue(r.Context(), langContextKey{}, lang)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ── Parser Accept-Language ──────────────────────────────────────────────────

// parseAcceptLanguage extrait la première langue supportée de l'en-tête
// Accept-Language.
//
// Exemples :
//
//	"fr-FR,fr;q=0.9,en;q=0.8" → "fr"
//	"en-US,en;q=0.9"           → "en"
//	"pt-BR,pt;q=0.9,en;q=0.8"  → "pt-br"
func parseAcceptLanguage(header string) string {
	if header == "" {
		return ""
	}

	// Accept-Language: fr-FR,fr;q=0.9,en-US;q=0.8,en;q=0.7
	for _, part := range strings.Split(header, ",") {
		// Supprimer le paramètre de qualité (ex: ";q=0.9")
		tag := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])

		// Extraire le code de langue de base (ex: "fr-FR" → "fr")
		candidate := config.NormalizeLanguageTag(tag)
		if config.IsSupportedLanguage(candidate) {
			return candidate
		}
	}

	return ""
}
