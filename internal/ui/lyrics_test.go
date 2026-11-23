package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/lyrics"
	"github.com/Franciss-Prog/go-ytm/internal/player"
)

type fakeLyrics struct {
	mu    sync.Mutex
	l     *lyrics.Lyrics
	err   error
	calls []lyrics.Track
}

func (f *fakeLyrics) Find(_ context.Context, t lyrics.Track) (*lyrics.Lyrics, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, t)
	return f.l, f.err
}

func syncedLyrics(n int) *lyrics.Lyrics {
	l := &lyrics.Lyrics{Synced: true, Source: "YouTube Music · LyricFind"}
	for i := range n {
		l.Lines = append(l.Lines, lyrics.Line{Start: time.Duration(i*10) * time.Second, Text: fmt.Sprintf("line %02d", i)})
	}
	return l
}

// lyricsModel is a model with the pane open and track abc playing at pos.
func lyricsModel(t testing.TB, f *fakeLyrics, w, h int, pos time.Duration) (*Model, *fakePlayer) {
	t.Helper()
	m, fp := newTestModel(t, w, h, func(o *Options) { o.Lyrics = f })
	m.pb.q.AddMany(tracks("abc"))
	m.pb.q.JumpTo(0)
	st := playing
	st.Position = pos
	m.Update(PlayerEventMsg{playerState(st)})
	drain(m, press(m, ch('g'), ch('L')))
	if !m.lyrics {
		t.Fatal("g L did not open the lyrics pane")
	}
	return m, fp
}

func TestLyricsPaneFetchesAndFollows(t *testing.T) {
	f := &fakeLyrics{l: syncedLyrics(40)}
	m, _ := lyricsModel(t, f, 140, 40, 205*time.Second)
	if len(f.calls) != 1 || f.calls[0].VideoID != "abc" || f.calls[0].Title != "Song abc" || f.calls[0].Artist != "Artist" {
		t.Fatalf("Find calls = %+v", f.calls)
	}
	out := plain(m)
	if !strings.Contains(out, "line 20") || strings.Contains(out, "line 00") || !strings.Contains(out, "YouTube Music · LyricFind") {
		t.Fatalf("pane does not center line 20 or lacks the source:\n%s", out)
	}
	// Same track again: no second fetch.
	m.Update(PlayerEventMsg{playerState(playing)})
	if len(f.calls) != 1 {
		t.Errorf("refetched: %d calls", len(f.calls))
	}
}

func TestLyricsOnlyFetchWhenShown(t *testing.T) {
	f := &fakeLyrics{l: syncedLyrics(3)}
	m, _ := newTestModel(t, 140, 40, func(o *Options) { o.Lyrics = f })
	m.pb.q.AddMany(tracks("abc"))
	m.pb.q.JumpTo(0)
	m.Update(PlayerEventMsg{playerState(playing)})
	if len(f.calls) != 0 {
		t.Errorf("fetched with the pane hidden: %+v", f.calls)
	}
}

func TestLyricsStaleResultIgnored(t *testing.T) {
	f := &fakeLyrics{l: syncedLyrics(3)}
	m, _ := lyricsModel(t, f, 140, 40, 0)
	m.Update(lyricsLoadedMsg{videoID: "old", l: &lyrics.Lyrics{Lines: []lyrics.Line{{Text: "stale"}}}})
	if strings.Contains(plain(m), "stale") {
		t.Error("a result for another track replaced the lyrics")
	}
}

func TestLyricsStates(t *testing.T) {
	for _, tt := range []struct {
		name string
		f    *fakeLyrics
		want string
	}{
		{"none", &fakeLyrics{}, "No lyrics for this track."},
		{"failed", &fakeLyrics{err: errors.New("boom")}, "Couldn't load lyrics. r retries."},
		{"unsynced", &fakeLyrics{l: &lyrics.Lyrics{Source: "LRCLIB", Lines: []lyrics.Line{{Text: "plain words"}}}}, "LRCLIB · not synced"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := lyricsModel(t, tt.f, 140, 40, 0)
			if out := plain(m); !strings.Contains(out, tt.want) {
				t.Errorf("missing %q:\n%s", tt.want, out)
			}
		})
	}
	m, _ := newTestModel(t, 140, 40)
	m.lyrics = true
	if !strings.Contains(plain(m), "Lyrics unavailable.") {
		t.Error("no finder: want the unavailable state")
	}
}

func TestLyricsRetry(t *testing.T) {
	f := &fakeLyrics{err: errors.New("boom")}
	m, _ := lyricsModel(t, f, 140, 40, 0)
	drain(m, m.focusAction("focus_pane_right"))
	if m.focus != paneLyrics {
		t.Fatalf("focus = %v, want lyrics", m.focus)
	}
	f.mu.Lock()
	f.err, f.l = nil, syncedLyrics(2)
	f.mu.Unlock()
	drain(m, press(m, ch('r')))
	if len(f.calls) != 2 || !strings.Contains(plain(m), "line 01") {
		t.Errorf("retry: %d calls\n%s", len(f.calls), plain(m))
	}
}

