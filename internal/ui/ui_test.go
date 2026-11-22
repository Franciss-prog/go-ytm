package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/player"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/theme"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// scenario is one registered screen state. Every page and component the
// TUI can show must appear here: the transparent-render test and the
// goldens iterate this registry (spec 08). Add new pages when you add them.
type scenario struct {
	name  string
	setup func(m *Model)
}

var scenarios = func() []scenario {
	s := []scenario{}
	for _, id := range PageIDs {
		s = append(s, scenario{"page-" + string(id), func(m *Model) { drain(m, m.navigate(id)) }})
	}
	return append(s,
		scenario{"playing", func(m *Model) {
			m.Update(PlayerEventMsg{playerState(playing)})
			m.Update(NowPlayingMsg{VideoID: "abc", Title: "Instant Crush", Artist: "Daft Punk, Julian Casablancas", Album: "Random Access Memories"})
		}},
		scenario{"paused-line-progress", func(m *Model) {
			st := playing
			st.Status, st.Paused = player.Paused, true
			m.bar.style = "line"
			m.Update(PlayerEventMsg{playerState(st)})
		}},
		scenario{"error-and-toasts", func(m *Model) {
			st := playing
			st.Status, st.Err = player.Error, "loading failed"
			m.Update(PlayerEventMsg{playerState(st)})
			m.Update(Toast{Severity: SevError, Text: "Couldn't play this track: loading failed"})
			m.Update(Toast{Severity: SevWarn, Text: "Playback engine restarted"})
			m.Update(Toast{Severity: SevSuccess, Text: "Saved"})
		}},
		scenario{"lyrics-pending-chord", func(m *Model) {
			m.lyrics = true
			press(m, ch('g'))
		}},
		scenario{"help-filtering", func(m *Model) {
			m.navigate(PageHelp)
			press(m, ch('/'), ch('v'), ch('o'), ch('l'))
		}},
		scenario{"settings-popup", func(m *Model) {
			m.navigate(PageHelp)
			press(m, ch(','), tea.Key{Code: tea.KeyDown})
		}},
		scenario{"queue-filled", func(m *Model) {
			q := m.Queue()
			ts := scenarioTracks()
			q.AddMany(ts[:5])
			q.AddRadio(ts[5:], true)
			q.JumpTo(1)
			m.syncQueueFlags()
			m.queueChanged()
			m.navigate(PageQueue)
			press(m, ch('j'), ch('j'), ch('v'))
		}},
		scenario{"track-playing", func(m *Model) {
			q := m.Queue()
			q.AddMany(scenarioTracks())
			q.JumpTo(1)
			m.queueChanged()
			m.Update(PlayerEventMsg{playerState(playing)})
			m.Update(NowPlayingMsg{VideoID: "abc", Title: "Instant Crush", Artist: "Daft Punk, Julian Casablancas", Album: "Random Access Memories"})
			drain(m, press(m, ch('g'), ch('n')))
		}},
		scenario{"queue-filter-sort", func(m *Model) {
			m.Queue().AddMany(scenarioTracks())
			m.queueChanged()
			m.navigate(PageQueue)
			press(m, ch('s'), ch('d'), ch('/'), ch('d'), ch('a'))
		}},
		scenario{"context-playlist", func(m *Model) {
			drain(m, m.open(newPlaylistPage("PLnight", "Late night drives", columns(m.o.Config))))
			press(m, ch('j'))
		}},
		scenario{"context-playlist-locked", func(m *Model) {
			m.o.ShuffleLocks = &fakeLocks{on: map[string]bool{"PLnight": true}}
			drain(m, m.open(newPlaylistPage("PLnight", "Late night drives", columns(m.o.Config))))
		}},
		scenario{"context-album", func(m *Model) {
			drain(m, m.open(newAlbumPage("MPREb_ram", "Random Access Memories", columns(m.o.Config))))
		}},
		scenario{"context-artist", func(m *Model) {
			drain(m, m.open(newArtistPage("UCdp", "Daft Punk", columns(m.o.Config))))
			press(m, tea.Key{Code: tea.KeyTab})
		}},
		scenario{"popup-track-actions", func(m *Model) {
			drain(m, m.open(newPlaylistPage("PLnight", "Late night drives", columns(m.o.Config))))
			drain(m, press(m, ch('a')))
			press(m, ch('j'), ch('j'))
		}},
		scenario{"popup-playlist-picker", func(m *Model) {
			drain(m, m.open(newPlaylistPage("PLnight", "Late night drives", columns(m.o.Config))))
			drain(m, press(m, ch('v'), ch('v')))
			drain(m, press(m, ch('A')))
			press(m, tea.Key{Code: tea.KeyDown})
		}},
		scenario{"popup-create-playlist", func(m *Model) {
			drain(m, m.open(newPlaylistPage("PLnight", "Late night drives", columns(m.o.Config))))
			drain(m, press(m, ch('A')))
			drain(m, press(m, tea.Key{Code: tea.KeyEnter}))
			press(m, ch('R'), ch('o'), ch('a'), ch('d'), tea.Key{Code: tea.KeyTab})
		}},
		scenario{"popup-confirm-delete", func(m *Model) {
			drain(m, m.activate())
			drain(m, press(m, ch('j'), tea.Key{Code: tea.KeyDelete}))
		}},
		scenario{"browse-charts", func(m *Model) {
			drain(m, m.navigate(PageBrowse))
			drain(m, press(m, tea.Key{Code: tea.KeyTab}))
		}},
		scenario{"popup-country-picker", func(m *Model) {
			drain(m, m.navigate(PageBrowse))
			drain(m, press(m, tea.Key{Code: tea.KeyTab}))
			drain(m, press(m, ch('c')))
			press(m, ch('u'), ch('n'))
		}},
		scenario{"recent-local", func(m *Model) {
			ts := scenarioTracks()
			m.plays.add(ts[3])
			m.plays.add(ts[1]) // also in the account history: keeps its local position
			drain(m, m.navigate(PageRecent))
		}},
		scenario{"library-signed-out", func(m *Model) {
			m.o.API.(*fakeAPI).errs["LibraryAlbums"] = ytmusic.ErrUnauthenticated
			drain(m, press(m, ch('j'), ch('j'), ch('j'), tea.Key{Code: tea.KeyRight}))
		}},
		scenario{"library-columns", func(m *Model) {
			// root → Playlists → Late night drives → its tracks, cursor on row 2.
			lib := m.pages[PageLibrary].(*libraryPage)
			preview := func() { drain(m, m.deliver(pageMsg{to: lib, msg: libraryPreviewMsg{lib.seq}})) }
			drain(m, press(m, ch('j'), ch('j'), ch('j'), ch('j')))
			preview()
			drain(m, press(m, tea.Key{Code: tea.KeyRight}, ch('j')))
			preview()
			drain(m, press(m, tea.Key{Code: tea.KeyRight}, ch('j')))
		}},
		scenario{"search-suggestions", func(m *Model) {
			drain(m, m.navigate(PageSearch))
			for _, r := range "daft" {
				drain(m, press(m, ch(r)))
			}
			p := m.pages[PageSearch].(*searchPage)
			drain(m, func() tea.Msg { return pageMsg{to: p, msg: suggestTickMsg{p.seq}} })
			press(m, tea.Key{Code: tea.KeyDown})
		}},
		scenario{"search-results", func(m *Model) {
			drain(m, m.navigate(PageSearch))
			for _, r := range "daft punk" {
				drain(m, press(m, ch(r)))
			}
			drain(m, press(m, tea.Key{Code: tea.KeyEnter}))
		}},
		scenario{"search-playlists-all-mode", func(m *Model) {
			drain(m, m.navigate(PageSearch))
			drain(m, press(m, ch('d'), tea.Key{Code: tea.KeyEnter}))
			m.action(keys.Action{Name: "toggle_search_mode"})
			press(m, tea.Key{Code: tea.KeyTab}, tea.Key{Code: tea.KeyTab}, tea.Key{Code: tea.KeyTab})
		}},
		scenario{"sidebar-focused-wrap", func(m *Model) {
			m.sidebar = true
			drain(m, m.side.ensure(m.ctx()))
			m.side.list.SetWrap(true)
			drain(m, m.focusAction("focus_pane_left"))
			drain(m, m.action(keys.Action{Name: "move_down"}))
		}},
		scenario{"muted", func(m *Model) {
			st := playing
			st.Muted = true
			m.Update(PlayerEventMsg{playerState(st)})
		}},
	)
}()

