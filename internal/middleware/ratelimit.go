package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimiter allows each client (by IP) at most `limit` requests per `window`.
//
// FIXED WINDOW:
// Each client gets a counter that resets every window (e.g. 10 requests per minute).
// It is the simplest strategy and makes the X-RateLimit-* headers exact.
// The trade-off: a client can send `limit` requests at the end of one window and
// `limit` more at the start of the next. For a playground that's acceptable.
//
// The limiter keys on r.RemoteAddr, so ClientIP must run before it.
type RateLimiter struct {
	limit  int
	window time.Duration
	now    func() time.Time // replaceable in tests

	mu        sync.Mutex
	clients   map[string]*rateWindow
	lastSweep time.Time
}

type rateWindow struct {
	start time.Time
	count int
}

// NewRateLimiter creates a limiter allowing `limit` requests per `window` per client.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		limit:   limit,
		window:  window,
		now:     time.Now,
		clients: make(map[string]*rateWindow),
	}
}

// Middleware enforces the limit and sets the X-RateLimit-* headers on every response.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed, remaining, reset := rl.take(r.RemoteAddr)

		h := w.Header()
		h.Set("X-RateLimit-Limit", strconv.Itoa(rl.limit))
		h.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		h.Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))

		if !allowed {
			retryAfter := int(reset.Sub(rl.now()).Round(time.Second).Seconds())
			h.Set("Retry-After", strconv.Itoa(max(retryAfter, 1)))
			h.Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			if err := json.NewEncoder(w).Encode(map[string]string{
				"error": "Too many requests. Please try again later.",
			}); err != nil {
				slog.Error("failed to encode rate limit response", slog.String("error", err.Error()))
			}
			return
		}

		next.ServeHTTP(w, r)
	})
}

// take counts one request for the client and reports whether it is allowed,
// how many requests are left, and when the client's window resets.
func (rl *RateLimiter) take(client string) (allowed bool, remaining int, reset time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	rl.sweep(now)

	win, ok := rl.clients[client]
	if !ok || !now.Before(win.start.Add(rl.window)) {
		win = &rateWindow{start: now}
		rl.clients[client] = win
	}

	reset = win.start.Add(rl.window)
	if win.count >= rl.limit {
		return false, 0, reset
	}
	win.count++
	return true, rl.limit - win.count, reset
}

// sweep forgets clients whose window has ended, at most once per window,
// so the map only holds clients seen recently.
func (rl *RateLimiter) sweep(now time.Time) {
	if now.Sub(rl.lastSweep) < rl.window {
		return
	}
	for client, win := range rl.clients {
		if !now.Before(win.start.Add(rl.window)) {
			delete(rl.clients, client)
		}
	}
	rl.lastSweep = now
}
