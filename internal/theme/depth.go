package theme

import (
	"image/color"
	"math"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucasb-eyer/go-colorful"
)

// xterm16 are the assumed sRGB values of the 16 ANSI slots: xterm's
// defaults (XTerm-color app-defaults *VT100*colorN, X11 rgb.txt names).
var xterm16 = [16]colorful.Color{
	rgb8(0, 0, 0), rgb8(205, 0, 0), rgb8(0, 205, 0), rgb8(205, 205, 0),
	rgb8(0, 0, 238), rgb8(205, 0, 205), rgb8(0, 205, 205), rgb8(229, 229, 229),
	rgb8(127, 127, 127), rgb8(255, 0, 0), rgb8(0, 255, 0), rgb8(255, 255, 0),
	rgb8(92, 92, 255), rgb8(255, 0, 255), rgb8(0, 255, 255), rgb8(255, 255, 255),
}

func rgb8(r, g, b uint8) colorful.Color {
	return colorful.Color{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255}
}

// xterm256 returns the sRGB value of palette index n in 16..255 (6×6×6
// cube, then 24 grays).
func xterm256(n int) colorful.Color {
	if n >= 232 {
		v := uint8(8 + 10*(n-232))
		return rgb8(v, v, v)
	}
	n -= 16
	lvl := [6]uint8{0, 95, 135, 175, 215, 255}
	return rgb8(lvl[n/36], lvl[(n/6)%6], lvl[n%6])
}

func okDist(a, b colorful.Color) float64 {
	l1, a1, b1 := a.OkLab()
	l2, a2, b2 := b.OkLab()
	return math.Sqrt((l1-l2)*(l1-l2) + (a1-a2)*(a1-a2) + (b1-b2)*(b1-b2))
}

// Nearest256 is the closest xterm-256 cube/gray index (16..255) to c by
// OKLab distance. The first 16 slots are skipped: the terminal owns them.
func Nearest256(c colorful.Color) ansi.IndexedColor {
	best, bestD := 16, math.Inf(1)
	for n := 16; n < 256; n++ {
		if d := okDist(c, xterm256(n)); d < bestD {
			best, bestD = n, d
		}
	}
	return ansi.IndexedColor(best)
}

// Nearest16 is the closest ANSI slot to c, assuming xterm's default values
// (or palette when non-nil, e.g. answered OSC 4 queries).
func Nearest16(c colorful.Color, palette *[16]colorful.Color) ansi.BasicColor {
	p := &xterm16
	if palette != nil {
		p = palette
	}
	best, bestD := 0, math.Inf(1)
	for n, v := range p {
		if d := okDist(c, v); d < bestD {
			best, bestD = n, d
		}
	}
	return ansi.BasicColor(best)
}

// Downsample converts every concrete RGB token to what profile can show:
// TrueColor keeps them, ANSI256 maps to the nearest cube/gray index, ANSI
// to the nearest of the 16 slots. ANSI slots and Default pass through.
// Ascii/NoTTY are handled by the style layer (no colors at all); the
// theme is returned unchanged for them. Done once per Styles build, not
// per frame.
func Downsample(t Theme, p colorprofile.Profile) Theme {
	var conv func(colorful.Color) color.Color
	switch p {
	case colorprofile.ANSI256:
		conv = func(c colorful.Color) color.Color { return Nearest256(c) }
	case colorprofile.ANSI:
		conv = func(c colorful.Color) color.Color { return Nearest16(c, nil) }
	default:
		return t
	}
	for _, n := range TokenNames {
		ptr := t.Token(n)
		if c, ok := RGB(*ptr); ok {
			*ptr = conv(c)
		}
	}
	return t
}

// ParseDepth maps [theme] color_depth onto a profile; "auto" (or "")
// returns detected.
func ParseDepth(s string, detected colorprofile.Profile) colorprofile.Profile {
	switch s {
	case "truecolor":
		return colorprofile.TrueColor
	case "256":
		return colorprofile.ANSI256
	case "16":
		return colorprofile.ANSI
	}
	return detected
}
