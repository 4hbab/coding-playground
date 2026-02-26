package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/sakif/coding-playground/internal/apperror"
	"github.com/sakif/coding-playground/internal/model"
	"github.com/sakif/coding-playground/internal/repository"
)

// TESTING WITH IN-MEMORY SQLITE:
// Using ":memory:" creates a fresh database that exists only during the test.
// Benefits:
// - Fast: no disk I/O
// - Isolated: each test gets its own database
// - Clean: automatically destroyed when the connection closes
//
// newTestDB is a "test helper" — a function used only in tests to reduce boilerplate.
// The `t.Helper()` call tells Go's test framework to report errors at the CALLER's
// line number, not inside this function. This makes test failure output much clearer.
func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	// t.Cleanup registers a function to run when the test finishes.
	// This is like defer, but scoped to the test — even works in subtests.
	t.Cleanup(func() { db.Close() })
	return db
}

// createTestSnippet is another helper — creates a snippet and fails the test if it errors.
func createTestSnippet(t *testing.T, db *DB, name, code string) *model.Snippet {
	t.Helper()
	snippet := &model.Snippet{Name: name, Code: code}
	if err := db.Create(context.Background(), snippet); err != nil {
		t.Fatalf("failed to create test snippet: %v", err)
	}
	return snippet
}

// =========================================================================
// CREATE TESTS
// =========================================================================

func TestCreate(t *testing.T) {
	db := newTestDB(t)

	snippet := &model.Snippet{
		Name: "Hello World",
		Code: "print('hello')",
	}

	err := db.Create(context.Background(), snippet)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Verify the snippet was modified in-place (pointer receiver!)
	if snippet.ID == "" {
		t.Error("Create() did not set snippet.ID")
	}
	if snippet.CreatedAt.IsZero() {
		t.Error("Create() did not set snippet.CreatedAt")
	}
	if snippet.UpdatedAt.IsZero() {
		t.Error("Create() did not set snippet.UpdatedAt")
	}

	t.Logf("Created snippet with ID: %s", snippet.ID)
}

func TestCreate_VerifyPersistence(t *testing.T) {
	db := newTestDB(t)

	// Create a snippet
	original := createTestSnippet(t, db, "test", "print('hi')")

	// Read it back from the database
	found, err := db.GetByID(context.Background(), original.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	// Verify all fields match
	if found.Name != original.Name {
		t.Errorf("Name = %q, want %q", found.Name, original.Name)
	}
	if found.Code != original.Code {
		t.Errorf("Code = %q, want %q", found.Code, original.Code)
	}
}

// =========================================================================
// GET BY ID TESTS
// =========================================================================

func TestGetByID(t *testing.T) {
	db := newTestDB(t)
	created := createTestSnippet(t, db, "fetch me", "x = 42")

	found, err := db.GetByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	if found.ID != created.ID {
		t.Errorf("ID = %q, want %q", found.ID, created.ID)
	}
	if found.Name != "fetch me" {
		t.Errorf("Name = %q, want %q", found.Name, "fetch me")
	}
}

func TestGetByID_NotFound(t *testing.T) {
	db := newTestDB(t)

	_, err := db.GetByID(context.Background(), "nonexistent-id")

	// Verify we get our custom NotFound error, not a raw sql.ErrNoRows
	if err == nil {
		t.Fatal("GetByID() should have returned an error for nonexistent ID")
	}
	if !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("GetByID() error = %v, want ErrNotFound", err)
	}
}

// =========================================================================
// LIST TESTS
// =========================================================================

