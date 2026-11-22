package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/config"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

func browseModel(t *testing.T) pbHarness {
	t.Helper()
	h := newPB(t)
	drain(h.m, h.m.navigate(PageBrowse))
	return h
}

func TestBrowseForYouOpensRows(t *testing.T) {
	h := browseModel(t)
	b := h.m.pages[PageBrowse].(*browsePage)
	if _, i, _ := b.cur().list.Selected(); i != 1 {
		t.Fatalf("cursor starts on the first item, not the heading: %d", i)
	}
	// Song: plays it.
	drain(h.m, press(h.m, tea.Key{Code: tea.KeyEnter}))
	if h.current() != "abc" && h.lastItem() == "<none>" {
		t.Fatalf("song did not play: %s", h.lastItem())
	}
	// Album: opens the album page.
	drain(h.m, h.m.navigate(PageBrowse))
	drain(h.m, press(h.m, ch('j'), tea.Key{Code: tea.KeyEnter}))
	if h.m.pageTitle() != "Album" {
		t.Errorf("album row opened %q", h.m.pageTitle())
	}
	// A radio mix plays its watch queue instead of opening a page.
	drain(h.m, h.m.navigate(PageBrowse))
	api := h.m.o.API.(*fakeAPI)
	api.watch = map[string][]ytmusic.Track{"RDTMAK5uy_mix": tracks("m1", "m2")}
	b.cur().list.SetCursor(4)
	drain(h.m, press(h.m, tea.Key{Code: tea.KeyEnter}))
	if h.current() != "m1" || h.m.pb.radio.source != "My Supermix" {
		t.Errorf("mix: current %s source %q", h.current(), h.m.pb.radio.source)
	}
}

func TestBrowseTabsLoadLazily(t *testing.T) {
	h := browseModel(t)
	api := h.m.o.API.(*fakeAPI)
	count := func(m string) int {
		n := 0
		for _, c := range api.Calls() {
			if strings.HasPrefix(c, m) {
				n++
			}
		}
		return n
	}
	if count("Charts") != 0 || count("Explore") != 0 || count("Home") != 1 {
		t.Fatalf("calls %v", api.Calls())
	}
	for range 3 {
		drain(h.m, press(h.m, tea.Key{Code: tea.KeyTab}))
	}
	if count("Charts") != 1 || count("Explore") != 1 || count("Home") != 1 {
		t.Errorf("each tab loads once, playlists reuse home: %v", api.Calls())
	}
	b := h.m.pages[PageBrowse].(*browsePage)
	if b.tab != browsePlaylists || b.cur().list.Len() != 2 {
		t.Errorf("playlists tab: tab %d, %d rows", b.tab, b.cur().list.Len())
	}
}

func TestBrowseRegionPick(t *testing.T) {
	pr := &patchRec{}
	h := newPB(t, func(o *Options) { o.ConfigPath, o.PatchConfig = "/cfg/config.toml", pr.patch })
	drain(h.m, h.m.navigate(PageBrowse))
	drain(h.m, press(h.m, tea.Key{Code: tea.KeyTab}))
	drain(h.m, press(h.m, ch('c')))
	if h.m.popup == nil {
		t.Fatal("c did not open the picker")
	}
	press(h.m, ch('j'), ch('a'), ch('p'), ch('a'), ch('n'))
	drain(h.m, press(h.m, tea.Key{Code: tea.KeyEnter}))
	b := h.m.pages[PageBrowse].(*browsePage)
	if b.region != "JP" || h.m.o.Config.UI.Region != "JP" || !slices.Contains(h.m.o.API.(*fakeAPI).Calls(), "Charts:JP") {
		t.Fatalf("region %s config %s calls %v", b.region, h.m.o.Config.UI.Region, h.m.o.API.(*fakeAPI).Calls())
	}
	if !slices.Contains(pr.changes, config.Change{Key: "ui.region", Value: "JP"}) {
		t.Errorf("not persisted: %v", pr.changes)
	}
	// A late answer for the old region is dropped.
	b.Receive(Loaded{Key: "charts@ZZ", Value: ytmusic.Charts{Playlists: []ytmusic.PlaylistRef{{Title: "old", ID: "x"}}}}, h.m.ctx())
	if it, _, _ := b.cur().list.Selected(); it.Left == "old" {
		t.Error("stale region answer applied")
	}
}

func TestRecentTabsAndMerge(t *testing.T) {
	h := newPB(t)
	ts := scenarioTracks()
	h.m.plays.add(ts[3])
	h.m.plays.add(ts[1])
	drain(h.m, h.m.navigate(PageRecent))
	r := h.m.pages[PageRecent].(*recentPage)
	ids := func() []string {
		var out []string
		for _, t := range r.table.Rows() {
			out = append(out, t.VideoID)
		}
		return out
	}
	// Account history: ts[1], ts[2]. All: local first, then account-only.
	if got, want := ids(), []string{ts[1].VideoID, ts[3].VideoID, ts[2].VideoID}; !slices.Equal(got, want) {
		t.Fatalf("all %v want %v", got, want)
	}
	drain(h.m, press(h.m, tea.Key{Code: tea.KeyTab}))
	if got := ids(); len(got) != 2 || r.tab != recentLocal {
		t.Errorf("local %v", got)
	}
	drain(h.m, press(h.m, tea.Key{Code: tea.KeyTab}))
	if got := ids(); len(got) != 2 || got[0] != ts[1].VideoID {
		t.Errorf("yt music %v", got)
	}

	// Signed out: All keeps local rows and says what is missing.
	h2 := newPB(t, func(o *Options) { o.SignedIn = false })
	h2.m.plays.add(ts[0])
	drain(h2.m, h2.m.navigate(PageRecent))
	if v := plain(h2.m); !strings.Contains(v, ts[0].Title) || !strings.Contains(v, "Account history unavailable") {
		t.Errorf("partial view:\n%s", v)
	}
}

func TestLocalPlaysRecordedWithoutSync(t *testing.T) {
	h := newPB(t, func(o *Options) {
		o.SignedIn = false
		o.Config.Playback.HistoryMinListenSeconds = 0
	})
	h.send(PlayMsg{Tracks: tracks("a"), Start: 0})
	h.started("a")
	h.pl.st.Position = 2e9
	h.send(PlayerEventMsg{playerState(h.pl.st)})
	if len(h.m.plays.tracks) != 1 || h.m.plays.tracks[0].VideoID != "a" {
		t.Fatalf("local plays %v", h.m.plays.tracks)
	}
}

func TestLikedStartRadioClick(t *testing.T) {
	h := newPB(t)
	api := h.m.o.API.(*fakeAPI)
	api.pages["LM"] = scenarioTracks()
	api.watch = map[string][]ytmusic.Track{"": tracks("r1", "r2")}
	drain(h.m, h.m.navigate(PageLiked))
	p := h.m.pages[PageLiked].(*contextPage)
	if p.ID() != PageLiked || p.PanelTitle() != "liked songs" {
		t.Fatalf("liked page id %s title %q", p.ID(), p.PanelTitle())
	}
	plain(h.m) // lay out the page first, as the program does
	_, cmd := p.Click(mouseEv{y: 0}, h.m.ctx())
	drain(h.m, cmd)
	if h.m.pb.radio.source != "Radio: Liked Songs" || h.m.Queue().Len() < 2 {
		t.Errorf("radio source %q, queue %d", h.m.pb.radio.source, h.m.Queue().Len())
	}
}
