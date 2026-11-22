package ytmusic

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// Fixtures: recorded 2026-11-22 with TestRecordLyrics, cut down to the
// lyrics subtree and the first lines (lyrics are copyrighted).

func TestLyricsTimed(t *testing.T) {
	a, reqs := serveFixture(t, "lyrics_timed_queen")
	l, err := a.Lyrics(context.Background(), "MPLYt_x")
	if err != nil {
		t.Fatal(err)
	}
	if l == nil || !l.Timed || l.Source != "Source: Musixmatch" || len(l.Lines) != 4 {
		t.Fatalf("lyrics = %+v", l)
	}
	if l.Lines[0].Start != 170*time.Millisecond || l.Lines[0].Text != "Is this the real life? Is this just fantasy?" {
		t.Errorf("line 0 = %+v", l.Lines[0])
	}
	if l.Lines[3].Start != 25140*time.Millisecond {
		t.Errorf("line 3 start = %v", l.Lines[3].Start)
	}
	r := reqs()
	if len(r) != 1 {
		t.Fatalf("requests = %d, want 1", len(r))
	}
	client := r[0].body["context"].(map[string]any)["client"].(map[string]any)
	if client["clientName"] != "ANDROID_MUSIC" || client["clientVersion"] != "7.21.50" || r[0].body["browseId"] != "MPLYt_x" {
		t.Errorf("request = %+v", r[0].body)
	}
	if _, leaked := r[0].body[clientOverride]; leaked {
		t.Error("client override sent as a body field")
	}
}

func TestLyricsPlainFallback(t *testing.T) {
	// The app client answers without timed lyrics: fall back to the web page.
	srv, reqs := server(t, func(w http.ResponseWriter, r recorded, n int) {
		if n == 1 {
			_, _ = w.Write([]byte(`{"contents":{}}`))
			return
		}
		_, _ = w.Write(fixtureBytes(t, "lyrics_plain_lucky"))
	})
	a := newTestAPI(t, srv, Options{})
	l, err := a.Lyrics(context.Background(), "MPLYt_x")
	if err != nil {
		t.Fatal(err)
	}
	if l == nil || l.Timed || l.Source != "Source: LyricFind" || len(l.Lines) != 6 {
		t.Fatalf("lyrics = %+v", l)
	}
	if l.Lines[0].Text != "Like the legend of the phoenix" || l.Lines[4].Text != "" {
		t.Errorf("lines = %q", l.Lines) // \r\n split, blank stanza line kept
	}
	if c := reqs()[1].body["context"].(map[string]any)["client"].(map[string]any)["clientName"]; c != "WEB_REMIX" {
		t.Errorf("fallback client = %v", c)
	}
}

func TestLyricsNone(t *testing.T) {
	srv, reqs := server(t, func(w http.ResponseWriter, _ recorded, _ int) { _, _ = w.Write([]byte(`{"contents":{}}`)) })
	a := newTestAPI(t, srv, Options{})
	if l, err := a.Lyrics(context.Background(), "MPLYt_x"); l != nil || err != nil {
		t.Fatalf("Lyrics = %+v, %v; want nil, nil", l, err)
	}
	if l, err := a.Lyrics(context.Background(), ""); l != nil || err != nil || len(reqs()) != 2 {
		t.Fatalf("empty id: %+v, %v, %d requests", l, err, len(reqs()))
	}
}

func TestParseTimedLyricsMissingCue(t *testing.T) {
	n := node{v: map[string]any{"contents": map[string]any{"elementRenderer": map[string]any{"newElement": map[string]any{"type": map[string]any{"componentType": map[string]any{"model": map[string]any{"timedLyricsModel": map[string]any{"lyricsData": map[string]any{
		"timedLyricsData": []any{
			map[string]any{"lyricLine": "a", "cueRange": map[string]any{"startTimeMilliseconds": "100"}},
			map[string]any{"lyricLine": "b"},
		},
	}}}}}}}}}}
	l := parseTimedLyrics(n)
	if l == nil || l.Timed || l.Lines[0].Start != 0 || l.Lines[1].Text != "b" {
		t.Fatalf("lyrics = %+v", l)
	}
}
