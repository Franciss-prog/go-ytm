package ui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/cache"
	"github.com/Franciss-Prog/go-ytm/internal/queue"
	"github.com/Franciss-Prog/go-ytm/internal/store"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// fakeLocal is an in-memory Local.
type fakeLocal struct {
	mu       sync.Mutex
	plays    []ytmusic.Track // stored, newest first
	logged   []string        // LogPlay ids
	searches []string        // stored, newest first
	logSrch  []string        // LogSearch "query|filter"
}

func (f *fakeLocal) LogPlay(_ context.Context, t ytmusic.Track, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logged = append(f.logged, t.VideoID)
	return nil
}

func (f *fakeLocal) RecentPlays(context.Context, int) ([]ytmusic.Track, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.plays), nil
}

func (f *fakeLocal) LogSearch(_ context.Context, q, filter string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logSrch = append(f.logSrch, q+"|"+filter)
	return nil
}

func (f *fakeLocal) RecentSearches(context.Context, int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.searches), nil
}

func TestPlaysSeededFromStoreAndLogged(t *testing.T) {
	l := &fakeLocal{plays: tracks("old1", "a")}
	h := newPB(t, func(o *Options) { o.Local = l })
	if got := playIDs(h.m.plays.tracks); got != "old1 a" {
		t.Fatalf("seeded plays %q", got)
	}
	h.send(PlayMsg{Tracks: tracks("a", "b")})
	h.started("a")
	h.pl.st.Position = 10 * time.Second // past history_min_listen_seconds (5)
	h.send(PlayerEventMsg{playerState(h.pl.st)})
	if got := playIDs(h.m.plays.tracks); got != "a old1" {
		t.Errorf("after play: %q (want a first, deduplicated)", got)
	}
	if !slices.Equal(l.logged, []string{"a"}) {
		t.Errorf("LogPlay calls %v", l.logged)
	}
}

func playIDs(ts []ytmusic.Track) string {
	ids := make([]string, len(ts))
	for i, t := range ts {
		ids[i] = t.VideoID
	}
	return strings.Join(ids, " ")
}

func TestSearchHistory(t *testing.T) {
	l := &fakeLocal{searches: []string{"muse", "daft punk"}}
	m, _ := newTestModel(t, 100, 30, func(o *Options) { o.Local = l })
	drain(m, m.navigate(PageSearch))
	p := m.pages[PageSearch].(*searchPage)
	if !slices.Equal(p.suggest, []string{"muse", "daft punk"}) {
		t.Fatalf("empty input shows %v, want the recent searches", p.suggest)
	}
	// Down + enter runs the highlighted recent search and logs it.
	drain(m, press(m, tea.Key{Code: tea.KeyDown}, tea.Key{Code: tea.KeyEnter}))
	if p.query != "muse" || !slices.Equal(l.logSrch, []string{"muse|music"}) {
		t.Errorf("query %q, logged %v", p.query, l.logSrch)
	}
	// Back to an empty input: the history leads with the last search.
	drain(m, press(m, tea.Key{Code: 's', Text: "s"}))
	p.editing, p.input = true, nil
	drain(m, p.edited(m.ctx()))
	if len(p.suggest) == 0 || p.suggest[0] != "muse" {
		t.Errorf("history after search %v", p.suggest)
	}
}

// fakeDownloads answers Download from a table.
type fakeDownloads struct {
	mu  sync.Mutex
	ids []string
	res map[string]cache.Result
	err map[string]error
}

func (f *fakeDownloads) Download(_ context.Context, id string) (cache.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids = append(f.ids, id)
	return f.res[id], f.err[id]
}

func TestDownloadToasts(t *testing.T) {
	d := &fakeDownloads{
		res: map[string]cache.Result{"b": {Already: true}, "c": {Evicted: true}},
		err: map[string]error{"d": errors.New("boom"), "e": cache.ErrBusy},
	}
	m, _ := newTestModel(t, 100, 30, func(o *Options) { o.Downloads = d })
	drain(m, m.download(DownloadMsg{Tracks: tracks("a", "b", "c", "d", "e")}))
	if !slices.Equal(d.ids, []string{"a", "b", "c", "d", "e"}) {
		t.Fatalf("downloads %v, want all in order", d.ids)
	}
	for id, want := range map[string]string{
		"a": "Downloaded “Song a”", "b": "Already downloaded “Song b”", "c": "“Song c” is bigger than the cache",
		"d": "Download failed: “Song d”", "e": "Already downloading “Song e”",
	} {
		tr := tracks(id)[0]
		m.downloadToast(downloadedMsg{track: tr, res: d.res[id], err: d.err[id]})
		if got := m.toasts.items[len(m.toasts.items)-1].Text; !strings.HasPrefix(got, want) {
			t.Errorf("%s: toast %q, want prefix %q", id, got, want)
		}
	}

	m, _ = newTestModel(t, 100, 30)
	drain(m, m.download(DownloadMsg{Tracks: tracks("a")}))
	if len(m.toasts.items) != 1 || m.toasts.items[0].Severity != SevWarn {
		t.Errorf("without a cache: %+v", m.toasts.items)
	}
}

