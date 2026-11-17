package cli

import (
	"errors"
	"os/exec"
	"os/signal"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Franciss-Prog/go-ytm/internal/app"
)

var execCommand = exec.CommandContext

// NewRootCmd builds the full command tree around a.
func NewRootCmd(a *App) *cobra.Command {
	root := &cobra.Command{
		Use:   "ytm",
		Short: "A fast YouTube Music client for your terminal",
		Long: "ytm-go is a YouTube Music client for the terminal, with mpv playback,\n" +
			"vim-style keys, and wallpaper-driven themes (pywal, matugen).\n\n" +
			"Run `ytm` with no arguments to open the player.",
		Example: "# open the player\nytm\n" +
			"# check mpv, yt-dlp, paths and config\nytm doctor\n" +
			"# validate config.toml\nytm config check",
		Args:              usageArgs(cobra.NoArgs),
		PersistentPreRunE: a.preRun,
		RunE:              a.runTUI,
	}
	root.SetVersionTemplate("ytm {{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &ExitError{Code: ExitUsage, Err: err}
	})

	pf := root.PersistentFlags()
	pf.BoolVar(&a.debug, "debug", false, "verbose logging, also printed to stderr")
	pf.BoolVar(&a.noColor, "no-color", false, "disable colors and styling (also NO_COLOR)")
	pf.StringVar(&a.configDir, "config-dir", "", "config directory (default: platform config dir; env YTM_GO_CONFIG_DIR)")
	pf.StringVar(&a.pprofAddr, "pprof", "", "serve net/http/pprof on `addr` (default 127.0.0.1:6060)")
	pf.Lookup("pprof").NoOptDefVal = "127.0.0.1:6060"
	root.Flags().BoolVar(&a.traceFrames, "trace-frames", false, "log per-frame Update/View timings to the debug log (player UI only)")

	root.AddCommand(newDoctorCmd(a), newConfigCmd(a), newSearchCmd(a), newSetupCmd(a))
	return root
}

// usageArgs marks argument-validation failures as usage errors (exit 2).
func usageArgs(fn cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := fn(cmd, args); err != nil {
			return &ExitError{Code: ExitUsage, Err: err}
		}
		return nil
	}
}

// skipPreRun lists commands that must not touch config or logs.
var skipPreRun = map[string]bool{"completion": true, "man": true, cobra.ShellCompRequestCmd: true, cobra.ShellCompNoDescRequestCmd: true}

func (a *App) preRun(cmd *cobra.Command, _ []string) error {
	for c := cmd; c != nil; c = c.Parent() {
		if skipPreRun[c.Name()] {
			return nil
		}
	}
	if err := a.load(); err != nil {
		return err
	}
	if a.pprofAddr != "" {
		if err := a.startPprof(cmd.Context(), a.pprofAddr); err != nil {
			return &ExitError{Code: ExitFailure, Err: err}
		}
	}
	return nil
}

func (a *App) runTUI(cmd *cobra.Command, _ []string) error {
	tty := a.IO.IsTTY != nil && a.IO.IsTTY(a.IO.Out) && a.IO.InTTY != nil && a.IO.InTTY()
	if !tty {
		return &ExitError{Code: ExitUsage, Err: errors.New("the player needs an interactive terminal; use a subcommand (see `ytm --help`) when piping")}
	}
	if a.debug {
		a.Log.Warn("--debug also logs to stderr, which draws over the player; redirect it: ytm --debug 2>debug.log")
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), shutdownSignals...)
	defer stop()
	return TUIRunner(ctx, app.Options{
		Config:      a.Config,
		ConfigDiags: a.Diags,
		Paths:       a.Paths,
		Theme:       a.Theme,
		NoColor:     a.noColor || a.getenv("NO_COLOR") != "",
		Log:         a.Log,
		TraceFrames: a.traceFrames,
		Env:         a.IO.Env,
	})
}

// TUIRunner starts the player UI; tests replace it.
var TUIRunner = app.Run

// addJSONFlag registers the standard --json flag.
func addJSONFlag(fs *pflag.FlagSet, p *bool) {
	fs.BoolVar(p, "json", false, "print compact JSON (no colors, stable fields)")
}

// errSilent marks failures whose details were already printed.
var errSilent = errors.New("reported")
