// Package handler contains HTTP request handlers for the playground application.
//
// HANDLER RESPONSIBILITIES (UPDATED):
// Now that we have a service layer, handlers are even simpler:
// 1. Parse the incoming HTTP request (JSON body, URL params, query params)
// 2. Call the appropriate service method with the parsed data
// 3. Map the result (or error) to an HTTP response
//
// Handlers should NOT contain:
// - Business logic (that's in the service layer)
// - Database queries (that's in the repository layer)
// - Validation rules (that's in the service layer)
//
// REQUEST TYPES:
// We use dedicated request structs (CreateSnippetRequest, UpdateSnippetRequest)
// instead of decoding directly into model.Snippet. Why?
//
//  1. DECOUPLING: The API request shape can differ from the database model.
//     Example: the request has {name, code} but the model also has {id, createdAt, updatedAt}.
//     Clients shouldn't send (or even know about) auto-generated fields.
//
//  2. SECURITY: If we decode into model.Snippet, a malicious client could send
//     {"id": "someone-elses-id"} and overwrite data they don't own.
//     With a request struct, we control exactly which fields the client can set.
//
// 3. EVOLUTION: We can change the API format without changing the model (or vice versa).
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/sakif/coding-playground/internal/auth"
	"github.com/sakif/coding-playground/internal/service"
)

// SnippetHandler manages HTTP endpoints for code snippets.
// It delegates all business logic to the SnippetService.
type SnippetHandler struct {
	service *service.SnippetService
	logger  *slog.Logger
}

// NewSnippetHandler creates a new SnippetHandler.
//
// DEPENDENCY INJECTION:
// The handler receives the service as a parameter (not creating it internally).
// The full dependency chain is wired in main.go / server.go:
//
//	DB → Repository → Service → Handler
//
// Each layer only knows about the one directly below it.
func NewSnippetHandler(svc *service.SnippetService, logger *slog.Logger) *SnippetHandler {
	return &SnippetHandler{
		service: svc,
		logger:  logger,
	}
}

// --- Request Types ---
// These define the shape of JSON that clients send.
// They are distinct from model.Snippet to control exactly what's accepted.

// CreateSnippetRequest is the expected JSON body for creating a snippet.
type CreateSnippetRequest struct {
	Name        string `json:"name"`
	Code        string `json:"code"`
	Description string `json:"description"`
}

// UpdateSnippetRequest is the expected JSON body for updating a snippet.
type UpdateSnippetRequest struct {
	Name        string `json:"name"`
	Code        string `json:"code"`
	Description string `json:"description"`
}

// HandleList returns all saved snippets.
//
// HTTP: GET /api/snippets
// Query params: ?limit=20&offset=0
//
// QUERY PARAMETER PARSING:
// r.URL.Query().Get("param") returns the parameter as a string (or "" if absent).
// We use strconv.Atoi to convert to int, with defaults for missing/invalid values.
// This is the standard way to handle optional query parameters in Go.
func (h *SnippetHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	// Parse optional query parameters for pagination
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	// Delegate to the service (it handles defaults and clamping)
	snippets, err := h.service.List(r.Context(), limit, offset)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, snippets)
}

// HandleGetByID retrieves a single snippet by its ID.
//
// HTTP: GET /api/snippets/{id}
//
// URL PARAMETERS:
// Chi extracts named URL parameters from the path pattern.
// For the route pattern "/api/snippets/{id}", requesting /api/snippets/abc123
// makes r.PathValue("id") return "abc123".
func (h *SnippetHandler) HandleGetByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	snippet, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, snippet)
}

// HandleCreate saves a new snippet.
//
// HTTP: POST /api/snippets
// Request body: {"name": "my snippet", "code": "print('hello')"}
//
// REQUEST PARSING FLOW:
// 1. json.NewDecoder(r.Body) creates a streaming JSON decoder
// 2. .Decode(&req) reads the body and fills the struct fields
// 3. If the JSON is malformed, Decode returns an error → 400 Bad Request
// 4. We pass the parsed fields (not the struct) to the service
// 5. The service validates and creates → returns the snippet with ID
// 6. We send back the created snippet as JSON with 201 Created
//
// r.Context():
// We pass the request's context to the service. This context carries:
// - Request deadline/timeout (if set by middleware)
// - Request ID (from Chi's RequestID middleware)
// - Cancellation signal (if the client disconnects)
// The context flows: handler → service → repository → database
// If the client disconnects, the entire chain cancels automatically.
func (h *SnippetHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var req CreateSnippetRequest

	// Parse JSON body
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid snippet JSON",
			slog.String("error", err.Error()),
		)
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_json",
			Message: "Request body must be valid JSON",
		})
		return
	}

	// Extract the authenticated user's ID from context (set by OptionalAuth middleware).
	// userID is "" for anonymous requests — the service treats that as unowned.
	userID, _ := auth.UserIDFromContext(r.Context())

	// Delegate to service (handles validation, ID generation, persistence)
	snippet, err := h.service.Create(r.Context(), req.Name, req.Code, req.Description, userID)
	if err != nil {
		writeError(w, err)
		return
	}

	// 201 Created — the standard status code for successful resource creation
	writeJSON(w, http.StatusCreated, snippet)
}

// HandleUpdate modifies an existing snippet.
//
// HTTP: PUT /api/snippets/{id}
// Request body: {"name": "new name", "code": "new code"}
//
// PUT vs PATCH:
// - PUT: replace the entire resource (all fields required)
// - PATCH: partially update (only provided fields change)
// We use PUT semantics here for simplicity. In a production API,
// you might offer both.
func (h *SnippetHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req UpdateSnippetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid snippet JSON",
			slog.String("error", err.Error()),
			slog.String("id", id),
		)
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_json",
			Message: "Request body must be valid JSON",
		})
		return
	}

	userID, _ := auth.UserIDFromContext(r.Context())

	snippet, err := h.service.Update(r.Context(), id, req.Name, req.Code, req.Description, userID)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, snippet)
}

// HandleDelete removes a saved snippet.
//
// HTTP: DELETE /api/snippets/{id}
//
// 204 No Content:
// The standard response for successful deletion. It means:
// "The operation succeeded, and there's nothing to send back."
// We don't return the deleted snippet (it's gone!) — just the status code.
func (h *SnippetHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	userID, _ := auth.UserIDFromContext(r.Context())

	if err := h.service.Delete(r.Context(), id, userID); err != nil {
		writeError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent) // 204 — success, no body
}
