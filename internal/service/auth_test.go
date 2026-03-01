package service

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/sakif/coding-playground/internal/auth"
	"github.com/sakif/coding-playground/internal/model"
)

// =========================================================================
// FAKES AND HELPERS
// =========================================================================

// fakeUserRepo is an in-memory implementation of repository.UserRepository.
// Using a fake (not a mock framework) keeps tests dependency-free and easy
// to read — you can see exactly what the fake does.
type fakeUserRepo struct {
	users  map[string]*model.User // keyed by internal ID
	byGHID map[int64]*model.User  // keyed by GitHub ID (for Upsert)
	nextID int
	// set to a non-nil error to simulate a database failure
	upsertErr  error
	getByIDErr error
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		users:  make(map[string]*model.User),
		byGHID: make(map[int64]*model.User),
		nextID: 1,
	}
}

func (f *fakeUserRepo) Upsert(ctx context.Context, user *model.User) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	// Simulate INSERT: assign an ID on first encounter
	if existing, ok := f.byGHID[user.GitHubID]; ok {
		// UPDATE path — keep ID, refresh profile fields
		existing.Login = user.Login
		existing.Email = user.Email
		existing.AvatarURL = user.AvatarURL
		// Reflect changes back into the caller's struct
		*user = *existing
	} else {
		// INSERT path — assign a new ID
		user.ID = "user-fake-id-" + string(rune('0'+f.nextID))
		f.nextID++
		user.CreatedAt = time.Now()
		user.UpdatedAt = time.Now()
		// Store a copy
		copied := *user
		f.users[user.ID] = &copied
		f.byGHID[user.GitHubID] = &copied
	}
	return nil
}

func (f *fakeUserRepo) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	if f.getByIDErr != nil {
		return nil, f.getByIDErr
	}
	u, ok := f.users[id]
	if !ok {
		return nil, errors.New("user not found")
	}
	return u, nil
}

// newTestAuthService returns an AuthService wired with fake dependencies.
// The TokenService uses a short secret, suitable for tests only.
func newTestAuthService(t *testing.T, repo *fakeUserRepo) *AuthService {
	t.Helper()

	ts, err := auth.NewTokenService("test-secret-at-least-16-chars!!")
	if err != nil {
		t.Fatalf("NewTokenService: %v", err)
	}

	// Cost 4 is bcrypt minimum — makes tests fast
	ps := auth.NewPasswordServiceForTest(4)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewAuthService(repo, ts, ps, logger)
}

// =========================================================================
// LoginOrRegisterGitHub TESTS
// =========================================================================

func TestLoginOrRegisterGitHub_NewUser(t *testing.T) {
	repo := newFakeUserRepo()
	svc := newTestAuthService(t, repo)

	ghUser := &auth.GitHubUser{
		ID:        42,
		Login:     "octocat",
		Email:     "octocat@github.com",
		AvatarURL: "https://avatars.githubusercontent.com/u/42",
	}

	result, err := svc.LoginOrRegisterGitHub(context.Background(), ghUser)
	if err != nil {
		t.Fatalf("LoginOrRegisterGitHub() error = %v", err)
	}

	if result.User == nil {
		t.Fatal("LoginOrRegisterGitHub() returned nil User")
	}
	if result.Token == "" {
		t.Fatal("LoginOrRegisterGitHub() returned empty Token")
	}
	if result.User.Login != "octocat" {
		t.Errorf("User.Login = %q, want %q", result.User.Login, "octocat")
	}
	if result.User.ID == "" {
		t.Error("User.ID should be set after upsert")
	}
}

func TestLoginOrRegisterGitHub_ExistingUserGetsUpdatedProfile(t *testing.T) {
	repo := newFakeUserRepo()
	svc := newTestAuthService(t, repo)

	firstLogin := &auth.GitHubUser{ID: 99, Login: "old-login", Email: "old@email.com"}
	if _, err := svc.LoginOrRegisterGitHub(context.Background(), firstLogin); err != nil {
		t.Fatalf("first login error: %v", err)
	}

	// Second login with updated profile
	secondLogin := &auth.GitHubUser{ID: 99, Login: "new-login", Email: "new@email.com"}
	result, err := svc.LoginOrRegisterGitHub(context.Background(), secondLogin)
	if err != nil {
		t.Fatalf("second login error: %v", err)
	}

	if result.User.Login != "new-login" {
		t.Errorf("User.Login after update = %q, want %q", result.User.Login, "new-login")
	}
}

