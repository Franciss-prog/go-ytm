package player

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/clock"
	"github.com/Franciss-Prog/go-ytm/internal/player/mpvipc"
)

// Observed property IDs (spec 02 table).
const (
	propPause = iota + 1
	propIdle
	propTimePos
	propDuration
	propVolume
	propMute
	propPlaylistPos
	propPausedForCache
	propCacheTime
)

var observed = []struct {
	id   int
	name string
}{
	{propPause, "pause"},
	{propIdle, "idle-active"},
	{propTimePos, "time-pos"},
	{propDuration, "duration"},
	{propVolume, "volume"},
	{propMute, "mute"},
	{propPlaylistPos, "playlist-pos"},
	{propPausedForCache, "paused-for-cache"},
	{propCacheTime, "demuxer-cache-time"},
}

// Restart policy: delays before relaunch attempt 1, 2, 3; give up after
// maxFailures failures within failureWindow.
var restartBackoff = []time.Duration{0, 250 * time.Millisecond, time.Second}

const (
	maxFailures   = 3
	failureWindow = 30 * time.Second
)

// Options configure a Controller.
type Options struct {
	Launcher Launcher
	Clock    clock.Clock  // default clock.Real{}
	Log      *slog.Logger // default discard
	Volume   int          // initial volume, 0–100
	// CallTimeout bounds each mpv command; a timed-out command is abandoned
	// (default 5 s).
	CallTimeout time.Duration
	// PositionInterval throttles position-only StateChanged events
	// (default 250 ms = 4 Hz).
	PositionInterval time.Duration
	// QuitTimeout is how long Close waits for mpv to quit before killing it
	// (default 2 s).
	QuitTimeout time.Duration
}

// Controller implements Player. Create with Start; always Close.
type Controller struct {
	opts  Options
	log   *slog.Logger
	clk   clock.Clock
	state atomic.Pointer[State]

	inMu   sync.Mutex
	inbox  []command
	inWake chan struct{}

	out *outbox

	stop     chan struct{}
	stopOnce sync.Once
	done     chan struct{}
}

// Start creates a Controller and its goroutines. mpv itself is launched
// lazily on the first command that needs it. Cancelling ctx shuts the
// controller down like Close.
func Start(ctx context.Context, opts Options) *Controller {
	if opts.Clock == nil {
		opts.Clock = clock.Real{}
	}
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}
	if opts.CallTimeout == 0 {
		opts.CallTimeout = 5 * time.Second
	}
	if opts.PositionInterval == 0 {
		opts.PositionInterval = 250 * time.Millisecond
	}
	if opts.QuitTimeout == 0 {
		opts.QuitTimeout = 2 * time.Second
	}
	opts.Volume = clampVolume(opts.Volume)
	c := &Controller{
		opts:   opts,
		log:    opts.Log,
		clk:    opts.Clock,
		inWake: make(chan struct{}, 1),
		out:    newOutbox(),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	c.state.Store(&State{Volume: opts.Volume})
	l := &loop{c: c, st: State{Volume: opts.Volume}, vol: opts.Volume, entries: map[int64]Item{}}
	go c.out.run(c.done)
	go func() {
		defer close(c.done)
		l.run(ctx)
	}()
	return c
}

// State returns the latest snapshot without locking.
func (c *Controller) State() State { return *c.state.Load() }

// Events delivers player events. It is closed after Close returns.
func (c *Controller) Events() <-chan Event { return c.out.ch }

// Close stops mpv (quit, then kill after QuitTimeout) and all goroutines.
func (c *Controller) Close(ctx context.Context) error {
	c.stopOnce.Do(func() { close(c.stop) })
	select {
	case <-c.done:
		<-c.out.exited
		return nil
	case <-ctx.Done():
		return fmt.Errorf("close player: %w", ctx.Err())
	}
}

