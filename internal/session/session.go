// Package session gère les sessions admin de JellyGate.
//
// Ce package est isolé pour éviter les cycles d'import entre
// handlers et middleware. Il contient :
//   - SessionPayload : données stockées dans le cookie
//   - signSession / VerifySession : signature HMAC-SHA256
//   - SessionFromContext : récupération depuis le contexte de requête
//   - Constantes de session (nom du cookie, durée)
package session

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ── Constantes ──────────────────────────────────────────────────────────────

const (
	// CookieName est le nom du cookie de session admin.
	CookieName = "jellygate_session"

	// Duration est la durée de validité d'une session (24 heures).
	Duration = 24 * time.Hour

	// RememberDuration est la duree de validite quand l'utilisateur choisit
	// de rester connecte sur ce navigateur.
	RememberDuration = 30 * 24 * time.Hour
)

// ── Clés de contexte ────────────────────────────────────────────────────────

// contextKey est un type privé pour les clés de contexte (évite les collisions).
type contextKey string

const (
	// sessionKey est la clé de contexte pour la session admin.
	sessionKey contextKey = "session"
)

// ── Structures de données ───────────────────────────────────────────────────

// Payload contient les données stockées dans le cookie de session.
// Le cookie est signé (HMAC-SHA256) mais pas chiffré — ne jamais y mettre
// de données sensibles (mots de passe, tokens Jellyfin).
type Payload struct {
	UserID   string `json:"uid"` // ID Jellyfin de l'utilisateur
	Username string `json:"usr"` // Nom d'utilisateur
	IsAdmin  bool   `json:"adm"` // Est administrateur Jellyfin
	Exp      int64  `json:"exp"` // Timestamp d'expiration (Unix)
}

// ── Signature et vérification ───────────────────────────────────────────────

// Sign sérialise le payload en JSON, l'encode en base64, et y ajoute
// une signature HMAC-SHA256 pour empêcher toute falsification.
//
// Format du cookie : base64(payload).base64(hmac)
func Sign(payload Payload, secretKey string) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("erreur de sérialisation du payload: %w", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(data)

	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(encoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return encoded + "." + signature, nil
}

// Verify vérifie la signature du cookie et retourne le payload décodé.
// Retourne une erreur si la signature est invalide ou la session expirée.
func Verify(cookieValue, secretKey string) (*Payload, error) {
	parts := strings.SplitN(cookieValue, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("format de cookie invalide")
	}

	encoded := parts[0]
	providedSig := parts[1]

	// Recalculer le HMAC attendu
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(encoded))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	// Comparaison en temps constant (protection contre timing attacks)
	if !hmac.Equal([]byte(providedSig), []byte(expectedSig)) {
		return nil, fmt.Errorf("signature invalide")
	}

	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("erreur de décodage base64: %w", err)
	}

	var payload Payload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("erreur de décodage JSON: %w", err)
	}

	if time.Now().Unix() > payload.Exp {
		return nil, fmt.Errorf("session expirée")
	}

	return &payload, nil
}

// ── Contexte ────────────────────────────────────────────────────────────────

// NewContext injecte une session dans un contexte.
func NewContext(ctx context.Context, s *Payload) context.Context {
	return context.WithValue(ctx, sessionKey, s)
}

// FromContext récupère la session admin depuis le contexte de la requête.
// Retourne nil si aucune session n'est présente.
func FromContext(ctx context.Context) *Payload {
	s, ok := ctx.Value(sessionKey).(*Payload)
	if !ok {
		return nil
	}
	return s
}
