package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/player"
)

// Screen geometry of newPB (100×30, sidebar shown): tab bar row 0, rule
// row 1, panels from row 2 with a one-cell border; the sidebar panel is
// columns 0–31, the main panel 32–99, so the page starts at (33, 3).
// Sidebar content cell (x, y) is at screen (x+1, mainY0+y).
const (
	mainX0 = 33
	mainY0 = 3
)

func (h pbHarness) click(x, y int, b tea.MouseButton, mod tea.KeyMod) {
	h.m.View() // geometry of the current frame
	h.send(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: b, Mod: mod}))
}

func (h pbHarness) left(x, y int) { h.click(x, y, tea.MouseLeft, 0) }

func (h pbHarness) wheel(x, y int, b tea.MouseButton) {
	h.m.View()
	h.send(tea.MouseWheelMsg(tea.Mouse{X: x, Y: y, Button: b}))
}

// openNightTable opens "Late night drives" and returns its page; table
// rows start at screen row tableRow0 (no header row since spec 09).
func openNightTable(t *testing.T, h pbHarness) (*contextPage, int) {
	t.Helper()
	p := openNight(t, h)
	h.m.View()
	return p, mainY0 + p.tableY
}

func TestClickTableRowsMarksAndActivate(t *testing.T) {
	h := newPB(t)
	p, row0 := openNightTable(t, h)
	x := mainX0 + 10
	h.left(x, row0+2)
	if p.table.Cursor() != 2 || h.current() != "" {
		t.Fatalf("plain click: cursor %d playing %q", p.table.Cursor(), h.current())
	}
	// Ctrl-click and a gutter click toggle marks; shift-click extends.
	h.click(x, row0+4, tea.MouseLeft, tea.ModCtrl)
	h.left(mainX0, row0)
	if !p.table.Marked(4) || !p.table.Marked(0) || p.table.Cursor() != 0 {
		t.Fatalf("marks %d cursor %d", p.table.MarkCount(), p.table.Cursor())
	}
	h.click(x, row0+1, tea.MouseLeft, tea.ModShift)
	if p.table.MarkCount() != 3 {
		t.Fatalf("shift-click marks %d", p.table.MarkCount())
	}
	p.table.ClearMarks()
	// A click on the cursor row plays it.
	h.left(x, row0+1)
	if h.current() != "t2" {
		t.Fatalf("activate played %q", h.current())
	}
}

func TestRightClickOpensActionsAndPopupBlocksMouse(t *testing.T) {
	h := newPB(t)
	p, row0 := openNightTable(t, h)
	h.click(mainX0+10, row0+3, tea.MouseRight, 0)
	if h.m.popup == nil || p.table.Cursor() != 3 || !strings.Contains(plain(h.m), "Play next") {
		t.Fatalf("no actions popup:\n%s", plain(h.m))
	}
	h.left(mainX0+10, row0)
	if p.table.Cursor() != 3 {
		t.Fatal("click went through the popup")
	}
}

func TestLibraryColumnClicks(t *testing.T) {
	h := newPB(t)
	lib := h.m.pages[PageLibrary].(*libraryPage)
	h.left(mainX0+2, mainY0+4) // root: Playlists
	if c := lib.cols[0].list.Cursor(); c != 4 || lib.focus != 0 {
		t.Fatalf("cursor %d focus %d", c, lib.focus)
	}
	h.left(mainX0+2, mainY0+4) // again: open it
	if lib.focus != 1 || lib.col().key != "playlists" {
		t.Fatalf("second click did not open: focus %d", lib.focus)
	}
	// A click in the preview column to the right focuses it.
	h.keys(ch('j'))
	libPreview(h)
	h.m.View()
	x := mainX0 + lib.colX[2] + 3
	h.left(x, mainY0+1)
	if lib.focus != 2 || lib.col().table.Cursor() != 1 {
		t.Fatalf("track column click: focus %d", lib.focus)
	}
}

func TestSidebarClicks(t *testing.T) {
	h := newPB(t)
	s := h.m.side
	h.m.View()
	h.left(5, mainY0+s.listY+1) // Focus
	if p, ok := h.m.pages[PageContext].(*contextPage); !ok || p.id != "PLfocus" || !h.m.sideFocused() {
		t.Fatalf("sidebar click: %s focused %v", h.m.Current(), h.m.sideFocused())
	}
	// Clicking the main area takes focus back.
	h.left(mainX0+5, mainY0+8)
	if h.m.sideFocused() {
		t.Fatal("main click left focus in the sidebar")
	}
	h.click(5, mainY0+s.listY, tea.MouseRight, 0)
	if h.m.popup == nil || !strings.Contains(plain(h.m), "Late night drives") {
		t.Fatalf("no playlist actions:\n%s", plain(h.m))
	}
	h.keys(kEsc)
	h.left(3, mainY0+1) // Recently Played
	if h.m.Current() != PageRecent {
		t.Fatalf("pinned row went to %s", h.m.Current())
	}
}

