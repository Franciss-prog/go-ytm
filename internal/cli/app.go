// Package cli builds the `ytm` command tree (Cobra + fang) and wires the
// shared CLI state: paths, config, logging, theme and output policy.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"charm.land/fang/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"golang.org/x/term"

	"github.com/Franciss-Prog/go-ytm/internal/buildinfo"
	"github.com/Franciss-Prog/go-ytm/internal/config"
	"github.com/Franciss-Prog/go-ytm/internal/logging"
	"github.com/Franciss-Prog/go-ytm/internal/platform"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/theme"
)

// Exit codes (docs/specs/06-cli-ipc.md).
const (
	ExitOK          = 0
	ExitFailure     = 1
	ExitUsage       = 2
	ExitUnauth      = 3
	ExitNotRunning  = 4
	ExitMissingDep  = 5
	ExitInterrupted = 130
)

// ExitError carries a process exit code. Silent errors have already been
// reported to the user and are not printed again.
type ExitError struct {
	Code   int
	Err    error
	Silent bool
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error { return e.Err }

// IO bundles the process streams and environment so commands are testable.
type IO struct {
	In   io.Reader
	Out  io.Writer
	Err  io.Writer
	Env  []string
	GOOS string
	// IsTTY reports whether w is an interactive terminal.
	IsTTY func(w io.Writer) bool
	// InTTY reports whether In is an interactive terminal (prompts allowed).
	InTTY func() bool
	// Profile detects the color profile of a terminal stream.
	Profile func(w io.Writer) colorprofile.Profile
}

// SystemIO returns IO for the real process.
func SystemIO() IO {
	env := os.Environ()
	return IO{
		In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Env: env, GOOS: runtime.GOOS,
		IsTTY: func(w io.Writer) bool {
			f, ok := w.(interface{ Fd() uintptr })
			return ok && term.IsTerminal(int(f.Fd()))
		},
		Profile: func(w io.Writer) colorprofile.Profile { return colorprofile.Detect(w, env) },
		InTTY:   func() bool { return term.IsTerminal(int(os.Stdin.Fd())) },
	}
}

// App is the shared state behind every command.
type App struct {
	IO IO

	// flags
	debug       bool
	noColor     bool
	traceFrames bool
	configDir   string
	pprofAddr   string

	// resolved lazily by load()
	loaded bool
	Paths  platform.Paths
	Config config.Config
	Diags  config.Diagnostics
	Theme  theme.Theme
	// ThemeSpec and ThemeResult are how Theme was resolved ([theme]).
	ThemeSpec   theme.Spec
	ThemeResult theme.Result
	Log         *slog.Logger

	closeMu sync.Mutex
	closers []func() error

	httpOnce sync.Once
	httpc    *http.Client // shared by every ytmusic client of this command

	// RunCommand runs an external program attached to the terminal
	// (overridden in tests).
	RunCommand func(ctx context.Context, name string, args ...string) error
}

// NewApp returns an App using io.
func NewApp(io IO) *App {
	a := &App{IO: io, Log: slog.New(slog.DiscardHandler), Theme: theme.Fallback()}
	a.RunCommand = func(ctx context.Context, name string, args ...string) error {
		c := execCommand(ctx, name, args...)
		c.Stdin, c.Stdout, c.Stderr = a.IO.In, a.IO.Out, a.IO.Err
		return c.Run()
	}
	return a
}

func (a *App) getenv(k string) string {
	for i := len(a.IO.Env) - 1; i >= 0; i-- {
		if kk, v, ok := cutEnv(a.IO.Env[i]); ok && kk == k {
			return v
		}
	}
	return ""
}

func cutEnv(kv string) (string, string, bool) {
	for i := 0; i < len(kv); i++ {
		if kv[i] == '=' {
			return kv[:i], kv[i+1:], true
		}
	}
	return "", "", false
}

func (a *App) platformEnv() platform.Env {
	e := platform.DefaultEnv()
	e.GOOS = a.IO.GOOS
	e.Getenv = a.getenv
	e.HomeDir = func() (string, error) {
		for _, k := range []string{"HOME", "USERPROFILE"} {
			if h := a.getenv(k); h != "" {
				return h, nil
			}
		}
		return os.UserHomeDir()
	}
	return e
}

// load resolves paths, config, theme and logging once. Errors that should
// not stop the CLI (e.g. unwritable log dir) are logged as diagnostics.
func (a *App) load() error {
	if a.loaded {
		return nil
	}
	a.loaded = true
	p, err := platform.Resolve(a.platformEnv(), a.configDir)
	if err != nil {
		return &ExitError{Code: ExitFailure, Err: err}
	}
	a.Paths = p
	a.Config, a.Diags = config.LoadFile(p.ConfigFile())
	home, _ := a.platformEnv().HomeDir()
	a.ThemeSpec = theme.SpecFromConfig(a.Config.Theme, p.Config, home)
	a.ThemeSpec.TerminalBg = theme.ParseCOLORFGBG(a.getenv("COLORFGBG"))
	a.ThemeResult = theme.Resolve(a.ThemeSpec)
	a.Theme = a.ThemeResult.Theme

	logger, closer, lerr := logging.Setup(logging.Options{
		File:        p.LogFile(),
		Level:       logging.LevelFromConfig(a.Config.Logging.Level),
		MaxBytes:    int64(a.Config.Logging.MaxBytes),
		BackupCount: a.Config.Logging.BackupCount,
		Debug:       a.debug,
		Stderr:      a.IO.Err,
	})
	a.Log = logger
	a.addCloser(closer.Close)
	if lerr != nil {
		a.Log.Warn("file logging disabled", "err", lerr)
	}
	for _, d := range a.Diags {
		a.Log.Debug("config diagnostic", "diag", d.String())
	}
	return nil
}

// Mode returns the output mode for stdout.
func (a *App) Mode(jsonOut bool) style.Mode {
	return a.modeFor(a.IO.Out, jsonOut)
}

func (a *App) modeFor(w io.Writer, jsonOut bool) style.Mode {
	in := style.PolicyInput{Env: a.IO.Env, NoColor: a.noColor, JSON: jsonOut}
	if a.IO.IsTTY != nil {
		in.IsTTY = a.IO.IsTTY(w)
	}
	if in.IsTTY && a.IO.Profile != nil {
		in.Detected = a.IO.Profile(w)
	}
	return style.Decide(in)
}

// Renderer returns a renderer for stdout in the given mode.
func (a *App) Renderer(m style.Mode) *style.Renderer {
	icons := style.ResolveIcons(a.Config.UI.Icons, m.Styled, style.SupportsUnicode(a.IO.Env, a.IO.GOOS))
	return style.New(style.Options{Theme: a.Theme, Transparent: a.Config.Theme.Transparent, Icons: icons, Mode: m})
}

// Print writes styled text to stdout through the mode's color writer.
func (a *App) Print(m style.Mode, s string) error {
	_, err := io.WriteString(m.Writer(a.IO.Out), s)
	return err
}

// PrintJSON writes v as compact single-line JSON to stdout.
func (a *App) PrintJSON(v any) error {
	enc := json.NewEncoder(a.IO.Out)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// startPprof serves net/http/pprof on addr until Close.
func (a *App) startPprof(ctx context.Context, addr string) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("pprof: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	a.addCloser(func() error {
		// Shutdown runs after the command context is done; keep its values
		// but not its cancellation.
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	})
	m := a.modeFor(a.IO.Err, false)
	_, err = io.WriteString(m.Writer(a.IO.Err), a.Renderer(m).Hint("pprof listening on", "http://"+ln.Addr().String()+"/debug/pprof/"))
	return err
}

