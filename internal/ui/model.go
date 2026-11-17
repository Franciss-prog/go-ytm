package ui

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/Franciss-Prog/go-ytm/internal/art"
	"github.com/Franciss-Prog/go-ytm/internal/config"
	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/player"
	"github.com/Franciss-Prog/go-ytm/internal/queue"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/theme"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// MaxHistory bounds the back/forward stacks (as the original).
const MaxHistory = 20

// volumeStep is the volume change per volume_up/down press.
const volumeStep = 5

// Options configure the root model. Everything is a value or an interface
// the model only calls non-blocking methods on.
type Options struct {
	Config     config.Config
	Keymap     *keys.Keymap
	KeymapPath string
	Theme      theme.Theme
	NoColor    bool
	// Player may be nil (mpv missing): playback actions then toast.
	Player player.Player
	Log    *slog.Logger
	// Toasts are shown at start (config diagnostics, missing mpv, …).
	Toasts []Toast
	// Context ends timers when the program stops (default Background).
	Context context.Context

	// Themes re-resolves the theme on settings changes (nil: styles only).
	Themes ThemeService
	// ConfigPath and PatchConfig persist Settings and toggle_transparency
	// (config.Patch in production; nil = session only).
	ConfigPath  string
	PatchConfig func(path string, changes []config.Change) error
	// Profile is the color depth styles are built for.
	Profile colorprofile.Profile

	// Streams resolves stream URLs; nil disables playback from the queue.
	Streams Streams
	// Prefetch keeps the next track resolved (nil: none).
	Prefetch Prefetcher
	// Queue is the playback queue (nil: a new one).
	Queue *queue.Queue
	// API is the YouTube Music client (nil: library and search pages show
	// an error). SignedIn reports whether it carries a saved session.
	API      ytmusic.Client
	SignedIn bool
	// Art fetches cover art (nil: no art).
	Art art.Fetcher
}

// PlayerEventMsg carries a player event into Update (sent by the app's
// forwarder goroutine with tea.Program.Send).
type PlayerEventMsg struct{ Event player.Event }

// NowPlayingMsg sets display metadata for the current track.
type NowPlayingMsg NowPlaying

type chordTimeoutMsg struct{ seq uint64 }

// Model is the root Bubble Tea model.
type Model struct {
	o    Options
	log  *slog.Logger
	st   *style.UI
	disp *keys.Dispatcher

	width, height int

	pages   map[PageID]Page
	current PageID
	tab     PageID // highlighted tab (spec 09)
	back    []navEntry
	forward []navEntry

	sidebar, lyrics bool
	side            *playlistSidebar
	focus           pane

	settings *settings // open Settings popup, or nil
	popup    popup     // open popup (actions, picker, confirm, …), or nil
	themeErr string    // last theme problem toasted (toast once)

	bar     barState
	geom    frameGeom // interactive frame parts of the last View
	toasts  toasts
	quiting bool

	pb  playback
	art artState

	scrubSeq uint64 // bumps per wheel tick on the progress bar
}

