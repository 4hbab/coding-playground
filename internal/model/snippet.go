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
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Code      string    `json:"code"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
