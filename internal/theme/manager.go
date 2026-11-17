package theme

import (
	"context"
	"image/color"
	"log/slog"
	"sync"

	"github.com/Franciss-Prog/go-ytm/internal/clock"
)

// Manager owns theme resolution and the file watcher for the running app.
// All work happens on its Run goroutine; Set, SetTerminalBg and Reload
// never block, so the UI can call them from Update.
type Manager struct {
	send  func(Result)
	clock clock.Clock
	log   *slog.Logger

	mu      sync.Mutex
	spec    Spec
	pending bool
	wake    chan struct{}
	reload  chan struct{}
}

// NewManager returns a manager for spec. last is the result already
// applied (resolved synchronously at startup so the first frame is
// themed); send delivers later results (tea.Program.Send).
func NewManager(spec Spec, send func(Result), clk clock.Clock, log *slog.Logger) *Manager {
	if clk == nil {
		clk = clock.Real{}
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Manager{
		send: send, clock: clk, log: log, spec: spec,
		wake: make(chan struct{}, 1), reload: make(chan struct{}, 1),
	}
}

// Spec returns the current spec.
func (m *Manager) Spec() Spec {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.spec
}

// Set replaces the spec; the theme is re-resolved and always sent
// (CauseSettings), and watching restarts on the new paths. Calls coalesce:
// only the latest spec is applied.
func (m *Manager) Set(s Spec) {
	m.mu.Lock()
	s.TerminalBg = m.spec.TerminalBg
	s.ReadFile = m.spec.ReadFile
	m.spec, m.pending = s, true
	m.mu.Unlock()
	m.poke(m.wake)
}

// SetTerminalBg records the terminal's background (OSC 11 answer) for
// contrast in transparent mode and re-resolves if it matters.
func (m *Manager) SetTerminalBg(c color.Color) {
	m.mu.Lock()
	m.spec.TerminalBg = c
	matters := m.spec.Transparent && m.spec.EnforceContrast
	m.pending = m.pending || matters
	m.mu.Unlock()
	if matters {
		m.poke(m.wake)
	}
}

// Reload forces a re-resolve that is sent even when unchanged.
func (m *Manager) Reload() { m.poke(m.reload) }

func (*Manager) poke(c chan struct{}) {
	select {
	case c <- struct{}{}:
	default:
	}
}

// Run serves until ctx ends. initial is the result applied at startup.
func (m *Manager) Run(ctx context.Context, initial Result) {
	var (
		wctx    context.Context
		wcancel context.CancelFunc = func() {}
		wdone   chan struct{}
		w       *Watcher
	)
	stop := func() {
		wcancel()
		if wdone != nil {
			<-wdone
		}
		w, wdone = nil, nil
	}
	start := func(spec Spec, last Result) {
		if !spec.Watch {
			return
		}
		w = NewWatcher(spec.WatchPaths(), func() Result { return Resolve(spec) }, m.send, last)
		w.Clock, w.Log = m.clock, m.log
		wctx, wcancel = context.WithCancel(ctx)
		wdone = make(chan struct{})
		ww := w
		go func() {
			defer close(wdone)
			if err := ww.Run(wctx); err != nil {
				m.log.Warn("theme: live reload unavailable", "err", err)
			}
		}()
	}
	defer stop()
	start(m.Spec(), initial)

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
			m.mu.Lock()
			spec, ok := m.spec, m.pending
			m.pending = false
			m.mu.Unlock()
			if !ok {
				continue
			}
			stop()
			r := Resolve(spec)
			r.Cause = CauseSettings
			m.send(r)
			start(spec, r)
		case <-m.reload:
			if w != nil {
				w.Trigger()
				continue
			}
			r := Resolve(m.Spec())
			r.Cause = CauseReload
			m.send(r)
		}
	}
}
