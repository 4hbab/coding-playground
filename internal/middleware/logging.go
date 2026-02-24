// Package middleware contains HTTP middleware functions.
//
// WHAT IS MIDDLEWARE?
// Middleware is a function that wraps an HTTP handler to add cross-cutting behaviour
// (logging, auth, CORS, etc.) without modifying the handler itself.
//
// The pattern is:
//
//	func MyMiddleware(next http.Handler) http.Handler {
//	    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//	        // Do something BEFORE the handler runs
//	        next.ServeHTTP(w, r)  // Call the actual handler
//	        // Do something AFTER the handler runs
//	    })
//	}
//
// This is the "decorator pattern" — we wrap the real handler with extra behaviour.
package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// responseWriter wraps http.ResponseWriter to capture the status code.
// Go's http.ResponseWriter doesn't expose the status code after WriteHeader is called,
// so we wrap it to track it ourselves. This is a common Go pattern.
type responseWriter struct {
	http.ResponseWriter       // Embedding: this struct "inherits" all methods
	statusCode          int   // Our addition: track the status code
	written             int64 // Track bytes written
}

// WriteHeader captures the status code before delegating to the embedded ResponseWriter.
// By defining this method, we "override" the embedded ResponseWriter's WriteHeader.
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Write captures bytes written and delegates to the embedded ResponseWriter.
func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.written += int64(n)
	return n, err
}

// Logger returns an HTTP middleware that logs each request using Go's slog package.
//
// slog (structured logging) was added in Go 1.21. It produces structured log output
// that's easy to parse and search, unlike fmt.Println.
//
// Each log line includes: method, path, status code, duration, and bytes written.
func Logger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Record when the request started
			start := time.Now()

			// Wrap the ResponseWriter so we can capture status code and bytes
			wrapped := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK, // Default if WriteHeader is never called
			}

			// Call the next handler in the chain
			next.ServeHTTP(wrapped, r)

			// Log the completed request with structured fields
			logger.Info("request completed",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", wrapped.statusCode),
				slog.Duration("duration", time.Since(start)),
				slog.Int64("bytes", wrapped.written),
			)
		})
	}
}
