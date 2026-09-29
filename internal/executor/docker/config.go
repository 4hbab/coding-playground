package docker

import (
	"time"
)

// Config holds the configuration for Docker execution.
type Config struct {
	// Image is the Docker image to use for execution.
	Image string
	// MemoryLimit is the maximum amount of memory the container can use (in bytes).
	// Swap is disabled, so this is a hard cap.
	MemoryLimit int64
	// CPULimit is the number of CPUs the container can use.
	CPULimit float64
	// Timeout is the maximum amount of time the execution can take.
	Timeout time.Duration
	// PoolSize is the number of pre-warmed containers to maintain.
	PoolSize int
	// AcquireTimeout is how long a request waits for a free container before
	// giving up with executor.ErrBusy. Timeout + AcquireTimeout must stay below
	// the HTTP server's write timeout.
	AcquireTimeout time.Duration
	// PidsLimit is the maximum number of processes/threads in the container (stops fork bombs).
	PidsLimit int64
	// TmpSize is the size of the writable in-memory /tmp, in Docker's tmpfs syntax (e.g. "16m").
	TmpSize string
	// OutputLimit is the maximum number of bytes kept per stream (stdout and stderr).
	// Anything beyond it is discarded and the result is marked as truncated.
	OutputLimit int
}

// DefaultConfig provides sensible defaults for a Python sandbox.
func DefaultConfig() Config {
	return Config{
		// Use a lightweight python image
		Image: "python:3.12-alpine",
		// 128 MB memory limit
		MemoryLimit: 128 * 1024 * 1024,
		// 0.5 CPU shares
		CPULimit: 0.5,
		// 5 second default timeout
		Timeout:        5 * time.Second,
		PoolSize:       3,
		AcquireTimeout: 5 * time.Second,
		// Enough for normal programs and multiprocessing demos, far too few for a fork bomb
		PidsLimit: 64,
		// 16 MB of scratch space in /tmp (counts against MemoryLimit)
		TmpSize: "16m",
		// 64 KB per stream is plenty for a playground and keeps responses small
		OutputLimit: 64 * 1024,
	}
}
