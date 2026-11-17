package ui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Franciss-Prog/go-ytm/internal/style"
)

// Severity of a toast.
type Severity int

// Severities.
const (
	SevInfo Severity = iota
	SevSuccess
	SevWarn
	SevError
)

// Toast is a short non-blocking notice. Send one to the program (or
// return it from a Cmd) to show it.
type Toast struct {
	Severity Severity
	Text     string
	id       int
}

// Toast limits (spec 05).
const (
	maxToasts   = 3
	toastTTL    = 4 * time.Second
	toastWidth  = 44
	toastMargin = 1
)

type toastExpiredMsg struct{ id int }

type toasts struct {
	ctx    context.Context
	items  []Toast
	nextID int
}

// push adds t (dropping the oldest beyond maxToasts) and arms its expiry.
func (ts *toasts) push(t Toast) tea.Cmd {
	ts.nextID++
	t.id = ts.nextID
	ts.items = append(ts.items, t)
	if len(ts.items) > maxToasts {
		ts.items = ts.items[len(ts.items)-maxToasts:]
	}
	return after(ts.ctx, toastTTL, toastExpiredMsg{t.id})
}

// armAll arms expiry for toasts queued before the program started.
func (ts *toasts) armAll() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(ts.items))
	for _, t := range ts.items {
		cmds = append(cmds, after(ts.ctx, toastTTL, toastExpiredMsg{t.id}))
	}
	return tea.Batch(cmds...)
}

func (ts *toasts) expire(id int) {
	for i, t := range ts.items {
		if t.id == id {
			ts.items = append(ts.items[:i], ts.items[i+1:]...)
			return
		}
	}
}

// overlay draws the toasts bottom-right over rows (newest lowest).
func (ts *toasts) overlay(rows []string, width int, st *style.UI) {
	if len(ts.items) == 0 || width < 20 {
		return
	}
	w := min(toastWidth, width-toastMargin)
	inner := w - 4 // border + padding
	bottom := len(rows)
	for i := len(ts.items) - 1; i >= 0 && bottom > 0; i-- {
		t := ts.items[i]
		box := toastStyle(t.Severity, st).Width(w).Render(ansi.Truncate(t.Text, inner*2, "…"))
		lines := strings.Split(box, "\n")
		if len(lines) > bottom {
			break
		}
		start := bottom - len(lines)
		for j, l := range lines {
			rows[start+j] = overlayRight(rows[start+j], l+strings.Repeat(" ", toastMargin), width, w+toastMargin)
		}
		bottom = start
	}
}

func toastStyle(s Severity, st *style.UI) lipgloss.Style {
	switch s {
	case SevSuccess:
		return st.ToastSuccess
	case SevWarn:
		return st.ToastWarn
	case SevError:
		return st.ToastError
	default:
		return st.ToastInfo
	}
}
