//go:build windows

package player

import (
	"context"
	"io"
	"strconv"

	"github.com/Microsoft/go-winio"
)

// prepareEndpoint returns the named pipe mpv should listen on. Pipes vanish
// with their owner, so there is nothing stale to clean.
func prepareEndpoint(_ context.Context, _ string, pid int) (string, error) {
	return `\\.\pipe\ytm-go-mpv-` + strconv.Itoa(pid), nil
}

func dialEndpoint(ctx context.Context, path string) (io.ReadWriteCloser, error) {
	return winio.DialPipeContext(ctx, path)
}

func cleanupEndpoint(string) {}
