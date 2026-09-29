package docker

// Unit tests for the sandbox limits. These run without Docker, so the limits are
// verified on every machine; sandbox_test.go checks they actually hold in a container.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostConfig(t *testing.T) {
	cfg := DefaultConfig()
	hc := hostConfig(cfg)

	t.Run("no network", func(t *testing.T) {
		assert.Equal(t, "none", string(hc.NetworkMode))
	})

	t.Run("memory capped with no extra swap", func(t *testing.T) {
		assert.Equal(t, cfg.MemoryLimit, hc.Memory)
		// MemorySwap is memory+swap, so equal values mean zero swap.
		assert.Equal(t, cfg.MemoryLimit, hc.MemorySwap)
	})

	t.Run("cpu capped", func(t *testing.T) {
		assert.Equal(t, int64(cfg.CPULimit*1e9), hc.NanoCPUs)
	})

	t.Run("process count capped", func(t *testing.T) {
		require.NotNil(t, hc.PidsLimit)
		assert.Equal(t, cfg.PidsLimit, *hc.PidsLimit)
	})

	t.Run("privileges dropped", func(t *testing.T) {
		assert.Equal(t, []string{"ALL"}, []string(hc.CapDrop))
		assert.Contains(t, hc.SecurityOpt, "no-new-privileges")
	})

	t.Run("read-only root with a small writable /tmp", func(t *testing.T) {
		assert.True(t, hc.ReadonlyRootfs)
		require.Contains(t, hc.Tmpfs, "/tmp")
		assert.Contains(t, hc.Tmpfs["/tmp"], "size="+cfg.TmpSize)
		assert.Contains(t, hc.Tmpfs["/tmp"], "noexec")
	})
}

func TestCappedBuffer(t *testing.T) {
	t.Run("keeps everything under the limit", func(t *testing.T) {
		b := &cappedBuffer{limit: 10}
		n, err := b.Write([]byte("hello"))
		require.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, "hello", b.String())
		assert.False(t, b.truncated)
	})

	t.Run("keeps exactly the limit and flags the rest as dropped", func(t *testing.T) {
		b := &cappedBuffer{limit: 10}
		for range 3 {
			n, err := b.Write([]byte("abcdef"))
			require.NoError(t, err)
			// Reporting the full length keeps the stream draining, so the
			// program is never blocked on a full pipe.
			assert.Equal(t, 6, n)
		}
		assert.Equal(t, "abcdefabcd", b.String())
		assert.True(t, b.truncated)
	})

	t.Run("a large single write is cut at the limit", func(t *testing.T) {
		b := &cappedBuffer{limit: 1024}
		_, err := b.Write([]byte(strings.Repeat("x", 1<<20)))
		require.NoError(t, err)
		assert.Equal(t, 1024, len(b.String()))
		assert.True(t, b.truncated)
	})
}
