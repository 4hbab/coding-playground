package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/sakif/coding-playground/internal/model"
)

// Upsert creates or updates a user using GitHub's unique numeric ID for deduplication.
//
// UPSERT PATTERN (INSERT ... ON CONFLICT DO UPDATE):
// If a user with this github_id already exists, we update their profile fields
// (login, email, avatar_url) to stay in sync with GitHub — users can change
// their username/email on GitHub at any time.
func (db *DB) Upsert(ctx context.Context, user *model.User) error {
	now := time.Now()

	_, err := db.conn.ExecContext(ctx,
		`INSERT INTO users (id, github_id, login, email, avatar_url, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(github_id) DO UPDATE SET
		     login      = excluded.login,
		     email      = excluded.email,
		     avatar_url = excluded.avatar_url,
		     updated_at = excluded.updated_at`,
		user.ID, user.GitHubID, user.Login, user.Email, user.AvatarURL, now, now,
	)
	if err != nil {
		return fmt.Errorf("sqlite: upsert user: %w", err)
	}

	// Retrieve the actual row (in case it was an update, the ID is the existing one)
	row := db.conn.QueryRowContext(ctx,
		`SELECT id, created_at, updated_at FROM users WHERE github_id = ?`,
		user.GitHubID,
	)
	return row.Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)
}

// GetUserByID retrieves a user by their internal ID.
func (db *DB) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	row := db.conn.QueryRowContext(ctx,
		`SELECT id, github_id, login, email, avatar_url, created_at, updated_at
		 FROM users WHERE id = ?`, id,
	)

	var user model.User
	err := row.Scan(
		&user.ID, &user.GitHubID, &user.Login, &user.Email,
		&user.AvatarURL, &user.CreatedAt, &user.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: get user by id: %w", err)
	}
	return &user, nil
}
