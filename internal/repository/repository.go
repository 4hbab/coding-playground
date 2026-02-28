package repository

import (
	"context"

	"github.com/sakif/coding-playground/internal/model"
)

type ListOptions struct {
	Limit  int
	Offset int
}

// SnippetRepository defines persistence operations for code snippets.
type SnippetRepository interface {
	Create(ctx context.Context, snippet *model.Snippet) error
	GetByID(ctx context.Context, id string) (*model.Snippet, error)
	List(ctx context.Context, opts ListOptions) ([]model.Snippet, error)
	Update(ctx context.Context, snippet *model.Snippet) error
	Delete(ctx context.Context, id string) error
}

// UserRepository defines persistence operations for user accounts.
//
// WHY Upsert?
// When a user logs in via GitHub OAuth we get their profile data. We don't
// know ahead of time if they've logged in before. Instead of a SELECT + INSERT
// or SELECT + UPDATE, we use a single INSERT ... ON CONFLICT statement that
// either inserts a new row or updates the existing one. This is called an
// "upsert" (update + insert). It's atomic, efficient, and avoids race conditions.
type UserRepository interface {
	// Create inserts a brand-new user row. Returns an error if a user with the
	// same GitHubID already exists. Prefer Upsert for OAuth login flows.
	Create(ctx context.Context, user *model.User) error

	// GetByID retrieves a user by their internal app ID.
	GetByID(ctx context.Context, id string) (*model.User, error)

	// GetByGitHubID retrieves a user by their GitHub numeric user ID.
	// Returns apperror.ErrNotFound if no such user exists.
	GetByGitHubID(ctx context.Context, githubID int64) (*model.User, error)

	// Upsert inserts the user if they don't exist yet, or updates their
	// login, email, and avatar_url if they do. The user.ID field is always
	// set on return (existing ID if found, new xid if created).
	Upsert(ctx context.Context, user *model.User) error
}