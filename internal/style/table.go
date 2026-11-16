package style

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Column describes one table column.
type Column struct {
	Header string
	Min    int  // minimum width when shrinking (default: header width)
	Right  bool // right-align (numbers, durations)
	Muted  bool // render cells muted (ids, secondary info)
	Shrink bool // may be truncated with "…" to fit the width
}

// Table renders rows as aligned columns without borders, indented by the
// gutter and limited to width cells (MaxCLIWidth when width <= 0). Shrink
// columns give up space first, widest first. Plain mode renders the same
// layout without escape codes.
func (r *Renderer) Table(cols []Column, rows [][]string, width int) string {
	if width <= 0 {
		width = MaxCLIWidth
	}
	pad := strings.Repeat(" ", Gutter)
	w := make([]int, len(cols))
	for i, c := range cols {
		w[i] = lipgloss.Width(c.Header)
		for _, row := range rows {
			if i < len(row) {
				w[i] = max(w[i], lipgloss.Width(row[i]))
			}
		}
	}
	total := func() int {
		n := Gutter + ColumnGap*(len(cols)-1)
		for _, x := range w {
			n += x
		}
		return n
	}
	for total() > width {
		best := -1
		for i, c := range cols {
			if c.Shrink && w[i] > max(c.Min, lipgloss.Width(c.Header), 1) && (best < 0 || w[i] > w[best]) {
				best = i
			}
		}
		if best < 0 {
			break
		}
		w[best]--
	}

	cell := func(i int, s string) string {
		if lipgloss.Width(s) > w[i] {
			s = ansi.Truncate(s, w[i], "…")
		}
		gap := strings.Repeat(" ", max(0, w[i]-lipgloss.Width(s)))
		if cols[i].Right {
			return gap + s
		}
		if i == len(cols)-1 {
			return s // no trailing padding inside styled spans
		}
		return s + gap
	}
	line := func(cells []string, style func(i int) lipgloss.Style) string {
		var b strings.Builder
		b.WriteString(pad)
		for i := range cols {
			v := ""
			if i < len(cells) {
				v = cells[i]
			}
			if i > 0 {
				b.WriteString(strings.Repeat(" ", ColumnGap))
			}
			b.WriteString(style(i).Render(cell(i, v)))
		}
		return strings.TrimRight(b.String(), " ") + "\n"
	}

	var b strings.Builder
	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = c.Header
	}
	b.WriteString(line(headers, func(int) lipgloss.Style { return r.S.Section }))
	for _, row := range rows {
		b.WriteString(line(row, func(i int) lipgloss.Style {
			if cols[i].Muted {
				return r.S.Muted
			}
			return r.S.Value
		}))
	}
	return b.String()
}

// Heading renders a title line with right-hand meta, like Report's header.
func (r *Renderer) Heading(title, meta string) string {
	pad := strings.Repeat(" ", Gutter)
	if !r.Opts.Mode.Styled {
		if meta != "" {
			return pad + title + " (" + meta + ")\n\n"
		}
		return pad + title + "\n\n"
	}
	return pad + r.S.Title.Render(title) + "  " + r.S.Subtitle.Render(meta) + "\n\n"
}
