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

// PageContext is the album / playlist / artist page. It is not in the
// header list: it is opened from other pages, one instance per item.
const PageContext PageID = "context"

// trackAction applies the actions every track list shares: play from the
// cursor, play next, add to queue. source is the queue context id.
func trackAction(t *components.TrackTable, a keys.Action, source string) (tea.Cmd, bool) {
	switch a.Name {
	case "select":
		rows, cur := t.Ordered()
		if len(rows) == 0 {
			return nil, true
		}
		return msgCmd(PlayMsg{Tracks: rows, Start: cur, Context: source}), true
	case "play_next", "add_to_queue":
		ts := t.TargetTracks()
		if len(ts) == 0 {
			return nil, true
		}
		t.ClearMarks()
		return msgCmd(EnqueueMsg{Tracks: ts, Next: a.Name == "play_next"}), true
	}
	return nil, false
}

type contextKind int

const (
	kindPlaylist contextKind = iota
	kindAlbum
	kindArtist
)

var kindNames = [...]string{"Playlist", "Album", "Artist"}

// contextPage shows one playlist, album or artist (spec 05, "Context
// page"): a header, then a TrackTable; artists add a releases list
// (albums and singles, full discography) focused with tab.
type contextPage struct {
	kind  contextKind
	id    string
	name  string
	table *components.TrackTable
	state loadState
	err   string
	head  []string // header detail lines (plain text)
	queue string   // queue context id for PlayMsg

	// playlists: continuation paging
	cont  string
	total int

	// artists
	releases    *components.List
	releaseRefs []ytmusic.AlbumRef
	focus       int // 0 table, 1 releases
}

func newContextPage(kind contextKind, id, name string, cols components.Columns) *contextPage {
	empty := map[contextKind]string{
		kindPlaylist: "This playlist is empty.", kindAlbum: "This album has no tracks.", kindArtist: "No songs.",
	}[kind]
	p := &contextPage{kind: kind, id: id, name: name, table: components.NewTrackTable(cols, empty), queue: id}
	if kind == kindArtist {
		p.releases = components.NewList("No releases.")
	}
	return p
}

func newPlaylistPage(id, name string, cols components.Columns) *contextPage {
	return newContextPage(kindPlaylist, id, name, cols)
}

func newAlbumPage(id, name string, cols components.Columns) *contextPage {
	return newContextPage(kindAlbum, id, name, cols)
}

func newArtistPage(id, name string, cols components.Columns) *contextPage {
	return newContextPage(kindArtist, id, name, cols)
}

func (p *contextPage) ID() PageID { return PageContext }

// Title implements titler.
func (p *contextPage) Title() string { return kindNames[p.kind] }

func (p *contextPage) Capturing() bool {
	if p.focus == 1 {
		return p.releases.Filtering()
	}
	return p.table.Filtering()
}

func (p *contextPage) Info() string {
	if p.focus == 1 {
		if it, _, ok := p.releases.Selected(); ok {
			return it.Left + " · " + it.Right
		}
		return ""
	}
	return p.table.Info()
}

func (p *contextPage) Key(msg tea.KeyPressMsg, _ *Context) (Page, tea.Cmd) {
	if p.focus == 1 {
		p.releases.Key(msg)
	} else {
		p.table.Key(msg)
	}
	return p, nil
}

// Activate implements activator.
func (p *contextPage) Activate(ctx *Context) tea.Cmd {
	if p.state != stateIdle {
		return nil
	}
	return p.load(ctx)
}

func (p *contextPage) load(ctx *Context) tea.Cmd {
	p.state, p.err = stateLoading, ""
	id := p.id
	switch p.kind {
	case kindPlaylist:
		return ctx.Load(p, "playlist", func(c context.Context, api ytmusic.Client) (any, error) { return api.Playlist(c, id) })
	case kindAlbum:
		return ctx.Load(p, "album", func(c context.Context, api ytmusic.Client) (any, error) { return api.Album(c, id) })
	default:
		return ctx.Load(p, "artist", func(c context.Context, api ytmusic.Client) (any, error) { return api.Artist(c, id) })
	}
}

func (p *contextPage) more(ctx *Context) tea.Cmd {
	token := p.cont
	return ctx.Load(p, "more", func(c context.Context, api ytmusic.Client) (any, error) {
		ts, next, err := api.PlaylistMore(c, token)
		return morePage{ts, next}, err
	})
}

type morePage struct {
	tracks []ytmusic.Track
	next   string
}

