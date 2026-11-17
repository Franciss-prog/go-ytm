// Package app is the composition root: it builds the concrete services,
// starts the Bubble Tea program, owns every long-lived goroutine and shuts
// them down in order (docs/specs/01-architecture.md, "Concurrency model").
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/config"
	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/platform"
	"github.com/Franciss-Prog/go-ytm/internal/player"
	"github.com/Franciss-Prog/go-ytm/internal/theme"
	"github.com/Franciss-Prog/go-ytm/internal/ui"
)

// ShutdownBudget bounds the whole shutdown (spec 01).
const ShutdownBudget = 3 * time.Second

// Options are what the CLI hands to the TUI.
type Options struct {
	Config      config.Config
	ConfigDiags config.Diagnostics
	Paths       platform.Paths
	Theme       theme.Theme
	NoColor     bool
	Log         *slog.Logger
	// TraceFrames logs per-frame Update/View timings at debug level.
	TraceFrames bool

	// In/Out/Env run the program; nil means the process's own.
	In  io.Reader
	Out io.Writer
	Env []string

	// LookPath and NewPlayer are replaced in tests.
	LookPath  func(string) (string, error)
	NewPlayer func(ctx context.Context, o player.Options) player.Player
	// ProgramOptions are appended (tests pass tea.WithInput etc.).
	ProgramOptions []tea.ProgramOption
}

// Run starts the TUI and blocks until it quits and everything has shut
// down (bounded by ShutdownBudget).
func Run(ctx context.Context, o Options) error {
	start := time.Now()
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	if o.NewPlayer == nil {
		o.NewPlayer = func(ctx context.Context, po player.Options) player.Player { return player.Start(ctx, po) }
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	km, kdiags := keys.LoadFile(o.Paths.KeymapFile())
	toasts := startupToasts(o.ConfigDiags, kdiags)
	for _, d := range kdiags {
		o.Log.Info("keymap diagnostic", "diag", d.String())
	}

	var pl player.Player
	if _, err := o.LookPath("mpv"); err != nil {
		o.Log.Warn("mpv not found; playback disabled", "err", err)
		toasts = append(toasts, ui.Toast{Severity: ui.SevError, Text: "mpv not found: playback is disabled. Run `ytm doctor`."})
	} else {
		pl = o.NewPlayer(ctx, player.Options{
			Launcher: &player.MPVLauncher{
				RuntimeDir: o.Paths.Runtime,
				UseConfig:  o.Config.Playback.MPVConfig,
				ExtraArgs:  o.Config.Playback.MPVArgs,
				Log:        o.Log,
			},
			Volume: o.Config.Playback.DefaultVolume,
			Log:    o.Log,
		})
	}

	var model tea.Model = ui.New(ui.Options{
		Config: o.Config, Keymap: km, KeymapPath: o.Paths.KeymapFile(),
		Theme: o.Theme, NoColor: o.NoColor, Player: pl, Log: o.Log, Toasts: toasts, Context: ctx,
	})
	if o.TraceFrames {
		model = &tracer{inner: model, log: o.Log, start: start}
	}
	opts := []tea.ProgramOption{tea.WithContext(ctx)}
	if o.In != nil {
		opts = append(opts, tea.WithInput(o.In))
	}
	if o.Out != nil {
		opts = append(opts, tea.WithOutput(o.Out))
	}
	if o.Env != nil {
		opts = append(opts, tea.WithEnvironment(o.Env))
	}
	opts = append(opts, o.ProgramOptions...)
	prog := tea.NewProgram(model, opts...)

	// Player events → program. Send blocks until the program starts and
	// is a no-op after it exits (tea docs), so this cannot deadlock.
	var wg sync.WaitGroup
	if pl != nil {
		events := pl.Events()
		wg.Go(func() {
			guard(o, "player-events", func() {
				for ev := range events {
					prog.Send(ui.PlayerEventMsg{Event: ev})
				}
			})
		})
	}

	_, runErr := prog.Run()
	if errors.Is(runErr, tea.ErrProgramKilled) && ctx.Err() != nil {
		runErr = nil // cancelled from outside (signal): a normal exit
	}

	// Shutdown: stop the player (mpv quit, kill after 2 s), which closes
	// its event channel and ends the forwarder; then cancel the root ctx.
	// The root ctx may already be cancelled: shutdown gets its own budget.
	sctx, scancel := context.WithTimeout(context.Background(), ShutdownBudget) //nolint:contextcheck // see above
	defer scancel()
	if pl != nil {
		if err := pl.Close(sctx); err != nil { //nolint:contextcheck // shutdown budget, not the cancelled root ctx
			o.Log.Warn("player shutdown", "err", err)
		}
	}
	cancel()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-sctx.Done():
		o.Log.Error("shutdown exceeded budget", "budget", ShutdownBudget)
	}
	if runErr != nil {
		return fmt.Errorf("tui: %w", runErr)
	}
	return nil
}

// startupToasts summarizes config and keymap diagnostics in one toast each.
func startupToasts(cfg, km config.Diagnostics) []ui.Toast {
	var out []ui.Toast
	for _, c := range []struct {
		name  string
		diags config.Diagnostics
		cmd   string
	}{{"config", cfg, "ytm config check"}, {"keymap", km, "ytm doctor"}} {
		errs, warns := c.diags.Count(config.SevError), c.diags.Count(config.SevWarning)
		switch {
		case errs > 0:
			out = append(out, ui.Toast{Severity: ui.SevError, Text: fmt.Sprintf("%s: %s — run %#q", c.name, plural(errs, "error"), c.cmd)})
		case warns > 0:
			out = append(out, ui.Toast{Severity: ui.SevWarn, Text: fmt.Sprintf("%s: %s — run %#q", c.name, plural(warns, "warning"), c.cmd)})
		}
	}
	return out
}

func plural(n int, w string) string {
	if n == 1 {
		return "1 " + w
	}
	return fmt.Sprintf("%d %ss", n, w)
}

// guard runs f, turning a panic into a logged error and a crash file in
// <state>/crashes (spec 01: no panic escapes a goroutine we own).
func guard(o Options, name string, f func()) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		stack := debug.Stack()
		o.Log.Error("goroutine panicked", "goroutine", name, "panic", fmt.Sprint(r))
		if o.Paths.State == "" {
			return
		}
		dir := filepath.Join(o.Paths.State, "crashes")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return
		}
		file := filepath.Join(dir, time.Now().UTC().Format("20060102T150405.000Z")+".log")
		_ = os.WriteFile(file, fmt.Appendf(nil, "goroutine %s: panic: %v\n\n%s", name, r, stack), 0o600)
		pruneCrashes(dir, o.Config.Logging.KeepCrashes)
	}()
	f()
}

// pruneCrashes keeps the newest keep crash files ([logging] keep_crashes).
func pruneCrashes(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= keep {
		return
	}
	// Names are UTC timestamps, so lexical order is chronological.
	for _, e := range entries[:len(entries)-keep] {
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
}
