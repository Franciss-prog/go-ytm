package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/player"
	"github.com/Franciss-Prog/go-ytm/internal/ui/components"
)

// Scrubbing with the wheel over the progress bar (as the original's
// progress_bar.py: 3 s per tick, seek 0.6 s after the last tick).
const (
	scrubStep  = 3 * time.Second
	scrubDelay = 600 * time.Millisecond
)

// mouseEv is a click inside a pane, in pane coordinates.
type mouseEv struct {
	x, y               int
	right, ctrl, shift bool
}

// mouser is a page that takes clicks in the main area.
type mouser interface {
	Click(ev mouseEv, ctx *Context) (Page, tea.Cmd)
}

// scrubCommitMsg seeks to the scrub position unless more wheel ticks came.
type scrubCommitMsg struct{ seq uint64 }

// tableClick maps a table click to the page action it asks for.
func tableClick(r components.ClickResult) string {
	switch r {
	case components.ClickActivate:
		return "select"
	case components.ClickMenu:
		return "track_actions"
	}
	return ""
}

// listClick maps a list click to the page action it asks for.
func listClick(r components.ClickResult) string { return tableClick(r) }

// tabHit maps x on a tabBar row (" songs · albums · artists ") to a tab
// index, or -1. Each tab owns its name and the space on either side.
func tabHit(names []string, x int) int {
	at := 0
	for i, n := range names {
		w := ansi.StringWidth(n) + 2
		if x >= at && x < at+w {
			return i
		}
		at += w + 1 // the "·" between tabs
	}
	return -1
}

// click routes a click by screen region: frame buttons and tabs, sidebar,
// main page, progress bar. Popups own input, so clicks behind them are
// ignored. The mouse's back/forward side buttons walk the history.
func (m *Model) click(ms tea.Mouse) tea.Cmd {
	switch ms.Button {
	case tea.MouseBackward:
		m.goBack()
		return nil
	case tea.MouseForward:
		m.goForward()
		return nil
	}
	right := ms.Button == tea.MouseRight
	if m.modal() || (ms.Button != tea.MouseLeft && !right) {
		return nil
	}
	g := m.geom
	if !right {
		for _, b := range g.buttons {
			if ms.Y == b.y && ms.X >= b.x && ms.X < b.x+b.w {
				return m.global(keys.Action{Name: b.action})
			}
		}
		if ms.Y == 0 {
			for _, t := range g.tabs {
				if ms.X >= t.x && ms.X < t.x+t.w {
					return m.navigate(t.page)
				}
			}
		}
		if g.progressW > 0 && ms.Y == g.progressY && ms.X >= g.progressX && ms.X < g.progressX+g.progressW {
			m.seekTo(ms.X - g.progressX)
			return nil
		}
	}
	l := m.layout()
	if x, y, ok := l.sideXY(ms.X, ms.Y); ok {
		return m.sideClick(x, y, right)
	}
	x, okX := l.mainX(ms.X)
	y, okY := l.mainY(ms.Y)
	if !okX || !okY {
		return nil
	}
	m.focus = paneMain
	mp, isMouser := m.pages[m.current].(mouser)
	if !isMouser {
		return nil
	}
	ev := mouseEv{x: x, y: y, right: right, ctrl: ms.Mod&tea.ModCtrl != 0, shift: ms.Mod&tea.ModShift != 0}
	p, cmd := mp.Click(ev, m.ctx())
	m.pages[m.current] = p
	return cmd
}

// seekTo seeks to cell x of the progress bar.
func (m *Model) seekTo(x int) {
	g := m.geom
	if m.o.Player == nil || m.bar.st.Duration <= 0 || g.progressW <= 0 {
		return
	}
	frac := float64(x) / float64(max(g.progressW-1, 1))
	m.bar.scrubbing = false
	if err := m.o.Player.Seek(time.Duration(min(frac, 1)*float64(m.bar.st.Duration)), player.SeekAbsolute); err != nil {
		m.log.Warn("ui: seek failed", "err", err)
	}
}

// sideClick: pinned rows navigate; a playlist click selects and opens it
// at once (the original's instant select); right click opens its actions.
func (m *Model) sideClick(x, y int, right bool) tea.Cmd {
	s := m.side
	if y < s.navRows {
		if right {
			return nil
		}
		return m.global(keys.Action{Name: sidebarNav[y].action})
	}
	m.focus = paneSidebar
	if y < s.listY || s.state != stateLoaded {
		return nil
	}
	switch s.list.Click(y-s.listY, right) {
	case components.ClickMoved, components.ClickActivate:
		cmd, _ := m.sidebarAction(keys.Action{Name: "select"})
		return cmd
	case components.ClickMenu:
		cmd, _ := m.sidebarAction(keys.Action{Name: "track_actions"})
		return cmd
	}
	return nil
}

// wheel: over the volume it changes volume, over the progress bar it
// scrubs, over the sidebar or main area it moves that pane's cursor.
func (m *Model) wheel(ms tea.Mouse) tea.Cmd {
	if m.modal() {
		return nil
	}
	d := 0
	switch ms.Button {
	case tea.MouseWheelUp:
		d = -1
	case tea.MouseWheelDown:
		d = 1
	default:
		return nil
	}
	g := m.geom
	if g.volumeW > 0 && ms.Y == g.volumeY && ms.X >= g.volumeX && ms.X < g.volumeX+g.volumeW {
		if d < 0 {
			return m.playback(keys.Action{Name: "volume_up"})
		}
		return m.playback(keys.Action{Name: "volume_down"})
	}
	if g.progressW > 0 && ms.Y == g.progressY && ms.X >= g.progressX && ms.X < g.progressX+g.progressW {
		return m.scrub(-d)
	}
	l := m.layout()
	name := "move_down"
	if d < 0 {
		name = "move_up"
	}
	a := keys.Action{Name: name}
	if _, _, ok := l.sideXY(ms.X, ms.Y); ok {
		if m.side.state == stateLoaded {
			m.side.list.Action(a)
		}
		return nil
	}
	_, okX := l.mainX(ms.X)
	if _, okY := l.mainY(ms.Y); !okX || !okY {
		return nil
	}
	p, cmd, _ := m.pages[m.current].Action(a, m.ctx())
	m.pages[m.current] = p
	return cmd
}

// scrub moves the preview position by dir ticks and (re)arms the commit.
func (m *Model) scrub(dir int) tea.Cmd {
	if m.o.Player == nil || m.bar.st.Duration <= 0 {
		return nil
	}
	if !m.bar.scrubbing {
		m.bar.scrubbing, m.bar.scrubPos = true, m.bar.st.Position
	}
	m.bar.scrubPos = min(max(m.bar.scrubPos+time.Duration(dir)*scrubStep, 0), m.bar.st.Duration)
	m.scrubSeq++
	return after(m.o.Context, scrubDelay, scrubCommitMsg{m.scrubSeq})
}

func (m *Model) commitScrub(seq uint64) {
	if seq != m.scrubSeq || !m.bar.scrubbing {
		return
	}
	m.bar.scrubbing = false
	if err := m.o.Player.Seek(m.bar.scrubPos, player.SeekAbsolute); err != nil {
		m.log.Warn("ui: seek failed", "err", err)
	}
}
