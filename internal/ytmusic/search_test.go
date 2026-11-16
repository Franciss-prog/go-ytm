package ytmusic

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite golden files")

func loadFixture(t testing.TB, name string) node {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "responses", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return node{v}
}

func golden(t *testing.T, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "golden", name+".json")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s: output differs from golden file; run go test ./internal/ytmusic -update and review the diff", path)
	}
}

func TestParseSearchGolden(t *testing.T) {
	cases := []struct {
		fixture string
		filter  SearchFilter
	}{
		{"search_all", FilterAll},
		{"search_songs", FilterSongs},
		{"search_videos", FilterVideos},
		{"search_albums", FilterAlbums},
		{"search_artists", FilterArtists},
		{"search_playlists", FilterPlaylists},
		{"search_empty", FilterAll},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			var p parser
			res := p.parseSearch(loadFixture(t, c.fixture), c.filter)
			if len(res.Items) == 0 {
				t.Fatal("no results parsed")
			}
			if len(p.unknown) > 0 {
				t.Errorf("unknown shapes: %v", p.unknown)
			}
			for i, r := range res.Items {
				if r.Title() == "" {
					t.Errorf("item %d (%s) has no title", i, r.Type)
				}
				if c.filter != FilterAll && r.Type != c.filter.resultType() && (c.filter != FilterSongs || r.Type != TypeVideo) {
					t.Errorf("item %d type %s under filter %s", i, r.Type, c.filter)
				}
				if r.Track != nil && r.Track.VideoID == "" {
					t.Errorf("item %d track without video id", i)
				}
			}
			if c.filter != FilterAll && c.filter != FilterArtists && res.Continuation == "" {
				t.Error("filtered search lost its continuation token")
			}
			golden(t, c.fixture, res.Items)
		})
	}
}

func TestParseSearchSongFields(t *testing.T) {
	var p parser
	res := p.parseSearch(loadFixture(t, "search_songs"), FilterSongs)
	got := res.Items[0].Track
	want := Track{
		VideoID: "-xFur30vvVc", Title: "Touch (2021 Epilogue) (feat. Paul Williams)",
		Artists:  []Artist{{"Daft Punk", "UCRr1xG_2WIDs18a6cIiCxeA"}, {"Paul Williams", "UCPtp29kketDMCRawl_lT2oQ"}},
		Album:    &AlbumRef{Name: "Random Access Memories (10th Anniversary Edition)", ID: "MPREb_eMBjPmWySjR"},
		Duration: 3 * time.Minute, Views: "33M", VideoType: videoTypeATV, Available: true,
	}
	if got == nil {
		t.Fatal("first result is not a track")
	}
	got.ThumbnailURL = ""
	gj, _ := json.Marshal(got)
	wj, _ := json.Marshal(want)
	if !bytes.Equal(gj, wj) {
		t.Errorf("got  %s\nwant %s", gj, wj)
	}
}

func TestParseSearchUnfilteredTypes(t *testing.T) {
	var p parser
	res := p.parseSearch(loadFixture(t, "search_all"), FilterAll)
	if top := res.Items[0]; !top.Top || top.Type != TypeArtist || top.Artist.ID != "UCRr1xG_2WIDs18a6cIiCxeA" {
		t.Errorf("top result = %+v", top)
	}
	// Card children list no artist; they inherit the card's.
	if tr := res.Items[1].Track; tr == nil || len(tr.Artists) != 1 || tr.Artists[0].Name != "Daft Punk" {
		t.Errorf("card child = %+v", res.Items[1])
	}
	counts := map[ResultType]int{}
	for _, r := range res.Items {
		counts[r.Type]++
		if r.Album != nil && r.Album.Kind == "" {
			t.Errorf("album %q lost its kind", r.Album.Name)
		}
		if r.Track != nil {
			for _, a := range r.Track.Artists {
				if a.Name == "Song" || a.Name == "Video" {
					t.Errorf("type label parsed as artist in %q", r.Track.Title)
				}
			}
		}
	}
	for _, typ := range []ResultType{TypeSong, TypeVideo, TypeAlbum, TypeArtist, TypePlaylist} {
		if counts[typ] == 0 {
			t.Errorf("no %s results in unfiltered search", typ)
		}
	}
}

