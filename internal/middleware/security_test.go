package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSecurityHeaders(t *testing.T) {
	serve := func(hsts bool) http.Header {
		rr := httptest.NewRecorder()
		SecurityHeaders(hsts)(ok).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
		return rr.Header()
	}

	t.Run("CSP denies everything by default", func(t *testing.T) {
		csp := serve(false).Get("Content-Security-Policy")
		assert.True(t, strings.HasPrefix(csp, "default-src 'none';"), csp)
		assert.Contains(t, csp, "frame-ancestors 'none'")
		assert.NotContains(t, csp, "'unsafe-eval'")
		assert.NotContains(t, csp, "script-src 'self' 'unsafe-inline'")
	})

	t.Run("basic hardening headers", func(t *testing.T) {
		h := serve(false)
		assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
		assert.Equal(t, "DENY", h.Get("X-Frame-Options"))
		assert.Equal(t, "strict-origin-when-cross-origin", h.Get("Referrer-Policy"))
	})

	t.Run("HSTS only when served over HTTPS", func(t *testing.T) {
		assert.Empty(t, serve(false).Get("Strict-Transport-Security"))
		assert.Equal(t, "max-age=31536000", serve(true).Get("Strict-Transport-Security"))
	})
}