// scenarioTracks is a small fixed album for page scenarios: long titles,
// several artists, CJK, a missing album, an unavailable row.
func scenarioTracks() []ytmusic.Track {
	ram := &ytmusic.AlbumRef{Name: "Random Access Memories", ID: "MPREb_ram"}
	dp := []ytmusic.Artist{{Name: "Daft Punk", ID: "UCdp"}}
	return []ytmusic.Track{
		{VideoID: "t1", Title: "Get Lucky", Artists: dp, Album: ram, Duration: 369 * time.Second, Available: true},
		{VideoID: "t2", Title: "Instant Crush", Artists: append(dp, ytmusic.Artist{Name: "Julian Casablancas"}), Album: ram, Duration: 337 * time.Second, Available: true},
		{VideoID: "t3", Title: "Lose Yourself to Dance (feat. Pharrell Williams)", Artists: dp, Album: ram, Duration: 353 * time.Second, Available: true},
		{VideoID: "t4", Title: "Giorgio by Moroder", Artists: dp, Album: ram, Duration: 544 * time.Second, Available: false},
		{VideoID: "t5", Title: "夜に駆ける", Artists: []ytmusic.Artist{{Name: "YOASOBI"}}, Duration: 261 * time.Second, Available: true},
		{VideoID: "t6", Title: "Around the World", Artists: dp, Album: &ytmusic.AlbumRef{Name: "Homework"}, Duration: 429 * time.Second, Available: true},
		{VideoID: "t7", Title: "One More Time", Artists: dp, Album: &ytmusic.AlbumRef{Name: "Discovery"}, Duration: 320 * time.Second, Available: true},
	}
}

