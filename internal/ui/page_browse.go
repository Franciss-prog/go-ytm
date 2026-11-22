package ui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/config"
	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/ui/components"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// Browse tabs (spec 05, "Browse").
const (
	browseForYou = iota
	browseCharts
	browseReleases
	browsePlaylists
)

var browseTabs = []string{"For You", "Charts", "New releases", "Playlists"}

const (
	browseShelfItems = 8  // items per home shelf on For You (ytm-player)
	browsePlaylistsN = 25 // Playlists tab cap
)

// browseEntry is what a Browse row opens.
type browseEntry struct {
	track    *ytmusic.Track
	album    *ytmusic.AlbumRef
	artist   *ytmusic.ArtistRef
	playlist *ytmusic.PlaylistRef
}

// browseTab is one tab's list and load state.
type browseTab struct {
	list    *components.List
	entries []browseEntry // parallel to the list items; zero = shelf heading
	state   loadState
	err     string
}

// browsePage is Browse: home shelves, charts of a region, new releases and
// the playlists of the home feed. Rows open their album, artist or
// playlist page; songs play (autoplay continues from them).
type browsePage struct {
	tab    int
	tabs   [4]*browseTab
	region string // chart region code ("ZZ" global)
	shelf  int    // home shelves to load ([ui] home_shelves)
	home   []ytmusic.HomeShelf
	homeOK bool // home loaded (For You and Playlists share it)
	bodyY  int
}

func newBrowsePage(region string, shelves int) *browsePage {
	p := &browsePage{region: normRegion(region), shelf: shelves}
	empties := []string{
		"No recommendations available.", "No chart data for this region; press c to pick another.",
		"No new releases.", "No playlists in your home feed right now.",
	}
	for i := range p.tabs {
		p.tabs[i] = &browseTab{list: components.NewList(empties[i])}
	}
	return p
}

func normRegion(r string) string {
	r = strings.ToUpper(strings.TrimSpace(r))
	if r == "" {
		return "ZZ"
	}
	return r
}

func (p *browsePage) ID() PageID      { return PageBrowse }
func (p *browsePage) cur() *browseTab { return p.tabs[p.tab] }
func (p *browsePage) Capturing() bool { return p.cur().list.Filtering() }

func (p *browsePage) Info() string {
	if it, _, ok := p.cur().list.Selected(); ok {
		return joinNonEmpty(" · ", it.Left, it.Right)
	}
	return ""
}

func (p *browsePage) Key(msg tea.KeyPressMsg, _ *Context) (Page, tea.Cmd) {
	p.cur().list.Key(msg)
	return p, nil
}

// Activate implements activator: the shown tab loads on first view.
func (p *browsePage) Activate(ctx *Context) tea.Cmd { return p.ensure(ctx) }

func (p *browsePage) ensure(ctx *Context) tea.Cmd {
	t := p.cur()
	if t.state != stateIdle {
		return nil
	}
	return p.load(ctx, p.tab)
}

func (p *browsePage) load(ctx *Context, tab int) tea.Cmd {
	t := p.tabs[tab]
	t.state, t.err = stateLoading, ""
	switch tab {
	case browseCharts:
		region := p.region
		return ctx.Load(p, "charts@"+region, func(c context.Context, api ytmusic.Client) (any, error) { return api.Charts(c, region) })
	case browseReleases:
		return ctx.Load(p, "explore", func(c context.Context, api ytmusic.Client) (any, error) { return api.Explore(c) })
	}
	// For You and Playlists both come from the home feed.
	if p.homeOK {
		p.fillHome()
		return nil
	}
	p.tabs[browseForYou].state, p.tabs[browsePlaylists].state = stateLoading, stateLoading
	n := p.shelf
	return ctx.Load(p, "home", func(c context.Context, api ytmusic.Client) (any, error) { return api.Home(c, n) })
}

// Receive implements receiver.
func (p *browsePage) Receive(msg tea.Msg, ctx *Context) tea.Cmd {
	l, ok := msg.(Loaded)
	if !ok {
		return nil
	}
	tab := map[string]int{"home": browseForYou, "explore": browseReleases}[l.Key]
	if k, region, ok := strings.Cut(l.Key, "@"); ok && k == "charts" {
		if region != p.region {
			return nil // a newer region was picked
		}
		tab = browseCharts
	}
	if l.Err != nil {
		ctx.Log.Warn("ui: browse load failed", "key", l.Key, "err", l.Err)
		t := p.tabs[tab]
		t.state, t.err = stateFailed, loadErrorText(l.Err)
		if tab == browseForYou {
			p.tabs[browsePlaylists].state, p.tabs[browsePlaylists].err = stateFailed, t.err
		}
		return nil
	}
	switch v := l.Value.(type) {
	case []ytmusic.HomeShelf:
		p.home, p.homeOK = v, true
		p.fillHome()
	case ytmusic.Charts:
		p.fillCharts(v)
	case ytmusic.Explore:
		var es []browseEntry
		var items []components.Item
		for i := range v.NewReleases {
			al := &v.NewReleases[i]
			es = append(es, browseEntry{album: al})
			items = append(items, components.Item{Left: al.Name, Right: albumDetail(*al), Key: al.ID})
		}
		p.set(browseReleases, items, es)
	}
	return nil
}

