package ytmusic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// Fixtures: recorded 2026-11-17 with record_test.go (signed in), scrubbed
// with scripts/scrubfixtures. library_* and playlist_owned are anonymized
// (texts "Text N", fake ids); lists are cut to 6 entries.

func TestParseBrowseGolden(t *testing.T) {
	var p parser
	golden(t, "album", p.parseAlbum(loadFixture(t, "album"), "MPREb_test"))
	golden(t, "playlist", p.parsePlaylist(loadFixture(t, "playlist"), "PLtest"))
	golden(t, "playlist_owned", p.parsePlaylist(loadFixture(t, "playlist_owned"), "PLfake0000003"))
	golden(t, "playlist_audio", p.parsePlaylist(loadFixture(t, "playlist_audio"), "OLAK5uy_test"))
	golden(t, "artist", p.parseArtist(loadFixture(t, "artist"), "UCtest"))

	cont := loadFixture(t, "playlist_cont").get("onResponseReceivedActions", 0, "appendContinuationItemsAction", "continuationItems").list()
	golden(t, "playlist_cont", p.parsePlaylistItems(cont, false))
	golden(t, "artist_albums", p.parseTwoRowAlbums(first(singleColumnSections(loadFixture(t, "artist_albums"))).get("gridRenderer", "items").list()))
	golden(t, "library_playlists", p.parseTwoRowPlaylists(libraryContents(loadFixture(t, "library_playlists"), "gridRenderer").get("items").list()))
	golden(t, "library_playlists_cont", p.parseTwoRowPlaylists(loadFixture(t, "library_playlists_cont").get("continuationContents", "gridContinuation", "items").list()))
	golden(t, "library_albums", p.parseTwoRowAlbums(libraryContents(loadFixture(t, "library_albums"), "gridRenderer").get("items").list()))
	golden(t, "library_artists", p.parseLibraryArtists(libraryContents(loadFixture(t, "library_artists"), "musicShelfRenderer").get("contents").list()))
	if len(p.unknown) > 0 {
		t.Errorf("unknown shapes: %v", p.unknown)
	}
}

// TestParseBrowseFacts pins the facts the goldens should show, so a
// regenerated golden cannot silently lose them.
func TestParseBrowseFacts(t *testing.T) {
	var p parser
	al := p.parseAlbum(loadFixture(t, "album"), "MPREb_x")
	if al.Name != "Random Access Memories" || al.Kind != "Album" || al.Year != "2013" || al.TrackCount != 13 ||
		al.Duration != "1 hour, 14 minutes" || len(al.Artists) != 1 || al.Artists[0].Name != "Daft Punk" ||
		!strings.HasPrefix(al.PlaylistID, "OLAK5uy_") || len(al.Tracks) != 6 {
		t.Errorf("album header: %+v", al.AlbumRef)
	}
	for _, tr := range al.Tracks {
		if tr.VideoID == "" || tr.Duration == 0 || tr.Album == nil || tr.Album.ID != "MPREb_x" || len(tr.Artists) == 0 {
			t.Errorf("album track: %+v", tr)
		}
	}

	pl := p.parsePlaylist(loadFixture(t, "playlist"), "PLx")
	if pl.Owned || pl.TrackCount != 500 || pl.Views != "14M views" || pl.Duration != "35 hours" || pl.Continuation == "" || pl.Author == "" || len(pl.Tracks) != 6 {
		t.Errorf("public playlist: %+v", pl.PlaylistRef)
	}
	own := p.parsePlaylist(loadFixture(t, "playlist_owned"), "PLfake0000003")
	if !own.Owned || own.Privacy != PrivacyPrivate || own.TrackCount != 1 || own.Duration != "4 minutes, 25 seconds" ||
		len(own.Tracks) != 1 || own.Tracks[0].SetVideoID == "" || own.Continuation != "" {
		t.Errorf("owned playlist: %+v %+v", own, own.Tracks)
	}
	audio := p.parsePlaylist(loadFixture(t, "playlist_audio"), "OLAK5uy_x")
	if audio.Title != "Random Access Memories" || len(audio.Tracks) == 0 || audio.Tracks[0].Album == nil {
		t.Errorf("audio playlist: %+v", audio.PlaylistRef)
	}

	ar := p.parseArtist(loadFixture(t, "artist"), "UCx")
	if ar.Name != "Daft Punk" || ar.Audience != "65.9M" || len(ar.TopSongs.Items) != 5 || ar.TopSongs.BrowseID == "" ||
		len(ar.Albums.Items) == 0 || ar.Albums.Params == "" || len(ar.Singles.Items) == 0 || len(ar.Videos.Items) == 0 {
		t.Errorf("artist: %+v", ar)
	}
	for _, a := range ar.Albums.Items {
		if !strings.HasPrefix(a.ID, "MPRE") || a.Name == "" {
			t.Errorf("artist album card: %+v", a)
		}
	}

	libs := p.parseTwoRowPlaylists(libraryContents(loadFixture(t, "library_playlists"), "gridRenderer").get("items").list())
	if len(libs) == 0 || libs[0].Title != "Liked Music" || libs[0].ID != "LM" {
		t.Fatalf("library playlists: %+v", libs)
	}
	owned := 0
	for _, l := range libs {
		if l.Owned {
			owned++
		}
	}
	if owned == 0 {
		t.Error("no owned playlist detected in library")
	}
	if p.parseLibraryArtists(libraryContents(loadFixture(t, "library_artists"), "musicShelfRenderer").get("contents").list())[0].Audience != "55 songs" {
		t.Error("library artist subtitle")
	}
}

