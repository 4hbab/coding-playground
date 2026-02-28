package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/sakif/coding-playground/internal/apperror"
	"github.com/sakif/coding-playground/internal/model"
)

// newTestUserDB is a helper that returns a *UserDB backed by the same in-memory DB.
// It mirrors newTestDB from snippet_test.go.
func newTestUserDB(t *testing.T) (*DB, *UserDB) {
	t.Helper()
	db := newTestDB(t)
	return db, db.Users()
}

// createTestUser is a test helper that creates a user and fails the test if it errors.
func createTestUser(t *testing.T, u *UserDB, githubID int64, login string) *model.User {
	t.Helper()
	user := &model.User{
		GitHubID:  githubID,
		Login:     login,
		Email:     login + "@example.com",
		AvatarURL: "https://avatars.githubusercontent.com/u/123",
	}
	if err := u.Create(context.Background(), user); err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	return user
}

// =========================================================================
// CREATE TESTS
// =========================================================================

func TestUserCreate(t *testing.T) {
	_, u := newTestUserDB(t)

	user := &model.User{
		GitHubID:  12345,
		Login:     "testuser",
		Email:     "test@example.com",
		AvatarURL: "https://example.com/avatar.png",
	}

	err := u.Create(context.Background(), user)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Verify the user was modified in-place (pointer receiver)
	if user.ID == "" {
		t.Error("Create() did not set user.ID")
	}
	if user.CreatedAt.IsZero() {
		t.Error("Create() did not set user.CreatedAt")
	}
	if user.UpdatedAt.IsZero() {
		t.Error("Create() did not set user.UpdatedAt")
	}

	t.Logf("Created user with ID: %s", user.ID)
}

func TestUserCreate_DuplicateGitHubID(t *testing.T) {
	_, u := newTestUserDB(t)

	// Same GitHubID — second create should fail (UNIQUE constraint)
	createTestUser(t, u, 99999, "firstuser")

	duplicate := &model.User{
		GitHubID: 99999, // same GitHub ID
		Login:    "seconduser",
	}
	err := u.Create(context.Background(), duplicate)
	if err == nil {
		t.Fatal("Create() should have returned an error for duplicate github_id")
	}
}

// =========================================================================
// GET BY ID TESTS
// =========================================================================

func TestUserGetByID(t *testing.T) {
	_, u := newTestUserDB(t)
	created := createTestUser(t, u, 111, "getbyid_user")

	found, err := u.GetByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	if found.ID != created.ID {
		t.Errorf("ID = %q, want %q", found.ID, created.ID)
	}
	if found.Login != "getbyid_user" {
		t.Errorf("Login = %q, want %q", found.Login, "getbyid_user")
	}
	if found.GitHubID != 111 {
		t.Errorf("GitHubID = %d, want %d", found.GitHubID, 111)
	}
}

func TestUserGetByID_NotFound(t *testing.T) {
	_, u := newTestUserDB(t)

	_, err := u.GetByID(context.Background(), "nonexistent-id")

	if err == nil {
		t.Fatal("GetByID() should have returned an error for nonexistent ID")
	}
	if !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("GetByID() error = %v, want ErrNotFound", err)
	}
}

// =========================================================================
// GET BY GITHUB ID TESTS
// =========================================================================

func TestUserGetByGitHubID(t *testing.T) {
	_, u := newTestUserDB(t)
	created := createTestUser(t, u, 778899, "github_lookup_user")

	found, err := u.GetByGitHubID(context.Background(), 778899)
	if err != nil {
		t.Fatalf("GetByGitHubID() error = %v", err)
	}

	if found.ID != created.ID {
		t.Errorf("ID = %q, want %q", found.ID, created.ID)
	}
	if found.GitHubID != 778899 {
		t.Errorf("GitHubID = %d, want 778899", found.GitHubID)
	}
}

func TestUserGetByGitHubID_NotFound(t *testing.T) {
	_, u := newTestUserDB(t)

	_, err := u.GetByGitHubID(context.Background(), 999999999)

	if err == nil {
		t.Fatal("GetByGitHubID() should have returned an error for nonexistent github_id")
	}
	if !errors.Is(err, apperror.ErrNotFound) {
		t.Errorf("GetByGitHubID() error = %v, want ErrNotFound", err)
	}
}

// =========================================================================
// UPSERT TESTS
// =========================================================================

