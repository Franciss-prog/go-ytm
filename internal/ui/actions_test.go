package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// openNight opens "Late night drives" (owned, 7 rows) from the library.
func openNight(t *testing.T, h pbHarness) *contextPage {
	t.Helper()
	h.send(openMsg{newPlaylistPage("PLnight", "Late night drives", columns(h.m.o.Config))})
	p, ok := h.m.pages[PageContext].(*contextPage)
	if !ok || p.table.Len() != 7 {
		t.Fatalf("playlist not open: %s", plain(h.m))
	}
	return p
}

// pick moves the open menu's cursor to label and selects it.
func pick(t *testing.T, h pbHarness, label string) {
	t.Helper()
	var mp *menuPopup
	switch p := h.m.popup.(type) {
	case *menuPopup:
		mp = p
	case *pickerPopup:
		mp = p.menuPopup
	default:
		t.Fatalf("no menu open (%T)", h.m.popup)
	}
	for range len(mp.view) {
		if mp.items[mp.view[mp.cursor]].label == label {
			h.keys(kEnter)
			return
		}
		h.keys(kDown)
	}
	t.Fatalf("no %q in menu:\n%s", label, plain(h.m))
}

func edits(h pbHarness) []string {
	f := h.m.o.API.(*fakeAPI)
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.edits...)
}

func TestTrackActionsMenu(t *testing.T) {
	h := newPB(t)
	openNight(t, h)
	h.keys(ch('a'))
	out := plain(h.m)
	for _, want := range []string{"Get Lucky", "Play next", "Go to artist", "Go to album", "Remove from playlist", "Like", "Copy link"} {
		if !strings.Contains(out, want) {
			t.Fatalf("menu lacks %q:\n%s", want, out)
		}
	}
	// Keys go to the popup, not the keymap: j moves, esc closes.
	h.keys(ch('j'), ch('j'))
	if h.m.popup == nil || h.m.Current() != PageContext {
		t.Fatal("popup closed or page changed by j")
	}
	h.keys(kEsc)
	if h.m.popup != nil {
		t.Fatal("esc did not close")
	}
	// Go to album opens the album page.
	h.keys(ch('a'))
	pick(t, h, "Go to album")
	if h.m.pageTitle() != "Album" || !slices.Contains(h.m.o.API.(*fakeAPI).Calls(), "Album:MPREb_ram") {
		t.Fatalf("go to album: %s", h.m.pageTitle())
	}
}

func TestLikeFromActions(t *testing.T) {
	h := newPB(t)
	p := openNight(t, h)
	h.keys(ch('a'))
	pick(t, h, "Like")
	if !slices.Contains(edits(h), "rate t1 like") {
		t.Fatalf("edits %v", edits(h))
	}
	if r, _ := p.table.Selected(); r.LikeStatus != ytmusic.Like {
		t.Fatal("row not marked liked")
	}
	h.keys(ch('a'))
	pick(t, h, "Unlike")
	if !slices.Contains(edits(h), "rate t1 indifferent") {
		t.Fatalf("edits %v", edits(h))
	}
	// Signed out: the error says how to fix it, the row is untouched.
	h.m.o.API.(*fakeAPI).errs["Rate"] = ytmusic.ErrUnauthenticated
	h.keys(ch('a'))
	pick(t, h, "Like")
	if !strings.Contains(plain(h.m), "ytm setup") {
		t.Fatalf("no sign-in hint:\n%s", plain(h.m))
	}
}

func TestAddToPlaylistAndDuplicate(t *testing.T) {
	h := newPB(t)
	api := h.m.o.API.(*fakeAPI)
	p := openNight(t, h)
	h.keys(ch('v'), ch('v'), ch('A')) // v marks and moves down
	out := plain(h.m)
	if !strings.Contains(out, "Add 2 tracks to playlist") || !strings.Contains(out, "+ New playlist") {
		t.Fatalf("picker:\n%s", out)
	}
	if strings.Contains(out, "Liked Music") {
		t.Fatal("picker offers a playlist the account does not own")
	}
	// Typing filters (j is text here, not a motion).
	for _, r := range "foc" {
		h.keys(ch(r))
	}
	if mp := h.m.popup.(*pickerPopup); len(mp.view) != 2 {
		t.Fatalf("filter: %d rows shown:\n%s", len(mp.view), plain(h.m))
	}
	// YouTube Music refuses a duplicate: ask, then add anyway.
	api.errs["AddPlaylistItems"] = ytmusic.ErrEditFailed
	pick(t, h, "Focus")
	if !strings.Contains(plain(h.m), "already in “Focus”") {
		t.Fatalf("no duplicate question:\n%s", plain(h.m))
	}
	delete(api.errs, "AddPlaylistItems")
	h.keys(ch('y'))
	if !slices.Contains(edits(h), "add PLfocus [t1 t2] dup=true") {
		t.Fatalf("edits %v", edits(h))
	}
	if h.m.popup != nil || p.table.MarkCount() != 0 || !strings.Contains(plain(h.m), "Added 2 tracks to “Focus”") {
		t.Fatalf("after add: popup %T marks %d\n%s", h.m.popup, p.table.MarkCount(), plain(h.m))
	}
}

