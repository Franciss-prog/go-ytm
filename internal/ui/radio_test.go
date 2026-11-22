package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Franciss-Prog/go-ytm/internal/player"
	"github.com/Franciss-Prog/go-ytm/internal/queue"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

func radioQueue(q *queue.Queue) string {
	var s []string
	for _, e := range q.Entries() {
		id := e.Track.VideoID
		if q.IsAutoplay(e.ID) {
			id += "↻"
		}
		s = append(s, id)
	}
	return strings.Join(s, ",")
}

func TestStartRadioReplacesQueue(t *testing.T) {
	h := newPB(t)
	api := h.m.o.API.(*fakeAPI)
	api.watch = map[string][]ytmusic.Track{"a": tracks("a", "r1", "r2")}
	h.send(PlayMsg{Tracks: tracks("x", "y"), Start: 0, Context: "PL1"})
	h.send(RadioMsg{Seeds: tracks("a")})
	if got := radioQueue(h.m.Queue()); got != "a,r1,r2" {
		t.Fatalf("queue %s", got)
	}
	if h.current() != "a" || h.lastItem() != "url:a" || h.m.Queue().Context() != "" {
		t.Fatalf("current %s load %s ctx %q", h.current(), h.lastItem(), h.m.Queue().Context())
	}
	qp := h.m.pages[PageQueue].(*queuePage)
	if qp.source != "Radio from Song a" {
		t.Errorf("queue source %q", qp.source)
	}
	if !strings.Contains(ansi.Strip(qp.View(80, 10, h.m.st)), "Generated from: Radio from Song a") {
		t.Error("queue page lacks the source line")
	}
	// A new PlayMsg is not a radio: the label goes.
	h.send(PlayMsg{Tracks: tracks("x"), Start: 0})
	if qp.source != "" {
		t.Errorf("source kept after play: %q", qp.source)
	}
}

func TestStartRadioFromPlaylistAndEmpty(t *testing.T) {
	h := newPB(t)
	api := h.m.o.API.(*fakeAPI)
	api.watch = map[string][]ytmusic.Track{"RDAMPLPLnight": tracks("p1", "p2")}
	h.send(RadioMsg{Playlist: "VLPLnight", Label: "Radio from Late night drives"})
	if got := radioQueue(h.m.Queue()); got != "p1,p2" || h.current() != "p1" {
		t.Fatalf("playlist radio queue %s current %s", got, h.current())
	}
	api.watch = nil
	h.send(RadioMsg{Seeds: tracks("zz")})
	if got := radioQueue(h.m.Queue()); got != "p1,p2" {
		t.Errorf("empty radio must keep the queue: %s", got)
	}
	if !slices.ContainsFunc(h.m.toasts.items, func(t Toast) bool { return strings.Contains(t.Text, "No radio suggestions") }) {
		t.Error("no warning toast")
	}
}

func TestAutoplayRefillsNearTheEnd(t *testing.T) {
	h := newPB(t)
	api := h.m.o.API.(*fakeAPI)
	api.watch = map[string][]ytmusic.Track{"": tracks("a", "n1", "n2")}
	h.send(PlayMsg{Tracks: tracks("a", "b"), Start: 0})
	// Few tracks left: autoplay appends related tracks, marked ↻, without
	// the ones already queued.
	if got := radioQueue(h.m.Queue()); got != "a,b,n1↻,n2↻" {
		t.Fatalf("queue %s", got)
	}
	calls := len(api.Calls())
	h.started("a") // gapless start of the same track: no second fetch for this queue
	h.send(PlayMsg{Tracks: tracks("a", "b"), Start: 0})
	if len(api.Calls()) == calls {
		t.Error("a new queue should refill again")
	}

	// Repeat on: no autoplay.
	h = newPB(t)
	h.m.o.API.(*fakeAPI).watch = map[string][]ytmusic.Track{"": tracks("n1")}
	h.m.Queue().SetRepeat(queue.RepeatAll)
	h.send(PlayMsg{Tracks: tracks("a"), Start: 0})
	if got := radioQueue(h.m.Queue()); got != "a" {
		t.Errorf("repeat all refilled: %s", got)
	}

	// Autoplay off.
	h = newPB(t, func(o *Options) { o.Config.Playback.Autoplay = false })
	h.m.o.API.(*fakeAPI).watch = map[string][]ytmusic.Track{"": tracks("n1")}
	h.send(PlayMsg{Tracks: tracks("a"), Start: 0})
	if got := radioQueue(h.m.Queue()); got != "a" {
		t.Errorf("autoplay off refilled: %s", got)
	}
}

