package theme

import (
	"bytes"
	"fmt"
	"image/color"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// MatugenRoles are the Material You roles the shipped template writes
// (contrib/matugen/ytm-go.toml).
var MatugenRoles = []string{
	"primary", "on_primary", "primary_container", "secondary", "tertiary",
	"surface", "surface_container", "surface_container_high", "on_surface",
	"on_surface_variant", "outline", "outline_variant", "error",
}

// Matugen is a parsed matugen-colors.toml.
type Matugen struct {
	Mode  string // "dark" or "light"
	Roles map[string]color.Color
}

type matugenTOML struct {
	Mode   string            `toml:"mode"`
	Colors map[string]string `toml:"colors"`
}

// ParseMatugen parses the file rendered from the ytm-go matugen template.
// Missing or invalid roles are left out (and derived later); a file with
// neither surface nor on_surface is rejected.
func ParseMatugen(data []byte) (Matugen, []string, error) {
	var raw matugenTOML
	if err := toml.NewDecoder(bytes.NewReader(data)).Decode(&raw); err != nil {
		return Matugen{}, nil, fmt.Errorf("parse matugen colors: %w", err)
	}
	m := Matugen{Mode: strings.ToLower(raw.Mode), Roles: map[string]color.Color{}}
	var notes []string
	keys := make([]string, 0, len(raw.Colors))
	for k := range raw.Colors {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		c, err := parseRGB(raw.Colors[k])
		if err != nil {
			notes = append(notes, fmt.Sprintf("colors.%s: %v", k, err))
			continue
		}
		m.Roles[k] = c
	}
	if m.Roles["surface"] == nil && m.Roles["on_surface"] == nil {
		return Matugen{}, notes, fmt.Errorf("matugen colors: %w", ErrNoColors)
	}
	if m.Mode != "dark" && m.Mode != "light" {
		notes = append(notes, fmt.Sprintf("mode = %q: want dark or light; guessing from surface", raw.Mode))
		m.Mode = ""
	}
	return m, notes, nil
}

// FromMatugen maps Material You roles onto tokens (spec 08 table).
func FromMatugen(m Matugen) Theme {
	r := m.Roles
	t := Theme{
		Name: "matugen", Source: "matugen",
		Bg: r["surface"], Fg: r["on_surface"],
		Surface: r["surface_container"], SurfaceAlt: r["surface_container_high"],
		Border: r["outline_variant"], Accent: r["primary"], AccentDim: r["primary_container"],
		Muted: r["on_surface_variant"], SelectedBg: r["primary_container"], SelectedFg: r["on_surface"],
		ProgressFilled: r["primary"], ProgressEmpty: r["outline_variant"],
		LyricsCurrent: r["primary"], LyricsPlayed: r["on_surface_variant"], LyricsUpcoming: r["on_surface"],
		Error: r["error"],
	}
	if t.Accent != nil {
		// Material You has no success/warn roles: hue 140°/85° at primary's L/C.
		t.Success = statusLike(t.Accent, 140, nil)
		t.Warn = statusLike(t.Accent, hueYellow, nil)
	}
	Complete(&t)
	switch m.Mode {
	case "dark":
		t.Dark = true
	case "light":
		t.Dark = false
	}
	return t
}
