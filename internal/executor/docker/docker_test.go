package docker_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"log/slog"
	"os"

	"github.com/sakif/coding-playground/internal/executor"
	"github.com/sakif/coding-playground/internal/executor/docker"
	"github.com/stretchr/testify/assert"
)

func TestDockerExecutor(t *testing.T) {
	// Skip in CI environments if docker is not available
	if os.Getenv("CI") != "" {
		t.Skip("Skipping docker test in CI environment")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := docker.DefaultConfig()
	// reduce pool size for local test speed
	cfg.PoolSize = 1

	exec, err := docker.New(cfg, logger)
	assert.NoError(t, err, "Should initialize docker executor without error")
	defer exec.Close()

	// Wait a moment for the pool manager to start and warm up containers
	time.Sleep(2 * time.Second)

	t.Run("successful execution", func(t *testing.T) {
		req := executor.ExecutionRequest{
			Code: `print("Hello from test sandbox!")`,
		}

		res, err := exec.Execute(context.Background(), req)
		assert.NoError(t, err)
		assert.Equal(t, 0, res.ExitCode)
		assert.Contains(t, res.Stdout, "Hello from test sandbox!")
		assert.Empty(t, res.Stderr)
		assert.Greater(t, res.Duration, time.Duration(0))
	})

	t.Run("syntax error", func(t *testing.T) {
		req := executor.ExecutionRequest{
			Code: `print("Missing parenthesis"`,
		}

		res, err := exec.Execute(context.Background(), req)
		assert.NoError(t, err)
		assert.NotEqual(t, 0, res.ExitCode)
		assert.Contains(t, res.Stderr, "SyntaxError")
		assert.Empty(t, res.Stdout)
	})

	t.Run("infinite loop timeout", func(t *testing.T) {
		// Override timeout for this test to be fast
		cfg.Timeout = 2 * time.Second
		fastExec, err := docker.New(cfg, logger)
		assert.NoError(t, err)
		defer fastExec.Close()
		time.Sleep(1 * time.Second) // Wait for pool

		req := executor.ExecutionRequest{
			Code: `while True: pass`,
		}

		res, err := fastExec.Execute(context.Background(), req)
		assert.NoError(t, err)
		assert.Equal(t, 124, res.ExitCode) // Our custom timeout format
		assert.Contains(t, res.Stderr, "timed out")
	})

	t.Run("multiline logic", func(t *testing.T) {
		req := executor.ExecutionRequest{
			Code: strings.Join([]string{
				"def fib(n):",
				"    if n <= 1: return n",
				"    return fib(n-1) + fib(n-2)",
				"print(fib(5))",
			}, "\n"),
		}

		res, err := exec.Execute(context.Background(), req)
		assert.NoError(t, err)
		assert.Equal(t, 0, res.ExitCode)
		assert.Contains(t, res.Stdout, "5")
	})
}
