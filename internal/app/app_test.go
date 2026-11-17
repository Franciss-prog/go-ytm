package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"go.uber.org/goleak"

	"github.com/Franciss-Prog/go-ytm/internal/config"
	"github.com/Franciss-Prog/go-ytm/internal/platform"
	"github.com/Franciss-Prog/go-ytm/internal/player"
	"github.com/Franciss-Prog/go-ytm/internal/theme"
	"github.com/Franciss-Prog/go-ytm/internal/ui"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// eventPlayer is a fake whose event channel closes on Close, like the
// real controller.
type eventPlayer struct {
	ch     chan player.Event
	once   sync.Once
	closed chan struct{}
}

func newEventPlayer() *eventPlayer {
	return &eventPlayer{ch: make(chan player.Event, 4), closed: make(chan struct{})}
}

func (p *eventPlayer) Load(context.Context, player.Item, time.Duration) error { return nil }
func (p *eventPlayer) Append(player.Item) error                               { return nil }
func (p *eventPlayer) ClearNext() error                                       { return nil }
func (p *eventPlayer) TogglePause() error                                     { return nil }
func (p *eventPlayer) Play() error                                            { return nil }
func (p *eventPlayer) Pause() error                                           { return nil }
func (p *eventPlayer) Seek(time.Duration, player.SeekMode) error              { return nil }
func (p *eventPlayer) SetVolume(int) error                                    { return nil }
func (p *eventPlayer) ToggleMute() error                                      { return nil }
func (p *eventPlayer) State() player.State                                    { return player.State{Volume: 80} }
func (p *eventPlayer) Events() <-chan player.Event                            { return p.ch }
func (p *eventPlayer) Close(context.Context) error {
	p.once.Do(func() { close(p.ch); close(p.closed) })
	return nil
}

func testOptions(t *testing.T, in io.Reader) (Options, *eventPlayer) {
	t.Helper()
	dir := t.TempDir()
	fp := newEventPlayer()
	return Options{
		Config:    config.Default(),
		Paths:     platform.Paths{Config: dir, State: dir, Runtime: dir},
		Theme:     theme.Result{Theme: theme.Fallback()},
		ThemeSpec: theme.Spec{Source: "static", Name: "ytm-dark", ConfigDir: dir, Watch: true},
		In:        in, Out: io.Discard, Env: []string{"TERM=xterm-256color"},
		LookPath:       func(string) (string, error) { return "/usr/bin/mpv", nil },
		NewPlayer:      func(context.Context, player.Options) player.Player { return fp },
		ProgramOptions: []tea.ProgramOption{tea.WithWindowSize(80, 24)},
	}, fp
}

func TestRunQuitsAndShutsDown(t *testing.T) {
	o, fp := testOptions(t, strings.NewReader("q"))
	fp.ch <- player.StateChanged{State: player.State{Volume: 50}}
	errc := make(chan error, 1)
	go func() { errc <- Run(context.Background(), o) }()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after q")
	}
	select {
	case <-fp.closed:
	default:
		t.Error("player not closed on shutdown")
	}
}

func TestRunCancelledBySignalContext(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	o, fp := testOptions(t, pr)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- Run(ctx, o) }()
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("cancel is a normal exit, got %v", err)
		}
		if d := time.Since(start); d > ShutdownBudget {
			t.Errorf("shutdown took %v", d)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run ignored cancellation")
	}
	<-fp.closed
}

func TestRunWithoutMPV(t *testing.T) {
	o, _ := testOptions(t, strings.NewReader("q"))
	called := false
	o.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	o.NewPlayer = func(context.Context, player.Options) player.Player { called = true; return nil }
	if err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("player started without mpv")
	}
}

func TestStartupToasts(t *testing.T) {
	cfg := config.Diagnostics{{Severity: config.SevWarning}, {Severity: config.SevWarning}}
	km := config.Diagnostics{{Severity: config.SevError}, {Severity: config.SevWarning}}
	got := startupToasts(cfg, km)
	if len(got) != 2 || got[0].Severity != ui.SevWarn || !strings.Contains(got[0].Text, "2 warnings") ||
		got[1].Severity != ui.SevError || !strings.Contains(got[1].Text, "keymap: 1 error") {
		t.Errorf("toasts = %+v", got)
	}
	if len(startupToasts(nil, config.Diagnostics{{Severity: config.SevInfo}})) != 0 {
		t.Error("info diagnostics must not toast")
	}
}

func TestGuardWritesCrashFile(t *testing.T) {
	dir := t.TempDir()
	o := Options{Paths: platform.Paths{State: dir}, Config: config.Default(), Log: discard()}
	o.Config.Logging.KeepCrashes = 2
	for range 4 {
		guard(o, "test", func() { panic("boom") })
		time.Sleep(2 * time.Millisecond) // distinct file names
	}
	files, _ := os.ReadDir(filepath.Join(dir, "crashes"))
	if len(files) != 2 {
		t.Fatalf("crash files = %d, want 2 (keep_crashes)", len(files))
	}
	b, _ := os.ReadFile(filepath.Join(dir, "crashes", files[1].Name()))
	if !strings.Contains(string(b), "panic: boom") || !strings.Contains(string(b), "goroutine") {
		t.Errorf("crash file: %s", b)
	}
}

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestThemeToast(t *testing.T) {
	if themeToast(theme.Result{Theme: theme.Fallback()}, theme.Spec{}) != nil {
		t.Error("toast without error")
	}
	r := theme.Result{Theme: theme.Fallback(), Err: errors.New("pywal colors not found at /x"), Path: "/x"}
	got := themeToast(r, theme.Spec{Watch: true})
	if got == nil || got.Text != "Theme: pywal colors not found at /x — using ytm-dark; will switch when it appears" {
		t.Errorf("toast = %+v", got)
	}
}
