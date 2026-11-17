package ui

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/player"
	"github.com/Franciss-Prog/go-ytm/internal/queue"
	"github.com/Franciss-Prog/go-ytm/internal/stream"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// fakeStreams resolves every id to "url:<id>" unless it is in fail.
type fakeStreams struct {
	mu    sync.Mutex
	fail  map[string]error
	calls []string // "playable:<id>" / "refresh:<id>"
}

func (f *fakeStreams) Playable(_ context.Context, id string, _ stream.Quality) (stream.Stream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "playable:"+id)
	if err := f.fail[id]; err != nil {
		return stream.Stream{}, err
	}
	return stream.Stream{VideoID: id, URL: "url:" + id}, nil
}

func (f *fakeStreams) Refresh(_ context.Context, id string, q stream.Quality) (stream.Stream, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "refresh:"+id)
	f.mu.Unlock()
	return stream.Stream{VideoID: id, URL: "url2:" + id}, nil
}

type fakePrefetch struct{ wants [][]string }

func (f *fakePrefetch) Want(_ stream.Quality, ids ...string) {
	f.wants = append(f.wants, slices.Clone(ids))
}

func (f *fakePrefetch) last() string {
	if len(f.wants) == 0 {
		return "<none>"
	}
	return strings.Join(f.wants[len(f.wants)-1], ",")
}

// drain runs cmd and feeds every resulting message back into m, like the
// program loop, until nothing is left. The model's context is cancelled,
// so timer commands (toast expiry) return at once.
func drain(m *Model, cmd tea.Cmd) {
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		default:
			_, next := m.Update(msg)
			queue = append(queue, next)
		}
	}
}

func tracks(ids ...string) []ytmusic.Track {
	out := make([]ytmusic.Track, len(ids))
	for i, id := range ids {
		out[i] = ytmusic.Track{VideoID: id, Title: "Song " + id, Artists: []ytmusic.Artist{{Name: "Artist"}}, Available: true}
	}
	return out
}

type pbHarness struct {
	m  *Model
	pl *fakePlayer
	s  *fakeStreams
	pf *fakePrefetch
}

func newPB(t *testing.T, mut ...func(*Options)) pbHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := pbHarness{s: &fakeStreams{fail: map[string]error{}}, pf: &fakePrefetch{}}
	m, pl := newTestModel(t, 100, 30, append([]func(*Options){withSidebar, func(o *Options) {
		o.Streams, o.Prefetch, o.Context = h.s, h.pf, ctx
		o.Queue = queue.New(rand.New(rand.NewPCG(1, 2)))
	}}, mut...)...)
	h.m, h.pl = m, pl
	return h
}

func (h pbHarness) send(msg tea.Msg) {
	_, cmd := h.m.Update(msg)
	drain(h.m, cmd)
}

func (h pbHarness) act(name string) { drain(h.m, h.m.action(keys.Action{Name: name})) }

// started simulates mpv reporting it started it (and the state change).
func (h pbHarness) started(id string) {
	it := player.Item{ID: id, URL: "url:" + id}
	h.pl.st = player.State{Status: player.Playing, Item: it, Volume: 80}
	h.send(PlayerEventMsg{playerState(h.pl.st)})
	h.send(PlayerEventMsg{player.TrackStarted{Item: it}})
}

func (h pbHarness) lastItem() string {
	if len(h.pl.items) == 0 {
		return "<none>"
	}
	return h.pl.items[len(h.pl.items)-1].URL
}

func (h pbHarness) current() string {
	t, _ := h.m.Queue().Current()
	return t.VideoID
}

func TestPlayLoadsAndPrefetches(t *testing.T) {
	h := newPB(t)
	h.send(PlayMsg{Tracks: tracks("a", "b", "c"), Start: 1, Context: "PL1"})
	if h.lastItem() != "url:b" || h.current() != "b" || h.m.Queue().Context() != "PL1" {
		t.Fatalf("load %s current %s", h.lastItem(), h.current())
	}
	if h.m.bar.now.Title != "Song b" {
		t.Fatalf("bar now %+v", h.m.bar.now)
	}
	if h.pf.last() != "c" {
		t.Fatalf("prefetch %s", h.pf.last())
	}
}

func TestGaplessAppendAndAdvance(t *testing.T) {
	h := newPB(t)
	h.send(PlayMsg{Tracks: tracks("a", "b", "c"), Start: 0})
	h.started("a")
	h.send(PrefetchedMsg{stream.Result{VideoID: "b", Stream: stream.Stream{URL: "url:b"}}})
	if got := h.pl.Calls(); got[len(got)-1] != "append" || h.lastItem() != "url:b" {
		t.Fatalf("calls %v last %s", got, h.lastItem())
	}
	// A prefetch for a track that is not next is ignored.
	h.send(PrefetchedMsg{stream.Result{VideoID: "c"}})
	loads := len(h.pl.items)
	// mpv ends a naturally and moves into b by itself.
	h.send(PlayerEventMsg{player.TrackEnded{Item: player.Item{ID: "a"}, Reason: player.EndNatural}})
	h.started("b")
	if h.current() != "b" || len(h.pl.items) != loads || h.m.bar.now.VideoID != "b" {
		t.Fatalf("current %s items %d→%d", h.current(), loads, len(h.pl.items))
	}
	if h.pf.last() != "c" {
		t.Fatalf("prefetch after advance %s", h.pf.last())
	}
}