func (p *browsePage) set(tab int, items []components.Item, es []browseEntry) {
	t := p.tabs[tab]
	t.list.SetItems(items)
	t.entries, t.state = es, stateLoaded
	// Start on the first row that opens something (not a heading).
	for i, e := range es {
		if e != (browseEntry{}) {
			t.list.SetCursor(i)
			break
		}
	}
}

// fillHome builds For You (shelves with headings, 8 items each) and
// Playlists (every playlist of the feed, up to 25).
func (p *browsePage) fillHome() {
	var items, pls []components.Item
	var es, pes []browseEntry
	seen := map[string]bool{}
	for _, sh := range p.home {
		items = append(items, components.Item{Left: "── " + sh.Title})
		es = append(es, browseEntry{})
		for i, r := range sh.Items {
			e, it := entryOf(r)
			if i < browseShelfItems {
				items, es = append(items, it), append(es, e)
			}
			if r.Playlist != nil && !seen[r.Playlist.ID] && len(pls) < browsePlaylistsN {
				seen[r.Playlist.ID] = true
				pls, pes = append(pls, it), append(pes, e)
			}
		}
	}
	p.set(browseForYou, items, es)
	p.set(browsePlaylists, pls, pes)
}

// entryOf maps a home item to a row.
func entryOf(r ytmusic.SearchResult) (browseEntry, components.Item) {
	switch {
	case r.Track != nil:
		return browseEntry{track: r.Track}, components.Item{Left: r.Track.Title, Right: components.ArtistNames(r.Track.Artists), Key: r.Track.VideoID}
	case r.Album != nil:
		return browseEntry{album: r.Album}, components.Item{Left: r.Album.Name, Right: albumDetail(*r.Album), Key: r.Album.ID}
	case r.Artist != nil:
		return browseEntry{artist: r.Artist}, components.Item{Left: r.Artist.Name, Right: joinNonEmpty(" · ", "Artist", r.Artist.Audience), Key: r.Artist.ID}
	case r.Playlist != nil:
		return browseEntry{playlist: r.Playlist}, components.Item{Left: r.Playlist.Title, Right: joinNonEmpty(" · ", r.Playlist.Author, r.Playlist.Count), Key: r.Playlist.ID}
	}
	return browseEntry{}, components.Item{}
}

// fillCharts lists the region's chart playlists, then its top artists.
func (p *browsePage) fillCharts(c ytmusic.Charts) {
	var items []components.Item
	var es []browseEntry
	for i := range c.Playlists {
		pl := &c.Playlists[i]
		es = append(es, browseEntry{playlist: pl})
		items = append(items, components.Item{Left: pl.Title, Right: "Chart", Key: pl.ID})
	}
	if len(c.Artists) > 0 {
		items = append(items, components.Item{Left: "── Top artists"})
		es = append(es, browseEntry{})
		for i := range c.Artists {
			ar := &c.Artists[i]
			es = append(es, browseEntry{artist: ar})
			items = append(items, components.Item{Left: ar.Name, Right: ar.Audience, Key: ar.ID})
		}
	}
	p.set(browseCharts, items, es)
}

func (p *browsePage) Action(a keys.Action, ctx *Context) (Page, tea.Cmd, bool) {
	switch a.Name {
	case "focus_next", "focus_prev":
		d := 1
		if a.Name == "focus_prev" {
			d = len(browseTabs) - 1
		}
		p.tab = (p.tab + d) % len(browseTabs)
		cmd := p.ensure(ctx)
		return p, cmd, true
	case "retry":
		if p.cur().state == stateFailed {
			if p.tab == browseForYou || p.tab == browsePlaylists {
				p.homeOK = false
			}
			cmd := p.load(ctx, p.tab)
			return p, cmd, true
		}
		return p, nil, false
	case "pick_country":
		return p, msgCmd(openPopupMsg{countryPicker(p.region)}), true
	case "select":
		cmd := p.open(ctx)
		return p, cmd, true
	}
	return p, nil, p.cur().list.Action(a)
}

