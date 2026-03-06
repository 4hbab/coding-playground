package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

// GitHubUser represents the user profile returned by the GitHub API.
type GitHubUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

// GitHubProvider wraps the OAuth2 config and provides convenience methods
// for the GitHub OAuth flow.
//
// OAUTH2 FLOW:
//  1. AuthURL(state) → redirect user to GitHub's authorization page
//  2. User authorizes → GitHub redirects back with ?code=...&state=...
//  3. Exchange(code) → swap the code for an access token
//  4. GetUser(token) → call GitHub API to fetch user profile
type GitHubProvider struct {
	config *oauth2.Config
}

// NewGitHubProvider creates a GitHubProvider with the given credentials.
func NewGitHubProvider(clientID, clientSecret, callbackURL string) *GitHubProvider {
	return &GitHubProvider{
		config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  callbackURL,
			Scopes:       []string{"read:user", "user:email"},
			Endpoint:     github.Endpoint,
		},
	}
}

// AuthURL generates the GitHub authorization URL with the given CSRF state.
func (p *GitHubProvider) AuthURL(state string) string {
	return p.config.AuthCodeURL(state)
}

// Exchange swaps an authorization code for an OAuth2 token.
func (p *GitHubProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	token, err := p.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("auth: github code exchange failed: %w", err)
	}
	return token, nil
}

// GetUser fetches the authenticated user's profile from the GitHub API.
func (p *GitHubProvider) GetUser(ctx context.Context, token *oauth2.Token) (*GitHubUser, error) {
	client := p.config.Client(ctx, token)

	resp, err := client.Get("https://api.github.com/user")
	if err != nil {
		return nil, fmt.Errorf("auth: github API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("auth: github API returned %d: %s", resp.StatusCode, string(body))
	}

	var user GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("auth: failed to decode github user: %w", err)
	}

	return &user, nil
}
