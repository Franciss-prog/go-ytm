package ui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/player"
	"github.com/Franciss-Prog/go-ytm/internal/queue"
	"github.com/Franciss-Prog/go-ytm/internal/stream"
	"github.com/Franciss-Prog/go-ytm/internal/ui/components"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// restartThreshold: previous_track restarts the track when more than this
// far in (ytm-player's rule, see queue.Previous).
const restartThreshold = 3 * time.Second

// Streams resolves playable stream URLs (stream.Cache in production).
// Methods are called from tea.Cmds, never from Update.
type Streams interface {
	// Playable resolves (or reuses) a URL and waits until it is servable.
	Playable(ctx context.Context, id string, q stream.Quality) (stream.Stream, error)
	// Refresh drops a cached URL and resolves again.
	Refresh(ctx context.Context, id string, q stream.Quality) (stream.Stream, error)
}

// Prefetcher keeps upcoming tracks resolved (stream.Prefetcher). Want must
// not block.
type Prefetcher interface {
	Want(q stream.Quality, ids ...string)
}

// PlayMsg replaces the queue with Tracks and plays Tracks[Start]. Context
// is the collection they came from (playlist or album id, may be "").
// With shuffle on (already, or via Shuffle) the started track comes first
// and the rest follow in random order; Shuffle with Start < 0 starts at a
// random track (the [⤮ Shuffle] button).
type PlayMsg struct {
	Tracks  []ytmusic.Track
	Start   int
	Context string
	Shuffle bool
}

// EnqueueMsg adds tracks to the queue: after the current one (Next) or at
// the end.
type EnqueueMsg struct {
	Tracks []ytmusic.Track
	Next   bool
}

// PrefetchedMsg reports a finished background resolve (forwarded from the
// prefetcher by the app).
type PrefetchedMsg struct{ Result stream.Result }

// resolvedMsg is the answer to a play request: the URL for the current
// track, or why there is none.
type resolvedMsg struct {
	seq     uint64
	track   ytmusic.Track
	s       stream.Stream
	err     error
	startAt time.Duration
}

// appendReadyMsg is a servable URL for the gapless next track.
type appendReadyMsg struct {
	rev uint64
	id  string
	s   stream.Stream
	err error
}

// playback is the queue owner: it turns queue moves into resolve + load,
// keeps the next track preloaded in mpv for gapless playback and applies
// the re-resolve-once-then-skip rule on stream errors (specs 02, 03).
type playback struct {
	q        *queue.Queue
	seq      uint64 // latest play request; older resolvedMsgs are dropped
	loading  string // video ID being resolved for the current slot
	appended string // video ID preloaded in mpv as the next track
	retried  string // video ID already re-resolved once after an error
}

func (m *Model) quality() stream.Quality {
	return stream.ParseQuality(m.o.Config.Playback.AudioQuality)
}

// Queue exposes the queue (tests, Queue page).
func (m *Model) Queue() *queue.Queue { return m.pb.q }

// syncQueueFlags mirrors shuffle/repeat into the bar.
func (m *Model) syncQueueFlags() {
	m.bar.shuffle = m.pb.q.Shuffle()
	m.bar.repeat = m.pb.q.Repeat().String()
}

func nowPlaying(t ytmusic.Track) NowPlaying {
	n := NowPlaying{VideoID: t.VideoID, Title: t.Title, Artist: components.ArtistNames(t.Artists)}
	if t.Album != nil {
		n.Album = t.Album.Name
	}
	return n
}

// startCurrent resolves and loads the queue's current track at startAt.
func (m *Model) startCurrent(startAt time.Duration) tea.Cmd {
	t, ok := m.pb.q.Current()
	if !ok {
		return nil
	}
	if m.o.Player == nil {
		return m.toasts.push(Toast{Severity: SevError, Text: "Playback unavailable: mpv not found. Run `ytm doctor`."})
	}
	if m.o.Streams == nil {
		return m.toasts.push(Toast{Severity: SevError, Text: "Playback unavailable: no stream resolver"})
	}
	m.pb.seq++
	m.pb.loading = t.VideoID
	m.pb.retried = ""
	m.dropAppended()
	m.bar.now = nowPlaying(t)
	m.queueChanged()
	return m.resolveCmd(m.pb.seq, t, startAt, false)
}

func (m *Model) resolveCmd(seq uint64, t ytmusic.Track, startAt time.Duration, refresh bool) tea.Cmd {
	s, q, ctx := m.o.Streams, m.quality(), m.o.Context
	return func() tea.Msg {
		var st stream.Stream
		var err error
		if refresh {
			_, err = s.Refresh(ctx, t.VideoID, q)
		}
		if err == nil {
			st, err = s.Playable(ctx, t.VideoID, q) // cache hit after Refresh; waits for available_at
		}
		return resolvedMsg{seq: seq, track: t, s: st, err: err, startAt: startAt}
	}
}

