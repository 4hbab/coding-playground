// Package service contains the business logic layer of the application.
//
// THE THREE-LAYER ARCHITECTURE:
// In a well-structured Go web app, code is organised into three layers:
//
//	Handler (HTTP layer)     → parses requests, writes responses
//	Service (Business layer) → validates, enforces rules, orchestrates
//	Repository (Data layer)  → reads/writes to the database
//
// WHY A SEPARATE SERVICE LAYER?
// Without a service layer, handlers do everything: parse HTTP, validate data,
// call the database, format responses. With a service layer:
//
//  1. TESTING: Test business logic with plain Go function calls, not HTTP requests.
//  2. REUSE: Business rules work the same from HTTP, CLI, or background jobs.
//  3. SEPARATION: Handlers know HTTP; services know business rules; neither knows SQL.
//
// THE DEPENDENCY CHAIN:
//
//	main.go creates:  DB → Repository → Service → Handler
//	At runtime:       Handler calls Service calls Repository calls DB
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
const (
	MaxSnippetNameLength = 100
	MaxCodeLength        = 100000 // ~100KB of code
	DefaultListLimit     = 20
	MaxListLimit         = 100
)

// SnippetService handles business logic for code snippets.
type SnippetService struct {
	repo   repository.SnippetRepository
	logger *slog.Logger
}

// NewSnippetService creates a new SnippetService.
func NewSnippetService(repo repository.SnippetRepository, logger *slog.Logger) *SnippetService {
	return &SnippetService{
		repo:   repo,
		logger: logger,
	}
}

// Create validates and saves a new snippet.
//
// ownerID is the internal user ID of the authenticated caller, or "" for anonymous.
// When non-empty, the snippet is associated with that user and only they can update/delete it.
func (s *SnippetService) Create(ctx context.Context, name, code, description, ownerID string) (*model.Snippet, error) {
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

	snippet := &model.Snippet{
		Name:        name,
		Code:        code,
		Description: strings.TrimSpace(description),
	}

	// Associate with owner when the user is authenticated.
	// We store a *string (nullable) so anonymous snippets have NULL in the DB.
	if ownerID != "" {
		snippet.UserID = &ownerID
	}

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
		slog.String("ownerID", ownerID),
	)

	return snippet, nil
}

// GetByID retrieves a snippet by its ID. Public — no ownership check.
func (s *SnippetService) GetByID(ctx context.Context, id string) (*model.Snippet, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, apperror.ValidationFailed("id", "snippet ID is required")
	}

	snippet, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return snippet, nil
}

// List retrieves snippets with pagination.
func (s *SnippetService) List(ctx context.Context, limit, offset int) ([]model.Snippet, error) {
	if limit <= 0 {
		limit = DefaultListLimit
	}
	if limit > MaxListLimit {
		limit = MaxListLimit
	}
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
// OWNERSHIP RULE:
// If the snippet has an owner, only that owner (matching callerID) may update it.
// Anonymous snippets (UserID == nil) can be mutated by anyone — this is the
// pre-auth behaviour preserved for backwards compatibility.
//
// callerID is the authenticated user's ID, or "" for anonymous requests.
func (s *SnippetService) Update(ctx context.Context, id, name, code, description, callerID string) (*model.Snippet, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, apperror.ValidationFailed("id", "snippet ID is required")
	}

	snippet, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Enforce ownership: if the snippet has an owner, the caller must be that owner.
	if snippet.UserID != nil && *snippet.UserID != callerID {
		return nil, apperror.Forbidden("you do not have permission to update this snippet")
	}

	if name = strings.TrimSpace(name); name != "" {
		if len(name) > MaxSnippetNameLength {
			return nil, apperror.ValidationFailed("name",
				fmt.Sprintf("snippet name must be %d characters or less", MaxSnippetNameLength))
		}
		snippet.Name = name
	}

	if len(code) > MaxCodeLength {
		return nil, apperror.ValidationFailed("code",
			fmt.Sprintf("code must be %d characters or less", MaxCodeLength))
	}
	snippet.Code = code
	snippet.Description = strings.TrimSpace(description)

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
//
// OWNERSHIP RULE: Same as Update — only the owner may delete an owned snippet.
// callerID is "" for anonymous requests.
func (s *SnippetService) Delete(ctx context.Context, id, callerID string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return apperror.ValidationFailed("id", "snippet ID is required")
	}

	// Fetch first to check ownership.
	snippet, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	// Enforce ownership.
	if snippet.UserID != nil && *snippet.UserID != callerID {
		return apperror.Forbidden("you do not have permission to delete this snippet")
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}

	s.logger.Info("snippet deleted", slog.String("id", id))
	return nil
}