// New builds the root model.
func New(o Options) *Model {
	if o.Keymap == nil {
		o.Keymap = keys.Default()
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.Context == nil {
		o.Context = context.Background()
	}
	if o.Queue == nil {
		o.Queue = queue.New(nil)
	}
	m := &Model{
		o: o, log: o.Log,
		disp:    keys.NewDispatcher(o.Keymap),
		pages:   newPages(),
		current: PageID(o.Config.General.StartupPage),
		sidebar: o.Config.UI.ShowSidebar,
		side:    newPlaylistSidebar(o.Config.UI.SidebarOverflow == "wrap"),
		bar:     barState{style: o.Config.UI.ProgressStyle, repeat: "off"},
		toasts:  toasts{ctx: o.Context},
		pb:      playback{q: o.Queue},
		art:     newArtState(o.Art, o.Config.UI.AlbumArtMode),
	}
	m.pages[PageHelp] = &help{keymap: o.Keymap, path: o.KeymapPath}
	m.pages[PageQueue] = newQueuePage(columns(o.Config), o.Queue)
	m.pages[PageLibrary] = newLibraryPage()
	m.pages[PageSearch] = newSearchPage(columns(o.Config))
	m.pages[PageTrack] = newTrackPage(&m.bar, &m.art, columns(o.Config), o.Queue)
	if _, ok := m.pages[m.current]; !ok {
		m.current = PageLibrary
	}
	m.tab = tabOf(m.current)
	if m.tab == "" {
		m.tab = PageLibrary
	}
	if o.Player != nil {
		m.bar.st = o.Player.State()
	}
	m.syncQueueFlags()
	m.restyle()
	for _, t := range o.Toasts {
		m.toasts.push(t)
	}
	return m
}

func (m *Model) restyle() {
	var v uint64 = 1
	if m.st != nil {
		v = m.st.Version + 1
	}
	m.st = style.NewUI(m.o.Theme, style.UIOptions{
		Transparent: m.o.Config.Theme.Transparent,
		Selection:   style.Selection(m.o.Config.Theme.TransparentSelection),
		PopupFill:   m.o.Config.Theme.PopupFill,
		NoColor:     m.o.NoColor,
		Icons:       style.ResolveIcons(m.o.Config.UI.Icons, true, true),
		Profile:     m.o.Profile,
	}, v)
}

// Styles exposes the current styles (tests).
func (m *Model) Styles() *style.UI { return m.st }

// Current returns the active page.
func (m *Model) Current() PageID { return m.current }

// Init implements tea.Model.
//
// In transparent mode it asks the terminal for its background (OSC 11):
// contrast is then checked against what is really behind the text. Only
// at start, while no root background has been set by us.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.toasts.armAll(), m.activate(), m.queryKitty()}
	if m.sidebar {
		cmds = append(cmds, m.side.ensure(m.ctx()))
	}
	if m.o.Config.Theme.Transparent && m.o.Themes != nil && !m.o.NoColor {
		cmds = append(cmds, tea.RequestBackgroundColor)
	}
	return tea.Batch(cmds...)
}

// SettingsOpen reports whether the Settings popup is open (tests).
func (m *Model) SettingsOpen() bool { return m.settings != nil }

func (m *Model) ctx() *Context {
	return &Context{
		Keymap: m.o.Keymap, KeymapPath: m.o.KeymapPath, Height: m.layout().mainH,
		API: m.o.API, SignedIn: m.o.SignedIn, Root: m.o.Context, Columns: columns(m.o.Config), Styles: m.st, Log: m.log,
	}
}

// Update implements tea.Model. After every message the cover art follows
// the current track (syncArt).
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.update(msg)
	if a := m.syncArt(); a != nil {
		cmd = tea.Batch(cmd, a)
	}
	return m, cmd
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case artLoadedMsg:
		m.artLoaded(msg)
		return m, nil
	case uv.KittyGraphicsEvent:
		m.kittyReply(msg)
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyPressMsg:
		cmd := m.key(msg)
		return m, cmd
	case tea.MouseClickMsg:
		cmd := m.click(msg.Mouse())
		return m, cmd
	case scrubCommitMsg:
		m.commitScrub(msg.seq)
		return m, nil
	case tea.MouseWheelMsg:
		cmd := m.wheel(msg.Mouse())
		return m, cmd
	case chordTimeoutMsg:
		m.disp.Timeout(msg.seq)
		return m, nil
	case toastExpiredMsg:
		m.toasts.expire(msg.id)
		return m, nil
	case Toast:
		return m, m.toasts.push(msg)
	case PlayerEventMsg:
		cmd := m.playerEvent(msg.Event)
		return m, cmd
	case NowPlayingMsg:
		m.bar.now = NowPlaying(msg)
		return m, nil
	case ThemeChangedMsg:
		cmd := m.themeChanged(msg.Result)
		return m, cmd
	case tea.BackgroundColorMsg:
		if m.o.Themes != nil && msg.Color != nil {
			m.o.Themes.SetTerminalBg(msg.Color)
		}
		return m, nil
	case PlayMsg:
		cmd := m.play(msg)
		return m, cmd
	case EnqueueMsg:
		cmd := m.enqueue(msg)
		return m, cmd
	case PrefetchedMsg:
		cmd := m.prefetched(msg.Result)
		return m, cmd
	case resolvedMsg:
		cmd := m.resolved(msg)
		return m, cmd
	case appendReadyMsg:
		cmd := m.appendReady(msg)
		return m, cmd
	case sidebarLoadedMsg:
		m.side.receive(msg)
		return m, nil
	case pageMsg:
		cmd := m.deliver(msg)
		return m, cmd
	case popupMsg:
		cmd := m.deliverPopup(msg)
		return m, cmd
	case openPopupMsg:
		m.popup = msg.p
		return m, nil
	case openPickerMsg:
		cmd := m.openPicker(msg)
		return m, cmd
	case actionMsg:
		cmd := m.action(msg.a)
		return m, cmd
	case editDoneMsg:
		cmd := m.editDone(msg)
		return m, cmd
	case openMsg:
		cmd := m.open(msg.page)
		return m, cmd
	case jumpMsg:
		if _, ok := m.pb.q.JumpTo(msg.pos); !ok {
			return m, nil
		}
		cmd := m.startCurrent(0)
		return m, cmd
	case queueEditedMsg:
		cmd := m.queueEdited()
		return m, cmd
	case settingsSavedMsg:
		if msg.err != nil {
			m.log.Error("ui: saving settings failed", "err", msg.err)
			return m, m.toasts.push(Toast{Severity: SevError, Text: "Couldn't save settings; see the log"})
		}
		return m, m.toasts.push(Toast{Severity: SevSuccess, Text: "Settings saved"})
	}
	return m, nil
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	if m.settings != nil {
		return m.settingsKey(msg)
	}
	if m.popup != nil {
		return m.popupKey(msg)
	}
	if m.sideFocused() && m.side.list.Filtering() {
		m.side.list.Key(msg)
		return nil
	}
	page := m.pages[m.current]
	if page.Capturing() {
		p, cmd := page.Key(msg, m.ctx())
		m.pages[m.current] = p
		return cmd
	}
	r := m.disp.Feed(keys.FromTea(msg.Key()))
	switch {
	case r.Pending:
		seq := m.disp.Seq()
		d := time.Duration(m.o.Config.Keys.ChordTimeoutMS) * time.Millisecond
		if d <= 0 {
			d = time.Second
		}
		return after(m.o.Context, d, chordTimeoutMsg{seq})
	case r.Action != nil:
		return m.action(*r.Action)
	}
	return nil
}