// Receive implements receiver.
func (p *contextPage) Receive(msg tea.Msg, ctx *Context) tea.Cmd {
	l, ok := msg.(Loaded)
	if !ok {
		return nil
	}
	if l.Err != nil {
		if l.Key == "more" || l.Key == "discography" {
			// Keep what loaded; say the rest failed in the footer/log.
			p.table.SetLoading(0, 0)
			return ctx.Toast(Toast{Severity: SevWarn, Text: "Couldn't load the rest of this list"})
		}
		p.state, p.err = stateFailed, loadErrorText(l.Err)
		return nil
	}
	switch v := l.Value.(type) {
	case ytmusic.Playlist:
		p.state, p.name = stateLoaded, v.Title
		p.head = playlistHead(v)
		p.table.SetRows(v.Tracks)
		p.cont, p.total = v.Continuation, v.TrackCount
		if p.cont != "" {
			p.table.SetLoading(len(v.Tracks), p.total)
			return p.more(ctx)
		}
	case morePage:
		p.table.AppendRows(v.tracks)
		p.cont = v.next
		if p.cont == "" || len(v.tracks) == 0 {
			p.table.SetLoading(0, 0)
			return nil
		}
		p.table.SetLoading(p.table.Len(), p.total)
		return p.more(ctx)
	case ytmusic.Album:
		p.state, p.name = stateLoaded, v.Name
		p.head = albumHead(v)
		if v.PlaylistID != "" {
			p.queue = v.PlaylistID
		}
		p.table.SetRows(v.Tracks)
	case ytmusic.ArtistPage:
		p.state, p.name = stateLoaded, v.Name
		p.head = artistHead(v)
		p.table.SetRows(v.TopSongs.Items)
		p.setReleases(append(append([]ytmusic.AlbumRef(nil), v.Albums.Items...), v.Singles.Items...))
		if v.Albums.BrowseID != "" {
			sh := v.Albums
			singles := v.Singles.Items
			return ctx.Load(p, "discography", func(c context.Context, api ytmusic.Client) (any, error) {
				all, err := api.ArtistAlbums(c, sh.BrowseID, sh.Params, 0)
				return discography(append(all, singles...)), err
			})
		}
	case discography:
		p.setReleases(v)
	}
	return nil
}

// discography is the full album list of an artist (plus singles).
type discography []ytmusic.AlbumRef

func (p *contextPage) setReleases(refs []ytmusic.AlbumRef) {
	p.releaseRefs = refs
	items := make([]components.Item, len(refs))
	for i, r := range refs {
		right := r.Kind
		if r.Year != "" {
			right = strings.TrimPrefix(right+" · "+r.Year, " · ")
		}
		items[i] = components.Item{Left: r.Name, Right: right, Key: r.ID}
	}
	p.releases.SetItems(items)
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, sep)
}

func playlistHead(v ytmusic.Playlist) []string {
	count := ""
	if v.TrackCount > 0 {
		count = plural(v.TrackCount, "song")
	}
	return []string{joinNonEmpty(" · ", v.Author, count, v.Duration, strings.ToLower(v.Privacy))}
}

func albumHead(v ytmusic.Album) []string {
	count := ""
	if v.TrackCount > 0 {
		count = plural(v.TrackCount, "song")
	}
	return []string{joinNonEmpty(" · ", v.Kind, components.ArtistNames(v.Artists), v.Year, count, v.Duration)}
}

func artistHead(v ytmusic.ArtistPage) []string {
	return []string{joinNonEmpty(" · ", v.Audience, v.Subscribers)}
}

func (p *contextPage) Action(a keys.Action, ctx *Context) (Page, tea.Cmd, bool) {
	switch a.Name {
	case "retry":
		if p.state == stateFailed {
			cmd := p.load(ctx)
			return p, cmd, true
		}
		return p, nil, false
	case "focus_next", "focus_prev":
		if p.kind == kindArtist {
			p.focus = 1 - p.focus
			return p, nil, true
		}
		return p, nil, false
	}
	if p.focus == 1 {
		if a.Name == "select" {
			if _, i, ok := p.releases.Selected(); ok {
				r := p.releaseRefs[i]
				return p, msgCmd(openMsg{newAlbumPage(r.ID, r.Name, ctx.Columns)}), true
			}
			return p, nil, true
		}
		return p, nil, p.releases.Action(a)
	}
	if cmd, ok := trackAction(p.table, a, p.queue); ok {
		return p, cmd, true
	}
	return p, nil, p.table.Action(a)
}

func (p *contextPage) View(width, height int, st *style.UI) string {
	lines := []string{" " + st.Title.Render(p.name)}
	for _, h := range p.head {
		if h != "" {
			lines = append(lines, " "+st.Muted.Render(h))
		}
	}
	lines = append(lines, "")
	headH := len(lines)
	body := max(height-headH, 0)
	var main string
	switch p.state {
	case stateIdle, stateLoading:
		main = components.Block([]string{" " + st.Muted.Render("Loading…")}, width, body)
	case stateFailed:
		main = components.Block([]string{" " + st.ErrorText.Render(p.err)}, width, body)
	default:
		main = p.body(width, body, st)
	}
	return components.Block(lines, width, min(headH, height)) + "\n" + main
}

func (p *contextPage) body(width, height int, st *style.UI) string {
	if p.kind != kindArtist {
		return p.table.View(width, height, st)
	}
	// Artist: songs on top, releases below (about a third), tab switches.
	relH := max(min(height/3, p.releases.Len()+1), 2)
	songsH := max(height-relH, 0)
	label := func(s string, on bool) string {
		if on {
			return " " + st.Section.Render(s)
		}
		return " " + st.Muted.Render(s)
	}
	return label("Top songs", p.focus == 0) + "\n" +
		p.table.View(width, max(songsH-1, 0), st) + "\n" +
		label("Albums & singles (tab)", p.focus == 1) + "\n" +
		p.releases.View(width, max(relH-1, 0), st, p.focus == 1)
}