// Load replaces the current track. ctx is accepted for interface symmetry;
// the call never blocks.
func (c *Controller) Load(_ context.Context, item Item, startAt time.Duration) error {
	return c.enqueue(command{kind: cmdLoad, item: item, d: startAt})
}

// Append preloads item as the gapless next track.
func (c *Controller) Append(item Item) error {
	return c.enqueue(command{kind: cmdAppend, item: item})
}

// ClearNext removes preloaded entries.
func (c *Controller) ClearNext() error { return c.enqueue(command{kind: cmdClearNext}) }

// TogglePause flips pause.
func (c *Controller) TogglePause() error { return c.enqueue(command{kind: cmdTogglePause}) }

// Play unpauses.
func (c *Controller) Play() error { return c.enqueue(command{kind: cmdPause, on: false}) }

// Pause pauses.
func (c *Controller) Pause() error { return c.enqueue(command{kind: cmdPause, on: true}) }

// Seek moves the playhead. Consecutive seeks are coalesced.
func (c *Controller) Seek(d time.Duration, mode SeekMode) error {
	return c.enqueue(command{kind: cmdSeek, d: d, abs: mode == SeekAbsolute})
}

// SetVolume sets volume (clamped 0–100). Consecutive calls are coalesced.
func (c *Controller) SetVolume(v int) error {
	return c.enqueue(command{kind: cmdVolume, vol: clampVolume(v)})
}

// ToggleMute flips mute.
func (c *Controller) ToggleMute() error { return c.enqueue(command{kind: cmdToggleMute}) }

type cmdKind int

const (
	cmdLoad cmdKind = iota
	cmdAppend
	cmdClearNext
	cmdTogglePause
	cmdPause
	cmdSeek
	cmdVolume
	cmdToggleMute
)

type command struct {
	kind cmdKind
	item Item
	d    time.Duration
	abs  bool
	on   bool
	vol  int
}

// enqueue adds a command, coalescing with the tail when it is the same kind:
// loads and volumes keep the newest, relative seeks add up. Only the tail is
// merged so ordering against other commands is preserved.
func (c *Controller) enqueue(cmd command) error {
	select {
	case <-c.stop:
		return ErrClosed
	case <-c.done:
		return ErrClosed
	default:
	}
	c.inMu.Lock()
	if n := len(c.inbox); n > 0 && c.inbox[n-1].kind == cmd.kind {
		tail := &c.inbox[n-1]
		switch cmd.kind {
		case cmdLoad, cmdVolume, cmdPause:
			*tail = cmd
			cmd.kind = -1
		case cmdSeek:
			if cmd.abs {
				*tail = cmd
			} else {
				tail.d += cmd.d
			}
			cmd.kind = -1
		}
	}
	if cmd.kind != -1 {
		c.inbox = append(c.inbox, cmd)
	}
	c.inMu.Unlock()
	select {
	case c.inWake <- struct{}{}:
	default:
	}
	return nil
}

func (c *Controller) takeInbox() (command, bool) {
	c.inMu.Lock()
	defer c.inMu.Unlock()
	if len(c.inbox) == 0 {
		return command{}, false
	}
	cmd := c.inbox[0]
	c.inbox = c.inbox[1:]
	return cmd, true
}

func (c *Controller) peekInbox() (command, bool) {
	c.inMu.Lock()
	defer c.inMu.Unlock()
	if len(c.inbox) == 0 {
		return command{}, false
	}
	return c.inbox[0], true
}

func (c *Controller) dropInbox() {
	c.inMu.Lock()
	c.inbox = nil
	c.inMu.Unlock()
}

// mpvCmd is one IPC request issued by the loop.
type mpvCmd struct {
	args    []any
	onReply func(json.RawMessage, error)
}

type launchResult struct {
	eng Engine
	err error
}

type restoreInfo struct {
	item   Item
	pos    time.Duration
	paused bool
}

