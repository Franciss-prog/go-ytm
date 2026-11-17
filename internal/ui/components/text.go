// Package components holds reusable TUI pieces shared by pages
// (docs/specs/05-ui-pages.md): the track table, list helpers and text
// fitting. Components are plain values driven by their page; they never do
// I/O.
package components

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Fit truncates s (ANSI-aware, never splitting graphemes) to width cells,
// ending in "…" when cut, and pads it with spaces to exactly width.
func Fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := ansi.StringWidth(s)
	if w > width {
		s = ansi.Truncate(s, width, "…")
		w = ansi.StringWidth(s)
	}
	if w < width {
		s += strings.Repeat(" ", width-w)
	}
	return s
}

// FitRight is Fit with the text aligned to the right edge.
func FitRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := ansi.StringWidth(s)
	if w > width {
		return Fit(s, width)
	}
	return strings.Repeat(" ", width-w) + s
}

// Block fits lines into a width×height rectangle and joins them.
func Block(lines []string, width, height int) string {
	out := make([]string, height)
	for i := range height {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out[i] = Fit(l, width)
	}
	return strings.Join(out, "\n")
}

// FuzzyMatch reports whether every rune of query appears in s in order
// (case-insensitive subsequence). An empty query matches everything.
func FuzzyMatch(s, query string) bool {
	if query == "" {
		return true
	}
	q := []rune(strings.ToLower(query))
	i := 0
	for _, r := range strings.ToLower(s) {
		if r == q[i] {
			i++
			if i == len(q) {
				return true
			}
		}
	}
	return false
}

// Width is the display width of s in terminal cells (ANSI-aware).
func Width(s string) int { return ansi.StringWidth(s) }
