package docker

// Benchmarks behind the pool warm-up numbers in the README. They need Docker:
//
//	go test -run '^$' -bench . -benchtime 20x ./internal/executor/docker/
//
// Both include everything a request waits for, including removing the used container.

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/sakif/coding-playground/internal/executor"
)

var benchReq = executor.ExecutionRequest{Code: `print("hi")`}

func newBenchExecutor(b *testing.B, cfg Config) *Executor {
	b.Helper()
	e, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		b.Skipf("Docker unavailable: %v", err)
	}
	b.Cleanup(func() { _ = e.Close() })
	return e
}

// BenchmarkColdStart creates and starts a fresh container for every run,
// which is what each request would pay without the pool.
func BenchmarkColdStart(b *testing.B) {
	cfg := DefaultConfig()
	cfg.PoolSize = 1
	e := newBenchExecutor(b, cfg)

	// Stop the pool manager so it doesn't create containers in the background;
	// the benchmark creates them itself.
	e.pool.Stop()
	e.pool = NewPool(e.cli, cfg, e.logger)

	for b.Loop() {
		id, err := e.pool.createContainer()
		if err != nil {
			b.Fatal(err)
		}
		e.pool.containers <- id
		if _, err := e.Execute(context.Background(), benchReq); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWarmPool runs code in an already-started container, as requests do
// when the pool is full. The pool refills between runs, outside the timing.
func BenchmarkWarmPool(b *testing.B) {
	cfg := DefaultConfig()
	e := newBenchExecutor(b, cfg)

	waitForFullPool := func() {
		deadline := time.Now().Add(30 * time.Second)
		for len(e.pool.containers) < cap(e.pool.containers) {
			if time.Now().After(deadline) {
				b.Fatal("pool did not refill")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	for b.Loop() {
		b.StopTimer()
		waitForFullPool()
		b.StartTimer()
		if _, err := e.Execute(context.Background(), benchReq); err != nil {
			b.Fatal(err)
		}
	}
}