func TestPlaylistPaging(t *testing.T) {
	srv, reqs := server(t, func(w http.ResponseWriter, r recorded, n int) {
		switch n {
		case 1:
			_, _ = w.Write(fixtureBytes(t, "playlist"))
		default:
			_, _ = w.Write(fixtureBytes(t, "playlist_cont"))
		}
	})
	a := newTestAPI(t, srv, Options{})
	pl, err := a.Playlist(context.Background(), "VLPLabc")
	if err != nil {
		t.Fatal(err)
	}
	more, next, err := a.PlaylistMore(context.Background(), pl.Continuation)
	if err != nil || len(more) != 6 || next == "" {
		t.Fatalf("more: %d %q %v", len(more), next, err)
	}
	rs := reqs()
	if rs[0].body["browseId"] != "VLPLabc" {
		t.Errorf("first request browseId %v", rs[0].body["browseId"])
	}
	if rs[1].body["continuation"] != pl.Continuation || rs[1].body["browseId"] != nil {
		t.Errorf("continuation body %v", rs[1].body)
	}
	if tr, tok, err := a.PlaylistMore(context.Background(), ""); tr != nil || tok != "" || err != nil {
		t.Error("empty token should be a no-op")
	}
}

func TestLibraryFollowsGridContinuation(t *testing.T) {
	srv, reqs := server(t, func(w http.ResponseWriter, r recorded, n int) {
		if !strings.Contains(r.query, "ctoken=") {
			_, _ = w.Write(fixtureBytes(t, "library_playlists"))
			return
		}
		_, _ = w.Write(fixtureBytes(t, "library_playlists_cont"))
	})
	a := newTestAPI(t, srv, Options{Credentials: &Credentials{Cookie: "__Secure-3PAPISID=x"}})
	pls, err := a.LibraryPlaylists(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	rs := reqs()
	if len(rs) != 2 || !strings.Contains(rs[1].query, "ctoken=") || rs[1].body["browseId"] != "FEmusic_liked_playlists" {
		t.Fatalf("requests: %+v", rs)
	}
	if len(pls) < 7 {
		t.Errorf("got %d playlists across two pages", len(pls))
	}
	limited, _ := a.LibraryPlaylists(context.Background(), 2)
	if len(limited) != 2 {
		t.Errorf("limit 2: %d", len(limited))
	}
}

func TestLibraryNeedsSignIn(t *testing.T) {
	a := New(Options{})
	defer a.Close()
	if _, err := a.LibraryAlbums(context.Background(), 0); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v", err)
	}
}

func TestInvalidIDs(t *testing.T) {
	a := New(Options{})
	defer a.Close()
	ctx := context.Background()
	if _, err := a.Album(ctx, "OLAK5uy_x"); !errors.Is(err, ErrInvalidID) {
		t.Error("album id not checked")
	}
	if _, err := a.Artist(ctx, "MPREb_x"); !errors.Is(err, ErrInvalidID) {
		t.Error("artist id not checked")
	}
	if _, err := a.Playlist(ctx, "VL"); !errors.Is(err, ErrInvalidID) {
		t.Error("playlist id not checked")
	}
}

