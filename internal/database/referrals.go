// Package database — referrals.go
//
// Moteur de calcul des quotas de parrainage, suivi de l'arbre parrain-filleul
// et gestion des statuts de parrainage (pending, accepted, active, expired, cancelled, revoked).
package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ReferralRecord représente une relation de parrainage en base.
type ReferralRecord struct {
	ID                  int64          `json:"id"`
	SponsorUserID       int64          `json:"sponsor_user_id"`
	GodchildUserID      sql.NullInt64  `json:"godchild_user_id"`
	GodchildAuthentikID sql.NullString `json:"godchild_authentik_id"`
	InvitationID        sql.NullInt64  `json:"invitation_id"`
	Status              string         `json:"status"` // pending, accepted, active, expired, cancelled, revoked
	AcceptedAt          sql.NullTime   `json:"accepted_at,omitempty"`
	ActivatedAt         sql.NullTime   `json:"activated_at,omitempty"`
	RevokedAt           sql.NullTime   `json:"revoked_at,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

// QuotaCalculation résume le calcul du quota pour un parrain.
type QuotaCalculation struct {
	SponsorUserID  int64 `json:"sponsor_user_id"`
	DefaultQuota   int   `json:"default_quota"`
	CustomQuota    *int  `json:"custom_quota"`
	BonusQuota     int   `json:"bonus_quota"`
	MalusQuota     int   `json:"malus_quota"`
	TotalQuota     int   `json:"total_quota"`
	UsedQuota      int   `json:"used_quota"`
	RemainingQuota int   `json:"remaining_quota"`
}

// ReferralNode représente une ligne lisible dans l'arbre de parrainage.
type ReferralNode struct {
	ID                  int64      `json:"id"`
	SponsorID           int64      `json:"sponsor_id"`
	SponsorUsername     string     `json:"sponsor_username"`
	GodchildID          *int64     `json:"godchild_id,omitempty"`
	GodchildAuthentikID string     `json:"godchild_authentik_id,omitempty"`
	GodchildUsername    string     `json:"godchild_username,omitempty"`
	GodchildEmail       string     `json:"godchild_email,omitempty"`
	InvitationCode      string     `json:"invitation_code,omitempty"`
	Status              string     `json:"status"`
	CreatedAt           time.Time  `json:"created_at"`
	AcceptedAt          *time.Time `json:"accepted_at,omitempty"`
	ActivatedAt         *time.Time `json:"activated_at,omitempty"`
}

// CalculateUserQuota calcule le quota total, utilisé et restant pour un parrain.
func (db *DB) CalculateUserQuota(ctx context.Context, userID int64) (*QuotaCalculation, error) {
	calc := &QuotaCalculation{
		SponsorUserID: userID,
		DefaultQuota:  3, // Valeur par défaut globale
	}

	// 1. Lire la config système d'invitation si disponible
	inviteProfileCfg, err := db.GetInvitationProfileConfig()
	if err == nil && inviteProfileCfg.InviterQuotaMonth > 0 {
		calc.DefaultQuota = inviteProfileCfg.InviterQuotaMonth
	}

	// 2. Lire les surcharges spécifiques à l'utilisateur dans users
	var (
		customQuota sql.NullInt64
		bonusQuota  int
		malusQuota  int
	)
	err = db.QueryRowContext(ctx,
		`SELECT custom_quota, bonus_quota, malus_quota FROM users WHERE id = ?`,
		userID,
	).Scan(&customQuota, &bonusQuota, &malusQuota)

	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to query user quota fields: %w", err)
	}

	if customQuota.Valid {
		val := int(customQuota.Int64)
		calc.CustomQuota = &val
	}
	calc.BonusQuota = bonusQuota
	calc.MalusQuota = malusQuota

	// Formule: Total = Custom (si non null) SINON Default + Bonus - Malus
	if calc.CustomQuota != nil {
		calc.TotalQuota = *calc.CustomQuota + calc.BonusQuota - calc.MalusQuota
	} else {
		calc.TotalQuota = calc.DefaultQuota + calc.BonusQuota - calc.MalusQuota
	}
	if calc.TotalQuota < 0 {
		calc.TotalQuota = 0
	}

	// 3. Compter les parrainages actifs (status IN ('pending', 'accepted', 'active'))
	var usedCount int
	err = db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM referrals WHERE sponsor_user_id = ? AND status IN ('pending', 'accepted', 'active')`,
		userID,
	).Scan(&usedCount)
	if err != nil {
		// Fallback: compter dans invitations si referrals n'a pas encore de données
		_ = db.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM invitations WHERE created_by_user_id = ? OR created_by = (SELECT username FROM users WHERE id = ?)`,
			userID, userID,
		).Scan(&usedCount)
	}

	calc.UsedQuota = usedCount
	calc.RemainingQuota = calc.TotalQuota - calc.UsedQuota
	if calc.RemainingQuota < 0 {
		calc.RemainingQuota = 0
	}

	return calc, nil
}

// SetUserQuotaOverrides permet à un administrateur d'ajuster les quotas d'un utilisateur.
func (db *DB) SetUserQuotaOverrides(ctx context.Context, userID int64, customQuota *int, bonusQuota, malusQuota int) error {
	query := `UPDATE users SET custom_quota = ?, bonus_quota = ?, malus_quota = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	if db.IsSQLite() {
		query = `UPDATE users SET custom_quota = ?, bonus_quota = ?, malus_quota = ?, updated_at = datetime('now') WHERE id = ?`
	}
	var cqVal interface{}
	if customQuota != nil {
		cqVal = *customQuota
	}
	_, err := db.ExecContext(ctx, query, cqVal, bonusQuota, malusQuota, userID)
	return err
}