// open runs the row under the cursor.
func (p *browsePage) open(ctx *Context) tea.Cmd {
	t := p.cur()
	_, i, ok := t.list.Selected()
	if !ok || i >= len(t.entries) {
		return nil
	}
	e := t.entries[i]
	switch {
	case e.track != nil:
		return msgCmd(PlayMsg{Tracks: []ytmusic.Track{*e.track}})
	case e.album != nil:
		return msgCmd(openMsg{newAlbumPage(e.album.ID, e.album.Name, ctx.Columns)})
	case e.artist != nil:
		return msgCmd(openMsg{newArtistPage(e.artist.ID, e.artist.Name, ctx.Columns)})
	case e.playlist != nil:
		// Radio mixes ("RD…") have no playlist page: play their queue.
		if strings.HasPrefix(e.playlist.ID, "RD") {
			return msgCmd(RadioMsg{Mix: e.playlist.ID, Label: e.playlist.Title})
		}
		return msgCmd(openMsg{newPlaylistPage(e.playlist.ID, e.playlist.Title, ctx.Columns)})
	}
	return nil
}

// regionPicked switches the charts to code (the picker's choice).
func (p *browsePage) regionPicked(code string, ctx *Context) tea.Cmd {
	p.region = code
	t := p.tabs[browseCharts]
	t.list.SetItems(nil)
	t.entries = nil
	p.tab = browseCharts
	return p.load(ctx, browseCharts)
}

func (p *browsePage) View(width, height int, st *style.UI) string {
	right := ""
	if p.tab == browseCharts {
		right = st.Muted.Render("Region: " + regionName(p.region) + " (c) ")
	}
	tabs := tabBar(browseTabs, p.tab, st)
	lines := []string{components.Fit(tabs, max(width-components.Width(right), 0)) + right}
	p.bodyY = len(lines)
	head := components.Block(lines, width, min(len(lines), height))
	body := height - len(lines)
	if body <= 0 {
		return head
	}
	t := p.cur()
	var main string
	switch t.state {
	case stateIdle, stateLoading:
		main = components.Block([]string{" " + st.Muted.Render("Loading…")}, width, body)
	case stateFailed:
		main = components.Block([]string{" " + st.ErrorText.Render(t.err)}, width, body)
	default:
		main = t.list.View(width, body, st, true)
	}
	return head + "\n" + main
}

// Click implements mouser: the tab row switches tabs, rows take clicks.
func (p *browsePage) Click(ev mouseEv, ctx *Context) (Page, tea.Cmd) {
	if ev.y == 0 {
		if t := tabHit(browseTabs, ev.x); t >= 0 && !ev.right {
			p.tab = t
			cmd := p.ensure(ctx)
			return p, cmd
		}
		return p, nil
	}
	if ev.y < p.bodyY || p.cur().state != stateLoaded {
		return p, nil
	}
	name := listClick(p.cur().list.Click(ev.y-p.bodyY, ev.right))
	if name != "select" {
		return p, nil
	}
	cmd := p.open(ctx)
	return p, cmd
}

// Hovered implements hoverer: a song row.
func (p *browsePage) Hovered() string {
	t := p.cur()
	if _, i, ok := t.list.Selected(); ok && i < len(t.entries) && t.entries[i].track != nil {
		return t.entries[i].track.VideoID
	}
	return ""
}

// regionPickedMsg carries the country picker's choice to the root, which
// saves it and reloads the charts.
type regionPickedMsg struct{ code string }

// countryPicker is the chart region popup (68 regions, Global first),
// filtered by typing.
func countryPicker(current string) popup {
	items := make([]menuItem, len(chartRegions))
	cur := 0
	for i, r := range chartRegions {
		code := r.code
		items[i] = menuItem{label: r.name, run: func(*Context) (popup, tea.Cmd) { return nil, msgCmd(regionPickedMsg{code}) }}
		if code == current {
			cur = i
		}
	}
	m := newMenu("Chart region", items)
	m.filterable = true
	m.cursor = cur
	m.hint = "type to filter · enter pick · esc close"
	return m
}

// regionPicked saves [ui] region and reloads the charts.
func (m *Model) regionPicked(code string) tea.Cmd {
	m.o.Config.UI.Region = code
	var cmd tea.Cmd
	if b, ok := m.pages[PageBrowse].(*browsePage); ok {
		cmd = b.regionPicked(code, m.ctx())
	}
	return tea.Batch(cmd, m.persist([]config.Change{{Key: "ui.region", Value: code}}),
		m.toasts.push(Toast{Severity: SevInfo, Text: "Chart region: " + regionName(code)}))
}
