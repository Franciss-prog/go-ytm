//go:build !windows

package player

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/platform"
)

// prepareEndpoint returns the socket path for this process, creating the
// runtime dir and removing a stale socket left by a crashed run.
func prepareEndpoint(ctx context.Context, dir string, pid int) (string, error) {
	if dir == "" {
		return "", errors.New("mpv runtime dir not set")
	}
	path := filepath.Join(dir, "mpv-"+strconv.Itoa(pid)+".sock")
	if err := platform.CheckSocketPath(path); err != nil {
		return "", fmt.Errorf("mpv socket: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create runtime dir: %w", err)
	}
	if _, err := os.Lstat(path); err == nil {
		// Only remove it when nothing answers (a live socket means another
		// mpv we own is still running under a reused PID; refuse to clobber).
		dctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		c, derr := (&net.Dialer{}).DialContext(dctx, "unix", path)
		cancel()
		if derr == nil {
			_ = c.Close()
			return "", fmt.Errorf("mpv socket %s is in use by another process", path)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("remove stale mpv socket: %w", err)
		}
	}
	return path, nil
}

func dialEndpoint(ctx context.Context, path string) (io.ReadWriteCloser, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", path)
}

func cleanupEndpoint(path string) { _ = os.Remove(path) }
