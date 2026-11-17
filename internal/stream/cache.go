package stream

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/clock"
)

// MaxEntries caps the cache (ytm-player's _CACHE_MAX_SIZE).
const MaxEntries = 128

// MaxProcesses limits concurrent yt-dlp runs (spec 03).
const MaxProcesses = 2

// retryDelays are the waits before each attempt of a transient failure:
// one try plus two retries, as ytm-player's _resolve_sync.
var retryDelays = []time.Duration{0, time.Second, 2 * time.Second}

type key struct {
	id string
	q  Quality
}

// flight is one shared resolution. It runs on its own context, cancelled
// only when every caller waiting on it has gone (or on Close), so a
// cancelled prefetch never aborts a play request for the same track.
type flight struct {
	done   chan struct{}
	s      Stream
	err    error
	refs   int
	cancel context.CancelFunc
	gen    uint64
}

// Cache wraps a Resolver with an in-memory URL cache, request
// de-duplication, a limit of MaxProcesses concurrent resolutions and
// retries of transient failures. URLs are IP-bound, so nothing is
// persisted. All methods are safe for concurrent use and none blocks
// beyond the ctx it is given; Close stops in-flight work.
type Cache struct {
	res   Resolver
	clk   clock.Clock
	log   *slog.Logger
	sem   chan struct{}
	base  context.Context
	stop  context.CancelFunc
	wg    sync.WaitGroup
	mu    sync.Mutex
	items map[key]Stream
	fly   map[key]*flight
	gen   uint64 // bumped by Clear
	shut  bool
}

// NewCache returns a Cache over res. clk and log may be nil.
func NewCache(res Resolver, clk clock.Clock, log *slog.Logger) *Cache {
	if clk == nil {
		clk = clock.Real{}
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	base, stop := context.WithCancel(context.Background())
	return &Cache{
		res: res, clk: clk, log: log,
		sem:  make(chan struct{}, MaxProcesses),
		base: base, stop: stop,
		items: map[key]Stream{},
		fly:   map[key]*flight{},
	}
}

// Cached returns a fresh cached stream without resolving.
func (c *Cache) Cached(id string, q Quality) (Stream, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cachedLocked(key{id, q})
}

func (c *Cache) cachedLocked(k key) (Stream, bool) {
	s, ok := c.items[k]
	if !ok {
		return Stream{}, false
	}
	if !s.Fresh(c.clk.Now()) {
		delete(c.items, k)
		return Stream{}, false
	}
	return s, true
}

// Resolve implements Resolver: a fresh cache hit, else a shared
// resolution. Cancelling ctx returns at once; the shared work stops only
// when no other caller still waits for it.
func (c *Cache) Resolve(ctx context.Context, id string, q Quality) (Stream, error) {
	if err := checkID(id); err != nil {
		return Stream{}, err
	}
	k := key{id, q}
	c.mu.Lock()
	if c.shut {
		c.mu.Unlock()
		return Stream{}, ErrClosed
	}
	if s, ok := c.cachedLocked(k); ok {
		c.mu.Unlock()
		return s, nil
	}
	f, ok := c.fly[k]
	if !ok || f.gen != c.gen {
		f = c.startLocked(k) //nolint:contextcheck // shared work outlives any one caller's ctx; see flight
	}
	f.refs++
	c.mu.Unlock()

	select {
	case <-f.done:
		c.release(k, f)
		return f.s, f.err
	case <-ctx.Done():
		c.release(k, f)
		return Stream{}, ctx.Err()
	}
}

// Playable resolves id and then waits until googlevideo serves it
// (Stream.AvailableAt). mpv opens URLs at once and fails with HTTP 403
// inside that window (observed live: ~3 s after resolving), so every URL
// handed to the player goes through here. Run it in a tea.Cmd.
func (c *Cache) Playable(ctx context.Context, id string, q Quality) (Stream, error) {
	s, err := c.Resolve(ctx, id, q)
	if err != nil {
		return Stream{}, err
	}
	if err := s.Wait(ctx, c.clk); err != nil {
		return Stream{}, err
	}
	return s, nil
}

// Refresh drops any cached URL for id and resolves it again: the
// re-resolve-once step after mpv reports a load error (spec 02).
func (c *Cache) Refresh(ctx context.Context, id string, q Quality) (Stream, error) {
	c.Invalidate(id)
	return c.Resolve(ctx, id, q)
}

// release drops one waiter; the last one cancels unfinished work.
func (c *Cache) release(k key, f *flight) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f.refs--
	if f.refs > 0 {
		return
	}
	select {
	case <-f.done:
	default:
		f.cancel()
		if c.fly[k] == f {
			delete(c.fly, k) // a later request starts afresh
		}
	}
}

