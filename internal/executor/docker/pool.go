package docker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// Pool manages a pool of pre-warmed Docker containers for fast code execution.
type Pool struct {
	cli        *client.Client
	config     Config
	logger     *slog.Logger
	containers chan string
	done       chan struct{}
	wg         sync.WaitGroup
	startDone  sync.Once
}

// NewPool initializes a new container pool wrapper.
func NewPool(cli *client.Client, cfg Config, logger *slog.Logger) *Pool {
	return &Pool{
		cli:        cli,
		config:     cfg,
		logger:     logger,
		containers: make(chan string, cfg.PoolSize),
		done:       make(chan struct{}),
	}
}

// Start begins filling the pool with fresh containers in the background.
func (p *Pool) Start() {
	p.startDone.Do(func() {
		p.logger.Info("starting docker container pool manager", slog.Int("poolSize", p.config.PoolSize))
		p.wg.Add(1)
		go p.manager()
	})
}

// Stop shuts down the manager and cleans up all pre-warmed containers.
func (p *Pool) Stop() {
	p.logger.Info("shutting down docker container pool")
	close(p.done)
	p.wg.Wait()

	// Drain channel and remove surviving containers
	for {
		select {
		case id := <-p.containers:
			p.removeContainer(id)
		default:
			return
		}
	}
}

// GetContainer returns a ready-to-use container ID from the pool.
// It blocks until one is available or the context is canceled.
func (p *Pool) GetContainer(ctx context.Context) (string, error) {
	select {
	case id := <-p.containers:
		return id, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// manager continuously ensures the pool is at capacity.
func (p *Pool) manager() {
	defer p.wg.Done()

	for {
		select {
		case <-p.done:
			return
		default:
			// Ensure we only try to create a container if there's room in the channel
			if len(p.containers) < cap(p.containers) {
				id, err := p.createContainer()
				if err != nil {
					p.logger.Error("failed to create pre-warmed container", slog.String("error", err.Error()))
					time.Sleep(1 * time.Second) // backoff on failure
					continue
				}

				// Try to push to channel, or delete if shutting down
				select {
				case p.containers <- id:
					// Successfully added to pool
				case <-p.done:
					// Shutting down while trying to push
					p.removeContainer(id)
					return
				}
			} else {
				// Pool is full, wait a bit
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
}

// hostConfig builds the sandbox limits for a container from the config.
// It is a pure function so the limits can be unit-tested without Docker.
func hostConfig(cfg Config) *container.HostConfig {
	pidsLimit := cfg.PidsLimit
	return &container.HostConfig{
		// No network interfaces except loopback: no outbound or inbound traffic.
		NetworkMode: "none",
		Resources: container.Resources{
			Memory: cfg.MemoryLimit,
			// MemorySwap is memory + swap; setting it equal to Memory disables swap,
			// otherwise the container could use up to twice the limit.
			MemorySwap: cfg.MemoryLimit,
			NanoCPUs:   int64(cfg.CPULimit * 1e9),
			PidsLimit:  &pidsLimit,
		},
		AutoRemove: false,
		// The root filesystem is read-only; /tmp is a small in-memory scratch area
		// where nothing can be executed.
		ReadonlyRootfs: true,
		Tmpfs: map[string]string{
			"/tmp": "rw,noexec,nosuid,nodev,mode=1777,size=" + cfg.TmpSize,
		},
		// Drop every Linux capability and forbid gaining new privileges (e.g. via setuid binaries).
		CapDrop:     []string{"ALL"},
		SecurityOpt: []string{"no-new-privileges"},
	}
}

// createContainer starts a container running `sleep infinity`.
func (p *Pool) createContainer() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := p.cli.ContainerCreate(ctx, &container.Config{
		Image:        p.config.Image,
		Cmd:          []string{"sleep", "infinity"},
		Tty:          false,
		AttachStdout: false,
		AttachStderr: false,
		// Run as the unprivileged "nobody" user rather than root.
		User: "nobody",
	}, hostConfig(p.config), nil, nil, "")

	if err != nil {
		return "", fmt.Errorf("ContainerCreate failed: %w", err)
	}

	if err := p.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		p.removeContainer(resp.ID) // Cleanup
		return "", fmt.Errorf("ContainerStart failed: %w", err)
	}

	return resp.ID, nil
}

// removeContainer force removes a container by ID.
func (p *Pool) removeContainer(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = p.cli.ContainerRemove(ctx, id, container.RemoveOptions{
		Force: true,
	})
}
