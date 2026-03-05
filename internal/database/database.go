// Package database gère la connexion SQLite et les migrations automatiques
// pour JellyGate.
//
// Utilise modernc.org/sqlite (pure Go, sans CGO) pour une compatibilité
// maximale et un déploiement simplifié sous Docker.
//
// Schéma : users, invitations, password_resets, settings, audit_log.
// Note : Pas de table admin_users — l’authentification admin est déléguée à Jellyfin.
package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	// Driver SQLite pure Go (sans CGO) — import anonyme pour enregistrer le driver.
	_ "modernc.org/sqlite"
)

// DB encapsule la connexion SQLite et expose les méthodes d'accès aux données.
type DB struct {
	conn *sql.DB
	path string
}

// New crée une nouvelle instance DB, ouvre le fichier SQLite dans dataDir,
// configure les paramètres de performance, et exécute les migrations.
//
// Le fichier sera créé à : <dataDir>/jellygate.db
func New(dataDir string) (*DB, error) {
	// S'assurer que le répertoire de données existe
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("impossible de créer le répertoire de données %q: %w", dataDir, err)
	}

	dbPath := filepath.Join(dataDir, "jellygate.db")

	// Ouvrir la connexion SQLite.
	// Le driver "sqlite" est enregistré par l'import de modernc.org/sqlite.
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("impossible d'ouvrir la base de données %q: %w", dbPath, err)
	}

	// ── Paramètres de performance SQLite ─────────────────────────────────
	// WAL = Write-Ahead Logging : meilleures performances en écriture concurrente
	// journal_mode=WAL permet les lectures pendant les écritures
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-64000", // 64 Mo de cache
		"PRAGMA busy_timeout=5000", // 5s de timeout en cas de lock
		"PRAGMA foreign_keys=ON",   // Activer les clés étrangères
		"PRAGMA temp_store=MEMORY", // Stocker les tables temporaires en RAM
	}

	for _, pragma := range pragmas {
		if _, err := conn.Exec(pragma); err != nil {
			conn.Close()
			return nil, fmt.Errorf("erreur lors de l'exécution de %q: %w", pragma, err)
		}
	}

	// ── Pool de connexions ──────────────────────────────────────────────
	// SQLite ne supporte qu'une seule connexion en écriture à la fois.
	// On limite les connexions pour éviter les erreurs "database is locked".
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	conn.SetConnMaxLifetime(0) // Pas d'expiration

	// Vérifier que la connexion fonctionne
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("impossible de ping la base de données: %w", err)
	}

	db := &DB{
		conn: conn,
		path: dbPath,
	}

	// Exécuter les migrations
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("erreur lors des migrations: %w", err)
	}

	return db, nil
}

// Close ferme proprement la connexion à la base de données.
func (db *DB) Close() error {
	slog.Info("Fermeture de la base de données", "path", db.path)
	return db.conn.Close()
}

// Path renvoie le chemin absolu du fichier SQLite.
func (db *DB) Path() string {
	return db.path
}

// Conn renvoie la connexion SQL brute pour les requêtes directes.
// À utiliser avec précaution — préférer les méthodes typées.
func (db *DB) Conn() *sql.DB {
	return db.conn
}

// ── Migrations ──────────────────────────────────────────────────────────────

