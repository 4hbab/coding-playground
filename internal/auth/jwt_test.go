package auth

import (
	"testing"
	"time"
)

// newTestTokenService creates a TokenService for testing.
// It uses a fixed, known secret so tests are deterministic.
func newTestTokenService(t *testing.T) *TokenService {
	t.Helper()
	ts, err := NewTokenService("test-secret-at-least-16-chars!!")
	if err != nil {
		t.Fatalf("NewTokenService: %v", err)
	}
	return ts
}

// =========================================================================
// TOKEN SERVICE CONSTRUCTION TESTS
// =========================================================================

func TestNewTokenService_ShortSecret(t *testing.T) {
	_, err := NewTokenService("short")
	if err == nil {
		t.Fatal("NewTokenService() should reject secrets shorter than 16 chars")
	}
}

func TestNewTokenService_ValidSecret(t *testing.T) {
	_, err := NewTokenService("this-is-16-chars")
	if err != nil {
		t.Fatalf("NewTokenService() unexpected error for valid secret: %v", err)
	}
}

// =========================================================================
// GENERATE TESTS
// =========================================================================

func TestGenerate_ReturnsNonEmptyToken(t *testing.T) {
	ts := newTestTokenService(t)

	token, err := ts.Generate("user-123")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if token == "" {
		t.Error("Generate() returned empty token")
	}

	// JWT tokens have 3 dot-separated parts: header.payload.signature
	// Count dots to sanity-check the format
	dots := 0
	for _, c := range token {
		if c == '.' {
			dots++
		}
	}
	if dots != 2 {
		t.Errorf("Generate() token doesn't look like a JWT (expected 2 dots, got %d)", dots)
	}
}

func TestGenerate_DifferentUsersGetDifferentTokens(t *testing.T) {
	ts := newTestTokenService(t)

	token1, _ := ts.Generate("user-aaa")
	token2, _ := ts.Generate("user-bbb")

	if token1 == token2 {
		t.Error("Generate() returned identical tokens for different user IDs")
	}
}

// =========================================================================
// VALIDATE TESTS
// =========================================================================

func TestValidate_RoundTrip(t *testing.T) {
	ts := newTestTokenService(t)
	userID := "user-abc-123"

	token, err := ts.Generate(userID)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	// Validate should return the exact same userID we put in
	got, err := ts.Validate(token)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got != userID {
		t.Errorf("Validate() userID = %q, want %q", got, userID)
	}
}

func TestValidate_ExpiredToken(t *testing.T) {
	ts := newTestTokenService(t)

	// Generate a token that expired 1 second ago
	token, err := ts.GenerateWithDuration("user-123", -1*time.Second)
	if err != nil {
		t.Fatalf("GenerateWithDuration() error = %v", err)
	}

	_, err = ts.Validate(token)
	if err == nil {
		t.Fatal("Validate() should return an error for an expired token")
	}
	t.Logf("Expired token error (expected): %v", err)
}

func TestValidate_TamperedToken(t *testing.T) {
	ts := newTestTokenService(t)

	token, _ := ts.Generate("user-123")

	// Flip a character in the signature (last segment after the 2nd dot)
	// to simulate an attacker modifying the payload
	tampered := token[:len(token)-3] + "xxx"

	_, err := ts.Validate(tampered)
	if err == nil {
		t.Fatal("Validate() should return an error for a tampered token")
	}
	t.Logf("Tampered token error (expected): %v", err)
}

func TestValidate_WrongSecret(t *testing.T) {
	ts1, _ := NewTokenService("correct-secret-32-chars-long!!!!")
	ts2, _ := NewTokenService("wrong-secret-32-chars-long!!!!!!")

	// Token signed with ts1's secret
	token, _ := ts1.Generate("user-123")

	// Validating with ts2's (different) secret must fail
	_, err := ts2.Validate(token)
	if err == nil {
		t.Fatal("Validate() should fail when using a different secret")
	}
}

func TestValidate_EmptyToken(t *testing.T) {
	ts := newTestTokenService(t)

	_, err := ts.Validate("")
	if err == nil {
		t.Fatal("Validate() should return an error for an empty string")
	}
}

func TestValidate_GarbageString(t *testing.T) {
	ts := newTestTokenService(t)

	_, err := ts.Validate("not.a.jwt.token")
	if err == nil {
		t.Fatal("Validate() should return an error for a garbage string")
	}
}

// =========================================================================
// DURATION TESTS
// =========================================================================

func TestGenerateWithDuration_FutureToken(t *testing.T) {
	ts := newTestTokenService(t)

	token, err := ts.GenerateWithDuration("user-123", 1*time.Hour)
	if err != nil {
		t.Fatalf("GenerateWithDuration() error = %v", err)
	}

	// A 1-hour token should be valid now
	userID, err := ts.Validate(token)
	if err != nil {
		t.Fatalf("Validate() on 1h token error = %v", err)
	}
	if userID != "user-123" {
		t.Errorf("userID = %q, want %q", userID, "user-123")
	}
}