// loop owns all mutable player state; only its goroutine touches it.
type loop struct {
	c  *Controller
	st State

	// mpv instance
	eng      Engine
	conn     *mpvipc.Conn
	replies  chan mpvipc.Reply
	launchCh chan launchResult
	launchCx context.CancelFunc

	// command pipeline: queue (internal, priority) then inbox (user)
	queue     []mpvCmd
	inflight  *mpvCmd
	inflightI int64
	callTimer clock.Timer

	// playlist tracking
	entries     map[int64]Item // mpv playlist_entry_id → item
	curEntry    int64
	nextEntry   int64
	loadGen     int
	orphanStart int64 // start-file seen before its loadfile reply

	vol         int
	muted       bool
	pendingSeek *command // seek requested while Loading

	// crash supervision
	failures     []time.Time
	restartTimer clock.Timer
	restore      *restoreInfo

	// position throttle
	lastEmit time.Time
	posTimer clock.Timer
}

func (l *loop) run(ctx context.Context) {
	// base carries ctx's values but not its cancellation: shutdown must
	// still be able to talk to mpv after ctx is cancelled.
	base := context.WithoutCancel(ctx)
	defer l.shutdown(base)
	for {
		l.pump(base)
		var (
			evCh       <-chan mpvipc.Event
			connDone   <-chan struct{}
			exited     <-chan struct{}
			callT      <-chan time.Time
			restartT   <-chan time.Time
			posT       <-chan time.Time
			repliesCh  <-chan mpvipc.Reply
			launchedCh <-chan launchResult
		)
		if l.conn != nil {
			evCh, connDone, repliesCh = l.conn.Events(), l.conn.Done(), l.replies
		}
		if l.eng != nil {
			exited = l.eng.Exited()
		}
		if l.callTimer != nil {
			callT = l.callTimer.C()
		}
		if l.restartTimer != nil {
			restartT = l.restartTimer.C()
		}
		if l.posTimer != nil {
			posT = l.posTimer.C()
		}
		if l.launchCh != nil {
			launchedCh = l.launchCh
		}
		select {
		case <-ctx.Done():
			return
		case <-l.c.stop:
			return
		case <-l.c.inWake:
		case ev := <-evCh:
			l.handleEvent(ev)
		case r := <-repliesCh:
			l.handleReply(r)
		case <-callT:
			l.callTimer = nil
			if l.inflight != nil {
				l.c.log.Warn("mpv command timed out; abandoning", "command", l.inflight.args[0])
				l.conn.Forget(l.inflightI)
				cb := l.inflight.onReply
				l.inflight = nil
				if cb != nil {
					cb(nil, context.DeadlineExceeded)
				}
			}
		case <-connDone:
			l.crashed(l.conn.Err())
		case <-exited:
			l.crashed(errors.New("mpv process exited"))
		case <-restartT:
			l.restartTimer = nil
			l.launch(base)
		case res := <-launchedCh:
			l.launched(res)
		case <-posT:
			l.posTimer = nil
			l.publish(true)
		}
	}
}

// pump dispatches the next command when nothing is in flight.
func (l *loop) pump(ctx context.Context) {
	for l.inflight == nil {
		if len(l.queue) > 0 {
			if l.conn == nil {
				l.queue = nil
				continue
			}
			cmd := l.queue[0]
			l.queue = l.queue[1:]
			l.send(ctx, cmd)
			continue
		}
		if l.launchCh != nil || l.restartTimer != nil {
			return // wait for mpv before touching user commands
		}
		cmd, ok := l.c.peekInbox()
		if !ok {
			return
		}
		if needsEngine(cmd) && l.eng == nil {
			l.launch(ctx)
			return
		}
		_, _ = l.c.takeInbox()
		l.apply(cmd)
	}
}

func needsEngine(cmd command) bool { return cmd.kind == cmdLoad || cmd.kind == cmdAppend }

