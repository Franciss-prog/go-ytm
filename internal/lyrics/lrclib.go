package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/clock"
)

// LRCLIB is a client for https://lrclib.net (API docs: lrclib.net/docs.txt,
// read 2026-11-22). It is safe for concurrent use.
type LRCLIB struct {
	Client    *http.Client // nil: http.DefaultClient
	BaseURL   string       // "" = https://lrclib.net
	UserAgent string       // required by LRCLIB: "name/version (homepage)"
	Clock     clock.Clock  // nil: clock.Real

	mu    sync.Mutex
	until time.Time // no requests before this (429/503 Retry-After)
}

// Record is the part of an LRCLIB lyrics record ytm-go uses.
type Record struct {
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  *string `json:"plainLyrics"`
	SyncedLyrics *string `json:"syncedLyrics"`
}

// ErrRateLimited is returned while LRCLIB's Retry-After is pending.
var ErrRateLimited = errors.New("lrclib: rate limited")

// Get looks a track up with GET /api/get. LRCLIB only matches when the
// duration is within ±2 s of its record. It returns nil, nil when LRCLIB
// has no record (404).
func (c *LRCLIB) Get(ctx context.Context, title, artist, album string, dur time.Duration) (*Record, error) {
	now := c.now()
	c.mu.Lock()
	wait := c.until.After(now)
	c.mu.Unlock()
	if wait {
		return nil, ErrRateLimited
	}
	q := url.Values{"track_name": {title}, "artist_name": {artist}}
	if album != "" {
		q.Set("album_name", album)
	}
	if s := int(dur / time.Second); s >= 1 && s <= 3600 {
		q.Set("duration", strconv.Itoa(s))
	}
	base := c.BaseURL
	if base == "" {
		base = "https://lrclib.net"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/get?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("lrclib: %w", err)
	}
	req.Header.Set("User-Agent", c.UserAgent)
	hc := c.Client
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lrclib: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("lrclib: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, nil
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		// The docs say clients must honor Retry-After (seconds).
		secs, err := strconv.Atoi(resp.Header.Get("Retry-After"))
		if err != nil || secs <= 0 {
			secs = 60
		}
		c.mu.Lock()
		c.until = now.Add(time.Duration(secs) * time.Second)
		c.mu.Unlock()
		return nil, fmt.Errorf("%w: HTTP %d", ErrRateLimited, resp.StatusCode)
	default:
		return nil, fmt.Errorf("lrclib: HTTP %d", resp.StatusCode)
	}
	// Bad query strings get a plain-text reply, not JSON (docs).
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt != "application/json" {
		return nil, fmt.Errorf("lrclib: unexpected %q reply", mt)
	}
	var r Record
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("lrclib: %w", err)
	}
	return &r, nil
}

func (c *LRCLIB) now() time.Time {
	if c.Clock == nil {
		return time.Now()
	}
	return c.Clock.Now()
}
