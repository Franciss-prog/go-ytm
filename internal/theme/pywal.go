package theme

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"slices"
	"strconv"
	"strings"
)

// Pywal is a parsed pywal colors.json.
type Pywal struct {
	Wallpaper string
	// Alpha is pywal's terminal opacity hint, shown by `ytm theme show`
	// and never used to fake transparency.
	Alpha   string
	Special map[string]color.Color // background, foreground, cursor (if set)
	Colors  [16]color.Color        // nil = missing or invalid
}

type pywalJSON struct {
	Wallpaper string            `json:"wallpaper"`
	Alpha     json.RawMessage   `json:"alpha"`
	Special   map[string]string `json:"special"`
	Colors    map[string]string `json:"colors"`
}

// ErrNoColors means a palette file had neither a background nor a
// foreground (empty, half-written or not a palette).
var ErrNoColors = errors.New("no background or foreground color")

// ParsePywal parses colors.json. Empty or invalid entries count as
// missing (matugen-written files leave most of them empty, spec 08); only
// a file without any usable background or foreground is rejected.
func ParsePywal(data []byte) (Pywal, error) {
	var raw pywalJSON
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&raw); err != nil {
		return Pywal{}, fmt.Errorf("parse pywal colors: %w", err)
	}
	if dec.More() {
		return Pywal{}, errors.New("parse pywal colors: trailing data after JSON object")
	}
	p := Pywal{Wallpaper: raw.Wallpaper, Special: map[string]color.Color{}}
	if len(raw.Alpha) > 0 {
		var s string
		if json.Unmarshal(raw.Alpha, &s) == nil {
			p.Alpha = s
		} else {
			p.Alpha = string(raw.Alpha)
		}
	}
	for k, v := range raw.Special {
		if c, err := parseRGB(v); err == nil {
			p.Special[k] = c
		}
	}
	for k, v := range raw.Colors {
		n, err := strconv.Atoi(strings.TrimPrefix(k, "color"))
		if err != nil || n < 0 || n > 15 || !strings.HasPrefix(k, "color") {
			continue
		}
		if c, err := parseRGB(v); err == nil {
			p.Colors[n] = c
		}
	}
	if p.bg() == nil && p.fg() == nil {
		return Pywal{}, fmt.Errorf("pywal colors: %w", ErrNoColors)
	}
	return p, nil
}

// parseRGB accepts only #rgb/#rrggbb (pywal never writes ansi:/default).
func parseRGB(s string) (color.Color, error) {
	if !strings.HasPrefix(strings.TrimSpace(s), "#") {
		return nil, fmt.Errorf("invalid color %q", s)
	}
	return ParseColor(s)
}

func (p Pywal) bg() color.Color { return firstSet(p.Special["background"], p.Colors[0]) }
func (p Pywal) fg() color.Color {
	return firstSet(p.Special["foreground"], p.Colors[15], p.Colors[7])
}

func firstSet(cs ...color.Color) color.Color {
	for _, c := range cs {
		if c != nil {
			return c
		}
	}
	return nil
}

// lookup resolves a pywal_map value: "color0".."color15", "background",
// "foreground" or "cursor".
func (p Pywal) lookup(ref string) (color.Color, bool) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if n, err := strconv.Atoi(strings.TrimPrefix(ref, "color")); err == nil && strings.HasPrefix(ref, "color") && n >= 0 && n <= 15 {
		return p.Colors[n], true
	}
	switch ref {
	case "background":
		return p.bg(), true
	case "foreground":
		return p.fg(), true
	case "cursor":
		return firstSet(p.Special["cursor"], p.fg()), true
	}
	return nil, false
}

// minAccentChroma is the OKLCH chroma below which a color reads as grey and
// is not an accent candidate (low-saturation palettes fall back to color4).
const minAccentChroma = 0.03

// pickAccent: the most chroma-saturated of color1..color6 with contrast
// ≥ 3 vs bg (ties → color4; greys excluded); else color4; else the most saturated of
// color1..6; else the first of color9..14 (spec 08 mapping table).
func (p Pywal) pickAccent(bg color.Color) color.Color {
	b, bgOK := RGB(bg)
	best, bestC := -1, -1.0
	for i := 1; i <= 6; i++ {
		c, ok := RGB(p.Colors[i])
		ch := chroma(p.Colors[i])
		if !ok || ch < minAccentChroma || (bgOK && Contrast(c, b) < MinAccentContrast) {
			continue
		}
		if ch > bestC+1e-9 || (ch > bestC-1e-9 && i == 4) {
			best, bestC = i, ch
		}
	}
	if best > 0 {
		return p.Colors[best]
	}
	if p.Colors[4] != nil {
		return p.Colors[4]
	}
	for i := 1; i <= 6; i++ {
		if p.Colors[i] != nil && chroma(p.Colors[i]) > bestC {
			best, bestC = i, chroma(p.Colors[i])
		}
	}
	if best > 0 {
		return p.Colors[best]
	}
	return firstSet(p.Colors[9:15]...)
}

// FromPywal maps a pywal palette onto tokens. pywalMap ([theme.pywal_map])
// overrides individual tokens with a colorN/background/foreground/cursor
// reference; unknown tokens or references become notes.
func FromPywal(p Pywal, pywalMap map[string]string) (Theme, []string) {
	t := Theme{Name: "pywal", Source: "pywal"}
	t.Bg, t.Fg = p.bg(), p.fg()
	t.Accent = p.pickAccent(t.Bg)
	t.Muted = p.Colors[8]
	t.Error, t.Success, t.Warn = p.Colors[1], p.Colors[2], p.Colors[3]
	if p.Colors[8] != nil && t.Bg != nil {
		t.Border = Mix(p.Colors[8], t.Bg, 0.40)
	}
	var notes []string
	keys := make([]string, 0, len(pywalMap))
	for k := range pywalMap {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		dst := t.Token(strings.ToLower(k))
		if dst == nil {
			notes = append(notes, fmt.Sprintf("pywal_map.%s: unknown token", k))
			continue
		}
		c, ok := p.lookup(pywalMap[k])
		switch {
		case !ok:
			notes = append(notes, fmt.Sprintf("pywal_map.%s: unknown color %q (want color0..color15, background, foreground, cursor)", k, pywalMap[k]))
		case c == nil:
			notes = append(notes, fmt.Sprintf("pywal_map.%s: %s is empty in colors.json", k, pywalMap[k]))
		default:
			*dst = c
		}
	}
	Complete(&t)
	return t, notes
}
