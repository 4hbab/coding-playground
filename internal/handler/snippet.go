package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/sakif/coding-playground/internal/model"
)

// SnippetHandler manages CRUD operations for code snippets.
//
// WHY A SEPARATE HANDLER?
// Separating snippet logic from playground logic follows the Single Responsibility Principle.
// Each handler struct "owns" one area of functionality. This makes code easier to:
// - Test (mock dependencies independently)
// - Understand (find all snippet logic in one place)
// - Modify (change snippet storage without touching playground rendering)
//
// NOTE: Currently snippets are stored in browser localStorage (client-side).
// These API endpoints exist as a foundation — when we add server-side SQLite storage,
// we only need to modify this file. The frontend code won't change.
type SnippetHandler struct {
	logger *slog.Logger
}

// NewSnippetHandler creates a new SnippetHandler.
func NewSnippetHandler(logger *slog.Logger) *SnippetHandler {
	return &SnippetHandler{logger: logger}
}

// HandleList returns all saved snippets.
//
// HTTP: GET /api/snippets
//
// RESPONSE FORMAT:
//
//	[
//	  {"id":"abc","name":"hello","code":"print('hi')","createdAt":"...","updatedAt":"..."},
//	  ...
//	]
//
// For now, this returns an empty list since storage is client-side.
// When we add SQLite, this will query the database.
func (h *SnippetHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	// Return an empty list for now — snippets are in localStorage
	snippets := []model.Snippet{}

	// Set JSON content type
	w.Header().Set("Content-Type", "application/json")

	// json.NewEncoder writes JSON directly to the ResponseWriter (streaming)
	// This is more efficient than json.Marshal + w.Write for large responses
	if err := json.NewEncoder(w).Encode(snippets); err != nil {
		h.logger.Error("failed to encode snippets", slog.String("error", err.Error()))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// HandleCreate saves a new snippet.
//
// HTTP: POST /api/snippets
// REQUEST BODY: {"name": "my snippet", "code": "print('hello')"}
//
// JSON DECODING:
// json.NewDecoder(r.Body) reads the request body as a stream and decodes it into a struct.
// We use Decode() instead of json.Unmarshal() because:
// 1. Streaming: doesn't need to buffer the entire body in memory
// 2. Error detection: can enforce single JSON value with DisallowUnknownFields()
func (h *SnippetHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var snippet model.Snippet

	// Decode JSON request body into our Snippet struct
	if err := json.NewDecoder(r.Body).Decode(&snippet); err != nil {
		h.logger.Warn("invalid snippet JSON", slog.String("error", err.Error()))
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	// Set timestamps
	now := time.Now()
	snippet.CreatedAt = now
	snippet.UpdatedAt = now

	// For now, just echo back the snippet — actual persistence is client-side
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated) // 201 Created

	if err := json.NewEncoder(w).Encode(snippet); err != nil {
		h.logger.Error("failed to encode snippet", slog.String("error", err.Error()))
	}
}

// HandleDelete removes a saved snippet.
//
// HTTP: DELETE /api/snippets/{id}
//
// URL PARAMETERS:
// Chi provides r.PathValue("id") to extract URL parameters.
// For a request to DELETE /api/snippets/abc123, PathValue("id") returns "abc123".
func (h *SnippetHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "Snippet ID is required", http.StatusBadRequest)
		return
	}

	h.logger.Info("snippet delete requested", slog.String("id", id))

	// For now, just acknowledge — actual deletion is client-side
	w.WriteHeader(http.StatusNoContent) // 204 No Content — successful deletion, no body
}
