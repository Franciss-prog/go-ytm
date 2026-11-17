package cli

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/theme"
)

func newThemeCmd(a *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "theme",
		Short: "Inspect the active theme",
		Args:  usageArgs(cobra.NoArgs),
	}
	cmd.AddCommand(newThemeShowCmd(a))
	return cmd
}

// themeJSON is the `ytm theme show --json` shape (spec 06).
type themeJSON struct {
	Source      string             `json:"source"`
	Name        string             `json:"name"`
	Dark        bool               `json:"dark"`
	Transparent bool               `json:"transparent"`
	Path        string             `json:"path,omitempty"`
	Error       string             `json:"error,omitempty"`
	Wallpaper   string             `json:"wallpaper,omitempty"`
	Alpha       string             `json:"alpha,omitempty"`
	Tokens      map[string]string  `json:"tokens"`
	Against     string             `json:"contrast_against,omitempty"`
	Adjustments []theme.Adjustment `json:"adjustments"`
	Notes       []string           `json:"notes"`
}

func newThemeShowCmd(a *App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the resolved palette, its source and contrast adjustments",
		Long: "Resolves the theme exactly as the player would ([theme] in config.toml,\n" +
			"theme.toml overrides, contrast enforcement) and prints every token.",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			r := a.ThemeResult
			if asJSON {
				return a.PrintJSON(themeToJSON(r, a.Config.Theme.Transparent))
			}
			m := a.Mode(false)
			return a.Print(m, a.renderTheme(r, m.Styled))
		},
	}
	addJSONFlag(cmd.Flags(), &asJSON)
	return cmd
}

func themeToJSON(r theme.Result, transparent bool) themeJSON {
	t := r.Theme
	out := themeJSON{
		Source: t.Source, Name: t.Name, Dark: t.Dark, Transparent: transparent, Path: r.Path,
		Wallpaper: r.Wallpaper, Alpha: r.Alpha, Tokens: map[string]string{}, Against: r.Against,
		Adjustments: r.Adjustments, Notes: r.Notes,
	}
	if out.Adjustments == nil {
		out.Adjustments = []theme.Adjustment{}
	}
	if out.Notes == nil {
		out.Notes = []string{}
	}
	if r.Err != nil {
		out.Error = r.Err.Error()
	}
	for n, c := range t.Tokens() {
		out.Tokens[n] = theme.FormatColor(c)
	}
	return out
}

func (a *App) renderTheme(r theme.Result, styled bool) string {
	rd := a.Renderer(a.Mode(false))
	s := rd.S
	t := r.Theme
	var b strings.Builder
	pad := strings.Repeat(" ", 1)
	line := func(label, value string) {
		fmt.Fprintf(&b, "%s  %s %s\n", pad, s.Label.Render(fmt.Sprintf("%-12s", label)), value)
	}
	fmt.Fprintf(&b, "%s%s\n\n", pad, s.Title.Render("Theme"))
	line("source", s.Value.Render(t.Source))
	line("name", s.Value.Render(t.Name))
	mode := "light"
	if t.Dark {
		mode = "dark"
	}
	line("mode", s.Value.Render(mode))
	if a.Config.Theme.Transparent {
		line("transparent", s.Value.Render("yes")+s.Muted.Render("  (the terminal's own opacity shows through)"))
	}
	if r.Path != "" {
		line("palette", s.Value.Render(r.Path))
	}
	if r.Wallpaper != "" {
		line("wallpaper", s.Muted.Render(r.Wallpaper))
	}
	if r.Alpha != "" {
		line("alpha", s.Muted.Render(r.Alpha+" (informational)"))
	}
	if r.Err != nil {
		fmt.Fprintf(&b, "%s  %s %s\n", pad, rd.Icon(style.StatusWarn), s.Warn.Render(r.Err.Error()+"; using the fallback"))
	}

	fmt.Fprintf(&b, "\n%s%s\n", pad, s.Section.Render("Tokens"))
	for n, c := range t.Tokens() {
		sw := "  "
		if styled && !theme.IsDefault(c) {
			sw = lipgloss.NewStyle().Foreground(c).Render("██")
		}
		fmt.Fprintf(&b, "%s  %s %s %s\n", pad, sw, s.Label.Render(fmt.Sprintf("%-16s", n)), s.Value.Render(theme.FormatColor(c)))
	}

	if r.Against != "" || len(r.Adjustments) > 0 {
		fmt.Fprintf(&b, "\n%s%s\n", pad, s.Section.Render("Contrast"))
		if len(r.Adjustments) == 0 {
			fmt.Fprintf(&b, "%s  %s\n", pad, s.Muted.Render("all text meets its ratio against "+r.Against))
		}
		for _, adj := range r.Adjustments {
			fmt.Fprintf(&b, "%s  %s\n", pad, s.Value.Render(adj.String()))
		}
	}
	if len(r.Notes) > 0 {
		fmt.Fprintf(&b, "\n%s%s\n", pad, s.Section.Render("Notes"))
		for _, n := range r.Notes {
			fmt.Fprintf(&b, "%s  %s\n", pad, s.Warn.Render(n))
		}
	}
	return b.String()
}
