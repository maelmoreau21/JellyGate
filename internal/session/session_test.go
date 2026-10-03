package session

import (
	"testing"
	"time"
)

func TestRememberDuration(t *testing.T) {
	if Duration != 24*time.Hour {
		t.Fatalf("Duration = %s, want 24h", Duration)
	}
	if RememberDuration != 30*24*time.Hour {
		t.Fatalf("RememberDuration = %s, want 30 days", RememberDuration)
	}
	if RememberDuration <= Duration {
		t.Fatalf("RememberDuration = %s, want longer than Duration = %s", RememberDuration, Duration)
	}
	if IndefiniteDuration <= RememberDuration {
		t.Fatalf("IndefiniteDuration = %s, want longer than RememberDuration = %s", IndefiniteDuration, RememberDuration)
	}
}

func TestSessionSignAndVerifySecurity(t *testing.T) {
	secret := "a-very-secret-test-key-at-least-32-chars-long"

	// 1. Rejet clé vide
	p := Payload{
		UserID:   "user1",
		Username: "alice",
		IsAdmin:  false,
		Exp:      time.Now().Add(1 * time.Hour).Unix(),
		Iat:      time.Now().Unix(),
	}
	if _, err := Sign(p, ""); err == nil {
		t.Fatal("Sign with empty secret should fail")
	}
	if _, err := Verify("dummy.signature", ""); err == nil {
		t.Fatal("Verify with empty secret should fail")
	}

	// 2. Signature et vérification valides
	cookieVal, err := Sign(p, secret)
	if err != nil {
		t.Fatalf("Sign error: %v", err)
	}
	verified, err := Verify(cookieVal, secret)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if verified.Username != "alice" || verified.UserID != "user1" {
		t.Fatalf("unexpected verified payload: %+v", verified)
	}

	// 3. Rejet falsification (tampering)
	tampered := cookieVal + "x"
	if _, err := Verify(tampered, secret); err == nil {
		t.Fatal("Verify with tampered signature should fail")
	}

	// 4. Rejet mauvaise clé
	if _, err := Verify(cookieVal, "another-wrong-secret-key-that-is-long-enough"); err == nil {
		t.Fatal("Verify with wrong secret should fail")
	}

	// 5. Rejet session expirée
	expiredP := Payload{
		UserID:   "user1",
		Username: "alice",
		Exp:      time.Now().Add(-10 * time.Minute).Unix(),
		Iat:      time.Now().Add(-1 * time.Hour).Unix(),
	}
	expiredCookie, err := Sign(expiredP, secret)
	if err != nil {
		t.Fatalf("Sign error: %v", err)
	}
	if _, err := Verify(expiredCookie, secret); err == nil {
		t.Fatal("Verify with expired session should fail")
	}

	// 6. Rejet session émise dans le futur
	futureP := Payload{
		UserID:   "user1",
		Username: "alice",
		Exp:      time.Now().Add(2 * time.Hour).Unix(),
		Iat:      time.Now().Add(10 * time.Minute).Unix(),
	}
	futureCookie, err := Sign(futureP, secret)
	if err != nil {
		t.Fatalf("Sign error: %v", err)
	}
	if _, err := Verify(futureCookie, secret); err == nil {
		t.Fatal("Verify with future iat should fail")
	}
}
