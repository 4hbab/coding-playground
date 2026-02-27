package docker

import (
	"time"
)

// Config holds the configuration for Docker execution.
type Config struct {
	// Image is the Docker image to use for execution.
	Image string
	// MemoryLimit is the maximum amount of memory the container can use (in bytes).
	MemoryLimit int64
	// CPULimit is the number of CPUs the container can use.
	CPULimit float64
	// Timeout is the maximum amount of time the execution can take.
	Timeout time.Duration
	// PoolSize is the number of pre-warmed containers to maintain.
	PoolSize int
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
		Timeout:  5 * time.Second,
		PoolSize: 3,
	}
}
