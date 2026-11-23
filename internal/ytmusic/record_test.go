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

// TestRecordM6 captures the M6 read-only calls: watch playlists (radio,
// up next, playlist radio), home, charts, explore, history and player.
func TestRecordM6(t *testing.T) {
	dir := os.Getenv("YTM_RECORD_DIR")
	if dir == "" {
		t.Skip("set YTM_RECORD_DIR")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	creds := recordCreds(t)
	a := New(Options{Credentials: creds})
	defer a.Close()

	call := func(name, endpoint string, body map[string]any, q string) node {
		t.Helper()
		n, err := a.read(ctx, endpoint, body, q)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, _ := json.MarshalIndent(n.v, "", " ")
		if err := os.WriteFile(filepath.Join(dir, name+".json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("saved %s (%d bytes)", name, len(b))
		return n
	}

	songs, err := a.Search(ctx, "daft punk get lucky", FilterSongs, 1)
	if err != nil || len(songs.Items) == 0 {
		t.Fatalf("song search: %v", err)
	}
	vid := songs.Items[0].Track.VideoID
	radio := call("watch_radio", "next", map[string]any{
		"enablePersistentPlaylistPanel": true, "isAudioOnly": true, "tunerSettingValue": "AUTOMIX_SETTING_NORMAL",
		"videoId": vid, "playlistId": "RDAMVM" + vid, "params": "wAEB",
	}, "")
	panel := radio.get("contents", "singleColumnMusicWatchNextResultsRenderer", "tabbedRenderer", "watchNextTabbedResultsRenderer", "tabs", 0, "tabRenderer", "content", "musicQueueRenderer", "content", "playlistPanelRenderer")
	if tok := nextContinuation(panel); tok != "" {
		call("watch_radio_cont", "next", map[string]any{
			"enablePersistentPlaylistPanel": true, "isAudioOnly": true, "tunerSettingValue": "AUTOMIX_SETTING_NORMAL",
			"videoId": vid, "playlistId": "RDAMVM" + vid, "params": "wAEB",
		}, ctokenQuery(tok)+"Radio")
	}
	call("watch_playlist_radio", "next", map[string]any{
		"enablePersistentPlaylistPanel": true, "isAudioOnly": true, "tunerSettingValue": "AUTOMIX_SETTING_NORMAL",
		"playlistId": "RDAMPLPL4fGSI1pDJn6O1LS0XSdF3RyO0Rq_LDeI", "params": "wAEB",
	}, "")

	call("home", "browse", map[string]any{"browseId": "FEmusic_home"}, "")
	call("charts", "browse", map[string]any{"browseId": "FEmusic_charts", "formData": map[string]any{"selectedValues": []string{"ZZ"}}}, "")
	call("charts_us", "browse", map[string]any{"browseId": "FEmusic_charts", "formData": map[string]any{"selectedValues": []string{"US"}}}, "")
	call("explore", "browse", map[string]any{"browseId": "FEmusic_explore"}, "")
	days := int(time.Now().Unix()/86400) - 1
	call("player", "player", map[string]any{
		"playbackContext": map[string]any{"contentPlaybackContext": map[string]any{"signatureTimestamp": days}},
		"video_id":        vid,
	}, "")
	if creds == nil {
		t.Log("no credentials: history skipped")
		return
	}
	call("history", "browse", map[string]any{"browseId": "FEmusic_history"}, "")
	call("liked", "browse", map[string]any{"browseId": "VLLM"}, "")
}

// TestRecordLyrics captures the M8 lyrics browse page twice: with the
// Android app client (timed lyrics, ytmusicapi as_mobile) and the web
// client (plain lyrics). Public, no sign-in needed.
func TestRecordLyrics(t *testing.T) {
	dir := os.Getenv("YTM_RECORD_DIR")
	if dir == "" {
		t.Skip("set YTM_RECORD_DIR")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	a := New(Options{})
	defer a.Close()

	for _, q := range []string{"daft punk get lucky", "bohemian rhapsody queen"} {
		songs, err := a.Search(ctx, q, FilterSongs, 1)
		if err != nil || len(songs.Items) == 0 {
			t.Fatalf("song search %q: %v", q, err)
		}
		w, err := a.WatchPlaylist(ctx, WatchRequest{VideoID: songs.Items[0].Track.VideoID, Limit: 1})
		if err != nil || w.LyricsID == "" {
			t.Fatalf("watch %q: lyrics id %q, %v", q, w.LyricsID, err)
		}
		slug := strings.ReplaceAll(strings.Fields(q)[len(strings.Fields(q))-1], " ", "_")
		for name, body := range map[string]map[string]any{
			"lyrics_timed_" + slug: {"browseId": w.LyricsID, clientOverride: map[string]any{"clientName": "ANDROID_MUSIC", "clientVersion": "7.21.50"}},
			"lyrics_plain_" + slug: {"browseId": w.LyricsID},
		} {
			n, err := a.read(ctx, "browse", body, "")
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			b, _ := json.MarshalIndent(n.v, "", " ")
			if err := os.WriteFile(filepath.Join(dir, name+".json"), b, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Logf("saved %s (%d bytes)", name, len(b))
		}
	}
}
