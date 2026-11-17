package ui

import (
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

// Track page geometry (spec 09): the now-playing panel takes 40 % of the
// main area, 30–60 cells; below trackSplitMin cells only one panel shows
// and tab switches between them.
const (
	trackLeftPct  = 40
	trackLeftMin  = 30
	trackLeftMax  = 60
	trackSplitMin = 70
)

// trackPage shows the playing track beside "up next" (the queue, with the
// Queue page's keys). It reads playback state from the root's bar.
type trackPage struct {
	bar    *barState
	art    *artState
	upNext *queuePage
	// Narrow main area: one panel at a time (lastSingle), showNext picks it.
	showNext, lastSingle bool
	// rightX is where up next's content starts, in page coordinates.
	rightX int

	// Progress line of the last View in page coordinates (w 0 = none).
	progX, progY, progW int
}

func newTrackPage(bar *barState, a *artState, cols components.Columns, q *queue.Queue) *trackPage {
	return &trackPage{bar: bar, art: a, upNext: newQueuePage(cols, q)}
}

// spectrumRows is the height of the spectrum area (0 until phase 6).
func (p *trackPage) spectrumRows() int { return 0 }

func (p *trackPage) ID() PageID { return PageTrack }

func (p *trackPage) nextShown() bool { return !p.lastSingle || p.showNext }

func (p *trackPage) Capturing() bool { return p.nextShown() && p.upNext.Capturing() }

func (p *trackPage) Info() string {
	if p.nextShown() {
		return p.upNext.Info()
	}
	return ""
}

func (p *trackPage) Key(msg tea.KeyPressMsg, ctx *Context) (Page, tea.Cmd) {
	_, cmd := p.upNext.Key(msg, ctx)
	return p, cmd
}

// QueueChanged implements queueListener.
func (p *trackPage) QueueChanged(q *queue.Queue, id string) { p.upNext.QueueChanged(q, id) }

func (p *trackPage) Action(a keys.Action, ctx *Context) (Page, tea.Cmd, bool) {
	if a.Name == "focus_next" || a.Name == "focus_prev" {
		if p.lastSingle {
			p.showNext = !p.showNext
			return p, nil, true
		}
		return p, nil, false
	}
	if !p.nextShown() {
		return p, nil, false
	}
	_, cmd, handled := p.upNext.Action(a, ctx)
	return p, cmd, handled
}

// Split implements splitter: the outer width of the now-playing panel
// for a main area w wide (w: only now playing, 0: only up next).
func (p *trackPage) Split(w int) int {
	p.lastSingle = w < trackSplitMin
	lw := min(max(w*trackLeftPct/100, trackLeftMin), trackLeftMax)
	if p.lastSingle {
		lw = w
		if p.showNext {
			lw = 0
		}
	}
	p.rightX = lw
	return lw
}

// Panels implements splitter.
func (p *trackPage) Panels() (left, right panelSpec) {
	return panelSpec{title: "now playing"}, panelSpec{title: "up next", counter: countText(p.upNext), focused: true}
}

// ViewLeft renders "now playing": cover art (centered square), title,
// artist, album, then the progress line on the last row. The art box is
// kept while the image loads so the text does not jump.
func (p *trackPage) ViewLeft(w, h int, st *style.UI) string {
	p.progW = 0
	b := *p.bar
	if b.st.Item.IsZero() {
		return components.Block([]string{"", " " + st.Muted.Render("Nothing playing")}, w, h)
	}
	lines := []string{""}
	if cols, rows := artBox(w, h, p.spectrumRows()); cols > 0 && p.art.url != "" && p.art.enabled(st) {
		cells := p.art.cells(st, cols, rows)
		pad := strings.Repeat(" ", (w-cols)/2)
		for i := range rows {
			if cells != nil {
				lines = append(lines, pad+cells[i])
			} else {
				lines = append(lines, "")
			}
		}
		lines = append(lines, "")
	}
	title, _ := b.title()
	lines = append(lines, " "+st.BarTitle.Render(ansi.Truncate(title, w-2, "…")))
	if b.now.VideoID == b.st.Item.ID {
		if b.now.Artist != "" {
			lines = append(lines, " "+st.Accent.Render(ansi.Truncate(b.now.Artist, w-2, "…")))
		}
		if b.now.Album != "" {
			lines = append(lines, " "+st.Muted.Render(ansi.Truncate(b.now.Album, w-2, "…")))
		}
	}
	if h < len(lines)+2 {
		return components.Block(lines, w, h)
	}
	pos, dur := fmtDur(b.pos()), fmtDur(b.st.Duration)
	pw := w - 2 - ansi.StringWidth(pos) - ansi.StringWidth(dur) - 2
	bar := ""
	if pw > 0 {
		if b.style == "line" {
			bar = b.knobLine(pw, st)
		} else {
			bar = b.blockLine(pw, st)
		}
		p.progX, p.progY, p.progW = 1+ansi.StringWidth(pos)+1, h-1, pw
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	lines = append(lines, " "+st.BarTime.Render(pos)+" "+bar+" "+st.BarTime.Render(dur))
	return components.Block(lines, w, h)
}

// ViewRight renders up next.
func (p *trackPage) ViewRight(w, h int, st *style.UI) string { return p.upNext.View(w, h, st) }

// View is used when the frame has no border room: one column of both.
func (p *trackPage) View(width, height int, st *style.UI) string {
	if p.Split(width) == 0 {
		return p.ViewRight(width, height, st)
	}
	return p.ViewLeft(width, height, st)
}

// Progress implements progressor: the progress line in page coordinates.
func (p *trackPage) Progress() (x, y, w int) { return p.progX, p.progY, p.progW }

// Click implements mouser: clicks in up next (page x from leftW on, as
// the frame lays the two panels out) go to the queue table.
func (p *trackPage) Click(ev mouseEv, ctx *Context) (Page, tea.Cmd) {
	if !p.nextShown() || ev.x < p.rightX {
		return p, nil
	}
	ev.x -= p.rightX
	_, cmd := p.upNext.Click(ev, ctx)
	return p, cmd
}

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

// Activate implements activator: up next opens on the playing track.
func (p *trackPage) Activate(ctx *Context) tea.Cmd {
	p.upNext.Action(keys.Action{Name: "jump_to_current"}, ctx)
	return nil
}