func (m *Model) action(a keys.Action) tea.Cmd {
	if m.sideFocused() {
		if cmd, ok := m.sidebarAction(a); ok {
			return cmd
		}
	}
	p, cmd, handled := m.pages[m.current].Action(a, m.ctx())
	m.pages[m.current] = p
	if handled {
		return cmd
	}
	return tea.Batch(cmd, m.global(a))
}

// notYet lists actions whose features arrive in later milestones.
var notYet = map[string]string{
	"like_toggle":   "likes",
	"discovery_mix": "radio", "current_context": "pages", "jump_to_current": "the queue",
	"command_palette": "the command palette", "theme_picker": "the theme picker",
	"toggle_transliteration": "lyrics",
}

func (m *Model) global(a keys.Action) tea.Cmd {
	if id, ok := pageActions[a.Name]; ok {
		return m.navigate(id)
	}
	switch a.Name {
	case "quit":
		m.quiting = true
		if rel := m.releaseArt(); rel != nil {
			return tea.Sequence(rel, tea.Quit)
		}
		return tea.Quit
	case "toggle_album_art":
		m.art.on = !m.art.on
	case "go_back":
		m.goBack()
	case "go_forward":
		m.goForward()
	case "toggle_sidebar", "focus_pane_left", "focus_pane_right", "focus_pane_cycle":
		return m.focusAction(a.Name)
	case "lyrics":
		m.lyrics = !m.lyrics
	case "toggle_transparency":
		return m.toggleTransparency()
	case "settings":
		m.openSettings()
	case "next_track", "previous_track", "play_random", "cycle_repeat", "toggle_shuffle":
		return m.queueAction(a.Name)
	case "play_pause", "volume_up", "volume_down", "mute", "seek_forward", "seek_backward", "seek_start":
		return m.playback(a)
	default:
		if what, ok := notYet[a.Name]; ok {
			return m.toasts.push(Toast{Severity: SevInfo, Text: "Not available yet: " + what + " arrives in a later version"})
		}
	}
	return nil
}

