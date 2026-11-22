package ytmusic

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Fixtures: recorded 2026-11-22 signed out with TestRecordM6, scrubbed
// with scripts/scrubfixtures. history_synthetic and player_synthetic are
// hand-built (see their _comment); the signed-in history capture is a
// known gap (docs/PROGRESS.md).

func serveFixture(t *testing.T, names ...string) (*API, func() []recorded) {
	t.Helper()
	srv, reqs := server(t, func(w http.ResponseWriter, r recorded, n int) {
		_, _ = w.Write(fixtureBytes(t, names[min(n, len(names))-1]))
	})
	return newTestAPI(t, srv, Options{Credentials: &Credentials{Cookie: "__Secure-3PAPISID=x"}}), reqs
}

func TestWatchPlaylistRadio(t *testing.T) {
	a, reqs := serveFixture(t, "watch_radio")
	w, err := a.WatchPlaylist(context.Background(), WatchRequest{VideoID: "abc", Radio: true})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "watch_radio", w)
	if len(w.Tracks) != 6 || w.LyricsID == "" || w.RelatedID == "" || !strings.HasPrefix(w.PlaylistID, "RDAMVM") {
		t.Fatalf("watch: %d tracks, %+v", len(w.Tracks), w)
	}
	for _, tr := range w.Tracks {
		if tr.VideoID == "" || tr.Title == "" || len(tr.Artists) == 0 || tr.Duration == 0 {
			t.Errorf("track: %+v", tr)
		}
	}
	body := reqs()[0].body
	if body["videoId"] != "abc" || body["playlistId"] != "RDAMVMabc" || body["params"] != "wAEB" || body["watchEndpointMusicSupportedConfigs"] != nil {
		t.Errorf("radio body: %v", body)
	}
}

func TestWatchPlaylistBodies(t *testing.T) {
	cases := []struct {
		name string
		req  WatchRequest
		want map[string]any // nil value = key absent
	}{
		{"up next", WatchRequest{VideoID: "v1"}, map[string]any{"playlistId": "RDAMVMv1", "params": nil}},
		{"playlist radio", WatchRequest{PlaylistID: "VLRDAMPLPLx", Radio: true}, map[string]any{"playlistId": "RDAMPLPLx", "params": "wAEB", "videoId": nil}},
		{"shuffle", WatchRequest{PlaylistID: "PLx", Shuffle: true}, map[string]any{"playlistId": "PLx", "params": "wAEB8gECKAE%3D"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, reqs := serveFixture(t, "watch_playlist_radio")
			if _, err := a.WatchPlaylist(context.Background(), c.req); err != nil {
				t.Fatal(err)
			}
			body := reqs()[0].body
			for k, v := range c.want {
				if body[k] != v {
					t.Errorf("%s = %v, want %v", k, body[k], v)
				}
			}
		})
	}
	a, _ := serveFixture(t, "watch_radio")
	if _, err := a.WatchPlaylist(context.Background(), WatchRequest{}); !errors.Is(err, ErrInvalidID) {
		t.Errorf("empty request: %v", err)
	}
}

