// Package database gère la connexion SQL (SQLite ou PostgreSQL)
// et les migrations automatiques de JellyGate.
package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/maelmoreau21/JellyGate/internal/config"

	// Driver PostgreSQL (database/sql)
	_ "github.com/jackc/pgx/v5/stdlib"
	// Driver SQLite pure Go (sans CGO)
	_ "modernc.org/sqlite"
)

const (
	DialectSQLite   = "sqlite"
	DialectPostgres = "postgres"
)

// DB encapsule la connexion SQL et expose les méthodes d'accès aux données.
type DB struct {
	conn   *sql.DB
	path   string
	driver string
}

// Rows encapsule sql.Rows avec un Scan tolerant aux valeurs temporelles.
type Rows struct {
	inner *sql.Rows
}

// Row encapsule sql.Row avec un Scan tolerant aux valeurs temporelles.
type Row struct {
	inner *sql.Row
}

// Tx encapsule une transaction SQL et applique l'adaptation de dialecte.
type Tx struct {
	db    *DB
	inner *sql.Tx
}

// New crée une nouvelle instance DB et exécute les migrations.
func New(dbCfg config.DatabaseConfig, dataDir string) (*DB, error) {
	driver := strings.TrimSpace(strings.ToLower(dbCfg.Type))
	if driver == "" {
		driver = DialectSQLite
	}

	var (
		conn *sql.DB
		err  error
		path string
	)

	switch driver {
	case DialectSQLite:
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return nil, fmt.Errorf("impossible de creer le repertoire de donnees %q: %w", dataDir, err)
		}

		path = filepath.Join(dataDir, "jellygate.db")
		conn, err = sql.Open("sqlite", path)
		if err != nil {
			return nil, fmt.Errorf("impossible d'ouvrir la base SQLite %q: %w", path, err)
		}

		pragmas := []string{
			"PRAGMA journal_mode=WAL",
			"PRAGMA synchronous=NORMAL",
			"PRAGMA cache_size=-64000",
			"PRAGMA busy_timeout=5000",
			"PRAGMA foreign_keys=ON",
			"PRAGMA temp_store=MEMORY",
		}
		for _, pragma := range pragmas {
			if _, err := conn.Exec(pragma); err != nil {
				_ = conn.Close()
				return nil, fmt.Errorf("erreur lors de l'execution de %q: %w", pragma, err)
			}
		}

		// SQLite: une seule connexion en ecriture.
		conn.SetMaxOpenConns(1)
		conn.SetMaxIdleConns(1)
		conn.SetConnMaxLifetime(0)

	case DialectPostgres:
		dsn, err := buildPostgresDSN(dbCfg)
		if err != nil {
			return nil, err
		}

		conn, err = sql.Open("pgx", dsn)
		if err != nil {
			return nil, fmt.Errorf("impossible d'ouvrir la base PostgreSQL: %w", err)
		}

		conn.SetMaxOpenConns(25)
		conn.SetMaxIdleConns(5)
		conn.SetConnMaxLifetime(30 * time.Minute)

	default:
		return nil, fmt.Errorf("type de base non supporte: %s", driver)
	}

	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("impossible de ping la base de donnees: %w", err)
	}

	db := &DB{conn: conn, path: path, driver: driver}
	if err := db.migrate(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("erreur lors des migrations: %w", err)
	}

	return db, nil
}

func buildPostgresDSN(cfg config.DatabaseConfig) (string, error) {
	host := strings.TrimSpace(cfg.Host)
	user := strings.TrimSpace(cfg.User)
	name := strings.TrimSpace(cfg.Name)
	sslMode := strings.TrimSpace(cfg.SSLMode)
	if sslMode == "" {
		sslMode = "disable"
	}
	if host == "" || user == "" || name == "" {
		return "", fmt.Errorf("configuration postgres incomplete (DB_HOST, DB_USER, DB_NAME)")
	}

	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, cfg.Password),
		Host:   fmt.Sprintf("%s:%d", host, cfg.Port),
		Path:   "/" + name,
	}
	q := u.Query()
	q.Set("sslmode", sslMode)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Close ferme proprement la connexion à la base de données.
func (db *DB) Close() error {
	slog.Info("Fermeture de la base de donnees", "driver", db.driver, "path", db.path)
	return db.conn.Close()
}

