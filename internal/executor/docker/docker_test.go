package docker_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"log/slog"
	"os"

	"github.com/docker/docker/client"
	"github.com/sakif/coding-playground/internal/executor"
	"github.com/sakif/coding-playground/internal/executor/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireDocker skips the test when no Docker daemon is reachable.
// With REQUIRE_DOCKER=1 (set in CI) a missing daemon fails the test instead,
// so the sandbox tests can never be skipped silently.
func requireDocker(t *testing.T) {
	t.Helper()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err == nil {
		defer func() { _ = cli.Close() }()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err = cli.Ping(ctx)
	}
	if err == nil {
		return
	}

	if os.Getenv("REQUIRE_DOCKER") == "1" {
		t.Fatalf("REQUIRE_DOCKER=1 but the Docker daemon is unreachable: %v", err)
	}
	t.Skipf("Docker daemon unreachable, skipping sandbox tests: %v", err)
}

// newTestExecutor starts an executor and stops it (removing its containers) when the test ends.
// There is no need to wait for the pool to warm up: Execute blocks until a container is ready.
func newTestExecutor(t *testing.T, cfg docker.Config, logger *slog.Logger) *docker.Executor {
	t.Helper()

	exec, err := docker.New(cfg, logger)
	require.NoError(t, err, "Should initialize docker executor without error")
	t.Cleanup(func() {
		assert.NoError(t, exec.Close())
	})
	return exec
}

func TestDockerExecutor(t *testing.T) {
	requireDocker(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := docker.DefaultConfig()
	// reduce pool size for local test speed
	cfg.PoolSize = 1

	exec := newTestExecutor(t, cfg, logger)

	t.Run("successful execution", func(t *testing.T) {
		req := executor.ExecutionRequest{
			Code: `print("Hello from test sandbox!")`,
		}

		res, err := exec.Execute(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, 0, res.ExitCode)
		assert.Contains(t, res.Stdout, "Hello from test sandbox!")
		assert.Empty(t, res.Stderr)
		assert.False(t, res.Truncated)
		assert.Greater(t, res.Duration, time.Duration(0))
	})

	t.Run("syntax error", func(t *testing.T) {
		req := executor.ExecutionRequest{
			Code: `print("Missing parenthesis"`,
		}

		res, err := exec.Execute(context.Background(), req)
		require.NoError(t, err)
		assert.NotEqual(t, 0, res.ExitCode)
		assert.Contains(t, res.Stderr, "SyntaxError")
		assert.Empty(t, res.Stdout)
	})

	t.Run("infinite loop timeout", func(t *testing.T) {
		// Override timeout for this test to be fast
		fastCfg := cfg
		fastCfg.Timeout = 2 * time.Second
		fastExec := newTestExecutor(t, fastCfg, logger)

		req := executor.ExecutionRequest{
			Code: `while True: pass`,
		}

		res, err := fastExec.Execute(context.Background(), req)
		require.NoError(t, err)
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
		require.NoError(t, err)
		assert.Equal(t, 0, res.ExitCode)
		assert.Contains(t, res.Stdout, "5")
	})
}
