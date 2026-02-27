package executor

import (
	"context"
	"time"
)

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
}

// Executor represents the core interface for running code in an isolated environment.
type Executor interface {
	Execute(ctx context.Context, req ExecutionRequest) (*ExecutionResult, error)
}