var sizes = [][2]int{{80, 24}, {140, 40}}

func TestTransparentRender(t *testing.T) {
	for _, sc := range scenarios {
		for _, sz := range sizes {
			for _, sel := range []string{"bar", "reverse", "underline", "ansi-theme"} {
				m, _ := newTestModel(t, sz[0], sz[1], func(o *Options) {
					o.Config.Theme.Transparent = true
					o.Config.Theme.TransparentSelection = sel
					if sel == "ansi-theme" {
						o.Config.Theme.TransparentSelection = "bar"
						o.Theme = theme.ANSITheme(4)
					}
				})
				sc.setup(m)
				v := m.View()
				if bg := style.BackgroundSGRs(v.Content); len(bg) > 0 {
					t.Errorf("%s %dx%d sel=%s: background SGRs %q", sc.name, sz[0], sz[1], sel, bg)
				}
				if v.BackgroundColor != nil {
					t.Errorf("%s: transparent view sets the terminal background", sc.name)
				}
				if m.Styles().PaintsBackground() {
					t.Errorf("%s: styles claim to paint backgrounds", sc.name)
				}
			}
		}
	}
}

func TestOpaqueSetsRootBackground(t *testing.T) {
	m, _ := newTestModel(t, 80, 24)
	if m.View().BackgroundColor == nil || m.View().ForegroundColor == nil {
		t.Error("opaque mode must set the terminal default colors")
	}
}

func TestNoColor(t *testing.T) {
	for _, sc := range scenarios {
		m, _ := newTestModel(t, 80, 24, func(o *Options) { o.NoColor = true })
		sc.setup(m)
		v := m.View()
		if strings.Contains(v.Content, "38;") || len(style.BackgroundSGRs(v.Content)) > 0 {
			t.Errorf("%s: colors emitted under NO_COLOR", sc.name)
		}
		if v.BackgroundColor != nil || v.ForegroundColor != nil {
			t.Errorf("%s: terminal colors set under NO_COLOR", sc.name)
		}
	}
}

// TestGoldens snapshots every scenario, plain text plus the styled form
// in opaque and transparent mode. Update with: go test ./internal/ui -update
func TestGoldens(t *testing.T) {
	for _, sc := range scenarios {
		for _, sz := range sizes {
			t.Run(fmt.Sprintf("%s-%dx%d", sc.name, sz[0], sz[1]), func(t *testing.T) {
				m, _ := newTestModel(t, sz[0], sz[1])
				sc.setup(m)
				golden.RequireEqual(t, plain(m))
			})
		}
		for _, mode := range []string{"opaque", "transparent"} {
			t.Run(sc.name+"-styled-"+mode, func(t *testing.T) {
				m, _ := newTestModel(t, 80, 24, func(o *Options) { o.Config.Theme.Transparent = mode == "transparent" })
				sc.setup(m)
				golden.RequireEqual(t, m.View().Content)
			})
		}
	}
}