// CreateReferral enregistre une nouvelle relation de parrainage en statut 'pending'.
func (db *DB) CreateReferral(ctx context.Context, sponsorID int64, invitationID int64, godchildEmail string) (*ReferralRecord, error) {
	insertQuery := `INSERT INTO referrals (sponsor_user_id, invitation_id, status) VALUES (?, ?, 'pending')`
	res, err := db.ExecContext(ctx, insertQuery, sponsorID, invitationID)
	if err != nil {
		return nil, fmt.Errorf("failed to insert referral record: %w", err)
	}

	refID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &ReferralRecord{
		ID:            refID,
		SponsorUserID: sponsorID,
		InvitationID:  sql.NullInt64{Int64: invitationID, Valid: true},
		Status:        "pending",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}, nil
}

// UpdateReferralStatus fait évoluer le statut d'un parrainage.
func (db *DB) UpdateReferralStatus(ctx context.Context, referralID int64, status string, godchildUserID *int64, godchildAuthentikID string) error {
	var (
		acceptedAtVal  interface{}
		activatedAtVal interface{}
		revokedAtVal   interface{}
	)
	now := time.Now().Format("2006-01-02 15:04:05")

	switch strings.ToLower(status) {
	case "accepted":
		acceptedAtVal = now
	case "active":
		activatedAtVal = now
	case "revoked":
		revokedAtVal = now
	}

	query := `UPDATE referrals 
		SET status = ?, 
		    godchild_user_id = COALESCE(?, godchild_user_id), 
		    godchild_authentik_id = COALESCE(?, godchild_authentik_id),
		    accepted_at = COALESCE(?, accepted_at),
		    activated_at = COALESCE(?, activated_at),
		    revoked_at = COALESCE(?, revoked_at),
		    updated_at = CURRENT_TIMESTAMP 
		WHERE id = ?`
	if db.IsSQLite() {
		query = `UPDATE referrals 
			SET status = ?, 
			    godchild_user_id = COALESCE(?, godchild_user_id), 
			    godchild_authentik_id = COALESCE(?, godchild_authentik_id),
			    accepted_at = COALESCE(?, accepted_at),
			    activated_at = COALESCE(?, activated_at),
			    revoked_at = COALESCE(?, revoked_at),
			    updated_at = datetime('now') 
			WHERE id = ?`
	}

	var godchildIDVal interface{}
	if godchildUserID != nil {
		godchildIDVal = *godchildUserID
	}
	var authIDVal interface{}
	if strings.TrimSpace(godchildAuthentikID) != "" {
		authIDVal = strings.TrimSpace(godchildAuthentikID)
	}

	_, err := db.ExecContext(ctx, query, status, godchildIDVal, authIDVal, acceptedAtVal, activatedAtVal, revokedAtVal, referralID)
	return err
}

// GetReferralsBySponsor renvoie tous les parrainages créés par un parrain.
func (db *DB) GetReferralsBySponsor(ctx context.Context, sponsorID int64) ([]ReferralNode, error) {
	query := `
		SELECT r.id, r.sponsor_user_id, su.username, r.godchild_user_id, COALESCE(r.godchild_authentik_id, ''),
		       COALESCE(gu.username, ''), COALESCE(gu.email, ''), COALESCE(inv.code, ''),
		       r.status, r.created_at, r.accepted_at, r.activated_at
		FROM referrals r
		JOIN users su ON su.id = r.sponsor_user_id
		LEFT JOIN users gu ON gu.id = r.godchild_user_id
		LEFT JOIN invitations inv ON inv.id = r.invitation_id
		WHERE r.sponsor_user_id = ?
		ORDER BY r.created_at DESC`

	return db.queryReferralNodes(ctx, query, sponsorID)
}

// GetAllReferrals renvoie l'arbre de parrainage complet pour l'interface administration.
func (db *DB) GetAllReferrals(ctx context.Context) ([]ReferralNode, error) {
	query := `
		SELECT r.id, r.sponsor_user_id, su.username, r.godchild_user_id, COALESCE(r.godchild_authentik_id, ''),
		       COALESCE(gu.username, ''), COALESCE(gu.email, ''), COALESCE(inv.code, ''),
		       r.status, r.created_at, r.accepted_at, r.activated_at
		FROM referrals r
		JOIN users su ON su.id = r.sponsor_user_id
		LEFT JOIN users gu ON gu.id = r.godchild_user_id
		LEFT JOIN invitations inv ON inv.id = r.invitation_id
		ORDER BY r.created_at DESC`

	return db.queryReferralNodes(ctx, query)
}

func (db *DB) queryReferralNodes(ctx context.Context, query string, args ...interface{}) ([]ReferralNode, error) {
	rows, err := db.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []ReferralNode
	for rows.Next() {
		var n ReferralNode
		var godchildID sql.NullInt64
		var createdAtRaw, acceptedAtRaw, activatedAtRaw interface{}

		err := rows.Scan(
			&n.ID,
			&n.SponsorID,
			&n.SponsorUsername,
			&godchildID,
			&n.GodchildAuthentikID,
			&n.GodchildUsername,
			&n.GodchildEmail,
			&n.InvitationCode,
			&n.Status,
			&createdAtRaw,
			&acceptedAtRaw,
			&activatedAtRaw,
		)
		if err != nil {
			continue
		}

		if godchildID.Valid {
			val := godchildID.Int64
			n.GodchildID = &val
		}

		n.CreatedAt = parseDBTime(createdAtRaw)
		if acceptedAtRaw != nil {
			t := parseDBTime(acceptedAtRaw)
			n.AcceptedAt = &t
		}
		if activatedAtRaw != nil {
			t := parseDBTime(activatedAtRaw)
			n.ActivatedAt = &t
		}

		nodes = append(nodes, n)
	}

	return nodes, nil
}
