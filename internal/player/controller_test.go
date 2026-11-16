package player_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/Franciss-Prog/go-ytm/internal/clock"
	"github.com/Franciss-Prog/go-ytm/internal/player"
	"github.com/Franciss-Prog/go-ytm/internal/player/fakempv"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// fakeLauncher hands out fakempv instances over net.Pipe.
type fakeLauncher struct {
	mu       sync.Mutex
	servers  []*fakempv.Server
	opts     []player.LaunchOptions
	failNext int  // fail this many upcoming launches
	failAll  bool // fail every launch
	setup    func(*fakempv.Server)
	launched chan struct{}
}

func newLauncher() *fakeLauncher { return &fakeLauncher{launched: make(chan struct{}, 16)} }

type fakeEngine struct {
	conn io.ReadWriteCloser
	srv  *fakempv.Server
}

func (e *fakeEngine) Conn() io.ReadWriteCloser { return e.conn }
func (e *fakeEngine) Exited() <-chan struct{}  { return e.srv.Exited() }
func (e *fakeEngine) Kill() error              { e.srv.Crash(); return nil }

func (l *fakeLauncher) Launch(_ context.Context, opts player.LaunchOptions) (player.Engine, error) {
	l.mu.Lock()
	defer func() { l.launched <- struct{}{} }()
	defer l.mu.Unlock()
	l.opts = append(l.opts, opts)
	if l.failAll || l.failNext > 0 {
		l.failNext--
		return nil, errors.New("spawn failed")
	}
	srv := fakempv.New()
	if l.setup != nil {
		l.setup(srv)
	}
	cli, side := net.Pipe()
	go srv.Serve(side)
	l.servers = append(l.servers, srv)
	return &fakeEngine{conn: cli, srv: srv}, nil
}

func (l *fakeLauncher) server(i int) *fakempv.Server {
	l.mu.Lock()
	defer l.mu.Unlock()
	if i >= len(l.servers) {
		return nil
	}
	return l.servers[i]
}

func (l *fakeLauncher) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.opts)
}

func start(t *testing.T, l *fakeLauncher, mod ...func(*player.Options)) *player.Controller {
	t.Helper()
	opts := player.Options{Launcher: l, Volume: 70, QuitTimeout: 500 * time.Millisecond}
	for _, m := range mod {
		m(&opts)
	}
	c := player.Start(context.Background(), opts)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := c.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return c
}

// waitEvent reads events until pred matches.
func waitEvent(t *testing.T, c *player.Controller, desc string, pred func(player.Event) bool) player.Event {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-c.Events():
			if !ok {
				t.Fatalf("events closed waiting for %s", desc)
			}
			if pred(ev) {
				return ev
			}
		case <-deadline:
			t.Fatalf("timeout waiting for %s (state %+v)", desc, c.State())
		}
	}
}

func waitState(t *testing.T, c *player.Controller, desc string, pred func(player.State) bool) {
	t.Helper()
	waitEvent(t, c, desc, func(ev player.Event) bool {
		sc, ok := ev.(player.StateChanged)
		return ok && pred(sc.State)
	})
}

func waitUntil(t *testing.T, desc string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout: %s", desc)
		}
		time.Sleep(time.Millisecond)
	}
}

func item(id string) player.Item { return player.Item{ID: id, URL: "https://x/" + id} }

func isStarted(id string) func(player.Event) bool {
	return func(ev player.Event) bool {
		ts, ok := ev.(player.TrackStarted)
		return ok && ts.Item.ID == id
	}
}

func TestLazyLaunchAndLoad(t *testing.T) {
	l := newLauncher()
	c := start(t, l)
	if err := c.SetVolume(150); err != nil {
		t.Fatal(err)
	}
	waitState(t, c, "volume", func(s player.State) bool { return s.Volume == 100 })
	if l.count() != 0 {
		t.Fatal("mpv launched before a command needed it")
	}

	if err := c.Load(context.Background(), item("a"), 12*time.Second); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, c, "TrackStarted a", isStarted("a"))
	waitState(t, c, "playing", func(s player.State) bool { return s.Status == player.Playing })
	st := c.State()
	if st.Item.ID != "a" || st.Duration != 180*time.Second || st.Position != 12*time.Second {
		t.Fatalf("state = %+v", st)
	}
	if l.opts[0].Volume != 100 {
		t.Fatalf("launch volume = %d, want 100", l.opts[0].Volume)
	}
	srv := l.server(0)
	if n := srv.CountCommand("observe_property"); n != 9 {
		t.Fatalf("observe_property × %d, want 9", n)
	}
	if v := srv.Prop("volume"); v != 100.0 {
		t.Fatalf("mpv volume = %v", v)
	}
}