func TestWatchPlaylistFollowsRadioContinuation(t *testing.T) {
	a, reqs := serveFixture(t, "watch_radio", "watch_radio_cont")
	// The fixture's 6 tracks are fewer than the limit: one continuation.
	srvCont := loadFixture(t, "watch_radio").get("contents", "singleColumnMusicWatchNextResultsRenderer", "tabbedRenderer",
		"watchNextTabbedResultsRenderer", "tabs", 0, "tabRenderer", "content", "musicQueueRenderer", "content", "playlistPanelRenderer",
		"continuations", 0, "nextRadioContinuationData", "continuation").str()
	if srvCont == "" {
		t.Skip("fixture has no radio continuation")
	}
	w, err := a.WatchPlaylist(context.Background(), WatchRequest{VideoID: "abc", Radio: true, Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	rs := reqs()
	if len(rs) != 2 || !strings.Contains(rs[1].query, "ctoken="+srvCont) || len(w.Tracks) != 12 {
		t.Fatalf("%d requests, %d tracks, query %q", len(rs), len(w.Tracks), rs[len(rs)-1].query)
	}
}

type fakeWatcher struct {
	byID  map[string][]Track
	calls atomic.Int32
}

func (f *fakeWatcher) WatchPlaylist(_ context.Context, r WatchRequest) (WatchPlaylist, error) {
	f.calls.Add(1)
	ts, ok := f.byID[r.VideoID]
	if !ok {
		return WatchPlaylist{}, errors.New("boom")
	}
	return WatchPlaylist{Tracks: ts}, nil
}

func TestRadioMergesSeeds(t *testing.T) {
	tr := func(ids ...string) []Track {
		var out []Track
		for _, id := range ids {
			out = append(out, Track{VideoID: id})
		}
		return out
	}
	f := &fakeWatcher{byID: map[string][]Track{"a": tr("a", "x", "y"), "b": tr("b", "x", "z")}}
	one, err := Radio(context.Background(), f, []string{"a"}, 25)
	if err != nil || len(one) != 3 || one[0].VideoID != "a" {
		t.Fatalf("single seed keeps order: %v %v", one, err)
	}
	many, err := Radio(context.Background(), f, []string{"a", "b", "bad"}, 25)
	if err != nil || len(many) != 5 {
		t.Fatalf("merged without duplicates, bad seed skipped: %v %v", many, err)
	}
	capped, _ := Radio(context.Background(), f, []string{"a", "b"}, 2)
	if len(capped) != 2 {
		t.Errorf("limit: %d", len(capped))
	}
	if _, err := Radio(context.Background(), f, []string{"bad"}, 25); err == nil {
		t.Error("all seeds failed: want error")
	}
}

func TestHome(t *testing.T) {
	a, reqs := serveFixture(t, "home")
	shelves, err := a.Home(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "home", shelves)
	if len(reqs()) != 1 {
		t.Errorf("enough shelves on page 1: %d requests", len(reqs()))
	}
	if len(shelves) < 2 || shelves[0].Title == "" {
		t.Fatalf("shelves: %+v", shelves)
	}
	kinds := map[ResultType]int{}
	for _, s := range shelves {
		for _, it := range s.Items {
			kinds[it.Type]++
			if it.Title() == "" {
				t.Errorf("untitled item in %q: %+v", s.Title, it)
			}
		}
	}
	if kinds[TypeSong] == 0 || kinds[TypePlaylist] == 0 {
		t.Errorf("item kinds: %v", kinds)
	}
	// More shelves than page 1 holds: follows the section continuation.
	a, reqs = serveFixture(t, "home", "home")
	if _, err := a.Home(context.Background(), 50); err != nil {
		t.Fatal(err)
	}
	if rs := reqs(); len(rs) < 2 || !strings.Contains(rs[1].query, "ctoken=") {
		t.Errorf("continuation not requested: %d", len(rs))
	}
}

func TestCharts(t *testing.T) {
	a, reqs := serveFixture(t, "charts_us")
	ch, err := a.Charts(context.Background(), "us")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "charts_us", ch)
	if ch.Country != "United States" || len(ch.Playlists) < 2 || len(ch.Artists) == 0 {
		t.Fatalf("charts: %+v", ch)
	}
	for _, p := range ch.Playlists {
		if p.ID == "" || p.Title == "" || strings.HasPrefix(p.ID, "VL") {
			t.Errorf("chart playlist: %+v", p)
		}
	}
	fd := reqs()[0].body["formData"].(map[string]any)["selectedValues"].([]any)
	if fd[0] != "US" {
		t.Errorf("country sent: %v", fd)
	}
	a, _ = serveFixture(t, "charts")
	g, err := a.Charts(context.Background(), "")
	if err != nil || g.Country != "Global" || len(g.Playlists) == 0 {
		t.Errorf("global: %+v %v", g, err)
	}
}

func TestExplore(t *testing.T) {
	a, _ := serveFixture(t, "explore")
	ex, err := a.Explore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "explore", ex)
	if len(ex.NewReleases) == 0 || !strings.HasPrefix(ex.Trending.BrowseID, "OLAK5uy_") || len(ex.Trending.Items) == 0 || len(ex.NewVideos) == 0 {
		t.Fatalf("explore: %d releases, trending %q (%d), %d videos", len(ex.NewReleases), ex.Trending.BrowseID, len(ex.Trending.Items), len(ex.NewVideos))
	}
	for _, al := range ex.NewReleases {
		if !strings.HasPrefix(al.ID, "MPRE") || al.Name == "" {
			t.Errorf("release: %+v", al)
		}
	}
}

