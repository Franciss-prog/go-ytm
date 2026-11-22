package ytmusic

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Lyrics (M8), ported from ytmusicapi mixins/browsing.py get_lyrics and
// models/lyrics.py LyricLine.from_raw. Timed lyrics are only served to the
// Android app client (ytmusicapi as_mobile: ANDROID_MUSIC 7.21.50). Shapes
// verified against live captures (TestRecordLyrics, 2026-11-22); the plain
// source line sits under the shelf's "footer", not its top-level runs.

// Lyrics is a track's lyrics from YouTube Music.
type Lyrics struct {
	Lines  []LyricLine `json:"lines"`
	Timed  bool        `json:"timed"`            // every line has a start time
	Source string      `json:"source,omitempty"` // e.g. "Source: LyricFind"
}

// LyricLine is one line; Start is zero for untimed lyrics.
type LyricLine struct {
	Start time.Duration `json:"start"`
	Text  string        `json:"text"`
}

var androidClient = map[string]any{"clientName": "ANDROID_MUSIC", "clientVersion": "7.21.50"}

// Lyrics fetches the lyrics page browseID (WatchPlaylist.LyricsID). It asks
// for timed lyrics first and falls back to the web page's plain text. It
// returns nil, nil when YouTube has no lyrics for the track.
func (a *API) Lyrics(ctx context.Context, browseID string) (*Lyrics, error) {
	if browseID == "" {
		return nil, nil
	}
	resp, err := a.read(ctx, "browse", map[string]any{"browseId": browseID, clientOverride: androidClient}, "")
	if err != nil {
		return nil, fmt.Errorf("ytmusic: timed lyrics: %w", err)
	}
	if l := parseTimedLyrics(resp); l != nil {
		return l, nil
	}
	if l := parsePlainLyrics(resp); l != nil {
		return l, nil
	}
	resp, err = a.read(ctx, "browse", map[string]any{"browseId": browseID}, "")
	if err != nil {
		return nil, fmt.Errorf("ytmusic: lyrics: %w", err)
	}
	return parsePlainLyrics(resp), nil
}

func parseTimedLyrics(resp node) *Lyrics {
	data := resp.get("contents", "elementRenderer", "newElement", "type", "componentType", "model", "timedLyricsModel", "lyricsData")
	raw := data.get("timedLyricsData").list()
	if len(raw) == 0 {
		return nil
	}
	out := &Lyrics{Timed: true, Source: data.get("sourceMessage").str()}
	for _, r := range raw {
		text := r.get("lyricLine").str()
		ms, err := strconv.ParseInt(r.get("cueRange", "startTimeMilliseconds").str(), 10, 64)
		if err != nil {
			out.Timed = false // ytmusicapi: any line without a cueRange makes the whole set plain
		}
		out.Lines = append(out.Lines, LyricLine{Start: time.Duration(ms) * time.Millisecond, Text: text})
	}
	if !out.Timed {
		for i := range out.Lines {
			out.Lines[i].Start = 0
		}
	}
	return out
}

func parsePlainLyrics(resp node) *Lyrics {
	shelf := resp.get("contents", "sectionListRenderer", "contents", 0, "musicDescriptionShelfRenderer")
	text := shelf.get("description").runsText()
	if strings.TrimSpace(text) == "" {
		return nil
	}
	out := &Lyrics{Source: shelf.get("footer").runsText()}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		out.Lines = append(out.Lines, LyricLine{Text: line})
	}
	return out
}