func (m *Model) playback(a keys.Action) tea.Cmd {
	p := m.o.Player
	if p == nil {
		return m.toasts.push(Toast{Severity: SevError, Text: "Playback unavailable: mpv not found. Run `ytm doctor`."})
	}
	var err error
	switch a.Name {
	case "play_pause":
		if m.bar.st.Item.IsZero() {
			return m.toasts.push(Toast{Severity: SevInfo, Text: "Nothing to play yet"})
		}
		err = p.TogglePause()
	case "volume_up":
		err = p.SetVolume(min(m.bar.st.Volume+volumeStep*a.N(), 100))
		m.bar.st.Volume = min(m.bar.st.Volume+volumeStep*a.N(), 100)
	case "volume_down":
		err = p.SetVolume(max(m.bar.st.Volume-volumeStep*a.N(), 0))
		m.bar.st.Volume = max(m.bar.st.Volume-volumeStep*a.N(), 0)
	case "mute":
		err = p.ToggleMute()
	case "seek_forward", "seek_backward":
		d := time.Duration(m.o.Config.Playback.SeekStep*a.N()) * time.Second
		if a.Name == "seek_backward" {
			d = -d
		}
		err = p.Seek(d, player.SeekRelative)
	case "seek_start":
		err = p.Seek(0, player.SeekAbsolute)
	}
	if err != nil {
		m.log.Warn("ui: player command failed", "action", a.Name, "err", err)
		return m.toasts.push(Toast{Severity: SevError, Text: "Playback command failed; see the log"})
	}
	return nil
}

func (m *Model) playerEvent(ev player.Event) tea.Cmd {
	switch e := ev.(type) {
	case player.StateChanged:
		m.bar.st = e.State
	case player.TrackStarted:
		m.bar.scrubbing = false
		return m.trackStarted(e.Item)
	case player.TrackEnded:
		return m.trackEnded(e)
	case player.EngineCrashed:
		if e.GaveUp {
			return m.toasts.push(Toast{Severity: SevError, Text: "Playback engine keeps crashing. Run `ytm doctor`."})
		}
	case player.Restarted:
		return m.toasts.push(Toast{Severity: SevWarn, Text: "Playback engine restarted"})
	case player.EngineError:
		m.log.Error("ui: mpv failed to start", "err", e.Err)
		return m.toasts.push(Toast{Severity: SevError, Text: "Couldn't start mpv. Run `ytm doctor`."})
	}
	return nil
}

// navEntry is one history step: the page id and the page instance shown
// (Context pages differ per album/playlist/artist; restoring the instance
// restores cursor, scroll, filter and sort).
type navEntry struct {
	id   PageID
	page Page
	tab  PageID
}

// activator is a page that loads data when it is shown.
type activator interface {
	Activate(ctx *Context) tea.Cmd
}

// navigate opens id, recording history.
func (m *Model) navigate(id PageID) tea.Cmd {
	if id == m.current {
		return nil
	}
	m.pushHistory()
	m.current = id
	m.setTab(id)
	return m.activate()
}

// open shows a new page instance (a Context page), recording history.
func (m *Model) open(p Page) tea.Cmd {
	m.pushHistory()
	m.current = p.ID()
	m.pages[p.ID()] = p
	m.setTab(p.ID())
	return m.activate()
}

func (m *Model) pushHistory() {
	m.back = pushBounded(m.back, navEntry{m.current, m.pages[m.current], m.tab})
	m.forward = nil
}

// setTab highlights id's tab; pages without one keep the current tab.
func (m *Model) setTab(id PageID) {
	if t := tabOf(id); t != "" {
		m.tab = t
	}
}

func (m *Model) activate() tea.Cmd {
	if a, ok := m.pages[m.current].(activator); ok {
		return a.Activate(m.ctx())
	}
	return nil
}

func (m *Model) goBack() {
	if len(m.back) == 0 {
		return
	}
	m.forward = pushBounded(m.forward, navEntry{m.current, m.pages[m.current], m.tab})
	e := m.back[len(m.back)-1]
	m.back = m.back[:len(m.back)-1]
	m.current, m.pages[e.id], m.tab = e.id, e.page, e.tab
}

func (m *Model) goForward() {
	if len(m.forward) == 0 {
		return
	}
	m.back = pushBounded(m.back, navEntry{m.current, m.pages[m.current], m.tab})
	e := m.forward[len(m.forward)-1]
	m.forward = m.forward[:len(m.forward)-1]
	m.current, m.pages[e.id], m.tab = e.id, e.page, e.tab
}

func pushBounded(s []navEntry, e navEntry) []navEntry {
	s = append(s, e)
	if len(s) > MaxHistory {
		s = s[len(s)-MaxHistory:]
	}
	return s
}

// titler is a page whose header title depends on its content.
type titler interface{ Title() string }

func (m *Model) pageTitle() string {
	if t, ok := m.pages[m.current].(titler); ok {
		return t.Title()
	}
	return m.current.Title()
}

// pageMsg carries an async result back to the page instance that asked
// for it. Results for pages no longer in the history are dropped.
type pageMsg struct {
	to  Page
	msg tea.Msg
}

