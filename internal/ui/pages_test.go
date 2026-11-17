package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
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

func typeKeys(h pbHarness, s string) {
	for _, r := range s {
		h.keys(ch(r))
	}
}

func TestSearchFlow(t *testing.T) {
	h := newPB(t)
	api := h.m.o.API.(*fakeAPI)
	h.keys(ch('g'), ch('s'))
	p := h.m.pages[PageSearch].(*searchPage)
	if !p.Capturing() {
		t.Fatal("input not focused on arrival")
	}
	typeKeys(h, "daf")
	stale := p.seq
	typeKeys(h, "t")
	// The tick for an older keystroke is ignored; the latest one asks.
	h.send(pageMsg{to: p, msg: suggestTickMsg{stale}})
	if slices.Contains(api.Calls(), "SearchSuggestions:daf") {
		t.Fatal("debounce: stale tick requested suggestions")
	}
	h.send(pageMsg{to: p, msg: suggestTickMsg{p.seq}})
	if !strings.Contains(plain(h.m), "daft punk get lucky") {
		t.Fatalf("no suggestions:\n%s", plain(h.m))
	}
	// A reply for older input is dropped.
	h.send(pageMsg{to: p, msg: Loaded{Key: "suggest@" + itoa(stale), Value: []string{"stale!"}}})
	if strings.Contains(plain(h.m), "stale!") {
		t.Fatal("stale suggestions shown")
	}
	// Down + enter searches the second suggestion.
	h.keys(tea.Key{Code: tea.KeyDown}, tea.Key{Code: tea.KeyDown}, tea.Key{Code: tea.KeyEnter})
	if p.query != "daft punk get lucky" || !slices.Contains(api.Calls(), "Search:daft punk get lucky|songs") {
		t.Fatalf("query %q calls %v", p.query, api.Calls())
	}
	out := plain(h.m)
	for _, want := range []string{"Top result: Daft Punk · Artist", "songs · albums", "Get Lucky", "Instant Crush"} {
		if !strings.Contains(out, want) {
			t.Fatalf("results lack %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "videos") {
		t.Fatal("videos panel in music mode")
	}
	// Enter on a song plays the songs list from there.
	h.keys(ch('j'), tea.Key{Code: tea.KeyEnter})
	if h.current() != "t2" {
		t.Fatalf("play from search: %s", h.current())
	}
	// Tab to Albums, enter opens it.
	h.keys(tea.Key{Code: tea.KeyTab}, tea.Key{Code: tea.KeyEnter})
	if h.m.pageTitle() != "Album" || !slices.Contains(api.Calls(), "Album:MPREb_disc") {
		t.Fatalf("album: %s %v", h.m.pageTitle(), api.Calls())
	}
	h.keys(tea.Key{Code: tea.KeyBackspace})
	// All mode adds Videos.
	h.m.action(keyAction("toggle_search_mode"))
	if !strings.Contains(plain(h.m), "· videos") {
		t.Fatal("all mode has no Videos panel")
	}
	// Esc in the input hands keys back to the keymap.
	h.keys(ch('g'), ch('s'), tea.Key{Code: tea.KeyEscape}, ch('g'), ch('l'))
	if h.m.Current() != PageLibrary {
		t.Fatalf("esc then g l: on %s", h.m.Current())
	}
}

func TestSearchErrorAndEmpty(t *testing.T) {
	h := newPB(t)
	api := h.m.o.API.(*fakeAPI)
	api.errs["Search"] = errors.New("boom")
	h.keys(ch('g'), ch('s'))
	typeKeys(h, "x")
	h.keys(tea.Key{Code: tea.KeyEnter})
	if !strings.Contains(plain(h.m), "press r to retry") {
		t.Fatalf("no error:\n%s", plain(h.m))
	}
	delete(api.errs, "Search")
	h.keys(ch('r'))
	if !strings.Contains(plain(h.m), "Top result") {
		t.Fatalf("retry:\n%s", plain(h.m))
	}
}

func keyAction(name string) keys.Action { return keys.Action{Name: name} }
