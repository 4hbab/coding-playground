package executor

import (
	"context"
	"errors"
	"time"
)

// ErrBusy means every sandbox is in use and none became free in time.
// It is temporary: the client should retry shortly.
var ErrBusy = errors.New("all sandboxes are busy")

// ExecutionRequest represents a request to execute Python code.
type ExecutionRequest struct {
	Code string `json:"code"`
}

// ExecutionResult represents the output and status of the code execution.
type ExecutionResult struct {
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
	ExitCode int           `json:"exitCode"`
	Duration time.Duration `json:"duration"`
	// Truncated is true when stdout or stderr went over the output limit and was cut.
	Truncated bool `json:"truncated"`
}

// Executor represents the core interface for running code in an isolated environment.
type Executor interface {
	Execute(ctx context.Context, req ExecutionRequest) (*ExecutionResult, error)
}
