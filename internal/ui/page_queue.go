package ui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/queue"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/ui/components"
)

// jumpMsg asks the root to play queue position pos.
type jumpMsg struct{ pos int }

// queueEditedMsg tells the root that a page changed the queue order.
type queueEditedMsg struct{}

func msgCmd(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

// queuePage shows the queue in playback order (shuffle-aware) with
// autoplay marks; J/K reorder (with counts), delete / d d remove, enter
// plays (spec 05, "Queue").
type queuePage struct {
	table *components.TrackTable
	q     *queue.Queue
	rev   uint64
	total time.Duration
}

func newQueuePage(cols components.Columns, q *queue.Queue) *queuePage {
	p := &queuePage{table: components.NewTrackTable(cols, "The queue is empty. Press enter on a track to play it."), q: q}
	p.sync(true)
	return p
}

func (p *queuePage) ID() PageID      { return PageQueue }
func (p *queuePage) Capturing() bool { return p.table.Filtering() }
func (p *queuePage) Info() string    { return p.table.Info() }

func (p *queuePage) Key(msg tea.KeyPressMsg, _ *Context) (Page, tea.Cmd) {
	p.table.Key(msg)
	return p, nil
}

// QueueChanged implements queueListener.
func (p *queuePage) QueueChanged(q *queue.Queue, _ string) {
	p.q = q
	p.sync(false)
}

// sync reloads rows when the queue changed, keeping the cursor on the same
// position.
func (p *queuePage) sync(force bool) {
	if !force && p.q.Rev() == p.rev {
		return
	}
	p.rev = p.q.Rev()
	entries := p.q.Entries()
	rows := p.q.Tracks()
	ap := make([]bool, len(entries))
	p.total = 0
	for i, e := range entries {
		ap[i] = p.q.IsAutoplay(e.ID)
		p.total += rows[i].Duration
	}
	keep := p.table.CursorSource()
	if keep >= len(rows) {
		keep = len(rows) - 1
	}
	p.table.ReplaceRows(rows, keep)
	p.table.SetAutoplay(ap)
	p.table.SetPlaying(p.q.CurrentIndex())
}

func (p *queuePage) Action(a keys.Action, ctx *Context) (Page, tea.Cmd, bool) {
	switch a.Name {
	case "select":
		if i := p.table.CursorSource(); i >= 0 {
			return p, msgCmd(jumpMsg{pos: i}), true
		}
		return p, nil, true
	case "jump_to_current":
		if i := p.q.CurrentIndex(); i >= 0 {
			p.table.SetFilter("")
			p.table.SelectSource(i)
		}
		return p, nil, true
	case "reorder_down", "reorder_up":
		cmd := p.reorder(a, ctx)
		return p, cmd, true
	case "delete_item":
		cmd := p.remove(ctx)
		return p, cmd, true
	}
	return p, nil, p.table.Action(a)
}

func (p *queuePage) reorder(a keys.Action, ctx *Context) tea.Cmd {
	if p.table.Sorted() || p.table.Filter() != "" {
		return ctx.Toast(Toast{Severity: SevInfo, Text: "Clear the filter and sort to reorder"})
	}
	from := p.table.CursorSource()
	if from < 0 {
		return nil
	}
	to := from + a.N()
	if a.Name == "reorder_up" {
		to = from - a.N()
	}
	to = max(min(to, p.q.Len()-1), 0)
	if to == from {
		return nil
	}
	p.q.Move(from, to)
	p.sync(false)
	p.table.SelectSource(to)
	return msgCmd(queueEditedMsg{})
}

func (p *queuePage) remove(ctx *Context) tea.Cmd {
	idx := p.table.Targets()
	if len(idx) == 0 {
		return nil
	}
	slices.Sort(idx)
	for _, i := range slices.Backward(idx) {
		p.q.Remove(i)
	}
	p.sync(false)
	return tea.Batch(msgCmd(queueEditedMsg{}), ctx.Toast(Toast{Severity: SevInfo, Text: "Removed " + plural(len(idx), "track")}))
}

func (p *queuePage) View(width, height int, st *style.UI) string {
	p.sync(false)
	head := " " + st.Title.Render("Queue")
	if n := p.q.Len(); n > 0 {
		head += st.Muted.Render("  " + plural(n, "track") + " · " + components.FormatDuration(p.total))
		var flags []string
		if p.q.Shuffle() {
			flags = append(flags, "shuffle")
		}
		if r := p.q.Repeat(); r != queue.RepeatOff {
			flags = append(flags, "repeat "+r.String())
		}
		for _, f := range flags {
			head += st.Muted.Render(" · ") + st.Accent.Render(f)
		}
	}
	if height <= 2 {
		return components.Block([]string{head}, width, height)
	}
	return components.Fit(head, width) + "\n" + components.Fit("", width) + "\n" + p.table.View(width, height-2, st)
}
