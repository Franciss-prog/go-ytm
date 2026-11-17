// Package stream turns YouTube video IDs into playable audio URLs with
// yt-dlp, caches them until shortly before they expire, and prefetches the
// next tracks (docs/specs/03-ytmusic-api.md, "Stream resolution").
package stream

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/clock"
)

// Sentinel errors. Resolve wraps them; test with errors.Is.
var (
	// ErrUnavailable means the track cannot be played here: removed,
	// private, region-blocked, Premium-only or no audio format. Retrying
	// does not help; the queue skips it.
	ErrUnavailable = errors.New("track unavailable")
	// ErrSignInRequired means YouTube wants a signed-in session (age gate,
	// bot check, members-only). Enabling [yt_dlp] use_session_cookies after
	// `ytm setup` usually fixes it.
	ErrSignInRequired = errors.New("YouTube requires sign-in for this track")
	// ErrRateLimited means YouTube rate-limited the session; retrying within
	// seconds does not help.
	ErrRateLimited = errors.New("YouTube rate-limited stream requests")
	// ErrYtDlpNotFound means the yt-dlp binary could not be started.
	ErrYtDlpNotFound = errors.New("yt-dlp not found")
	// ErrInvalidID means the video ID is malformed; yt-dlp is not run.
	ErrInvalidID = errors.New("invalid video id")
	// ErrClosed is returned after Cache.Close.
	ErrClosed = errors.New("stream resolver closed")
)

// Quality is the [playback] audio_quality setting.
type Quality string

// Qualities.
const (
	High   Quality = "high"
	Medium Quality = "medium"
	Low    Quality = "low"
)

// combinedFallback is a muxed audio+video format for clients that expose no
// audio-only stream; mpv runs with --no-video so the video track is unused.
const combinedFallback = "best[vcodec!=none][acodec!=none]"

// Format returns the yt-dlp format selector for q, copied from ytm-player
// 2.2.0 services/stream.py QUALITY_FORMATS. Unknown values mean High.
func (q Quality) Format() string {
	switch q {
	case Medium:
		return "bestaudio[abr<=128]/bestaudio/" + combinedFallback
	case Low:
		return "bestaudio[abr<=64]/bestaudio/" + combinedFallback
	default:
		return "bestaudio/" + combinedFallback
	}
}

// ParseQuality maps a config value to a Quality (High when unknown).
func ParseQuality(s string) Quality {
	switch Quality(s) {
	case Medium, Low:
		return Quality(s)
	default:
		return High
	}
}

// Stream is a resolved, directly playable audio URL.
type Stream struct {
	VideoID string
	URL     string
	// Headers are the HTTP headers yt-dlp would send (User-Agent, …).
	// googlevideo URLs play without them; they are kept for diagnostics.
	Headers map[string]string
	// Expires is when the URL stops working (the URL's expire parameter).
	Expires time.Time
	// AvailableAt is when googlevideo starts serving the URL; before it,
	// requests get 403 (streams behind a pre-roll ad). Zero means now.
	AvailableAt time.Time
	Codec       string // "opus", "mp4a.40.2", or the container when unknown
	Bitrate     int    // kbit/s, 0 when unknown
	Duration    time.Duration
}

// Fresh reports whether s can still be handed to the player at now: it
// must not expire within the safety margin.
func (s Stream) Fresh(now time.Time) bool {
	return s.URL != "" && now.Before(s.Expires.Add(-ExpiryMargin))
}

// Wait blocks until s is fetchable (AvailableAt) or ctx ends. Callers run
// it in a tea.Cmd or goroutine, never in Update.
func (s Stream) Wait(ctx context.Context, clk clock.Clock) error {
	d := s.AvailableAt.Sub(clk.Now())
	if d <= 0 {
		return nil
	}
	t := clk.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ExpiryMargin is how long before Expires a cached URL counts as stale, so
// playback is never handed a URL that dies mid-track (ytm-player uses the
// same 300 s buffer).
const ExpiryMargin = 5 * time.Minute

// DefaultTTL is assumed when a URL carries no expire parameter
// (ytm-player's fixed _CACHE_TTL_SECONDS).
const DefaultTTL = 5 * time.Hour

// Resolver turns a video ID into a Stream.
type Resolver interface {
	Resolve(ctx context.Context, videoID string, q Quality) (Stream, error)
}

// validID matches ytm-player's VALID_VIDEO_ID. It also keeps IDs from
// being read as yt-dlp options or URL fragments.
var validID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidID reports whether id looks like a YouTube video ID.
func ValidID(id string) bool { return validID.MatchString(id) }

func checkID(id string) error {
	if !ValidID(id) {
		return fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	return nil
}

// retryable reports whether another attempt might succeed.
func retryable(err error) bool {
	for _, e := range []error{ErrUnavailable, ErrSignInRequired, ErrRateLimited, ErrYtDlpNotFound, ErrInvalidID, ErrClosed, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, e) {
			return false
		}
	}
	return true
}
