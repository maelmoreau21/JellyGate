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
)

const encryptionPrefix = "enc:"

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

	candidateKeys := make([][]byte, 0, 2)
	if key, err := derivePrimaryEncryptionKey(db.secretKey); err == nil {
		candidateKeys = append(candidateKeys, key)
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

func derivePrimaryEncryptionKey(secretKey string) ([]byte, error) {
	trimmed := strings.TrimSpace(secretKey)
	if trimmed == "" {
		return nil, errors.New("invalid secret key for encryption")
	}
	derived := sha256.Sum256([]byte(trimmed))
	key := make([]byte, len(derived))
	copy(key, derived[:])
	return key, nil
}

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
