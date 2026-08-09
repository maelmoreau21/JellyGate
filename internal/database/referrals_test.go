package database

import (
	"context"
	"testing"

	"github.com/maelmoreau21/JellyGate/internal/config"
)

func newReferralTestDB(t *testing.T) *DB {
	t.Helper()
	tempDir := t.TempDir()
	db, err := New(config.DatabaseConfig{Type: "sqlite"}, tempDir, "test-secret-32-chars-long-key-123")
	if err != nil {
		t.Fatalf("New DB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestQuotaCalculation(t *testing.T) {
	db := newReferralTestDB(t)
	ctx := context.Background()

	// Create test sponsor user
	res, err := db.Exec(`INSERT INTO users (username, email, can_invite) VALUES ('sponsor1', 'sponsor@example.com', 1)`)
	if err != nil {
		t.Fatalf("Failed to insert sponsor: %v", err)
	}
	sponsorID, _ := res.LastInsertId()

	t.Run("Default Quota Calculation", func(t *testing.T) {
		calc, err := db.CalculateUserQuota(ctx, sponsorID)
		if err != nil {
			t.Fatalf("CalculateUserQuota failed: %v", err)
		}
		if calc.TotalQuota != calc.DefaultQuota {
			t.Errorf("Expected TotalQuota=%d, got %d", calc.DefaultQuota, calc.TotalQuota)
		}
		if calc.UsedQuota != 0 {
			t.Errorf("Expected UsedQuota=0, got %d", calc.UsedQuota)
		}
		if calc.RemainingQuota != calc.TotalQuota {
			t.Errorf("Expected RemainingQuota=%d, got %d", calc.TotalQuota, calc.RemainingQuota)
		}
	})

	t.Run("Custom Quota and Overrides", func(t *testing.T) {
		custom := 10
		err := db.SetUserQuotaOverrides(ctx, sponsorID, &custom, 2, 1)
		if err != nil {
			t.Fatalf("SetUserQuotaOverrides failed: %v", err)
		}

		calc, err := db.CalculateUserQuota(ctx, sponsorID)
		if err != nil {
			t.Fatalf("CalculateUserQuota failed: %v", err)
		}

		// Total = 10 + 2 - 1 = 11
		expectedTotal := 11
		if calc.TotalQuota != expectedTotal {
			t.Errorf("Expected TotalQuota=%d, got %d", expectedTotal, calc.TotalQuota)
		}
	})
}

func TestReferralLifecycleAndTree(t *testing.T) {
	db := newReferralTestDB(t)
	ctx := context.Background()

	// Seed sponsor and invitation
	res, _ := db.Exec(`INSERT INTO users (username, email) VALUES ('sponsor_bob', 'bob@example.com')`)
	sponsorID, _ := res.LastInsertId()

	invRes, _ := db.Exec(`INSERT INTO invitations (code, created_by_user_id, created_by) VALUES ('JG-TEST1', ?, 'sponsor_bob')`, sponsorID)
	invID, _ := invRes.LastInsertId()

	t.Run("Create Referral Pending", func(t *testing.T) {
		ref, err := db.CreateReferral(ctx, sponsorID, invID, "godchild@example.com")
		if err != nil {
			t.Fatalf("CreateReferral failed: %v", err)
		}
		if ref.Status != "pending" {
			t.Errorf("Expected status pending, got %s", ref.Status)
		}

		// Insert godchild user to satisfy FK constraint
		godchildRes, gErr := db.Exec(`INSERT INTO users (username, email, authentik_id) VALUES ('godchild_user', 'godchild@example.com', 'uuid-godchild-102')`)
		if gErr != nil {
			t.Fatalf("Failed to insert godchild user: %v", gErr)
		}
		godchildUserID, _ := godchildRes.LastInsertId()

		err = db.UpdateReferralStatus(ctx, ref.ID, "accepted", &godchildUserID, "uuid-godchild-102")
		if err != nil {
			t.Fatalf("UpdateReferralStatus failed: %v", err)
		}

		tree, err := db.GetReferralsBySponsor(ctx, sponsorID)
		if err != nil {
			t.Fatalf("GetReferralsBySponsor failed: %v", err)
		}
		if len(tree) != 1 {
			t.Fatalf("Expected 1 referral node, got %d", len(tree))
		}
		if tree[0].Status != "accepted" || tree[0].GodchildAuthentikID != "uuid-godchild-102" {
			t.Errorf("Unexpected referral node data: %+v", tree[0])
		}
	})
}