func TestEveryLineFitsWidth(t *testing.T) {
	for _, sc := range scenarios {
		for _, sz := range [][2]int{{80, 24}, {140, 40}, {59, 20}, {79, 11}, {99, 30}, {20, 5}} {
			m, _ := newTestModel(t, sz[0], sz[1])
			sc.setup(m)
			lines := strings.Split(plain(m), "\n")
			if len(lines) != sz[1] {
				t.Errorf("%s %v: %d lines", sc.name, sz, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != sz[0] {
					t.Errorf("%s %v: line %d is %d cells: %q", sc.name, sz, i, w, l)
					break
				}
			}
		}
	}
}

func TestLayoutBreakpoints(t *testing.T) {
	tests := []struct {
		w, h                 int
		lyrics               bool
		side, overlay, rule  bool
		mainW, mainH, mainY0 int
	}{
		// 140: sidebar 32 + main panel 70 + lyrics 38; rows 2 + 36 + 2.
		{w: 140, h: 40, lyrics: true, side: true, rule: true, mainW: 68, mainH: 34, mainY0: 3},
		{w: 99, h: 30, lyrics: true, side: true, overlay: true, rule: true, mainW: 65, mainH: 24, mainY0: 3},
		{w: 79, h: 30, rule: true, mainW: 77, mainH: 24, mainY0: 3},
		// Short: no rule, no hints → tab bar + panel + mini player.
		{w: 100, h: 11, side: true, mainW: 66, mainH: 7, mainY0: 2},
	}
	for _, tt := range tests {
		m, _ := newTestModel(t, tt.w, tt.h, withSidebar)
		m.lyrics = tt.lyrics
		l := m.layout()
		if (l.sideW > 0) != tt.side || l.lyricsOverlay != tt.overlay || l.rule != tt.rule ||
			l.mainW != tt.mainW || l.mainH != tt.mainH || l.mainY0 != tt.mainY0 {
			t.Errorf("%dx%d: %+v", tt.w, tt.h, l)
		}
	}
}

// withSidebar shows the playlist sidebar at start (off by default).
func withSidebar(o *Options) { o.Config.UI.ShowSidebar = true }

func TestNavigation(t *testing.T) {
	m, _ := newTestModel(t, 80, 24)
	press(m, ch('g'), ch('s'))
	if m.Current() != PageSearch {
		t.Fatalf("g s → %s", m.Current())
	}
	// Search focuses its input on arrival; escape hands keys back.
	press(m, tea.Key{Code: tea.KeyEscape}, ch('z'))
	if m.Current() != PageQueue {
		t.Fatalf("z → %s", m.Current())
	}
	press(m, tea.Key{Code: tea.KeyBackspace})
	if m.Current() != PageSearch {
		t.Errorf("back → %s", m.Current())
	}
	press(m, tea.Key{Code: tea.KeyBackspace, Mod: tea.ModShift})
	if m.Current() != PageQueue {
		t.Errorf("forward → %s", m.Current())
	}
	press(m, ch('g'), ch('l')) // new navigation clears forward
	if len(m.forward) != 0 {
		t.Errorf("forward not cleared: %v", m.forward)
	}
	for i := range 50 {
		m.navigate(PageIDs[i%len(PageIDs)])
	}
	if len(m.back) != MaxHistory {
		t.Errorf("history = %d, want %d", len(m.back), MaxHistory)
	}
}

func TestChordTimeoutAndPendingHeader(t *testing.T) {
	m, _ := newTestModel(t, 80, 24)
	cmd := press(m, ch('g'))
	if cmd == nil {
		t.Fatal("pending chord must arm a timeout")
	}
	if !strings.Contains(plain(m), "g-") {
		t.Error("pending chord not shown in header")
	}
	m.Update(chordTimeoutMsg{m.disp.Seq()}) // what the armed tea.Tick delivers
	if strings.Contains(plain(m), "g-") {
		t.Error("timeout did not clear the chord")
	}
	press(m, ch('l'))
	if m.Current() != PageLibrary {
		t.Errorf("after timeout l went to %s", m.Current())
	}
}

func TestPlaybackActions(t *testing.T) {
	m, fp := newTestModel(t, 80, 24)
	press(m, tea.Key{Code: tea.KeySpace, Text: " "})
	if len(fp.Calls()) != 0 {
		t.Errorf("toggled with nothing loaded: %v", fp.Calls())
	}
	m.Update(PlayerEventMsg{playerState(playing)})
	press(m, tea.Key{Code: tea.KeySpace, Text: " "})
	press(m, ch('3'), ch('+'))
	press(m, ch('2'), ch('>'))
	press(m, ch('<'))
	press(m, ch('^'))
	press(m, ch('_'))
	want := []string{"toggle_pause", "volume", "seek", "seek", "seek", "mute"}
	if got := fp.Calls(); !slices.Equal(got, want) {
		t.Fatalf("calls %v, want %v", got, want)
	}
	if fp.st.Volume != 95 {
		t.Errorf("volume = %d, want 95 (80 + 3×5)", fp.st.Volume)
	}
	step := time.Duration(m.o.Config.Playback.SeekStep) * time.Second
	wantSeeks := []time.Duration{2 * step, -step, 0}
	if !slices.Equal(fp.seeks, wantSeeks) || fp.modes[2] != player.SeekAbsolute {
		t.Errorf("seeks %v %v", fp.seeks, fp.modes)
	}
	press(m, ch('+'), ch('+'), ch('+'), ch('+'))
	if fp.st.Volume != 100 {
		t.Errorf("volume not clamped: %d", fp.st.Volume)
	}
}

func TestNoPlayerToasts(t *testing.T) {
	m, _ := newTestModel(t, 80, 24, func(o *Options) { o.Player = nil })
	press(m, tea.Key{Code: tea.KeySpace, Text: " "})
	if !strings.Contains(plain(m), "mpv not found") {
		t.Error("missing mpv not reported")
	}
}

func TestNotYetToast(t *testing.T) {
	m, _ := newTestModel(t, 100, 24)
	press(m, ch('l'))
	if !strings.Contains(plain(m), "Not available yet") {
		t.Error("no feedback for an action from a later milestone")
	}
}

func TestToastsExpireAndCap(t *testing.T) {
	m, _ := newTestModel(t, 100, 24)
	for i := range 5 {
		if _, c := m.Update(Toast{Text: fmt.Sprintf("toast %d", i)}); c == nil {
			t.Fatal("toast without expiry timer")
		}
	}
	out := plain(m)
	if strings.Contains(out, "toast 1") || !strings.Contains(out, "toast 4") || !strings.Contains(out, "toast 2") {
		t.Errorf("want the newest 3 toasts:\n%s", out)
	}
	m.Update(toastExpiredMsg{m.toasts.items[2].id}) // what the armed tea.Tick delivers
	if strings.Contains(plain(m), "toast 4") {
		t.Error("toast did not expire")
	}
}

func TestHelpFollowsKeymapAndFilters(t *testing.T) {
	km, _ := keys.Load([]byte("[keys]\nlike_toggle = [\"C-l\"]\n"), "k.toml")
	m, _ := newTestModel(t, 140, 40, func(o *Options) { o.Keymap = km })
	press(m, ch('?'))
	out := plain(m)
	if !strings.Contains(out, "C-l") || !strings.Contains(out, "~/.config/ytm-go/keymap.toml") {
		t.Errorf("help ignores the live keymap:\n%s", out)
	}
	press(m, ch('/'), ch('s'), ch('h'), ch('u'))
	out = plain(m)
	if !strings.Contains(out, "Toggle shuffle") || strings.Contains(out, "Volume up") {
		t.Errorf("filter not applied:\n%s", out)
	}
	press(m, ch('q')) // captured by the filter, must not quit
	if m.quiting {
		t.Fatal("q quit while typing in the filter")
	}
	press(m, tea.Key{Code: tea.KeyEscape})
	if strings.Contains(plain(m), "/shu") {
		t.Error("escape did not leave the filter")
	}
}

func TestMouse(t *testing.T) {
	m, fp := newTestModel(t, 100, 24)
	m.Update(PlayerEventMsg{playerState(playing)})
	_ = plain(m) // layout + geometry
	g := m.geom
	m.Update(tea.MouseClickMsg{X: g.progressX + g.progressW/2, Y: g.progressY, Button: tea.MouseLeft})
	if len(fp.seeks) != 1 || fp.modes[0] != player.SeekAbsolute {
		t.Fatalf("click on progress: %v %v", fp.seeks, fp.modes)
	}
	if d := fp.seeks[0] - playing.Duration/2; d < -5*time.Second || d > 5*time.Second {
		t.Errorf("seek to %v, want ~half of %v", fp.seeks[0], playing.Duration)
	}
	m.Update(tea.MouseWheelMsg{X: g.volumeX + 1, Y: g.volumeY, Button: tea.MouseWheelDown})
	if fp.st.Volume != 75 {
		t.Errorf("wheel volume = %d", fp.st.Volume)
	}
	// Tabs navigate; the mouse's back button walks the history.
	_ = plain(m)
	var queueX int
	for _, tb := range m.geom.tabs {
		if tb.page == PageQueue {
			queueX = tb.x
		}
	}
	m.Update(tea.MouseClickMsg{X: queueX, Y: 0, Button: tea.MouseLeft})
	if m.Current() != PageQueue {
		t.Fatalf("click on the queue tab → %s", m.Current())
	}
	m.Update(tea.MouseClickMsg{X: 50, Y: 10, Button: tea.MouseBackward})
	if m.Current() != PageLibrary {
		t.Errorf("mouse back → %s", m.Current())
	}
}

func TestPlayerEventToasts(t *testing.T) {
	for _, ev := range []player.Event{
		player.TrackEnded{Reason: player.EndError, Err: "loading failed"},
		player.EngineCrashed{GaveUp: true},
		player.Restarted{},
		player.EngineError{Err: fmt.Errorf("boom")},
	} {
		m, _ := newTestModel(t, 100, 24)
		if _, cmd := m.Update(PlayerEventMsg{ev}); cmd == nil || len(m.toasts.items) != 1 {
			t.Errorf("%T: no toast", ev)
		}
	}
}

// TestProgram runs the real Bubble Tea program loop via teatest.
func TestProgram(t *testing.T) {
	m, _ := newTestModel(t, 80, 24)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	tm.Send(tea.KeyPressMsg(ch('g')))
	tm.Send(tea.KeyPressMsg(ch('b')))
	tm.Send(tea.KeyPressMsg(ch('q')))
	fm, ok := tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second)).(*Model)
	if !ok || fm.Current() != PageBrowse || !fm.quiting {
		t.Errorf("final model: %v", fm)
	}
}

