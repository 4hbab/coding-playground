package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rs/xid"
	"github.com/sakif/coding-playground/internal/apperror"
	"github.com/sakif/coding-playground/internal/model"
	"github.com/sakif/coding-playground/internal/repository"
)

// compile-time check that *DB implements repository.UserRepository
var _ repository.UserRepository = (*DB)(nil)

// Upsert inserts or updates a user based on their GitHub ID.
//
// INSERT OR REPLACE semantics:
// SQLite's "INSERT OR REPLACE" (also written "REPLACE INTO") will:
//   - INSERT a new row if no row with the same UNIQUE key (github_id) exists
//   - DELETE the old row and INSERT a new one if a conflict is found
//
// After the upsert, we SELECT the row back to populate the ID and timestamps.
// This is necessary because the ID is generated here (if new) or already exists
// (if updating), and we need to return the canonical record to the caller.
//
// WHY UPSERT AND NOT INSERT + ON CONFLICT UPDATE?
// Both work. We use INSERT OR REPLACE for clarity — it's the simplest SQLite idiom.
// The downside is that it increments the rowid on every update, but for our scale
// that doesn't matter.
func (db *DB) Upsert(ctx context.Context, user *model.User) error {
	// Generate an ID only for new users. We check by GitHub ID — if a user with
	// this github_id already exists, we want to KEEP their existing internal ID.
	var existingID string
	err := db.conn.QueryRowContext(ctx,
		`SELECT id FROM users WHERE github_id = ?`, user.GitHubID,
	).Scan(&existingID)

	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("sqlite: looking up user by github_id %d: %w", user.GitHubID, err)
	}

	if existingID != "" {
		// User already exists — update their profile in case login/email/avatar changed
		user.ID = existingID
		user.UpdatedAt = time.Now()
		_, err = db.conn.ExecContext(ctx,
			`UPDATE users SET login = ?, email = ?, avatar_url = ?, updated_at = ?
			 WHERE id = ?`,
			user.Login,
			user.Email,
			user.AvatarURL,
			user.UpdatedAt,
			user.ID,
		)
		if err != nil {
			return fmt.Errorf("sqlite: updating user %s: %w", user.ID, err)
		}
	} else {
		// New user — generate an ID and INSERT
		now := time.Now()
		user.ID = xid.New().String()
		user.CreatedAt = now
		user.UpdatedAt = now

		_, err = db.conn.ExecContext(ctx,
			`INSERT INTO users (id, github_id, login, email, avatar_url, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			user.ID,
			user.GitHubID,
			user.Login,
			user.Email,
			user.AvatarURL,
			user.CreatedAt,
			user.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("sqlite: inserting user (githubID=%d): %w", user.GitHubID, err)
		}
	}

	return nil
}

// GetUserByID retrieves a user by their internal ID.
// Returns apperror.ErrNotFound if no user exists with that ID.
func (db *DB) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	var u model.User

	err := db.conn.QueryRowContext(ctx,
		`SELECT id, github_id, login, email, avatar_url, created_at, updated_at
		 FROM users WHERE id = ?`,
		id,
	).Scan(
		&u.ID,
		&u.GitHubID,
		&u.Login,
		&u.Email,
		&u.AvatarURL,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperror.NotFound("user", id)
		}
		return nil, fmt.Errorf("sqlite: getting user %s: %w", id, err)
	}

	return &u, nil
}
