package stream

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/clock"
)

// fakeResolver records calls; behaviour comes from fn, defaulting to an
// immediately fresh stream.
type fakeResolver struct {
	calls atomic.Int32
	cur   atomic.Int32
	peak  atomic.Int32
	fn    func(ctx context.Context, id string, q Quality, n int32) (Stream, error)
	clk   clock.Clock
}

func (f *fakeResolver) Resolve(ctx context.Context, id string, q Quality) (Stream, error) {
	n := f.calls.Add(1)
	c := f.cur.Add(1)
	defer f.cur.Add(-1)
	for {
		p := f.peak.Load()
		if c <= p || f.peak.CompareAndSwap(p, c) {
			break
		}
	}
	if f.fn != nil {
		return f.fn(ctx, id, q, n)
	}
	return f.stream(id), nil
}

func (f *fakeResolver) stream(id string) Stream {
	return Stream{VideoID: id, URL: "https://x/" + id, Expires: f.clk.Now().Add(time.Hour)}
}

func newCache(t testing.TB, fn func(ctx context.Context, id string, q Quality, n int32) (Stream, error)) (*Cache, *fakeResolver, *clock.Fake) {
	t.Helper()
	clk := clock.NewFake(fixed)
	r := &fakeResolver{fn: fn, clk: clk}
	c := NewCache(r, clk, nil)
	t.Cleanup(func() {
		if err := c.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return c, r, clk
}

func TestCacheHitAndExpiry(t *testing.T) {
	c, r, clk := newCache(t, nil)
	ctx := context.Background()
	for range 3 {
		if _, err := c.Resolve(ctx, "aaa", High); err != nil {
			t.Fatal(err)
		}
	}
	if n := r.calls.Load(); n != 1 {
		t.Fatalf("calls = %d, want 1", n)
	}
	if _, err := c.Resolve(ctx, "aaa", Low); err != nil {
		t.Fatal(err)
	}
	if n := r.calls.Load(); n != 2 {
		t.Fatalf("quality is part of the key: calls = %d", n)
	}
	// 1 h expiry minus the 5 min margin: still fresh at 54 min, stale at 55.
	clk.Advance(54 * time.Minute)
	if _, ok := c.Cached("aaa", High); !ok {
		t.Error("stale too early")
	}
	clk.Advance(time.Minute)
	if _, ok := c.Cached("aaa", High); ok {
		t.Error("served inside the expiry margin")
	}
	if _, err := c.Resolve(ctx, "aaa", High); err != nil {
		t.Fatal(err)
	}
	if n := r.calls.Load(); n != 3 {
		t.Errorf("calls = %d, want 3", n)
	}
}

func TestCacheDedupes(t *testing.T) {
	release := make(chan struct{})
	c, r, _ := newCache(t, nil)
	r.fn = func(ctx context.Context, id string, _ Quality, _ int32) (Stream, error) {
		<-release
		return r.stream(id), nil
	}
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if _, err := c.Resolve(context.Background(), "aaa", High); err != nil {
				t.Error(err)
			}
		})
	}
	waitFor(t, func() bool { return r.calls.Load() == 1 })
	close(release)
	wg.Wait()
	if n := r.calls.Load(); n != 1 {
		t.Errorf("calls = %d, want 1", n)
	}
}

func TestCacheLimitsConcurrency(t *testing.T) {
	release := make(chan struct{})
	c, r, _ := newCache(t, nil)
	r.fn = func(ctx context.Context, id string, _ Quality, _ int32) (Stream, error) {
		<-release
		return r.stream(id), nil
	}
	var wg sync.WaitGroup
	for i := range 5 {
		wg.Go(func() {
			if _, err := c.Resolve(context.Background(), fmt.Sprintf("id%d", i), High); err != nil {
				t.Error(err)
			}
		})
	}
	waitFor(t, func() bool { return r.cur.Load() == MaxProcesses })
	time.Sleep(20 * time.Millisecond) // give a third run the chance to (wrongly) start
	close(release)
	wg.Wait()
	if p := r.peak.Load(); p != MaxProcesses {
		t.Errorf("peak concurrency = %d, want %d", p, MaxProcesses)
	}
	if c.Len() != 5 {
		t.Errorf("len = %d", c.Len())
	}
}

