package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/sakif/coding-playground/internal/executor"
)

// Executor implements the executor.Executor interface using Docker.
type Executor struct {
	cli    *client.Client
	config Config
	logger *slog.Logger
	pool   *Pool
}

// New creates a new Docker Executor and initializes the connection.
func New(cfg Config, logger *slog.Logger) (*Executor, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	// Make sure the image is pulled
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	logger.Info("ensuring docker image is available", slog.String("image", cfg.Image))
	reader, err := cli.ImagePull(ctx, cfg.Image, image.PullOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to pull image: %w", err)
	}
	defer reader.Close()
	// Read everything to block until the pull is complete
	io.Copy(io.Discard, reader)
	logger.Info("docker image is ready")

	exec := &Executor{
		cli:    cli,
		config: cfg,
		logger: logger,
	}

	exec.pool = NewPool(cli, cfg, logger)
	exec.pool.Start()

	return exec, nil
}

// Close shuts down the executor pool and docker client.
func (e *Executor) Close() error {
	e.pool.Stop()
	return e.cli.Close()
}

// Execute runs the provided Python code in a sandboxed Docker container.
func (e *Executor) Execute(ctx context.Context, req executor.ExecutionRequest) (*executor.ExecutionResult, error) {
	start := time.Now()

	// Get a pre-warmed container ID from the pool
	containerID, err := e.pool.GetContainer(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get container from pool: %w", err)
	}

	// Always ensure we clean up the container that we acquired
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := e.cli.ContainerRemove(cleanupCtx, containerID, container.RemoveOptions{
			Force: true,
		})
		if err != nil {
			e.logger.Error("failed to remove container", slog.String("id", containerID), slog.String("error", err.Error()))
		}
	}()

	// We apply a timeout context purely for the container wait
	executeCtx, executeCancel := context.WithTimeout(ctx, e.config.Timeout)
	defer executeCancel()

	// Copy the code into the container (using `python -c`) or by running `docker exec`.
	// Since we already started it with `sleep 3600`, we can `docker exec` the code.
	execConfig := container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          []string{"python", "-c", req.Code},
	}

	execResp, err := e.cli.ContainerExecCreate(executeCtx, containerID, execConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %w", err)
	}

	attachResp, err := e.cli.ContainerExecAttach(executeCtx, execResp.ID, container.ExecStartOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer attachResp.Close()

	var stdout, stderr bytes.Buffer

	// Channels to manage sync and timeout
	done := make(chan struct{})
	go func() {
		// Use stdcopy to demultiplex stdout from stderr
		_, _ = stdcopy.StdCopy(&stdout, &stderr, attachResp.Reader)
		close(done)
	}()

	var finalExitCode int

	select {
	case <-done:
		// Completed normally
		inspectResp, err := e.cli.ContainerExecInspect(ctx, execResp.ID)
		if err == nil {
			finalExitCode = inspectResp.ExitCode
		}
	case <-executeCtx.Done():
		// Timeout reached
		finalExitCode = 124 // Custom exit code for timeout (similar to unix timeout command)
		stderr.WriteString("\nExecution timed out.\n")
	}

	return &executor.ExecutionResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: finalExitCode,
		Duration: time.Since(start),
	}, nil
}
