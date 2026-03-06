package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/rs/xid"
	"github.com/sakif/coding-playground/internal/auth"
	"github.com/sakif/coding-playground/internal/model"
	"github.com/sakif/coding-playground/internal/repository"
)

// AuthService handles authentication business logic.
//
// FLOW:
//  1. User clicks "Sign in with GitHub" → frontend redirects to /auth/github/login
//  2. Server generates CSRF state, redirects to GitHub's OAuth page
//  3. User authorizes → GitHub redirects back to /auth/github/callback?code=...&state=...
//  4. Server calls LoginOrRegisterGitHub(code):
//     a) Exchange code for GitHub access token
//     b) Fetch user profile from GitHub API
//     c) Upsert user in our database (create or update)
//     d) Generate a JWT (1-hour expiry)
//  5. Server sets JWT in HttpOnly cookie → redirects to /
type AuthService struct {
	users  repository.UserRepository
	github *auth.GitHubProvider
	tokens *auth.TokenService
	logger *slog.Logger
}

// NewAuthService creates an AuthService.
func NewAuthService(
	users repository.UserRepository,
	github *auth.GitHubProvider,
	tokens *auth.TokenService,
	logger *slog.Logger,
) *AuthService {
	return &AuthService{
		users:  users,
		github: github,
		tokens: tokens,
		logger: logger,
	}
}

// LoginResult holds the JWT token and user profile after a successful login.
type LoginResult struct {
	Token string
	User  *model.User
}

// LoginOrRegisterGitHub handles the OAuth callback:
// exchanges the code, fetches the GitHub profile, upserts the user, and generates a JWT.
func (s *AuthService) LoginOrRegisterGitHub(ctx context.Context, code string) (*LoginResult, error) {
	// 1. Exchange the authorization code for a GitHub access token
	oauthToken, err := s.github.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("github exchange: %w", err)
	}

	// 2. Fetch the user's GitHub profile
	ghUser, err := s.github.GetUser(ctx, oauthToken)
	if err != nil {
		return nil, fmt.Errorf("github get user: %w", err)
	}

	s.logger.Info("GitHub user authenticated",
		slog.String("login", ghUser.Login),
		slog.Int64("github_id", ghUser.ID),
	)

	// 3. Upsert the user in our database
	user := &model.User{
		ID:        xid.New().String(),
		GitHubID:  ghUser.ID,
		Login:     ghUser.Login,
		Email:     ghUser.Email,
		AvatarURL: ghUser.AvatarURL,
	}

	if err := s.users.Upsert(ctx, user); err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}

	// 4. Generate a JWT for the user
	token, err := s.tokens.Generate(user.ID)
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	return &LoginResult{Token: token, User: user}, nil
}

// GetUserByID retrieves a user by their internal ID.
func (s *AuthService) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	return s.users.GetUserByID(ctx, id)
}
