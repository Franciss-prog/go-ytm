package theme

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucasb-eyer/go-colorful"
)

// ParseColor parses a theme color value: "#rgb", "#rrggbb", "ansi:0".."ansi:15"
// or "default" (the terminal's default color). Case and surrounding space
// are ignored.
func ParseColor(s string) (color.Color, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	switch {
	case v == "default":
		return Default, nil
	case strings.HasPrefix(v, "ansi:"):
		n, err := strconv.Atoi(v[len("ansi:"):])
		if err != nil || n < 0 || n > 15 {
			return nil, fmt.Errorf("invalid color %q: ansi index must be 0..15", s)
		}
		return ansi.BasicColor(n), nil
	case strings.HasPrefix(v, "#") && (len(v) == 4 || len(v) == 7):
		if len(v) == 4 {
			v = "#" + strings.Repeat(v[1:2], 2) + strings.Repeat(v[2:3], 2) + strings.Repeat(v[3:4], 2)
		}
		if _, err := strconv.ParseUint(v[1:], 16, 32); err != nil {
			return nil, fmt.Errorf("invalid color %q: not hex", s)
		}
		return lipgloss.Color(v), nil
	}
	return nil, fmt.Errorf("invalid color %q: want #rrggbb, #rgb, ansi:N or default", s)
}

// FormatColor is the inverse of ParseColor ("" for nil).
func FormatColor(c color.Color) string {
	switch v := c.(type) {
	case nil:
		return ""
	case lipgloss.NoColor:
		return "default"
	case ansi.BasicColor:
		return "ansi:" + strconv.Itoa(int(v))
	case ansi.IndexedColor:
		return "ansi256:" + strconv.Itoa(int(v))
	}
	rgb, _ := RGB(c)
	return rgb.Hex()
}

// RGB returns c as a concrete sRGB color. ok is false for colors whose
// real value we cannot know: the terminal default and ANSI palette slots
// (the terminal decides those).
func RGB(c color.Color) (colorful.Color, bool) {
	switch c.(type) {
	case nil, lipgloss.NoColor, ansi.BasicColor, ansi.IndexedColor:
		return colorful.Color{}, false
	}
	col, ok := colorful.MakeColor(c)
	return col, ok
}

// IsDefault reports whether c is the terminal default (or unset).
func IsDefault(c color.Color) bool {
	_, ok := c.(lipgloss.NoColor)
	return c == nil || ok
}

func toColor(c colorful.Color) color.Color { return lipgloss.Color(c.Clamped().Hex()) }

// Luminance is the WCAG 2.x relative luminance of c.
func Luminance(c colorful.Color) float64 {
	r, g, b := c.Clamped().LinearRgb()
	return 0.2126*r + 0.7152*g + 0.0722*b
}

// Contrast is the WCAG 2.x contrast ratio between a and b (1..21).
func Contrast(a, b colorful.Color) float64 {
	la, lb := Luminance(a), Luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Mix blends a toward b by t (0 = a, 1 = b) in OKLab. If either side is
// not a concrete RGB color the result is a (the derivation cannot reason
// about terminal-owned colors).
func Mix(a, b color.Color, t float64) color.Color {
	ra, okA := RGB(a)
	rb, okB := RGB(b)
	if !okA || !okB {
		return a
	}
	return toColor(ra.BlendOkLab(rb, t))
}

// oklch returns c in OKLCH (L 0..1, C ≥ 0, h degrees).
func oklch(c colorful.Color) (l, ch, h float64) { return c.OkLch() }

// fromOklch builds an in-gamut color with lightness l and hue h, keeping
// as much of chroma ch as sRGB allows (chroma is reduced only when the
// color would clip).
func fromOklch(l, ch, h float64) colorful.Color {
	if c := colorful.OkLch(l, ch, h); c.IsValid() {
		return c
	}
	lo, hi := 0.0, ch
	for range 20 {
		mid := (lo + hi) / 2
		if colorful.OkLch(l, mid, h).IsValid() {
			lo = mid
		} else {
			hi = mid
		}
	}
	return colorful.OkLch(l, lo, h).Clamped()
}

// hueDist is the angular distance between two hues in degrees.
func hueDist(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 360)
	return min(d, 360-d)
}
