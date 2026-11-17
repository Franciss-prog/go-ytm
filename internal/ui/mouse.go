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

// tabHit maps x on a tabBar row to a tab index, or -1.
func tabHit(names []string, x int) int {
	at := 0
	for i, n := range names {
		w := ansi.StringWidth(n) + 2
		if x >= at && x < at+w {
			return i
		}
		at += w
	}
	return -1
}

// click routes a click by screen region: header, sidebar, main area, bar.
// Popups own input, so clicks behind them are ignored.
func (m *Model) click(ms tea.Mouse) tea.Cmd {
	right := ms.Button == tea.MouseRight
	if m.modal() || (ms.Button != tea.MouseLeft && !right) {
		return nil
	}
	l := m.layout()
	top := 0
	if l.header {
		top = 1
		if ms.Y == 0 {
			return m.headerClick(ms.X, right)
		}
	}
	if ms.Y >= top && ms.Y < top+l.mainH {
		y := ms.Y - top
		if l.sideW > 0 && ms.X < l.sideW {
			return m.sideClick(ms.X, y, right)
		}
		x, ok := l.mainX(ms.X)
		if !ok {
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
	if !right && ms.Y >= m.barTop {
		return m.barClick(ms.X, ms.Y-m.barTop)
	}
	return nil
}

func (m *Model) headerClick(x int, right bool) tea.Cmd {
	if right {
		return nil
	}
	backW, fwdW := ansi.StringWidth(backLabel), ansi.StringWidth(forwardLabel)
	switch {
	case x >= 1 && x < 1+backW:
		m.goBack()
	case x >= m.width-1-fwdW && x < m.width-1:
		m.goForward()
	}
	return nil
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

// barClick: progress seeks, the like/shuffle/repeat/play glyphs toggle.
func (m *Model) barClick(x, y int) tea.Cmd {
	g := m.geom
	if y == g.progressRow && g.progressW > 0 && x >= g.progressX && x < g.progressX+g.progressW &&
		m.o.Player != nil && m.bar.st.Duration > 0 {
		frac := float64(x-g.progressX) / float64(g.progressW)
		m.bar.scrubbing = false
		if err := m.o.Player.Seek(time.Duration(frac*float64(m.bar.st.Duration)), player.SeekAbsolute); err != nil {
			m.log.Warn("ui: seek failed", "err", err)
		}
		return nil
	}
	for _, b := range g.buttons {
		if y == g.buttonRow && x >= b.x && x < b.x+b.w {
			return m.global(keys.Action{Name: b.action})
		}
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
	if ms.Y == m.barTop+g.volumeRow && ms.X >= g.volumeX && ms.X < g.volumeX+g.volumeW {
		if d < 0 {
			return m.playback(keys.Action{Name: "volume_up"})
		}
		return m.playback(keys.Action{Name: "volume_down"})
	}
	if ms.Y == m.barTop+g.progressRow && g.progressW > 0 && ms.X >= g.progressX && ms.X < g.progressX+g.progressW {
		return m.scrub(-d)
	}
	l := m.layout()
	top := 0
	if l.header {
		top = 1
	}
	if ms.Y < top || ms.Y >= top+l.mainH {
		return nil
	}
	name := "move_down"
	if d < 0 {
		name = "move_up"
	}
	a := keys.Action{Name: name}
	if l.sideW > 0 && ms.X < l.sideW {
		if m.side.state == stateLoaded {
			m.side.list.Action(a)
		}
		return nil
	}
	if _, ok := l.mainX(ms.X); !ok {
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

// mainX maps screen column x to a main-area column; false over the
// sidebar, its separator or the lyrics pane.
func (l layout) mainX(x int) (int, bool) {
	if l.sideW > 0 {
		x -= l.sideW + 1
	}
	w := l.mainW
	if l.lyricsOverlay {
		w -= l.lyricsW + 1
	}
	return x, x >= 0 && x < w
}