func TestCacheRetriesTransient(t *testing.T) {
	c, r, clk := newCache(t, nil)
	r.fn = func(_ context.Context, id string, _ Quality, n int32) (Stream, error) {
		if n < 3 {
			return Stream{}, errors.New("yt-dlp: HTTP Error 503")
		}
		return r.stream(id), nil
	}
	done := make(chan error, 1)
	go func() { _, err := c.Resolve(context.Background(), "aaa", High); done <- err }()
	// Attempt 1 fails at once, then a 1 s and a 2 s back-off.
	for _, d := range []time.Duration{time.Second, 2 * time.Second} {
		clk.BlockUntil(1, nil)
		clk.Advance(d)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n := r.calls.Load(); n != 3 {
		t.Errorf("calls = %d, want 3", n)
	}
}

func TestCacheGivesUpAfterThreeAttempts(t *testing.T) {
	c, r, clk := newCache(t, func(context.Context, string, Quality, int32) (Stream, error) {
		return Stream{}, errors.New("yt-dlp: connection reset")
	})
	done := make(chan error, 1)
	go func() { _, err := c.Resolve(context.Background(), "aaa", High); done <- err }()
	for _, d := range []time.Duration{time.Second, 2 * time.Second} {
		clk.BlockUntil(1, nil)
		clk.Advance(d)
	}
	if err := <-done; err == nil {
		t.Fatal("want error")
	}
	if n := r.calls.Load(); n != 3 {
		t.Errorf("calls = %d, want 3", n)
	}
}

func TestCacheNoRetryForPermanent(t *testing.T) {
	for _, perm := range []error{ErrUnavailable, ErrSignInRequired, ErrRateLimited, ErrYtDlpNotFound} {
		c, r, _ := newCache(t, func(context.Context, string, Quality, int32) (Stream, error) {
			return Stream{}, fmt.Errorf("%w: detail", perm)
		})
		_, err := c.Resolve(context.Background(), "aaa", High)
		if !errors.Is(err, perm) {
			t.Errorf("got %v, want %v", err, perm)
		}
		if n := r.calls.Load(); n != 1 {
			t.Errorf("%v: calls = %d, want 1", perm, n)
		}
		if c.Len() != 0 {
			t.Errorf("%v: error cached", perm)
		}
	}
}

// TestCacheWaiterCancel: one caller giving up must not abort the shared
// run another caller still waits for; the last one leaving cancels it.
func TestCacheWaiterCancel(t *testing.T) {
	release := make(chan struct{})
	cancelled := make(chan struct{})
	c, r, _ := newCache(t, nil)
	r.fn = func(ctx context.Context, id string, _ Quality, _ int32) (Stream, error) {
		select {
		case <-release:
			return r.stream(id), nil
		case <-ctx.Done():
			close(cancelled)
			return Stream{}, ctx.Err()
		}
	}
	prefetch, stopPrefetch := context.WithCancel(context.Background())
	pErr := make(chan error, 1)
	go func() { _, err := c.Resolve(prefetch, "aaa", High); pErr <- err }()
	waitFor(t, func() bool { return r.calls.Load() == 1 })
	play := make(chan error, 1)
	go func() { _, err := c.Resolve(context.Background(), "aaa", High); play <- err }()
	waitFor(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.fly[key{"aaa", High}].refs == 2 })

	stopPrefetch()
	if err := <-pErr; !errors.Is(err, context.Canceled) {
		t.Errorf("prefetch err = %v", err)
	}
	select {
	case <-cancelled:
		t.Fatal("shared run cancelled while play still waits")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-play; err != nil {
		t.Fatal(err)
	}

	// Sole waiter leaves: the run is cancelled.
	r.fn = func(ctx context.Context, _ string, _ Quality, _ int32) (Stream, error) {
		<-ctx.Done()
		return Stream{}, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := c.Resolve(ctx, "bbb", High); errc <- err }()
	waitFor(t, func() bool { return r.calls.Load() == 2 })
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Errorf("sole waiter err = %v", err)
	}
	waitFor(t, func() bool { return r.cur.Load() == 0 })
}

