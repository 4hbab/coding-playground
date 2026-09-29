package handler_test

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/sakif/coding-playground/internal/auth"
	"github.com/sakif/coding-playground/internal/handler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthHandler_CookieFlags(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	for _, secure := range []bool{true, false} {
		t.Run(fmt.Sprintf("logout cookie with secureCookies=%t", secure), func(t *testing.T) {
			// Logout needs neither the auth service nor the GitHub provider.
			h := handler.NewAuthHandler(nil, nil, logger, secure)

			rr := httptest.NewRecorder()
			h.HandleLogout(rr, httptest.NewRequest(http.MethodPost, "/auth/logout", nil))

			cookies := rr.Result().Cookies()
			require.Len(t, cookies, 1)
			c := cookies[0]
			assert.Equal(t, auth.CookieName, c.Name)
			assert.Equal(t, secure, c.Secure)
			assert.True(t, c.HttpOnly, "session cookie must never be readable from JS")
			assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
			assert.Less(t, c.MaxAge, 0, "logout must delete the cookie")
		})
	}

	t.Run("login sets a secure, HttpOnly OAuth state cookie", func(t *testing.T) {
		gh := auth.NewGitHubProvider("client-id", "client-secret", "https://example.test/auth/github/callback")
		h := handler.NewAuthHandler(nil, gh, logger, true)

		rr := httptest.NewRecorder()
		h.HandleGitHubLogin(rr, httptest.NewRequest(http.MethodGet, "/auth/github/login", nil))

		assert.Equal(t, http.StatusTemporaryRedirect, rr.Code)
		cookies := rr.Result().Cookies()
		require.Len(t, cookies, 1)
		assert.Equal(t, "oauth_state", cookies[0].Name)
		assert.NotEmpty(t, cookies[0].Value)
		assert.True(t, cookies[0].Secure)
		assert.True(t, cookies[0].HttpOnly)
	})
}
