package theme

import (
	"fmt"
	"image/color"
	"math"

	"github.com/lucasb-eyer/go-colorful"
)

// Contrast targets (spec 08, "Contrast safety").
const (
	MinTextContrast   = 4.5 // fg on bg
	MinAccentContrast = 3.0 // accent, status, muted, selected fg
)

// Adjustment records one token changed for contrast.
type Adjustment struct {
	Token    string  `json:"token"`
	From     string  `json:"from"`
	To       string  `json:"to"`
	Against  string  `json:"against"`
	Before   float64 `json:"before"`
	After    float64 `json:"after"`
	Fallback bool    `json:"fallback,omitempty"` // fell back to black/white
}

func (a Adjustment) String() string {
	s := fmt.Sprintf("%s %s → %s (%.2f → %.2f vs %s)", a.Token, a.From, a.To, a.Before, a.After, a.Against)
	if a.Fallback {
		s += ", black/white fallback"
	}
	return s
}

// EnforceContrast raises the contrast of t's text tokens against bg —
// t.Bg normally, the assumed terminal background in transparent mode.
// Tokens that are not concrete RGB colors, or a bg that is not, are left
// alone (we cannot know what the terminal will show). Bg is never
// modified.
func EnforceContrast(t Theme, bg color.Color) (Theme, []Adjustment) {
	b, ok := RGB(bg)
	if !ok {
		return t, nil
	}
	var adj []Adjustment
	fix := func(name string, p *color.Color, against colorful.Color, target float64) {
		c, ok := RGB(*p)
		if !ok {
			return
		}
		out, fb := raiseContrast(c, against, target)
		if out == c {
			return
		}
		adj = append(adj, Adjustment{
			Token: name, From: c.Hex(), To: out.Hex(), Against: against.Hex(),
			Before: Contrast(c, against), After: Contrast(out, against), Fallback: fb,
		})
		*p = toColor(out)
	}
	fix("fg", &t.Fg, b, MinTextContrast)
	for _, tok := range []struct {
		name string
		p    *color.Color
	}{
		{"accent", &t.Accent},
		{"lyrics_current", &t.LyricsCurrent},
		{"muted", &t.Muted},
		{"error", &t.Error},
		{"warn", &t.Warn},
		{"success", &t.Success},
	} {
		fix(tok.name, tok.p, b, MinAccentContrast)
	}
	// selected_fg sits on selected_bg wherever a fill is drawn.
	if sb, ok := RGB(t.SelectedBg); ok {
		fix("selected_fg", &t.SelectedFg, sb, MinAccentContrast)
	} else {
		fix("selected_fg", &t.SelectedFg, b, MinAccentContrast)
	}
	return t, adj
}

// raiseContrast returns c unchanged when it already meets target against
// bg. Otherwise it moves OKLCH lightness toward whichever extreme (white or
// black) contrasts more with bg, keeping hue and as much chroma as the
// gamut allows, and binary-searches the smallest move that meets target
// (24 iterations). If even the extreme falls short it returns pure black or
// white (fallback = true).
func raiseContrast(c, bg colorful.Color, target float64) (out colorful.Color, fallback bool) {
	if Contrast(c, bg) >= target {
		return c, false
	}
	white, black := colorful.Color{R: 1, G: 1, B: 1}, colorful.Color{}
	l0, ch, h := oklch(c)
	lighter := Contrast(white, bg) >= Contrast(black, bg)
	end := 0.0
	if lighter {
		end = 1.0
	}
	if Contrast(quant(fromOklch(end, ch, h)), bg) < target {
		if lighter {
			return white, true
		}
		return black, true
	}
	// Invariant: hi meets target, lo does not.
	lo, hi := l0, end
	for range 24 {
		mid := (lo + hi) / 2
		if Contrast(quant(fromOklch(mid, ch, h)), bg) >= target {
			hi = mid
		} else {
			lo = mid
		}
	}
	return quant(fromOklch(hi, ch, h)), false
}

// quant rounds c to 8 bits per channel, the precision tokens are stored at,
// so contrast is checked on the color the terminal will actually get.
func quant(c colorful.Color) colorful.Color {
	c = c.Clamped()
	r := func(v float64) float64 { return math.Round(v*255) / 255 }
	return colorful.Color{R: r(c.R), G: r(c.G), B: r(c.B)}
}