func TestParseSearchContinuation(t *testing.T) {
	var p parser
	res := p.parseSearchContinuation(loadFixture(t, "search_songs_cont"), FilterSongs)
	if len(res.Items) != 20 || res.Continuation == "" {
		t.Fatalf("items=%d continuation=%q", len(res.Items), res.Continuation)
	}
}

func TestParseSuggestions(t *testing.T) {
	got := parseSuggestions(loadFixture(t, "suggestions"))
	if len(got) == 0 || got[0] == "" {
		t.Fatalf("suggestions = %q", got)
	}
	golden(t, "suggestions", got)
}

func TestParseAccount(t *testing.T) {
	info, ok := parseAccount(loadFixture(t, "account_synthetic"))
	want := AccountInfo{Name: "Jane Doe", Handle: "@jane", ChannelID: "UCabcdefghijklmnopqrstuv", PhotoURL: "https://yt3.ggpht.com/example=s88"}
	if !ok || info != want {
		t.Errorf("got %+v ok=%v", info, ok)
	}
	if _, ok := parseAccount(loadFixture(t, "account_unauth")); ok {
		t.Error("signed-out account menu parsed as an account")
	}
}

func TestNodeNeverPanics(t *testing.T) {
	var n node
	if n.get("a", 0, "b", -1).str() != "" || n.list() != nil || n.firstKey() != "" || n.runsText() != "" {
		t.Error("zero node returned data")
	}
	v := node{map[string]any{"a": []any{"x"}}}
	if v.get("a", -1).str() != "x" || v.get("a", 5).exists() || v.get("a", "k").exists() || v.get(1.5).exists() {
		t.Error("bad navigation")
	}
}

func TestParseRunsAndHelpers(t *testing.T) {
	cases := []struct {
		in   string
		secs int
		ok   bool
	}{{"3:09", 189, true}, {"1:02:03", 3723, true}, {"3", 0, false}, {"a:b", 0, false}, {"1::2", 0, false}}
	for _, c := range cases {
		if s, ok := parseDuration(c.in); s != c.secs || ok != c.ok {
			t.Errorf("parseDuration(%q) = %d,%v", c.in, s, ok)
		}
	}
	for in, want := range map[string]string{"22M views": "22M", "1.9B plays": "1.9B", "676 views": "676", "7clouds Dance": "", "2Pac": ""} {
		if got := viewsCount(in); got != want {
			t.Errorf("viewsCount(%q) = %q", in, got)
		}
	}
	if _, err := ParseFilter("bogus"); err == nil {
		t.Error("bad filter accepted")
	}
	for _, s := range []string{"", "all", "Songs"} {
		if _, err := ParseFilter(s); err != nil {
			t.Errorf("ParseFilter(%q): %v", s, err)
		}
	}
}

func TestSearchResultJSON(t *testing.T) {
	r := SearchResult{Type: TypeArtist, Top: true, Artist: &ArtistRef{Name: "A", ID: "UC1"}}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"type":"artist","top":true,"name":"A","id":"UC1"}`; string(b) != want {
		t.Errorf("got %s want %s", b, want)
	}
	if _, err := json.Marshal(SearchResult{Type: TypeSong}); err == nil {
		t.Error("empty result marshalled")
	}
}

func FuzzParseSearch(f *testing.F) {
	f.Add([]byte(`{"contents":{"tabbedSearchResultsRenderer":{"tabs":[{"tabRenderer":{"content":{"sectionListRenderer":{"contents":[{"musicShelfRenderer":{"contents":[{"musicResponsiveListItemRenderer":{}}]}}]}}}}]}}}`))
	f.Add([]byte(`{"contents":[]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var v any
		if json.Unmarshal(data, &v) != nil {
			return
		}
		var p parser
		for _, flt := range append([]SearchFilter{FilterAll}, Filters...) {
			p.parseSearch(node{v}, flt)
			p.parseSearchContinuation(node{v}, flt)
		}
		parseSuggestions(node{v})
		parseAccount(node{v})
	})
}

func BenchmarkParseSearch(b *testing.B) {
	n := loadFixture(b, "search_all")
	b.ReportAllocs()
	for b.Loop() {
		var p parser
		p.parseSearch(n, FilterAll)
	}
}
