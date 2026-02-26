// Package service contains the business logic layer of the application.
//
// THE THREE-LAYER ARCHITECTURE:
// In a well-structured Go web app, code is organised into three layers:
//
//   Handler (HTTP layer)    → parses requests, writes responses
//   Service (Business layer) → validates, enforces rules, orchestrates
//   Repository (Data layer) → reads/writes to the database
//
// WHY A SEPARATE SERVICE LAYER?
// Without a service layer, handlers do everything: parse HTTP, validate data,
// call the database, format responses. This creates several problems:
//
//   1. TESTING: To test business logic, you'd need to create HTTP requests.
//      With a service layer, you test business logic with plain Go function calls.
//
//   2. REUSE: What if you need the same logic in a CLI tool or a background job?
//      Handlers are tied to HTTP. Services are not.
//
//   3. SEPARATION: Handlers should only know about HTTP (status codes, headers, JSON).
//      Services should only know about business rules (validation, permissions).
//      Neither should know about SQL or database details.
//
// THE DEPENDENCY CHAIN:
//   main.go creates:  DB → Repository → Service → Handler
//   At runtime:       Handler calls Service calls Repository calls DB
//
// DEPENDENCY INJECTION:
// Notice that SnippetService takes a repository.SnippetRepository (interface),
// NOT a *sqlite.DB (concrete type). This is called "programming to an interface."
//
// Benefits:
// - TESTING: In tests, we pass a mock repository (see snippet_test.go)
// - FLEXIBILITY: Swap SQLite for Postgres by changing one line in main.go
// - DECOUPLING: The service doesn't import the sqlite package at all
package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/sakif/coding-playground/internal/apperror"
	"github.com/sakif/coding-playground/internal/model"
	"github.com/sakif/coding-playground/internal/repository"
)

// Validation constants.
// Defining these as constants (not magic numbers in code) makes them:
// - Easy to find and change
// - Self-documenting (the name explains the purpose)
// - Referenceable in error messages
const (
	MaxSnippetNameLength = 100
	MaxCodeLength        = 100000 // ~100KB of code
	DefaultListLimit     = 20
	MaxListLimit         = 100
)

// SnippetService handles business logic for code snippets.
//
// STRUCT FIELDS:
// - repo: the database interface (injected, not created here)
// - logger: for structured logging of business events
//
// Both fields are unexported (lowercase) — they're private to this package.
// External code interacts with SnippetService only through its methods.
type SnippetService struct {
	repo   repository.SnippetRepository
	logger *slog.Logger
}

// NewSnippetService creates a new SnippetService.
//
// CONSTRUCTOR PATTERN IN GO:
// Go doesn't have constructors like Java/Python. Instead, we use "New" functions.
// Convention: NewXxx returns *Xxx and takes all dependencies as parameters.
//
// This is where dependency injection happens — the caller decides WHICH
// repository implementation to use (SQLite, Postgres, mock for tests).
func NewSnippetService(repo repository.SnippetRepository, logger *slog.Logger) *SnippetService {
	return &SnippetService{
		repo:   repo,
		logger: logger,
	}
}