func TestSessionRestoreAndResume(t *testing.T) {
	s := &store.Session{
		Volume: 30, Repeat: "one", Tracks: tracks("a", "b", "c"), Index: 1, Context: "PL1",
		Resume: &store.Resume{VideoID: "b", Position: 42},
	}
	h := newPB(t, func(o *Options) { o.Session = s })
	q := h.m.Queue()
	if queueIDs(q) != "a,b,c" || h.current() != "b" || q.Repeat() != queue.RepeatOne || q.Context() != "PL1" {
		t.Fatalf("queue %s cur %s repeat %v ctx %q", queueIDs(q), h.current(), q.Repeat(), q.Context())
	}
	if h.pl.st.Volume != 30 || h.m.bar.now.VideoID != "b" {
		t.Errorf("player volume %d, bar shows %q", h.pl.st.Volume, h.m.bar.now.VideoID)
	}
	// Nothing played yet: the resume point is saved back unchanged.
	if got := h.m.Session(); got.Resume == nil || got.Resume.Position != 42 || got.Index != 1 || got.Volume != 30 {
		t.Errorf("unplayed session %+v", got)
	}
	// play_pause with nothing loaded starts b at the saved position.
	h.act("play_pause")
	if h.lastItem() != "url:b" || h.pl.at[len(h.pl.at)-1] != 42*time.Second {
		t.Fatalf("resume load %s at %v", h.lastItem(), h.pl.at)
	}
	h.started("b")
	h.pl.st.Position = 50 * time.Second
	h.send(PlayerEventMsg{playerState(h.pl.st)})
	if got := h.m.Session(); got.Resume == nil || got.Resume.VideoID != "b" || got.Resume.Position != 50 {
		t.Errorf("live resume %+v", got.Resume)
	}
}

func TestSessionResumeOff(t *testing.T) {
	s := &store.Session{Tracks: tracks("a", "b"), Index: 1, Resume: &store.Resume{VideoID: "b", Position: 42}}
	h := newPB(t, func(o *Options) { o.Session = s; o.Config.Playback.ResumeOnLaunch = false })
	h.act("play_pause")
	if h.lastItem() != "url:b" || h.pl.at[len(h.pl.at)-1] != 0 {
		t.Errorf("load %s at %v, want b from the start", h.lastItem(), h.pl.at)
	}
	if got := h.m.Session(); got.Resume != nil {
		t.Errorf("a track loaded: old resume point kept %+v", got.Resume)
	}
}

// fakeLocks is an in-memory ShuffleLocks.
type fakeLocks struct {
	mu    sync.Mutex
	on    map[string]bool
	saves int
}

func (f *fakeLocks) Locked(id string) bool { f.mu.Lock(); defer f.mu.Unlock(); return f.on[id] }
func (f *fakeLocks) Set(id string, on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.on[id] = on
}

func (f *fakeLocks) Save() error { f.mu.Lock(); defer f.mu.Unlock(); f.saves++; return nil }

func TestShuffleLock(t *testing.T) {
	locks := &fakeLocks{on: map[string]bool{"PL1": true}}
	h := newPB(t, func(o *Options) { o.ShuffleLocks = locks })
	// A locked playlist (browse id form) plays shuffled even with shuffle off.
	h.send(PlayMsg{Tracks: tracks("a", "b", "c"), Context: "VLPL1"})
	if !h.m.Queue().Shuffle() {
		t.Fatal("locked playlist played in order")
	}
	h.act("toggle_shuffle")
	if !h.m.Queue().Shuffle() {
		t.Error("toggle_shuffle turned a locked playlist's shuffle off")
	}
	// Unlock (toast, file save), then shuffle toggles again.
	h.send(shuffleLockMsg{"VLPL1"})
	if locks.Locked("PL1") || locks.saves != 1 {
		t.Fatalf("unlock: locked=%v saves=%d", locks.Locked("PL1"), locks.saves)
	}
	h.act("toggle_shuffle")
	if h.m.Queue().Shuffle() {
		t.Error("unlocked playlist: shuffle did not toggle off")
	}
	// Locking the playing playlist turns shuffle on at once.
	h.send(shuffleLockMsg{"PL1"})
	if !h.m.Queue().Shuffle() {
		t.Error("locking the playing playlist left shuffle off")
	}
	// An unlocked playlist plays in order.
	h.send(shuffleLockMsg{"PL1"})
	h.act("toggle_shuffle")
	h.send(PlayMsg{Tracks: tracks("x", "y"), Context: "PL2"})
	if h.m.Queue().Shuffle() {
		t.Error("unlocked playlist played shuffled")
	}
}

func TestPlaylistPageLockLine(t *testing.T) {
	locks := &fakeLocks{on: map[string]bool{}}
	m, _ := newTestModel(t, 100, 30, func(o *Options) { o.ShuffleLocks = locks })
	p := newPlaylistPage("VLPL1", "Mix", columns(m.o.Config))
	drain(m, m.open(p))
	if !strings.Contains(p.View(100, 20, m.st), "Shuffle lock: off") || p.lockY < 0 {
		t.Fatalf("no lock line (lockY %d)", p.lockY)
	}
	_, cmd := p.Click(mouseEv{y: p.lockY}, m.ctx())
	drain(m, cmd)
	if !locks.Locked("PL1") || !strings.Contains(p.View(100, 20, m.st), "Shuffle lock: ON") {
		t.Errorf("click did not lock: %v", locks.on)
	}
	album := newAlbumPage("MPREb_1", "Album", columns(m.o.Config))
	drain(m, m.open(album))
	if strings.Contains(album.View(100, 20, m.st), "Shuffle lock") {
		t.Error("album page shows a shuffle lock")
	}
}