// Path renvoie le chemin du fichier SQLite. Vide en mode PostgreSQL.
func (db *DB) Path() string { return db.path }

// Driver renvoie le dialecte actif: sqlite ou postgres.
func (db *DB) Driver() string { return db.driver }

func (db *DB) IsSQLite() bool { return db.driver == DialectSQLite }

func (db *DB) IsPostgres() bool { return db.driver == DialectPostgres }

// Conn renvoie la connexion SQL brute (a eviter hors cas specifiques).
func (db *DB) Conn() *sql.DB { return db.conn }

// Exec exécute une requête en adaptant le dialecte SQL si nécessaire.
func (db *DB) Exec(query string, args ...interface{}) (sql.Result, error) {
	return db.conn.Exec(db.prepareQuery(query), args...)
}

// Query exécute une requête SELECT en adaptant le dialecte SQL.
func (db *DB) Query(query string, args ...interface{}) (*Rows, error) {
	rows, err := db.conn.Query(db.prepareQuery(query), args...)
	if err != nil {
		return nil, err
	}
	return &Rows{inner: rows}, nil
}

// QueryRow exécute une requête SELECT avec un seul résultat.
func (db *DB) QueryRow(query string, args ...interface{}) *Row {
	return &Row{inner: db.conn.QueryRow(db.prepareQuery(query), args...)}
}

// Begin démarre une transaction SQL adaptée au dialecte actif.
func (db *DB) Begin() (*Tx, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, err
	}
	return &Tx{db: db, inner: tx}, nil
}

func (db *DB) prepareQuery(query string) string {
	if !db.IsPostgres() {
		return query
	}

	q := query
	q = strings.ReplaceAll(q, "datetime('now', '-24 hours')", "(CURRENT_TIMESTAMP - INTERVAL '24 hours')")
	q = strings.ReplaceAll(q, "datetime('now')", "CURRENT_TIMESTAMP")
	q = rewriteInsertOrIgnore(q)
	q = rebindPostgresPlaceholders(q)
	return q
}

func rewriteInsertOrIgnore(query string) string {
	trimmedUpper := strings.ToUpper(strings.TrimSpace(query))
	if !strings.HasPrefix(trimmedUpper, "INSERT OR IGNORE INTO") {
		return query
	}

	q := strings.Replace(query, "INSERT OR IGNORE INTO", "INSERT INTO", 1)
	if strings.Contains(strings.ToUpper(q), "ON CONFLICT") {
		return q
	}

	trimmed := strings.TrimSpace(q)
	if strings.HasSuffix(trimmed, ";") {
		trimmed = strings.TrimSuffix(trimmed, ";")
		return trimmed + " ON CONFLICT DO NOTHING;"
	}
	return q + " ON CONFLICT DO NOTHING"
}

func rebindPostgresPlaceholders(query string) string {
	var b strings.Builder
	b.Grow(len(query) + 16)

	argIndex := 1
	inSingle := false

	for i := 0; i < len(query); i++ {
		ch := query[i]
		if ch == '\'' {
			b.WriteByte(ch)
			if inSingle {
				if i+1 < len(query) && query[i+1] == '\'' {
					b.WriteByte(query[i+1])
					i++
					continue
				}
				inSingle = false
				continue
			}
			inSingle = true
			continue
		}

		if ch == '?' && !inSingle {
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(argIndex))
			argIndex++
			continue
		}

		b.WriteByte(ch)
	}

	return b.String()
}

func scanWithAdaptedDest(scan func(dest ...interface{}) error, dest ...interface{}) error {
	scanDest := make([]interface{}, len(dest))
	post := make([]func() error, 0, len(dest))

	for i := range dest {
		switch target := dest[i].(type) {
		case *sql.NullString:
			cell := new(interface{})
			scanDest[i] = cell
			post = append(post, func(dst *sql.NullString, src *interface{}) func() error {
				return func() error { return assignNullString(dst, *src) }
			}(target, cell))
		case *string:
			cell := new(interface{})
			scanDest[i] = cell
			post = append(post, func(dst *string, src *interface{}) func() error {
				return func() error { return assignString(dst, *src) }
			}(target, cell))
		default:
			scanDest[i] = dest[i]
		}
	}

	if err := scan(scanDest...); err != nil {
		return err
	}

	for _, fn := range post {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}

func assignNullString(dst *sql.NullString, src interface{}) error {
	if dst == nil {
		return nil
	}
	if src == nil {
		dst.String = ""
		dst.Valid = false
		return nil
	}

	s, err := stringifyValue(src)
	if err != nil {
		return err
	}
	dst.String = s
	dst.Valid = true
	return nil
}

func assignString(dst *string, src interface{}) error {
	if dst == nil {
		return nil
	}
	if src == nil {
		*dst = ""
		return nil
	}

	s, err := stringifyValue(src)
	if err != nil {
		return err
	}
	*dst = s
	return nil
}

func stringifyValue(src interface{}) (string, error) {
	switch v := src.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case time.Time:
		return v.Format("2006-01-02 15:04:05"), nil
	case fmt.Stringer:
		return v.String(), nil
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	default:
		return fmt.Sprintf("%v", src), nil
	}
}

