package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sakif/coding-playground/internal/executor"
	"github.com/sakif/coding-playground/internal/handler"
	"github.com/stretchr/testify/assert"
)

// MockExecutor implements a fast, mock executor for handler testing without Docker overhead.
type MockExecutor struct {
	CapturedReq executor.ExecutionRequest
	ReturnRes   *executor.ExecutionResult
	ReturnErr   error
}

func (m *MockExecutor) Execute(ctx context.Context, req executor.ExecutionRequest) (*executor.ExecutionResult, error) {
	m.CapturedReq = req
	if m.ReturnErr != nil {
		return nil, m.ReturnErr
	}
	return m.ReturnRes, nil
}

func TestExecuteHandler_HandleExecute(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("valid execution", func(t *testing.T) {
		mockExec := &MockExecutor{
			ReturnRes: &executor.ExecutionResult{
				Stdout:   "Hello World\n",
				Stderr:   "",
				ExitCode: 0,
				Duration: 100 * time.Millisecond,
			},
		}

		h := handler.NewExecuteHandler(mockExec, logger)

		reqBody := `{"code":"print('Hello World')"}`
		req := httptest.NewRequest(http.MethodPost, "/api/execute", bytes.NewBufferString(reqBody))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		h.HandleExecute(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var res executor.ExecutionResult
		err := json.NewDecoder(rr.Body).Decode(&res)
		assert.NoError(t, err)
		assert.Equal(t, "Hello World\n", res.Stdout)
		assert.Equal(t, 0, res.ExitCode)

		assert.Equal(t, "print('Hello World')", mockExec.CapturedReq.Code)
	})

	t.Run("invalid request body", func(t *testing.T) {
		mockExec := &MockExecutor{}
		h := handler.NewExecuteHandler(mockExec, logger)

		reqBody := `{"invalid_json":`
		req := httptest.NewRequest(http.MethodPost, "/api/execute", bytes.NewBufferString(reqBody))
		rr := httptest.NewRecorder()

		h.HandleExecute(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("request body over the size limit", func(t *testing.T) {
		mockExec := &MockExecutor{}
		h := handler.NewExecuteHandler(mockExec, logger)

		code := strings.Repeat("x", handler.MaxExecuteRequestBytes+1)
		reqBody := `{"code":"` + code + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/execute", bytes.NewBufferString(reqBody))
		rr := httptest.NewRecorder()

		h.HandleExecute(rr, req)

		assert.Equal(t, http.StatusRequestEntityTooLarge, rr.Code)
		assert.Empty(t, mockExec.CapturedReq.Code, "oversized code must never reach the executor")
	})

	t.Run("all sandboxes busy", func(t *testing.T) {
		mockExec := &MockExecutor{ReturnErr: fmt.Errorf("docker: %w", executor.ErrBusy)}
		h := handler.NewExecuteHandler(mockExec, logger)

		req := httptest.NewRequest(http.MethodPost, "/api/execute", bytes.NewBufferString(`{"code":"print(1)"}`))
		rr := httptest.NewRecorder()

		h.HandleExecute(rr, req)

		assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
		assert.NotEmpty(t, rr.Header().Get("Retry-After"), "clients need to know when to try again")
	})

	t.Run("empty code", func(t *testing.T) {
		mockExec := &MockExecutor{}
		h := handler.NewExecuteHandler(mockExec, logger)

		reqBody := `{"code":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/execute", bytes.NewBufferString(reqBody))
		rr := httptest.NewRecorder()

		h.HandleExecute(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})
}
