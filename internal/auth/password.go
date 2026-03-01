// Package auth — password hashing utilities.
//
// WHY BCRYPT?
// bcrypt is a password hashing function specifically designed to be slow.
// That slowness is a security feature: it makes brute-force attacks expensive.
//
// bcrypt automatically:
//   - Generates a random salt (so two users with the same password get different hashes)
//   - Embeds the salt in the output hash (no separate salt column needed)
//   - Controls the work factor via "cost" (higher = slower = harder to crack)
//
// NEVER store passwords in plain text or with fast hashes (MD5, SHA-256).
// Those can be cracked with GPU-accelerated rainbow tables in minutes.
// bcrypt with cost 12 takes ~250ms — negligible for login, brutal for attackers.
//
// Hash format (the full output of bcrypt.GenerateFromPassword):
//
//	$2a$12$<22-char salt><31-char hash>
//	 ^   ^
//	 |   cost (12 rounds → 2^12 = 4096 iterations)
//	 version
package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// defaultCost is the bcrypt work factor.
//
// Cost 12 is the current recommended minimum for new applications (2024).
// It takes roughly ~250ms on a modern server.
//
// COST TUNING RULE OF THUMB:
// Set cost so that hashing takes ~200–300ms on your production hardware.
// Too low → easy to crack. Too high → login is sluggish and your server
// spends all its time on bcrypt during traffic spikes.
const defaultCost = 12

// PasswordService provides bcrypt hashing and verification.
//
// It's a struct (not free functions) so that the cost can be injected
// in tests — using a lower cost (e.g. 4) makes tests run much faster
// without compromising the logic being tested.
type PasswordService struct {
	cost int
}

// NewPasswordService creates a PasswordService with the default cost (12).
func NewPasswordService() *PasswordService {
	return &PasswordService{cost: defaultCost}
}

// newPasswordServiceWithCost creates a PasswordService with a custom cost.
// Unexported helper used by the tests in this package.
func newPasswordServiceWithCost(cost int) *PasswordService {
	return &PasswordService{cost: cost}
}

// NewPasswordServiceForTest creates a PasswordService with bcrypt cost 4
// (the minimum allowed). Use this in tests in other packages to avoid the
// ~250ms overhead of cost 12 per hashing operation.
//
// Do NOT use in production — cost 4 is far too weak.
func NewPasswordServiceForTest(cost int) *PasswordService {
	return &PasswordService{cost: cost}
}

// Hash hashes the given plaintext password with bcrypt.
//
// The output is a self-contained string like:
//
//	$2a$12$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy
//
// Store this string directly in the database. It includes the salt and
// cost — bcrypt.CompareHashAndPassword knows how to decode it.
//
// Returns an error if the plaintext is too long (>72 bytes — a bcrypt limit).
func (p *PasswordService) Hash(plaintext string) (string, error) {
	if len(plaintext) > 72 {
		// bcrypt silently truncates passwords longer than 72 bytes.
		// We reject them explicitly so callers aren't surprised.
		return "", fmt.Errorf("auth: password must be 72 bytes or fewer")
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(plaintext), p.cost)
	if err != nil {
		return "", fmt.Errorf("auth: hashing password: %w", err)
	}

	return string(hashed), nil
}

// Verify checks whether a plaintext password matches a stored bcrypt hash.
//
// Returns nil if they match, a non-nil error if they don't.
//
// TIMING SAFETY:
// bcrypt.CompareHashAndPassword uses a constant-time comparison internally,
// so this function is safe against timing attacks — an attacker can't tell
// from response time whether they got the first byte right.
//
// Usage:
//
//	if err := ps.Verify(user.PasswordHash, inputPassword); err != nil {
//	    // wrong password
//	}
func (p *PasswordService) Verify(hash, plaintext string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext))
	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return fmt.Errorf("auth: invalid password")
		}
		return fmt.Errorf("auth: comparing password hash: %w", err)
	}
	return nil
}