func (l *loop) send(ctx context.Context, cmd mpvCmd) {
	id, err := l.conn.Send(ctx, l.replies, cmd.args...)
	if err != nil {
		if cmd.onReply != nil {
			cmd.onReply(nil, err)
		}
		return
	}
	l.inflight, l.inflightI = &cmd, id
	l.callTimer = l.c.clk.NewTimer(l.c.opts.CallTimeout)
}

func (l *loop) handleReply(r mpvipc.Reply) {
	if l.inflight == nil || r.ID != l.inflightI {
		return
	}
	cmd := l.inflight
	l.inflight = nil
	if l.callTimer != nil {
		l.callTimer.Stop()
		l.callTimer = nil
	}
	if r.Err != nil {
		l.c.log.Debug("mpv command failed", "command", cmd.args[0], "err", r.Err)
	}
	if cmd.onReply != nil {
		cmd.onReply(r.Data, r.Err)
	}
}

func (l *loop) push(cb func(json.RawMessage, error), args ...any) {
	l.queue = append(l.queue, mpvCmd{args: args, onReply: cb})
}

// apply turns a user command into state changes and mpv commands.
func (l *loop) apply(cmd command) {
	switch cmd.kind {
	case cmdLoad:
		l.load(cmd.item, cmd.d, false)
	case cmdAppend:
		if l.st.Item.IsZero() {
			l.c.log.Debug("append with nothing playing; ignored", "id", cmd.item.ID)
			return
		}
		gen := l.loadGen
		item := cmd.item
		l.st.Next = item
		l.push(func(data json.RawMessage, err error) {
			if err != nil || gen != l.loadGen {
				if err != nil {
					l.c.log.Warn("preload failed", "id", item.ID, "err", err)
					l.st.Next = Item{}
					l.publish(false)
				}
				return
			}
			if id, ok := entryID(data); ok {
				l.entries[id] = item
				l.nextEntry = id
			}
		}, "loadfile", item.URL, "append")
		l.publish(false)
	case cmdClearNext:
		if l.conn == nil {
			return
		}
		l.forgetNext()
		l.push(nil, "playlist-clear")
		l.publish(false)
	case cmdTogglePause:
		if l.conn != nil && !l.st.Item.IsZero() {
			l.push(nil, "cycle", "pause")
		}
	case cmdPause:
		if l.conn != nil && !l.st.Item.IsZero() {
			l.push(nil, "set_property", "pause", cmd.on)
		}
	case cmdSeek:
		switch {
		case l.st.Item.IsZero():
		case l.st.Status == Loading || l.st.Status == Crashed:
			l.bufferSeek(cmd)
		case l.conn != nil:
			l.pushSeek(cmd)
		}
	case cmdVolume:
		l.vol = cmd.vol
		l.st.Volume = cmd.vol
		if l.conn != nil {
			l.push(nil, "set_property", "volume", cmd.vol)
		}
		l.publish(false)
	case cmdToggleMute:
		l.muted = !l.muted
		l.st.Muted = l.muted
		if l.conn != nil {
			l.push(nil, "set_property", "mute", l.muted)
		}
		l.publish(false)
	}
}

func (l *loop) bufferSeek(cmd command) {
	switch {
	case l.pendingSeek == nil || cmd.abs:
		l.pendingSeek = &cmd
	default:
		l.pendingSeek.d += cmd.d
	}
}

func (l *loop) pushSeek(cmd command) {
	secs := strconv.FormatFloat(cmd.d.Seconds(), 'f', 3, 64)
	if cmd.abs {
		l.push(nil, "seek", secs, "absolute+exact")
	} else {
		l.push(nil, "seek", secs, "relative")
	}
}