func BenchmarkViewHelp(b *testing.B) {
	m, _ := newTestModel(b, 140, 40)
	m.navigate(PageHelp)
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}

func BenchmarkPlaybackBarTick(b *testing.B) {
	m, _ := newTestModel(b, 140, 40)
	st := playing
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		st.Position = time.Duration(i%300) * time.Second
		m.Update(PlayerEventMsg{playerState(st)})
		_ = m.View()
	}
}

func BenchmarkKeypressRepaint(b *testing.B) {
	m, _ := newTestModel(b, 140, 40)
	m.navigate(PageHelp)
	b.ReportAllocs()
	for b.Loop() {
		m.Update(tea.KeyPressMsg(ch('j')))
		_ = m.View()
	}
}

// BenchmarkThemeApplyAndRepaint: ThemeChangedMsg (build styles) + full
// repaint at 200×60. Budget 50 ms (spec 08).
func BenchmarkThemeApplyAndRepaint(b *testing.B) {
	m, _ := newTestModel(b, 200, 60)
	m.Update(PlayerEventMsg{playerState(playing)})
	m.navigate(PageHelp)
	themes := []theme.Theme{}
	for _, n := range theme.BuiltinNames() {
		th, _ := theme.Builtin(n)
		themes = append(themes, th)
	}
	i := 0
	for b.Loop() {
		m.Update(ThemeChangedMsg{theme.Result{Theme: themes[i%len(themes)], Cause: theme.CauseFile}})
		_ = m.View()
		i++
	}
}

