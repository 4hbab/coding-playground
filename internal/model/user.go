// Package model defines the data structures used throughout the application.
package model

import "time"

// User represents a registered user account.
//
// We use GitHub OAuth as the identity provider, so the primary external
// identifier is the GitHub user ID (an integer). We still generate our own
// internal string ID (xid) for consistency with Snippet and to avoid tying
// our primary keys to a third-party's numbering scheme.
//
// WHY GitHubID int64?
// GitHub user IDs are integers (e.g. 1234567). Using int64 avoids overflow
// for large GitHub account numbers. The UNIQUE constraint on github_id in the
// DB ensures one GitHub account maps to exactly one app account.
//
// WHY Email string (not *string)?
// GitHub OAuth returns the primary public email, which can be empty if the
// user has hidden it. We use an empty string as the zero value rather than a
// nullable pointer — simpler to work with and safe to display.
type User struct {
	ID        string    `json:"id"        db:"id"`
	GitHubID  int64     `json:"githubId"  db:"github_id"` // GitHub's numeric user ID
	Login     string    `json:"login"     db:"login"`      // GitHub username, e.g. "sakif"
	Email     string    `json:"email"     db:"email"`      // Primary public email (may be empty)
	AvatarURL string    `json:"avatarUrl" db:"avatar_url"` // Profile picture URL
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt time.Time `json:"updatedAt" db:"updated_at"`
}