func (c *Cache) startLocked(k key) *flight {
	ctx, cancel := context.WithCancel(c.base)
	f := &flight{done: make(chan struct{}), cancel: cancel, gen: c.gen}
	c.fly[k] = f
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer cancel()
		s, err := c.resolveWithRetry(ctx, k)
		c.mu.Lock()
		f.s, f.err = s, err
		if c.fly[k] == f {
			delete(c.fly, k)
		}
		if err == nil && f.gen == c.gen && !c.shut {
			c.putLocked(k, s)
		}
		c.mu.Unlock()
		close(f.done)
	}()
	return f
}

func (c *Cache) resolveWithRetry(ctx context.Context, k key) (Stream, error) {
	var err error
	for attempt, d := range retryDelays {
		if d > 0 {
			if werr := c.sleep(ctx, d); werr != nil {
				return Stream{}, werr
			}
		}
		var s Stream
		s, err = c.resolveOnce(ctx, k)
		if err == nil {
			return s, nil
		}
		c.log.Warn("stream: resolve failed", "id", k.id, "attempt", attempt+1, "err", err)
		if !retryable(err) || ctx.Err() != nil {
			break
		}
	}
	if ctx.Err() != nil && !errors.Is(err, ctx.Err()) {
		return Stream{}, ctx.Err()
	}
	return Stream{}, err
}

func (c *Cache) resolveOnce(ctx context.Context, k key) (Stream, error) {
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return Stream{}, ctx.Err()
	}
	defer func() { <-c.sem }()
	return c.res.Resolve(ctx, k.id, k.q)
}

func (c *Cache) sleep(ctx context.Context, d time.Duration) error {
	t := c.clk.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// putLocked stores s, prunes stale entries and evicts the soonest-expiring
// ones beyond MaxEntries.
func (c *Cache) putLocked(k key, s Stream) {
	c.items[k] = s
	now := c.clk.Now()
	for kk, v := range c.items {
		if !v.Fresh(now) {
			delete(c.items, kk)
		}
	}
	if len(c.items) <= MaxEntries {
		return
	}
	keys := make([]key, 0, len(c.items))
	for kk := range c.items {
		keys = append(keys, kk)
	}
	sort.Slice(keys, func(i, j int) bool { return c.items[keys[i]].Expires.Before(c.items[keys[j]].Expires) })
	for _, kk := range keys[:len(keys)-MaxEntries] {
		delete(c.items, kk)
	}
}

// Invalidate removes id (all qualities) from the cache.
func (c *Cache) Invalidate(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.items {
		if k.id == id {
			delete(c.items, k)
		}
	}
}

// Clear empties the cache. Resolutions already running finish for their
// callers but are not stored, and new requests do not join them (their
// cookies or options may be stale, e.g. after `ytm setup`).
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.items)
	c.gen++
}

// Len returns the number of cached entries (stale ones included).
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// Close cancels in-flight resolutions (killing their yt-dlp processes)
// and waits for them, or for ctx.
func (c *Cache) Close(ctx context.Context) error {
	c.mu.Lock()
	c.shut = true
	c.mu.Unlock()
	c.stop()
	done := make(chan struct{})
	go func() { c.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
