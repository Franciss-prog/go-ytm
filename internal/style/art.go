package style

import (
	"image"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	colorful "github.com/lucasb-eyer/go-colorful"

	"github.com/Franciss-Prog/go-ytm/internal/art"
	"github.com/Franciss-Prog/go-ytm/internal/theme"
)

// Cover art cells (spec 09). These are the TUI's only colors that do not
// come from theme tokens: pixels, and the kitty image id. Half blocks paint
// backgrounds by design, so their region is exempt from the
// transparent-render check; kitty placeholders are foreground only.

// ArtCapable reports whether half-block art can be drawn at this color
// depth (needs 256 colors or more, and colors at all).
func (u *UI) ArtCapable() bool {
	return !u.Opts.NoColor && u.Opts.Profile != colorprofile.ANSI && u.Opts.Profile != colorprofile.Ascii &&
		u.Opts.Profile != colorprofile.NoTTY
}

// pixel converts an image color for the current depth.
func (u *UI) pixel(c color.Color) color.Color {
	cf, _ := colorful.MakeColor(c)
	if u.Opts.Profile == colorprofile.ANSI256 {
		return theme.Nearest256(cf)
	}
	return c
}

// HalfBlocks draws img as cols×rows cells of "▀": the upper pixel is the
// foreground, the lower one the background. img is scaled to cols×2·rows.
func (u *UI) HalfBlocks(img image.Image, cols, rows int) []string {
	px := art.Scale(img, cols, rows*2)
	out := make([]string, rows)
	n := lipgloss.NewStyle
	for y := range rows {
		var b strings.Builder
		for x := range cols {
			b.WriteString(n().Foreground(u.pixel(px.At(x, 2*y))).Background(u.pixel(px.At(x, 2*y+1))).Render("▀"))
		}
		out[y] = b.String()
	}
	return out
}

// KittyCells draws the placeholder cells of kitty image id, cols×rows.
func (u *UI) KittyCells(id, cols, rows int) []string {
	r, g, b := art.IDColor(id)
	s := lipgloss.NewStyle().Foreground(color.RGBA{r, g, b, 0xff})
	out := make([]string, rows)
	for y := range rows {
		out[y] = s.Render(art.PlaceholderRow(y, cols))
	}
	return out
}
