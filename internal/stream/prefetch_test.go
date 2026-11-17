package stream

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// gate lets a test release individual resolutions.
type gate struct {
	mu      sync.Mutex
	started []string
	chans   map[string]chan struct{}
	ended   map[string]error
}

func newGate() *gate { return &gate{chans: map[string]chan struct{}{}, ended: map[string]error{}} }

func (g *gate) ch(id string) chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	c, ok := g.chans[id]
	if !ok {
		c = make(chan struct{})
		g.chans[id] = c
	}
	return c
}

func (g *gate) resolve(r *fakeResolver) func(ctx context.Context, id string, q Quality, n int32) (Stream, error) {
	return func(ctx context.Context, id string, _ Quality, _ int32) (Stream, error) {
		g.mu.Lock()
		g.started = append(g.started, id)
		g.mu.Unlock()
		select {
		case <-g.ch(id):
			return r.stream(id), nil
		case <-ctx.Done():
			g.mu.Lock()
			g.ended[id] = ctx.Err()
			g.mu.Unlock()
			return Stream{}, ctx.Err()
		}
	}
}

func (g *gate) order() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.started...)
}

func (g *gate) cancelled(id string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return errors.Is(g.ended[id], context.Canceled)
}

func newPrefetch(t *testing.T) (*Prefetcher, *Cache, *gate, chan Result) {
	t.Helper()
	c, r, _ := newCache(t, nil)
	g := newGate()
	r.fn = g.resolve(r)
	results := make(chan Result, 16)
	p := NewPrefetcher(c, func(res Result) { results <- res })
	t.Cleanup(p.Close)
	return p, c, g, results
}

func TestPrefetchInPriorityOrder(t *testing.T) {
	p, c, g, results := newPrefetch(t)
	p.Want(High, "next", "after", "ignored")
	waitFor(t, func() bool { return len(g.order()) == 1 })
	if got := g.order(); got[0] != "next" {
		t.Fatalf("started %v", got)
	}
	close(g.ch("next"))
	if r := <-results; r.VideoID != "next" || r.Err != nil {
		t.Fatalf("result %+v", r)
	}
	waitFor(t, func() bool { return len(g.order()) == 2 })
	close(g.ch("after"))
	if r := <-results; r.VideoID != "after" {
		t.Fatalf("result %+v", r)
	}
	time.Sleep(20 * time.Millisecond)
	if got := g.order(); len(got) != 2 {
		t.Errorf("third slot used: %v", got)
	}
	for _, id := range []string{"next", "after"} {
		if _, ok := c.Cached(id, High); !ok {
			t.Errorf("%s not cached", id)
		}
	}
}

func TestPrefetchCancelsDroppedTrack(t *testing.T) {
	p, _, g, results := newPrefetch(t)
	p.Want(High, "old")
	waitFor(t, func() bool { return len(g.order()) == 1 })
	p.Want(High, "new") // queue changed: "old" is no longer next
	waitFor(t, func() bool { return g.cancelled("old") })
	waitFor(t, func() bool { return len(g.order()) == 2 })
	close(g.ch("new"))
	r := <-results
	if r.VideoID != "new" || r.Err != nil {
		t.Fatalf("got %+v (cancelled work must not be reported)", r)
	}
}

func TestPrefetchKeepsStillWantedTrack(t *testing.T) {
	p, _, g, results := newPrefetch(t)
	p.Want(High, "a", "b")
	waitFor(t, func() bool { return len(g.order()) == 1 })
	p.Want(High, "x", "a") // "a" is still wanted: keep going, then "x"
	time.Sleep(20 * time.Millisecond)
	if g.cancelled("a") {
		t.Fatal("still-wanted prefetch cancelled")
	}
	close(g.ch("a"))
	if r := <-results; r.VideoID != "a" {
		t.Fatalf("got %+v", r)
	}
	waitFor(t, func() bool { return len(g.order()) == 2 })
	if got := g.order()[1]; got != "x" {
		t.Errorf("then started %q, want x", got)
	}
	close(g.ch("x"))
	<-results
}

func TestPrefetchSkipsCached(t *testing.T) {
	p, c, g, results := newPrefetch(t)
	close(g.ch("a"))
	if _, err := c.Resolve(context.Background(), "a", High); err != nil {
		t.Fatal(err)
	}
	p.Want(High, "a", "b")
	waitFor(t, func() bool { return len(g.order()) == 2 })
	if got := g.order()[1]; got != "b" {
		t.Errorf("started %q, want b", got)
	}
	close(g.ch("b"))
	if r := <-results; r.VideoID != "b" {
		t.Fatalf("got %+v", r)
	}
}

func TestPrefetchReportsFailureOnce(t *testing.T) {
	c, r, _ := newCache(t, func(context.Context, string, Quality, int32) (Stream, error) {
		return Stream{}, ErrUnavailable
	})
	results := make(chan Result, 4)
	p := NewPrefetcher(c, func(res Result) { results <- res })
	defer p.Close()
	p.Want(High, "gone")
	if res := <-results; !errors.Is(res.Err, ErrUnavailable) {
		t.Fatalf("got %+v", res)
	}
	p.Want(High, "gone") // same list again: not retried
	time.Sleep(20 * time.Millisecond)
	if n := r.calls.Load(); n != 1 {
		t.Errorf("calls = %d, want 1", n)
	}
}

func TestPrefetchCloseCancels(t *testing.T) {
	c, r, _ := newCache(t, nil)
	g := newGate()
	r.fn = g.resolve(r)
	p := NewPrefetcher(c, nil)
	p.Want(High, "a")
	waitFor(t, func() bool { return len(g.order()) == 1 })
	p.Close()
	waitFor(t, func() bool { return g.cancelled("a") })
}
