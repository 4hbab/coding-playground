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

// exitCodeKilled is what a process reports when it receives SIGKILL (128 + 9).
// Timeouts are reported as 124 and are handled separately, so inside the sandbox
// SIGKILL almost always means the kernel's out-of-memory killer stopped the program
// (the only other way is the program killing itself).
const exitCodeKilled = 137

// cappedBuffer keeps at most limit bytes and silently drops the rest.
// It always reports the full write as successful so the output stream keeps
// draining; otherwise a program printing in a loop would block instead of finishing.
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := b.limit - b.buf.Len(); n > remaining {
		b.truncated = true
		p = p[:max(remaining, 0)]
	}
	b.buf.Write(p)
	return n, nil
}

func (b *cappedBuffer) String() string {
	return b.buf.String()
}

// note appends a message from the sandbox itself (such as "timed out"). It is not
// subject to the limit, so the user always sees why their program was stopped.
func (b *cappedBuffer) note(msg string) {
	b.buf.WriteString(msg)
}

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
	defer func() { _ = reader.Close() }()
	// Read everything to block until the pull is complete
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return nil, fmt.Errorf("failed to pull image: %w", err)
	}
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

	// Get a pre-warmed container ID from the pool, waiting at most AcquireTimeout
	acquireCtx, acquireCancel := context.WithTimeout(ctx, e.config.AcquireTimeout)
	containerID, err := e.pool.GetContainer(acquireCtx)
	acquireCancel()
	if err != nil {
		if ctx.Err() == nil {
			// Our own wait ran out, not the caller's context: every sandbox is in use.
			return nil, executor.ErrBusy
		}
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

	// The pooled container is already running `sleep infinity`, so we `docker exec`
	// the code into it with `python -c`.
	// -u makes output unbuffered: Python buffers stdout when it isn't a terminal, so
	// anything printed before a timeout or out-of-memory kill would otherwise be lost.
	execConfig := container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          []string{"python", "-u", "-c", req.Code},
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

	stdout := &cappedBuffer{limit: e.config.OutputLimit}
	stderr := &cappedBuffer{limit: e.config.OutputLimit}

	// Channels to manage sync and timeout
	done := make(chan struct{})
	go func() {
		// Use stdcopy to demultiplex stdout from stderr
		_, _ = stdcopy.StdCopy(stdout, stderr, attachResp.Reader)
		close(done)
	}()

	var finalExitCode int

	select {
	case <-done:
		// Completed normally
		inspectResp, err := e.cli.ContainerExecInspect(ctx, execResp.ID)
		if err != nil {
			// Without the exit code we can't tell success from failure, so don't guess.
			return nil, fmt.Errorf("failed to inspect exec: %w", err)
		}
		finalExitCode = inspectResp.ExitCode
		if finalExitCode == exitCodeKilled {
			stderr.note(fmt.Sprintf("\nKilled: most likely the program exceeded the memory limit (%d MB).\n", e.config.MemoryLimit/(1024*1024)))
		}
	case <-executeCtx.Done():
		// Timeout reached. Closing the stream unblocks the copier goroutine, and we
		// wait for it to exit before touching stdout/stderr — reading the buffers
		// while it is still writing to them is a data race.
		// The process itself is killed when the deferred ContainerRemove runs.
		attachResp.Close()
		<-done
		finalExitCode = 124 // Custom exit code for timeout (similar to unix timeout command)
		stderr.note(fmt.Sprintf("\nExecution timed out after %s.\n", e.config.Timeout))
	}

	return &executor.ExecutionResult{
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		ExitCode:  finalExitCode,
		Duration:  time.Since(start),
		Truncated: stdout.truncated || stderr.truncated,
	}, nil
}
