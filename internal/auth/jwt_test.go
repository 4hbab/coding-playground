package auth

import (
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

	// Tamper with the last character
	tampered := token[:len(token)-1] + "X"
	_, err = ts.Validate(tampered)
	if err == nil {
		t.Error("Validate: expected error for tampered token, got nil")
	}
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
