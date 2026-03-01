package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/rs/xid"
	"github.com/sakif/coding-playground/internal/auth"
	"github.com/sakif/coding-playground/internal/model"
	"github.com/sakif/coding-playground/internal/repository"
)

// AuthHandler manages the GitHub OAuth login flow and session management.
//
// HANDLER RESPONSIBILITIES:
//   - HandleGitHubLogin  → redirect the browser to GitHub's authorization page
//   - HandleGitHubCallback → receive the code, exchange it for a user, issue JWT
//   - HandleLogout       → clear the JWT cookie
//   - HandleMe           → return the currently logged-in user's profile
//
// DEPENDENCY CHAIN:
//   - github *auth.GitHubProvider  → performs the OAuth code exchange
//   - tokens *auth.TokenService    → issues JWT access tokens
//   - users  repository.UserRepository → upsert/lookup users in the DB
type AuthHandler struct {
	github *auth.GitHubProvider
	tokens *auth.TokenService
	users  repository.UserRepository
	logger *slog.Logger
}

// NewAuthHandler creates an AuthHandler. All dependencies are injected here;
// the handler has no knowledge of how they're constructed.
func NewAuthHandler(
	github *auth.GitHubProvider,
	tokens *auth.TokenService,
	users repository.UserRepository,
	logger *slog.Logger,
) *AuthHandler {
	return &AuthHandler{
		github: github,
		tokens: tokens,
		users:  users,
		logger: logger,
	}
}

// HandleGitHubLogin redirects the user to GitHub's authorization page.
//
// HTTP: GET /auth/github/login
//
// CSRF PROTECTION VIA STATE:
// We generate a random state string and store it in a short-lived cookie.
// When GitHub calls back, HandleGitHubCallback verifies the state matches.
// This proves the callback was initiated by this server, not a CSRF attacker.
//
// The state cookie is:
//   - HttpOnly: JavaScript can't read it
//   - SameSite=Lax: not sent on cross-site POSTs (extra CSRF protection)
//   - 10-minute expiry: long enough for the user to approve, short enough to limit risk
func (h *AuthHandler) HandleGitHubLogin(w http.ResponseWriter, r *http.Request) {
	// Generate a random, unguessable state value
	state := xid.New().String()

	// Store it in a cookie so we can verify it on callback
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   600, // 10 minutes
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect the browser to GitHub
	http.Redirect(w, r, h.github.AuthURL(state), http.StatusTemporaryRedirect)
}

// HandleGitHubCallback completes the OAuth login flow.
//
// HTTP: GET /auth/github/callback?code=xxx&state=yyy
//
// FLOW:
//  1. Validate the state parameter (CSRF check)
//  2. Exchange the code for a GitHub user profile
//  3. Upsert the user in the database
//  4. Issue a JWT access token stored in an HttpOnly cookie
//  5. Redirect to the app home page
func (h *AuthHandler) HandleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	// --- Step 1: Validate CSRF state ---
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || stateCookie.Value == "" {
		h.logger.Warn("auth callback: missing state cookie")
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}

	if r.URL.Query().Get("state") != stateCookie.Value {
		h.logger.Warn("auth callback: state mismatch",
			slog.String("expected", stateCookie.Value),
			slog.String("got", r.URL.Query().Get("state")),
		)
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}

	// Clear the state cookie — it's single-use
	http.SetCookie(w, &http.Cookie{
		Name:   "oauth_state",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	// Check if GitHub sent an error (user denied authorization)
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		h.logger.Info("auth callback: user denied authorization",
			slog.String("error", errParam),
		)
		http.Redirect(w, r, "/?auth=denied", http.StatusSeeOther)
		return
	}

	// --- Step 2: Exchange code for GitHub user profile ---
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing OAuth code", http.StatusBadRequest)
		return
	}

	ghUser, err := h.github.Exchange(r.Context(), code)
	if err != nil {
		h.logger.Error("auth callback: GitHub exchange failed", slog.String("error", err.Error()))
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}

	// --- Step 3: Upsert user in the database ---
	user := &model.User{
		GitHubID:  ghUser.ID,
		Login:     ghUser.Login,
		Email:     ghUser.Email,
		AvatarURL: ghUser.AvatarURL,
	}
	if err := h.users.Upsert(r.Context(), user); err != nil {
		h.logger.Error("auth callback: upsert failed",
			slog.Int64("githubID", ghUser.ID),
			slog.String("error", err.Error()),
		)
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}

	h.logger.Info("user authenticated",
		slog.String("userID", user.ID),
		slog.String("login", user.Login),
	)

	// --- Step 4: Issue JWT cookie ---
	tokenStr, err := h.tokens.Generate(user.ID)
	if err != nil {
		h.logger.Error("auth callback: token generation failed", slog.String("error", err.Error()))
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}

	// Set the JWT as an HttpOnly cookie.
	// HttpOnly = JavaScript cannot read this cookie (XSS protection).
	// SameSite=Lax = cookie is sent on top-level navigations but not cross-site POSTs.
	// Secure should be true in production (HTTPS only). We leave it false for local dev.
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    tokenStr,
		Path:     "/",
		MaxAge:   int((15 * time.Minute).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Secure: true, // Uncomment in production (requires HTTPS)
	})

	// --- Step 5: Redirect to the app ---
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// HandleLogout clears the JWT cookie, effectively logging the user out.
//
// HTTP: POST /auth/logout
//
// WHY POST AND NOT GET?
// Logout is a state-changing operation. Using GET would be vulnerable to
// CSRF and to browsers pre-fetching the URL. POST ensures intentional action.
//
// Since we're stateless (JWT), "logout" just means deleting the client-side
// cookie. The token remains technically valid until it expires (15 min), but
// without the cookie the browser can't send it.
func (h *AuthHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1, // tells the browser to delete the cookie immediately
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

// HandleMe returns the currently authenticated user's profile.
//
// HTTP: GET /api/me
// Auth: Required (RequireAuth middleware sets userID in context)
//
// This is useful for the frontend to:
//   - Know who is logged in (to show the username/avatar)
//   - Check authentication state on app load
func (h *AuthHandler) HandleMe(w http.ResponseWriter, r *http.Request) {
	// Auth middleware has already validated the JWT and set userID in context.
	// UserIDFromContext will always return (id, true) on this protected route.
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		// Should never happen on a RequireAuth-protected route, but be safe.
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	user, err := h.users.GetUserByID(r.Context(), userID)
	if err != nil {
		h.logger.Error("HandleMe: user not found", slog.String("userID", userID))
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, user)
}
