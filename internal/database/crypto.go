package database

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	// HKDF — RFC 5869, Key Derivation Function sécurisée.
	// golang.org/x/crypto est déjà une dépendance transitive via pgx.
	gohkdf "golang.org/x/crypto/hkdf"
)

const encryptionPrefix = "enc:"

// hkdfInfo est le contexte HKDF fixe qui lie la clé dérivée à cet usage précis.
// Ne pas modifier : changer cette constante invalide tous les chiffrements existants.
const hkdfInfo = "jellygate-db-encryption-v1"

func (db *DB) encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	key, err := derivePrimaryEncryptionKey(db.secretKey)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encryptionPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (db *DB) decrypt(input string) (string, error) {
	if !strings.HasPrefix(input, encryptionPrefix) {
		return input, nil
	}

	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(input, encryptionPrefix))
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	// Ordre des clés candidates :
	// 1. Clé HKDF (nouvelle, sécurisée) — pour les données chiffrées après la migration
	// 2. Clé SHA-256 brute (legacy) — pour décrypter les données existantes avant migration
	// 3. Clé hex brute (ancienne) — pour les déploiements pré-existants
	candidateKeys := make([][]byte, 0, 3)
	if key, err := derivePrimaryEncryptionKey(db.secretKey); err == nil {
		candidateKeys = append(candidateKeys, key)
	}
	if legacy, ok := deriveLegacySHA256EncryptionKey(db.secretKey); ok {
		candidateKeys = append(candidateKeys, legacy)
	}
	if legacy, ok := deriveLegacyHexEncryptionKey(db.secretKey); ok {
		candidateKeys = append(candidateKeys, legacy)
	}
	if len(candidateKeys) == 0 {
		return "", errors.New("invalid secret key for decryption")
	}

	var lastErr error
	for _, key := range candidateKeys {
		block, err := aes.NewCipher(key)
		if err != nil {
			lastErr = err
			continue
		}

		gcm, err := cipher.NewGCM(block)
		if err != nil {
			lastErr = err
			continue
		}

		nonceSize := gcm.NonceSize()
		if len(ciphertext) < nonceSize {
			lastErr = errors.New("ciphertext too short")
			continue
		}

		nonce, encryptedData := ciphertext[:nonceSize], ciphertext[nonceSize:]
		plaintext, err := gcm.Open(nil, nonce, encryptedData, nil)
		if err != nil {
			lastErr = err
			continue
		}

		return string(plaintext), nil
	}

	if lastErr != nil {
		return "", fmt.Errorf("gcm open: %w", lastErr)
	}
	return "", errors.New("unable to decrypt value")
}

// derivePrimaryEncryptionKey dérive une clé AES-256 via HKDF-SHA256 (RFC 5869).
// HKDF est une vraie KDF avec contexte d'info, ce qui évite la faiblesse d'un SHA-256 brut.
// La clé est liée à l'usage "jellygate-db-encryption-v1" pour prévenir toute réutilisation de clé inter-domaines.
func derivePrimaryEncryptionKey(secretKey string) ([]byte, error) {
	trimmed := strings.TrimSpace(secretKey)
	if trimmed == "" {
		return nil, errors.New("invalid secret key for encryption")
	}
	hkdf := gohkdf.New(sha256.New, []byte(trimmed), nil, []byte(hkdfInfo))
	key := make([]byte, 32) // AES-256
	if _, err := io.ReadFull(hkdf, key); err != nil {
		return nil, fmt.Errorf("hkdf derivation failed: %w", err)
	}
	return key, nil
}

// deriveLegacySHA256EncryptionKey est le fallback de migration pour décrypter les données
// chiffrées par l'ancienne implémentation (sha256.Sum256 brut, sans contexte HKDF).
// Utilisé uniquement en déchiffrement, jamais pour de nouveaux chiffrements.
func deriveLegacySHA256EncryptionKey(secretKey string) ([]byte, bool) {
	trimmed := strings.TrimSpace(secretKey)
	if trimmed == "" {
		return nil, false
	}
	derived := sha256.Sum256([]byte(trimmed))
	key := make([]byte, len(derived))
	copy(key, derived[:])
	return key, true
}

// deriveLegacyHexEncryptionKey est un fallback pour les secrets hex encodés d'anciennes versions.
func deriveLegacyHexEncryptionKey(secretKey string) ([]byte, bool) {
	raw, err := hex.DecodeString(strings.TrimSpace(secretKey))
	if err != nil || len(raw) < 16 {
		return nil, false
	}

	key := normalizeAESKeyLength(raw)
	if len(key) == 0 {
		return nil, false
	}
	return key, true
}

func normalizeAESKeyLength(raw []byte) []byte {
	if len(raw) == 0 {
		return nil
	}

	key := raw
	if len(key) > 32 {
		key = key[:32]
	} else if len(key) > 16 && len(key) < 24 {
		key = key[:16]
	} else if len(key) > 24 && len(key) < 32 {
		key = key[:24]
	}

	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return nil
	}

	out := make([]byte, len(key))
	copy(out, key)
	return out
}
