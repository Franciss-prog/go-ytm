package player

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// Engine is one running mpv instance.
type Engine interface {
	// Conn is the IPC byte stream to mpv.
	Conn() io.ReadWriteCloser
	// Exited is closed when the process has exited.
	Exited() <-chan struct{}
	// Kill terminates the process (if still running) and releases resources
	// such as the socket file. It is safe to call more than once.
	Kill() error
}

// LaunchOptions are per-launch settings.
type LaunchOptions struct {
	Volume int
}

// Launcher starts mpv. Tests substitute a fake.
type Launcher interface {
	Launch(ctx context.Context, opts LaunchOptions) (Engine, error)
}

// MPVLauncher starts the real mpv binary.
type MPVLauncher struct {
	// Binary is the mpv executable name or path (default "mpv").
	Binary string
	// RuntimeDir holds the IPC socket on Unix (created 0700 if missing).
	RuntimeDir string
	// UseConfig loads the user's mpv.conf ([playback] mpv_config).
	UseConfig bool
	// ExtraArgs are appended verbatim ([playback] mpv_args).
	ExtraArgs []string
	// Log receives mpv's stderr at debug level.
	Log *slog.Logger
	// ConnectTimeout bounds how long Launch waits for the socket (default 3 s).
	ConnectTimeout time.Duration
}

// Args returns the mpv command line for the given socket and options.
func (l *MPVLauncher) Args(socket string, opts LaunchOptions) []string {
	args := []string{
		"--idle=yes", "--no-video", "--no-terminal",
		"--input-ipc-server=" + socket,
		"--prefetch-playlist=yes", "--gapless-audio=weak",
		"--volume=" + strconv.Itoa(clampVolume(opts.Volume)), "--volume-max=100",
		"--cache=yes", "--demuxer-max-bytes=50MiB",
		"--ytdl=no",
	}
	if !l.UseConfig {
		args = append([]string{"--no-config"}, args...)
	}
	return append(args, l.ExtraArgs...)
}

// Launch starts mpv and connects to its IPC endpoint.
func (l *MPVLauncher) Launch(ctx context.Context, opts LaunchOptions) (Engine, error) {
	bin := l.Binary
	if bin == "" {
		bin = "mpv"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMPVNotFound, err)
	}
	endpoint, err := prepareEndpoint(ctx, l.RuntimeDir, os.Getpid())
	if err != nil {
		return nil, err
	}

	// The process outlives ctx (ctx only bounds startup), so no CommandContext.
	cmd := exec.Command(path, l.Args(endpoint, opts)...) //nolint:noctx // lifetime managed by Engine.Kill
	cmd.Env = append(os.Environ(), "LC_NUMERIC=C")
	cmd.Stdout = io.Discard
	cmd.Stderr = &logWriter{log: l.Log}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mpv: %w", err)
	}
	e := &mpvEngine{cmd: cmd, endpoint: endpoint, exited: make(chan struct{})}
	go func() {
		e.waitErr = cmd.Wait()
		close(e.exited)
	}()

	timeout := l.ConnectTimeout
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := dialWithBackoff(dctx, endpoint, e.exited)
	if err != nil {
		_ = e.Kill()
		if e.waitErr != nil {
			err = errors.Join(err, fmt.Errorf("mpv exited: %w", e.waitErr))
		}
		return nil, fmt.Errorf("connect to mpv at %s: %w", endpoint, err)
	}
	e.conn = conn
	return e, nil
}

// dialWithBackoff polls the endpoint 5 ms → 200 ms until it connects, ctx
// expires, or the process exits.
func dialWithBackoff(ctx context.Context, endpoint string, exited <-chan struct{}) (io.ReadWriteCloser, error) {
	delay := 5 * time.Millisecond
	for {
		c, err := dialEndpoint(ctx, endpoint)
		if err == nil {
			return c, nil
		}
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, errors.Join(ctx.Err(), err)
		case <-exited:
			t.Stop()
			return nil, errors.New("mpv exited before accepting connections")
		case <-t.C:
		}
		delay = min(delay*2, 200*time.Millisecond)
	}
}

type mpvEngine struct {
	cmd      *exec.Cmd
	endpoint string
	conn     io.ReadWriteCloser
	exited   chan struct{}
	waitErr  error
	once     sync.Once
}

func (e *mpvEngine) Conn() io.ReadWriteCloser { return e.conn }
func (e *mpvEngine) Exited() <-chan struct{}  { return e.exited }

func (e *mpvEngine) Kill() error {
	e.once.Do(func() {
		if e.conn != nil {
			_ = e.conn.Close()
		}
		select {
		case <-e.exited:
		default:
			_ = e.cmd.Process.Kill()
			<-e.exited
		}
		cleanupEndpoint(e.endpoint)
	})
	return nil
}

// logWriter forwards mpv's stderr lines to the debug log.
type logWriter struct{ log *slog.Logger }

func (w *logWriter) Write(p []byte) (int, error) {
	if w.log != nil {
		w.log.Debug("mpv stderr", "line", string(p))
	}
	return len(p), nil
}

func clampVolume(v int) int { return max(0, min(100, v)) }
