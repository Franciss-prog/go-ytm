package ui

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/ui/components"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// suggestDebounce is how long typing must pause before suggestions are
// requested (spec 05).
const suggestDebounce = 150 * time.Millisecond

// searchSongsLimit is how many songs the Songs panel asks for.
const searchSongsLimit = 50

// Search panels, in tab order. Videos shows only in "all" mode.
const (
	panelSongs = iota
	panelAlbums
	panelArtists
	panelPlaylists
	panelVideos
)

var panelNames = []string{"Songs", "Albums", "Artists", "Playlists", "Videos"}

// suggestTickMsg fires suggestDebounce after a keystroke.
type suggestTickMsg struct{ seq uint64 }

// searchPage is the Search page (spec 05): a query input with debounced
// predictive suggestions, then results grouped into panels (tab cycles).
// Each request carries the input's seq; replies for older input are
// dropped, so typing fast never shows stale suggestions or results.
type searchPage struct {
	input    []rune
	editing  bool
	seq      uint64 // bumps on every edit and search
	suggest  []string
	sugSel   int // -1: none
	all      bool
	query    string // last searched
	state    loadState
	err      string
	top      *ytmusic.SearchResult
	panel    int
	songs    *components.TrackTable
	videos   *components.TrackTable
	lists    map[int]*components.List
	albums   []ytmusic.AlbumRef
	artists  []ytmusic.ArtistRef
	playlist []ytmusic.PlaylistRef
}

func newSearchPage(cols components.Columns) *searchPage {
	return &searchPage{
		editing: true, sugSel: -1,
		songs:  components.NewTrackTable(cols, "No songs."),
		videos: components.NewTrackTable(cols, "No videos."),
		lists: map[int]*components.List{
			panelAlbums: components.NewList("No albums."), panelArtists: components.NewList("No artists."),
			panelPlaylists: components.NewList("No playlists."),
		},
	}
}

func (p *searchPage) ID() PageID { return PageSearch }

// Activate implements activator: arriving on the page focuses the input.
func (p *searchPage) Activate(*Context) tea.Cmd {
	p.editing = true
	return nil
}

func (p *searchPage) Capturing() bool {
	if p.editing {
		return true
	}
	switch p.panel {
	case panelSongs:
		return p.songs.Filtering()
	case panelVideos:
		return p.videos.Filtering()
	}
	return p.lists[p.panel].Filtering()
}

func (p *searchPage) Info() string {
	if p.editing {
		return ""
	}
	switch p.panel {
	case panelSongs:
		return p.songs.Info()
	case panelVideos:
		return p.videos.Info()
	}
	if it, _, ok := p.lists[p.panel].Selected(); ok {
		return joinNonEmpty(" · ", it.Left, it.Right)
	}
	return ""
}

func (p *searchPage) panels() int {
	if p.all {
		return len(panelNames)
	}
	return len(panelNames) - 1
}

// Key edits the query while editing; otherwise it goes to the focused
// panel's filter.
func (p *searchPage) Key(msg tea.KeyPressMsg, ctx *Context) (Page, tea.Cmd) {
	if !p.editing {
		switch p.panel {
		case panelSongs:
			p.songs.Key(msg)
		case panelVideos:
			p.videos.Key(msg)
		default:
			p.lists[p.panel].Key(msg)
		}
		return p, nil
	}
	k := msg.Key()
	switch k.Code {
	case tea.KeyEscape:
		if len(p.suggest) > 0 {
			p.suggest, p.sugSel = nil, -1
			return p, nil
		}
		p.editing = false // keys go back to the keymap (g l, q, …)
		return p, nil
	case tea.KeyEnter:
		q := strings.TrimSpace(string(p.input))
		if p.sugSel >= 0 && p.sugSel < len(p.suggest) {
			q = p.suggest[p.sugSel]
			p.input = []rune(q)
		}
		if q == "" {
			return p, nil
		}
		cmd := p.search(q, ctx)
		return p, cmd
	case tea.KeyDown:
		if len(p.suggest) > 0 {
			p.sugSel = min(p.sugSel+1, len(p.suggest)-1)
		}
		return p, nil
	case tea.KeyUp:
		p.sugSel = max(p.sugSel-1, -1)
		return p, nil
	case tea.KeyTab:
		if p.sugSel >= 0 && p.sugSel < len(p.suggest) {
			p.input = []rune(p.suggest[p.sugSel])
			p.sugSel = -1
			cmd := p.edited(ctx)
			return p, cmd
		}
		return p, nil
	case tea.KeyBackspace:
		if len(p.input) == 0 {
			return p, nil
		}
		p.input = p.input[:len(p.input)-1]
		cmd := p.edited(ctx)
		return p, cmd
	}
	if k.Mod&tea.ModCtrl != 0 && k.Code == 'u' {
		p.input = nil
		cmd := p.edited(ctx)
		return p, cmd
	}
	if k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
		p.input = append(p.input, []rune(k.Text)...)
		cmd := p.edited(ctx)
		return p, cmd
	}
	return p, nil
}

