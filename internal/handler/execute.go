package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/sakif/coding-playground/internal/executor"
)

// MaxExecuteRequestBytes caps the size of an execution request body (64 KB).
// Larger bodies are rejected before they are read into memory or reach Docker.
// The code is passed to the container as a command-line argument, so it also
// keeps well below the kernel's per-argument size limit.
const MaxExecuteRequestBytes = 64 * 1024

// ExecuteHandler handles code execution requests.
type ExecuteHandler struct {
	exec   executor.Executor
	logger *slog.Logger
}

// NewExecuteHandler creates a new ExecuteHandler.
func NewExecuteHandler(exec executor.Executor, logger *slog.Logger) *ExecuteHandler {
	return &ExecuteHandler{
		exec:   exec,
		logger: logger,
	}
}

// HandleExecute processes an incoming Python code execution request.
func (h *ExecuteHandler) HandleExecute(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxExecuteRequestBytes)

	var req executor.ExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "code is too large", http.StatusRequestEntityTooLarge)
			return
		}
		h.logger.Warn("invalid execution request body", slog.String("error", err.Error()))
		http.Error(w, "invalid request configuration", http.StatusBadRequest)
		return
	}

	if req.Code == "" {
		http.Error(w, "code cannot be empty", http.StatusBadRequest)
		return
	}

	h.logger.Info("executing python code snippet")

	result, err := h.exec.Execute(r.Context(), req)
	if errors.Is(err, executor.ErrBusy) {
		h.logger.Warn("all sandboxes busy")
		w.Header().Set("Retry-After", "5")
		http.Error(w, "all sandboxes are busy, try again in a few seconds", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		h.logger.Error("code execution failed", slog.String("error", err.Error()))
		http.Error(w, "internal server error during execution", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		h.logger.Error("failed to encode execution result", slog.String("error", err.Error()))
	}
}
