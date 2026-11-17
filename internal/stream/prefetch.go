package stream

import (
	"context"
	"errors"
	"slices"
)

// PrefetchSlots is how many upcoming tracks are kept resolved (next and
// next+1, spec 03).
const PrefetchSlots = 2

// Result reports a finished prefetch.
type Result struct {
	VideoID string
	Quality Quality
	Stream  Stream
	Err     error
}

// Prefetcher resolves upcoming tracks in the background, one at a time in
// priority order, through a Cache (so a play request for the same track
// joins the prefetch instead of starting another yt-dlp). Want replaces
// the list; work for a track no longer wanted is cancelled.
//
// It owns one goroutine, started by NewPrefetcher and stopped by Close.
type Prefetcher struct {
	cache  *Cache
	onDone func(Result)
	want   chan wantMsg
	stop   context.CancelFunc
	done   chan struct{}
}

type wantMsg struct {
	q   Quality
	ids []string
}

// NewPrefetcher starts a prefetcher. onDone (may be nil) is called from
// the prefetch goroutine after each attempt; it must not block — the TUI
// forwards it with tea.Program.Send.
func NewPrefetcher(cache *Cache, onDone func(Result)) *Prefetcher {
	ctx, stop := context.WithCancel(context.Background())
	p := &Prefetcher{
		cache: cache, onDone: onDone,
		want: make(chan wantMsg, 1),
		stop: stop, done: make(chan struct{}),
	}
	go p.run(ctx)
	return p
}

// Want sets the upcoming tracks, highest priority first; only the first
// PrefetchSlots are used and an empty call cancels all prefetching. It
// never blocks: a newer call replaces one not yet picked up.
func (p *Prefetcher) Want(q Quality, ids ...string) {
	ids = slices.Clone(ids[:min(len(ids), PrefetchSlots)])
	m := wantMsg{q: q, ids: ids}
	for {
		select {
		case p.want <- m:
			return
		default:
		}
		select {
		case <-p.want: // drop the stale request
		default:
		}
	}
}

// Close stops the prefetcher, cancels its work and waits for it.
func (p *Prefetcher) Close() {
	p.stop()
	<-p.done
}

func (p *Prefetcher) run(ctx context.Context) {
	defer close(p.done)
	var (
		want     wantMsg
		cur      string // video ID being resolved, "" when idle
		curQ     Quality
		cancel   context.CancelFunc = func() {}
		finished                    = make(chan Result, 1)
		attempts                    = map[string]bool{} // tried for this want list
	)
	defer func() { cancel() }()

	startNext := func() {
		if cur != "" {
			return
		}
		for _, id := range want.ids {
			if attempts[id] {
				continue
			}
			attempts[id] = true
			if _, ok := p.cache.Cached(id, want.q); ok {
				continue
			}
			var jctx context.Context
			jctx, cancel = context.WithCancel(ctx)
			cur, curQ = id, want.q
			go func(id string, q Quality) {
				s, err := p.cache.Resolve(jctx, id, q)
				finished <- Result{VideoID: id, Quality: q, Stream: s, Err: err}
			}(id, want.q)
			return
		}
	}

	for {
		select {
		case <-ctx.Done():
			if cur != "" {
				cancel()
				<-finished
			}
			return
		case m := <-p.want:
			if cur != "" && (!slices.Contains(m.ids, cur) || m.q != curQ) {
				cancel()
			}
			if m.q != want.q {
				clear(attempts)
			}
			// Tracks dropped from the list may be tried again if re-added.
			for id := range attempts {
				if !slices.Contains(m.ids, id) {
					delete(attempts, id)
				}
			}
			if cur != "" && m.q == curQ && slices.Contains(m.ids, cur) {
				attempts[cur] = true
			}
			want = m
			startNext()
		case r := <-finished:
			cur = ""
			cancel()
			if r.Err == nil || ctx.Err() == nil && !isCancel(r.Err) {
				if p.onDone != nil {
					p.onDone(r)
				}
			}
			startNext()
		}
	}
}

func isCancel(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
