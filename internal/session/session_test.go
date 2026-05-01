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