func TestCacheClearDropsInFlightResult(t *testing.T) {
	release := make(chan struct{})
	c, r, _ := newCache(t, nil)
	r.fn = func(_ context.Context, id string, _ Quality, _ int32) (Stream, error) {
		<-release
		return r.stream(id), nil
	}
	errc := make(chan error, 1)
	go func() { _, err := c.Resolve(context.Background(), "aaa", High); errc <- err }()
	waitFor(t, func() bool { return r.calls.Load() == 1 })
	c.Clear()
	// A request after Clear does not join the old run.
	go func() { _, err := c.Resolve(context.Background(), "aaa", High); errc <- err }()
	waitFor(t, func() bool { return r.calls.Load() == 2 })
	close(release)
	for range 2 {
		if err := <-errc; err != nil {
			t.Fatal(err)
		}
	}
	if c.Len() != 1 {
		t.Errorf("len = %d, want 1 (only the post-Clear result)", c.Len())
	}
}

func TestCacheRefreshAndInvalidate(t *testing.T) {
	c, r, _ := newCache(t, nil)
	ctx := context.Background()
	if _, err := c.Resolve(ctx, "aaa", High); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Refresh(ctx, "aaa", High); err != nil {
		t.Fatal(err)
	}
	if n := r.calls.Load(); n != 2 {
		t.Errorf("calls = %d, want 2", n)
	}
	c.Invalidate("aaa")
	if _, ok := c.Cached("aaa", High); ok {
		t.Error("still cached")
	}
}

func TestCacheEvictsSoonestExpiring(t *testing.T) {
	c, r, clk := newCache(t, nil)
	r.fn = func(_ context.Context, id string, _ Quality, n int32) (Stream, error) {
		return Stream{VideoID: id, URL: "u", Expires: clk.Now().Add(time.Hour + time.Duration(n)*time.Second)}, nil
	}
	for i := range MaxEntries + 3 {
		if _, err := c.Resolve(context.Background(), fmt.Sprintf("id%d", i), High); err != nil {
			t.Fatal(err)
		}
	}
	if c.Len() != MaxEntries {
		t.Fatalf("len = %d", c.Len())
	}
	for i := range 3 {
		if _, ok := c.Cached(fmt.Sprintf("id%d", i), High); ok {
			t.Errorf("id%d kept; soonest-expiring should go", i)
		}
	}
}

func TestCacheCloseCancelsWork(t *testing.T) {
	clk := clock.NewFake(fixed)
	r := &fakeResolver{clk: clk, fn: func(ctx context.Context, _ string, _ Quality, _ int32) (Stream, error) {
		<-ctx.Done()
		return Stream{}, ctx.Err()
	}}
	c := NewCache(r, clk, nil)
	errc := make(chan error, 1)
	go func() { _, err := c.Resolve(context.Background(), "aaa", High); errc <- err }()
	waitFor(t, func() bool { return r.calls.Load() == 1 })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-errc; err == nil {
		t.Error("want error after Close")
	}
	if _, err := c.Resolve(context.Background(), "aaa", High); !errors.Is(err, ErrClosed) {
		t.Errorf("after Close: %v", err)
	}
}

func TestStreamWait(t *testing.T) {
	clk := clock.NewFake(fixed)
	if err := (Stream{}).Wait(context.Background(), clk); err != nil {
		t.Fatal(err)
	}
	s := Stream{AvailableAt: fixed.Add(3 * time.Second)}
	done := make(chan error, 1)
	go func() { done <- s.Wait(context.Background(), clk) }()
	clk.BlockUntil(1, nil)
	clk.Advance(3 * time.Second)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.AvailableAt = clk.Now().Add(time.Second)
	if err := s.Wait(ctx, clk); !errors.Is(err, context.Canceled) {
		t.Errorf("got %v", err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(time.Millisecond)
	}
}
