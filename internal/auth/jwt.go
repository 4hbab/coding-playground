// Package auth provides JWT token generation and validation for the playground API.
//
// AUTHENTICATION FLOW OVERVIEW:
// 1. User visits /auth/github/login → redirected to GitHub
// 2. GitHub calls back /auth/github/callback with a code
// 3. Server exchanges code for GitHub user info, upserts user in DB
// 4. Server issues a JWT access token, stores it in an HttpOnly cookie
// 5. On subsequent API calls, middleware reads the cookie, validates the JWT,
//    and sets the userID in the request context
//
// WHY JWT?
// JWT (JSON Web Token) is stateless — the server doesn't need to store session
// data. All the information needed (userID, expiry) is inside the signed token.
// The signature ensures nobody can tamper with it without the secret key.
//
// JWT STRUCTURE (three base64-encoded parts separated by dots):
//
//	HEADER.PAYLOAD.SIGNATURE
//	- Header: algorithm + token type → {"alg":"HS256","typ":"JWT"}
//	- Payload: claims (data) → {"sub":"userID","exp":1234567890}
//	- Signature: HMAC-SHA256(header+"."+payload, secretKey)
//
// The server can verify the signature without any DB lookup — just the secret.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenService handles JWT creation and validation.
//
// It holds the HMAC secret key used to sign and verify tokens.
// The same secret must be used for both operations — keep it safe, rotate it
// periodically in production.
type TokenService struct {
	secret []byte
}

// NewTokenService creates a TokenService with the given secret.
// The secret should be at least 32 bytes of random data in production.
// Example: JWT_SECRET=$(openssl rand -hex 32)
func NewTokenService(secret string) (*TokenService, error) {
	if len(secret) < 16 {
		return nil, errors.New("auth: JWT secret must be at least 16 characters")
	}
	return &TokenService{secret: []byte(secret)}, nil
}

// claims is the JWT payload. It embeds jwt.RegisteredClaims which includes
// standard fields like Issuer, Subject, ExpiresAt, IssuedAt.
//
// We use "sub" (Subject) to store the internal user ID.
// This is the standard JWT claim for identifying who the token belongs to.
type claims struct {
	jwt.RegisteredClaims
}

// Generate creates and signs a new JWT access token for the given userID.
//
// Token lifetime: 15 minutes.
// After expiry, the client must re-authenticate (in Step 3 we'll add
// refresh tokens via HttpOnly cookie to make this seamless).
//
// Signing algorithm: HS256 (HMAC-SHA256)
// - Symmetric: same key for signing and verifying
// - Fast and simple — good for single-server deployments
// - Alternative HS384/RS256 for asymmetric (multi-server key rotation)
func (s *TokenService) Generate(userID string) (string, error) {
	now := time.Now()

	c := claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
			Issuer:    "coding-playground",
		},
	}

	// jwt.NewWithClaims creates an unsigned token with the given algorithm.
	// SignedString(key) signs it and returns the complete JWT string.
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("auth: signing token: %w", err)
	}

	return signed, nil
}

// GenerateWithDuration creates a token with a custom expiry duration.
// Used in tests and for issuing longer-lived tokens (e.g. refresh tokens).
func (s *TokenService) GenerateWithDuration(userID string, d time.Duration) (string, error) {
	now := time.Now()

	c := claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(d)),
			Issuer:    "coding-playground",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("auth: signing token: %w", err)
	}

	return signed, nil
}

// Validate parses and verifies a JWT string.
// Returns the userID (stored in the "sub" claim) if the token is valid.
//
// VALIDATION CHECKS (performed by the jwt library):
//   - Signature is valid (wasn't tampered with)
//   - Token is not expired (ExpiresAt is in the future)
//   - Issuer matches "coding-playground" (prevents tokens from other apps)
//   - Algorithm is HS256 (prevents algorithm confusion attacks)
//
// ALGORITHM CONFUSION ATTACK:
// Without checking the algorithm, an attacker could send a token signed with
// "none" and the library might accept it. Passing jwt.WithValidMethods prevents this.
func (s *TokenService) Validate(tokenStr string) (string, error) {
	token, err := jwt.ParseWithClaims(
		tokenStr,
		&claims{},
		func(token *jwt.Token) (any, error) {
			// Reject tokens that aren't signed with HS256
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("auth: unexpected signing method: %v", token.Header["alg"])
			}
			return s.secret, nil
		},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer("coding-playground"),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		// Translate jwt library errors into cleaner messages
		if errors.Is(err, jwt.ErrTokenExpired) {
			return "", fmt.Errorf("auth: token expired")
		}
		return "", fmt.Errorf("auth: invalid token: %w", err)
	}

	c, ok := token.Claims.(*claims)
	if !ok || !token.Valid {
		return "", fmt.Errorf("auth: invalid token claims")
	}

	userID := c.Subject
	if userID == "" {
		return "", fmt.Errorf("auth: token has no subject")
	}

	return userID, nil
}
