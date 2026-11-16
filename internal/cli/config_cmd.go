package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Franciss-Prog/go-ytm/internal/config"
	"github.com/Franciss-Prog/go-ytm/internal/style"
)

const configHeader = `# ytm-go configuration. Every key is optional.
# See all keys and defaults with:  ytm config defaults
# Validate this file with:         ytm config check
`

func newConfigCmd(a *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Open config.toml in $EDITOR",
		Long: "Opens config.toml in $VISUAL or $EDITOR, creating it if needed.\n" +
			"Without an editor: opens the config directory (Linux, xdg-open) or\n" +
			"config.toml in its default app (Windows). On macOS set $EDITOR.",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error { return a.openConfig(cmd) },
	}
	cmd.AddCommand(newConfigCheckCmd(a), newConfigPathCmd(a), newConfigDefaultsCmd(a))
	return cmd
}

func (a *App) openConfig(cmd *cobra.Command) error {
	file := a.Paths.ConfigFile()
	if err := ensureConfigFile(file); err != nil {
		return &ExitError{Code: ExitFailure, Err: err}
	}
	editor := a.getenv("VISUAL")
	if editor == "" {
		editor = a.getenv("EDITOR")
	}
	var name string
	var args []string
	switch {
	case editor != "":
		f := strings.Fields(editor)
		name, args = f[0], append(slices.Clone(f[1:]), file)
	case a.IO.GOOS == "linux" || a.IO.GOOS == "freebsd" || a.IO.GOOS == "openbsd":
		name, args = "xdg-open", []string{filepath.Dir(file)}
	case a.IO.GOOS == "windows":
		name, args = "cmd", []string{"/c", "start", "", file}
	default:
		return &ExitError{Code: ExitUsage, Err: fmt.Errorf("$EDITOR is not set; set it or edit %s directly", file)}
	}
	if err := a.RunCommand(cmd.Context(), name, args...); err != nil {
		return &ExitError{Code: ExitFailure, Err: fmt.Errorf("run %s: %w", name, err)}
	}
	return nil
}

func ensureConfigFile(file string) error {
	if _, err := os.Stat(file); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := f.WriteString(configHeader); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func newConfigCheckCmd(a *App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "check [file|-]",
		Short: "Validate config.toml and report problems with line numbers",
		Long:  "Validates config.toml (or the given file, or stdin with -).\nExits with status 1 when there are errors.",
		Args:  usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			file := a.Paths.ConfigFile()
			diags := a.Diags
			if len(args) == 1 {
				var data []byte
				var err error
				if args[0] == "-" {
					file = "<stdin>"
					data, err = io.ReadAll(cmd.InOrStdin())
				} else {
					file = args[0]
					data, err = os.ReadFile(file)
				}
				if err != nil {
					return &ExitError{Code: ExitFailure, Err: err}
				}
				_, diags = config.Load(data, file)
			}
			if diags == nil {
				diags = config.Diagnostics{}
			}

			var err error
			if asJSON {
				err = a.PrintJSON(diags)
			} else {
				m := a.Mode(false)
				err = a.Print(m, a.renderDiags(m, file, diags))
			}
			if err != nil {
				return err
			}
			if diags.Count(config.SevError) > 0 {
				return &ExitError{Code: ExitFailure, Err: errSilent, Silent: true}
			}
			return nil
		},
	}
	addJSONFlag(cmd.Flags(), &asJSON)
	return cmd
}

func (a *App) renderDiags(m style.Mode, file string, diags config.Diagnostics) string {
	r := a.Renderer(m)
	if len(diags) == 0 {
		return r.Message(style.StatusOK, file+": no problems")
	}
	var b strings.Builder
	for _, d := range diags {
		st := style.StatusInfo
		switch d.Severity {
		case config.SevError:
			st = style.StatusFail
		case config.SevWarning:
			st = style.StatusWarn
		}
		loc := d.File
		if d.Line > 0 {
			loc = fmt.Sprintf("%s:%d:%d", d.File, d.Line, d.Col)
		}
		line := r.S.Muted.Render(loc)
		if d.Key != "" {
			line += " " + r.S.Accent.Render(d.Key)
		}
		b.WriteString(r.Message(st, line+" "+d.Message))
	}
	errs, warns := diags.Count(config.SevError), diags.Count(config.SevWarning)
	fmt.Fprintf(&b, "\n%s%s, %s\n", strings.Repeat(" ", style.Gutter), style.Plural(errs, "error"), style.Plural(warns, "warning"))
	return b.String()
}

func newConfigPathCmd(a *App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "path",
		Short: "Print config, data, cache and log locations",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			if asJSON {
				return a.PrintJSON(struct {
					ConfigFile string `json:"config_file"`
					LogFile    string `json:"log_file"`
					Dirs       any    `json:"dirs"`
				}{a.Paths.ConfigFile(), a.Paths.LogFile(), a.Paths})
			}
			_, err := fmt.Fprintln(a.IO.Out, a.Paths.ConfigFile())
			return err
		},
	}
	addJSONFlag(cmd.Flags(), &asJSON)
	return cmd
}

func newConfigDefaultsCmd(a *App) *cobra.Command {
	return &cobra.Command{
		Use:   "defaults",
		Short: "Print the default config.toml",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			b, err := config.DefaultsTOML()
			if err != nil {
				return err
			}
			_, err = a.IO.Out.Write(b)
			return err
		},
	}
}
