package model

import "time"

// User represents an authenticated user (linked via GitHub OAuth).
type User struct {
	ID        string    `json:"id"        db:"id"`
	GitHubID  int64     `json:"githubId"  db:"github_id"`
	Login     string    `json:"login"     db:"login"`
	Email     string    `json:"email"     db:"email"`
	AvatarURL string    `json:"avatarUrl" db:"avatar_url"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt time.Time `json:"updatedAt" db:"updated_at"`
}
