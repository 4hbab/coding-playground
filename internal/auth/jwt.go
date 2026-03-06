// Package auth provides authentication primitives: JWT tokens, OAuth providers,
// and HTTP middleware for protecting routes.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Token lifetimes.
const (
	DefaultTokenDuration = 1 * time.Hour // access tokens expire after 1 hour
)

// Custom claims embedded in every JWT.
type Claims struct {
	jwt.RegisteredClaims
	UserID string `json:"uid"`
}

// TokenService creates and validates JWT access tokens.
//
// SECURITY NOTES:
// - Uses HMAC-SHA256 (symmetric) — the same secret signs and verifies.
// - Tokens are stored in HttpOnly cookies, not localStorage (XSS safe).
// - 1-hour expiry with no refresh token — user simply re-authenticates.
type TokenService struct {
	secret []byte
}

// NewTokenService creates a TokenService. The secret must be at least 32 bytes
// for HMAC-SHA256 security.
func NewTokenService(secret string) (*TokenService, error) {
	if len(secret) < 32 {
		return nil, errors.New("auth: JWT secret must be at least 32 characters")
	}
	return &TokenService{secret: []byte(secret)}, nil
}

// Generate creates a signed JWT for the given user ID with the default 1-hour expiry.
func (ts *TokenService) Generate(userID string) (string, error) {
	return ts.GenerateWithDuration(userID, DefaultTokenDuration)
}

// GenerateWithDuration creates a signed JWT with a custom duration.
func (ts *TokenService) GenerateWithDuration(userID string, duration time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(duration)),
			Issuer:    "pyplayground",
		},
		UserID: userID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(ts.secret)
}

// Validate parses and validates a JWT string. Returns the claims if valid,
// or an error if expired, tampered, or malformed.
func (ts *TokenService) Validate(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		// Ensure the signing method is HMAC (prevent algorithm confusion attacks)
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("auth: unexpected signing method: %v", t.Header["alg"])
		}
		return ts.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("auth: invalid token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("auth: invalid token claims")
	}

	return claims, nil
}