// migrate exécute les migrations de schéma dans l'ordre.
// Chaque migration est idempotente (IF NOT EXISTS) et peut être rejouée.
func (db *DB) migrate() error {
	slog.Info("Exécution des migrations de la base de données...")

	migrations := []struct {
		name string
		sql  string
	}{
		{
			name: "create_users",
			sql: `CREATE TABLE IF NOT EXISTS users (
				id                INTEGER PRIMARY KEY AUTOINCREMENT,
				jellyfin_id       TEXT    UNIQUE,
				username          TEXT    UNIQUE NOT NULL,
				email             TEXT,
				ldap_dn           TEXT,
				invited_by        TEXT,
				is_active         BOOLEAN NOT NULL DEFAULT 1,
				is_banned         BOOLEAN NOT NULL DEFAULT 0,
				can_invite        BOOLEAN NOT NULL DEFAULT 0,
				access_expires_at DATETIME,
				expiry_action     TEXT    NOT NULL DEFAULT 'disable',
				expiry_delete_after_days INTEGER NOT NULL DEFAULT 0,
				expired_at        DATETIME,
				created_at        DATETIME NOT NULL DEFAULT (datetime('now')),
				updated_at        DATETIME NOT NULL DEFAULT (datetime('now'))
			)`,
		},
		{
			name: "create_invitations",
			sql: `CREATE TABLE IF NOT EXISTS invitations (
				id               INTEGER PRIMARY KEY AUTOINCREMENT,
				code             TEXT    UNIQUE NOT NULL,
				label            TEXT,
				max_uses         INTEGER NOT NULL DEFAULT 1,
				used_count       INTEGER NOT NULL DEFAULT 0,
				jellyfin_profile TEXT,
				expires_at       DATETIME,
				created_by       TEXT,
				created_at       DATETIME NOT NULL DEFAULT (datetime('now'))
			)`,
		},
		{
			name: "create_password_resets",
			sql: `CREATE TABLE IF NOT EXISTS password_resets (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				code       TEXT    UNIQUE NOT NULL,
				expires_at DATETIME NOT NULL,
				used       BOOLEAN NOT NULL DEFAULT 0,
				created_at DATETIME NOT NULL DEFAULT (datetime('now'))
			)`,
		},

		{
			name: "create_settings",
			sql: `CREATE TABLE IF NOT EXISTS settings (
				key        TEXT PRIMARY KEY,
				value      TEXT,
				updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
			)`,
		},
		{
			name: "create_audit_log",
			sql: `CREATE TABLE IF NOT EXISTS audit_log (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				action     TEXT NOT NULL,
				actor      TEXT,
				target     TEXT,
				details    TEXT,
				created_at DATETIME NOT NULL DEFAULT (datetime('now'))
			)`,
		},
		{
			name: "index_invitations_code",
			sql:  `CREATE INDEX IF NOT EXISTS idx_invitations_code ON invitations(code)`,
		},
		{
			name: "index_password_resets_code",
			sql:  `CREATE INDEX IF NOT EXISTS idx_password_resets_code ON password_resets(code)`,
		},
		{
			name: "index_audit_log_action",
			sql:  `CREATE INDEX IF NOT EXISTS idx_audit_log_action ON audit_log(action)`,
		},
		{
			name: "index_audit_log_created_at",
			sql:  `CREATE INDEX IF NOT EXISTS idx_audit_log_created_at ON audit_log(created_at)`,
		},
		{
			name: "create_user_messages",
			sql: `CREATE TABLE IF NOT EXISTS user_messages (
				id              INTEGER PRIMARY KEY AUTOINCREMENT,
				title           TEXT NOT NULL,
				body            TEXT NOT NULL,
				created_by      TEXT,
				target_group    TEXT NOT NULL DEFAULT 'all',
				target_user_ids TEXT NOT NULL DEFAULT '',
				channels        TEXT NOT NULL DEFAULT 'in_app',
				is_campaign     BOOLEAN NOT NULL DEFAULT 0,
				starts_at       DATETIME,
				ends_at         DATETIME,
				sent_at         DATETIME,
				created_at      DATETIME NOT NULL DEFAULT (datetime('now'))
			)`,
		},
		{
			name: "create_user_message_reads",
			sql: `CREATE TABLE IF NOT EXISTS user_message_reads (
				message_id  INTEGER NOT NULL REFERENCES user_messages(id) ON DELETE CASCADE,
				user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				read_at     DATETIME NOT NULL DEFAULT (datetime('now')),
				PRIMARY KEY (message_id, user_id)
			)`,
		},
		{
			name: "create_scheduled_tasks",
			sql: `CREATE TABLE IF NOT EXISTS scheduled_tasks (
				id          INTEGER PRIMARY KEY AUTOINCREMENT,
				name        TEXT NOT NULL,
				task_type   TEXT NOT NULL,
				enabled     BOOLEAN NOT NULL DEFAULT 1,
				hour        INTEGER NOT NULL DEFAULT 3,
				minute      INTEGER NOT NULL DEFAULT 0,
				payload     TEXT,
				last_run_at DATETIME,
				created_by  TEXT,
				created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
				updated_at  DATETIME NOT NULL DEFAULT (datetime('now'))
			)`,
		},
		{
			name: "index_user_messages_created_at",
			sql:  `CREATE INDEX IF NOT EXISTS idx_user_messages_created_at ON user_messages(created_at)`,
		},
		{
			name: "index_scheduled_tasks_enabled",
			sql:  `CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_enabled ON scheduled_tasks(enabled, hour, minute)`,
		},
	}

	for _, m := range migrations {
		start := time.Now()
		if _, err := db.conn.Exec(m.sql); err != nil {
			return fmt.Errorf("migration %q échouée: %w", m.name, err)
		}
		slog.Debug("Migration exécutée", "name", m.name, "duration", time.Since(start))
	}

	// Retro-compatibilité logic: Add can_invite to existing tables.
	// We ignore the error since it will fail cleanly if the column already exists.
	_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN can_invite BOOLEAN NOT NULL DEFAULT 0`)
	_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN preferred_lang TEXT NOT NULL DEFAULT ''`)
	_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN notify_expiry_reminder BOOLEAN NOT NULL DEFAULT 1`)
	_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN notify_account_events BOOLEAN NOT NULL DEFAULT 1`)
	_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN contact_discord TEXT NOT NULL DEFAULT ''`)
	_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN contact_telegram TEXT NOT NULL DEFAULT ''`)
	_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN opt_in_email BOOLEAN NOT NULL DEFAULT 1`)
	_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN opt_in_discord BOOLEAN NOT NULL DEFAULT 0`)
	_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN opt_in_telegram BOOLEAN NOT NULL DEFAULT 0`)
	_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN expiry_action TEXT NOT NULL DEFAULT 'disable'`)
	_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN expiry_delete_after_days INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN expired_at DATETIME`)

	slog.Info("Migrations terminées", "count", len(migrations))
	return nil
}

// ── Méthodes utilitaires d'audit ────────────────────────────────────────────

// LogAction enregistre une action dans le journal d'audit.
//
// Paramètres :
//   - action : type d'action (ex: "user.created", "invite.used", "admin.login")
//   - actor  : qui a effectué l'action (username ou "system")
//   - target : sur qui/quoi porte l'action
//   - details: détails supplémentaires (JSON ou texte libre)
func (db *DB) LogAction(action, actor, target, details string) error {
	_, err := db.conn.Exec(
		`INSERT INTO audit_log (action, actor, target, details) VALUES (?, ?, ?, ?)`,
		action, actor, target, details,
	)
	if err != nil {
		slog.Error("Erreur lors de l'écriture dans le journal d'audit",
			"action", action,
			"actor", actor,
			"error", err,
		)
	}
	return err
}
