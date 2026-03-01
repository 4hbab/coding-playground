// Package service — authentication business logic.
//
// AuthService is the business logic layer for authentication. It sits between
// the HTTP handlers and the repository/auth utilities:
//
//	AuthHandler (HTTP) → AuthService (business rules) → UserRepository (DB)
//	                   ↘ TokenService (JWT)
//
// KEY RESPONSIBILITIES:
//   - Orchestrate the GitHub OAuth callback: upsert the user, issue tokens
//   - Encapsulate all auth rules in one place, away from HTTP concerns
//   - Be easily testable with mock dependencies
//
// NOTE ON PASSWORD AUTH:
// This app uses GitHub OAuth as its primary identity provider — users never
// set a password directly. The PasswordService (password.go) is included for
// completeness (e.g. if email/password auth is added later) but is not used
// in the main auth flow described here.
package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/sakif/coding-playground/internal/auth"
	"github.com/sakif/coding-playground/internal/model"
	"github.com/sakif/coding-playground/internal/repository"
)

// AuthService handles the authentication business logic.
//
// DEPENDENCIES (injected via NewAuthService):
//   - users      repository.UserRepository  → read/write user records
//   - tokens     *auth.TokenService         → generate/validate JWTs
//   - passwords  *auth.PasswordService      → bcrypt hashing (for future use)
//   - logger     *slog.Logger               → structured logging
type AuthService struct {
	users     repository.UserRepository
	tokens    *auth.TokenService
	passwords *auth.PasswordService
	logger    *slog.Logger
}

// NewAuthService creates an AuthService with all required dependencies.
// Call this in server.go (or main.go) when wiring the dependency graph.
func NewAuthService(
	users repository.UserRepository,
	tokens *auth.TokenService,
	passwords *auth.PasswordService,
	logger *slog.Logger,
) *AuthService {
	return &AuthService{
		users:     users,
		tokens:    tokens,
		passwords: passwords,
		logger:    logger,
	}
}

// AuthResult is returned by authentication operations.
// It bundles the user record and the issued JWT together so the caller
// (the HTTP handler) can set the cookie and respond in one step.
type AuthResult struct {
	User  *model.User
	Token string
}

// LoginOrRegisterGitHub handles the GitHub OAuth callback.
//
// This is the core of the OAuth flow. After the handler exchanges the GitHub
// code for a GitHubUser profile, it calls this method to:
//
//  1. Upsert the user in the database (create on first login, update on subsequent logins)
//  2. Generate a JWT access token for the authenticated user
//  3. Return both so the handler can set the HttpOnly cookie and redirect
//
// WHY UPSERT (not insert + check conflict)?
// GitHub's OAuth guarantees the GitHub ID is stable and unique, so we can
// always upsert on (github_id). First login → INSERT; subsequent logins → UPDATE
// the email/avatar in case the user changed them on GitHub.
//
// WHAT THIS METHOD DOES NOT DO:
//   - It does NOT set cookies (that's the handler's job — HTTP concern)
//   - It does NOT read HTTP requests
//   - It is NOT tied to Chi or any routing framework
func (s *AuthService) LoginOrRegisterGitHub(ctx context.Context, ghUser *auth.GitHubUser) (*AuthResult, error) {
	if ghUser == nil {
		return nil, fmt.Errorf("service/auth: GitHub user must not be nil")
	}

	// Build the user model from GitHub profile data.
	// The repository's Upsert will fill in ID, CreatedAt, and UpdatedAt.
	user := &model.User{
		GitHubID:  ghUser.ID,
		Login:     ghUser.Login,
		Email:     ghUser.Email,
		AvatarURL: ghUser.AvatarURL,
	}

	// Upsert: INSERT or UPDATE based on github_id.
	// After this call, user.ID is populated by the repository.
	if err := s.users.Upsert(ctx, user); err != nil {
		return nil, fmt.Errorf("service/auth: upserting user (githubID=%d): %w", ghUser.ID, err)
	}

	s.logger.Info("user authenticated via GitHub",
		slog.String("userID", user.ID),
		slog.String("login", user.Login),
	)

	// Issue a JWT access token containing the user's internal ID.
	token, err := s.tokens.Generate(user.ID)
	if err != nil {
		return nil, fmt.Errorf("service/auth: generating token for user %s: %w", user.ID, err)
	}

	return &AuthResult{
		User:  user,
		Token: token,
	}, nil
}

// GetUserByID returns the user for the given internal ID.
//
// Used by the /api/me handler to look up the full user record after the
// middleware validates the JWT and extracts the userID from the token's
// Subject claim.
func (s *AuthService) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	if id == "" {
		return nil, fmt.Errorf("service/auth: user ID must not be empty")
	}

	user, err := s.users.GetUserByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("service/auth: fetching user %s: %w", id, err)
	}

	return user, nil
}

// ValidateToken validates a JWT string and returns the userID it encodes.
//
// This is a thin delegation to TokenService.Validate. Having it on
// AuthService means callers only need to import the service package, not
// the auth package directly.
//
// Returns an error if the token is expired, tampered, or otherwise invalid.
func (s *AuthService) ValidateToken(tokenStr string) (string, error) {
	userID, err := s.tokens.Validate(tokenStr)
	if err != nil {
		return "", fmt.Errorf("service/auth: %w", err)
	}
	return userID, nil
}