// receiver is a page that takes async results.
type receiver interface {
	Receive(msg tea.Msg, ctx *Context) tea.Cmd
}

func (m *Model) deliver(pm pageMsg) tea.Cmd {
	if !m.hasPage(pm.to) {
		return nil
	}
	if r, ok := pm.to.(receiver); ok {
		return r.Receive(pm.msg, m.ctx())
	}
	return nil
}

func (m *Model) hasPage(p Page) bool {
	for _, q := range m.pages {
		if q == p {
			return true
		}
	}
	for _, s := range [][]navEntry{m.back, m.forward} {
		for _, e := range s {
			if e.page == p {
				return true
			}
		}
	}
	return false
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	if m.o.Config.UI.Mouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	v.BackgroundColor = m.st.RootBg()
	v.ForegroundColor = m.st.RootFg()
	v.WindowTitle = "ytm-go"
	if t, _ := m.bar.title(); t != "" && !m.bar.st.Item.IsZero() {
		v.WindowTitle = t + " — ytm-go"
	}
	return v
}

func (m *Model) render() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	l := m.layout()
	st := m.st
	m.geom.buttons = m.geom.buttons[:0]
	m.geom.progressW = 0
	var out []string
	if l.tabs {
		out = append(out, m.tabBar())
	}
	if l.rule {
		out = append(out, m.tabRule())
	}

	page := m.pages[m.current]
	info := ""
	if m.o.Config.UI.ShowSelectionInfo {
		info = page.Info()
	}
	var main []string
	if sp, ok := page.(splitter); ok && l.border {
		main = splitPanels(st, sp, l.panelW, l.panelH)
	} else {
		main = panel(st, panelSpec{
			title: m.panelTitle(), counter: countText(page), info: info, focused: !m.sideFocused(),
		}, page.View(l.mainW, l.mainH, st), l.panelW, l.panelH, l.border)
	}
	if pr, ok := page.(progressor); ok {
		if x, y, w := pr.Progress(); w > 0 {
			m.geom.progressX, m.geom.progressY, m.geom.progressW = l.mainX0+x, l.mainY0+y, w
		}
	}
	var side, lyr []string
	in := l.inset()
	if l.sideW > 0 {
		sideInfo := ""
		if m.o.Config.UI.ShowSelectionInfo && m.sideFocused() {
			sideInfo = m.side.info()
		}
		side = panel(st, panelSpec{title: sideTitle, info: sideInfo, focused: m.sideFocused()},
			m.sidebarView(l.sideW-2*in, l.panelH-2*in), l.sideW, l.panelH, l.border)
	}
	if l.lyricsW > 0 {
		lyr = panel(st, panelSpec{title: "lyrics"}, m.lyricsView(l.lyricsW-2*in, l.panelH-2*in), l.lyricsW, l.panelH, l.border)
	}
	rows := make([]string, l.panelH)
	for i := range l.panelH {
		var b strings.Builder
		if side != nil {
			b.WriteString(side[i])
		}
		line := main[i]
		if lyr != nil && l.lyricsOverlay {
			line = overlayRight(line, lyr[i], l.panelW, l.lyricsW)
		}
		b.WriteString(line)
		if lyr != nil && !l.lyricsOverlay {
			b.WriteString(lyr[i])
		}
		rows[i] = b.String()
	}
	if m.settings != nil {
		m.overlaySettings(rows, m.width, st)
	} else if m.popup != nil {
		m.overlayPopup(rows, m.width, st)
	}
	m.toasts.overlay(rows, m.width, st)
	out = append(out, rows...)

	if l.mini {
		out = append(out, m.miniPlayer(len(out)))
	}
	if l.hints {
		out = append(out, m.hintLine())
	}
	return strings.Join(out, "\n")
}

func (m *Model) sidebarView(w, h int) string {
	return m.side.view(w, h, m.st, m.sideFocused(), m.current)
}

func (m *Model) lyricsView(w, h int) string {
	st := m.st
	return block([]string{" " + st.Section.Render("Lyrics"), "", " " + st.Muted.Render("No lyrics yet.")}, w, h)
}

// modal reports that a popup (or Settings) owns input.
func (m *Model) modal() bool { return m.settings != nil || m.popup != nil }

// String describes the model for debugging.
func (m *Model) String() string {
	return fmt.Sprintf("ui.Model{page=%s size=%dx%d}", m.current, m.width, m.height)
}
