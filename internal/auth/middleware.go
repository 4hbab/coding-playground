package auth

import (
	"context"
	"net/http"
)

// contextKey is an unexported type to prevent collisions in context values.
type contextKey string

const userIDKey contextKey = "userID"

// CookieName is the name of the HttpOnly cookie that holds the JWT.
const CookieName = "pyplayground_token"

// RequireAuth is middleware that rejects requests without a valid JWT cookie.
// Returns 401 Unauthorized if the token is missing or invalid.
func RequireAuth(ts *TokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(CookieName)
			if err != nil {
				http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
				return
			}

			claims, err := ts.Validate(cookie.Value)
			if err != nil {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			// Inject the user ID into the request context
			ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalAuth is middleware that injects the user ID into the context
// if a valid JWT cookie is present, but does NOT reject the request otherwise.
// Use this on routes that work for both anonymous and authenticated users.
func OptionalAuth(ts *TokenService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(CookieName)
			if err == nil {
				if claims, err := ts.Validate(cookie.Value); err == nil {
					ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)
					r = r.WithContext(ctx)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UserIDFromContext extracts the user ID from the request context.
// Returns ("", false) if no user ID is present (anonymous request).
func UserIDFromContext(ctx context.Context) (string, bool) {
	uid, ok := ctx.Value(userIDKey).(string)
	return uid, ok
}