func TestSearchClicks(t *testing.T) {
	h := newPB(t)
	h.keys(ch('g'), ch('s'), ch('d'), ch('a'), ch('f'), ch('t'), kEnter)
	sp := h.m.pages[PageSearch].(*searchPage)
	h.m.View()
	if sp.state != stateLoaded || sp.tabY < 0 {
		t.Fatalf("search not loaded: %v", sp.state)
	}
	h.left(mainX0+8, mainY0+sp.tabY) // " Songs " 0–6, " Albums " 7–14
	if sp.panel != panelAlbums {
		t.Fatalf("panel %d", sp.panel)
	}
	h.left(mainX0+3, mainY0)
	if !sp.editing {
		t.Fatal("input click did not start editing")
	}
	h.left(mainX0+3, mainY0+sp.bodyY)
	h.left(mainX0+3, mainY0+sp.bodyY)
	if p, ok := h.m.pages[PageContext].(*contextPage); sp.editing || !ok || p.id != "MPREb_disc" {
		t.Fatalf("album click: editing %v page %s", sp.editing, h.m.Current())
	}
}

func TestBarButtonClicks(t *testing.T) {
	h := newPB(t)
	h.send(PlayMsg{Tracks: scenarioTracks()[:3]})
	h.started("t1")
	h.m.View()
	btn := func(action string) barButton {
		for _, b := range h.m.geom.buttons {
			if b.action == action {
				return b
			}
		}
		t.Fatalf("no %s button", action)
		return barButton{}
	}
	at := func(action string) (int, int) { b := btn(action); return b.x, b.y }
	h.left(at("toggle_shuffle"))
	if !h.m.Queue().Shuffle() {
		t.Fatal("shuffle glyph did not toggle shuffle")
	}
	h.left(at("play_pause"))
	if calls := h.pl.Calls(); calls[len(calls)-1] != "toggle_pause" {
		t.Fatalf("play glyph: %v", calls)
	}
	h.left(at("cycle_repeat"))
	if h.m.bar.repeat == "off" {
		t.Fatal("repeat glyph did not cycle")
	}
}

func TestWheelMovesPaneUnderPointer(t *testing.T) {
	h := newPB(t)
	lib := h.m.pages[PageLibrary].(*libraryPage)
	h.wheel(mainX0+5, mainY0+5, tea.MouseWheelDown)
	if lib.cols[0].list.Cursor() != 1 {
		t.Fatalf("main cursor %d", lib.cols[0].list.Cursor())
	}
	h.wheel(5, mainY0+8, tea.MouseWheelDown)
	if h.m.side.list.Cursor() != 1 || lib.cols[0].list.Cursor() != 1 {
		t.Fatalf("sidebar cursor %d main %d", h.m.side.list.Cursor(), lib.cols[0].list.Cursor())
	}
}

func TestWheelScrubCommitsLast(t *testing.T) {
	h := newPB(t)
	st := playing
	h.pl.st = st
	h.send(PlayerEventMsg{playerState(st)})
	h.m.View()
	g := h.m.geom
	x, y := g.progressX+2, g.progressY
	m := h.m
	// Ticks arm timers; deliver the commits by hand, oldest first.
	m.Update(tea.MouseWheelMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseWheelUp}))
	m.Update(tea.MouseWheelMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseWheelUp}))
	if !strings.Contains(plain(m), "2:19") { // 2:13 + 2×3 s
		t.Fatalf("no scrub preview:\n%s", plain(m))
	}
	m.Update(scrubCommitMsg{m.scrubSeq - 1})
	if len(h.pl.seeks) != 0 {
		t.Fatal("stale scrub tick seeked")
	}
	m.Update(scrubCommitMsg{m.scrubSeq})
	if len(h.pl.seeks) != 1 || h.pl.seeks[0] != 139*time.Second || h.pl.modes[0] != player.SeekAbsolute {
		t.Fatalf("seeks %v", h.pl.seeks)
	}
	if m.bar.scrubbing {
		t.Fatal("still scrubbing after commit")
	}
}

func TestTabHit(t *testing.T) {
	names := []string{"Playlists", "Albums", "Artists"}
	// " playlists · albums · artists ": 0–10, separator 11, 12–19, 20, 21–29.
	for x, want := range map[int]int{0: 0, 10: 0, 11: -1, 12: 1, 19: 1, 21: 2, 29: 2, 30: -1} {
		if got := tabHit(names, x); got != want {
			t.Errorf("tabHit(%d) = %d, want %d", x, got, want)
		}
	}
}