func TestAutoplayContinuesAfterLastTrack(t *testing.T) {
	h := newPB(t)
	api := h.m.o.API.(*fakeAPI)
	h.send(PlayMsg{Tracks: tracks("a"), Start: 0}) // refill finds nothing yet
	api.watch = map[string][]ytmusic.Track{"": tracks("n1", "n2")}
	h.started("a")
	h.send(PlayerEventMsg{player.TrackEnded{Item: player.Item{ID: "a"}, Reason: player.EndNatural}})
	if h.current() != "n1" || h.lastItem() != "url:n1" {
		t.Fatalf("after the end: current %s load %s queue %s", h.current(), h.lastItem(), radioQueue(h.m.Queue()))
	}
}

func TestDiscoveryMixRotatesSources(t *testing.T) {
	h := newPB(t)
	api := h.m.o.API.(*fakeAPI)
	api.charts = map[string]ytmusic.Charts{"ZZ": {Country: "Global", Playlists: []ytmusic.PlaylistRef{{Title: "Top 100", ID: "PLnight"}}}}
	api.watch = map[string][]ytmusic.Track{"": tracks("r1", "r2")}
	h.m.o.Config.UI.Region = "ZZ"
	h.act("discovery_mix")
	if h.m.current != PageQueue || h.m.pb.radio.discovery != 1 {
		t.Fatalf("page %s source %d", h.m.current, h.m.pb.radio.discovery)
	}
	if src := h.m.pages[PageQueue].(*queuePage).source; !strings.HasPrefix(src, "Discovery (Global Top 100") {
		t.Errorf("label %q", src)
	}
	// Next press starts after charts: trending (empty) → home.
	api.home = []ytmusic.HomeShelf{{Title: "Mixed", Items: []ytmusic.SearchResult{{Type: ytmusic.TypeSong, Track: &tracks("h1")[0]}}}}
	h.act("discovery_mix")
	if h.m.pb.radio.discovery != 3 {
		t.Errorf("second source %d, want 3 (home)", h.m.pb.radio.discovery)
	}
	// Nothing anywhere: warning, queue untouched.
	h2 := newPB(t)
	for _, m := range []string{"Charts", "Explore", "Home", "Playlist", "LibraryArtists", "History"} {
		h2.m.o.API.(*fakeAPI).errs[m] = errUnauth()
	}
	h2.act("discovery_mix")
	if !slices.ContainsFunc(h2.m.toasts.items, func(t Toast) bool { return strings.Contains(t.Text, "Discovery failed") }) {
		t.Error("no failure toast")
	}
}

func TestHistoryReportAfterThreshold(t *testing.T) {
	h := newPB(t, func(o *Options) { o.Config.Playback.HistoryMinListenSeconds = 5 })
	api := h.m.o.API.(*fakeAPI)
	h.send(PlayMsg{Tracks: tracks("a", "b"), Start: 0})
	pos := func(id string, s int) {
		h.pl.st = player.State{Status: player.Playing, Item: player.Item{ID: id}, Position: time.Duration(s) * time.Second}
		h.send(PlayerEventMsg{playerState(h.pl.st)})
	}
	pos("a", 3)
	if len(api.reported) != 0 {
		t.Fatal("reported before the threshold")
	}
	pos("a", 6)
	pos("a", 7)
	if !slices.Equal(api.reported, []string{"a"}) {
		t.Fatalf("reported %v, want once", api.reported)
	}
	// Signed out or sync off: nothing.
	h = newPB(t, func(o *Options) { o.SignedIn = false })
	h.send(PlayMsg{Tracks: tracks("a"), Start: 0})
	h.pl.st = player.State{Status: player.Playing, Item: player.Item{ID: "a"}, Position: time.Minute}
	h.send(PlayerEventMsg{playerState(h.pl.st)})
	if r := h.m.o.API.(*fakeAPI).reported; len(r) != 0 {
		t.Errorf("signed out reported %v", r)
	}
}

func TestStartRadioMenuItems(t *testing.T) {
	h := newPB(t)
	pp := playlistActionsPopup(ytmusic.PlaylistRef{Title: "Focus", ID: "PLfocus"}).(*menuPopup)
	found := false
	for _, it := range pp.items {
		if it.label == "Start radio" {
			found = true
			_, cmd := it.run(h.m.ctx())
			if msg, ok := cmd().(RadioMsg); !ok || msg.Playlist != "PLfocus" {
				t.Errorf("playlist radio msg %#v", msg)
			}
		}
	}
	if !found {
		t.Error("playlist menu lacks Start radio")
	}
}
