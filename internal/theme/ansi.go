package theme

import (
	"github.com/charmbracelet/x/ansi"
)

// ANSITheme is the zero-file theme: every token is an ANSI palette slot or
// the terminal default, so palette reloads by pywal escape sequences or the
// terminal recolor the app instantly (spec 08, "ansi"). accent is the
// [theme] ansi_accent slot (0..15). The selection is drawn in reverse
// video (SelectedBg and SelectedFg are Default; see IsReverseSelection).
func ANSITheme(accent int) Theme {
	if accent < 0 || accent > 15 {
		accent = int(ansi.Blue)
	}
	a := ansi.BasicColor(accent)
	return Theme{
		Name: "ansi", Source: "ansi", Dark: true,
		Bg: Default, Fg: Default, Surface: Default, SurfaceAlt: Default,
		Border: ansi.BrightBlack, Accent: a, AccentDim: a, Muted: ansi.BrightBlack,
		SelectedBg: Default, SelectedFg: Default,
		ProgressFilled: a, ProgressEmpty: ansi.BrightBlack,
		LyricsPlayed: ansi.BrightBlack, LyricsCurrent: a, LyricsUpcoming: Default,
		Error: ansi.Red, Warn: ansi.Yellow, Success: ansi.Green,
	}
}

// IsReverseSelection reports whether the selection cannot be a fill (its
// background is the terminal default) and must be drawn in reverse video.
func (t *Theme) IsReverseSelection() bool { return IsDefault(t.SelectedBg) }