// load issues loadfile replace. paused selects the initial pause state
// (used by crash restore); a user Load always plays.
func (l *loop) load(item Item, startAt time.Duration, paused bool) {
	l.loadGen++
	gen := l.loadGen
	l.entries = map[int64]Item{}
	l.curEntry, l.nextEntry, l.orphanStart = 0, 0, 0
	l.pendingSeek = nil
	l.st.Item, l.st.Next = item, Item{}
	l.st.Position, l.st.Duration, l.st.CacheAhead = startAt, 0, 0
	l.st.Err = ""
	l.st.Status = nextStatus(l.st.Status, trLoad, false)
	l.push(nil, "set_property", "pause", paused)
	start := "start=" + strconv.FormatFloat(startAt.Seconds(), 'f', 3, 64)
	l.push(func(data json.RawMessage, err error) {
		if gen != l.loadGen {
			return
		}
		if err != nil {
			l.trackError(item, err.Error())
			return
		}
		id, ok := entryID(data)
		if !ok {
			return
		}
		l.entries[id] = item
		l.curEntry = id
		if l.orphanStart == id {
			l.orphanStart = 0
			l.started(id)
		}
	}, mpvipc.Named{"name": "loadfile", "url": item.URL, "flags": "replace", "options": start})
	l.publish(false)
}

func entryID(data json.RawMessage) (int64, bool) {
	var v struct {
		ID *int64 `json:"playlist_entry_id"`
	}
	if json.Unmarshal(data, &v) != nil || v.ID == nil {
		return 0, false
	}
	return *v.ID, true
}

func (l *loop) forgetNext() {
	if l.nextEntry != 0 {
		delete(l.entries, l.nextEntry)
		l.nextEntry = 0
	}
	l.st.Next = Item{}
}

func (l *loop) handleEvent(ev mpvipc.Event) {
	switch ev.Name {
	case "property-change":
		l.propertyChanged(ev)
	case "start-file":
		if _, ok := l.entries[ev.EntryID]; ok {
			l.started(ev.EntryID)
		} else if l.curEntry == 0 {
			l.orphanStart = ev.EntryID // reply to our loadfile not seen yet
		}
	case "playback-restart":
		was := l.st.Status
		l.st.Status = nextStatus(l.st.Status, trPlaybackRestart, l.st.Paused)
		if was == Loading && l.pendingSeek != nil {
			l.pushSeek(*l.pendingSeek)
			l.pendingSeek = nil
		}
		l.publish(false)
	case "end-file":
		item, ok := l.entries[ev.EntryID]
		if !ok {
			return // stale entry from a replaced load
		}
		switch ev.Reason {
		case "eof":
			delete(l.entries, ev.EntryID)
			l.c.out.send(TrackEnded{Item: item, Reason: EndNatural})
		case "error":
			delete(l.entries, ev.EntryID)
			msg := ev.FileError
			if msg == "" {
				msg = "playback error"
			}
			if ev.EntryID == l.nextEntry {
				// A failed preload must not stop the current track.
				l.forgetNext()
				l.c.out.send(TrackEnded{Item: item, Reason: EndError, Err: msg})
				l.publish(false)
				return
			}
			l.trackError(item, msg)
		}
	}
}

func (l *loop) trackError(item Item, msg string) {
	l.st.Status = nextStatus(l.st.Status, trEndError, false)
	l.st.Err = msg
	l.c.out.send(TrackEnded{Item: item, Reason: EndError, Err: msg})
	l.publish(false)
}

func (l *loop) started(id int64) {
	item := l.entries[id]
	gapless := l.curEntry != 0 && id != l.curEntry
	l.curEntry = id
	if id == l.nextEntry {
		l.nextEntry = 0
		l.st.Next = Item{}
	}
	l.st.Item = item
	l.st.Status = nextStatus(l.st.Status, trStartFile, false)
	l.st.Position, l.st.Duration = 0, 0
	if gapless {
		// Keep mpv's playlist at current (+ next) so indexes never drift.
		l.push(nil, "playlist-clear")
	}
	l.c.out.send(TrackStarted{Item: item})
	l.publish(false)
}

