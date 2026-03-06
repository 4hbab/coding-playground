package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/sakif/coding-playground/internal/auth"
	"github.com/sakif/coding-playground/internal/service"
)

// AuthHandler handles authentication HTTP routes.
type AuthHandler struct {
	authService *service.AuthService
	github      *auth.GitHubProvider
	logger      *slog.Logger
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(as *service.AuthService, gh *auth.GitHubProvider, logger *slog.Logger) *AuthHandler {
	return &AuthHandler{
		authService: as,
		github:      gh,
		logger:      logger,
	}
}

// HandleGitHubLogin redirects the user to GitHub's OAuth authorization page.
//
// CSRF PROTECTION:
// We generate a random "state" parameter and store it in a short-lived cookie.
// When GitHub redirects back, we verify the state matches. This prevents
// an attacker from crafting a login URL that would associate their GitHub
// account with the victim's session.
func (h *AuthHandler) HandleGitHubLogin(w http.ResponseWriter, r *http.Request) {
	// Generate a cryptographically random state parameter
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		h.logger.Error("failed to generate OAuth state", slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	state := hex.EncodeToString(stateBytes)

	// Store state in a short-lived cookie (5 minutes, HttpOnly, SameSite=Lax)
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   300, // 5 minutes
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect to GitHub
	url := h.github.AuthURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// HandleGitHubCallback handles the OAuth callback from GitHub.
// Validates the CSRF state, exchanges the code for user info, and sets the JWT cookie.
func (h *AuthHandler) HandleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	// 1. Validate CSRF state
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil {
		h.logger.Warn("missing OAuth state cookie")
		http.Error(w, "Invalid OAuth state", http.StatusBadRequest)
		return
	}

	queryState := r.URL.Query().Get("state")
	if queryState == "" || queryState != stateCookie.Value {
		h.logger.Warn("OAuth state mismatch")
		http.Error(w, "Invalid OAuth state", http.StatusBadRequest)
		return
	}

	// Clear the state cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	// 2. Check for OAuth errors from GitHub
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		h.logger.Warn("GitHub OAuth error",
			slog.String("error", errMsg),
			slog.String("description", r.URL.Query().Get("error_description")),
		)
		http.Error(w, "GitHub authentication failed: "+errMsg, http.StatusBadRequest)
		return
	}

	// 3. Exchange code for user info and JWT
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	result, err := h.authService.LoginOrRegisterGitHub(r.Context(), code)
	if err != nil {
		h.logger.Error("login/register failed", slog.String("error", err.Error()))
		http.Error(w, "Authentication failed", http.StatusInternalServerError)
		return
	}

	// 4. Set the JWT in an HttpOnly cookie
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    result.Token,
		Path:     "/",
		MaxAge:   3600, // 1 hour (matches JWT expiry)
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Secure:   true, // uncomment in production (requires HTTPS)
	})

	h.logger.Info("user logged in",
		slog.String("user_id", result.User.ID),
		slog.String("login", result.User.Login),
	)

	// 5. Redirect to the playground
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

// HandleLogout clears the JWT cookie.
func (h *AuthHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1, // delete the cookie
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "logged out"})
}

// HandleMe returns the current authenticated user's profile.
// Returns 401 if no valid JWT cookie is present.
func (h *AuthHandler) HandleMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "not authenticated"})
		return
	}

	user, err := h.authService.GetUserByID(r.Context(), userID)
	if err != nil {
		h.logger.Error("failed to get user", slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if user == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "user not found"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

// TokenExpiry is exported so server.go can set cookie max-age consistently.
const TokenExpiry = 1 * time.Hour
