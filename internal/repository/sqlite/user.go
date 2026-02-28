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

// UserDB is a separate type that implements repository.UserRepository.
//
// WHY NOT USE *DB DIRECTLY?
// The main *DB struct already implements SnippetRepository, which has methods
// named Create, GetByID, Update, and Delete. UserRepository has the same
// method names. Go does not allow two methods with the same name on the same
// type — the compiler would error with "method redeclared".
//
// The solution is to wrap the same *sql.DB connection in a distinct type (UserDB)
// so its methods live in a separate namespace. You get the UserDB via db.Users().
//
// This pattern is sometimes called "method set separation" or "role interfaces":
// the same underlying resource (the database connection) is exposed through
// different typed facades, each implementing a different interface.
type UserDB struct {
	conn *sql.DB
}

// Users returns a UserDB that implements repository.UserRepository.
// It shares the same underlying *sql.DB connection pool as the parent DB.
//
// Usage in server.go:
//
//	userRepo := db.Users()   // repository.UserRepository
//	snippetRepo := db        // repository.SnippetRepository
func (db *DB) Users() *UserDB {
	return &UserDB{conn: db.conn}
}

// COMPILE-TIME INTERFACE CHECK:
// Verify that *UserDB implements repository.UserRepository.
var _ repository.UserRepository = (*UserDB)(nil)

// Create inserts a brand-new user into the database.
//
// This is used for the first-ever login of a new GitHub account.
// For the normal OAuth login flow, prefer Upsert, which handles both
// new and returning users in a single query.
func (u *UserDB) Create(ctx context.Context, user *model.User) error {
	user.ID = xid.New().String()
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	_, err := u.conn.ExecContext(ctx,
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
		return fmt.Errorf("sqlite: creating user: %w", err)
	}
	return nil
}

// GetByID retrieves a user by their internal app ID (xid string).
//
// Used when the JWT has been validated and we need to load the full user
// profile. The JWT contains only the user.ID, so this is a fast PK lookup.
func (u *UserDB) GetByID(ctx context.Context, id string) (*model.User, error) {
	var usr model.User

	err := u.conn.QueryRowContext(ctx,
		`SELECT id, github_id, login, email, avatar_url, created_at, updated_at
		 FROM users
		 WHERE id = ?`,
		id,
	).Scan(
		&usr.ID,
		&usr.GitHubID,
		&usr.Login,
		&usr.Email,
		&usr.AvatarURL,
		&usr.CreatedAt,
		&usr.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperror.NotFound("user", id)
		}
		return nil, fmt.Errorf("sqlite: getting user %s: %w", id, err)
	}

	return &usr, nil
}

// GetByGitHubID retrieves a user by their GitHub numeric ID.
//
// Called during the OAuth callback to check if the GitHub account is already
// registered. If it returns apperror.ErrNotFound, it's a new user and we
// need to Create() them. For the combined create-or-update flow, use Upsert.
func (u *UserDB) GetByGitHubID(ctx context.Context, githubID int64) (*model.User, error) {
	var usr model.User

	err := u.conn.QueryRowContext(ctx,
		`SELECT id, github_id, login, email, avatar_url, created_at, updated_at
		 FROM users
		 WHERE github_id = ?`,
		githubID,
	).Scan(
		&usr.ID,
		&usr.GitHubID,
		&usr.Login,
		&usr.Email,
		&usr.AvatarURL,
		&usr.CreatedAt,
		&usr.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperror.NotFound("user", fmt.Sprintf("github_id=%d", githubID))
		}
		return nil, fmt.Errorf("sqlite: getting user by github_id %d: %w", githubID, err)
	}

	return &usr, nil
}

// Upsert inserts a new user OR updates an existing one, keyed on github_id.
//
// INSERT OR REPLACE is SQLite's "upsert" syntax. When github_id already exists
// (UNIQUE constraint), it replaces the entire row. We use this at the end of
// every OAuth callback to:
//   - Create the user account on first login
//   - Keep their login, email, and avatar_url up-to-date on subsequent logins
//
// HOW INSERT OR REPLACE WORKS:
// SQLite's INSERT OR REPLACE deletes the conflicting row and re-inserts the new
// one. This means the "id" (our xid) would change on every login if we let
// SQLite generate it. To prevent that, we first try to find the existing ID, and
// pass it back into the INSERT so the row keeps its original ID.
func (u *UserDB) Upsert(ctx context.Context, user *model.User) error {
	now := time.Now()

	// Look up the existing user's internal ID (to preserve it across upserts).
	// If not found, generate a new xid.
	existingID := ""
	var existingCreatedAt time.Time

	row := u.conn.QueryRowContext(ctx,
		`SELECT id, created_at FROM users WHERE github_id = ?`,
		user.GitHubID,
	)
	if err := row.Scan(&existingID, &existingCreatedAt); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("sqlite: upsert lookup for github_id %d: %w", user.GitHubID, err)
	}

	if existingID == "" {
		// Brand-new user — generate a new ID
		existingID = xid.New().String()
		existingCreatedAt = now
	}

	// Set the resolved values back on the struct before writing
	user.ID = existingID
	user.CreatedAt = existingCreatedAt
	user.UpdatedAt = now

	// INSERT OR REPLACE atomically writes the row.
	// If github_id conflicts, the old row is deleted and this one is inserted.
	// Because we preserved the original id and created_at, it looks like an update.
	_, err := u.conn.ExecContext(ctx,
		`INSERT OR REPLACE INTO users
		    (id, github_id, login, email, avatar_url, created_at, updated_at)
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
		return fmt.Errorf("sqlite: upserting user github_id %d: %w", user.GitHubID, err)
	}

	return nil
}