func TestHistory(t *testing.T) {
	a, _ := serveFixture(t, "history_synthetic")
	h, err := a.History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 4 || h[0].Played != "Today" || h[3].Played != "Yesterday" || h[0].VideoID == "" {
		t.Fatalf("history: %+v", h)
	}
	a, _ = serveFixture(t, "history_unauth")
	if _, err := a.History(context.Background()); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("signed-out page: %v", err)
	}
	anon := New(Options{})
	defer anon.Close()
	if _, err := anon.History(context.Background()); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("no credentials: %v", err)
	}
}

// tlsAll routes every host to srv, so the playback-tracking URL's
// youtube.com host reaches the test server.
func tlsAll(srv *httptest.Server) *http.Client {
	addr := srv.Listener.Addr().String()
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test server
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}}
}

func TestAddHistoryItem(t *testing.T) {
	var ping atomic.Value
	status := http.StatusNoContent
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/player") {
			_, _ = w.Write(fixtureBytes(t, "player_synthetic"))
			return
		}
		ping.Store(r.Host + r.URL.Path + "?" + r.URL.RawQuery + " cookie=" + r.Header.Get("Cookie"))
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	a := New(Options{
		BaseURL: srv.URL + "/youtubei/v1/", HTTP: tlsAll(srv), RatePerSec: 1000, Burst: 1000,
		Credentials: &Credentials{Cookie: "__Secure-3PAPISID=x; SID=y"},
	})
	t.Cleanup(func() { _ = a.Close() })

	if err := a.AddHistoryItem(context.Background(), "VIDEO"); err != nil {
		t.Fatal(err)
	}
	got, _ := ping.Load().(string)
	for _, want := range []string{"s.youtube.com/api/stats/playback?", "docid=VIDEO", "ver=2", "c=WEB_REMIX", "cpn=", "SID=y"} {
		if !strings.Contains(got, want) {
			t.Errorf("ping %q lacks %q", got, want)
		}
	}
	status = http.StatusForbidden
	if err := a.AddHistoryItem(context.Background(), "VIDEO"); err == nil {
		t.Error("non-204: want error")
	}
	if err := a.AddHistoryItem(context.Background(), "bad id!"); !errors.Is(err, ErrInvalidID) {
		t.Errorf("bad id: %v", err)
	}
}

func TestAddHistoryItemRefusesForeignHost(t *testing.T) {
	srv, reqs := server(t, func(w http.ResponseWriter, r recorded, n int) {
		_, _ = w.Write([]byte(`{"playbackTracking":{"videostatsPlaybackUrl":{"baseUrl":"https://evil.example/stats"}}}`))
	})
	a := newTestAPI(t, srv, Options{Credentials: &Credentials{Cookie: "__Secure-3PAPISID=x"}})
	if err := a.AddHistoryItem(context.Background(), "VIDEO"); err == nil || len(reqs()) != 1 {
		t.Errorf("foreign host must not be pinged: %v, %d requests", err, len(reqs()))
	}
}

func TestCPN(t *testing.T) {
	c := cpn()
	if len(c) != 16 || strings.Trim(c, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") != "" {
		t.Errorf("cpn %q", c)
	}
}
