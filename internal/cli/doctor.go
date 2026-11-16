package cli

import (
	"github.com/charmbracelet/colorprofile"
	"github.com/spf13/cobra"

	"github.com/Franciss-Prog/go-ytm/internal/doctor"
	"github.com/Franciss-Prog/go-ytm/internal/style"
)

// DoctorDeps is overridden in tests.
var DoctorDeps = doctor.RealDeps

func newDoctorCmd(a *App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check mpv, yt-dlp, paths, config and terminal support",
		Long: "Runs a series of checks and prints a report.\n" +
			"Exits with status 1 when any check fails.",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			d := DoctorDeps()
			d.Paths, d.Config, d.Diags = a.Paths, a.Config, a.Diags
			d.Env = a.platformEnv()
			d.YtDlpBinary = a.Config.YtDlp.Binary
			d.Terminal = colorprofile.NoTTY
			if a.IO.IsTTY != nil && a.IO.IsTTY(a.IO.Out) && a.IO.Profile != nil {
				d.Terminal = a.IO.Profile(a.IO.Out)
			}
			rep := doctor.Run(cmd.Context(), d)
			fails, warns := rep.Counts()

			var err error
			if asJSON {
				err = a.PrintJSON(struct {
					style.Report
					Problems int `json:"problems"`
					Warnings int `json:"warnings"`
				}{rep, fails, warns})
			} else {
				m := a.Mode(false)
				err = a.Print(m, a.Renderer(m).Report(rep))
			}
			if err != nil {
				return err
			}
			if fails > 0 {
				return &ExitError{Code: ExitFailure, Err: errSilent, Silent: true}
			}
			return nil
		},
	}
	addJSONFlag(cmd.Flags(), &asJSON)
	return cmd
}
