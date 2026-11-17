package ui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/queue"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/ui/components"
)

// PageTrack is the now-playing "track" tab (spec 09).
const PageTrack PageID = "track"

// Track page geometry (spec 09): the card column is cardPct of the
// content width, cardMin–cardMax cells, centered; the art is hidden when
// it would get fewer than artMinRows rows.
const (
	cardPct    = 60
	cardMin    = 30
	cardMax    = 80
	artMinRows = 4
)

// trackPage is the now-playing "track" tab (spec 09): a centered playing
// card (art, title, artist, album, time and volume, spectrum, progress).
// It reads playback state from the root's bar.
type trackPage struct {
	bar  *barState
	art  *artState
	spec *specState
	q    *queue.Queue

	// Progress line of the last View in page coordinates (w 0 = none).
	progX, progY, progW int
}

func newTrackPage(bar *barState, a *artState, s *specState, q *queue.Queue) *trackPage {
	return &trackPage{bar: bar, art: a, spec: s, q: q}
}

// spectrumRows is the height of the spectrum area: fixed while the
// visualizer is enabled, so the layout does not jump when it starts.
func (p *trackPage) spectrumRows() int {
	if p.spec.enabled {
		return spectrumHeight
	}
	return 0
}

func (p *trackPage) ID() PageID { return PageTrack }

func (p *trackPage) Capturing() bool { return false }

func (p *trackPage) Info() string { return "" }

func (p *trackPage) Key(_ tea.KeyPressMsg, _ *Context) (Page, tea.Cmd) { return p, nil }

func (p *trackPage) Action(_ keys.Action, _ *Context) (Page, tea.Cmd, bool) { return p, nil, false }

// Title implements titler: the panel title.
func (p *trackPage) Title() string { return "now playing" }

// cardText is the number of non-art rows of the card: gap, title, artist,
// album, gap, time line, gap, spectrum, progress.
func cardText(spectrum int) int { return 1 + 3 + 1 + 1 + 1 + spectrum + 1 }

// cardLayout sizes the card for a w×h page: the column width and the art
// box (0×0 when it does not fit).
func cardLayout(w, h, spectrum int) (cw, cols, rows int) {
	cw = min(max(w*cardPct/100, cardMin), cardMax, max(w-2, 0))
	rows = min(h-2-cardText(spectrum), cw/2) // one row of margin above and below
	if rows < artMinRows {
		return cw, 0, 0
	}
	return cw, rows * 2, rows
}

// View renders the card centered in width×height.
func (p *trackPage) View(w, h int, st *style.UI) string {
	p.progW = 0
	b := *p.bar
	if b.st.Item.IsZero() {
		return components.Block([]string{"", " " + st.Muted.Render("Nothing playing")}, w, h)
	}
	sr := p.spectrumRows()
	cw, cols, rows := cardLayout(w, h, sr)
	if cw <= 0 {
		return components.Block(nil, w, h)
	}
	var lines []string
	if cols > 0 && p.art.url != "" && p.art.enabled(st) {
		cells := p.art.cells(st, cols, rows)
		for i := range rows {
			if cells != nil {
				lines = append(lines, cells[i])
			} else {
				lines = append(lines, "") // keep the box while the image loads
			}
		}
		lines = append(lines, "")
	}
	title, _ := b.title()
	lines = append(lines, st.BarTitle.Render(ansi.Truncate(title, cw, "…")))
	artist, album := "", ""
	if b.now.VideoID == b.st.Item.ID {
		artist, album = b.now.Artist, b.now.Album
	}
	lines = append(lines,
		st.Accent.Render(ansi.Truncate(artist, cw, "…")),
		st.Muted.Render(ansi.Truncate(album, cw, "…")),
		"",
		st.Muted.Render(ansi.Truncate(p.status(b), cw, "…")),
		"")
	if sr > 0 {
		lines = append(lines, spectrumLines(p.spec.levels, cw, sr, st)...)
	}
	progRow := len(lines)
	if b.style == "line" {
		lines = append(lines, b.knobLine(cw, st))
	} else {
		lines = append(lines, b.blockLine(cw, st))
	}
	// Short panel: drop rows from the top (the art goes first).
	if len(lines) > h {
		cut := len(lines) - h
		lines, progRow = lines[cut:], progRow-cut
	}
	top, left := (h-len(lines))/2, (w-cw)/2
	pad := strings.Repeat(" ", left)
	out := make([]string, 0, h)
	for range top {
		out = append(out, "")
	}
	for _, l := range lines {
		out = append(out, pad+l)
	}
	p.progX, p.progY, p.progW = left, top+progRow, cw
	return components.Block(out, w, h)
}

// status is the "pos / dur · vol N% · i/n" line.
func (p *trackPage) status(b barState) string {
	s := fmtDur(b.pos()) + " / " + fmtDur(b.st.Duration)
	if b.st.Muted {
		s += " · muted"
	} else {
		s += " · vol " + strconv.Itoa(b.st.Volume) + "%"
	}
	if p.q != nil {
		if i, n := p.q.CurrentIndex(), p.q.Len(); i >= 0 && n > 0 {
			s += " · " + strconv.Itoa(i+1) + "/" + strconv.Itoa(n)
		}
	}
	return s
}

// Progress implements progressor: the progress line in page coordinates.
func (p *trackPage) Progress() (x, y, w int) { return p.progX, p.progY, p.progW }

// blockLine draws "████░░░░" for progress_style = "block".
func (b barState) blockLine(width int, u *style.UI) string {
	if width <= 0 {
		return ""
	}
	frac := 0.0
	if b.st.Duration > 0 {
		frac = min(max(float64(b.pos())/float64(b.st.Duration), 0), 1)
	}
	filled := int(frac * float64(width))
	return u.ProgressFilled.Render(strings.Repeat("█", filled)) + u.ProgressEmpty.Render(strings.Repeat("░", width-filled))
}
