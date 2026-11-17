package ui

import (
	"context"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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

// The Library browses like a file manager (spec 09): one column per level.
//
//	root ─┬ Liked Music → tracks
//	      ├ Artists → artist → releases → tracks
//	      ├ Albums → album → tracks
//	      └ Playlists → playlist → tracks
//
// Columns are cached per node key for the session, so walking back and
// forth costs no requests. Moving the cursor loads the next column after
// libraryPreviewDelay; a newer move makes older previews stale.

// nodeKind is what a column lists.
type nodeKind int

const (
	nodeRoot      nodeKind = iota
	nodePlaylists          // library playlists
	nodeAlbums             // library albums
	nodeArtists            // library artists
	nodeArtist             // one artist's releases
	nodeTracks             // a playlist's or album's tracks
)

// Node keys: the fixed lists, or a prefix and an id.
const (
	keyPlaylists = "playlists"
	keyAlbums    = "albums"
	keyArtists   = "artists"
	prefPlaylist = "pl:"
	prefAlbum    = "al:"
	prefArtist   = "ar:"
	likedID      = "LM" // the Liked Music playlist
)

// libraryRoot is the first column.
var libraryRoot = []components.Item{
	{Left: "Liked Music", Key: prefPlaylist + likedID, Drill: true},
	{Left: "Artists", Key: keyArtists, Drill: true},
	{Left: "Albums", Key: keyAlbums, Drill: true},
	{Left: "Playlists", Key: keyPlaylists, Drill: true},
}

// libraryPreviewDelay debounces loading the next column while the cursor
// moves.
const libraryPreviewDelay = 150 * time.Millisecond

// maxColumns is how many columns show at once; deeper levels push the
// leftmost out.
const maxColumns = 4

// columnWeights are column width percentages by visible column count.
var columnWeights = [maxColumns + 1][]int{nil, {100}, {35, 65}, {25, 30, 45}, {20, 20, 25, 35}}

// millerCol is one column: a list of nodes, or a track list.
type millerCol struct {
	key, title string
	kind       nodeKind
	state      loadState
	err        string
	list       *components.List       // every kind but nodeTracks
	table      *components.TrackTable // nodeTracks
	src        trackSource            // nodeTracks: queue context, owned playlist
	cont       string                 // nodeTracks: playlist pages still to load
	playlists  []ytmusic.PlaylistRef  // nodePlaylists: for playlist actions
}

func keyKind(k string) nodeKind {
	switch {
	case k == keyPlaylists:
		return nodePlaylists
	case k == keyAlbums:
		return nodeAlbums
	case k == keyArtists:
		return nodeArtists
	case strings.HasPrefix(k, prefArtist):
		return nodeArtist
	}
	return nodeTracks
}

func newMillerCol(key, title string, cols components.Columns) *millerCol {
	c := &millerCol{key: key, title: title, kind: keyKind(key)}
	if c.kind == nodeTracks {
		c.table = components.NewTrackTable(cols, "No tracks here.")
		c.table.SetCompact(true)
		return c
	}
	c.list = components.NewList("Nothing here.")
	return c
}

// libraryPage is the Library tab.
type libraryPage struct {
	cols  []*millerCol // the open path, cols[0] = root
	focus int
	cache map[string]*millerCol
	seq   uint64 // bumps per cursor move; stale previews are dropped

	// Drawn columns at the last View (mouse): first index and x ranges.
	first      int
	colX, colW []int
}

// libraryPreviewMsg loads the next column once the cursor rests.
type libraryPreviewMsg struct{ seq uint64 }

// playlistMore is the next page of a playlist column.
type playlistMore struct {
	tracks []ytmusic.Track
	next   string
}

func newLibraryPage() *libraryPage {
	root := &millerCol{key: "root", kind: nodeRoot, state: stateLoaded, list: components.NewList("")}
	root.list.SetItems(libraryRoot)
	return &libraryPage{cols: []*millerCol{root}, cache: map[string]*millerCol{}}
}

func (p *libraryPage) ID() PageID { return PageLibrary }

func (p *libraryPage) col() *millerCol { return p.cols[p.focus] }

func (p *libraryPage) Capturing() bool {
	c := p.col()
	if c.table != nil {
		return c.table.Filtering()
	}
	return c.list.Filtering()
}

func (p *libraryPage) Key(msg tea.KeyPressMsg, _ *Context) (Page, tea.Cmd) {
	if c := p.col(); c.table != nil {
		c.table.Key(msg)
	} else {
		c.list.Key(msg)
	}
	return p, nil
}

func (p *libraryPage) Info() string {
	c := p.col()
	if c.table != nil {
		return c.table.Info()
	}
	if it, _, ok := c.list.Selected(); ok {
		return joinNonEmpty(" · ", it.Left, it.Right)
	}
	return ""
}

// Activate implements activator: show the first column's preview.
func (p *libraryPage) Activate(ctx *Context) tea.Cmd {
	if len(p.cols) > 1 {
		return nil
	}
	return p.openChild(ctx)
}

// selectedNode is the drillable item under the focused list's cursor.
func (p *libraryPage) selectedNode() (components.Item, bool) {
	c := p.col()
	if c.list == nil || c.state != stateLoaded {
		return components.Item{}, false
	}
	it, _, ok := c.list.Selected()
	return it, ok && it.Drill
}

// openChild makes the column right of the focus show the selected node,
// from the cache or loading it.
func (p *libraryPage) openChild(ctx *Context) tea.Cmd {
	it, ok := p.selectedNode()
	if !ok {
		p.cols = p.cols[:p.focus+1]
		return nil
	}
	if len(p.cols) > p.focus+1 && p.cols[p.focus+1].key == it.Key {
		return nil
	}
	p.cols = p.cols[:p.focus+1]
	child, cached := p.cache[it.Key]
	if !cached {
		child = newMillerCol(it.Key, it.Left, ctx.Columns)
		p.cache[it.Key] = child
	}
	p.cols = append(p.cols, child)
	if cached && child.state != stateFailed {
		return nil
	}
	return p.load(ctx, child)
}

// schedulePreview arms the debounced preview of the next column.
func (p *libraryPage) schedulePreview(ctx *Context) tea.Cmd {
	p.seq++
	if _, ok := p.selectedNode(); !ok {
		p.cols = p.cols[:p.focus+1]
		return nil
	}
	return after(ctx.Root, libraryPreviewDelay, pageMsg{to: p, msg: libraryPreviewMsg{p.seq}})
}

func (p *libraryPage) load(ctx *Context, c *millerCol) tea.Cmd {
	c.state, c.err = stateLoading, ""
	key := c.key
	id := key[min(3, len(key)):]
	return ctx.Load(p, key, func(cx context.Context, api ytmusic.Client) (any, error) {
		switch {
		case key == keyPlaylists:
			return api.LibraryPlaylists(cx, 0)
		case key == keyAlbums:
			return api.LibraryAlbums(cx, 0)
		case key == keyArtists:
			return api.LibraryArtists(cx, 0)
		case strings.HasPrefix(key, prefArtist):
			return api.Artist(cx, id)
		case strings.HasPrefix(key, prefAlbum):
			return api.Album(cx, id)
		}
		return api.Playlist(cx, id)
	})
}

// loadMore fetches the next page of a playlist column.
func (p *libraryPage) loadMore(ctx *Context, c *millerCol) tea.Cmd {
	token := c.cont
	return ctx.Load(p, "more:"+c.key, func(cx context.Context, api ytmusic.Client) (any, error) {
		ts, next, err := api.PlaylistMore(cx, token)
		return playlistMore{ts, next}, err
	})
}

// Receive implements receiver.
func (p *libraryPage) Receive(msg tea.Msg, ctx *Context) tea.Cmd {
	switch m := msg.(type) {
	case libraryPreviewMsg:
		if m.seq != p.seq {
			return nil
		}
		return p.openChild(ctx)
	case Loaded:
		return p.loaded(m, ctx)
	}
	return nil
}

func (p *libraryPage) loaded(l Loaded, ctx *Context) tea.Cmd {
	key, more := strings.CutPrefix(l.Key, "more:")
	c := p.cache[key]
	if c == nil {
		return nil
	}
	if l.Err != nil {
		if more { // keep what loaded; stop paging
			c.cont = ""
			c.table.SetLoading(0, 0)
			ctx.Log.Warn("ui: library playlist page failed", "key", key, "err", l.Err)
			return nil
		}
		c.state, c.err = stateFailed, loadErrorText(l.Err)
		return nil
	}
	c.state = stateLoaded
	var items []components.Item
	switch v := l.Value.(type) {
	case []ytmusic.PlaylistRef:
		c.playlists = v
		items = playlistItems(v)
	case []ytmusic.AlbumRef:
		for _, r := range v {
			items = append(items, components.Item{Left: r.Name, Right: albumDetail(r), Key: prefAlbum + r.ID, Drill: true})
		}
	case []ytmusic.ArtistRef:
		for _, r := range v {
			items = append(items, components.Item{Left: r.Name, Right: r.Audience, Key: prefArtist + r.ID, Drill: true})
		}
	case ytmusic.ArtistPage:
		for _, r := range slices.Concat(v.Albums.Items, v.Singles.Items) {
			items = append(items, components.Item{Left: r.Name, Right: joinNonEmpty(" · ", r.Kind, r.Year), Key: prefAlbum + r.ID, Drill: true})
		}
		c.list.SetEmpty("No releases.")
	case ytmusic.Album:
		c.table.SetRows(v.Tracks)
		c.src = trackSource{queue: cmpOr(v.PlaylistID, v.ID)}
		return nil
	case ytmusic.Playlist:
		c.table.SetRows(v.Tracks)
		c.src = trackSource{queue: v.ID}
		if v.Owned && v.ID != likedID {
			c.src.playlist, c.src.name = v.ID, v.Title
		}
		c.cont = v.Continuation
		if c.cont != "" {
			total := v.TrackCount
			if total <= 0 {
				total = -1 // unknown
			}
			c.table.SetLoading(len(v.Tracks), total)
			return p.loadMore(ctx, c)
		}
		return nil
	case playlistMore:
		c.table.AppendRows(v.tracks)
		c.cont = v.next
		if c.cont == "" {
			c.table.SetLoading(0, 0)
			return nil
		}
		c.table.SetLoading(c.table.Len(), -1)
		return p.loadMore(ctx, c)
	}
	c.list.SetItems(items)
	if c == p.col() { // focused list arrived: preview its first node
		return p.openChild(ctx)
	}
	return nil
}

func playlistItems(v []ytmusic.PlaylistRef) []components.Item {
	items := make([]components.Item, len(v))
	for i, r := range v {
		items[i] = components.Item{Left: r.Title, Right: r.Count, Key: prefPlaylist + r.ID, Drill: true}
	}
	return items
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
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

// descend moves the focus into the column of the selected node.
func (p *libraryPage) descend(ctx *Context) tea.Cmd {
	if _, ok := p.selectedNode(); !ok {
		return nil
	}
	p.seq++ // a pending preview is now moot
	cmd := p.openChild(ctx)
	p.focus++
	if c := p.col(); c.list != nil && c.state == stateLoaded {
		cmd = tea.Batch(cmd, p.openChild(ctx))
	}
	return cmd
}

func (p *libraryPage) Action(a keys.Action, ctx *Context) (Page, tea.Cmd, bool) {
	c := p.col()
	switch a.Name {
	case "retry":
		if c.state == stateFailed {
			cmd := p.load(ctx, c)
			return p, cmd, true
		}
		if n := p.focus + 1; n < len(p.cols) && p.cols[n].state == stateFailed {
			cmd := p.load(ctx, p.cols[n])
			return p, cmd, true
		}
		return p, nil, false
	case "column_left":
		if p.focus > 0 {
			p.focus--
			p.seq++
		}
		return p, nil, true
	case "column_right":
		cmd := p.descend(ctx)
		return p, cmd, true
	case "play_all":
		t := c.table
		if t == nil && p.focus+1 < len(p.cols) {
			t = p.cols[p.focus+1].table
		}
		if t == nil || t.Len() == 0 {
			return p, nil, true
		}
		src := c.src
		if c.table == nil {
			src = p.cols[p.focus+1].src
		}
		return p, msgCmd(PlayMsg{Tracks: t.Rows(), Start: 0, Context: src.queue}), true
	case "current_context":
		cmd := p.openContext(ctx)
		return p, cmd, true
	}
	if c.state != stateLoaded {
		return p, nil, false
	}
	if c.table != nil {
		if cmd, ok := trackAction(c.table, a, c.src); ok {
			return p, cmd, true
		}
		return p, nil, c.table.Action(a)
	}
	switch a.Name {
	case "select":
		cmd := p.descend(ctx)
		return p, cmd, true
	case "track_actions", "delete_item":
		if c.kind != nodePlaylists {
			return p, nil, a.Name == "track_actions"
		}
		_, i, ok := c.list.Selected()
		if !ok {
			return p, nil, true
		}
		pl := c.playlists[i]
		switch {
		case a.Name == "track_actions":
			return p, msgCmd(openPopupMsg{playlistActionsPopup(pl)}), true
		case !pl.Owned:
			return p, ctx.Toast(Toast{Severity: SevInfo, Text: "Only your own playlists can be deleted"}), true
		}
		return p, msgCmd(openPopupMsg{deletePlaylistConfirm(pl)}), true
	}
	before, filter := c.list.Cursor(), c.list.Filter()
	handled := c.list.Action(a)
	if handled && (c.list.Cursor() != before || c.list.Filter() != filter) {
		cmd := p.schedulePreview(ctx)
		return p, cmd, true
	}
	return p, nil, handled
}

// openContext opens the full page (header, filter, sort) of the focused
// track column, or of the artist under the cursor.
func (p *libraryPage) openContext(ctx *Context) tea.Cmd {
	c := p.col()
	if c.list != nil {
		if it, ok := p.selectedNode(); ok {
			c = p.cache[it.Key]
			if c == nil {
				c = &millerCol{key: it.Key, title: it.Left, kind: keyKind(it.Key)}
			}
		}
	}
	id := c.key[min(3, len(c.key)):]
	var next Page
	switch {
	case strings.HasPrefix(c.key, prefPlaylist):
		next = newPlaylistPage(id, c.title, ctx.Columns)
	case strings.HasPrefix(c.key, prefAlbum):
		next = newAlbumPage(id, c.title, ctx.Columns)
	case strings.HasPrefix(c.key, prefArtist):
		next = newArtistPage(id, c.title, ctx.Columns)
	default:
		return nil
	}
	return msgCmd(openMsg{next})
}

// dropPlaylist removes a deleted playlist from the cached columns.
func (p *libraryPage) dropPlaylist(id string) {
	c := p.cache[keyPlaylists]
	if c != nil {
		if i := slices.IndexFunc(c.playlists, func(r ytmusic.PlaylistRef) bool { return r.ID == id }); i >= 0 {
			c.playlists = slices.Delete(slices.Clone(c.playlists), i, i+1)
			cur := c.list.Cursor()
			c.list.SetItems(playlistItems(c.playlists))
			c.list.SetCursor(min(cur, len(c.playlists)-1))
		}
	}
	key := prefPlaylist + id
	delete(p.cache, key)
	if i := slices.IndexFunc(p.cols, func(c *millerCol) bool { return c.key == key }); i >= 0 {
		p.cols = p.cols[:i]
		p.focus = min(p.focus, i-1)
	}
}

// playlistsChanged reloads the Playlists column when it is open; else it
// is dropped from the cache and loads when next shown.
func (p *libraryPage) playlistsChanged(ctx *Context) tea.Cmd {
	c := p.cache[keyPlaylists]
	if c == nil {
		return nil
	}
	if slices.Contains(p.cols, c) {
		return p.load(ctx, c)
	}
	delete(p.cache, keyPlaylists)
	return nil
}

// visible is the range of columns drawn: up to maxColumns ending at the
// last column, shifted left when the focus would fall off.
func (p *libraryPage) visible() (first, n int) {
	n = min(len(p.cols), maxColumns)
	first = len(p.cols) - n
	if p.focus < first {
		first = p.focus
	}
	return first, n
}

func (p *libraryPage) View(width, height int, st *style.UI) string {
	first, n := p.visible()
	avail := max(width-(n-1), 0)
	p.first, p.colX, p.colW = first, p.colX[:0], p.colW[:0]
	views := make([][]string, n)
	x, used := 0, 0
	for i := range n {
		w := avail * columnWeights[n][i] / 100
		if i == n-1 {
			w = avail - used
		}
		used += w
		p.colX, p.colW = append(p.colX, x), append(p.colW, w)
		x += w + 1
		views[i] = splitFixed(p.colView(p.cols[first+i], first+i == p.focus, w, height, st), w, height)
	}
	sep := st.Border.Render("│")
	rows := make([]string, height)
	for y := range height {
		var b strings.Builder
		for i := range n {
			if i > 0 {
				b.WriteString(sep)
			}
			b.WriteString(views[i][y])
		}
		rows[y] = b.String()
	}
	return strings.Join(rows, "\n")
}

func (p *libraryPage) colView(c *millerCol, focused bool, w, h int, st *style.UI) string {
	switch c.state {
	case stateIdle, stateLoading:
		if c.table == nil || c.table.Len() == 0 {
			return components.Block([]string{" " + st.Muted.Render("Loading…")}, w, h)
		}
	case stateFailed:
		var lines []string
		for _, l := range strings.Split(ansi.Wrap(c.err, max(w-2, 1), ""), "\n") {
			lines = append(lines, " "+st.ErrorText.Render(l))
		}
		return components.Block(lines, w, h)
	}
	if c.table != nil {
		return c.table.View(w, h, st)
	}
	return c.list.View(w, h, st, focused)
}

// columnAt maps a page x to a drawn column index, or -1 (separator).
func (p *libraryPage) columnAt(x int) int {
	for i := range p.colX {
		if x >= p.colX[i] && x < p.colX[i]+p.colW[i] {
			return p.first + i
		}
	}
	return -1
}

// Click implements mouser: a click focuses its column; in a list it moves
// the cursor (on the cursor row it opens the node), in a track column it
// acts like a track table click.
func (p *libraryPage) Click(ev mouseEv, ctx *Context) (Page, tea.Cmd) {
	i := p.columnAt(ev.x)
	if i < 0 {
		return p, nil
	}
	c := p.cols[i]
	if c.state != stateLoaded {
		return p, nil
	}
	if i != p.focus {
		p.focus = i
		p.seq++
	}
	if c.table != nil {
		name := tableClick(c.table.Click(ev.x-p.colX[i-p.first], ev.y, ev.right, ev.ctrl, ev.shift))
		if name == "" {
			return p, nil
		}
		_, cmd, _ := p.Action(keys.Action{Name: name}, ctx)
		return p, cmd
	}
	switch c.list.Click(ev.y, ev.right) {
	case components.ClickMoved:
		cmd := p.schedulePreview(ctx)
		return p, cmd
	case components.ClickActivate:
		_, cmd, _ := p.Action(keys.Action{Name: "select"}, ctx)
		return p, cmd
	case components.ClickMenu:
		_, cmd, _ := p.Action(keys.Action{Name: "track_actions"}, ctx)
		return p, cmd
	}
	return p, nil
}

// WheelAt implements wheeler: the wheel moves the cursor of the column
// under the pointer, focusing it.
func (p *libraryPage) WheelAt(x, _ int, a keys.Action, ctx *Context) tea.Cmd {
	if i := p.columnAt(x); i >= 0 && i != p.focus && p.cols[i].state == stateLoaded {
		p.focus = i
	}
	_, cmd, _ := p.Action(a, ctx)
	return cmd
}

// Hovered implements hoverer: the track under the cursor in a track column.
func (p *libraryPage) Hovered() string {
	if c := p.col(); c != nil && c.table != nil {
		return tableHovered(c.table)
	}
	return ""
}