func TestWriteRequestBodies(t *testing.T) {
	var reply string
	srv, reqs := server(t, func(w http.ResponseWriter, r recorded, n int) { _, _ = w.Write([]byte(reply)) })
	a := newTestAPI(t, srv, Options{Credentials: &Credentials{Cookie: "__Secure-3PAPISID=x"}})
	ctx := context.Background()

	reply = `{"playlistId":"PLnew"}`
	if id, err := a.CreatePlaylist(ctx, " Mix ", "d", "", []string{"v1"}); err != nil || id != "PLnew" {
		t.Fatalf("create: %q %v", id, err)
	}
	reply = `{"status":"STATUS_SUCCEEDED"}`
	if err := a.AddPlaylistItems(ctx, "VLPLnew", []string{"v1", "v2"}, false); err != nil {
		t.Fatal(err)
	}
	if err := a.AddPlaylistItems(ctx, "PLnew", []string{"v1"}, true); err != nil {
		t.Fatal(err)
	}
	if err := a.RemovePlaylistItems(ctx, "PLnew", []Track{{VideoID: "v1", SetVideoID: "s1"}, {VideoID: "v2"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.DeletePlaylist(ctx, "VLPLnew"); err != nil {
		t.Fatal(err)
	}
	reply = `{}`
	for _, s := range []LikeStatus{Like, Dislike, LikeIndifferent} {
		if err := a.Rate(ctx, "v9", s); err != nil {
			t.Fatal(err)
		}
	}

	rs := reqs()
	want := []struct {
		path string
		body string
	}{
		{"/youtubei/v1/playlist/create", `"description":"d","privacyStatus":"PRIVATE","title":"Mix","videoIds":["v1"]`},
		{"/youtubei/v1/browse/edit_playlist", `"actions":[{"action":"ACTION_ADD_VIDEO","addedVideoId":"v1"},{"action":"ACTION_ADD_VIDEO","addedVideoId":"v2"}],"playlistId":"PLnew"`},
		{"/youtubei/v1/browse/edit_playlist", `"actions":[{"action":"ACTION_ADD_VIDEO","addedVideoId":"v1","dedupeOption":"DEDUPE_OPTION_SKIP"}],"playlistId":"PLnew"`},
		{"/youtubei/v1/browse/edit_playlist", `"actions":[{"action":"ACTION_REMOVE_VIDEO","removedVideoId":"v1","setVideoId":"s1"}],"playlistId":"PLnew"`},
		{"/youtubei/v1/playlist/delete", `"playlistId":"PLnew"`},
		{"/youtubei/v1/like/like", `"target":{"videoId":"v9"}`},
		{"/youtubei/v1/like/dislike", `"target":{"videoId":"v9"}`},
		{"/youtubei/v1/like/removelike", `"target":{"videoId":"v9"}`},
	}
	if len(rs) != len(want) {
		t.Fatalf("%d requests, want %d", len(rs), len(want))
	}
	for i, w := range want {
		if rs[i].path != w.path {
			t.Errorf("%d: path %s, want %s", i, rs[i].path, w.path)
		}
		body := bodyWithoutContext(t, rs[i].body)
		if !strings.Contains(body, w.body) {
			t.Errorf("%d: body %s\nwant fragment %s", i, body, w.body)
		}
	}
}

func bodyWithoutContext(t *testing.T, b map[string]any) string {
	t.Helper()
	delete(b, "context")
	out, err := jsonCompact(b)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestWriteFailures(t *testing.T) {
	srv, reqs := server(t, func(w http.ResponseWriter, r recorded, n int) {
		_, _ = w.Write([]byte(`{"status":"STATUS_FAILED"}`))
	})
	a := newTestAPI(t, srv, Options{Credentials: &Credentials{Cookie: "__Secure-3PAPISID=x"}})
	ctx := context.Background()
	if err := a.AddPlaylistItems(ctx, "PL", []string{"v"}, false); !errors.Is(err, ErrEditFailed) {
		t.Errorf("failed status: %v", err)
	}
	if err := a.AddPlaylistItems(ctx, "PL", []string{"v", "v"}, false); err == nil {
		t.Error("duplicate input accepted")
	}
	if err := a.RemovePlaylistItems(ctx, "PL", []Track{{VideoID: "v"}}); err == nil {
		t.Error("remove without set id accepted")
	}
	if _, err := a.CreatePlaylist(ctx, "a<b", "", "", nil); err == nil {
		t.Error("invalid title accepted")
	}
	if err := a.Rate(ctx, "v", LikeUnknown); err == nil {
		t.Error("unknown rating accepted")
	}
	if n := len(reqs()); n != 1 {
		t.Errorf("%d requests; local validation should not send", n)
	}
	anon := newTestAPI(t, srv, Options{})
	if err := anon.Rate(ctx, "v", Like); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("signed out write: %v", err)
	}
}

func jsonCompact(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}
