package ui

import (
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/ui/components"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// pane is the column that owns the keyboard.
type pane int

const (
	paneMain pane = iota
	paneSidebar
)

// likedMusicID is the Liked Music playlist; the sidebar pins Liked Songs
// above the list instead (as the original).
const likedMusicID = "LM"

// sidebarNav are the pinned rows above the playlists, in order. They are
// reached with the mouse or their own keys (g y, g r, D); j/k move in the
// playlist list only, as in the original.
var sidebarNav = []struct {
	label  string
	action string
	icon   func(style.IconSet) string
}{
	{"Liked Songs", "liked_songs", func(i style.IconSet) string { return i.Heart }},
	{"Recently Played", "recently_played", func(i style.IconSet) string { return i.Recent }},
	{"Discovery Mix", "discovery_mix", func(i style.IconSet) string { return i.Mix }},
}

// playlistSidebar is the Playlists pane left of the main area (spec 05,
// "Sidebars"): pinned shortcuts, then the library playlists (without
// Liked Music) with filter, open and playlist actions.
type playlistSidebar struct {
	list      *components.List
	state     loadState
	err       string
	playlists []ytmusic.PlaylistRef
	seq       uint64

	// Geometry of the last draw, relative to the pane (mouse hits).
	navRows, listY int
}

// sidebarLoadedMsg answers load request seq.
type sidebarLoadedMsg struct {
	seq  uint64
	refs []ytmusic.PlaylistRef
	err  error
}

func newPlaylistSidebar(wrap bool) *playlistSidebar {
	l := components.NewList("No playlists yet.")
	l.SetWrap(wrap)
	return &playlistSidebar{list: l}
}

// ensure loads the playlists unless they are loaded or loading.
func (s *playlistSidebar) ensure(ctx *Context) tea.Cmd {
	if s.state != stateIdle {
		return nil
	}
	return s.load(ctx)
}

func (s *playlistSidebar) load(ctx *Context) tea.Cmd {
	s.seq++
	seq := s.seq
	s.state, s.err = stateLoading, ""
	api, root := ctx.API, ctx.Root
	return func() tea.Msg {
		if api == nil {
			return sidebarLoadedMsg{seq: seq, err: errNoAPI}
		}
		refs, err := api.LibraryPlaylists(root, 0)
		return sidebarLoadedMsg{seq: seq, refs: refs, err: err}
	}
}

// receive applies a load result; older requests are dropped. The cursor
// stays on the same playlist across reloads.
func (s *playlistSidebar) receive(msg sidebarLoadedMsg) {
	if msg.seq != s.seq {
		return
	}
	if msg.err != nil {
		s.state, s.err = stateFailed, loadErrorText(msg.err)
		return
	}
	s.state = stateLoaded
	s.playlists = slices.DeleteFunc(slices.Clone(msg.refs), func(r ytmusic.PlaylistRef) bool {
		return samePlaylist(r.ID, likedMusicID)
	})
	s.setItems()
}

func (s *playlistSidebar) setItems() {
	prev, _, had := s.list.Selected()
	items := make([]components.Item, len(s.playlists))
	for i, r := range s.playlists {
		items[i] = components.Item{Left: r.Title, Right: shortCount(r.Count), Key: r.ID}
	}
	s.list.SetItems(items)
	if had {
		s.list.SelectKey(prev.Key)
	}
}

// drop removes a deleted playlist.
func (s *playlistSidebar) drop(id string) {
	i := slices.IndexFunc(s.playlists, func(r ytmusic.PlaylistRef) bool { return samePlaylist(r.ID, id) })
	if i < 0 {
		return
	}
	cur := s.list.Cursor()
	s.playlists = slices.Delete(slices.Clone(s.playlists), i, i+1)
	s.setItems()
	s.list.SetCursor(min(cur, s.list.Shown()-1))
}

// selected is the playlist under the cursor.
func (s *playlistSidebar) selected() (ytmusic.PlaylistRef, bool) {
	_, i, ok := s.list.Selected()
	if !ok || s.state != stateLoaded {
		return ytmusic.PlaylistRef{}, false
	}
	return s.playlists[i], true
}

// info is the selection info line while the sidebar has focus.
func (s *playlistSidebar) info() string {
	pl, ok := s.selected()
	if !ok {
		return ""
	}
	if pl.Count != "" {
		return pl.Title + " · " + pl.Count
	}
	return pl.Title
}

// shortCount keeps the number of a displayed count ("87 songs" → "87"):
// the sidebar is narrow. Counts that are not numbers are left out.
func shortCount(c string) string {
	f, _, _ := strings.Cut(c, " ")
	if f == "" || !unicode.IsDigit([]rune(f)[0]) {
		return ""
	}
	return f
}

// view renders the pane in exactly w×h. focused draws the cursor and the
// title highlighted; active is the main area's page (its pinned row is
// highlighted).
func (s *playlistSidebar) view(w, h int, st *style.UI, focused bool, active PageID) string {
	var lines []string
	s.navRows = 0
	if h >= 8 {
		for _, n := range sidebarNav {
			text := n.icon(st.Opts.Icons) + " " + n.label
			if PageID(n.action) == active {
				lines = append(lines, " "+st.Section.Render(text))
			} else {
				lines = append(lines, " "+st.Muted.Render(text))
			}
		}
		s.navRows = len(lines)
		lines = append(lines, " "+st.Border.Render(strings.Repeat("─", max(w-2, 0))))
	}
	title := st.Muted.Render("Playlists")
	if focused {
		title = st.Section.Render("Playlists")
	}
	lines = append(lines, " "+title)
	s.listY = len(lines)

	footer := ""
	if s.state == stateLoaded && len(s.playlists) > 0 {
		if s.list.Filter() != "" {
			footer = components.FormatCount(s.list.Shown()) + "/" + components.FormatCount(len(s.playlists))
		} else {
			footer = plural(len(s.playlists), "playlist")
		}
	}
	bodyH := h - len(lines)
	if footer != "" && bodyH > 2 {
		bodyH--
	}
	bodyH = max(bodyH, 0)

	var body string
	switch s.state {
	case stateIdle, stateLoading:
		body = block([]string{" " + st.Muted.Render("Loading…")}, w, bodyH)
	case stateFailed:
		var msg []string
		for _, l := range strings.Split(ansi.Wrap(s.err, max(w-2, 1), ""), "\n") {
			msg = append(msg, " "+st.ErrorText.Render(l))
		}
		body = block(msg, w, bodyH)
	default:
		body = s.list.View(w, bodyH, st, focused)
	}
	out := block(lines, w, len(lines))
	if bodyH > 0 {
		out += "\n" + body
	}
	if footer != "" && len(lines)+bodyH < h {
		out += "\n" + fit(" "+st.Muted.Render(footer), w)
	}
	return out
}

// sideFocused reports that the visible sidebar owns the keyboard.
func (m *Model) sideFocused() bool {
	return m.focus == paneSidebar && m.layout().sideW > 0
}

// sidebarAction runs a on the focused sidebar. handled=false lets the
// page and then the global handler take it (play_pause, g l, …).
func (m *Model) sidebarAction(a keys.Action) (tea.Cmd, bool) {
	s := m.side
	switch a.Name {
	case "select":
		pl, ok := s.selected()
		if !ok {
			return nil, true
		}
		if p, ok := m.pages[m.current].(*contextPage); ok && p.kind == kindPlaylist && samePlaylist(p.id, pl.ID) {
			return nil, true
		}
		return m.open(newPlaylistPage(pl.ID, pl.Title, columns(m.o.Config))), true
	case "track_actions", "delete_item":
		pl, ok := s.selected()
		switch {
		case !ok:
			return nil, true
		case a.Name == "track_actions":
			m.popup = playlistActionsPopup(pl)
			return nil, true
		case !pl.Owned:
			return m.toasts.push(Toast{Severity: SevInfo, Text: "Only your own playlists can be deleted"}), true
		}
		m.popup = deletePlaylistConfirm(pl)
		return nil, true
	case "retry":
		if s.state != stateFailed {
			return nil, false
		}
		return s.load(m.ctx()), true
	}
	return nil, s.list.Action(a)
}

// focusAction handles the pane focus and sidebar toggle actions.
func (m *Model) focusAction(name string) tea.Cmd {
	switch name {
	case "toggle_sidebar":
		m.sidebar = !m.sidebar
		if !m.sidebar {
			m.focus = paneMain
			return nil
		}
		return m.side.ensure(m.ctx())
	case "focus_pane_left":
		// Reaching for the sidebar shows it (as the original).
		m.sidebar = true
		if m.layout().sideW == 0 {
			return m.toasts.push(Toast{Severity: SevInfo, Text: "Window too narrow for the playlist sidebar"})
		}
		m.focus = paneSidebar
		return m.side.ensure(m.ctx())
	case "focus_pane_right":
		m.focus = paneMain
	case "focus_pane_cycle":
		if m.focus == paneMain && m.sidebar && m.layout().sideW > 0 {
			m.focus = paneSidebar
			return m.side.ensure(m.ctx())
		}
		m.focus = paneMain
	}
	return nil
}
