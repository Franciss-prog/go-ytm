// Package theme defines the semantic palette consumed by all UI code.
//
// M0 ships the token set and two static themes. Sources (pywal, matugen,
// ansi, file), contrast enforcement and live reload arrive in M4/M4b; see
// docs/specs/08-dynamic-theme-transparency.md.
package theme

import (
	"fmt"
	"image/color"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
)

// Default is a sentinel meaning "the terminal's default foreground or
// background" (SGR 39/49). It renders as no color at all.
var Default color.Color = lipgloss.NoColor{}

// Theme is a complete semantic palette. Token values are color.Color so a
// token can be an RGB value, an ANSI palette index (ansi.BasicColor /
// ansi.IndexedColor), or Default.
type Theme struct {
	Name   string
	Source string
	Dark   bool

	Bg, Fg, Surface, SurfaceAlt, Border         color.Color
	Accent, AccentDim, Muted                    color.Color
	SelectedBg, SelectedFg                      color.Color
	ProgressFilled, ProgressEmpty               color.Color
	LyricsPlayed, LyricsCurrent, LyricsUpcoming color.Color
	Error, Warn, Success                        color.Color
}

var builtins = map[string]func() Theme{
	"ytm-dark": func() Theme {
		return Theme{
			Name: "ytm-dark", Source: "static", Dark: true,
			Bg: hex("#0f0f0f"), Fg: hex("#f1f1f1"), Surface: hex("#1a1a1a"), SurfaceAlt: hex("#212121"),
			Border: hex("#3d3d3d"), Accent: hex("#ff4e45"), AccentDim: hex("#8c2b26"), Muted: hex("#9a9a9a"),
			SelectedBg: hex("#2a2a2a"), SelectedFg: hex("#ffffff"),
			ProgressFilled: hex("#ff0000"), ProgressEmpty: hex("#555555"),
			LyricsPlayed: hex("#999999"), LyricsCurrent: hex("#ff4e45"), LyricsUpcoming: hex("#aaaaaa"),
			Error: hex("#ff6b6b"), Warn: hex("#f5c451"), Success: hex("#5fd38d"),
		}
	},
	"ytm-light": func() Theme {
		return Theme{
			Name: "ytm-light", Source: "static", Dark: false,
			Bg: hex("#ffffff"), Fg: hex("#0f0f0f"), Surface: hex("#f2f2f2"), SurfaceAlt: hex("#e8e8e8"),
			Border: hex("#c4c4c4"), Accent: hex("#cc0000"), AccentDim: hex("#f2b8b5"), Muted: hex("#606060"),
			SelectedBg: hex("#e5e5e5"), SelectedFg: hex("#0f0f0f"),
			ProgressFilled: hex("#cc0000"), ProgressEmpty: hex("#c4c4c4"),
			LyricsPlayed: hex("#767676"), LyricsCurrent: hex("#cc0000"), LyricsUpcoming: hex("#3d3d3d"),
			Error: hex("#b3261e"), Warn: hex("#8a5a00"), Success: hex("#1e7b3a"),
		}
	},
}

// Builtin returns a built-in static theme by name.
func Builtin(name string) (Theme, error) {
	f, ok := builtins[strings.ToLower(name)]
	if !ok {
		return Theme{}, fmt.Errorf("unknown theme %q (available: %s)", name, strings.Join(BuiltinNames(), ", "))
	}
	return f(), nil
}

// BuiltinNames lists built-in theme names, sorted.
func BuiltinNames() []string {
	names := make([]string, 0, len(builtins))
	for n := range builtins {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// Fallback is the theme used when nothing else resolves.
func Fallback() Theme { return builtins["ytm-dark"]() }

func hex(s string) color.Color { return lipgloss.Color(s) }
