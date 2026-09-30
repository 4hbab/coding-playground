package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

const testSecret = "this-is-a-test-secret-for-jwt-testing-32ch"

func TestTokenService_RoundTrip(t *testing.T) {
	ts, err := NewTokenService(testSecret)
	if err != nil {
		t.Fatalf("NewTokenService: %v", err)
	}

	token, err := ts.Generate("user-123")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	claims, err := ts.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if claims.UserID != "user-123" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "user-123")
	}
	if claims.Issuer != "pyplayground" {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, "pyplayground")
	}
}

func TestTokenService_Expired(t *testing.T) {
	ts, err := NewTokenService(testSecret)
	if err != nil {
		t.Fatalf("NewTokenService: %v", err)
	}

	// Generate a token that expired 1 second ago
	token, err := ts.GenerateWithDuration("user-123", -1*time.Second)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	_, err = ts.Validate(token)
	if err == nil {
		t.Error("Validate: expected error for expired token, got nil")
	}
}

func TestTokenService_TamperedToken(t *testing.T) {
	ts, err := NewTokenService(testSecret)
	if err != nil {
		t.Fatalf("NewTokenService: %v", err)
	}

	token, err := ts.Generate("user-123")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// A JWT is header.payload.signature (each base64url-encoded).
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts, want 3", len(parts))
	}

	// Don't just change the last character: it carries only 4 bits of the
	// signature, so some replacements decode to the same bytes and the test
	// passed or failed depending on the token's timestamp.
	t.Run("payload changed to another user", func(t *testing.T) {
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			t.Fatalf("decoding payload: %v", err)
		}
		forged := strings.Replace(string(payload), "user-123", "user-999", 1)
		if forged == string(payload) {
			t.Fatal("payload doesn't contain the user ID")
		}
		tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(forged)) + "." + parts[2]

		if _, err := ts.Validate(tampered); err == nil {
			t.Error("Validate: expected error for a forged payload, got nil")
		}
	})

	t.Run("signature changed", func(t *testing.T) {
		sig := []byte(parts[2])
		// The first character carries 6 full bits, so changing it always changes the signature.
		if sig[0] == 'A' {
			sig[0] = 'B'
		} else {
			sig[0] = 'A'
		}
		tampered := parts[0] + "." + parts[1] + "." + string(sig)

		if _, err := ts.Validate(tampered); err == nil {
			t.Error("Validate: expected error for a changed signature, got nil")
		}
	})
}

func TestTokenService_WrongSecret(t *testing.T) {
	ts1, _ := NewTokenService(testSecret)
	ts2, _ := NewTokenService("another-secret-that-is-also-32-chars-long")

	token, err := ts1.Generate("user-123")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	_, err = ts2.Validate(token)
	if err == nil {
		t.Error("Validate: expected error for wrong secret, got nil")
	}
}

func TestTokenService_ShortSecret(t *testing.T) {
	_, err := NewTokenService("short")
	if err == nil {
		t.Error("NewTokenService: expected error for short secret, got nil")
	}
}
