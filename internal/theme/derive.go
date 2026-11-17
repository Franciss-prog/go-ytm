package theme

import (
	"image/color"

	"github.com/charmbracelet/x/ansi"
)

// darkThreshold is the WCAG luminance at which white and black give the
// same contrast ((1+0.05)/(L+0.05) == (L+0.05)/0.05). Below it a
// background is "dark": light text contrasts better.
const darkThreshold = 0.17913

// IsDarkBg reports whether text on bg should be light. Colors we cannot
// know (terminal default, ANSI slots) count as dark, the common case.
func IsDarkBg(bg color.Color) bool {
	c, ok := RGB(bg)
	return !ok || Luminance(c) < darkThreshold
}

// Status hues in OKLCH degrees.
const (
	hueRed    = 25.0
	hueYellow = 85.0
	hueGreen  = 145.0
	hueBlue   = 250.0
)

// statusColor is a red/yellow/green at a lightness that reads on bg (spec
// 08: "hue-shifted to the bg's lightness band").
func statusColor(bg color.Color, hue float64, fallback ansi.BasicColor) color.Color {
	if _, ok := RGB(bg); !ok {
		return fallback
	}
	l := 0.72
	if !IsDarkBg(bg) {
		l = 0.52
	}
	return toColor(fromOklch(l, 0.16, hue))
}

// statusLike is a status color at a reference color's L and C (matugen
// success/warn: "hue 140°/85° at primary's L/C").
func statusLike(ref color.Color, hue float64, fallback color.Color) color.Color {
	r, ok := RGB(ref)
	if !ok {
		return fallback
	}
	l, c, _ := oklch(r)
	return toColor(fromOklch(l, max(c, 0.08), hue))
}

// Complete fills every nil token of t from bg, fg and accent using the
// spec 08 derivation rules (mixing in OKLab), and sets Dark from bg.
// Bg and Fg default to the terminal's colors and Accent to ANSI blue when
// unset.
func Complete(t *Theme) {
	set := func(p *color.Color, v color.Color) {
		if *p == nil {
			*p = v
		}
	}
	set(&t.Bg, Default)
	set(&t.Fg, Default)
	set(&t.Accent, statusColor(t.Bg, hueBlue, ansi.Blue))
	t.Dark = IsDarkBg(t.Bg)

	set(&t.Surface, Mix(t.Bg, t.Fg, 0.06))
	set(&t.SurfaceAlt, Mix(t.Bg, t.Fg, 0.10))
	set(&t.Border, mixOr(t.Bg, t.Fg, 0.25, ansi.BrightBlack))
	set(&t.AccentDim, Mix(t.Accent, t.Bg, 0.45))
	set(&t.Muted, mixOr(t.Fg, t.Bg, 0.45, ansi.BrightBlack))
	set(&t.SelectedBg, Mix(t.Bg, t.Accent, 0.20))
	set(&t.SelectedFg, t.Fg)
	set(&t.ProgressFilled, t.Accent)
	set(&t.ProgressEmpty, t.Border)
	set(&t.LyricsPlayed, t.Muted)
	set(&t.LyricsCurrent, t.Accent)
	set(&t.LyricsUpcoming, t.Fg)
	set(&t.Error, statusColor(t.Bg, hueRed, ansi.Red))
	set(&t.Warn, statusColor(t.Bg, hueYellow, ansi.Yellow))
	set(&t.Success, statusColor(t.Bg, hueGreen, ansi.Green))
}

// mixOr mixes when both ends are concrete, else returns fallback (an ANSI
// slot is better than a token equal to bg or fg).
func mixOr(a, b color.Color, t float64, fallback color.Color) color.Color {
	_, okA := RGB(a)
	_, okB := RGB(b)
	if !okA || !okB {
		return fallback
	}
	return Mix(a, b, t)
}

// chroma is OKLCH C of c (0 for unknown colors).
func chroma(c color.Color) float64 {
	r, ok := RGB(c)
	if !ok {
		return 0
	}
	_, ch, _ := oklch(r)
	return ch
}