func TestDuplicateDeclined(t *testing.T) {
	h := newPB(t)
	api := h.m.o.API.(*fakeAPI)
	openNight(t, h)
	api.errs["AddPlaylistItems"] = ytmusic.ErrEditFailed
	h.keys(ch('A'))
	pick(t, h, "Focus")
	h.keys(ch('n'))
	if h.m.popup != nil || len(edits(h)) != 0 {
		t.Fatalf("declined: popup %T edits %v", h.m.popup, edits(h))
	}
}

func TestCreatePlaylistFromPicker(t *testing.T) {
	h := newPB(t)
	openNight(t, h)
	h.keys(ch('A'))
	pick(t, h, "+ New playlist")
	h.keys(kEnter) // empty name is refused in place
	if !strings.Contains(plain(h.m), "Enter a name.") {
		t.Fatalf("empty name:\n%s", plain(h.m))
	}
	for _, r := range "Road trip" {
		h.keys(ch(r))
	}
	h.keys(kTab, kRight, kEnter)
	if !slices.Contains(edits(h), "create Road trip PUBLIC [t1]") {
		t.Fatalf("edits %v", edits(h))
	}
	if h.m.popup != nil || !strings.Contains(plain(h.m), "Created “Road trip”") {
		t.Fatalf("after create:\n%s", plain(h.m))
	}
}

func TestRemoveFromOwnedPlaylist(t *testing.T) {
	h := newPB(t)
	p := openNight(t, h)
	h.keys(ch('d'), ch('d'))
	if !strings.Contains(plain(h.m), "Remove “Get Lucky” from “Late night drives”?") {
		t.Fatalf("no confirm:\n%s", plain(h.m))
	}
	h.keys(kEsc)
	if p.table.Len() != 7 || len(edits(h)) != 0 {
		t.Fatal("cancel removed something")
	}
	h.keys(ch('d'), ch('d'), ch('y'))
	if !slices.Contains(edits(h), "remove PLnight [t1]") || p.table.Len() != 6 {
		t.Fatalf("edits %v rows %d", edits(h), p.table.Len())
	}
	// A failed removal keeps the row.
	h.m.o.API.(*fakeAPI).errs["RemovePlaylistItems"] = ytmusic.ErrEditFailed
	h.keys(ch('d'), ch('d'), ch('y'))
	if p.table.Len() != 6 || !strings.Contains(plain(h.m), "Couldn't remove from") {
		t.Fatalf("rows %d\n%s", p.table.Len(), plain(h.m))
	}
}

func TestAlbumRowsCannotBeRemoved(t *testing.T) {
	h := newPB(t)
	h.send(openMsg{newAlbumPage("MPREb_ram", "RAM", columns(h.m.o.Config))})
	h.keys(ch('a'))
	if strings.Contains(plain(h.m), "Remove from playlist") {
		t.Fatal("album rows offer removal")
	}
}

func TestDeleteLibraryPlaylist(t *testing.T) {
	h := newPB(t)
	lib := h.m.pages[PageLibrary].(*libraryPage)
	h.keys(ch('j'), ch('j'), ch('j'), ch('j'), kRight) // the Playlists column
	// Liked Music is not owned.
	h.keys(tea.Key{Code: tea.KeyDelete})
	if h.m.popup != nil || !strings.Contains(plain(h.m), "Only your own playlists") {
		t.Fatalf("delete LM:\n%s", plain(h.m))
	}
	h.keys(ch('j'), ch('a'))
	pick(t, h, "Delete playlist")
	if !strings.Contains(plain(h.m), "Delete “Late night drives”?") {
		t.Fatalf("no confirm:\n%s", plain(h.m))
	}
	h.keys(kEnter)
	if !slices.Contains(edits(h), "delete PLnight") || lib.col().list.Len() != 2 {
		t.Fatalf("edits %v rows %d", edits(h), lib.col().list.Len())
	}
}

func TestQueueTrackActions(t *testing.T) {
	h := newPB(t)
	h.send(PlayMsg{Tracks: scenarioTracks()[:3]})
	h.keys(ch('z'), ch('j'), ch('a'))
	out := plain(h.m)
	if !strings.Contains(out, "Remove from queue") || strings.Contains(out, "Add to queue") {
		t.Fatalf("queue menu:\n%s", out)
	}
	pick(t, h, "Remove from queue")
	if h.m.Queue().Len() != 2 {
		t.Fatalf("queue len %d", h.m.Queue().Len())
	}
}

func TestPopupResultForClosedPickerDropped(t *testing.T) {
	h := newPB(t)
	openNight(t, h)
	h.keys(ch('A'))
	old := h.m.popup
	h.keys(kEsc)
	h.send(popupMsg{to: old, msg: Loaded{Key: "playlists", Value: []ytmusic.PlaylistRef{}}})
	if h.m.popup != nil {
		t.Fatal("result reopened a closed picker")
	}
}
