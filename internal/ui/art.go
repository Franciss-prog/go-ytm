package ui

import (
	"context"
	"errors"
	"image"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/Franciss-Prog/go-ytm/internal/art"
	"github.com/Franciss-Prog/go-ytm/internal/style"
)

// Cover art (spec 09): the current track's art on the track page, as
// kitty graphics when the terminal answers the support query, else as
// half blocks.

// Art fetch and transmit sizes, in pixels.
const (
	artFetchPx    = 400
	artTransmitPx = 320
)

// artLoadedMsg carries a fetched image (or the error) for url.
type artLoadedMsg struct {
	url string
	img image.Image
	err error
}

// artState is the root's cover art state.
type artState struct {
	fetch art.Fetcher // nil: no art
	cache *art.Cache
	mode  string // [ui] album_art_mode: auto|kitty|blocks|off
	on    bool   // session toggle (toggle_album_art)
	kitty bool   // the terminal answered the kitty graphics query

	url string      // art URL of the current track ("" none)
	img image.Image // decoded and squared; nil until loaded

	// Kitty: the transmitted image and its placement size.
	id, nextID         int
	sentURL            string
	sentCols, sentRows int

	// Half blocks of the last draw.
	blocksKey [4]any
	blocks    []string
}

func newArtState(f art.Fetcher, mode string) artState {
	return artState{fetch: f, cache: art.NewCache(32), mode: mode, on: mode != "off", nextID: 1}
}

// enabled reports whether art should be drawn at all.
func (a *artState) enabled(st *style.UI) bool {
	return a.fetch != nil && a.on && a.mode != "off" && st.ArtCapable()
}

// useKitty reports whether art goes out as kitty graphics.
func (a *artState) useKitty() bool { return a.kitty && (a.mode == "auto" || a.mode == "kitty") }

// cells returns the art as cols×rows cell strings, or nil.
func (a *artState) cells(st *style.UI, cols, rows int) []string {
	if a.img == nil || cols <= 0 || rows <= 0 || !a.enabled(st) {
		return nil
	}
	if a.useKitty() {
		if a.id == 0 || a.sentURL != a.url || a.sentCols != cols || a.sentRows != rows {
			return nil // placement not sent yet; syncArt sends it after this Update
		}
		return st.KittyCells(a.id, cols, rows)
	}
	k := [4]any{a.url, cols, rows, st.Version}
	if a.blocks == nil || a.blocksKey != k {
		a.blocks, a.blocksKey = st.HalfBlocks(a.img, cols, rows), k
	}
	return a.blocks
}

// queryKitty asks the terminal for graphics support (at start).
func (m *Model) queryKitty() tea.Cmd {
	if m.art.fetch == nil || m.o.NoColor || (m.art.mode != "auto" && m.art.mode != "kitty") {
		return nil
	}
	return tea.Raw(art.Query)
}

// kittyReply handles a graphics protocol reply.
func (m *Model) kittyReply(ev uv.KittyGraphicsEvent) {
	if art.QueryOK(ev.Options, ev.Payload) {
		m.art.kitty = true
		m.log.Debug("ui: kitty graphics supported")
	}
}

// syncArt runs after every Update: it starts fetching the current
// track's art and (kitty) transmits it at the size the track page wants.
func (m *Model) syncArt() tea.Cmd {
	a := &m.art
	if a.fetch == nil {
		return nil
	}
	a.mode = m.o.Config.UI.AlbumArtMode // Settings may change it live
	url := ""
	if t, ok := m.pb.q.Current(); ok {
		url = art.URL(t.ThumbnailURL, artFetchPx)
	}
	var cmds []tea.Cmd
	if url != a.url {
		a.url, a.img, a.blocks = url, nil, nil
		if url != "" {
			if img, ok := a.cache.Get(url); ok {
				a.img = img
			} else {
				cmds = append(cmds, m.fetchArt(url))
			}
		}
	}
	if a.useKitty() && a.img != nil && a.enabled(m.st) && m.current == PageTrack {
		cols, rows := m.trackArtSize()
		if cols > 0 && (a.sentURL != a.url || a.sentCols != cols || a.sentRows != rows) {
			cmds = append(cmds, m.transmitArt(cols, rows))
		}
	}
	return tea.Batch(cmds...)
}

func (m *Model) fetchArt(url string) tea.Cmd {
	f, ctx := m.art.fetch, m.o.Context
	return func() tea.Msg {
		img, err := f.Fetch(ctx, url)
		if err != nil {
			return artLoadedMsg{url: url, err: err}
		}
		return artLoadedMsg{url: url, img: art.Square(img)}
	}
}

func (m *Model) artLoaded(msg artLoadedMsg) {
	if msg.err != nil {
		if !errors.Is(msg.err, context.Canceled) {
			m.log.Warn("ui: cover art failed", "url", msg.url, "err", msg.err)
		}
		return
	}
	m.art.cache.Put(msg.url, msg.img)
	if msg.url == m.art.url {
		m.art.img, m.art.blocks = msg.img, nil
	}
}

// transmitArt sends the current image as a new kitty image (freeing the
// previous one) with a cols×rows virtual placement.
func (m *Model) transmitArt(cols, rows int) tea.Cmd {
	a := &m.art
	seq, err := art.Transmit(a.nextID, art.Scale(a.img, artTransmitPx, artTransmitPx), cols, rows)
	if err != nil {
		m.log.Warn("ui: kitty transmit failed", "err", err)
		return nil
	}
	if a.id != 0 {
		seq = art.Delete(a.id) + seq
	}
	a.id, a.sentURL, a.sentCols, a.sentRows = a.nextID, a.url, cols, rows
	a.nextID = a.nextID%0xFFFFFF + 1
	return tea.Raw(seq)
}

// releaseArt frees the kitty image (on quit).
func (m *Model) releaseArt() tea.Cmd {
	if m.art.id == 0 {
		return nil
	}
	id := m.art.id
	m.art.id = 0
	return tea.Raw(art.Delete(id))
}

// trackArtSize is the art box the track page draws at the current size.
func (m *Model) trackArtSize() (cols, rows int) {
	tp, ok := m.pages[PageTrack].(*trackPage)
	l := m.layout()
	if !ok || !l.border {
		return 0, 0
	}
	_, cols, rows = cardLayout(l.mainW, l.mainH, tp.spectrumRows())
	return cols, rows
}
