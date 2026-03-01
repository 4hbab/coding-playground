package auth

import (
	"context"
	"net/http"
)

// contextKey is an unexported type used for context keys in this package.
//
// WHY A CUSTOM TYPE FOR CONTEXT KEYS?
// context.WithValue uses any as the key type. If you use a plain string like
// context.WithValue(ctx, "userID", id), ANY package that knows the string "userID"
// can read or shadow your value. Using a package-private type prevents collisions:
// only THIS package can create a key of type contextKey, so only this package
// can read or write userID values in the context.
type contextKey string

const userIDKey contextKey = "userID"

// RequireAuth is a middleware that enforces authentication on protected routes.
//
// It reads the JWT from the "token" HttpOnly cookie, validates it, and
// stores the userID in the request context. If the token is missing or
// invalid, it returns 401 Unauthorized and stops the request chain.
//
// MIDDLEWARE PATTERN IN GO:
// A middleware is a function that takes an http.Handler and returns a new
// http.Handler. The new handler "wraps" the original:
//
//	func Middleware(next http.Handler) http.Handler {
//	    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//	        // ... do stuff before the handler ...
//	        next.ServeHTTP(w, r)
//	        // ... do stuff after the handler ...
//	    })
//	}
//
// Chi applies middlewares in a chain: req → M1 → M2 → Handler → M2 → M1 → resp
//
// COOKIE-BASED TOKEN STORAGE:
// We store the JWT in an HttpOnly cookie rather than localStorage or a
// header. HttpOnly means JavaScript cannot read it, which prevents
// XSS (Cross-Site Scripting) attacks from stealing the token.
func RequireAuth(tokens *TokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, err := extractUserID(r, tokens)
			if err != nil {
				http.Error(w, `{"error":"unauthorized","message":"valid authentication required"}`, http.StatusUnauthorized)
				return
			}

			// Store userID in context so handlers can read it
			ctx := context.WithValue(r.Context(), userIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalAuth is a middleware that extracts the user identity if a valid token
// is present, but does NOT block the request if it's missing or invalid.
//
// Use this on public routes like GET /api/snippets where:
// - Anonymous users can still read
// - But logged-in users might see additional data (e.g. their own snippets marked)
//
// Handlers check for the user via UserIDFromContext — if it returns ("", false),
// the request is anonymous.
func OptionalAuth(tokens *TokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if userID, err := extractUserID(r, tokens); err == nil && userID != "" {
				ctx := context.WithValue(r.Context(), userIDKey, userID)
				r = r.WithContext(ctx)
			}
			// Always continue — no 401 even if no token
			next.ServeHTTP(w, r)
		})
	}
}

// UserIDFromContext retrieves the authenticated user's ID from the request context.
//
// Returns ("", false) if the request is anonymous (no valid token was present).
// Returns (id, true) if the user is authenticated.
//
// Usage in handlers:
//
//	userID, ok := auth.UserIDFromContext(r.Context())
//	if !ok {
//	    // anonymous user
//	}
func UserIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(userIDKey).(string)
	return id, ok && id != ""
}

// extractUserID reads the JWT cookie and validates it.
// This is a private helper shared by RequireAuth and OptionalAuth.
//
// COOKIE FLOW:
// 1. Set-Cookie: token=<jwt>; HttpOnly; Secure; SameSite=Lax (set on login)
// 2. Browser automatically sends Cookie: token=<jwt> on subsequent requests
// 3. We read r.Cookie("token") and validate it
func extractUserID(r *http.Request, tokens *TokenService) (string, error) {
	cookie, err := r.Cookie("token")
	if err != nil {
		// http.ErrNoCookie means the cookie isn't present — not an error, just anonymous
		return "", err
	}

	return tokens.Validate(cookie.Value)
}
