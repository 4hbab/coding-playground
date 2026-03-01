package auth

import (
	"strings"
	"testing"
)

// =========================================================================
// HELPER
// =========================================================================

// newTestPasswordService returns a PasswordService with bcrypt cost 4.
// Cost 4 is the minimum allowed by the bcrypt library. This makes tests
// run in milliseconds instead of ~250ms each.
func newTestPasswordService() *PasswordService {
	return newPasswordServiceWithCost(4)
}

// =========================================================================
// Hash TESTS
// =========================================================================

func TestHash_ReturnsNonEmptyHash(t *testing.T) {
	ps := newTestPasswordService()

	hash, err := ps.Hash("my-secret-password")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if hash == "" {
		t.Error("Hash() returned empty string")
	}
}

func TestHash_OutputLooksBcrypt(t *testing.T) {
	ps := newTestPasswordService()

	hash, err := ps.Hash("password123")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	// bcrypt hashes always start with $2a$ or $2b$
	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("Hash() does not look like a bcrypt hash: %q", hash)
	}
}

func TestHash_SamePasswordProducesDifferentHashes(t *testing.T) {
	ps := newTestPasswordService()

	// bcrypt generates a random salt each time, so two hashes for the
	// same password must differ — otherwise rainbow tables would work.
	hash1, _ := ps.Hash("same-password")
	hash2, _ := ps.Hash("same-password")

	if hash1 == hash2 {
		t.Error("Hash() produced identical hashes for the same password (salt must be random)")
	}
}

func TestHash_RejectsPasswordOver72Bytes(t *testing.T) {
	ps := newTestPasswordService()

	// bcrypt silently truncates at 72 bytes — we reject it explicitly.
	longPassword := strings.Repeat("a", 73)
	_, err := ps.Hash(longPassword)
	if err == nil {
		t.Fatal("Hash() should return an error for passwords longer than 72 bytes")
	}
}

func TestHash_AcceptsPasswordExactly72Bytes(t *testing.T) {
	ps := newTestPasswordService()

	exactPassword := strings.Repeat("a", 72)
	_, err := ps.Hash(exactPassword)
	if err != nil {
		t.Fatalf("Hash() should accept a 72-byte password, got error: %v", err)
	}
}

// =========================================================================
// Verify TESTS
// =========================================================================

func TestVerify_CorrectPassword(t *testing.T) {
	ps := newTestPasswordService()

	hash, err := ps.Hash("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	if err := ps.Verify(hash, "correct-horse-battery-staple"); err != nil {
		t.Errorf("Verify() should return nil for a correct password, got: %v", err)
	}
}

func TestVerify_WrongPassword(t *testing.T) {
	ps := newTestPasswordService()

	hash, _ := ps.Hash("the-real-password")

	err := ps.Verify(hash, "the-wrong-password")
	if err == nil {
		t.Fatal("Verify() should return an error for a wrong password")
	}
	t.Logf("Wrong password error (expected): %v", err)
}

func TestVerify_EmptyPassword(t *testing.T) {
	ps := newTestPasswordService()

	hash, _ := ps.Hash("some-password")

	err := ps.Verify(hash, "")
	if err == nil {
		t.Fatal("Verify() should return an error when password is empty")
	}
}

func TestVerify_GarbageHash(t *testing.T) {
	ps := newTestPasswordService()

	err := ps.Verify("not-a-valid-bcrypt-hash", "password")
	if err == nil {
		t.Fatal("Verify() should return an error for a garbage hash")
	}
}

// =========================================================================
// ROUND-TRIP TEST
// =========================================================================

func TestHashVerify_RoundTrip(t *testing.T) {
	ps := newTestPasswordService()

	cases := []struct {
		name     string
		password string
	}{
		{"simple alphanumeric", "hello123"},
		{"special characters", "p@$$w0rd!#%"},
		{"unicode", "пароль-密码"},
		{"whitespace", "  leading and trailing  "},
		{"empty-ish", " "},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hash, err := ps.Hash(tc.password)
			if err != nil {
				t.Fatalf("Hash(%q) error = %v", tc.password, err)
			}

			if err := ps.Verify(hash, tc.password); err != nil {
				t.Errorf("Verify() failed for %q: %v", tc.password, err)
			}
		})
	}
}