// Next avance au résultat suivant.
func (r *Rows) Next() bool { return r.inner.Next() }

// Close ferme le curseur.
func (r *Rows) Close() error { return r.inner.Close() }

// Err retourne l'erreur de parcours éventuelle.
func (r *Rows) Err() error { return r.inner.Err() }

// Scan lit la ligne courante avec adaptation des valeurs temporelles.
func (r *Rows) Scan(dest ...interface{}) error {
	return scanWithAdaptedDest(r.inner.Scan, dest...)
}

// Scan lit la ligne unique avec adaptation des valeurs temporelles.
func (r *Row) Scan(dest ...interface{}) error {
	return scanWithAdaptedDest(r.inner.Scan, dest...)
}

// Exec exécute une requête dans la transaction.
func (tx *Tx) Exec(query string, args ...interface{}) (sql.Result, error) {
	return tx.inner.Exec(tx.db.prepareQuery(query), args...)
}

// Query exécute une requête SELECT dans la transaction.
func (tx *Tx) Query(query string, args ...interface{}) (*Rows, error) {
	rows, err := tx.inner.Query(tx.db.prepareQuery(query), args...)
	if err != nil {
		return nil, err
	}
	return &Rows{inner: rows}, nil
}

// QueryRow exécute une requête SELECT (une ligne) dans la transaction.
func (tx *Tx) QueryRow(query string, args ...interface{}) *Row {
	return &Row{inner: tx.inner.QueryRow(tx.db.prepareQuery(query), args...)}
}

func (tx *Tx) Commit() error   { return tx.inner.Commit() }
func (tx *Tx) Rollback() error { return tx.inner.Rollback() }

// ── Migrations ─────────────────────────────────────────────────────────────

type migration struct {
	name string
	sql  string
}

func (db *DB) migrate() error {
	slog.Info("Execution des migrations de la base de donnees...", "driver", db.driver)

	migrations := db.sqliteMigrations()
	if db.IsPostgres() {
		migrations = db.postgresMigrations()
	}

	for _, m := range migrations {
		start := time.Now()
		if _, err := db.conn.Exec(m.sql); err != nil {
			return fmt.Errorf("migration %q echouee: %w", m.name, err)
		}
		slog.Debug("Migration executee", "name", m.name, "duration", time.Since(start))
	}

	if db.IsSQLite() {
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN can_invite BOOLEAN NOT NULL DEFAULT 0`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN preferred_lang TEXT NOT NULL DEFAULT ''`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN notify_expiry_reminder BOOLEAN NOT NULL DEFAULT 1`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN notify_account_events BOOLEAN NOT NULL DEFAULT 1`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN contact_discord TEXT NOT NULL DEFAULT ''`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN contact_telegram TEXT NOT NULL DEFAULT ''`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN opt_in_email BOOLEAN NOT NULL DEFAULT 1`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN opt_in_discord BOOLEAN NOT NULL DEFAULT 0`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN opt_in_telegram BOOLEAN NOT NULL DEFAULT 0`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN email_verified BOOLEAN NOT NULL DEFAULT 0`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN pending_email TEXT NOT NULL DEFAULT ''`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN email_verification_sent_at DATETIME`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN group_name TEXT NOT NULL DEFAULT ''`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN delete_at DATETIME`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN expiry_action TEXT NOT NULL DEFAULT 'disable'`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN expiry_delete_after_days INTEGER NOT NULL DEFAULT 0`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN expired_at DATETIME`)
	} else {
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS can_invite BOOLEAN NOT NULL DEFAULT FALSE`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS preferred_lang TEXT NOT NULL DEFAULT ''`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS notify_expiry_reminder BOOLEAN NOT NULL DEFAULT TRUE`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS notify_account_events BOOLEAN NOT NULL DEFAULT TRUE`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS contact_discord TEXT NOT NULL DEFAULT ''`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS contact_telegram TEXT NOT NULL DEFAULT ''`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS opt_in_email BOOLEAN NOT NULL DEFAULT TRUE`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS opt_in_discord BOOLEAN NOT NULL DEFAULT FALSE`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS opt_in_telegram BOOLEAN NOT NULL DEFAULT FALSE`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified BOOLEAN NOT NULL DEFAULT FALSE`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS pending_email TEXT NOT NULL DEFAULT ''`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verification_sent_at TIMESTAMPTZ`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS group_name TEXT NOT NULL DEFAULT ''`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS delete_at TIMESTAMPTZ`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS expiry_action TEXT NOT NULL DEFAULT 'disable'`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS expiry_delete_after_days INTEGER NOT NULL DEFAULT 0`)
		_, _ = db.conn.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS expired_at TIMESTAMPTZ`)
	}

	if err := db.backfillExistingEmailVerificationState(); err != nil {
		return fmt.Errorf("backfill email verification historique: %w", err)
	}

	slog.Info("Migrations terminees", "count", len(migrations), "driver", db.driver)
	return nil
}

