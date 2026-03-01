package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"log/slog"
	"os"

	"github.com/sakif/coding-playground/internal/apperror"
	"github.com/sakif/coding-playground/internal/model"
	"github.com/sakif/coding-playground/internal/repository"
)

// =========================================================================
// MOCK REPOSITORY
// =========================================================================
//
// WHAT IS A MOCK?
// A mock is a fake implementation of an interface used in tests.
// Instead of talking to a real database, it stores data in memory.
//
// WHY MOCK?
// 1. SPEED: No database setup, no disk I/O, tests run in microseconds
// 2. ISOLATION: Tests only test the service logic, not the database
// 3. CONTROL: You can simulate errors (database down, connection timeout)
//    that would be hard to trigger with a real database
//
// HOW IT WORKS:
// mockSnippetRepo implements repository.SnippetRepository (same interface
// as sqlite.DB). The service doesn't know or care which one it gets.
// This is the power of interfaces — swappable implementations.
//
// In production code, you'd use a library like `github.com/stretchr/testify/mock`
// for more sophisticated mocks. For learning, a hand-written mock is clearer.

type mockSnippetRepo struct {
	snippets map[string]*model.Snippet // In-memory storage
	nextID   int                       // Auto-incrementing ID for testing
}

func newMockRepo() *mockSnippetRepo {
	return &mockSnippetRepo{
		snippets: make(map[string]*model.Snippet),
	}
}

func (m *mockSnippetRepo) Create(_ context.Context, snippet *model.Snippet) error {
	m.nextID++
	snippet.ID = fmt.Sprintf("mock-%d", m.nextID)
	// Store a copy (not the pointer) to avoid test interference
	stored := *snippet
	m.snippets[snippet.ID] = &stored
	return nil
}

func (m *mockSnippetRepo) GetByID(_ context.Context, id string) (*model.Snippet, error) {
	snippet, ok := m.snippets[id]
	if !ok {
		return nil, apperror.NotFound("snippet", id)
	}
	// Return a copy so the caller can't modify our internal state
	result := *snippet
	return &result, nil
}

func (m *mockSnippetRepo) List(_ context.Context, opts repository.ListOptions) ([]model.Snippet, error) {
	result := make([]model.Snippet, 0, len(m.snippets))
	for _, s := range m.snippets {
		result = append(result, *s)
	}

	// Apply basic pagination
	if opts.Offset >= len(result) {
		return []model.Snippet{}, nil
	}
	result = result[opts.Offset:]
	if opts.Limit > 0 && opts.Limit < len(result) {
		result = result[:opts.Limit]
	}

	return result, nil
}

func (m *mockSnippetRepo) Update(_ context.Context, snippet *model.Snippet) error {
	if _, ok := m.snippets[snippet.ID]; !ok {
		return apperror.NotFound("snippet", snippet.ID)
	}
	stored := *snippet
	m.snippets[snippet.ID] = &stored
	return nil
}

func (m *mockSnippetRepo) Delete(_ context.Context, id string) error {
	if _, ok := m.snippets[id]; !ok {
		return apperror.NotFound("snippet", id)
	}
	delete(m.snippets, id)
	return nil
}

// =========================================================================
// TEST HELPER
// =========================================================================

// newTestService creates a SnippetService with a mock repository.
// This is the dependency injection in action — we inject a mock instead of SQLite.
func newTestService(t *testing.T) (*SnippetService, *mockSnippetRepo) {
	t.Helper()
	repo := newMockRepo()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	svc := NewSnippetService(repo, logger)
	return svc, repo
}

// =========================================================================
// CREATE TESTS
// =========================================================================

func TestCreate_Success(t *testing.T) {
	svc, _ := newTestService(t)

	snippet, err := svc.Create(context.Background(), "hello world", "print('hi')", "a test", "")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if snippet.ID == "" {
		t.Error("expected snippet to have an ID")
	}
	if snippet.Name != "hello world" {
		t.Errorf("Name = %q, want %q", snippet.Name, "hello world")
	}
	if snippet.Code != "print('hi')" {
		t.Errorf("Code = %q, want %q", snippet.Code, "print('hi')")
	}
}

func TestCreate_TrimsWhitespace(t *testing.T) {
	svc, _ := newTestService(t)

	snippet, err := svc.Create(context.Background(), "  spaced out  ", "code", "  desc  ", "")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if snippet.Name != "spaced out" {
		t.Errorf("Name = %q, want trimmed %q", snippet.Name, "spaced out")
	}
	if snippet.Description != "desc" {
		t.Errorf("Description = %q, want trimmed %q", snippet.Description, "desc")
	}
}

func TestCreate_EmptyName(t *testing.T) {
	svc, _ := newTestService(t)

	_, err := svc.Create(context.Background(), "", "code", "", "")
	if err == nil {
		t.Fatal("Create() should error on empty name")
	}
	if !errors.Is(err, apperror.ErrValidation) {
		t.Errorf("error = %v, want ErrValidation", err)
	}
}

func TestCreate_WhitespaceOnlyName(t *testing.T) {
	svc, _ := newTestService(t)

	_, err := svc.Create(context.Background(), "   ", "code", "", "")
	if err == nil {
		t.Fatal("Create() should error on whitespace-only name")
	}
	if !errors.Is(err, apperror.ErrValidation) {
		t.Errorf("error = %v, want ErrValidation", err)
	}
}

