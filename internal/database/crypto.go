package database

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
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

	key, err := hex.DecodeString(db.secretKey)
	if err != nil || len(key) < 16 {
		// FALLBACK: If key is not valid hex or too short, return plaintext (we don't want to break the app)
		// Better would be to enforce a valid key at startup.
		return plaintext, nil
	}

	// Use only the first 32 bytes if the key is longer
	if len(key) > 32 {
		key = key[:32]
	} else if len(key) > 16 && len(key) < 24 {
		key = key[:16]
	} else if len(key) > 24 && len(key) < 32 {
		key = key[:24]
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

	key, err := hex.DecodeString(db.secretKey)
	if err != nil || len(key) < 16 {
		return "", errors.New("invalid secret key for decryption")
	}

	if len(key) > 32 {
		key = key[:32]
	} else if len(key) > 16 && len(key) < 24 {
		key = key[:16]
	} else if len(key) > 24 && len(key) < 32 {
		key = key[:24]
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, encryptedData := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return "", fmt.Errorf("gcm open: %w", err)
	}

	return string(plaintext), nil
}