func (m *Model) resolved(msg resolvedMsg) tea.Cmd {
	if msg.seq != m.pb.seq {
		return nil // superseded by a newer play request
	}
	m.pb.loading = ""
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			return nil
		}
		m.log.Warn("ui: resolve failed", "id", msg.track.VideoID, "err", msg.err)
		return tea.Batch(m.toasts.push(Toast{Severity: SevError, Text: streamErrText(msg.track, msg.err)}), m.skipAfterError())
	}
	item := player.Item{ID: msg.track.VideoID, URL: msg.s.URL, Title: msg.track.Title}
	if err := m.o.Player.Load(m.o.Context, item, msg.startAt); err != nil {
		m.log.Warn("ui: load failed", "err", err)
		return m.toasts.push(Toast{Severity: SevError, Text: "Playback command failed; see the log"})
	}
	return m.prefetchNext()
}

// streamErrText is the short toast for a failed resolve.
func streamErrText(t ytmusic.Track, err error) string {
	name := t.Title
	if name == "" {
		name = t.VideoID
	}
	switch {
	case errors.Is(err, stream.ErrYtDlpNotFound):
		return "yt-dlp not found. Run `ytm doctor`."
	case errors.Is(err, stream.ErrSignInRequired):
		return "“" + name + "” needs sign-in; set use_session_cookies. Skipping."
	case errors.Is(err, stream.ErrRateLimited):
		return "YouTube is rate limiting; try again later"
	}
	return "Can't play “" + name + "”; skipping"
}

// skipAfterError advances past a track that cannot play. It does not loop
// forever: a whole queue of failures stops at the end (repeat all aside,
// which the user can turn off).
func (m *Model) skipAfterError() tea.Cmd {
	if m.pb.q.Repeat() == queue.RepeatOne || m.pb.q.Len() <= 1 {
		return nil
	}
	if _, ok := m.pb.q.Next(); !ok {
		return nil
	}
	return m.startCurrent(0)
}

// prefetchNext asks for the next tracks to be resolved.
func (m *Model) prefetchNext() tea.Cmd {
	if m.o.Prefetch == nil {
		return nil
	}
	var ids []string
	if t, ok := m.pb.q.PeekNext(); ok {
		ids = append(ids, t.VideoID)
	}
	m.o.Prefetch.Want(m.quality(), ids...)
	return nil
}

// prefetched preloads the next track in mpv once its URL is ready.
func (m *Model) prefetched(r stream.Result) tea.Cmd {
	next, ok := m.pb.q.PeekNext()
	if r.Err != nil || !ok || next.VideoID != r.VideoID || !m.o.Config.Playback.Gapless ||
		m.o.Player == nil || m.o.Streams == nil || m.pb.appended == next.VideoID || m.bar.st.Item.IsZero() {
		return nil
	}
	rev, s, q, ctx, id := m.pb.q.Rev(), m.o.Streams, m.quality(), m.o.Context, next.VideoID
	return func() tea.Msg {
		st, err := s.Playable(ctx, id, q)
		return appendReadyMsg{rev: rev, id: id, s: st, err: err}
	}
}

func (m *Model) appendReady(msg appendReadyMsg) tea.Cmd {
	next, ok := m.pb.q.PeekNext()
	if msg.err != nil || !ok || next.VideoID != msg.id || m.pb.appended == msg.id {
		return nil
	}
	m.dropAppended()
	if err := m.o.Player.Append(player.Item{ID: next.VideoID, URL: msg.s.URL, Title: next.Title}); err != nil {
		m.log.Warn("ui: append failed", "err", err)
		return nil
	}
	m.pb.appended = next.VideoID
	return nil
}

// dropAppended removes a preloaded next track from mpv.
func (m *Model) dropAppended() {
	if m.pb.appended == "" || m.o.Player == nil {
		return
	}
	if err := m.o.Player.ClearNext(); err != nil {
		m.log.Warn("ui: clear next failed", "err", err)
	}
	m.pb.appended = ""
}

// queueEdited re-checks the preload after a queue change that may have
// changed what comes next (shuffle, reorder, removal, play next).
func (m *Model) queueEdited() tea.Cmd {
	m.syncQueueFlags()
	m.queueChanged()
	if next, ok := m.pb.q.PeekNext(); m.pb.appended != "" && (!ok || next.VideoID != m.pb.appended) {
		m.dropAppended()
	}
	if m.bar.st.Item.IsZero() {
		return nil
	}
	return m.prefetchNext()
}

// trackStarted follows mpv into a preloaded next track.
func (m *Model) trackStarted(it player.Item) tea.Cmd {
	if it.ID == "" || m.pb.appended == "" || it.ID != m.pb.appended {
		return nil
	}
	m.pb.appended = ""
	t, ok := m.pb.q.Next()
	if !ok || t.VideoID != it.ID {
		// The queue moved under us; load what it says is current.
		return m.startCurrent(0)
	}
	m.pb.retried = ""
	m.bar.now = nowPlaying(t)
	m.queueChanged()
	return m.prefetchNext()
}

