package cli

import (
	"errors"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Franciss-Prog/go-ytm/internal/style"
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
	m := a.modeFor(a.IO.Err, false)
	r := a.Renderer(m)
	msg := r.Message(style.StatusInfo, "The player UI is not built yet (milestone M4).") +
		r.Hint("Check your setup with", "ytm doctor")
	_, err := m.Writer(cmd.ErrOrStderr()).Write([]byte(msg))
	return err
}

// addJSONFlag registers the standard --json flag.
func addJSONFlag(fs *pflag.FlagSet, p *bool) {
	fs.BoolVar(p, "json", false, "print compact JSON (no colors, stable fields)")
}

// errSilent marks failures whose details were already printed.
var errSilent = errors.New("reported")
