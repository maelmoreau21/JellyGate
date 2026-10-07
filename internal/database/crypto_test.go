package database

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"strings"
	"testing"
)

func TestCrypto_RoundTrip(t *testing.T) {
	db := &DB{secretKey: "test-super-secret-key-that-is-very-long"}

	plaintext := "my-api-token-secret-12345"
	encrypted, err := db.encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	if !strings.HasPrefix(encrypted, encryptionPrefix) {
		t.Fatalf("expected encrypted string to have prefix %q, got %q", encryptionPrefix, encrypted)
	}

	decrypted, err := db.decrypt(encrypted)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Fatalf("got %q, want %q", decrypted, plaintext)
	}
}

func TestCrypto_LegacySHA256Fallback(t *testing.T) {
	secretKey := "legacy-secret-key"
	db := &DB{secretKey: secretKey}

	// Manually encrypt with the legacy single SHA-256 derivation
	plaintext := "secret-from-older-jellygate-version"
	derived := sha256.Sum256([]byte(secretKey))
	legacyKey := derived[:]

	block, err := aes.NewCipher(legacyKey)
	if err != nil {
		t.Fatalf("aes cipher failed: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("new gcm failed: %v", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatalf("read nonce failed: %v", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	legacyEncrypted := encryptionPrefix + base64.StdEncoding.EncodeToString(ciphertext)

	// Verify that db.decrypt successfully decrypts the legacy ciphertext using the fallback
	decrypted, err := db.decrypt(legacyEncrypted)
	if err != nil {
		t.Fatalf("decrypt legacy failed: %v", err)
	}

	if decrypted != plaintext {
		t.Fatalf("got %q, want %q", decrypted, plaintext)
	}
}

func TestCrypto_InvalidCiphertext(t *testing.T) {
	db := &DB{secretKey: "test-secret"}

	// Unprefixed string should be returned as-is (plaintext)
	plain := "plain-unencrypted-string"
	got, err := db.decrypt(plain)
	if err != nil {
		t.Fatalf("unexpected error for plain: %v", err)
	}
	if got != plain {
		t.Fatalf("got %q, want %q", got, plain)
	}

	// Malformed base64
	_, err = db.decrypt(encryptionPrefix + "invalid-base-64!!!")
	if err == nil {
		t.Fatalf("expected error for malformed base64")
	}

	// Corrupt ciphertext with valid base64
	_, err = db.decrypt(encryptionPrefix + base64.StdEncoding.EncodeToString([]byte("short")))
	if err == nil {
		t.Fatalf("expected error for corrupt ciphertext")
	}
}