// Create validates and saves a new snippet.
//
// IMPORTANT DESIGN DECISIONS:
//
// 1. ACCEPT PRIMITIVES, NOT HTTP TYPES:
//    The method signature is (ctx, name, code, description string), NOT (*http.Request).
//    This means the service has ZERO knowledge of HTTP. You could call it from:
//    - An HTTP handler
//    - A CLI tool
//    - A background job
//    - A gRPC server
//    All without changing this code.
//
// 2. VALIDATE AT THE SERVICE LEVEL:
//    The handler does basic parsing (is the JSON valid?).
//    The service enforces business rules (is the name too long? is it empty?).
//    Why here and not in the handler? Because EVERY caller needs these rules,
//    not just the HTTP handler.
//
// 3. RETURN DOMAIN ERRORS:
//    We return apperror.ValidationFailed, NOT http.StatusBadRequest.
//    The handler translates domain errors to HTTP status codes.
//    This keeps the service layer HTTP-agnostic.
func (s *SnippetService) Create(ctx context.Context, name, code, description string) (*model.Snippet, error) {
	// === VALIDATION ===
	// Trim whitespace first — " hello " becomes "hello"
	name = strings.TrimSpace(name)

	if name == "" {
		return nil, apperror.ValidationFailed("name", "snippet name is required")
	}
	if len(name) > MaxSnippetNameLength {
		return nil, apperror.ValidationFailed("name",
			fmt.Sprintf("snippet name must be %d characters or less", MaxSnippetNameLength))
	}
	if len(code) > MaxCodeLength {
		return nil, apperror.ValidationFailed("code",
			fmt.Sprintf("code must be %d characters or less", MaxCodeLength))
	}

	// === CREATE THE MODEL ===
	// We build the model.Snippet here. The repository will fill in ID and timestamps.
	snippet := &model.Snippet{
		Name:        name,
		Code:        code,
		Description: strings.TrimSpace(description),
	}

	// === DELEGATE TO REPOSITORY ===
	// The repo handles ID generation, timestamps, and SQL.
	// We pass ctx so the operation can be cancelled if the HTTP request is aborted.
	if err := s.repo.Create(ctx, snippet); err != nil {
		s.logger.Error("failed to create snippet",
			slog.String("name", name),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("creating snippet: %w", err)
	}

	s.logger.Info("snippet created",
		slog.String("id", snippet.ID),
		slog.String("name", snippet.Name),
	)

	return snippet, nil
}

// GetByID retrieves a snippet by its ID.
// Returns apperror.ErrNotFound if the snippet doesn't exist.
func (s *SnippetService) GetByID(ctx context.Context, id string) (*model.Snippet, error) {
	// Validate the ID isn't empty — catch obvious mistakes early
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, apperror.ValidationFailed("id", "snippet ID is required")
	}

	snippet, err := s.repo.GetByID(ctx, id)
	if err != nil {
		// Don't log NotFound as an error — it's a normal "not found" response.
		// Only log actual database failures.
		//
		// errors.Is() unwraps the error chain to check if ErrNotFound is anywhere
		// in the chain. This works because our AppError implements Unwrap().
		return nil, err // Let the error propagate (it's already a proper apperror)
	}

	return snippet, nil
}

// List retrieves snippets with pagination.
//
// PAGINATION PARAMETERS:
// - limit: how many items per page (clamped to 1-100, default 20)
// - offset: how many items to skip (for page navigation)
//
// Example: page 3 with 20 items → limit=20, offset=40
// The service enforces sane limits so callers can't request 1 million rows.
func (s *SnippetService) List(ctx context.Context, limit, offset int) ([]model.Snippet, error) {
	// Clamp limit to a sane range
	if limit <= 0 {
		limit = DefaultListLimit
	}
	if limit > MaxListLimit {
		limit = MaxListLimit
	}

	// Offset can't be negative
	if offset < 0 {
		offset = 0
	}

	snippets, err := s.repo.List(ctx, repository.ListOptions{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		s.logger.Error("failed to list snippets", slog.String("error", err.Error()))
		return nil, fmt.Errorf("listing snippets: %w", err)
	}

	return snippets, nil
}

// Update modifies an existing snippet.
//
// STRATEGY: "Fetch then update"
// 1. First, fetch the existing snippet (to confirm it exists)
// 2. Apply changes to the fetched copy
// 3. Save the updated version
//
// This is safer than a blind UPDATE because:
// - We can validate the new values against the old ones if needed
// - We return the full updated snippet to the caller
// - The "not found" error comes from GetByID, which is consistent
func (s *SnippetService) Update(ctx context.Context, id, name, code, description string) (*model.Snippet, error) {
	// Validate ID
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, apperror.ValidationFailed("id", "snippet ID is required")
	}

	// Fetch existing snippet — returns NotFound if it doesn't exist
	snippet, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply updates (only if provided — empty string means "don't change")
	if name = strings.TrimSpace(name); name != "" {
		if len(name) > MaxSnippetNameLength {
			return nil, apperror.ValidationFailed("name",
				fmt.Sprintf("snippet name must be %d characters or less", MaxSnippetNameLength))
		}
		snippet.Name = name
	}

	// Code CAN be empty (user might want to clear it), so always update it
	if len(code) > MaxCodeLength {
		return nil, apperror.ValidationFailed("code",
			fmt.Sprintf("code must be %d characters or less", MaxCodeLength))
	}
	snippet.Code = code
	snippet.Description = strings.TrimSpace(description)

	// Save to database
	if err := s.repo.Update(ctx, snippet); err != nil {
		s.logger.Error("failed to update snippet",
			slog.String("id", id),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("updating snippet: %w", err)
	}

	s.logger.Info("snippet updated",
		slog.String("id", snippet.ID),
		slog.String("name", snippet.Name),
	)

	return snippet, nil
}

// Delete removes a snippet by its ID.
// Returns apperror.ErrNotFound if the snippet doesn't exist.
func (s *SnippetService) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return apperror.ValidationFailed("id", "snippet ID is required")
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}

	s.logger.Info("snippet deleted", slog.String("id", id))
	return nil
}