// edited schedules a suggestion request after the debounce.
func (p *searchPage) edited(ctx *Context) tea.Cmd {
	p.seq++
	p.sugSel = -1
	if strings.TrimSpace(string(p.input)) == "" {
		p.suggest = nil
		return nil
	}
	return after(ctx.Root, suggestDebounce, pageMsg{to: p, msg: suggestTickMsg{p.seq}})
}

func (p *searchPage) search(q string, ctx *Context) tea.Cmd {
	p.seq++
	seq := p.seq
	p.query, p.editing = q, false
	p.suggest, p.sugSel = nil, -1
	p.state, p.err, p.top = stateLoading, "", nil
	p.panel = panelSongs
	tag := func(k string) string { return k + "@" + itoa(seq) }
	return tea.Batch(
		ctx.Load(p, tag("all"), func(c context.Context, api ytmusic.Client) (any, error) {
			return api.Search(c, q, ytmusic.FilterAll, 0)
		}),
		ctx.Load(p, tag("songs"), func(c context.Context, api ytmusic.Client) (any, error) {
			r, err := api.Search(c, q, ytmusic.FilterSongs, searchSongsLimit)
			return songResults(r), err
		}),
	)
}

// songResults marks a songs-filtered answer apart from the unfiltered one.
type songResults ytmusic.SearchResults

func itoa(n uint64) string { return strconv.FormatUint(n, 10) }

// Receive implements receiver.
func (p *searchPage) Receive(msg tea.Msg, ctx *Context) tea.Cmd {
	switch msg := msg.(type) {
	case suggestTickMsg:
		if msg.seq != p.seq || !p.editing {
			return nil
		}
		q, seq := string(p.input), p.seq
		return ctx.Load(p, "suggest@"+itoa(seq), func(c context.Context, api ytmusic.Client) (any, error) {
			return api.SearchSuggestions(c, q)
		})
	case Loaded:
		kind, at, _ := strings.Cut(msg.Key, "@")
		if at != itoa(p.seq) {
			return nil // stale
		}
		return p.loaded(kind, msg, ctx)
	}
	return nil
}

func (p *searchPage) loaded(kind string, l Loaded, ctx *Context) tea.Cmd {
	if kind == "suggest" {
		if l.Err == nil && p.editing {
			p.suggest, _ = l.Value.([]string)
			p.suggest = p.suggest[:min(len(p.suggest), 8)]
		}
		return nil
	}
	if l.Err != nil {
		if p.state != stateLoaded {
			p.state, p.err = stateFailed, loadErrorText(l.Err)
		}
		ctx.Log.Warn("ui: search failed", "kind", kind, "err", l.Err)
		return nil
	}
	p.state = stateLoaded
	switch v := l.Value.(type) {
	case songResults:
		var ts []ytmusic.Track
		for _, r := range v.Items {
			if r.Track != nil {
				ts = append(ts, *r.Track)
			}
		}
		p.songs.SetRows(ts)
	case ytmusic.SearchResults:
		p.grouped(v)
	}
	return nil
}

// grouped fills the album, artist, playlist and video panels from an
// unfiltered search; the top result also leads its own panel.
func (p *searchPage) grouped(v ytmusic.SearchResults) {
	p.albums, p.artists, p.playlist = nil, nil, nil
	var videos []ytmusic.Track
	for i, r := range v.Items {
		if r.Top {
			p.top = &v.Items[i]
		}
		switch {
		case r.Album != nil:
			p.albums = append(p.albums, *r.Album)
		case r.Artist != nil:
			p.artists = append(p.artists, *r.Artist)
		case r.Playlist != nil:
			p.playlist = append(p.playlist, *r.Playlist)
		case r.Track != nil && (r.Type == ytmusic.TypeVideo || r.Track.IsVideo):
			videos = append(videos, *r.Track)
		}
	}
	p.videos.SetRows(videos)
	items := func(n int, f func(i int) components.Item) []components.Item {
		out := make([]components.Item, n)
		for i := range out {
			out[i] = f(i)
		}
		return out
	}
	p.lists[panelAlbums].SetItems(items(len(p.albums), func(i int) components.Item {
		r := p.albums[i]
		return components.Item{Left: r.Name, Right: albumDetail(r), Key: r.ID}
	}))
	p.lists[panelArtists].SetItems(items(len(p.artists), func(i int) components.Item {
		r := p.artists[i]
		return components.Item{Left: r.Name, Right: r.Audience, Key: r.ID}
	}))
	p.lists[panelPlaylists].SetItems(items(len(p.playlist), func(i int) components.Item {
		r := p.playlist[i]
		return components.Item{Left: r.Title, Right: joinNonEmpty(" · ", r.Author, r.Count), Key: r.ID}
	}))
}

