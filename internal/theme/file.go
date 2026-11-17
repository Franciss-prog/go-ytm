package theme

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// legacyAliases maps the original ytm-player [colors] keys onto tokens
// (spec 04, "theme.toml").
var legacyAliases = map[string]string{
	"selected_item":   "selected_bg",
	"playback_bar_bg": "surface",
	"active_tab":      "fg",
	"inactive_tab":    "muted",
}

// File is a parsed theme.toml: a (possibly partial) palette plus overrides
// applied on top of any source.
type File struct {
	Colors    Theme
	Overrides Theme
	// Notes are non-fatal problems (bad values, unknown keys, aliases).
	Notes []string
}

type fileTOML struct {
	Colors    map[string]string `toml:"colors"`
	Overrides map[string]string `toml:"overrides"`
}

// ParseFile parses theme.toml content. Invalid or unknown entries become
// Notes and are skipped; only a syntax error fails.
func ParseFile(data []byte) (File, error) {
	var raw fileTOML
	dec := toml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	if err := dec.Decode(&raw); err != nil {
		return File{}, fmt.Errorf("parse theme file: %w", err)
	}
	var f File
	f.Notes = append(f.Notes, fill(&f.Colors, "colors", raw.Colors)...)
	f.Notes = append(f.Notes, fill(&f.Overrides, "overrides", raw.Overrides)...)
	return f, nil
}

func fill(t *Theme, table string, m map[string]string) []string {
	var notes []string
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		name := strings.ToLower(k)
		if alias, ok := legacyAliases[name]; ok {
			notes = append(notes, fmt.Sprintf("%s.%s is deprecated; use %s", table, k, alias))
			name = alias
		}
		p := t.Token(name)
		if p == nil {
			notes = append(notes, fmt.Sprintf("%s.%s: unknown token", table, k))
			continue
		}
		c, err := ParseColor(m[k])
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s.%s: %v", table, k, err))
			continue
		}
		*p = c
	}
	return notes
}

// FromFile builds a complete theme from a theme.toml [colors] table. bg,
// fg and accent are required; everything else is derived.
func FromFile(f File, name, source string) (Theme, error) {
	t := f.Colors
	var missing []string
	for _, k := range []string{"bg", "fg", "accent"} {
		if *t.Token(k) == nil {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return Theme{}, fmt.Errorf("theme file: [colors] needs %s", strings.Join(missing, ", "))
	}
	t.Name, t.Source = name, source
	Complete(&t)
	return t, nil
}

// ApplyOverrides sets every non-nil token of o on t.
func ApplyOverrides(t *Theme, o Theme) {
	for _, n := range TokenNames {
		if c := *o.Token(n); c != nil {
			*t.Token(n) = c
		}
	}
	t.Dark = IsDarkBg(t.Bg)
}