func TestSidebarHiddenByDefault(t *testing.T) {
	m, _ := newTestModel(t, 140, 40)
	if m.layout().sideW != 0 || strings.Contains(plain(m), "playlists │") {
		t.Fatal("sidebar shown with default config")
	}
	press(m, tea.Key{Code: 'e', Mod: tea.ModCtrl})
	if m.layout().sideW == 0 {
		t.Fatal("C-e did not show the sidebar")
	}
}

func TestHintsFollowKeymap(t *testing.T) {
	km, err := keys.Load([]byte("[keys]\nfilter = [\"F2\"]\nhelp = [\"F1\"]\n"), "k.toml")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := newTestModel(t, 140, 40, func(o *Options) { o.Keymap = km })
	drain(m, m.navigate(PageQueue))
	lines := strings.Split(plain(m), "\n")
	foot := lines[len(lines)-1]
	if !strings.Contains(foot, "f2 filter") || !strings.Contains(foot, "f1 help") || strings.Contains(foot, "/ filter") {
		t.Errorf("hints ignore the live keymap: %q", foot)
	}
	m.o.Config.UI.ShowHints = false
	if l := m.layout(); l.hints {
		t.Error("show_hints = false still shows hints")
	}
}

func TestTabFollowsNavigation(t *testing.T) {
	m, _ := newTestModel(t, 140, 40)
	for _, tt := range []struct {
		action string
		tab    PageID
	}{{"queue", PageQueue}, {"search", PageSearch}, {"liked_songs", PageLibrary}, {"help", PageLibrary}} {
		drain(m, m.action(keys.Action{Name: tt.action}))
		if m.tab != tt.tab {
			t.Errorf("%s: tab %s, want %s", tt.action, m.tab, tt.tab)
		}
	}
	m.goBack() // help → liked songs: still library
	m.goBack() // → search
	if m.tab != PageSearch {
		t.Errorf("back restores tab %s", m.tab)
	}
}

