package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

var (
	kCtrlW = tea.Key{Code: 'w', Mod: tea.ModCtrl}
	kCtrlE = tea.Key{Code: 'e', Mod: tea.ModCtrl}
)

func countCalls(h pbHarness, call string) int {
	n := 0
	for _, c := range h.m.o.API.(*fakeAPI).Calls() {
		if c == call {
			n++
		}
	}
	return n
}

func TestSidebarListsPlaylistsWithoutLikedMusic(t *testing.T) {
	h := newPB(t)
	out := plain(h.m)
	for _, want := range []string{"Liked Songs", "Recently Played", "Discovery Mix", "Late night drives", "2 playlists"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if len(h.m.side.playlists) != 2 || h.m.side.playlists[0].ID != "PLnight" {
		t.Fatalf("playlists %+v", h.m.side.playlists)
	}
}

func TestSidebarFocusMoveAndOpen(t *testing.T) {
	h := newPB(t)
	lib := h.m.pages[PageLibrary].(*libraryPage)
	h.keys(kCtrlW, ch('h'))
	if !h.m.sideFocused() {
		t.Fatal("C-w h did not focus the sidebar")
	}
	h.keys(ch('j'))
	if lib.cols[0].list.Cursor() != 0 || !strings.Contains(plain(h.m), "Focus · 43 songs") {
		t.Fatalf("j moved the page, or info line wrong:\n%s", plain(h.m))
	}
	h.keys(kEnter)
	p, ok := h.m.pages[PageContext].(*contextPage)
	if h.m.Current() != PageContext || !ok || p.id != "PLfocus" {
		t.Fatalf("enter opened %s", h.m.Current())
	}
	if !h.m.sideFocused() {
		t.Fatal("opening a playlist moved focus off the sidebar")
	}
	// Enter on the playlist already shown does not stack history.
	back := len(h.m.back)
	h.keys(kEnter)
	if len(h.m.back) != back {
		t.Fatal("reopened the shown playlist")
	}
	// C-w l hands j back to the page.
	h.keys(kCtrlW, ch('l'), ch('j'))
	if h.m.sideFocused() || p.table.Cursor() != 1 {
		t.Fatalf("focus %v cursor %d", h.m.focus, p.table.Cursor())
	}
	h.keys(kCtrlW, ch('w'))
	if !h.m.sideFocused() {
		t.Fatal("C-w w did not cycle to the sidebar")
	}
	h.keys(kCtrlW, ch('w'))
	if h.m.sideFocused() {
		t.Fatal("C-w w did not cycle back")
	}
}

func TestSidebarFilter(t *testing.T) {
	h := newPB(t)
	h.keys(kCtrlW, ch('h'), ch('/'), ch('f'), ch('o'), ch('c'))
	if !h.m.side.list.Filtering() || h.m.side.list.Shown() != 1 {
		t.Fatalf("filter shown %d", h.m.side.list.Shown())
	}
	// Typed letters went to the filter, not the keymap (no navigation).
	if h.m.Current() != PageLibrary {
		t.Fatalf("filter keys leaked: on %s", h.m.Current())
	}
	h.keys(kEnter)
	if !strings.Contains(plain(h.m), "1/2") {
		t.Fatalf("no filter count:\n%s", plain(h.m))
	}
	h.keys(kEnter)
	if p, ok := h.m.pages[PageContext].(*contextPage); !ok || p.id != "PLfocus" {
		t.Fatal("enter after filter did not open Focus")
	}
}

func TestSidebarActionsAndDelete(t *testing.T) {
	h := newPB(t)
	h.keys(kCtrlW, ch('h'), ch('a'))
	if h.m.popup == nil || !strings.Contains(plain(h.m), "Delete playlist") {
		t.Fatalf("no actions popup:\n%s", plain(h.m))
	}
	// "Open" runs select on the sidebar, not on the Library page.
	pick(t, h, "Open")
	if p, ok := h.m.pages[PageContext].(*contextPage); !ok || p.id != "PLnight" {
		t.Fatalf("Open went to %s", h.m.Current())
	}
	h.keys(tea.Key{Code: tea.KeyDelete})
	if !strings.Contains(plain(h.m), "Delete “Late night drives”?") {
		t.Fatalf("no confirm:\n%s", plain(h.m))
	}
	h.keys(kEnter)
	if !slices.Contains(edits(h), "delete PLnight") || len(h.m.side.playlists) != 1 {
		t.Fatalf("edits %v sidebar %d", edits(h), len(h.m.side.playlists))
	}
	if it, _, _ := h.m.side.list.Selected(); it.Key != "PLfocus" {
		t.Fatalf("cursor on %q after delete", it.Key)
	}
}

func TestSidebarReloadsAfterPlaylistEdit(t *testing.T) {
	h := newPB(t)
	h.keys(ch('z')) // off the Library page: only the sidebar reloads now
	before := countCalls(h, "LibraryPlaylists:")
	drain(h.m, h.m.playlistsChanged())
	if countCalls(h, "LibraryPlaylists:") != before+1 {
		t.Fatalf("sidebar not reloaded: %v", h.m.o.API.(*fakeAPI).Calls())
	}
}

func TestSidebarToggleAndNarrow(t *testing.T) {
	h := newPB(t)
	h.keys(kCtrlW, ch('h'), kCtrlE)
	if h.m.sidebar || h.m.focus != paneMain || strings.Contains(plain(h.m), "Discovery Mix") {
		t.Fatal("C-e did not hide the sidebar and return focus")
	}
	// C-w h shows it again.
	h.keys(kCtrlW, ch('h'))
	if !h.m.sidebar || !h.m.sideFocused() {
		t.Fatal("C-w h did not show and focus the sidebar")
	}
	h.send(tea.WindowSizeMsg{Width: 70, Height: 30})
	if h.m.sideFocused() {
		t.Fatal("hidden sidebar still owns keys")
	}
	h.keys(kCtrlW, ch('h'))
	if !strings.Contains(plain(h.m), "too narrow") {
		t.Fatalf("no narrow toast:\n%s", plain(h.m))
	}
}

func TestSidebarEnsureLoadsOnce(t *testing.T) {
	h := newPB(t)
	h.m.side = newPlaylistSidebar(false)
	if h.m.side.ensure(h.m.ctx()) == nil {
		t.Fatal("idle sidebar did not load")
	}
	if h.m.side.ensure(h.m.ctx()) != nil {
		t.Fatal("loading sidebar loaded twice")
	}
}

func TestSidebarErrorRetryAndStale(t *testing.T) {
	h := newPB(t)
	api := h.m.o.API.(*fakeAPI)
	api.errs["LibraryPlaylists"] = ytmusic.ErrUnauthenticated
	drain(h.m, h.m.side.load(h.m.ctx()))
	if !strings.Contains(plain(h.m), "Not signed in") {
		t.Fatalf("no error:\n%s", plain(h.m))
	}
	delete(api.errs, "LibraryPlaylists")
	h.keys(kCtrlW, ch('h'), ch('r'))
	if h.m.side.state != stateLoaded {
		t.Fatalf("retry state %v", h.m.side.state)
	}
	// An answer to an older request is dropped.
	stale := sidebarLoadedMsg{seq: h.m.side.seq - 1, err: errors.New("old")}
	h.send(stale)
	if h.m.side.state != stateLoaded {
		t.Fatal("stale result applied")
	}
}

func TestShortCount(t *testing.T) {
	for in, want := range map[string]string{"87 songs": "87", "1,204 songs": "1,204", "676 views": "676", "": "", "Auto playlist": ""} {
		if got := shortCount(in); got != want {
			t.Errorf("shortCount(%q) = %q, want %q", in, got, want)
		}
	}
}