func TestPauseToggleAndMute(t *testing.T) {
	l := newLauncher()
	c := start(t, l)
	_ = c.TogglePause() // nothing loaded: ignored, no launch
	_ = c.Load(context.Background(), item("a"), 0)
	waitState(t, c, "playing", func(s player.State) bool { return s.Status == player.Playing })
	_ = c.TogglePause()
	waitState(t, c, "paused", func(s player.State) bool { return s.Status == player.Paused && s.Paused })
	_ = c.Play()
	waitState(t, c, "playing again", func(s player.State) bool { return s.Status == player.Playing })
	_ = c.Pause()
	waitState(t, c, "paused again", func(s player.State) bool { return s.Status == player.Paused })
	_ = c.ToggleMute()
	waitState(t, c, "muted", func(s player.State) bool { return s.Muted })
	waitUntil(t, "mpv muted", func() bool { return l.server(0).Prop("mute") == true })
}

func TestSeekCoalescing(t *testing.T) {
	l := newLauncher()
	release := make(chan struct{})
	var once sync.Once
	l.setup = func(s *fakempv.Server) {
		s.SetHook(func(s *fakempv.Server, id int64, cmd []any) bool {
			if cmd[0] != "seek" {
				return false
			}
			held := false
			once.Do(func() { held = true })
			if !held {
				return false
			}
			go func() { // hold the first seek's reply until released
				<-release
				s.SetHook(nil)
				s.SetProp("time-pos", 1.0)
				s.Reply(id, "success", nil)
			}()
			return true
		})
	}
	c := start(t, l)
	_ = c.Load(context.Background(), item("a"), 0)
	waitState(t, c, "playing", func(s player.State) bool { return s.Status == player.Playing })

	_ = c.Seek(time.Second, player.SeekRelative)
	waitUntil(t, "first seek in flight", func() bool { return l.server(0).CountCommand("seek") == 1 })
	for range 99 {
		_ = c.Seek(time.Second, player.SeekRelative)
	}
	close(release)
	waitUntil(t, "coalesced seek applied", func() bool { return l.server(0).Prop("time-pos") == 100.0 })
	if n := l.server(0).CountCommand("seek"); n > 2 {
		t.Fatalf("100 seeks → %d IPC calls, want ≤ 2", n)
	}
}

func TestSeekWhileLoadingIsBuffered(t *testing.T) {
	l := newLauncher()
	release := make(chan struct{})
	l.setup = func(s *fakempv.Server) {
		s.SetHook(func(s *fakempv.Server, _ int64, cmd []any) bool {
			if cmd[0] == "loadfile" {
				<-release // stall loadfile so the player stays Loading
			}
			return false
		})
	}
	c := start(t, l)
	_ = c.Load(context.Background(), item("a"), 0)
	waitUntil(t, "loading", func() bool { return c.State().Status == player.Loading })
	_ = c.Seek(30*time.Second, player.SeekAbsolute)
	_ = c.Seek(5*time.Second, player.SeekRelative) // merges with the absolute seek: 35 s
	close(release)
	waitUntil(t, "buffered seek applied", func() bool { return l.server(0).Prop("time-pos") == 35.0 })
}

func TestGaplessAdvance(t *testing.T) {
	l := newLauncher()
	c := start(t, l)
	_ = c.Load(context.Background(), item("a"), 0)
	waitEvent(t, c, "a started", isStarted("a"))
	_ = c.Append(item("b"))
	waitUntil(t, "b preloaded", func() bool { return len(l.server(0).PlaylistURLs()) == 2 })
	if c.State().Next.ID != "b" {
		t.Fatalf("Next = %+v", c.State().Next)
	}

	l.server(0).FinishTrack()
	ended := waitEvent(t, c, "a ended", func(ev player.Event) bool { _, ok := ev.(player.TrackEnded); return ok })
	if te := ended.(player.TrackEnded); te.Item.ID != "a" || te.Reason != player.EndNatural {
		t.Fatalf("ended = %+v", te)
	}
	waitEvent(t, c, "b started", isStarted("b"))
	waitUntil(t, "played entry removed", func() bool {
		return slices.Equal(l.server(0).PlaylistURLs(), []string{"https://x/b"})
	})
	st := c.State()
	if st.Item.ID != "b" || !st.Next.IsZero() {
		t.Fatalf("state = %+v", st)
	}

	_ = c.Append(item("c"))
	_ = c.ClearNext()
	waitUntil(t, "next cleared", func() bool {
		return slices.Equal(l.server(0).PlaylistURLs(), []string{"https://x/b"})
	})
	waitUntil(t, "Next zero", func() bool { return c.State().Next.IsZero() })
}