func (l *loop) propertyChanged(ev mpvipc.Event) {
	posOnly := false
	switch ev.ID {
	case propPause:
		var b bool
		_ = json.Unmarshal(ev.Data, &b)
		l.st.Paused = b
		l.st.Status = nextStatus(l.st.Status, trPause, b)
	case propIdle:
		var b bool
		_ = json.Unmarshal(ev.Data, &b)
		if b {
			was := l.st.Status
			l.st.Status = nextStatus(l.st.Status, trIdle, true)
			if was != Stopped && l.st.Status == Stopped {
				l.st.Item, l.st.Next = Item{}, Item{}
				l.st.Position, l.st.Duration = 0, 0
				l.entries = map[int64]Item{}
				l.curEntry, l.nextEntry = 0, 0
			}
		}
	case propTimePos:
		l.st.Position = seconds(ev.Data)
		posOnly = true
	case propDuration:
		l.st.Duration = seconds(ev.Data)
	case propVolume:
		var f float64
		if json.Unmarshal(ev.Data, &f) == nil {
			l.st.Volume = int(f + 0.5)
			l.vol = l.st.Volume
		}
	case propMute:
		var b bool
		_ = json.Unmarshal(ev.Data, &b)
		l.st.Muted, l.muted = b, b
	case propPausedForCache:
		var b bool
		_ = json.Unmarshal(ev.Data, &b)
		l.st.Status = nextStatus(l.st.Status, trCache, b)
	case propCacheTime:
		l.st.CacheAhead = seconds(ev.Data)
		posOnly = true
	default:
		return
	}
	l.publish(posOnly)
}

func seconds(data json.RawMessage) time.Duration {
	var f float64
	if len(data) == 0 || json.Unmarshal(data, &f) != nil {
		return 0
	}
	return time.Duration(f * float64(time.Second))
}

// publish stores the snapshot and emits StateChanged. Position-only updates
// are throttled to PositionInterval.
func (l *loop) publish(posOnly bool) {
	snap := l.st
	l.c.state.Store(&snap)
	now := l.c.clk.Now()
	if posOnly && now.Sub(l.lastEmit) < l.c.opts.PositionInterval {
		if l.posTimer == nil {
			l.posTimer = l.c.clk.NewTimer(l.c.opts.PositionInterval - now.Sub(l.lastEmit))
		}
		return
	}
	if l.posTimer != nil {
		l.posTimer.Stop()
		l.posTimer = nil
	}
	l.lastEmit = now
	l.c.out.send(StateChanged{State: snap})
}

// launch starts mpv in the background.
func (l *loop) launch(base context.Context) {
	if l.launchCh != nil {
		return
	}
	ctx, cancel := context.WithCancel(base)
	l.launchCx = cancel
	ch := make(chan launchResult, 1)
	l.launchCh = ch
	opts := LaunchOptions{Volume: l.vol}
	launcher := l.c.opts.Launcher
	go func() {
		eng, err := launcher.Launch(ctx, opts)
		ch <- launchResult{eng, err}
	}()
}

func (l *loop) launched(res launchResult) {
	l.launchCh = nil
	l.launchCx()
	if res.err != nil {
		l.c.log.Error("mpv launch failed", "err", res.err)
		if l.restore != nil {
			l.recordFailure(res.err)
			return
		}
		l.c.dropInbox() // queued loads cannot run; the UI shows the error
		l.c.out.send(EngineError{Err: res.err})
		return
	}
	l.eng = res.eng
	l.replies = make(chan mpvipc.Reply, 8)
	l.conn = mpvipc.New(res.eng.Conn(), l.c.log)
	l.queue = nil
	for _, p := range observed {
		l.push(nil, "observe_property", p.id, p.name)
	}
	l.push(nil, "set_property", "volume", l.vol)
	l.push(nil, "set_property", "mute", l.muted)
	if r := l.restore; r != nil {
		l.restore = nil
		if s := l.pendingSeek; s != nil { // seek requested while crashed
			if s.abs {
				r.pos = s.d
			} else {
				r.pos = max(0, r.pos+s.d)
			}
		}
		l.st.Status = nextStatus(l.st.Status, trRestarted, false)
		l.load(r.item, r.pos, r.paused)
		l.c.out.send(Restarted{Resumed: r.item})
		l.c.log.Info("mpv restarted", "resumed", r.item.ID, "position", r.pos)
	}
}

