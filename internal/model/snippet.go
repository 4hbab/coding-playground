// Package model defines the data structures used throughout the application.
// In Go, we use structs to represent our data — similar to classes in other languages,
// but without inheritance. Go favours composition over inheritance.
package model

import "time"

// Snippet represents a saved code snippet.
// The `json:"..."` tags tell Go's encoding/json package how to serialize/deserialize
// this struct to/from JSON. This is called a "struct tag" — metadata attached to fields.
//
// For example, when we marshal a Snippet to JSON:
//
//	snippet := Snippet{ID: "abc", Name: "hello"}
//	json.Marshal(snippet) → {"id":"abc","name":"hello",...}
type Snippet struct {
	ID          string `json:"id"          db:"id"`
	Name        string `json:"name"        db:"name"`
	Code        string `json:"code"        db:"code"`
	Description string `json:"description" db:"description"`
	// UserID is nil for anonymous snippets. When a logged-in user creates a snippet,
	// this is set to their internal user ID. Only the owner may update or delete.
	UserID    *string   `json:"userId,omitempty" db:"user_id"`
	CreatedAt time.Time `json:"createdAt"   db:"created_at"`
	UpdatedAt time.Time `json:"updatedAt"   db:"updated_at"`
}
