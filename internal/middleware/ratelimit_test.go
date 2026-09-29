package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestLimiter returns a limiter with a controllable clock.
func newTestLimiter(limit int, window time.Duration) (*RateLimiter, *time.Time) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	rl := NewRateLimiter(limit, window)
	rl.now = func() time.Time { return now }
	return rl, &now
}

func send(h http.Handler, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip // ClientIP runs first in the real server and leaves just the IP here
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

var ok = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

func TestRateLimiter(t *testing.T) {
	t.Run("allows the limit, then rejects with 429", func(t *testing.T) {
		rl, now := newTestLimiter(3, time.Minute)
		h := rl.Middleware(ok)

		for i := range 3 {
			rr := send(h, "198.51.100.1")
			require.Equal(t, http.StatusOK, rr.Code, "request %d should pass", i+1)
			assert.Equal(t, "3", rr.Header().Get("X-RateLimit-Limit"))
			assert.Equal(t, strconv.Itoa(2-i), rr.Header().Get("X-RateLimit-Remaining"))
			assert.Equal(t, strconv.FormatInt(now.Add(time.Minute).Unix(), 10), rr.Header().Get("X-RateLimit-Reset"))
		}

		rr := send(h, "198.51.100.1")
		assert.Equal(t, http.StatusTooManyRequests, rr.Code)
		assert.Equal(t, "0", rr.Header().Get("X-RateLimit-Remaining"))
		assert.Equal(t, "60", rr.Header().Get("Retry-After"))
		assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		var body map[string]string
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
		assert.Equal(t, "Too many requests. Please try again later.", body["error"])
	})

	t.Run("each client has its own budget", func(t *testing.T) {
		rl, _ := newTestLimiter(1, time.Minute)
		h := rl.Middleware(ok)

		assert.Equal(t, http.StatusOK, send(h, "198.51.100.1").Code)
		assert.Equal(t, http.StatusTooManyRequests, send(h, "198.51.100.1").Code)
		assert.Equal(t, http.StatusOK, send(h, "198.51.100.2").Code)
	})

	t.Run("budget comes back when the window ends", func(t *testing.T) {
		rl, now := newTestLimiter(1, time.Minute)
		h := rl.Middleware(ok)

		assert.Equal(t, http.StatusOK, send(h, "198.51.100.1").Code)
		*now = now.Add(30 * time.Second)
		rr := send(h, "198.51.100.1")
		assert.Equal(t, http.StatusTooManyRequests, rr.Code)
		assert.Equal(t, "30", rr.Header().Get("Retry-After"))

		*now = now.Add(30 * time.Second)
		assert.Equal(t, http.StatusOK, send(h, "198.51.100.1").Code)
	})

	t.Run("finished windows are forgotten so memory doesn't grow", func(t *testing.T) {
		rl, now := newTestLimiter(5, time.Minute)
		h := rl.Middleware(ok)

		for i := range 100 {
			send(h, "198.51.100."+strconv.Itoa(i))
		}
		require.Len(t, rl.clients, 100)

		*now = now.Add(2 * time.Minute)
		send(h, "203.0.113.1")
		assert.Len(t, rl.clients, 1)
	})
}