// crashed handles an unexpected mpv death.
func (l *loop) crashed(err error) {
	l.c.log.Warn("mpv died", "err", err, "status", l.st.Status)
	l.teardown()
	if l.restore == nil && !l.st.Item.IsZero() && l.st.Status != Error && l.st.Status != Stopped {
		l.restore = &restoreInfo{item: l.st.Item, pos: l.st.Position, paused: l.st.Paused}
	}
	l.st.Status = nextStatus(l.st.Status, trCrash, false)
	l.st.Next = Item{}
	if l.restore == nil {
		// Nothing was playing: relaunch lazily on the next command.
		l.publish(false)
		return
	}
	l.recordFailure(err)
}

// recordFailure counts a crash or failed relaunch and schedules the next
// attempt, or gives up.
func (l *loop) recordFailure(err error) {
	now := l.c.clk.Now()
	l.failures = slices.DeleteFunc(l.failures, func(t time.Time) bool { return now.Sub(t) >= failureWindow })
	l.failures = append(l.failures, now)
	// The first failure is the crash itself; each later one is a failed
	// restart. Give up once maxFailures restarts have failed in the window.
	if len(l.failures) > maxFailures {
		l.giveUp(err)
		return
	}
	l.c.out.send(EngineCrashed{Err: err})
	l.publish(false)
	l.restartTimer = l.c.clk.NewTimer(restartBackoff[len(l.failures)-1])
}

func (l *loop) giveUp(err error) {
	l.c.log.Error("mpv keeps crashing; giving up", "failures", len(l.failures), "err", err)
	l.restore = nil
	l.failures = nil
	l.st.Status = nextStatus(l.st.Status, trGaveUp, false)
	l.st.Item, l.st.Next = Item{}, Item{}
	l.st.Position, l.st.Duration = 0, 0
	l.c.dropInbox()
	l.c.out.send(EngineCrashed{Err: err, GaveUp: true})
	l.publish(false)
}

// teardown releases the current mpv instance without quitting it politely.
func (l *loop) teardown() {
	if l.conn != nil {
		_ = l.conn.Close()
		l.conn = nil
	}
	if l.eng != nil {
		_ = l.eng.Kill()
		l.eng = nil
	}
	l.replies = nil
	l.queue = nil
	l.inflight = nil
	if l.callTimer != nil {
		l.callTimer.Stop()
		l.callTimer = nil
	}
	l.entries = map[int64]Item{}
	l.curEntry, l.nextEntry, l.orphanStart = 0, 0, 0
}

// shutdown quits mpv politely, then kills it after QuitTimeout.
func (l *loop) shutdown(base context.Context) {
	if l.restartTimer != nil {
		l.restartTimer.Stop()
		l.restartTimer = nil
	}
	if l.posTimer != nil {
		l.posTimer.Stop()
		l.posTimer = nil
	}
	if l.launchCh != nil {
		l.launchCx()
		if res := <-l.launchCh; res.eng != nil {
			_ = res.eng.Kill()
		}
		l.launchCh = nil
	}
	if l.conn != nil && l.eng != nil {
		ctx, cancel := context.WithTimeout(base, l.c.opts.QuitTimeout)
		_, _ = l.conn.Call(ctx, "quit")
		t := l.c.clk.NewTimer(l.c.opts.QuitTimeout)
		select {
		case <-l.eng.Exited():
		case <-t.C():
			l.c.log.Warn("mpv did not quit in time; killing")
		case <-ctx.Done():
		}
		t.Stop()
		cancel()
	}
	l.teardown()
}
