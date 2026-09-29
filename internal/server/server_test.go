package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/sakif/coding-playground/internal/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubExecutor struct{}

func (stubExecutor) Execute(context.Context, executor.ExecutionRequest) (*executor.ExecutionResult, error) {
	return &executor.ExecutionResult{Stdout: "ok\n"}, nil
}

// newTestServer builds a real server (routes, middleware, SQLite) against a temp database.
func newTestServer(t *testing.T, cfg Config, exec executor.Executor) *Server {
	t.Helper()
	cfg.Port = 8080
	cfg.TemplateDir = filepath.Join("..", "..", "web", "templates")
	cfg.StaticDir = filepath.Join("..", "..", "web", "static")
	cfg.DBPath = filepath.Join(t.TempDir(), "test.db")
	if cfg.RateLimitDefault == 0 {
		cfg.RateLimitDefault = 1000
	}
	if cfg.RateLimitStrict == 0 {
		cfg.RateLimitStrict = 1000
	}

	s, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), exec)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.db.Close() })
	return s
}

func (s *Server) do(method, path string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)
	return rr
}

func executeBody() io.Reader { return bytes.NewBufferString(`{"code":"print(1)"}`) }

func TestServer(t *testing.T) {
	t.Run("health check", func(t *testing.T) {
		s := newTestServer(t, Config{}, nil)
		rr := s.do(http.MethodGet, "/healthz", nil, nil)
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.JSONEq(t, `{"status":"ok"}`, rr.Body.String())
	})

	t.Run("config reports whether server execution is available", func(t *testing.T) {
		for _, tc := range []struct {
			exec executor.Executor
			want bool
		}{{nil, false}, {stubExecutor{}, true}} {
			s := newTestServer(t, Config{}, tc.exec)
			rr := s.do(http.MethodGet, "/api/config", nil, nil)
			require.Equal(t, http.StatusOK, rr.Code)

			var cfg map[string]bool
			require.NoError(t, json.NewDecoder(rr.Body).Decode(&cfg))
			assert.Equal(t, tc.want, cfg["serverExecution"])
		}
	})

	t.Run("execute route only exists with an executor", func(t *testing.T) {
		s := newTestServer(t, Config{}, nil)
		assert.Equal(t, http.StatusNotFound, s.do(http.MethodPost, "/api/execute", executeBody(), nil).Code)
	})

	t.Run("pages carry security headers", func(t *testing.T) {
		s := newTestServer(t, Config{}, nil)
		rr := s.do(http.MethodGet, "/", nil, nil)
		require.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Header().Get("Content-Security-Policy"), "default-src 'none'")
		assert.Empty(t, rr.Header().Get("Strict-Transport-Security"), "no HSTS on plain http")
	})

	t.Run("an https public URL turns on HSTS", func(t *testing.T) {
		s := newTestServer(t, Config{PublicURL: "https://pyplayground.example.workers.dev"}, nil)
		rr := s.do(http.MethodGet, "/", nil, nil)
		assert.NotEmpty(t, rr.Header().Get("Strict-Transport-Security"))
	})

	t.Run("execute has its own strict limit", func(t *testing.T) {
		s := newTestServer(t, Config{RateLimitStrict: 2}, stubExecutor{})

		assert.Equal(t, http.StatusOK, s.do(http.MethodPost, "/api/execute", executeBody(), nil).Code)
		assert.Equal(t, http.StatusOK, s.do(http.MethodPost, "/api/execute", executeBody(), nil).Code)
		assert.Equal(t, http.StatusTooManyRequests, s.do(http.MethodPost, "/api/execute", executeBody(), nil).Code)

		// Other routes are on the default limit and keep working.
		assert.Equal(t, http.StatusOK, s.do(http.MethodGet, "/api/config", nil, nil).Code)
	})

	t.Run("visitors behind the proxy get separate budgets", func(t *testing.T) {
		// All requests arrive from the proxy's address; only the trusted header differs.
		s := newTestServer(t, Config{RateLimitStrict: 1, ClientIPHeader: "X-Client-IP"}, stubExecutor{})
		alice := map[string]string{"X-Client-IP": "198.51.100.1"}
		bob := map[string]string{"X-Client-IP": "198.51.100.2"}

		assert.Equal(t, http.StatusOK, s.do(http.MethodPost, "/api/execute", executeBody(), alice).Code)
		assert.Equal(t, http.StatusTooManyRequests, s.do(http.MethodPost, "/api/execute", executeBody(), alice).Code)
		assert.Equal(t, http.StatusOK, s.do(http.MethodPost, "/api/execute", executeBody(), bob).Code)
	})
}