func TestShuffleDropsStalePreload(t *testing.T) {
	h := newPB(t)
	h.send(PlayMsg{Tracks: tracks("a", "b", "c", "d", "e", "f"), Start: 0})
	h.started("a")
	h.send(PrefetchedMsg{stream.Result{VideoID: "b"}})
	h.act("toggle_shuffle")
	next, _ := h.m.Queue().PeekNext()
	calls := h.pl.Calls()
	if next.VideoID != "b" && !slices.Contains(calls, "clear_next") {
		t.Fatalf("next %s changed but preload kept: %v", next.VideoID, calls)
	}
	if !h.m.bar.shuffle {
		t.Fatal("bar shuffle flag not set")
	}
}

func TestNaturalEndWithoutPreloadLoadsNext(t *testing.T) {
	h := newPB(t, func(o *Options) { o.Config.Playback.Gapless = false })
	h.send(PlayMsg{Tracks: tracks("a", "b"), Start: 0})
	h.started("a")
	h.send(PrefetchedMsg{stream.Result{VideoID: "b"}}) // gapless off: no append
	h.send(PlayerEventMsg{player.TrackEnded{Item: player.Item{ID: "a"}, Reason: player.EndNatural}})
	if h.lastItem() != "url:b" || slices.Contains(h.pl.Calls(), "append") {
		t.Fatalf("calls %v last %s", h.pl.Calls(), h.lastItem())
	}
	// End of queue: nothing more is loaded.
	h.started("b")
	n := len(h.pl.items)
	h.send(PlayerEventMsg{player.TrackEnded{Item: player.Item{ID: "b"}, Reason: player.EndNatural}})
	if len(h.pl.items) != n {
		t.Fatal("loaded past the end")
	}
}

func TestStreamErrorRetriesOnceThenSkips(t *testing.T) {
	h := newPB(t)
	h.send(PlayMsg{Tracks: tracks("a", "b"), Start: 0})
	h.started("a")
	h.pl.st.Position = 42 * time.Second
	h.send(PlayerEventMsg{playerState(h.pl.st)})
	h.send(PlayerEventMsg{player.TrackEnded{Item: player.Item{ID: "a"}, Reason: player.EndError, Err: "403"}})
	if h.lastItem() != "url:a" || h.pl.at[len(h.pl.at)-1] != 42*time.Second || !slices.Contains(h.s.calls, "refresh:a") {
		t.Fatalf("retry: last %s at %v calls %v", h.lastItem(), h.pl.at, h.s.calls)
	}
	h.send(PlayerEventMsg{player.TrackEnded{Item: player.Item{ID: "a"}, Reason: player.EndError, Err: "403"}})
	if h.current() != "b" || h.lastItem() != "url:b" || len(h.m.toasts.items) == 0 {
		t.Fatalf("skip: current %s last %s toasts %d", h.current(), h.lastItem(), len(h.m.toasts.items))
	}
}

func TestResolveErrorSkips(t *testing.T) {
	h := newPB(t)
	h.s.fail["a"] = fmt.Errorf("resolve: %w", stream.ErrSignInRequired)
	h.send(PlayMsg{Tracks: tracks("a", "b"), Start: 0})
	if h.current() != "b" || h.lastItem() != "url:b" {
		t.Fatalf("current %s last %s", h.current(), h.lastItem())
	}
	if !strings.Contains(plain(h.m), "needs sign-in") {
		t.Fatalf("no sign-in toast:\n%s", plain(h.m))
	}
	// Every track failing stops at the end instead of looping.
	h.s.fail["b"] = stream.ErrUnavailable
	h.send(PlayMsg{Tracks: tracks("a", "b"), Start: 0})
	if n := len(h.s.calls); n > 6 {
		t.Fatalf("resolve loop: %v", h.s.calls)
	}
}

func TestStaleResolveDropped(t *testing.T) {
	h := newPB(t)
	h.send(PlayMsg{Tracks: tracks("a", "b"), Start: 0})
	n := len(h.pl.items)
	h.send(resolvedMsg{seq: h.m.pb.seq - 1, track: tracks("z")[0], s: stream.Stream{URL: "url:z"}})
	if len(h.pl.items) != n {
		t.Fatal("stale resolve loaded")
	}
	h.send(resolvedMsg{seq: h.m.pb.seq, err: context.Canceled})
	if len(h.m.toasts.items) != 0 {
		t.Fatal("cancelled resolve toasted")
	}
}