func (db *DB) backfillExistingEmailVerificationState() error {
	status, err := db.GetSetting(SettingEmailVerificationBackfillV1)
	if err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(status), "done") {
		return nil
	}

	query := `UPDATE users
		SET email_verified = 1, updated_at = datetime('now')
		WHERE email_verified = 0
		  AND TRIM(COALESCE(email, '')) <> ''
		  AND TRIM(COALESCE(pending_email, '')) = ''
		  AND email_verification_sent_at IS NULL`
	if db.IsPostgres() {
		query = `UPDATE users
			SET email_verified = TRUE, updated_at = CURRENT_TIMESTAMP
			WHERE email_verified = FALSE
			  AND BTRIM(COALESCE(email, '')) <> ''
			  AND BTRIM(COALESCE(pending_email, '')) = ''
			  AND email_verification_sent_at IS NULL`
	}

	result, err := db.Exec(query)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if err := db.SetSetting(SettingEmailVerificationBackfillV1, "done"); err != nil {
		return err
	}

	slog.Info("Backfill verification email historique termine", "rows", rowsAffected)
	return nil
}

func (db *DB) sqliteMigrations() []migration {
	return []migration{
		{
			name: "create_users",
			sql: `CREATE TABLE IF NOT EXISTS users (
				id                INTEGER PRIMARY KEY AUTOINCREMENT,
				jellyfin_id       TEXT    UNIQUE,
				username          TEXT    UNIQUE NOT NULL,
				email             TEXT,
				email_verified    BOOLEAN NOT NULL DEFAULT 0,
				pending_email     TEXT    NOT NULL DEFAULT '',
				email_verification_sent_at DATETIME,
				ldap_dn           TEXT,
				group_name        TEXT    NOT NULL DEFAULT '',
				invited_by        TEXT,
				is_active         BOOLEAN NOT NULL DEFAULT 1,
				is_banned         BOOLEAN NOT NULL DEFAULT 0,
				can_invite        BOOLEAN NOT NULL DEFAULT 0,
				access_expires_at DATETIME,
				delete_at         DATETIME,
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
			name: "create_email_verifications",
			sql: `CREATE TABLE IF NOT EXISTS email_verifications (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				email      TEXT    NOT NULL,
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
}

func (db *DB) postgresMigrations() []migration {
	return []migration{
		{
			name: "create_users",
			sql: `CREATE TABLE IF NOT EXISTS users (
				id                BIGSERIAL PRIMARY KEY,
				jellyfin_id       TEXT UNIQUE,
				username          TEXT UNIQUE NOT NULL,
				email             TEXT,
				email_verified    BOOLEAN NOT NULL DEFAULT FALSE,
				pending_email     TEXT NOT NULL DEFAULT '',
				email_verification_sent_at TIMESTAMPTZ,
				ldap_dn           TEXT,
				group_name        TEXT NOT NULL DEFAULT '',
				invited_by        TEXT,
				is_active         BOOLEAN NOT NULL DEFAULT TRUE,
				is_banned         BOOLEAN NOT NULL DEFAULT FALSE,
				can_invite        BOOLEAN NOT NULL DEFAULT FALSE,
				access_expires_at TIMESTAMPTZ,
				delete_at         TIMESTAMPTZ,
				expiry_action     TEXT NOT NULL DEFAULT 'disable',
				expiry_delete_after_days INTEGER NOT NULL DEFAULT 0,
				expired_at        TIMESTAMPTZ,
				created_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
		},
		{
			name: "create_invitations",
			sql: `CREATE TABLE IF NOT EXISTS invitations (
				id               BIGSERIAL PRIMARY KEY,
				code             TEXT UNIQUE NOT NULL,
				label            TEXT,
				max_uses         INTEGER NOT NULL DEFAULT 1,
				used_count       INTEGER NOT NULL DEFAULT 0,
				jellyfin_profile TEXT,
				expires_at       TIMESTAMPTZ,
				created_by       TEXT,
				created_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
		},
		{
			name: "create_password_resets",
			sql: `CREATE TABLE IF NOT EXISTS password_resets (
				id         BIGSERIAL PRIMARY KEY,
				user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				code       TEXT UNIQUE NOT NULL,
				expires_at TIMESTAMPTZ NOT NULL,
				used       BOOLEAN NOT NULL DEFAULT FALSE,
				created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
		},
		{
			name: "create_email_verifications",
			sql: `CREATE TABLE IF NOT EXISTS email_verifications (
				id         BIGSERIAL PRIMARY KEY,
				user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				email      TEXT NOT NULL,
				code       TEXT UNIQUE NOT NULL,
				expires_at TIMESTAMPTZ NOT NULL,
				used       BOOLEAN NOT NULL DEFAULT FALSE,
				created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
		},
		{
			name: "create_settings",
			sql: `CREATE TABLE IF NOT EXISTS settings (
				key        TEXT PRIMARY KEY,
				value      TEXT,
				updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
		},
		{
			name: "create_audit_log",
			sql: `CREATE TABLE IF NOT EXISTS audit_log (
				id         BIGSERIAL PRIMARY KEY,
				action     TEXT NOT NULL,
				actor      TEXT,
				target     TEXT,
				details    TEXT,
				created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
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
				id              BIGSERIAL PRIMARY KEY,
				title           TEXT NOT NULL,
				body            TEXT NOT NULL,
				created_by      TEXT,
				target_group    TEXT NOT NULL DEFAULT 'all',
				target_user_ids TEXT NOT NULL DEFAULT '',
				channels        TEXT NOT NULL DEFAULT 'in_app',
				is_campaign     BOOLEAN NOT NULL DEFAULT FALSE,
				starts_at       TIMESTAMPTZ,
				ends_at         TIMESTAMPTZ,
				sent_at         TIMESTAMPTZ,
				created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
		},
		{
			name: "create_user_message_reads",
			sql: `CREATE TABLE IF NOT EXISTS user_message_reads (
				message_id  BIGINT NOT NULL REFERENCES user_messages(id) ON DELETE CASCADE,
				user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				read_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
				PRIMARY KEY (message_id, user_id)
			)`,
		},
		{
			name: "create_scheduled_tasks",
			sql: `CREATE TABLE IF NOT EXISTS scheduled_tasks (
				id          BIGSERIAL PRIMARY KEY,
				name        TEXT NOT NULL,
				task_type   TEXT NOT NULL,
				enabled     BOOLEAN NOT NULL DEFAULT TRUE,
				hour        INTEGER NOT NULL DEFAULT 3,
				minute      INTEGER NOT NULL DEFAULT 0,
				payload     TEXT,
				last_run_at TIMESTAMPTZ,
				created_by  TEXT,
				created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
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
}

// LogAction enregistre une action dans le journal d'audit.
func (db *DB) LogAction(action, actor, target, details string) error {
	_, err := db.Exec(
		`INSERT INTO audit_log (action, actor, target, details) VALUES (?, ?, ?, ?)`,
		action, actor, target, details,
	)
	if err != nil {
		slog.Error("Erreur lors de l'ecriture dans le journal d'audit",
			"action", action,
			"actor", actor,
			"error", err,
		)
	}
	return err
}