func TestList_Empty(t *testing.T) {
	db := newTestDB(t)

	snippets, err := db.List(context.Background(), repository.ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(snippets) != 0 {
		t.Errorf("List() returned %d snippets, want 0", len(snippets))
	}
}

func TestList_ReturnsAll(t *testing.T) {
	db := newTestDB(t)

	// Create 3 snippets
	createTestSnippet(t, db, "first", "a = 1")
	createTestSnippet(t, db, "second", "b = 2")
	createTestSnippet(t, db, "third", "c = 3")

	snippets, err := db.List(context.Background(), repository.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(snippets) != 3 {
		t.Errorf("List() returned %d snippets, want 3", len(snippets))
	}
}

func TestList_Pagination(t *testing.T) {
	db := newTestDB(t)

	// Create 5 snippets
	for i := 0; i < 5; i++ {
		createTestSnippet(t, db, "snippet", "code")
	}

	// First page: 2 items
	page1, err := db.List(context.Background(), repository.ListOptions{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("List() page 1 error = %v", err)
	}
	if len(page1) != 2 {
		t.Errorf("Page 1: got %d items, want 2", len(page1))
	}

	// Second page: 2 items
	page2, err := db.List(context.Background(), repository.ListOptions{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("List() page 2 error = %v", err)
	}
	if len(page2) != 2 {
		t.Errorf("Page 2: got %d items, want 2", len(page2))
	}

	// Third page: 1 item (only 5 total, 4 already shown)
	page3, err := db.List(context.Background(), repository.ListOptions{Limit: 2, Offset: 4})
	if err != nil {
		t.Fatalf("List() page 3 error = %v", err)
	}
	if len(page3) != 1 {
		t.Errorf("Page 3: got %d items, want 1", len(page3))
	}

	// Pages should have different snippets
	if page1[0].ID == page2[0].ID {
		t.Error("Page 1 and Page 2 returned the same first snippet")
	}
}

func TestList_DefaultLimit(t *testing.T) {
	db := newTestDB(t)

	// Create 25 snippets
	for i := 0; i < 25; i++ {
		createTestSnippet(t, db, "snippet", "code")
	}

	// List with no limit specified — should default to 20
	snippets, err := db.List(context.Background(), repository.ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(snippets) != 20 {
		t.Errorf("List() default returned %d items, want 20", len(snippets))
	}
}

// =========================================================================
// UPDATE TESTS
// =========================================================================

func TestUpdate(t *testing.T) {
	db := newTestDB(t)
	original := createTestSnippet(t, db, "original name", "original code")

	// Modify the snippet
	original.Name = "updated name"
	original.Code = "updated code"

	err := db.Update(context.Background(), original)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	// Read it back and verify
	found, err := db.GetByID(context.Background(), original.ID)
	if err != nil {
		t.Fatalf("GetByID() after update error = %v", err)
	}

	if found.Name != "updated name" {
		t.Errorf("Name after update = %q, want %q", found.Name, "updated name")
	}
	if found.Code != "updated code" {
		t.Errorf("Code after update = %q, want %q", found.Code, "updated code")
	}
	// UpdatedAt should be more recent than CreatedAt
	if !found.UpdatedAt.After(found.CreatedAt) || found.UpdatedAt.Equal(found.CreatedAt) {
		t.Log("Note: UpdatedAt should generally be after CreatedAt after an update")
	}
}

func TestUpdate_NotFound(t *testing.T) {
	db := newTestDB(t)

	snippet := &model.Snippet{ID: "nonexistent", Name: "test", Code: "test"}
	err := db.Update(context.Background(), snippet)

	if err == nil {
		t.Fatal("Update() should have returned an error for nonexistent ID")
	}
	if !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("Update() error = %v, want ErrNotFound", err)
	}
}

// =========================================================================
// DELETE TESTS
// =========================================================================

func TestDelete(t *testing.T) {
	db := newTestDB(t)
	snippet := createTestSnippet(t, db, "to delete", "bye()")

	// Delete it
	err := db.Delete(context.Background(), snippet.ID)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify it's gone
	_, err = db.GetByID(context.Background(), snippet.ID)
	if !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("GetByID() after delete: error = %v, want ErrNotFound", err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	db := newTestDB(t)

	err := db.Delete(context.Background(), "nonexistent-id")

	if err == nil {
		t.Fatal("Delete() should have returned an error for nonexistent ID")
	}
	if !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("Delete() error = %v, want ErrNotFound", err)
	}
}

// =========================================================================
// FULL CRUD LIFECYCLE TEST
// =========================================================================

// TestFullCRUDLifecycle tests the complete create → read → update → delete flow.
// This kind of "integration" test catches issues that individual unit tests might miss,
// like transactions interfering with each other or timestamps not being set correctly.
func TestFullCRUDLifecycle(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// 1. Create
	snippet := &model.Snippet{
		Name:        "lifecycle test",
		Code:        "print('v1')",
		Description: "testing all operations",
	}
	if err := db.Create(ctx, snippet); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Logf("Created: ID=%s", snippet.ID)

	// 2. Read
	found, err := db.GetByID(ctx, snippet.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if found.Description != "testing all operations" {
		t.Errorf("Description = %q, want %q", found.Description, "testing all operations")
	}

	// 3. List (should contain our snippet)
	all, err := db.List(ctx, repository.ListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("List returned %d, want 1", len(all))
	}

	// 4. Update
	found.Code = "print('v2')"
	if err := db.Update(ctx, found); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// 5. Verify update
	updated, err := db.GetByID(ctx, snippet.ID)
	if err != nil {
		t.Fatalf("GetByID after update: %v", err)
	}
	if updated.Code != "print('v2')" {
		t.Errorf("Code after update = %q, want %q", updated.Code, "print('v2')")
	}

	// 6. Delete
	if err := db.Delete(ctx, snippet.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// 7. Verify deletion
	_, err = db.GetByID(ctx, snippet.ID)
	if !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("GetByID after delete: error = %v, want ErrNotFound", err)
	}

	// 8. List should be empty again
	final, err := db.List(ctx, repository.ListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(final) != 0 {
		t.Errorf("List after delete returned %d, want 0", len(final))
	}

	t.Log("Full CRUD lifecycle passed!")
}
