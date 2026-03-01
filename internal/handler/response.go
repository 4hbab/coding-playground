package handler

// RESPONSE HELPERS:
// These functions standardise how we send JSON responses and errors.
//
// WHY HELPERS?
// Without helpers, every handler repeats the same boilerplate:
//   w.Header().Set("Content-Type", "application/json")
//   w.WriteHeader(statusCode)
//   json.NewEncoder(w).Encode(data)
//
// With helpers, handlers are cleaner and more consistent:
//   writeJSON(w, http.StatusOK, data)
//   writeError(w, err)
//
// CONSISTENT ERROR FORMAT:
// Every error response from our API has the same shape:
//   {"error": "not_found", "message": "snippet not found with id abc123"}
//
// This makes it easy for the frontend to parse errors — it always knows
// what fields to expect, regardless of whether it's a 400, 404, or 500.

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/sakif/coding-playground/internal/apperror"
)

// ErrorResponse is the standard error format returned by all API endpoints.
// Having a struct ensures consistent JSON shape across all error responses.
type ErrorResponse struct {
	Error   string `json:"error"`   // Machine-readable error type (e.g., "not_found")
	Message string `json:"message"` // Human-readable description
}

// writeJSON sends a JSON response with the given status code.
//
// HEADER ORDER MATTERS:
// You MUST set headers and status code BEFORE writing the body.
// Once you call w.Write() (which Encode does internally), the headers are sent.
// Any header changes after that are silently ignored.
//
// That's why we do:
//  1. w.Header().Set(...)     ← set headers
//  2. w.WriteHeader(status)   ← send status + headers
//  3. json.Encode(data)       ← send body
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			// If encoding fails, the headers are already sent — we can only log it.
			// This is rare (usually means the data has an unencodable type like a channel).
			slog.Error("failed to encode JSON response", slog.String("error", err.Error()))
		}
	}
}

// writeError maps a domain error to the appropriate HTTP status code and sends it.
//
// ERROR MAPPING:
// This is where domain errors (from the service layer) get translated to HTTP.
// The service layer returns apperror.ErrValidation, apperror.ErrNotFound, etc.
// This function maps those to 400, 404, etc.
//
// WHY HERE AND NOT IN THE SERVICE?
// The service layer should not know about HTTP status codes.
// Different consumers of the service might use different protocols:
// - HTTP handler: maps ErrNotFound → 404
// - gRPC handler: maps ErrNotFound → codes.NotFound
// - CLI tool: maps ErrNotFound → "Item not found" message
//
// errors.Is() UNWRAPPING:
// errors.Is(err, target) walks the entire error chain (via Unwrap())
// to see if `target` appears anywhere. This works because:
//
//	service returns: fmt.Errorf("creating snippet: %w", apperror.ValidationFailed(...))
//	which wraps:     AppError{Err: ErrValidation, Message: "..."}
//	errors.Is walks: outer error → AppError → ErrValidation ✓ match!
func writeError(w http.ResponseWriter, err error) {
	// Try to extract our AppError for the human-readable message
	var appErr *apperror.AppError

	// errors.As() is like errors.Is() but extracts the error value.
	// It walks the chain and fills appErr if it finds an *AppError.
	if errors.As(err, &appErr) {
		// We have a typed application error — map it to HTTP
		status := http.StatusInternalServerError
		errorType := "internal_error"

		switch {
		case errors.Is(err, apperror.ErrValidation):
			status = http.StatusBadRequest // 400
			errorType = "validation_error"
		case errors.Is(err, apperror.ErrNotFound):
			status = http.StatusNotFound // 404
			errorType = "not_found"
		case errors.Is(err, apperror.ErrForbidden):
			status = http.StatusForbidden // 403
			errorType = "forbidden"
		case errors.Is(err, apperror.ErrConflict):
			status = http.StatusConflict // 409
			errorType = "conflict"
		}

		writeJSON(w, status, ErrorResponse{
			Error:   errorType,
			Message: appErr.Message,
		})
		return
	}

	// Unknown error — return a generic 500
	// NEVER expose internal error details to the client in production!
	// The raw error message might contain SQL queries, file paths, or other sensitive info.
	writeJSON(w, http.StatusInternalServerError, ErrorResponse{
		Error:   "internal_error",
		Message: "An internal error occurred",
	})
}