func TestUserUpsert_NewUser(t *testing.T) {
	_, u := newTestUserDB(t)

	user := &model.User{
		GitHubID:  55555,
		Login:     "new_upsert_user",
		Email:     "new@example.com",
		AvatarURL: "https://example.com/new.png",
	}

	err := u.Upsert(context.Background(), user)
	if err != nil {
		t.Fatalf("Upsert() (new) error = %v", err)
	}

	if user.ID == "" {
		t.Error("Upsert() did not set user.ID for new user")
	}
	if user.CreatedAt.IsZero() {
		t.Error("Upsert() did not set user.CreatedAt for new user")
	}

	// Verify it's actually in the DB
	found, err := u.GetByGitHubID(context.Background(), 55555)
	if err != nil {
		t.Fatalf("GetByGitHubID() after Upsert: %v", err)
	}
	if found.Login != "new_upsert_user" {
		t.Errorf("Login = %q, want %q", found.Login, "new_upsert_user")
	}
}

func TestUserUpsert_ExistingUser_UpdatesProfile(t *testing.T) {
	_, u := newTestUserDB(t)

	// First login — inserts the user
	first := &model.User{
		GitHubID:  66666,
		Login:     "original_login",
		Email:     "old@example.com",
		AvatarURL: "https://example.com/old.png",
	}
	if err := u.Upsert(context.Background(), first); err != nil {
		t.Fatalf("Upsert() first login: %v", err)
	}
	originalID := first.ID

	// Second login — same GitHubID but updated profile
	second := &model.User{
		GitHubID:  66666, // same GitHub account
		Login:     "updated_login",
		Email:     "new@example.com",
		AvatarURL: "https://example.com/new.png",
	}
	if err := u.Upsert(context.Background(), second); err != nil {
		t.Fatalf("Upsert() second login: %v", err)
	}

	// The internal ID must NOT have changed — same user, same ID
	if second.ID != originalID {
		t.Errorf("Upsert() changed user ID: got %q, want %q", second.ID, originalID)
	}

	// But the profile fields should be updated
	found, err := u.GetByGitHubID(context.Background(), 66666)
	if err != nil {
		t.Fatalf("GetByGitHubID() after second Upsert: %v", err)
	}
	if found.Login != "updated_login" {
		t.Errorf("Login after upsert = %q, want %q", found.Login, "updated_login")
	}
	if found.Email != "new@example.com" {
		t.Errorf("Email after upsert = %q, want %q", found.Email, "new@example.com")
	}
	if found.ID != originalID {
		t.Errorf("ID after upsert = %q, want %q", found.ID, originalID)
	}

	t.Logf("User ID preserved across upserts: %s", originalID)
}

func TestUserUpsert_DoesNotChangeCreatedAt(t *testing.T) {
	_, u := newTestUserDB(t)

	// First upsert
	usr := &model.User{GitHubID: 77777, Login: "timecheck"}
	if err := u.Upsert(context.Background(), usr); err != nil {
		t.Fatalf("Upsert() first: %v", err)
	}
	originalCreatedAt := usr.CreatedAt

	// Second upsert
	usr2 := &model.User{GitHubID: 77777, Login: "timecheck_updated"}
	if err := u.Upsert(context.Background(), usr2); err != nil {
		t.Fatalf("Upsert() second: %v", err)
	}

	// CreatedAt on the returned struct should match the original
	if !usr2.CreatedAt.Equal(originalCreatedAt) {
		t.Errorf("Upsert() changed CreatedAt: got %v, want %v", usr2.CreatedAt, originalCreatedAt)
	}
}

// =========================================================================
// SNIPPET MIGRATION TESTS
// =========================================================================

// TestSnippetHasUserIDColumn verifies that the v2 migration correctly adds
// the user_id column to the snippets table. We test this by creating a snippet
// owned by a user and reading user_id directly from the DB.
func TestSnippetHasUserIDColumn(t *testing.T) {
	db, u := newTestUserDB(t)

	// Create a user and a snippet owned by them
	user := createTestUser(t, u, 88888, "owner")
	snippet := &model.Snippet{
		Name:   "owned snippet",
		Code:   "print(1)",
		UserID: &user.ID,
	}
	if err := db.Create(context.Background(), snippet); err != nil {
		t.Fatalf("Create snippet with UserID: %v", err)
	}
	if snippet.ID == "" {
		t.Fatal("snippet ID not set")
	}

	// Read user_id directly from DB to confirm the column exists and was written
	var readUserID *string
	row := db.conn.QueryRowContext(context.Background(),
		`SELECT user_id FROM snippets WHERE id = ?`, snippet.ID)
	if err := row.Scan(&readUserID); err != nil {
		t.Fatalf("reading user_id from snippets: %v", err)
	}
	if readUserID == nil {
		t.Fatal("user_id was NULL, expected it to be set")
	}
	if *readUserID != user.ID {
		t.Errorf("user_id = %q, want %q", *readUserID, user.ID)
	}
}
