package style

import (
	"image/color"

	"charm.land/fang/v2"
	"charm.land/lipgloss/v2"

	"github.com/Franciss-Prog/go-ytm/internal/theme"
)

// Spacing constants shared by CLI and TUI.
const (
	Gutter      = 1 // left margin for all CLI output
	Indent      = 2 // indentation of items under a section
	ColumnGap   = 2 // gap between table columns
	MaxCLIWidth = 100
)

// Options configure a Renderer.
type Options struct {
	Theme       theme.Theme
	Transparent bool // never emit background colors ([theme] transparent)
	Icons       IconSet
	Mode        Mode
}

// Styles is the complete set of CLI styles derived from a theme.
type Styles struct {
	Title    lipgloss.Style // report/command title
	Subtitle lipgloss.Style // right-aligned meta on the title line
	Section  lipgloss.Style
	Label    lipgloss.Style
	Value    lipgloss.Style
	Muted    lipgloss.Style
	Accent   lipgloss.Style
	OK       lipgloss.Style
	Warn     lipgloss.Style
	Fail     lipgloss.Style
	Info     lipgloss.Style
	Code     lipgloss.Style
	Card     lipgloss.Style // bordered box
}

// Renderer renders styled (or plain) CLI output.
type Renderer struct {
	Opts Options
	S    Styles
}

// New builds a Renderer. When opts.Mode.Styled is false every style is
// empty so output is plain text.
func New(opts Options) *Renderer {
	if opts.Icons.Name == "" {
		opts.Icons = UnicodeIcons
	}
	return &Renderer{Opts: opts, S: buildStyles(opts)}
}

func buildStyles(o Options) Styles {
	if !o.Mode.Styled {
		p := lipgloss.NewStyle()
		return Styles{p, p, p, p, p, p, p, p, p, p, p, p, p}
	}
	t := o.Theme
	fg := func(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	s := Styles{
		Subtitle: fg(t.Muted),
		Section:  fg(t.Accent).Bold(true),
		Label:    fg(t.Fg),
		Value:    fg(t.Fg),
		Muted:    fg(t.Muted),
		Accent:   fg(t.Accent),
		OK:       fg(t.Success).Bold(true),
		Warn:     fg(t.Warn).Bold(true),
		Fail:     fg(t.Error).Bold(true),
		Info:     fg(t.Muted),
		Code:     fg(t.Accent),
		Card: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.Border).
			Padding(0, 1),
	}
	if o.Transparent {
		// Spec 08: no background fills anywhere in transparent mode.
		s.Title = fg(t.Accent).Bold(true)
	} else {
		s.Title = lipgloss.NewStyle().Foreground(t.Bg).Background(t.Accent).Bold(true).Padding(0, 1)
		s.Code = s.Code.Background(t.Surface).Padding(0, 1)
	}
	return s
}

// Icon returns the glyph for a status, styled.
func (r *Renderer) Icon(st Status) string {
	ic := r.Opts.Icons
	switch st {
	case StatusOK:
		return r.S.OK.Render(ic.OK)
	case StatusWarn:
		return r.S.Warn.Render(ic.Warn)
	case StatusFail:
		return r.S.Fail.Render(ic.Fail)
	default:
		return r.S.Info.Render(ic.Info)
	}
}

// FangColorScheme maps the theme onto fang's help/error color scheme so
// `--help` and usage errors match the rest of the CLI.
func FangColorScheme(t theme.Theme, transparent bool) fang.ColorSchemeFunc {
	return func(lipgloss.LightDarkFunc) fang.ColorScheme {
		codeBg, errBg, errFg := t.Surface, t.Error, t.Bg
		if transparent {
			codeBg, errBg, errFg = lipgloss.NoColor{}, lipgloss.NoColor{}, t.Error
		}
		return fang.ColorScheme{
			Base:           t.Fg,
			Title:          t.Accent,
			Description:    t.Fg,
			Codeblock:      codeBg,
			Program:        t.Accent,
			DimmedArgument: t.Muted,
			Comment:        t.Muted,
			Flag:           t.Success,
			FlagDefault:    t.Muted,
			Command:        t.Accent,
			QuotedString:   t.Warn,
			Argument:       t.Fg,
			Help:           t.Muted,
			Dash:           t.Muted,
			ErrorHeader:    [2]color.Color{errFg, errBg},
			ErrorDetails:   t.Error,
		}
	}
}
