package app

import (
	"fmt"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"
)

// tracer wraps the root model and logs per-frame Update/View timings
// (`ytm --trace-frames`). The first View also logs time since process
// start, the cold-start measurement.
type tracer struct {
	inner   tea.Model
	log     *slog.Logger
	start   time.Time
	frames  uint64
	lastUpd time.Duration
	lastMsg string
}

func (t *tracer) Init() tea.Cmd { return t.inner.Init() }

func (t *tracer) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	s := time.Now()
	m, cmd := t.inner.Update(msg)
	t.inner = m
	t.lastUpd = time.Since(s)
	t.lastMsg = fmt.Sprintf("%T", msg)
	return t, cmd
}

func (t *tracer) View() tea.View {
	s := time.Now()
	v := t.inner.View()
	d := time.Since(s)
	t.frames++
	if t.frames == 1 {
		t.log.Debug("frame: first view", "since_start", time.Since(t.start), "view", d)
	}
	t.log.Debug("frame", "n", t.frames, "msg", t.lastMsg, "update", t.lastUpd, "view", d)
	return v
}

// Inner returns the wrapped model (tests).
func (t *tracer) Inner() tea.Model { return t.inner }
