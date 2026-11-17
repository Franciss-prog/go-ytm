package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestLibraryOpensPlaylistWithContinuation(t *testing.T) {
	h := newPB(t)
	api := h.m.o.API.(*fakeAPI)
	h.keys(ch('j'), tea.Key{Code: tea.KeyEnter}) // Late night drives
	if h.m.Current() != PageContext || h.m.pageTitle() != "Playlist" {
		t.Fatalf("on %s (%s)", h.m.Current(), h.m.pageTitle())
	}
	p := h.m.pages[PageContext].(*contextPage)
	if p.table.Len() != 7 || !slices.Contains(api.Calls(), "PlaylistMore:PLnight@4") {
		t.Fatalf("rows %d calls %v", p.table.Len(), api.Calls())
	}
	if strings.Contains(plain(h.m), "Loading…") {
		t.Fatal("loading footer left after the last page")
	}
	// Enter plays from the cursor with the playlist as queue context.
	h.keys(ch('j'), ch('j'), tea.Key{Code: tea.KeyEnter})
	if h.current() != "t3" || h.m.Queue().Context() != "PLnight" || h.m.Queue().Len() != 7 {
		t.Fatalf("play: current %s ctx %s len %d", h.current(), h.m.Queue().Context(), h.m.Queue().Len())
	}
	// Back restores the library with its cursor; forward restores the page.
	h.keys(tea.Key{Code: tea.KeyBackspace})
	if h.m.Current() != PageLibrary || !strings.Contains(h.m.pages[PageLibrary].Info(), "Late night drives") {
		t.Fatalf("back: %s %q", h.m.Current(), h.m.pages[PageLibrary].Info())
	}
	h.keys(tea.Key{Code: tea.KeyBackspace, Mod: tea.ModShift})
	if h.m.pages[PageContext] != p || p.table.Cursor() != 2 {
		t.Fatal("forward did not restore the playlist page and cursor")
	}
}

func TestStalePageResultDropped(t *testing.T) {
	h := newPB(t)
	gone := newPlaylistPage("PLx", "x", columns(h.m.o.Config))
	if cmd := h.m.deliver(pageMsg{to: gone, msg: Loaded{Key: "playlist"}}); cmd != nil || gone.state != stateIdle {
		t.Fatal("result for a page outside the history was delivered")
	}
}

func TestContextErrorAndRetry(t *testing.T) {
	h := newPB(t)
	api := h.m.o.API.(*fakeAPI)
	api.errs["Album"] = errors.New("boom")
	h.send(openMsg{newAlbumPage("MPREb_ram", "RAM", columns(h.m.o.Config))})
	if !strings.Contains(plain(h.m), "press r to retry") {
		t.Fatalf("no error state:\n%s", plain(h.m))
	}
	delete(api.errs, "Album")
	h.keys(ch('r'))
	if !strings.Contains(plain(h.m), "Giorgio by Moroder") {
		t.Fatalf("retry did not reload:\n%s", plain(h.m))
	}
	// X / Z enqueue (play next / add) the cursor row or the marks.
	h.keys(ch('v'), ch('v'), ch('Z'))
	if got := queueIDs(h.m.Queue()); got != "t1,t2" {
		t.Fatalf("Z marked: %s", got)
	}
	if p := h.m.pages[PageContext].(*contextPage); p.table.MarkCount() != 0 {
		t.Fatal("marks kept after enqueue")
	}
}

func TestArtistPage(t *testing.T) {
	h := newPB(t)
	h.send(openMsg{newArtistPage("UCdp", "Daft Punk", columns(h.m.o.Config))})
	api := h.m.o.API.(*fakeAPI)
	if !slices.Contains(api.Calls(), "ArtistAlbums:MPADUCdp") {
		t.Fatalf("no discography load: %v", api.Calls())
	}
	p := h.m.pages[PageContext].(*contextPage)
	if p.releases.Len() != 3 { // 2 albums from the full list + 1 single
		t.Fatalf("releases %d", p.releases.Len())
	}
	h.keys(tea.Key{Code: tea.KeyTab}, ch('j'), tea.Key{Code: tea.KeyEnter})
	if h.m.pageTitle() != "Album" || !slices.Contains(api.Calls(), "Album:MPREb_disc") {
		t.Fatalf("release did not open: %s %v", h.m.pageTitle(), api.Calls())
	}
}

func TestLibrarySignedOut(t *testing.T) {
	h := newPB(t)
	h.m.o.API.(*fakeAPI).errs["LibraryArtists"] = errUnauth()
	h.keys(tea.Key{Code: tea.KeyTab, Mod: tea.ModShift})
	if !strings.Contains(plain(h.m), "Run `ytm setup`") {
		t.Fatalf("no signed-out state:\n%s", plain(h.m))
	}
}
