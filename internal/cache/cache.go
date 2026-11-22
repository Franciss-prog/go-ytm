// Package cache is the on-disk audio cache: tracks downloaded for offline
// play, indexed in the store and evicted least recently used first when
// the total passes [cache] max_size_mb (ytm-player services/cache.py and
// services/download.py). Spec 01, "Persistence".
package cache

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/store"
	"github.com/Franciss-Prog/go-ytm/internal/stream"
)

// Errors a download can return besides the downloader's own.
var (
	// ErrDisabled: [cache] max_size_mb = 0.
	ErrDisabled = errors.New("the audio cache is off ([cache] max_size_mb = 0)")
	// ErrBusy: the same track is already downloading.
	ErrBusy = errors.New("already downloading")
)

// Index is the cache index (*store.DB).
type Index interface {
	CacheGet(ctx context.Context, id string) (store.CacheEntry, bool, error)
	CachePut(ctx context.Context, e store.CacheEntry) error
	CacheDelete(ctx context.Context, ids ...string) error
	CacheEntries(ctx context.Context) ([]store.CacheEntry, error)
}

// Downloader saves a track's audio into dir and returns the file path
// (stream.YtDlp).
type Downloader interface {
	Download(ctx context.Context, id, dir string) (string, error)
}

// Cache is the audio cache. Methods block on disk and network: call them
// from a tea.Cmd or goroutine.
type Cache struct {
	Dir   string
	Max   int64 // bytes; 0 disables downloads
	Index Index
	DL    Downloader
	Log   *slog.Logger

	mu     sync.Mutex
	active map[string]bool
}

// Lookup returns the cached file for id, dropping an index entry whose
// file was deleted behind our back.
func (c *Cache) Lookup(ctx context.Context, id string) (string, bool) {
	e, ok, err := c.Index.CacheGet(ctx, id)
	if err != nil {
		c.log().Warn("cache: lookup failed", "id", id, "err", err)
		return "", false
	}
	if !ok {
		return "", false
	}
	if _, err := os.Stat(e.Path); err != nil {
		_ = c.Index.CacheDelete(ctx, id)
		return "", false
	}
	return e.Path, true
}

// Result is a finished download.
type Result struct {
	Path string
	// Already: the track was cached before this call.
	Already bool
	// Evicted: the file alone is bigger than the cache, so eviction
	// removed it again.
	Evicted bool
}

// Download caches id's audio. The file is written to a private staging
// dir inside Dir and renamed into place, so Lookup never sees a partial
// file.
func (c *Cache) Download(ctx context.Context, id string) (Result, error) {
	if c.Max <= 0 {
		return Result{}, ErrDisabled
	}
	if !stream.ValidID(id) {
		return Result{}, fmt.Errorf("download: %w: %q", stream.ErrInvalidID, id)
	}
	if p, ok := c.Lookup(ctx, id); ok {
		return Result{Path: p, Already: true}, nil
	}
	c.mu.Lock()
	if c.active[id] {
		c.mu.Unlock()
		return Result{}, ErrBusy
	}
	if c.active == nil {
		c.active = map[string]bool{}
	}
	c.active[id] = true
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.active, id); c.mu.Unlock() }()

	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return Result{}, fmt.Errorf("download: %w", err)
	}
	stage, err := os.MkdirTemp(c.Dir, "."+id+"-")
	if err != nil {
		return Result{}, fmt.Errorf("download: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	got, err := c.DL.Download(ctx, id, stage)
	if err != nil {
		return Result{}, err
	}
	// yt-dlp prints the path it wrote; only accept one inside the stage.
	if filepath.Dir(filepath.Clean(got)) != filepath.Clean(stage) {
		return Result{}, fmt.Errorf("download %s: unexpected output path %q", id, got)
	}
	fi, err := os.Stat(got)
	if err != nil {
		return Result{}, fmt.Errorf("download %s: %w", id, err)
	}
	dest := filepath.Join(c.Dir, filepath.Base(got))
	if err := os.Rename(got, dest); err != nil {
		return Result{}, fmt.Errorf("download %s: %w", id, err)
	}
	if err := c.Index.CachePut(ctx, store.CacheEntry{VideoID: id, Path: dest, Size: fi.Size()}); err != nil {
		_ = os.Remove(dest)
		return Result{}, err
	}
	if err := c.evict(ctx); err != nil {
		return Result{}, err
	}
	if _, ok, _ := c.Index.CacheGet(ctx, id); !ok {
		return Result{Path: dest, Evicted: true}, nil
	}
	return Result{Path: dest}, nil
}

// evict removes least recently used files until the total fits Max.
func (c *Cache) evict(ctx context.Context) error {
	es, err := c.Index.CacheEntries(ctx)
	if err != nil {
		return err
	}
	var total int64
	for _, e := range es {
		total += e.Size
	}
	var drop []string
	for _, e := range es {
		if total <= c.Max {
			break
		}
		if err := os.Remove(e.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			c.log().Warn("cache: evict failed", "path", e.Path, "err", err)
			continue
		}
		drop = append(drop, e.VideoID)
		total -= e.Size
	}
	return c.Index.CacheDelete(ctx, drop...)
}

func (c *Cache) log() *slog.Logger {
	if c.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return c.Log
}

// localTTL is how long a cached file counts as fresh in the stream cache.
const localTTL = 24 * time.Hour

// local returns id's cached file as a Stream.
func (c *Cache) local(ctx context.Context, id string, now func() time.Time) (stream.Stream, bool) {
	p, ok := c.Lookup(ctx, id)
	if !ok {
		return stream.Stream{}, false
	}
	if now == nil {
		now = time.Now
	}
	return stream.Stream{
		VideoID: id, URL: p, Expires: now().Add(localTTL),
		Codec: strings.TrimPrefix(filepath.Ext(p), "."),
	}, true
}

// Resolver plays cached files instead of resolving them (spec 03: a local
// audio cache hit short-circuits resolution), and falls through to Next.
// It sits under stream.Cache, so prefetching a cached track runs no yt-dlp.
type Resolver struct {
	Cache *Cache
	Next  stream.Resolver
	Now   func() time.Time
}

// Resolve implements stream.Resolver.
func (r Resolver) Resolve(ctx context.Context, id string, q stream.Quality) (stream.Stream, error) {
	if s, ok := r.Cache.local(ctx, id, r.Now); ok {
		return s, nil
	}
	return r.Next.Resolve(ctx, id, q)
}

// Playable is what the player asks for a track (stream.Cache).
type Playable interface {
	Playable(ctx context.Context, id string, q stream.Quality) (stream.Stream, error)
	Refresh(ctx context.Context, id string, q stream.Quality) (stream.Stream, error)
}

// Streams checks the cache in front of Next: a URL resolved (or
// prefetched) before the track was downloaded stays memoized in Next, so
// the Resolver under it alone would keep playing the network copy.
type Streams struct {
	Cache *Cache
	Next  Playable
	Now   func() time.Time
}

// Playable implements ui.Streams.
func (s Streams) Playable(ctx context.Context, id string, q stream.Quality) (stream.Stream, error) {
	if st, ok := s.Cache.local(ctx, id, s.Now); ok {
		return st, nil
	}
	return s.Next.Playable(ctx, id, q)
}

// Refresh implements ui.Streams.
func (s Streams) Refresh(ctx context.Context, id string, q stream.Quality) (stream.Stream, error) {
	if st, ok := s.Cache.local(ctx, id, s.Now); ok {
		return st, nil
	}
	return s.Next.Refresh(ctx, id, q)
}
