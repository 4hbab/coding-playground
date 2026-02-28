// Package sqlite implements the repository interfaces using SQLite as the storage backend.
//
// WHY SQLITE?
// SQLite is an embedded database — it lives inside your Go binary as a single file.
// No separate database server to install, configure, or manage. Perfect for:
// - Learning database patterns without infrastructure complexity
// - Single-server deployments (which is most apps, honestly)
// - Development and testing (use ":memory:" for in-memory DB)
//
// WHY modernc.org/sqlite INSTEAD OF github.com/mattn/go-sqlite3?
// mattn/go-sqlite3 uses CGo (calls C code from Go), which means you need a C compiler
// installed and cross-compilation becomes painful. modernc.org/sqlite is a pure Go
// translation of the SQLite C code — no C compiler needed, works everywhere Go works.
//
// DATABASE/SQL OVERVIEW:
// Go's standard library provides "database/sql" — a generic interface for SQL databases.
// It works with any database through "drivers" (SQLite, Postgres, MySQL, etc.).
// Key types:
//   - sql.DB      — a connection pool (NOT a single connection!)
//   - sql.Tx      — a transaction
//   - sql.Row     — a single result row
//   - sql.Rows    — multiple result rows (must be closed!)
//
// The pattern is always:
//   1. sql.Open(driverName, dataSourceName) → creates a pool
//   2. db.QueryContext / db.ExecContext     → runs queries
//   3. rows.Scan(&field1, &field2)          → reads results into Go variables
package sqlite

import (
	"database/sql"
	"fmt"

	// BLANK IMPORT:
	// The underscore import `_ "modernc.org/sqlite"` is a "side-effect only" import.
	// It doesn't give us any symbols to use directly. Instead, the sqlite package's
	// init() function registers itself with database/sql as a driver named "sqlite".
	// After this import, sql.Open("sqlite", ...) knows how to talk to SQLite.
	//
	// This is Go's plugin pattern — database drivers register themselves at init time.
	_ "modernc.org/sqlite"
)

// DB wraps a sql.DB connection pool and provides repository methods.
//
// WHY WRAP sql.DB IN A STRUCT?
// 1. We can attach methods to it (Create, GetByID, etc.)
// 2. We can add more fields later (logger, config, prepared statements)
// 3. It implements the SnippetRepository interface from repository.go
// 4. We control the lifecycle (New creates it, Close destroys it)
type DB struct {
	conn *sql.DB
}

// New creates a new SQLite database connection and runs migrations.
//
// dbPath examples:
//   - "data/playground.db"  → file-based database (persistent)
//   - ":memory:"            → in-memory database (great for tests, lost on close)
//
// CONNECTION POOL:
// sql.Open() does NOT actually open a connection — it just creates a pool manager.
// The first real connection happens when you run your first query.
// We call db.Ping() to force an immediate connection and verify it works.
func New(dbPath string) (*DB, error) {
	// Open a connection pool to the SQLite database.
	// "sqlite" is the driver name registered by the blank import above.
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("sqlite: opening database: %w", err)
	}

	// Ping verifies the connection actually works.
	// Without this, a bad path or permissions issue would only surface
	// on the first query — which is much harder to debug.
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("sqlite: pinging database: %w", err)
	}

	// PRAGMA STATEMENTS:
	// SQLite has special "PRAGMA" commands that configure its behaviour.
	// These run once at connection time.

	// WAL (Write-Ahead Logging) mode:
	// Default SQLite locks the entire database during writes.
	// WAL mode allows concurrent reads WHILE a write is happening.
	// This is critical for a web server where multiple requests hit the DB.
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("sqlite: setting WAL mode: %w", err)
	}

	// Foreign keys are OFF by default in SQLite (for backwards compatibility).
	// We turn them on because we'll want referential integrity later (users → snippets).
	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("sqlite: enabling foreign keys: %w", err)
	}

	db := &DB{conn: conn}

	// Run database migrations to create/update tables
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("sqlite: running migrations: %w", err)
	}

	return db, nil
}

