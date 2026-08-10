// Package database — users.go
//
// Accès aux données de la table `users` et synchronisation d'identité OIDC JIT.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// User représente un enregistrement utilisateur JellyGate.
type User struct {
	ID              int64          `json:"id"`
	AuthentikID     sql.NullString `json:"authentik_id"`
	JellyfinID      sql.NullString `json:"jellyfin_id"`
	Username        string         `json:"username"`
	Email           string         `json:"email"`
	EmailVerified   bool           `json:"email_verified"`
	IsActive        bool           `json:"is_active"`
	IsBanned        bool           `json:"is_banned"`
	CanInvite       bool           `json:"can_invite"`
	InvitedBy       sql.NullString `json:"invited_by"`
	GroupName       string         `json:"group_name"`
	PreferredLang   string         `json:"preferred_lang"`
	PresetID        sql.NullString `json:"preset_id"`
	AccessExpiresAt sql.NullString `json:"access_expires_at"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// GetUserByID récupère un utilisateur par son ID local.
func (db *DB) GetUserByID(ctx context.Context, id int64) (*User, error) {
	query := `SELECT id, authentik_id, jellyfin_id, username, email, email_verified, is_active, is_banned, can_invite, COALESCE(invited_by, ''), group_name, preferred_lang, preset_id, access_expires_at, created_at, updated_at FROM users WHERE id = ?`
	return db.queryUserRow(ctx, query, id)
}

// GetUserByAuthentikID récupère un utilisateur par son UUID Authentik ('sub').
func (db *DB) GetUserByAuthentikID(ctx context.Context, authentikID string) (*User, error) {
	if strings.TrimSpace(authentikID) == "" {
		return nil, sql.ErrNoRows
	}
	query := `SELECT id, authentik_id, jellyfin_id, username, email, email_verified, is_active, is_banned, can_invite, COALESCE(invited_by, ''), group_name, preferred_lang, preset_id, access_expires_at, created_at, updated_at FROM users WHERE authentik_id = ?`
	return db.queryUserRow(ctx, query, strings.TrimSpace(authentikID))
}

// GetUserByUsername récupère un utilisateur par son nom d'utilisateur.
func (db *DB) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	if strings.TrimSpace(username) == "" {
		return nil, sql.ErrNoRows
	}
	query := `SELECT id, authentik_id, jellyfin_id, username, email, email_verified, is_active, is_banned, can_invite, COALESCE(invited_by, ''), group_name, preferred_lang, preset_id, access_expires_at, created_at, updated_at FROM users WHERE LOWER(username) = LOWER(?)`
	return db.queryUserRow(ctx, query, strings.TrimSpace(username))
}

// GetUserByEmail récupère un utilisateur par son email.
func (db *DB) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	if strings.TrimSpace(email) == "" {
		return nil, sql.ErrNoRows
	}
	query := `SELECT id, authentik_id, jellyfin_id, username, email, email_verified, is_active, is_banned, can_invite, COALESCE(invited_by, ''), group_name, preferred_lang, preset_id, access_expires_at, created_at, updated_at FROM users WHERE LOWER(email) = LOWER(?)`
	return db.queryUserRow(ctx, query, strings.TrimSpace(email))
}

// LinkAuthentikID associe l'UUID Authentik à un compte JellyGate existant.
func (db *DB) LinkAuthentikID(ctx context.Context, userID int64, authentikID string) error {
	query := `UPDATE users SET authentik_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	if db.IsSQLite() {
		query = `UPDATE users SET authentik_id = ?, updated_at = datetime('now') WHERE id = ?`
	}
	_, err := db.ExecContext(ctx, query, strings.TrimSpace(authentikID), userID)
	return err
}

// SyncOIDCUser gère le JIT d'un utilisateur authentifié par OIDC.
func (db *DB) SyncOIDCUser(ctx context.Context, authentikID, username, email string) (*User, error) {
	authentikID = strings.TrimSpace(authentikID)
	username = strings.TrimSpace(username)
	email = strings.TrimSpace(email)

	if authentikID == "" {
		return nil, errors.New("authentik_id cannot be empty")
	}
	if username == "" {
		return nil, errors.New("username cannot be empty")
	}

	// 1. Recherche par authentik_id
	user, err := db.GetUserByAuthentikID(ctx, authentikID)
	if err == nil && user != nil {
		if user.Username != username || user.Email != email {
			query := `UPDATE users SET username = ?, email = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
			if db.IsSQLite() {
				query = `UPDATE users SET username = ?, email = ?, updated_at = datetime('now') WHERE id = ?`
			}
			_, _ = db.ExecContext(ctx, query, username, email, user.ID)
			user.Username = username
			user.Email = email
		}
		return user, nil
	}

	// 2. Rapprochement par nom d'utilisateur (comptes migrés)
	user, err = db.GetUserByUsername(ctx, username)
	if err == nil && user != nil {
		_ = db.LinkAuthentikID(ctx, user.ID, authentikID)
		if email != "" && user.Email != email {
			query := `UPDATE users SET email = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
			if db.IsSQLite() {
				query = `UPDATE users SET email = ?, updated_at = datetime('now') WHERE id = ?`
			}
			_, _ = db.ExecContext(ctx, query, email, user.ID)
			user.Email = email
		}
		user.AuthentikID = sql.NullString{String: authentikID, Valid: true}
		return user, nil
	}

	// 3. Rapprochement par email
	if email != "" {
		user, err = db.GetUserByEmail(ctx, email)
		if err == nil && user != nil {
			_ = db.LinkAuthentikID(ctx, user.ID, authentikID)
			user.AuthentikID = sql.NullString{String: authentikID, Valid: true}
			return user, nil
		}
	}

	// 4. Inscription nouveau compte JIT
	insertQuery := `INSERT INTO users (authentik_id, username, email, email_verified, is_active) VALUES (?, ?, ?, 1, 1)`
	res, err := db.ExecContext(ctx, insertQuery, authentikID, username, email)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC user: %w", err)
	}

	newID, err := res.LastInsertId()
	if err != nil || newID <= 0 {
		user, err = db.GetUserByAuthentikID(ctx, authentikID)
	} else {
		user, err = db.GetUserByID(ctx, newID)
	}

	if err == nil && user != nil {
		_ = db.ReconcileReferralForOIDCUser(ctx, user.ID, authentikID, email)
	}

	return user, err
}

func (db *DB) queryUserRow(ctx context.Context, query string, args ...interface{}) (*User, error) {
	row := db.QueryRowContext(ctx, query, args...)
	var u User
	var createdAtRaw, updatedAtRaw interface{}
	err := row.Scan(
		&u.ID,
		&u.AuthentikID,
		&u.JellyfinID,
		&u.Username,
		&u.Email,
		&u.EmailVerified,
		&u.IsActive,
		&u.IsBanned,
		&u.CanInvite,
		&u.InvitedBy,
		&u.GroupName,
		&u.PreferredLang,
		&u.PresetID,
		&u.AccessExpiresAt,
		&createdAtRaw,
		&updatedAtRaw,
	)
	if err != nil {
		return nil, err
	}
	u.CreatedAt = parseDBTime(createdAtRaw)
	u.UpdatedAt = parseDBTime(updatedAtRaw)
	return &u, nil
}

func parseDBTime(val interface{}) time.Time {
	if val == nil {
		return time.Now()
	}
	switch v := val.(type) {
	case time.Time:
		return v
	case string:
		t, _ := time.Parse("2006-01-02 15:04:05", v)
		return t
	case []byte:
		t, _ := time.Parse("2006-01-02 15:04:05", string(v))
		return t
	default:
		return time.Now()
	}
}
