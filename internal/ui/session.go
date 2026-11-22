package ui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/queue"
	"github.com/Franciss-Prog/go-ytm/internal/store"
)

// resumeMin is how far a track must have played for its position to be
// saved (ytm-player: a premature exit must not record position 0).
const resumeMin = time.Second

// restoreSession applies the last session (ytm-player
// _restore_session_state): volume, repeat, queue, shuffle and queue
// context always; the resume point only with resume_on_launch. The track
// is shown, not played: play_pause starts it at the saved position.
func (m *Model) restoreSession(s *store.Session) {
	if s == nil {
		return
	}
	m.bar.st.Volume = s.Volume
	if m.o.Player != nil {
		if err := m.o.Player.SetVolume(s.Volume); err != nil {
			m.log.Warn("ui: restore volume", "err", err)
		}
	}
	q := m.pb.q
	q.SetRepeat(queue.ParseRepeat(s.Repeat))
	if len(s.Tracks) > 0 {
		q.AddMany(s.Tracks)
		if s.Index >= 0 {
			q.JumpTo(s.Index)
		}
	}
	// After the tracks, so the shuffle order is built from them; the saved
	// order already is the playback order, so only the rest reshuffles.
	q.SetShuffle(s.Shuffle)
	q.SetContext(s.Context)
	m.syncQueueFlags()
	m.pb.savedResume = s.Resume

	cur, ok := q.Current()
	if !ok {
		return
	}
	m.bar.now = nowPlaying(cur)
	if r := s.Resume; r != nil && r.VideoID == cur.VideoID && m.o.Config.Playback.ResumeOnLaunch {
		m.pb.resumeAt = r.ResumeAt()
		m.toasts.push(Toast{Severity: SevInfo, Text: "Resume “" + cur.Title + "” at " + fmtDur(m.pb.resumeAt) + " with play/pause"})
	}
}

// startRestored starts the restored current track when nothing is loaded
// (play_pause after a restore). ok is false when there is nothing to start.
func (m *Model) startRestored() (tea.Cmd, bool) {
	if _, ok := m.pb.q.Current(); !ok {
		return nil, false
	}
	at := m.pb.resumeAt
	m.pb.resumeAt = 0
	return m.startCurrent(at), true
}

// Session is the state to save on exit (ytm-player _save_session_state).
// The live position wins past resumeMin; otherwise the resume point read
// at start is kept until a track loads this session.
func (m *Model) Session() store.Session {
	q := m.pb.q
	s := store.Session{
		Volume: m.bar.st.Volume, Repeat: q.Repeat().String(), Shuffle: q.Shuffle(),
		Tracks: q.Tracks(), Index: q.CurrentIndex(), Context: q.Context(),
		Resume: m.pb.savedResume,
	}
	if st := m.bar.st; !st.Item.IsZero() && st.Position > resumeMin {
		s.Resume = &store.Resume{VideoID: st.Item.ID, Position: st.Position.Seconds()}
	}
	return s
}

// ShuffleLocks are the per-playlist shuffle locks (*store.ShufflePrefs).
// Locked and Set only touch memory; Save writes the file (commands only).
type ShuffleLocks interface {
	Locked(id string) bool
	Set(id string, on bool)
	Save() error
}

// lockKey is the playlist ID a lock is stored under: browse IDs (VL…) and
// playlist IDs name the same playlist.
func lockKey(id string) string { return strings.TrimPrefix(id, "VL") }

// shuffleLockMsg toggles the shuffle lock of a playlist.
type shuffleLockMsg struct{ id string }

// locked reports whether playlist id has shuffle lock on.
func (m *Model) locked(id string) bool {
	return m.o.ShuffleLocks != nil && id != "" && m.o.ShuffleLocks.Locked(lockKey(id))
}

// toggleShuffleLock flips id's lock (ytm-player _toggle_shuffle_lock).
// Locking the playlist the queue plays turns shuffle on; unlocking leaves
// shuffle as it is.
func (m *Model) toggleShuffleLock(id string) tea.Cmd {
	l := m.o.ShuffleLocks
	if l == nil || id == "" {
		return nil
	}
	key := lockKey(id)
	on := !l.Locked(key)
	l.Set(key, on)
	cmds := []tea.Cmd{localWrite(m.o.Context, m.log, "shuffle lock", func(context.Context) error { return l.Save() })}
	text := "Shuffle lock off"
	if on {
		text = "Shuffle lock on: this playlist always plays shuffled"
		if q := m.pb.q; lockKey(q.Context()) == key && !q.Shuffle() {
			q.SetShuffle(true)
			cmds = append(cmds, m.queueEdited())
		}
	}
	return tea.Batch(append(cmds, m.toasts.push(Toast{Severity: SevInfo, Text: text}))...)
}