func TestNaturalEndStops(t *testing.T) {
	l := newLauncher()
	c := start(t, l)
	_ = c.Load(context.Background(), item("a"), 0)
	waitState(t, c, "playing", func(s player.State) bool { return s.Status == player.Playing })
	l.server(0).FinishTrack()
	waitState(t, c, "stopped", func(s player.State) bool { return s.Status == player.Stopped && s.Item.IsZero() })
}

func TestLoadErrorReported(t *testing.T) {
	l := newLauncher()
	l.setup = func(s *fakempv.Server) { s.FailURL("https://x/bad") }
	c := start(t, l)
	_ = c.Load(context.Background(), item("bad"), 0)
	ev := waitEvent(t, c, "error end", func(ev player.Event) bool { _, ok := ev.(player.TrackEnded); return ok })
	if te := ev.(player.TrackEnded); te.Reason != player.EndError || te.Err != "loading failed" {
		t.Fatalf("ended = %+v", te)
	}
	waitUntil(t, "error status", func() bool { return c.State().Status == player.Error })

	_ = c.Load(context.Background(), item("good"), 0) // recovers from Error
	waitEvent(t, c, "good started", isStarted("good"))
}

func TestRapidLoadsEndOnLast(t *testing.T) {
	l := newLauncher()
	c := start(t, l)
	_ = c.Load(context.Background(), item("a"), 0)
	waitEvent(t, c, "a", isStarted("a"))
	for _, id := range []string{"b", "c", "d"} {
		_ = c.Load(context.Background(), item(id), 0)
	}
	waitEvent(t, c, "d", isStarted("d"))
	waitState(t, c, "playing d", func(s player.State) bool { return s.Item.ID == "d" && s.Status == player.Playing })
	// Replaced entries end with reason=stop; none may surface as TrackEnded.
	drain := time.After(50 * time.Millisecond)
	for {
		select {
		case ev := <-c.Events():
			if te, ok := ev.(player.TrackEnded); ok {
				t.Fatalf("unexpected %+v", te)
			}
		case <-drain:
			return
		}
	}
}

func TestCrashRestoresPlayback(t *testing.T) {
	l := newLauncher()
	c := start(t, l)
	_ = c.Load(context.Background(), item("a"), 0)
	waitState(t, c, "playing", func(s player.State) bool { return s.Status == player.Playing })
	_ = c.Seek(42*time.Second, player.SeekAbsolute)
	_ = c.Pause()
	_ = c.SetVolume(33)
	waitState(t, c, "paused at 42", func(s player.State) bool {
		return s.Paused && s.Position == 42*time.Second && s.Volume == 33
	})

	l.server(0).Crash()
	waitEvent(t, c, "crash", func(ev player.Event) bool {
		ce, ok := ev.(player.EngineCrashed)
		return ok && !ce.GaveUp
	})
	rs := waitEvent(t, c, "restart", func(ev player.Event) bool { _, ok := ev.(player.Restarted); return ok })
	if rs.(player.Restarted).Resumed.ID != "a" {
		t.Fatalf("restarted = %+v", rs)
	}
	waitState(t, c, "resumed paused", func(s player.State) bool {
		return s.Status == player.Paused && s.Item.ID == "a" && s.Position == 42*time.Second
	})
	srv := l.server(1)
	if srv.Prop("volume") != 33.0 || srv.Prop("pause") != true {
		t.Fatalf("restored volume=%v pause=%v", srv.Prop("volume"), srv.Prop("pause"))
	}
}

func TestCrashWhileIdleRelaunchesLazily(t *testing.T) {
	l := newLauncher()
	c := start(t, l)
	_ = c.Load(context.Background(), item("a"), 0)
	waitState(t, c, "playing", func(s player.State) bool { return s.Status == player.Playing })
	l.server(0).FinishTrack()
	waitState(t, c, "stopped", func(s player.State) bool { return s.Status == player.Stopped })
	l.server(0).Crash()
	time.Sleep(20 * time.Millisecond)
	if l.count() != 1 {
		t.Fatalf("relaunched eagerly: %d launches", l.count())
	}
	_ = c.Load(context.Background(), item("b"), 0)
	waitEvent(t, c, "b", isStarted("b"))
}

func TestRestartBackoffAndGiveUp(t *testing.T) {
	l := newLauncher()
	fc := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	c := start(t, l, func(o *player.Options) { o.Clock = fc })
	_ = c.Load(context.Background(), item("a"), 0)
	waitEvent(t, c, "a", isStarted("a"))
	<-l.launched

	l.mu.Lock()
	l.failAll = true
	l.mu.Unlock()
	l.server(0).Crash()

	// Attempt 1 is immediate.
	<-l.launched
	// Attempt 2 waits 250 ms, attempt 3 waits 1 s.
	for i, d := range []time.Duration{250 * time.Millisecond, time.Second} {
		fc.BlockUntil(1, nil)
		select {
		case <-l.launched:
			t.Fatalf("attempt %d ran before its backoff", i+2)
		case <-time.After(10 * time.Millisecond):
		}
		fc.Advance(d)
		<-l.launched
	}
	ev := waitEvent(t, c, "give up", func(ev player.Event) bool {
		ce, ok := ev.(player.EngineCrashed)
		return ok && ce.GaveUp
	})
	if ev.(player.EngineCrashed).Err == nil {
		t.Fatal("give-up without error")
	}
	if st := c.State(); st.Status != player.Stopped || !st.Item.IsZero() {
		t.Fatalf("state after give-up = %+v", st)
	}
	if l.count() != 4 {
		t.Fatalf("launches = %d, want 4 (initial + 3 restarts)", l.count())
	}
}