// trackEnded advances on a natural end (unless mpv already moved into a
// preloaded track) and applies re-resolve-once-then-skip on errors.
func (m *Model) trackEnded(e player.TrackEnded) tea.Cmd {
	cur, ok := m.pb.q.Current()
	if !ok || cur.VideoID != e.Item.ID {
		// Not the queue's track (restored after a crash, or replaced).
		if e.Reason == player.EndError {
			m.log.Warn("ui: track failed", "id", e.Item.ID, "err", e.Err)
			return m.toasts.push(Toast{Severity: SevError, Text: "Couldn't play this track: " + e.Err})
		}
		return nil
	}
	if e.Reason == player.EndError {
		m.log.Warn("ui: track failed", "id", e.Item.ID, "err", e.Err)
		if m.pb.retried != cur.VideoID && m.o.Streams != nil {
			m.pb.retried = cur.VideoID
			m.pb.seq++
			m.pb.loading = cur.VideoID
			return m.resolveCmd(m.pb.seq, cur, m.bar.st.Position, true)
		}
		return tea.Batch(m.toasts.push(Toast{Severity: SevError, Text: "Couldn't play this track: " + e.Err}), m.skipAfterError())
	}
	if m.pb.appended != "" {
		return nil // TrackStarted for the preloaded track follows
	}
	if _, ok := m.pb.q.Next(); !ok {
		m.queueChanged()
		return nil
	}
	return m.startCurrent(0)
}

// queueAction handles the playback actions that move through the queue.
func (m *Model) queueAction(name string) tea.Cmd {
	q := m.pb.q
	switch name {
	case "next_track":
		if _, ok := q.Next(); !ok {
			return m.toasts.push(Toast{Severity: SevInfo, Text: "End of queue"})
		}
		return m.startCurrent(0)
	case "previous_track":
		if m.bar.st.Position > restartThreshold && !m.bar.st.Item.IsZero() && m.o.Player != nil {
			if err := m.o.Player.Seek(0, player.SeekAbsolute); err != nil {
				m.log.Warn("ui: seek failed", "err", err)
			}
			return nil
		}
		if _, ok := q.Previous(); !ok {
			return nil
		}
		return m.startCurrent(0)
	case "play_random":
		if _, ok := q.PlayRandom(); !ok {
			return m.toasts.push(Toast{Severity: SevInfo, Text: "The queue is empty"})
		}
		return m.startCurrent(0)
	case "cycle_repeat":
		r := q.CycleRepeat()
		cmd := m.queueEdited()
		return tea.Batch(cmd, m.toasts.push(Toast{Severity: SevInfo, Text: "Repeat: " + r.String()}))
	case "toggle_shuffle":
		q.ToggleShuffle()
		cmd := m.queueEdited()
		state := "off"
		if q.Shuffle() {
			state = "on"
		}
		return tea.Batch(cmd, m.toasts.push(Toast{Severity: SevInfo, Text: "Shuffle: " + state}))
	}
	return nil
}

// play handles PlayMsg.
func (m *Model) play(msg PlayMsg) tea.Cmd {
	if len(msg.Tracks) == 0 {
		return nil
	}
	q := m.pb.q
	shuffle := q.Shuffle() || msg.Shuffle
	q.Clear()
	q.SetShuffle(false)
	q.AddMany(msg.Tracks)
	q.SetContext(msg.Context)
	if msg.Start >= 0 || !shuffle {
		q.JumpTo(max(min(msg.Start, len(msg.Tracks)-1), 0))
	}
	if shuffle {
		q.SetShuffle(true) // keeps the started track first
		if q.CurrentIndex() < 0 {
			q.Next()
		}
	}
	m.syncQueueFlags()
	return m.startCurrent(0)
}

// enqueue handles EnqueueMsg.
func (m *Model) enqueue(msg EnqueueMsg) tea.Cmd {
	if len(msg.Tracks) == 0 {
		return nil
	}
	q := m.pb.q
	idle := q.CurrentIndex() < 0 && m.bar.st.Item.IsZero()
	if msg.Next {
		q.AddNext(msg.Tracks...)
	} else {
		q.AddMany(msg.Tracks)
	}
	what := plural(len(msg.Tracks), "track")
	text := "Added " + what + " to the queue"
	if msg.Next {
		text = "Playing " + what + " next"
	}
	cmds := []tea.Cmd{m.toasts.push(Toast{Severity: SevSuccess, Text: text})}
	if idle {
		if _, ok := q.Next(); ok {
			cmds = append(cmds, m.startCurrent(0))
		}
	} else {
		cmds = append(cmds, m.queueEdited())
	}
	return tea.Batch(cmds...)
}

func plural(n int, w string) string {
	if n == 1 {
		return "1 " + w
	}
	return components.FormatCount(n) + " " + w + "s"
}

// queueChanged tells the Queue page (and other track tables) to refresh
// their playing marker.
func (m *Model) queueChanged() {
	for _, p := range m.pages {
		if l, ok := p.(queueListener); ok {
			l.QueueChanged(m.pb.q, m.bar.now.VideoID)
		}
	}
}

// queueListener is a page that shows queue state.
type queueListener interface {
	QueueChanged(q *queue.Queue, playingID string)
}