func TestTrackPage(t *testing.T) {
	m, fp := newTestModel(t, 140, 40)
	m.Queue().AddMany(scenarioTracks())
	m.Queue().JumpTo(2)
	m.queueChanged()
	m.Update(PlayerEventMsg{playerState(playing)})
	drain(m, press(m, ch('g'), ch('n')))
	if m.Current() != PageTrack || m.tab != PageTrack || m.layout().mini {
		t.Fatalf("g n: page %s tab %s mini %v", m.Current(), m.tab, m.layout().mini)
	}
	out := plain(m)
	// One centered card: no up next panel, status line with the queue position.
	if strings.Contains(out, "up next") || !strings.Contains(out, "now playing") {
		t.Fatalf("card layout:\n%s", out)
	}
	if !strings.Contains(out, "vol 80%") || !strings.Contains(out, "· 3/7") {
		t.Errorf("status line missing:\n%s", out)
	}
	tp := m.pages[PageTrack].(*trackPage)
	x, _, w := tp.Progress()
	if w == 0 || x < 20 {
		t.Fatalf("progress line not centered: x %d w %d", x, w)
	}
	// The progress line seeks on click.
	g := m.geom
	m.Update(tea.MouseClickMsg{X: g.progressX + g.progressW - 1, Y: g.progressY, Button: tea.MouseLeft})
	if len(fp.seeks) != 1 || fp.seeks[0] < playing.Duration*9/10 {
		t.Fatalf("seek %v", fp.seeks)
	}
	// Narrow and short: still one card, nothing overflows.
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 14})
	if out := plain(m); !strings.Contains(out, "now playing") {
		t.Fatalf("narrow layout:\n%s", out)
	}
}

func TestCardLayout(t *testing.T) {
	tests := []struct {
		w, h, spec     int
		cw, cols, rows int
	}{
		{136, 34, 6, 80, 36, 18}, // capped at cardMax; rows limited by height
		{100, 60, 6, 60, 60, 30}, // rows limited by width (cw/2)
		{40, 19, 6, 30, 0, 0},    // too short for 4 art rows
		{20, 40, 0, 18, 18, 9},   // never wider than the content
	}
	for _, tt := range tests {
		cw, cols, rows := cardLayout(tt.w, tt.h, tt.spec)
		if cw != tt.cw || cols != tt.cols || rows != tt.rows {
			t.Errorf("cardLayout(%d,%d,%d) = %d,%d,%d want %d,%d,%d", tt.w, tt.h, tt.spec, cw, cols, rows, tt.cw, tt.cols, tt.rows)
		}
	}
}