func (p *searchPage) Action(a keys.Action, ctx *Context) (Page, tea.Cmd, bool) {
	switch a.Name {
	case "search":
		p.editing = true // g s on the page: back to the input
		return p, nil, true
	case "toggle_search_mode":
		p.all = !p.all
		if !p.all && p.panel == panelVideos {
			p.panel = panelSongs
		}
		return p, nil, true
	case "focus_next", "focus_prev":
		n := p.panels()
		d := 1
		if a.Name == "focus_prev" {
			d = n - 1
		}
		p.panel = (p.panel + d) % n
		return p, nil, true
	case "retry":
		if p.state == stateFailed && p.query != "" {
			cmd := p.search(p.query, ctx)
			return p, cmd, true
		}
		return p, nil, false
	}
	switch p.panel {
	case panelSongs, panelVideos:
		t := p.songs
		if p.panel == panelVideos {
			t = p.videos
		}
		if cmd, ok := trackAction(t, a, ""); ok {
			return p, cmd, true
		}
		return p, nil, t.Action(a)
	}
	l := p.lists[p.panel]
	if a.Name != "select" {
		return p, nil, l.Action(a)
	}
	_, i, ok := l.Selected()
	if !ok {
		return p, nil, true
	}
	var next Page
	switch p.panel {
	case panelAlbums:
		next = newAlbumPage(p.albums[i].ID, p.albums[i].Name, ctx.Columns)
	case panelArtists:
		next = newArtistPage(p.artists[i].ID, p.artists[i].Name, ctx.Columns)
	default:
		next = newPlaylistPage(p.playlist[i].ID, p.playlist[i].Title, ctx.Columns)
	}
	return p, msgCmd(openMsg{next}), true
}

func (p *searchPage) View(width, height int, st *style.UI) string {
	mode := "music"
	if p.all {
		mode = "all"
	}
	cur := ""
	if p.editing {
		cur = "█"
	}
	in := " " + st.Section.Render("Search ▸ ") + st.Text.Render(string(p.input)+cur)
	right := st.Muted.Render("mode: " + mode + " ")
	lines := []string{components.Fit(in, max(width-components.Width(right), 0)) + right}

	if p.editing && len(p.suggest) > 0 {
		for i, s := range p.suggest {
			if i == p.sugSel {
				lines = append(lines, "   "+st.Selected.Render(" "+s+" "))
			} else {
				lines = append(lines, "   "+st.Muted.Render(" "+s))
			}
		}
		lines = append(lines, "")
		if p.state == stateIdle {
			return components.Block(lines, width, height)
		}
	}
	switch p.state {
	case stateIdle:
		lines = append(lines, "", " "+st.Muted.Render("Type to search, enter runs it, esc leaves the input. Tab cycles panels; g s edits the query."))
		return components.Block(lines, width, height)
	case stateLoading:
		lines = append(lines, "", " "+st.Muted.Render("Searching “"+p.query+"”…"))
		return components.Block(lines, width, height)
	case stateFailed:
		lines = append(lines, "", " "+st.ErrorText.Render(p.err))
		return components.Block(lines, width, height)
	}
	if p.top != nil {
		lines = append(lines, " "+st.Muted.Render("Top result: ")+st.Text.Render(p.top.Title())+st.Muted.Render(" · "+typeLabel(p.top.Type)))
	}
	lines = append(lines, tabBar(panelNames[:p.panels()], p.panel, st))
	head := components.Block(lines, width, min(len(lines), height))
	body := height - len(lines)
	if body <= 0 {
		return head
	}
	var main string
	switch p.panel {
	case panelSongs:
		main = p.songs.View(width, body, st)
	case panelVideos:
		main = p.videos.View(width, body, st)
	default:
		main = p.lists[p.panel].View(width, body, st, !p.editing)
	}
	return head + "\n" + main
}

func typeLabel(t ytmusic.ResultType) string {
	if t == "" {
		return ""
	}
	return strings.ToUpper(string(t[:1])) + string(t[1:])
}
