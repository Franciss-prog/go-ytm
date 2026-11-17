package art

import (
	"fmt"
	"image"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

// Kitty graphics with Unicode placeholders
// (https://sw.kovidgoyal.net/kitty/graphics-protocol/#unicode-placeholders):
// the image is transmitted once with a virtual placement (U=1) of
// cols×rows cells; the screen then shows it wherever cells hold the
// placeholder character with the image id as their foreground color.
// Placeholders are ordinary text cells, so Bubble Tea's diffing renderer
// keeps working. The escapes here are written with tea.Raw.

// QueryID is the image id used for the support query.
const QueryID = 31

// Query asks the terminal whether it supports the graphics protocol: a
// 1×1 RGB image query (a=q) that the terminal answers with APC
// "Gi=31;OK" and never displays.
var Query = ansi.KittyGraphics([]byte("AAAA"), "i=31", "s=1", "v=1", "a=q", "t=d", "f=24")

// QueryOK reports whether a graphics reply answers Query with support.
func QueryOK(o kitty.Options, payload []byte) bool {
	return o.ID == QueryID && string(payload) == "OK"
}

// Transmit returns the escapes that send img (already cropped and sized)
// as image id with a virtual placement of cols×rows cells. Replies are
// suppressed (q=2).
func Transmit(id int, img image.Image, cols, rows int) (string, error) {
	var b strings.Builder
	err := kitty.EncodeGraphics(&b, img, &kitty.Options{
		Action: kitty.TransmitAndPut, Transmission: kitty.Direct, Format: kitty.PNG,
		ID: id, Quiet: 2, Chunk: true, VirtualPlacement: true, Columns: cols, Rows: rows,
	})
	if err != nil {
		return "", fmt.Errorf("art: kitty encode: %w", err)
	}
	return b.String(), nil
}

// Delete returns the escape that frees image id and its placements.
func Delete(id int) string {
	return ansi.KittyGraphics(nil, "a=d", "d=I", fmt.Sprintf("i=%d", id), "q=2")
}

// PlaceholderRow is the text of placeholder row r (cols cells): each cell
// is the placeholder with the row and column diacritics. The caller colors
// it with the image id (IDColor).
func PlaceholderRow(r, cols int) string {
	var b strings.Builder
	for c := range cols {
		b.WriteRune(kitty.Placeholder)
		b.WriteRune(kitty.Diacritic(r))
		b.WriteRune(kitty.Diacritic(c))
	}
	return b.String()
}

// IDColor splits an image id (1..0xFFFFFF) into the 24-bit foreground
// color that addresses it.
func IDColor(id int) (r, g, b uint8) {
	return uint8(id >> 16), uint8(id >> 8), uint8(id)
}
