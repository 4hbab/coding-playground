// GO TESTING BASICS:
// 1. Test files MUST end in _test.go — Go's tooling auto-discovers them
// 2. Test functions MUST start with "Test" and take *testing.T as the only param
// 3. Same package as the code being tested (so we can access unexported stuff)
// 4. Run with: go test ./internal/apperror/ -v  (-v = verbose, shows each test name)
package apperror

import (
	"errors"
	"testing"
)

// TABLE-DRIVEN TESTS:
// This is Go's idiomatic pattern for testing multiple cases.
// Instead of writing 5 separate test functions, we define a slice of test cases
// and loop over them. Benefits:
// - Adding a new test case = adding one struct to the slice
// - Every case gets a name (shows up in test output)
// - DRY — the assertion logic is written once

func TestErrorsIs(t *testing.T) {
	// Each test case checks that errors.Is() correctly identifies the error type
	tests := []struct {
		name     string // Descriptive name for test output
		err      error  // The error to test
		target   error  // What we expect it to match
		wantMatch bool  // Should errors.Is() return true?
	}{
		{
			name:      "NotFound wraps ErrNotFound",
			err:       NotFound("snippet", "abc123"),
			target:    ErrNotFound,
			wantMatch: true,
		},
		{
			name:      "ValidationFailed wraps ErrValidation",
			err:       ValidationFailed("name", "name is required"),
			target:    ErrValidation,
			wantMatch: true,
		},
		{
			name:      "Conflict wraps ErrConflict",
			err:       Conflict("snippet", "abc123"),
			target:    ErrConflict,
			wantMatch: true,
		},
		{
			name:      "NotFound does NOT match ErrValidation",
			err:       NotFound("snippet", "abc123"),
			target:    ErrValidation,
			wantMatch: false,
		},
		{
			name:      "ValidationFailed does NOT match ErrNotFound",
			err:       ValidationFailed("name", "too long"),
			target:    ErrNotFound,
			wantMatch: false,
		},
	}

	// t.Run() creates a sub-test for each case.
	// Output looks like: TestErrorsIs/NotFound_wraps_ErrNotFound
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errors.Is(tt.err, tt.target)
			if got != tt.wantMatch {
				// t.Errorf marks the test as failed but continues running other tests
				// (vs t.Fatalf which stops immediately)
				t.Errorf("errors.Is(%v, %v) = %v, want %v", tt.err, tt.target, got, tt.wantMatch)
			}
		})
	}
}

func TestErrorMessages(t *testing.T) {
	tests := []struct {
		name        string
		err         *AppError
		wantMessage string
	}{
		{
			name:        "NotFound message includes resource and id",
			err:         NotFound("snippet", "abc123"),
			wantMessage: "snippet not found with id abc123",
		},
		{
			name:        "ValidationFailed uses custom message",
			err:         ValidationFailed("name", "name is required"),
			wantMessage: "name is required",
		},
		{
			name:        "Conflict message includes resource and id",
			err:         Conflict("snippet", "abc123"),
			wantMessage: "snippet conflict with id abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// .Error() should return the human-readable message
			if got := tt.err.Error(); got != tt.wantMessage {
				t.Errorf("Error() = %q, want %q", got, tt.wantMessage)
			}
		})
	}
}

func TestUnwrap(t *testing.T) {
	// Verify that Unwrap() returns the underlying sentinel error.
	// This is what makes errors.Is() work — it "unwraps" the chain.
	err := NotFound("snippet", "abc123")
	unwrapped := err.Unwrap()

	if unwrapped != ErrNotFound {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, ErrNotFound)
	}
}

func TestValidationFailedField(t *testing.T) {
	// Verify that the Field is set correctly for validation errors.
	// This lets handlers tell the frontend WHICH field was invalid.
	err := ValidationFailed("email", "invalid email format")

	if err.Field != "email" {
		t.Errorf("Field = %q, want %q", err.Field, "email")
	}
}