func TestLoginOrRegisterGitHub_TokenIsValidJWT(t *testing.T) {
	repo := newFakeUserRepo()
	svc := newTestAuthService(t, repo)

	result, err := svc.LoginOrRegisterGitHub(context.Background(), &auth.GitHubUser{
		ID: 1, Login: "testuser",
	})
	if err != nil {
		t.Fatalf("LoginOrRegisterGitHub() error = %v", err)
	}

	// Validate the token we issued
	userID, err := svc.ValidateToken(result.Token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if userID != result.User.ID {
		t.Errorf("token subject = %q, want %q", userID, result.User.ID)
	}
}

func TestLoginOrRegisterGitHub_NilGitHubUser(t *testing.T) {
	repo := newFakeUserRepo()
	svc := newTestAuthService(t, repo)

	_, err := svc.LoginOrRegisterGitHub(context.Background(), nil)
	if err == nil {
		t.Fatal("LoginOrRegisterGitHub() should return error for nil GitHubUser")
	}
}

func TestLoginOrRegisterGitHub_RepositoryError(t *testing.T) {
	repo := newFakeUserRepo()
	repo.upsertErr = errors.New("database is on fire")
	svc := newTestAuthService(t, repo)

	_, err := svc.LoginOrRegisterGitHub(context.Background(), &auth.GitHubUser{ID: 1, Login: "user"})
	if err == nil {
		t.Fatal("LoginOrRegisterGitHub() should propagate repository errors")
	}
}

// =========================================================================
// GetUserByID TESTS
// =========================================================================

func TestGetUserByID_Found(t *testing.T) {
	repo := newFakeUserRepo()
	svc := newTestAuthService(t, repo)

	// Register a user first so we have a valid ID
	result, err := svc.LoginOrRegisterGitHub(context.Background(), &auth.GitHubUser{
		ID: 7, Login: "findme",
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	user, err := svc.GetUserByID(context.Background(), result.User.ID)
	if err != nil {
		t.Fatalf("GetUserByID() error = %v", err)
	}
	if user.Login != "findme" {
		t.Errorf("user.Login = %q, want %q", user.Login, "findme")
	}
}

func TestGetUserByID_EmptyID(t *testing.T) {
	repo := newFakeUserRepo()
	svc := newTestAuthService(t, repo)

	_, err := svc.GetUserByID(context.Background(), "")
	if err == nil {
		t.Fatal("GetUserByID() should return error for empty ID")
	}
}

func TestGetUserByID_NotFound(t *testing.T) {
	repo := newFakeUserRepo()
	svc := newTestAuthService(t, repo)

	_, err := svc.GetUserByID(context.Background(), "non-existent-id")
	if err == nil {
		t.Fatal("GetUserByID() should return error for unknown ID")
	}
}

// =========================================================================
// ValidateToken TESTS
// =========================================================================

func TestValidateToken_ValidToken(t *testing.T) {
	repo := newFakeUserRepo()
	svc := newTestAuthService(t, repo)

	result, _ := svc.LoginOrRegisterGitHub(context.Background(), &auth.GitHubUser{ID: 5, Login: "tok"})

	userID, err := svc.ValidateToken(result.Token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if userID != result.User.ID {
		t.Errorf("userID = %q, want %q", userID, result.User.ID)
	}
}

func TestValidateToken_InvalidToken(t *testing.T) {
	repo := newFakeUserRepo()
	svc := newTestAuthService(t, repo)

	_, err := svc.ValidateToken("this.is.garbage")
	if err == nil {
		t.Fatal("ValidateToken() should return error for garbage token")
	}
}
