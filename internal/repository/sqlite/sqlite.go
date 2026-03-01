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
//  1. sql.Open(driverName, dataSourceName) → creates a pool
//  2. db.QueryContext / db.ExecContext     → runs queries
//  3. rows.Scan(&field1, &field2)          → reads results into Go variables
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

// migrate runs all database migrations.
//
// MIGRATIONS IN PRODUCTION:
// For a learning project, embedding SQL as string constants is fine.
// In production, you'd use golang-migrate which tracks which migrations have run.
//
// For now, CREATE TABLE IF NOT EXISTS is safe — it won't error if the table exists.
func (db *DB) migrate() error {
	// Phase 1: snippets table
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
	if err != nil {
		return fmt.Errorf("creating snippets table: %w", err)
	}

	// Phase 3: users table
	// github_id is UNIQUE — each GitHub account maps to exactly one row.
	_, err = db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id         TEXT PRIMARY KEY,
			github_id  INTEGER NOT NULL UNIQUE,
			login      TEXT NOT NULL,
			email      TEXT NOT NULL DEFAULT '',
			avatar_url TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("creating users table: %w", err)
	}

	// Phase 3: add user_id column to snippets (idempotent — safe on existing DBs).
	// ALTER TABLE errors if the column exists, so we check pragma_table_info first.
	if err := db.addColumnIfNotExists("snippets", "user_id",
		"TEXT REFERENCES users(id)"); err != nil {
		return fmt.Errorf("adding user_id to snippets: %w", err)
	}

	_, err = db.conn.Exec(`
		CREATE INDEX IF NOT EXISTS idx_snippets_user_id ON snippets(user_id);
	`)
	if err != nil {
		return fmt.Errorf("creating snippets user_id index: %w", err)
	}

	return nil
}

// addColumnIfNotExists adds a column to a table only if it doesn't already exist.
// Makes ALTER TABLE migrations idempotent — safe to run multiple times.
func (db *DB) addColumnIfNotExists(table, column, definition string) error {
	var count int
	err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`,
		table, column,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("checking column %s.%s: %w", table, column, err)
	}
	if count > 0 {
		return nil // column already exists
	}
	_, err = db.conn.Exec(fmt.Sprintf(
		`ALTER TABLE %s ADD COLUMN %s %s`, table, column, definition,
	))
	return err
}