func TestLyricsManualScrollPauses(t *testing.T) {
	f := &fakeLyrics{l: syncedLyrics(80)}
	m, _ := lyricsModel(t, f, 140, 40, 0)
	m.focus = paneLyrics
	plain(m) // the program draws between keys; scrolling clamps to that draw
	cmd := press(m, tea.Key{Code: 'f', Mod: tea.ModCtrl})
	if !m.lyr.manual || m.lyr.top != lyricsPage {
		t.Fatalf("after C-f: manual %v top %d", m.lyr.manual, m.lyr.top)
	}
	// Playback moves on: the pane stays where the user put it.
	st := playing
	st.Position = 600 * time.Second
	m.Update(PlayerEventMsg{playerState(st)})
	plain(m)
	if m.lyr.top != lyricsPage {
		t.Errorf("auto-scroll ran while paused: top %d", m.lyr.top)
	}
	// An older pause ending changes nothing; the latest one resumes.
	m.Update(lyricsResumeMsg{seq: m.lyr.seq - 1})
	if !m.lyr.manual {
		t.Error("stale resume ended the pause")
	}
	if cmd == nil {
		t.Fatal("no resume timer")
	}
	m.Update(lyricsResumeMsg{seq: m.lyr.seq})
	plain(m)
	if m.lyr.manual || m.lyr.top == lyricsPage {
		t.Errorf("did not resume: manual %v top %d", m.lyr.manual, m.lyr.top)
	}
}

func TestLyricsClickSeeks(t *testing.T) {
	f := &fakeLyrics{l: syncedLyrics(5)}
	m, fp := lyricsModel(t, f, 140, 40, 0)
	plain(m)
	l := m.layout()
	x0 := l.sideW + l.panelW + 1
	// Row 2 of the pane body holds line 02 (no scroll: 5 lines fit).
	m.Update(tea.MouseClickMsg{X: x0 + 2, Y: l.top + 1 + 2, Button: tea.MouseLeft})
	if m.focus != paneLyrics {
		t.Error("click did not focus the pane")
	}
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if len(fp.seeks) != 1 || fp.seeks[0] != 20*time.Second || fp.modes[0] != player.SeekAbsolute {
		t.Errorf("seeks = %v %v", fp.seeks, fp.modes)
	}
}

func TestToggleTransliteration(t *testing.T) {
	f := &fakeLyrics{l: &lyrics.Lyrics{Synced: true, Source: "LRCLIB", Lines: []lyrics.Line{{Text: "Ты меня"}, {Start: time.Second, Text: "ascii"}}}}
	m, _ := lyricsModel(t, f, 140, 40, 0)
	if strings.Contains(plain(m), "Ty menya") {
		t.Fatal("transliteration on by default")
	}
	drain(m, press(m, ch('T')))
	if out := plain(m); !strings.Contains(out, "Ty menya") || strings.Count(out, "ascii") != 1 {
		t.Errorf("transliteration on:\n%s", out)
	}
}

func TestLyricsRTLRightAligned(t *testing.T) {
	f := &fakeLyrics{l: &lyrics.Lyrics{Source: "LRCLIB", Lines: []lyrics.Line{{Text: "שלום"}}}}
	m, _ := lyricsModel(t, f, 140, 40, 0)
	found := false
	for _, ln := range strings.Split(plain(m), "\n") {
		if i := strings.Index(ln, "םולש"); i >= 0 {
			found = strings.HasSuffix(strings.TrimRight(ln, "│ "), "םולש")
		}
	}
	if !found {
		t.Errorf("RTL line not reordered and right-aligned:\n%s", plain(m))
	}
	m.o.Config.UI.BidiMode = "passthrough"
	if !strings.Contains(plain(m), "שלום") {
		t.Error("passthrough reordered the line")
	}
}

func TestLyricsFocusCycle(t *testing.T) {
	m, _ := lyricsModel(t, &fakeLyrics{}, 140, 40, 0)
	m.sidebar = true
	var got []pane
	for range 4 {
		drain(m, m.focusAction("focus_pane_cycle"))
		got = append(got, m.focus)
	}
	if fmt.Sprint(got) != fmt.Sprint([]pane{paneSidebar, paneLyrics, paneMain, paneSidebar}) {
		t.Errorf("cycle = %v", got)
	}
	m.focus = paneLyrics
	m.global(keyAction("lyrics"))
	if m.focus != paneMain {
		t.Error("hiding the pane kept its focus")
	}
}
