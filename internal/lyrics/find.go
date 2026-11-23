package lyrics

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// YouTube is the part of the YouTube Music client lyrics need.
type YouTube interface {
	WatchPlaylist(ctx context.Context, req ytmusic.WatchRequest) (ytmusic.WatchPlaylist, error)
	Lyrics(ctx context.Context, browseID string) (*ytmusic.Lyrics, error)
}

// Lookup is an LRCLIB-style lookup (*LRCLIB in production).
type Lookup interface {
	Get(ctx context.Context, title, artist, album string, dur time.Duration) (*Record, error)
}

// Track identifies the track to find lyrics for.
type Track struct {
	VideoID, Title, Artist, Album string
	Duration                      time.Duration
}

// Finder finds lyrics. Either source may be nil.
type Finder struct {
	YT     YouTube
	LRCLIB Lookup
}

// Find returns the best lyrics for t, in this order: YouTube timed, LRCLIB
// synced, YouTube plain, LRCLIB plain (ytm-player asks LRCLIB only when
// YouTube has no timestamps). It returns nil, nil when no source has
// lyrics, and an error only when nothing was found and a source failed.
func (f Finder) Find(ctx context.Context, t Track) (*Lyrics, error) {
	var errs []error
	var ytPlain *Lyrics
	if f.YT != nil && t.VideoID != "" {
		yt, err := f.youtube(ctx, t.VideoID)
		switch {
		case err != nil:
			errs = append(errs, err)
		case yt != nil && yt.Synced:
			return yt, nil
		default:
			ytPlain = yt
		}
	}
	var rec *Record
	if f.LRCLIB != nil && t.Title != "" && t.Artist != "" {
		var err error
		rec, err = f.LRCLIB.Get(ctx, SanitizeTitle(t.Title, t.Artist), t.Artist, t.Album, t.Duration)
		if err != nil {
			errs = append(errs, err)
		}
		if rec != nil && rec.SyncedLyrics != nil {
			if lines := ParseLRC(*rec.SyncedLyrics); len(lines) > 0 {
				return &Lyrics{Lines: lines, Synced: true, Source: "LRCLIB"}, nil
			}
		}
	}
	if ytPlain != nil {
		return ytPlain, nil
	}
	if rec != nil && rec.PlainLyrics != nil && strings.TrimSpace(*rec.PlainLyrics) != "" {
		return &Lyrics{Lines: plainLines(*rec.PlainLyrics), Source: "LRCLIB"}, nil
	}
	return nil, errors.Join(errs...)
}

func (f Finder) youtube(ctx context.Context, videoID string) (*Lyrics, error) {
	w, err := f.YT.WatchPlaylist(ctx, ytmusic.WatchRequest{VideoID: videoID, Limit: 1})
	if err != nil {
		return nil, err
	}
	l, err := f.YT.Lyrics(ctx, w.LyricsID)
	if err != nil || l == nil || len(l.Lines) == 0 {
		return nil, err
	}
	out := &Lyrics{Synced: l.Timed, Source: "YouTube Music"}
	if s := strings.TrimSpace(strings.TrimPrefix(l.Source, "Source:")); s != "" {
		out.Source += " · " + s
	}
	for _, ln := range l.Lines {
		out.Lines = append(out.Lines, Line{Start: ln.Start, Text: ln.Text})
	}
	return out, nil
}

func plainLines(s string) []Line {
	var out []Line
	for _, t := range strings.Split(strings.ReplaceAll(strings.TrimSpace(s), "\r\n", "\n"), "\n") {
		out = append(out, Line{Text: strings.TrimSpace(t)})
	}
	return out
}
