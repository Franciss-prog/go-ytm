// Package app is the composition root: it builds the concrete services,
// starts the Bubble Tea program, owns every long-lived goroutine and shuts
// them down in order (docs/specs/01-architecture.md, "Concurrency model").
package app

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/Franciss-Prog/go-ytm/internal/art"
	"github.com/Franciss-Prog/go-ytm/internal/cache"
	"github.com/Franciss-Prog/go-ytm/internal/clock"
	"github.com/Franciss-Prog/go-ytm/internal/config"
	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/platform"
	"github.com/Franciss-Prog/go-ytm/internal/player"
	"github.com/Franciss-Prog/go-ytm/internal/store"
	"github.com/Franciss-Prog/go-ytm/internal/stream"
	"github.com/Franciss-Prog/go-ytm/internal/theme"
	"github.com/Franciss-Prog/go-ytm/internal/ui"
	"github.com/Franciss-Prog/go-ytm/internal/viz"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// ShutdownBudget bounds the whole shutdown (spec 01).
const ShutdownBudget = 3 * time.Second

// artTimeout bounds one cover art download.
const artTimeout = 15 * time.Second

// spectrumBands is how many bands the visualizer analyzes; the track page
// resamples them to its width.
const spectrumBands = 96

// Options are what the CLI hands to the TUI.
type Options struct {
	Config      config.Config
	ConfigDiags config.Diagnostics
	Paths       platform.Paths
	// Theme is the theme resolved at startup from ThemeSpec (so the first
	// frame is themed); the theme manager takes over from there.
	Theme     theme.Result
	ThemeSpec theme.Spec
	// Profile is the color depth styles are built for.
	Profile colorprofile.Profile
	NoColor bool
	Log     *slog.Logger
	// TraceFrames logs per-frame Update/View timings at debug level.
	TraceFrames bool

	// In/Out/Env run the program; nil means the process's own.
	In  io.Reader
	Out io.Writer
	Env []string

	// LookPath and NewPlayer are replaced in tests.
	LookPath  func(string) (string, error)
	NewPlayer func(ctx context.Context, o player.Options) player.Player
	// API is the YouTube Music client (signed in when SignedIn).
	API      ytmusic.Client
	SignedIn bool
	// Resolver turns video IDs into stream URLs (nil: yt-dlp from
	// [yt_dlp]; tests pass a fake).
	Resolver stream.Resolver
	// Art fetches cover art (nil: over HTTP).
	Art art.Fetcher
	// Audio is the visualizer's capture (nil: pw-cat when available).
	Audio viz.Source
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

	// Streams: one cache shared by play requests and the prefetcher, so a
	// play joins an in-flight prefetch of the same track.
	home, _ := os.UserHomeDir()
	y := stream.FromConfig(o.Config.YtDlp, o.Paths.Config, func(p string) string { return platform.ExpandHome(p, home) })
	y.Log = o.Log
	res := o.Resolver
	if res == nil {
		res = y
	}
	loc, ltoasts := openLocal(ctx, o, y, home)
	defer loc.close(o.Log)
	toasts = append(toasts, ltoasts...)
	if loc.cache != nil {
		res = cache.Resolver{Cache: loc.cache, Next: res}
	}
	streams := stream.NewCache(res, clock.Real{}, o.Log)
	// The prefetcher's results go through prog, created below; onDone is
	// only called after a Want, which Update sends once prog runs.
	var prog *tea.Program
	prefetch := stream.NewPrefetcher(streams, func(r stream.Result) { prog.Send(ui.PrefetchedMsg{Result: r}) }) //nolint:contextcheck // stopped by Close below

	if t := themeToast(o.Theme, o.ThemeSpec); t != nil {
		toasts = append(toasts, *t)
	}
	// The manager's sends go through prog, created below; send is only
	// called from the manager goroutine, started after prog exists.
	themes := theme.NewManager(o.ThemeSpec, func(r theme.Result) { prog.Send(ui.ThemeChangedMsg{Result: r}) }, nil, o.Log)

	// Visualizer: one goroutine (started below, ends with ctx) captures
	// while the track page asks for it; levels go through prog.
	var spectrum *viz.Visualizer
	if src := o.Audio; o.Config.UI.Visualizer && (src != nil || viz.Available(o.LookPath)) {
		if src == nil {
			src = viz.PWCat{}
		}
		spectrum = viz.New(src, spectrumBands, func(l []float64) { prog.Send(ui.SpectrumMsg{Levels: l}) }, o.Log)
	}

	um := ui.New(ui.Options{
		Config: o.Config, Keymap: km, KeymapPath: o.Paths.KeymapFile(),
		Theme: o.Theme.Theme, NoColor: o.NoColor, Player: pl, Log: o.Log, Toasts: toasts, Context: ctx,
		Themes:     themeService{m: themes, configDir: o.ThemeSpec.ConfigDir, home: o.ThemeSpec.Home},
		ConfigPath: o.Paths.ConfigFile(), PatchConfig: config.Patch, Profile: o.Profile,
		Streams: streams, Prefetch: prefetch, API: o.API, SignedIn: o.SignedIn,
		Art: artFetcher(o.Art), Spectrum: uiSpectrum(spectrum),
		Local: loc.local(), Downloads: loc.downloads(), Session: loc.session, ShuffleLocks: loc.locks(),
	})
	var model tea.Model = um
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
	prog = tea.NewProgram(model, opts...)

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

	wg.Go(func() { guard(o, "theme-manager", func() { themes.Run(ctx, o.Theme) }) })
	if spectrum != nil {
		wg.Go(func() { guard(o, "visualizer", func() { spectrum.Run(ctx) }) })
	}
	stopSignals := reloadOnSignal(ctx, themes.Reload)
	defer stopSignals()

	_, runErr := prog.Run()
	if errors.Is(runErr, tea.ErrProgramKilled) && ctx.Err() != nil {
		runErr = nil // cancelled from outside (signal): a normal exit
	}

	// Shutdown (spec 01): save the session, then stop the player (mpv quit,
	// kill after 2 s), which closes its event channel and ends the
	// forwarder; then cancel the root ctx. The program has stopped, so
	// reading the model here races with nothing.
	loc.saveSession(um.Session(), o.Log)
	// The root ctx may already be cancelled: shutdown gets its own budget.
	sctx, scancel := context.WithTimeout(context.Background(), ShutdownBudget) //nolint:contextcheck // see above
	defer scancel()
	if pl != nil {
		if err := pl.Close(sctx); err != nil { //nolint:contextcheck // shutdown budget, not the cancelled root ctx
			o.Log.Warn("player shutdown", "err", err)
		}
	}
	prefetch.Close()
	if err := streams.Close(sctx); err != nil { //nolint:contextcheck // shutdown budget
		o.Log.Warn("stream resolver shutdown", "err", err)
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

// themeService adapts the theme manager to the UI's [theme] settings.
type themeService struct {
	m               *theme.Manager
	configDir, home string
}

func (s themeService) Set(c config.Theme)          { s.m.Set(theme.SpecFromConfig(c, s.configDir, s.home)) }
func (s themeService) Reload()                     { s.m.Reload() }
func (s themeService) SetTerminalBg(c color.Color) { s.m.SetTerminalBg(c) }

// themeToast reports a theme source that failed at startup (spec 08,
// "Fallback").
func themeToast(r theme.Result, spec theme.Spec) *ui.Toast {
	if r.Err == nil {
		return nil
	}
	text := fmt.Sprintf("Theme: %v — using %s", r.Err, r.Theme.Name)
	if spec.Watch && r.Path != "" {
		text += "; will switch when it appears"
	}
	return &ui.Toast{Severity: ui.SevWarn, Text: text}
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

// artFetcher is f, or an HTTP fetcher when nil.
func artFetcher(f art.Fetcher) art.Fetcher {
	if f != nil {
		return f
	}
	return art.HTTPFetcher{Client: &http.Client{Timeout: artTimeout}}
}

// uiSpectrum avoids handing the UI a typed nil.
func uiSpectrum(v *viz.Visualizer) ui.Spectrum {
	if v == nil {
		return nil
	}
	return v
}

// local is the on-disk state: history store, audio cache, session and
// shuffle locks. Each part is nil when it could not be opened; the app
// then runs without it (and says so in a toast).
type local struct {
	dir     string
	db      *store.DB
	cache   *cache.Cache
	session *store.Session
	prefs   *store.ShufflePrefs
}

// openLocal opens the store in the data dir and the audio cache.
func openLocal(ctx context.Context, o Options, y stream.YtDlp, home string) (*local, []ui.Toast) {
	l := &local{dir: o.Paths.Data}
	if l.dir == "" {
		return l, nil
	}
	var toasts []ui.Toast
	db, err := store.Open(ctx, l.dir)
	if err != nil {
		o.Log.Error("store unavailable", "err", err)
		toasts = append(toasts, ui.Toast{Severity: ui.SevWarn, Text: "History and downloads are off this session; see the log"})
	} else {
		l.db = db
		dir := platform.ExpandHome(o.Config.Cache.Location, home)
		if dir == "" {
			dir = filepath.Join(o.Paths.Cache, "audio")
		}
		l.cache = &cache.Cache{Dir: dir, Max: int64(o.Config.Cache.MaxSizeMB) << 20, Index: db, DL: y, Log: o.Log}
	}
	if s, ok, err := store.LoadSession(l.dir); err != nil {
		o.Log.Warn("session not restored", "err", err)
	} else if ok {
		l.session = &s
	}
	prefs, err := store.LoadShufflePrefs(l.dir)
	if err != nil {
		o.Log.Warn("shuffle prefs unreadable; starting empty", "err", err)
	}
	l.prefs = prefs
	return l, toasts
}

// The ui options take interfaces: a nil pointer must stay a nil interface.
func (l *local) local() ui.Local {
	if l.db == nil {
		return nil
	}
	return l.db
}

func (l *local) downloads() ui.Downloads {
	if l.cache == nil {
		return nil
	}
	return l.cache
}

func (l *local) locks() ui.ShuffleLocks {
	if l.prefs == nil {
		return nil
	}
	return l.prefs
}

// saveSession writes session.json (skipped without a data dir).
func (l *local) saveSession(s store.Session, log *slog.Logger) {
	if l.dir == "" {
		return
	}
	if err := store.SaveSession(l.dir, s); err != nil {
		log.Error("session not saved", "err", err)
	}
}

func (l *local) close(log *slog.Logger) {
	if l.db != nil {
		if err := l.db.Close(); err != nil {
			log.Warn("store close", "err", err)
		}
	}
}
