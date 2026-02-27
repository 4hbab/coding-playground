package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/sakif/coding-playground/internal/executor"
)

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
	var req executor.ExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