func TestInitialLaunchFailure(t *testing.T) {
	l := newLauncher()
	l.failNext = 1
	c := start(t, l)
	_ = c.Load(context.Background(), item("a"), 0)
	waitEvent(t, c, "engine error", func(ev player.Event) bool { _, ok := ev.(player.EngineError); return ok })
	_ = c.Load(context.Background(), item("a"), 0) // next attempt launches again
	waitEvent(t, c, "a", isStarted("a"))
}

func TestPositionThrottle(t *testing.T) {
	l := newLauncher()
	fc := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	c := start(t, l, func(o *player.Options) { o.Clock = fc })
	_ = c.Load(context.Background(), item("a"), 0)
	waitState(t, c, "playing", func(s player.State) bool { return s.Status == player.Playing })
	srv := l.server(0)
	for i := 1; i <= 50; i++ {
		srv.SetProp("time-pos", float64(i)/10)
	}
	waitUntil(t, "snapshot current", func() bool { return c.State().Position == 5*time.Second })
	// State() is current, but no StateChanged was emitted for the burst.
	select {
	case ev := <-c.Events():
		if sc, ok := ev.(player.StateChanged); ok && sc.State.Position > 0 {
			t.Fatalf("position event not throttled: %v", sc.State.Position)
		}
	case <-time.After(20 * time.Millisecond):
	}
	fc.Advance(250 * time.Millisecond)
	waitState(t, c, "throttled flush", func(s player.State) bool { return s.Position == 5*time.Second })
}

func TestCloseQuitsMPV(t *testing.T) {
	l := newLauncher()
	c := player.Start(context.Background(), player.Options{Launcher: l})
	_ = c.Load(context.Background(), item("a"), 0)
	waitEvent(t, c, "a", isStarted("a"))
	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv := l.server(0)
	if !slices.Contains(srv.Commands(), "quit") {
		t.Fatal("quit not sent")
	}
	if !srv.Wait(time.Second) {
		t.Fatal("mpv still running after Close")
	}
	if _, ok := <-c.Events(); ok {
		// drain until closed
		for range c.Events() {
		}
	}
	if err := c.Load(context.Background(), item("b"), 0); !errors.Is(err, player.ErrClosed) {
		t.Fatalf("Load after Close = %v", err)
	}
}

func TestCloseKillsWedgedMPV(t *testing.T) {
	l := newLauncher()
	l.setup = func(s *fakempv.Server) {
		s.SetHook(func(_ *fakempv.Server, _ int64, cmd []any) bool { return cmd[0] == "quit" })
	}
	c := player.Start(context.Background(), player.Options{Launcher: l, QuitTimeout: 50 * time.Millisecond})
	_ = c.Load(context.Background(), item("a"), 0)
	waitEvent(t, c, "a", isStarted("a"))
	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !l.server(0).Wait(time.Second) {
		t.Fatal("wedged mpv not killed")
	}
}

func TestContextCancelShutsDown(t *testing.T) {
	l := newLauncher()
	ctx, cancel := context.WithCancel(context.Background())
	c := player.Start(ctx, player.Options{Launcher: l})
	_ = c.Load(context.Background(), item("a"), 0)
	waitEvent(t, c, "a", isStarted("a"))
	cancel()
	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentCommandsAndState(t *testing.T) {
	l := newLauncher()
	c := start(t, l)
	_ = c.Load(context.Background(), item("a"), 0)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 200 {
				switch (g + i) % 4 {
				case 0:
					_ = c.Seek(time.Second, player.SeekRelative)
				case 1:
					_ = c.SetVolume(i % 100)
				case 2:
					_ = c.TogglePause()
				default:
					_ = c.State()
				}
			}
		})
	}
	go func() { // keep draining so nothing backs up
		for range c.Events() {
		}
	}()
	wg.Wait()
}

func ExampleController() {
	c := player.Start(context.Background(), player.Options{Launcher: &player.MPVLauncher{RuntimeDir: "/tmp/ytm-go-example"}})
	defer func() { _ = c.Close(context.Background()) }()
	fmt.Println(c.State().Status)
	// Output: stopped
}