func TestQueueActions(t *testing.T) {
	h := newPB(t)
	h.act("next_track")
	if !strings.Contains(plain(h.m), "End of queue") {
		t.Fatal("next on empty queue: no toast")
	}
	h.send(PlayMsg{Tracks: tracks("a", "b", "c"), Start: 1})
	h.started("b")
	h.act("next_track")
	if h.current() != "c" || h.lastItem() != "url:c" {
		t.Fatalf("next: %s", h.current())
	}
	h.started("c")
	h.pl.st.Position = 10 * time.Second
	h.send(PlayerEventMsg{playerState(h.pl.st)})
	h.act("previous_track")
	if h.current() != "c" || h.pl.seeks[len(h.pl.seeks)-1] != 0 {
		t.Fatalf("previous after 3 s should restart: current %s", h.current())
	}
	h.pl.st.Position = time.Second
	h.send(PlayerEventMsg{playerState(h.pl.st)})
	h.act("previous_track")
	if h.current() != "b" {
		t.Fatalf("previous: %s", h.current())
	}
	for _, want := range []string{"all", "one", "off"} {
		h.act("cycle_repeat")
		if h.m.bar.repeat != want {
			t.Fatalf("repeat %s want %s", h.m.bar.repeat, want)
		}
	}
	h.act("play_random")
	if h.current() == "" {
		t.Fatal("play_random selected nothing")
	}
}

func TestPlayShuffleButton(t *testing.T) {
	h := newPB(t)
	h.send(PlayMsg{Tracks: tracks("a", "b", "c", "d"), Start: -1, Shuffle: true})
	if !h.m.Queue().Shuffle() || h.current() == "" || h.m.Queue().CurrentIndex() != 0 {
		t.Fatalf("shuffle %v current %q index %d", h.m.Queue().Shuffle(), h.current(), h.m.Queue().CurrentIndex())
	}
	// Shuffle stays on for the next PlayMsg; the chosen track comes first.
	h.send(PlayMsg{Tracks: tracks("a", "b", "c", "d"), Start: 2})
	if !h.m.Queue().Shuffle() || h.current() != "c" || h.m.Queue().CurrentIndex() != 0 {
		t.Fatalf("kept shuffle: current %s index %d", h.current(), h.m.Queue().CurrentIndex())
	}
}

func TestEnqueue(t *testing.T) {
	h := newPB(t)
	h.send(EnqueueMsg{Tracks: tracks("a", "b")})
	if h.current() != "a" || h.lastItem() != "url:a" {
		t.Fatalf("enqueue on idle should start: %s", h.current())
	}
	h.started("a")
	h.send(EnqueueMsg{Tracks: tracks("x"), Next: true})
	if next, _ := h.m.Queue().PeekNext(); next.VideoID != "x" || h.pf.last() != "x" {
		t.Fatalf("play next: next %s prefetch %s", next.VideoID, h.pf.last())
	}
	if !strings.Contains(plain(h.m), "Playing 1 track next") {
		t.Fatal("no play-next toast")
	}
}

func TestPlaybackWithoutStreams(t *testing.T) {
	h := newPB(t, func(o *Options) { o.Streams = nil })
	h.send(PlayMsg{Tracks: tracks("a"), Start: 0})
	if !strings.Contains(plain(h.m), "no stream resolver") || len(h.pl.items) != 0 {
		t.Fatal("missing resolver not reported")
	}
}

// keys presses keys through the full Update path, draining commands.
func (h pbHarness) keys(ks ...tea.Key) {
	for _, k := range ks {
		h.send(tea.KeyPressMsg(k))
	}
}

func queueIDs(q *queue.Queue) string {
	var ids []string
	for _, t := range q.Tracks() {
		ids = append(ids, t.VideoID)
	}
	return strings.Join(ids, ",")
}

func TestQueuePage(t *testing.T) {
	h := newPB(t)
	h.send(EnqueueMsg{Tracks: tracks("a", "b", "c", "d", "e")})
	h.started("a")
	h.m.navigate(PageQueue)
	q := h.m.Queue()

	// 2J on row b moves it two down; the cursor follows.
	h.keys(ch('j'), ch('2'), ch('J'))
	if got := queueIDs(q); got != "a,c,d,b,e" {
		t.Fatalf("2J: %s", got)
	}
	if !strings.Contains(h.m.pages[PageQueue].Info(), "Song b") {
		t.Fatalf("cursor did not follow: %q", h.m.pages[PageQueue].Info())
	}
	h.keys(ch('K'))
	if got := queueIDs(q); got != "a,c,b,d,e" {
		t.Fatalf("K: %s", got)
	}

	// Mark c and d, delete both.
	h.keys(ch('k'), ch('v'), ch('j'), ch('v'))
	h.keys(ch('d'), ch('d'))
	if got := queueIDs(q); got != "a,b,e" {
		t.Fatalf("delete marked: %s", got)
	}

	// Enter plays the row under the cursor.
	h.keys(ch('g'), ch('g'), ch('j'))
	h.keys(tea.Key{Code: tea.KeyEnter})
	if h.current() != q.Tracks()[1].VideoID || h.lastItem() != "url:"+q.Tracks()[1].VideoID {
		t.Fatalf("enter: current %s last %s", h.current(), h.lastItem())
	}

	// Reordering a filtered view is refused with a hint.
	h.keys(ch('/'), ch('s'), tea.Key{Code: tea.KeyEnter}, ch('J'))
	if !strings.Contains(plain(h.m), "Clear the filter and sort to reorder") {
		t.Fatal("no reorder hint")
	}
}
