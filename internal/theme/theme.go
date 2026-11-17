// Package theme defines the semantic palette consumed by all UI code.
//
// Sources (static, pywal, matugen, ansi, file) produce a Theme; Complete
// derives missing tokens and EnforceContrast keeps text readable. See
// docs/specs/08-dynamic-theme-transparency.md.
package theme

import (
	"embed"
	"fmt"
	"hash/fnv"
	"image/color"
	"iter"
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

//go:embed builtin/*.toml
var builtinFS embed.FS

// Builtin returns a built-in static theme by name (case-insensitive).
func Builtin(name string) (Theme, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	data, err := builtinFS.ReadFile("builtin/" + n + ".toml")
	if err != nil || strings.ContainsAny(n, "/\\") {
		return Theme{}, fmt.Errorf("unknown theme %q (available: %s)", name, strings.Join(BuiltinNames(), ", "))
	}
	f, err := ParseFile(data)
	if err != nil {
		return Theme{}, fmt.Errorf("builtin theme %s: %w", n, err)
	}
	return FromFile(f, n, "static")
}

// BuiltinNames lists built-in theme names, sorted.
func BuiltinNames() []string {
	entries, _ := builtinFS.ReadDir("builtin")
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".toml"))
	}
	slices.Sort(names)
	return names
}

// Fallback is the theme used when nothing else resolves.
func Fallback() Theme {
	t, err := Builtin("ytm-dark")
	if err != nil { // unreachable: TestBuiltinsComplete parses every embedded file
		t = Theme{Name: "ytm-dark", Source: "static", Bg: lipgloss.Color("#0f0f0f"), Fg: lipgloss.Color("#f1f1f1"), Accent: lipgloss.Color("#ff4e45")}
		Complete(&t)
	}
	return t
}

// TokenNames lists every token in display order, as written in TOML
// (`[colors]` keys) and printed by `ytm theme show`.
var TokenNames = []string{
	"bg", "fg", "surface", "surface_alt", "border", "accent", "accent_dim", "muted",
	"selected_bg", "selected_fg", "progress_filled", "progress_empty",
	"lyrics_played", "lyrics_current", "lyrics_upcoming", "error", "warn", "success",
}

// Token returns a pointer to the token called name, or nil if there is no
// such token.
func (t *Theme) Token(name string) *color.Color {
	switch name {
	case "bg":
		return &t.Bg
	case "fg":
		return &t.Fg
	case "surface":
		return &t.Surface
	case "surface_alt":
		return &t.SurfaceAlt
	case "border":
		return &t.Border
	case "accent":
		return &t.Accent
	case "accent_dim":
		return &t.AccentDim
	case "muted":
		return &t.Muted
	case "selected_bg":
		return &t.SelectedBg
	case "selected_fg":
		return &t.SelectedFg
	case "progress_filled":
		return &t.ProgressFilled
	case "progress_empty":
		return &t.ProgressEmpty
	case "lyrics_played":
		return &t.LyricsPlayed
	case "lyrics_current":
		return &t.LyricsCurrent
	case "lyrics_upcoming":
		return &t.LyricsUpcoming
	case "error":
		return &t.Error
	case "warn":
		return &t.Warn
	case "success":
		return &t.Success
	}
	return nil
}

// Tokens iterates over (name, color) in TokenNames order.
func (t *Theme) Tokens() iter.Seq2[string, color.Color] {
	return func(yield func(string, color.Color) bool) {
		for _, n := range TokenNames {
			if !yield(n, *t.Token(n)) {
				return
			}
		}
	}
}

// Hash is a stable digest of the palette (name, source and every token),
// used to skip repaints when a tool rewrites identical colors.
func (t *Theme) Hash() uint64 {
	h := fnv.New64a()
	fmt.Fprintf(h, "%s\x00%s\x00%v", t.Name, t.Source, t.Dark)
	for n, c := range t.Tokens() {
		fmt.Fprintf(h, "\x00%s=%s", n, FormatColor(c))
	}
	return h.Sum64()
}

// MarshalTOML renders t in theme.toml form (every token under [colors]),
// with name/source/dark as leading comments. `ytm theme show --toml` and the
// golden mapping tests use it.
func (t *Theme) MarshalTOML() []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# name = %s, source = %s, dark = %v\n[colors]\n", t.Name, t.Source, t.Dark)
	for n, c := range t.Tokens() {
		fmt.Fprintf(&b, "%s = %q\n", n, FormatColor(c))
	}
	return []byte(b.String())
}
