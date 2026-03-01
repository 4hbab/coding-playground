package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

// GitHubUser is the portion of the GitHub /user API response we care about.
// GitHub returns a much larger object — we only unmarshal the fields we need.
//
// GitHub API docs: https://docs.github.com/en/rest/users/users#get-the-authenticated-user
type GitHubUser struct {
	ID        int64  `json:"id"`         // GitHub's numeric user ID — stable, never changes
	Login     string `json:"login"`      // GitHub username, e.g. "sakif"
	Email     string `json:"email"`      // Primary email (empty if hidden in GitHub settings)
	AvatarURL string `json:"avatar_url"` // Profile picture URL
}

// GitHubProvider wraps golang.org/x/oauth2 for the GitHub Authorization Code flow.
//
// OAUTH 2.0 AUTHORIZATION CODE FLOW:
// 1. Your server redirects the user to GitHub's authorization endpoint,
//    with your ClientID and the requested scopes.
// 2. The user approves (or denies) the authorization request on GitHub.
// 3. GitHub redirects back to your CallbackURL with a short-lived "code".
// 4. Your server exchanges the code for an access token (server-to-server call).
// 5. Your server uses the access token to call the GitHub API for user info.
//
// WHY SERVER-SIDE EXCHANGE?
// The code-for-token exchange happens server-to-server, using your ClientSecret.
// The access token never touches the client's browser. This is more secure than
// the implicit flow where tokens are returned directly to the browser.
type GitHubProvider struct {
	config *oauth2.Config
}

// NewGitHubProvider creates a GitHubProvider with the given credentials.
//
// You get ClientID and ClientSecret by registering an OAuth App at:
// https://github.com/settings/developers → "OAuth Apps" → "New OAuth App"
//
// callbackURL must match the "Authorization callback URL" you configured exactly.
// Example: "http://localhost:8080/auth/github/callback"
//
// Scopes we request:
//   - "read:user" — access to the user's public profile (ID, login, avatar)
//   - "user:email" — access to the user's email addresses
func NewGitHubProvider(clientID, clientSecret, callbackURL string) *GitHubProvider {
	return &GitHubProvider{
		config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  callbackURL,
			Scopes:       []string{"read:user", "user:email"},
			Endpoint:     github.Endpoint, // pre-defined GitHub OAuth endpoints
		},
	}
}

// AuthURL returns the URL to redirect the user to for authorization.
//
// STATE PARAMETER:
// The state is a random string we generate and store in a cookie before
// redirecting. When GitHub calls back, we verify the returned state matches
// our cookie. This prevents CSRF (Cross-Site Request Forgery) attacks where
// an attacker tricks your browser into completing an OAuth flow for their account.
//
// Example state: "xid:cv37rs3pp9olc6atsptg" (random, hard to guess)
func (p *GitHubProvider) AuthURL(state string) string {
	return p.config.AuthCodeURL(state, oauth2.AccessTypeOnline)
}

// Exchange completes the OAuth flow: trades the authorization code for a GitHub
// user profile. This is the core of the callback handler.
//
// Steps:
//  1. Exchange the code for an OAuth access token (server-to-server)
//  2. Use the token to call GitHub's /user API endpoint
//  3. Unmarshal the response into a GitHubUser struct
//
// The returned GitHubUser is used by the auth handler to upsert the user
// in the database and then issue a JWT access cookie.
func (p *GitHubProvider) Exchange(ctx context.Context, code string) (*GitHubUser, error) {
	// Step 1: exchange authorization code → OAuth access token
	// This makes a POST to GitHub's token endpoint using our ClientSecret.
	// The token is short-lived and scoped to the permissions we requested.
	oauthToken, err := p.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("auth: exchanging OAuth code: %w", err)
	}

	// Step 2: call the GitHub /user API with the token
	// oauth2.Config.Client returns an *http.Client that automatically adds
	// the "Authorization: Bearer <token>" header to every request.
	client := p.config.Client(ctx, oauthToken)

	resp, err := client.Get("https://api.github.com/user")
	if err != nil {
		return nil, fmt.Errorf("auth: calling GitHub /user API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth: GitHub /user API returned status %d", resp.StatusCode)
	}

	// Step 3: unmarshal the JSON response into our GitHubUser struct
	var ghUser GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&ghUser); err != nil {
		return nil, fmt.Errorf("auth: decoding GitHub /user response: %w", err)
	}

	if ghUser.ID == 0 {
		return nil, fmt.Errorf("auth: GitHub returned an invalid user (ID = 0)")
	}

	return &ghUser, nil
}