// Close closes the database connection pool.
//
// ALWAYS DEFER CLOSE:
// Wherever you call New(), immediately defer Close():
//
//	db, err := sqlite.New("data/playground.db")
//	if err != nil { ... }
//	defer db.Close()
//
// This ensures the connection is cleaned up even if a panic occurs.
func (db *DB) Close() error {
	return db.conn.Close()
}

// migrate runs all database migrations in version order.
//
// VERSIONED MIGRATIONS:
// We use a `schema_version` table to track which migrations have run.
// Each migration is assigned a version number and only runs once.
// This is a lightweight alternative to golang-migrate — same idea, simpler code.
//
// WHY NOT "CREATE TABLE IF NOT EXISTS" FOR EVERYTHING?
// That works for new tables, but "ALTER TABLE ... ADD COLUMN" errors if the
// column already exists. A version counter solves this cleanly: we only run
// each ALTER once, and subsequent app restarts skip already-applied migrations.
//
// Migration history:
//   v1 — create snippets table (original schema)
//   v2 — create users table + add user_id FK column to snippets
func (db *DB) migrate() error {
	// Bootstrap the schema_version table itself.
	// This is always safe to run — IF NOT EXISTS is idempotent.
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version    INTEGER PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("creating schema_version table: %w", err)
	}

	// Read the highest applied version (0 if no migrations have run yet).
	var currentVersion int
	row := db.conn.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`)
	if err := row.Scan(&currentVersion); err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}

	// Apply migrations in order. Each migration is a function that receives
	// the db connection and returns an error.
	type migration struct {
		version int
		name    string
		up      func() error
	}

	migrations := []migration{
		{
			version: 1,
			name:    "create snippets table",
			up: func() error {
				_, err := db.conn.Exec(`
					CREATE TABLE IF NOT EXISTS snippets (
						id          TEXT PRIMARY KEY,
						name        TEXT NOT NULL,
						code        TEXT NOT NULL DEFAULT '',
						description TEXT NOT NULL DEFAULT '',
						created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
						updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
					);
					CREATE INDEX IF NOT EXISTS idx_snippets_created_at ON snippets(created_at);
				`)
				return err
			},
		},
		{
			version: 2,
			name:    "create users table and add user_id to snippets",
			up: func() error {
				// Users table — stores GitHub OAuth identities.
				// github_id has a UNIQUE constraint so one GitHub account = one app account.
				_, err := db.conn.Exec(`
					CREATE TABLE IF NOT EXISTS users (
						id         TEXT PRIMARY KEY,
						github_id  INTEGER UNIQUE NOT NULL,
						login      TEXT NOT NULL,
						email      TEXT NOT NULL DEFAULT '',
						avatar_url TEXT NOT NULL DEFAULT '',
						created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
						updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
					);
					CREATE INDEX IF NOT EXISTS idx_users_github_id ON users(github_id);
				`)
				if err != nil {
					return fmt.Errorf("creating users table: %w", err)
				}

				// Add user_id as a nullable FK to snippets.
				// Existing rows will get NULL (= anonymous snippet). That's correct.
				// ON DELETE SET NULL: if a user is deleted their snippets survive as anonymous.
				//
				// NOTE: SQLite requires each ALTER TABLE statement to be separate.
				_, err = db.conn.Exec(`
					ALTER TABLE snippets ADD COLUMN user_id TEXT REFERENCES users(id) ON DELETE SET NULL;
				`)
				if err != nil {
					return fmt.Errorf("adding user_id to snippets: %w", err)
				}

				_, err = db.conn.Exec(`
					CREATE INDEX IF NOT EXISTS idx_snippets_user_id ON snippets(user_id);
				`)
				return err
			},
		},
	}

	// Run only the migrations that haven't been applied yet.
	for _, m := range migrations {
		if m.version <= currentVersion {
			continue // already applied
		}

		if err := m.up(); err != nil {
			return fmt.Errorf("migration v%d (%s): %w", m.version, m.name, err)
		}

		// Record that this migration has been applied.
		_, err := db.conn.Exec(
			`INSERT INTO schema_version (version) VALUES (?)`,
			m.version,
		)
		if err != nil {
			return fmt.Errorf("recording migration v%d: %w", m.version, err)
		}
	}

	return nil
}

