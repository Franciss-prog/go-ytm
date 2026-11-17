//go:build record

package ytmusic

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestRecordBrowse captures raw browse responses for fixtures. It only
// reads (no playlist edits, no ratings). Output is UNSCRUBBED and may hold
// account data: write it outside the repo, then scrub into testdata.
//
//	YTM_RECORD_DIR=/some/scratch go test -tags record -run Record ./internal/ytmusic
//
// YTM_AUTH (default ~/.config/ytm-go/auth.json) signs in for the library
// captures; without it only public pages are recorded.
func TestRecordBrowse(t *testing.T) {
	dir := os.Getenv("YTM_RECORD_DIR")
	if dir == "" {
		t.Skip("set YTM_RECORD_DIR")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	creds := recordCreds(t)
	a := New(Options{Credentials: creds})
	defer a.Close()

	save := func(name string, n node) {
		t.Helper()
		b, err := json.MarshalIndent(n.v, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("saved %s (%d bytes)", name, len(b))
	}
	browse := func(name string, body map[string]any) node {
		t.Helper()
		n, err := a.read(ctx, "browse", body, "")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		save(name, n)
		return n
	}

	albums, err := a.Search(ctx, "daft punk random access memories", FilterAlbums, 5)
	if err != nil || len(albums.Items) == 0 {
		t.Fatalf("album search: %v", err)
	}
	alb := albums.Items[0].Album
	browse("album", map[string]any{"browseId": alb.ID})
	if alb.PlaylistID != "" {
		browse("playlist_audio", map[string]any{"browseId": "VL" + alb.PlaylistID})
	}

	artists, err := a.Search(ctx, "daft punk", FilterArtists, 3)
	if err != nil || len(artists.Items) == 0 {
		t.Fatalf("artist search: %v", err)
	}
	art := browse("artist", map[string]any{"browseId": artists.Items[0].Artist.ID})
	for _, sec := range sectionContents(art) {
		title := sec.get("musicCarouselShelfRenderer", "header", "musicCarouselShelfBasicHeaderRenderer", "title", "runs", 0)
		if strings.EqualFold(title.get("text").str(), "albums") {
			be := title.get("navigationEndpoint", "browseEndpoint")
			if be.exists() {
				browse("artist_albums", map[string]any{"browseId": be.get("browseId").str(), "params": be.get("params").str()})
			}
		}
	}

	// A public playlist long enough to need a continuation page.
	var pls []SearchResult
	for _, q := range []string{"500 greatest songs of all time", "1000 songs", "top 100 songs global"} {
		res, err := a.Search(ctx, q, FilterPlaylists, 10)
		if err != nil {
			t.Fatalf("playlist search: %v", err)
		}
		pls = append(pls, res.Items...)
	}
	for _, r := range pls {
		n, err := a.read(ctx, "browse", map[string]any{"browseId": "VL" + r.Playlist.ID}, "")
		if err != nil {
			t.Fatalf("playlist %s: %v", r.Playlist.ID, err)
		}
		tok := lastContinuation(n)
		if tok == "" {
			continue
		}
		save("playlist", n)
		browse("playlist_cont", map[string]any{"continuation": tok})
		break
	}

	if creds == nil {
		t.Log("no credentials: library captures skipped")
		return
	}
	lib := browse("library_playlists", map[string]any{"browseId": "FEmusic_liked_playlists"})
	if tok := first(sectionContents(lib)).get("gridRenderer", "continuations", 0, "nextContinuationData", "continuation").str(); tok != "" {
		n, err := a.read(ctx, "browse", map[string]any{"browseId": "FEmusic_liked_playlists"}, "&ctoken="+tok+"&continuation="+tok+"&type=next")
		if err != nil {
			t.Logf("library continuation skipped: %v", err)
		} else {
			save("library_playlists_cont", n)
		}
	}
	browse("library_albums", map[string]any{"browseId": "FEmusic_liked_albums"})
	browse("library_artists", map[string]any{"browseId": "FEmusic_library_corpus_track_artists"})

	// First playlist the account owns (its menu offers the editor).
	raw, _ := json.Marshal(lib.v)
	if m := regexp.MustCompile(`"playlistEditorEndpoint":\{"playlistId":"([^"]+)"`).FindSubmatch(raw); m != nil {
		browse("playlist_owned", map[string]any{"browseId": "VL" + string(m[1])})
	}
}

func recordCreds(t *testing.T) *Credentials {
	path := os.Getenv("YTM_AUTH")
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".config", "ytm-go", "auth.json")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Logf("no auth file: %v", err)
		return nil
	}
	var h struct {
		Cookie    string `json:"cookie"`
		UserAgent string `json:"user-agent"`
		AuthUser  string `json:"x-goog-authuser"`
	}
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatalf("auth file: %v", err)
	}
	slot, _ := strconv.Atoi(h.AuthUser)
	return &Credentials{Cookie: h.Cookie, UserAgent: h.UserAgent, AuthUser: slot}
}

func sectionContents(resp node) []node {
	if l := resp.get("contents", "singleColumnBrowseResultsRenderer", "tabs", 0, "tabRenderer", "content", "sectionListRenderer", "contents").list(); l != nil {
		return l
	}
	return resp.get("contents", "twoColumnBrowseResultsRenderer", "tabs", 0, "tabRenderer", "content", "sectionListRenderer", "contents").list()
}

func lastContinuation(resp node) string {
	shelf := resp.get("contents", "twoColumnBrowseResultsRenderer", "secondaryContents", "sectionListRenderer", "contents", 0, "musicPlaylistShelfRenderer", "contents")
	return shelf.get(-1, "continuationItemRenderer", "continuationEndpoint", "continuationCommand", "token").str()
}