// addCloser registers a cleanup run by Close (safe for concurrent use).
func (a *App) addCloser(c func() error) {
	a.closeMu.Lock()
	defer a.closeMu.Unlock()
	a.closers = append(a.closers, c)
}

// httpClient returns the HTTP client shared by this command's API clients;
// Close releases its idle connections.
func (a *App) httpClient() *http.Client {
	a.httpOnce.Do(func() {
		a.httpc = &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}
		a.addCloser(func() error { a.httpc.CloseIdleConnections(); return nil })
	})
	return a.httpc
}

// Close releases resources (log file, pprof server).
func (a *App) Close() error {
	var errs []error
	a.closeMu.Lock()
	closers := a.closers
	a.closers = nil
	a.closeMu.Unlock()
	for _, c := range slices.Backward(closers) {
		errs = append(errs, c())
	}
	return errors.Join(errs...)
}

// Main runs the CLI and returns the process exit code.
func Main(ctx context.Context, args []string, io IO) int {
	a := NewApp(io)
	root := NewRootCmd(a) //nolint:contextcheck // commands get ctx from fang via cmd.Context()
	root.SetArgs(args)
	root.SetIn(io.In)
	root.SetOut(io.Out)
	root.SetErr(io.Err)

	b := buildinfo.Get()
	err := fang.Execute(ctx, root,
		fang.WithVersion(fmt.Sprintf("%s · %s · %s/%s", b.Version, b.GoVersion, b.OS, b.Arch)),
		fang.WithCommit(b.Commit),
		fang.WithColorSchemeFunc(a.fangColorScheme),
		fang.WithErrorHandler(a.handleError),
		fang.WithNotifySignal(os.Interrupt, syscall.SIGTERM),
	)
	_ = a.Close()
	return ExitCode(ctx, err)
}

// fangColorScheme is evaluated lazily by fang when rendering help or errors,
// after flags (e.g. --config-dir) have been parsed.
func (a *App) fangColorScheme(ld lipgloss.LightDarkFunc) fang.ColorScheme {
	_ = a.load()
	return style.FangColorScheme(a.Theme, a.Config.Theme.Transparent)(ld)
}

func (a *App) handleError(w io.Writer, styles fang.Styles, err error) {
	var ee *ExitError
	if errors.As(err, &ee) && ee.Silent {
		return
	}
	// fang title-cases the first word ("yt-dlp" → "Yt-Dlp", "--slots" →
	// "--Slots"); capitalize plain words only.
	styles.ErrorText = styles.ErrorText.Transform(capitalizeFirstWord)
	fang.DefaultErrorHandler(w, styles, err)
}

// capitalizeFirstWord upper-cases the first letter when the first word is
// a plain lowercase word, leaving names, flags and commands as written.
func capitalizeFirstWord(s string) string {
	word, _, _ := strings.Cut(s, " ")
	if word == "" || word[0] < 'a' || word[0] > 'z' || strings.ContainsAny(word, "-_`./[") {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ExitCode maps an error from Execute to a process exit code.
func ExitCode(ctx context.Context, err error) int {
	if err == nil {
		return ExitOK
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	if errors.Is(err, context.Canceled) || ctx.Err() != nil {
		return ExitInterrupted
	}
	if strings.HasPrefix(err.Error(), "unknown command") {
		return ExitUsage
	}
	return ExitFailure
}
