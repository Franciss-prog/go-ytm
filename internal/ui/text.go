package ui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/Franciss-Prog/go-ytm/internal/ui/components"
)

// fit and block are the shared text helpers (see components.Fit/Block).
var (
	fit   = components.Fit
	block = components.Block
)

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
