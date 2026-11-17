package ui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/ui/components"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// openMsg asks the root to show page p (recording history).
type openMsg struct{ page Page }

// loadState is where an async list is.
type loadState int

const (
	stateIdle loadState = iota
	stateLoading
	stateLoaded
	stateFailed
)

// libraryTabs are the Library tabs in order.
var libraryTabs = []string{"Playlists", "Albums", "Artists"}

// libraryPage lists the signed-in library: playlists, albums, artists
// (spec 05, "Library"). Enter opens the item as a Context page.
type libraryPage struct {
	tab   int
	lists [3]*components.List
	state [3]loadState
	err   [3]string

	playlists []ytmusic.PlaylistRef
	albums    []ytmusic.AlbumRef
	artists   []ytmusic.ArtistRef
}

func newLibraryPage() *libraryPage {
	p := &libraryPage{}
	for i, empty := range []string{"No playlists in your library.", "No albums in your library.", "No artists in your library."} {
		p.lists[i] = components.NewList(empty)
	}
	return p
}

func (p *libraryPage) ID() PageID      { return PageLibrary }
func (p *libraryPage) Capturing() bool { return p.lists[p.tab].Filtering() }

func (p *libraryPage) Info() string {
	it, _, ok := p.lists[p.tab].Selected()
	if !ok {
		return ""
	}
	if it.Right != "" {
		return it.Left + " · " + it.Right
	}
	return it.Left
}

func (p *libraryPage) Key(msg tea.KeyPressMsg, _ *Context) (Page, tea.Cmd) {
	p.lists[p.tab].Key(msg)
	return p, nil
}

// Activate implements activator: load the shown tab once.
func (p *libraryPage) Activate(ctx *Context) tea.Cmd {
	if p.state[p.tab] != stateIdle {
		return nil
	}
	return p.load(ctx)
}

func (p *libraryPage) load(ctx *Context) tea.Cmd {
	p.state[p.tab], p.err[p.tab] = stateLoading, ""
	switch p.tab {
	case 0:
		return ctx.Load(p, "playlists", func(c context.Context, api ytmusic.Client) (any, error) {
			return api.LibraryPlaylists(c, 0)
		})
	case 1:
		return ctx.Load(p, "albums", func(c context.Context, api ytmusic.Client) (any, error) {
			return api.LibraryAlbums(c, 0)
		})
	default:
		return ctx.Load(p, "artists", func(c context.Context, api ytmusic.Client) (any, error) {
			return api.LibraryArtists(c, 0)
		})
	}
}

// Receive implements receiver.
func (p *libraryPage) Receive(msg tea.Msg, _ *Context) tea.Cmd {
	l, ok := msg.(Loaded)
	if !ok {
		return nil
	}
	tab := map[string]int{"playlists": 0, "albums": 1, "artists": 2}[l.Key]
	if l.Err != nil {
		p.state[tab], p.err[tab] = stateFailed, loadErrorText(l.Err)
		return nil
	}
	p.state[tab] = stateLoaded
	var items []components.Item
	switch v := l.Value.(type) {
	case []ytmusic.PlaylistRef:
		p.playlists = v
		for _, r := range v {
			items = append(items, components.Item{Left: r.Title, Right: r.Count, Key: r.ID})
		}
	case []ytmusic.AlbumRef:
		p.albums = v
		for _, r := range v {
			items = append(items, components.Item{Left: r.Name, Right: albumDetail(r), Key: r.ID})
		}
	case []ytmusic.ArtistRef:
		p.artists = v
		for _, r := range v {
			items = append(items, components.Item{Left: r.Name, Right: r.Audience, Key: r.ID})
		}
	}
	p.lists[tab].SetItems(items)
	return nil
}

func albumDetail(r ytmusic.AlbumRef) string {
	var parts []string
	if a := components.ArtistNames(r.Artists); a != "" {
		parts = append(parts, a)
	}
	if r.Kind != "" && r.Kind != "Album" {
		parts = append(parts, r.Kind)
	}
	if r.Year != "" {
		parts = append(parts, r.Year)
	}
	return strings.Join(parts, " · ")
}

func (p *libraryPage) Action(a keys.Action, ctx *Context) (Page, tea.Cmd, bool) {
	switch a.Name {
	case "focus_next", "focus_prev":
		d := 1
		if a.Name == "focus_prev" {
			d = len(libraryTabs) - 1
		}
		p.tab = (p.tab + d) % len(libraryTabs)
		cmd := p.Activate(ctx)
		return p, cmd, true
	case "retry":
		if p.state[p.tab] == stateFailed {
			cmd := p.load(ctx)
			return p, cmd, true
		}
		return p, nil, false
	case "select":
		_, i, ok := p.lists[p.tab].Selected()
		if !ok {
			return p, nil, true
		}
		var next Page
		switch p.tab {
		case 0:
			r := p.playlists[i]
			next = newPlaylistPage(r.ID, r.Title, ctx.Columns)
		case 1:
			r := p.albums[i]
			next = newAlbumPage(r.ID, r.Name, ctx.Columns)
		default:
			r := p.artists[i]
			next = newArtistPage(r.ID, r.Name, ctx.Columns)
		}
		return p, msgCmd(openMsg{next}), true
	}
	return p, nil, p.lists[p.tab].Action(a)
}

// tabBar renders "Playlists  Albums  Artists" with the active tab marked.
func tabBar(names []string, active int, st *style.UI) string {
	var b strings.Builder
	for i, n := range names {
		b.WriteString(" ")
		if i == active {
			b.WriteString(st.HeaderActive.Render(n))
		} else {
			b.WriteString(st.Muted.Render(n))
		}
		b.WriteString(" ")
	}
	return b.String()
}

func (p *libraryPage) View(width, height int, st *style.UI) string {
	head := tabBar(libraryTabs, p.tab, st)
	body := max(height-2, 0)
	var main string
	switch p.state[p.tab] {
	case stateIdle, stateLoading:
		main = components.Block([]string{" " + st.Muted.Render("Loading…")}, width, body)
	case stateFailed:
		main = components.Block([]string{" " + st.ErrorText.Render(p.err[p.tab])}, width, body)
	default:
		main = p.lists[p.tab].View(width, body, st, true)
	}
	if height < 2 {
		return components.Block([]string{head}, width, height)
	}
	return components.Fit(head, width) + "\n" + components.Fit("", width) + "\n" + main
}
