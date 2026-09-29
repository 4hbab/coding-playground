package docker_test

// Integration tests proving each sandbox limit holds inside a real container.
// Where possible a limit is checked twice: by reading the value the kernel
// enforces (cgroup files) and by running code that tries to break it.

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sakif/coding-playground/internal/executor"
	"github.com/sakif/coding-playground/internal/executor/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSandboxLimits(t *testing.T) {
	requireDocker(t)

	cfg := docker.DefaultConfig()
	cfg.PoolSize = 2
	cfg.Timeout = 3 * time.Second // shorter than the default so the timeout tests stay quick

	exec := newTestExecutor(t, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))

	run := func(t *testing.T, code string) *executor.ExecutionResult {
		t.Helper()
		res, err := exec.Execute(context.Background(), executor.ExecutionRequest{Code: code})
		require.NoError(t, err)
		return res
	}

	// Pool wait + container teardown on top of the limit itself.
	const timeoutSlack = 3 * time.Second

	t.Run("time limit", func(t *testing.T) {
		t.Run("busy loop is stopped", func(t *testing.T) {
			res := run(t, `while True: pass`)
			assert.Equal(t, 124, res.ExitCode)
			assert.Contains(t, res.Stderr, "timed out")
			assert.Less(t, res.Duration, cfg.Timeout+timeoutSlack)
		})

		t.Run("sleeping program is stopped", func(t *testing.T) {
			res := run(t, "import time\ntime.sleep(60)")
			assert.Equal(t, 124, res.ExitCode)
			assert.Less(t, res.Duration, cfg.Timeout+timeoutSlack)
		})

		t.Run("program still printing when time runs out", func(t *testing.T) {
			// Regression test: output is still streaming at the timeout, which used
			// to race with reading the buffers (caught by -race).
			res := run(t, `while True: print("x" * 100)`)
			assert.Equal(t, 124, res.ExitCode)
			assert.LessOrEqual(t, len(res.Stdout), cfg.OutputLimit)
		})

		t.Run("output printed before the timeout is kept", func(t *testing.T) {
			// Python buffers stdout when it isn't a terminal; without unbuffered
			// output this line would be lost when the process is killed.
			res := run(t, "print(\"before the loop\")\nwhile True: pass")
			assert.Equal(t, 124, res.ExitCode)
			assert.Equal(t, "before the loop\n", res.Stdout)
		})

		t.Run("sandbox works again after a timeout", func(t *testing.T) {
			res := run(t, `print("still alive")`)
			assert.Equal(t, 0, res.ExitCode)
			assert.Contains(t, res.Stdout, "still alive")
		})
	})

	t.Run("memory limit", func(t *testing.T) {
		t.Run("cgroup enforces the configured limit", func(t *testing.T) {
			res := run(t, `print(open("/sys/fs/cgroup/memory.max").read().strip())`)
			require.Equal(t, 0, res.ExitCode, res.Stderr)
			assert.Equal(t, strconv.FormatInt(cfg.MemoryLimit, 10), strings.TrimSpace(res.Stdout))
		})

		t.Run("allocating past the limit kills the program", func(t *testing.T) {
			// bytes repetition writes every byte, so the pages are really used.
			res := run(t, fmt.Sprintf(`data = b"a" * %d`, 4*cfg.MemoryLimit))
			assert.NotEqual(t, 0, res.ExitCode)
			if res.ExitCode == 137 {
				assert.Contains(t, res.Stderr, "memory limit")
			} else {
				assert.Contains(t, res.Stderr, "MemoryError")
			}
		})

		t.Run("sandbox works again after running out of memory", func(t *testing.T) {
			res := run(t, `print("still alive")`)
			assert.Equal(t, 0, res.ExitCode)
		})
	})

	t.Run("cpu limit", func(t *testing.T) {
		// cpu.max is "<quota> <period>": the container may use quota µs of CPU per period µs.
		res := run(t, `print(open("/sys/fs/cgroup/cpu.max").read().strip())`)
		require.Equal(t, 0, res.ExitCode, res.Stderr)
		const period = 100000
		assert.Equal(t, fmt.Sprintf("%d %d", int64(cfg.CPULimit*period), period), strings.TrimSpace(res.Stdout))
	})

	t.Run("no network access", func(t *testing.T) {
		res := run(t, `
import socket
# The kernel may list built-in tunnel devices (gre0, sit0, ...) but they are down.
# What matters is that there are no routes, so no packet can leave the container.
ipv4 = open("/proc/net/route").read().splitlines()[1:]  # first line is a header
ipv6 = [r for r in open("/proc/net/ipv6_route").read().splitlines() if not r.endswith(" lo")]
print("routes", len(ipv4) + len(ipv6))
try:
    socket.create_connection(("1.1.1.1", 53), timeout=2)
    print("tcp connected")
except OSError:
    print("tcp blocked")
# DNS goes over UDP. A real lookup also fails, but only after the resolver's
# 5s retry, so send a UDP packet directly, which fails immediately.
try:
    socket.socket(socket.AF_INET, socket.SOCK_DGRAM).sendto(b"x", ("8.8.8.8", 53))
    print("udp sent")
except OSError:
    print("udp blocked")
`)
		require.Equal(t, 0, res.ExitCode, res.Stderr)
		assert.Contains(t, res.Stdout, "routes 0")
		assert.Contains(t, res.Stdout, "tcp blocked")
		assert.Contains(t, res.Stdout, "udp blocked")
	})

	t.Run("output size limit", func(t *testing.T) {
		t.Run("stdout is cut at the limit", func(t *testing.T) {
			res := run(t, `print("x" * 10_000_000)`)
			assert.Equal(t, 0, res.ExitCode)
			assert.Len(t, res.Stdout, cfg.OutputLimit)
			assert.True(t, res.Truncated)
		})

		t.Run("stderr is cut at the limit", func(t *testing.T) {
			res := run(t, "import sys\nsys.stderr.write(\"e\" * 10_000_000)")
			assert.Equal(t, 0, res.ExitCode)
			assert.Len(t, res.Stderr, cfg.OutputLimit)
			assert.True(t, res.Truncated)
		})

		t.Run("small output is not flagged", func(t *testing.T) {
			res := run(t, `print("short")`)
			assert.Equal(t, "short\n", res.Stdout)
			assert.False(t, res.Truncated)
		})
	})

	t.Run("process limit", func(t *testing.T) {
		t.Run("cgroup enforces the configured limit", func(t *testing.T) {
			res := run(t, `print(open("/sys/fs/cgroup/pids.max").read().strip())`)
			require.Equal(t, 0, res.ExitCode, res.Stderr)
			assert.Equal(t, strconv.FormatInt(cfg.PidsLimit, 10), strings.TrimSpace(res.Stdout))
		})

		t.Run("fork bomb is stopped", func(t *testing.T) {
			res := run(t, `
import os, signal
children = []
try:
    for _ in range(1000):
        pid = os.fork()
        if pid == 0:
            signal.pause()
            os._exit(0)
        children.append(pid)
    print("forked", len(children))
except OSError:
    print("blocked after", len(children))
finally:
    for pid in children:
        os.kill(pid, signal.SIGKILL)
`)
			require.Equal(t, 0, res.ExitCode, res.Stderr)
			assert.Contains(t, res.Stdout, "blocked after")
		})
	})

	t.Run("no privileges", func(t *testing.T) {
		res := run(t, `
import os
status = dict(line.split(":\t", 1) for line in open("/proc/self/status").read().splitlines())
print("uid", os.getuid())
print("capabilities", status["CapBnd"].strip())
print("no_new_privs", status["NoNewPrivs"].strip())
`)
		require.Equal(t, 0, res.ExitCode, res.Stderr)
		assert.Contains(t, res.Stdout, "uid 65534") // nobody
		assert.Contains(t, res.Stdout, "capabilities 0000000000000000")
		assert.Contains(t, res.Stdout, "no_new_privs 1")
	})

	t.Run("filesystem", func(t *testing.T) {
		res := run(t, `
root = [line.split() for line in open("/proc/mounts") if line.split()[1] == "/"][0]
print("root", root[3].split(",")[0])
open("/tmp/ok.txt", "w").write("hi")
print("tmp writable")
try:
    with open("/tmp/big", "wb") as f:
        f.write(b"0" * (32 * 1024 * 1024))
    print("tmp unlimited")
except OSError:
    print("tmp full")
`)
		require.Equal(t, 0, res.ExitCode, res.Stderr)
		assert.Contains(t, res.Stdout, "root ro")
		assert.Contains(t, res.Stdout, "tmp writable")
		assert.Contains(t, res.Stdout, "tmp full") // 32 MiB does not fit in the 16 MiB /tmp
	})
}