func TestCreate_NameTooLong(t *testing.T) {
	svc, _ := newTestService(t)

	// Create a name that's too long (101 chars)
	longName := ""
	for i := 0; i < MaxSnippetNameLength+1; i++ {
		longName += "a"
	}

	_, err := svc.Create(context.Background(), longName, "code", "", "")
	if err == nil {
		t.Fatal("Create() should error on name that's too long")
	}
	if !errors.Is(err, apperror.ErrValidation) {
		t.Errorf("error = %v, want ErrValidation", err)
	}
}

// =========================================================================
// GET BY ID TESTS
// =========================================================================

func TestGetByID_Success(t *testing.T) {
	svc, _ := newTestService(t)

	// Create a snippet first
	created, err := svc.Create(context.Background(), "test", "code", "", "")
	if err != nil {
		t.Fatalf("setup: Create() error = %v", err)
	}

	// Fetch it
	found, err := svc.GetByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if found.Name != "test" {
		t.Errorf("Name = %q, want %q", found.Name, "test")
	}
}

func TestGetByID_NotFound(t *testing.T) {
	svc, _ := newTestService(t)

	_, err := svc.GetByID(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("GetByID() should error on nonexistent ID")
	}
	if !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestGetByID_EmptyID(t *testing.T) {
	svc, _ := newTestService(t)

	_, err := svc.GetByID(context.Background(), "")
	if err == nil {
		t.Fatal("GetByID() should error on empty ID")
	}
	if !errors.Is(err, apperror.ErrValidation) {
		t.Errorf("error = %v, want ErrValidation", err)
	}
}

// =========================================================================
// LIST TESTS
// =========================================================================

func TestList_Empty(t *testing.T) {
	svc, _ := newTestService(t)

	snippets, err := svc.List(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(snippets) != 0 {
		t.Errorf("List() returned %d items, want 0", len(snippets))
	}
}

func TestList_ClampsBadValues(t *testing.T) {
	svc, _ := newTestService(t)

	// Should not error even with negative values
	_, err := svc.List(context.Background(), -5, -10)
	if err != nil {
		t.Fatalf("List() should handle negative values gracefully, got error = %v", err)
	}
}

// =========================================================================
// UPDATE TESTS
// =========================================================================

func TestUpdate_Success(t *testing.T) {
	svc, _ := newTestService(t)

	created, _ := svc.Create(context.Background(), "original", "old code", "old desc", "")

	updated, err := svc.Update(context.Background(), created.ID, "new name", "new code", "new desc", "")
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if updated.Name != "new name" {
		t.Errorf("Name = %q, want %q", updated.Name, "new name")
	}
	if updated.Code != "new code" {
		t.Errorf("Code = %q, want %q", updated.Code, "new code")
	}
}

func TestUpdate_NotFound(t *testing.T) {
	svc, _ := newTestService(t)

	_, err := svc.Update(context.Background(), "nonexistent", "name", "code", "", "")
	if err == nil {
		t.Fatal("Update() should error on nonexistent ID")
	}
	if !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

// TestUpdate_WrongOwner ensures a caller who doesn't own the snippet gets ErrForbidden.
func TestUpdate_WrongOwner(t *testing.T) {
	svc, _ := newTestService(t)

	// Create owned by "user-a"
	owner := "user-a"
	created, _ := svc.Create(context.Background(), "owned", "code", "", owner)

	// Attempt update by "user-b"
	_, err := svc.Update(context.Background(), created.ID, "hack", "evil", "", "user-b")
	if err == nil {
		t.Fatal("Update() should return ErrForbidden for wrong owner")
	}
	if !errors.Is(err, apperror.ErrForbidden) {
		t.Errorf("error = %v, want ErrForbidden", err)
	}
}

// TestUpdate_OwnerCanUpdate ensures the owner can update their own snippet.
func TestUpdate_OwnerCanUpdate(t *testing.T) {
	svc, _ := newTestService(t)

	owner := "user-a"
	created, _ := svc.Create(context.Background(), "mine", "code", "", owner)

	_, err := svc.Update(context.Background(), created.ID, "updated", "new", "", owner)
	if err != nil {
		t.Fatalf("Owner should be able to update their own snippet: %v", err)
	}
}

// =========================================================================
// DELETE TESTS
// =========================================================================

func TestDelete_Success(t *testing.T) {
	svc, _ := newTestService(t)

	created, _ := svc.Create(context.Background(), "to delete", "code", "", "")
	err := svc.Delete(context.Background(), created.ID, "")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify it's gone
	_, err = svc.GetByID(context.Background(), created.ID)
	if !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("after delete: error = %v, want ErrNotFound", err)
	}
}

func TestDelete_EmptyID(t *testing.T) {
	svc, _ := newTestService(t)

	err := svc.Delete(context.Background(), "", "")
	if err == nil {
		t.Fatal("Delete() should error on empty ID")
	}
	if !errors.Is(err, apperror.ErrValidation) {
		t.Errorf("error = %v, want ErrValidation", err)
	}
}

// TestDelete_WrongOwner ensures a caller who doesn't own the snippet gets ErrForbidden.
func TestDelete_WrongOwner(t *testing.T) {
	svc, _ := newTestService(t)

	owner := "user-a"
	created, _ := svc.Create(context.Background(), "owned", "code", "", owner)

	err := svc.Delete(context.Background(), created.ID, "user-b")
	if err == nil {
		t.Fatal("Delete() should return ErrForbidden for wrong owner")
	}
	if !errors.Is(err, apperror.ErrForbidden) {
		t.Errorf("error = %v, want ErrForbidden", err)
	}
}
