package docker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/sakif/coding-playground/internal/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An executor whose pool never fills: no Docker client and no pool manager.
// Execute only touches Docker after it gets a container, so this is safe.
func newEmptyPoolExecutor(cfg Config) *Executor {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Executor{config: cfg, logger: logger, pool: NewPool(nil, cfg, logger)}
}

func TestExecute_PoolBusy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AcquireTimeout = 50 * time.Millisecond

	t.Run("gives up with ErrBusy when no container frees up in time", func(t *testing.T) {
		e := newEmptyPoolExecutor(cfg)

		start := time.Now()
		_, err := e.Execute(context.Background(), executor.ExecutionRequest{Code: `print(1)`})

		require.ErrorIs(t, err, executor.ErrBusy)
		assert.Less(t, time.Since(start), time.Second)
	})

	t.Run("a cancelled request is not reported as busy", func(t *testing.T) {
		e := newEmptyPoolExecutor(cfg)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // the client went away before a container was free

		_, err := e.Execute(ctx, executor.ExecutionRequest{Code: `print(1)`})

		require.Error(t, err)
		assert.False(t, errors.Is(err, executor.ErrBusy))
		assert.ErrorIs(t, err, context.Canceled)
	})
}
