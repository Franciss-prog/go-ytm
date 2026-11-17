package ui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
)

// fit truncates s (ANSI-aware, never splitting graphemes) to width cells
// and pads it with spaces to exactly width.
func fit(s string, width int) string {
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

// block fits lines into a width×height rectangle and joins them.
func block(lines []string, width, height int) string {
	out := make([]string, height)
	for i := range height {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out[i] = fit(l, width)
	}
	return strings.Join(out, "\n")
}

// splitFixed splits s into exactly height lines of exactly width cells.
func splitFixed(s string, width, height int) []string {
	src := strings.Split(s, "\n")
	out := make([]string, height)
	for i := range height {
		l := ""
		if i < len(src) {
			l = src[i]
		}
		out[i] = fit(l, width)
	}
	return out
}

// overlayRight replaces the last cells of line with box (width cells).
func overlayRight(line, box string, width, boxWidth int) string {
	if boxWidth >= width {
		return fit(box, width)
	}
	return ansi.Truncate(line, width-boxWidth, "") + fit(box, boxWidth)
}

// after delivers msg once d has passed, like tea.Tick, but gives up when
// ctx ends so no timer goroutine outlives the program (tea.Tick's does).
func after(ctx context.Context, d time.Duration, msg tea.Msg) tea.Cmd {
	return func() tea.Msg {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-t.C:
			return msg
		case <-ctx.Done():
			return nil
		}
	}
}
