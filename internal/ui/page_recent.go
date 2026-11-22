package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/ui/components"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// Recently Played tabs and their one-line explanations (ytm-player
// ui/pages/recently_played.py).
const (
	recentAll = iota
	recentLocal
	recentYTM
)

var (
	recentTabs  = []string{"All", "Local", "YT Music"}
	recentDescs = []string{
		"Local history first, followed by additional account history — grouped, not one timeline",
		"Tracks played in this app, most recent first",
		"Your account's play history from any device, in YouTube Music's order",
	}
)

// recentMax caps each source (100 local + 100 account rows, as the original).
const recentMax = 100

// localPlays is the in-app play log, newest first. shortcut: kept in memory
// for the session only; M7's SQLite store persists it.
type localPlays struct{ tracks []ytmusic.Track }

// add records a play that passed history_min_listen_seconds.
func (l *localPlays) add(t ytmusic.Track) {
	l.tracks = append([]ytmusic.Track{t}, l.tracks...)
	if len(l.tracks) > recentMax {
		l.tracks = l.tracks[:recentMax]
	}
}

// recentPage is Recently Played (spec 05): All · Local · YT Music.
type recentPage struct {
	tab     int
	table   *components.TrackTable
	local   *localPlays
	account []ytmusic.Track
	state   loadState // account history
	err     string
	shown   int // local plays count at the last fill (refill on change)
	tableY  int
}

func newRecentPage(cols components.Columns, local *localPlays) *recentPage {
	return &recentPage{table: components.NewTrackTable(cols, "No play history yet."), local: local, shown: -1}
}

func (p *recentPage) ID() PageID      { return PageRecent }
func (p *recentPage) Capturing() bool { return p.table.Filtering() }
func (p *recentPage) Info() string    { return p.table.Info() }

func (p *recentPage) Key(msg tea.KeyPressMsg, _ *Context) (Page, tea.Cmd) {
	p.table.Key(msg)
	return p, nil
}

// Activate implements activator: the account history is fetched every
// time the page is shown (plays from other devices appear).
func (p *recentPage) Activate(ctx *Context) tea.Cmd {
	p.fill()
	if p.state == stateLoading {
		return nil
	}
	return p.load(ctx)
}

func (p *recentPage) load(ctx *Context) tea.Cmd {
	if !ctx.SignedIn {
		p.state, p.err = stateFailed, loadErrorText(ytmusic.ErrUnauthenticated)
		p.fill()
		return nil
	}
	p.state, p.err = stateLoading, ""
	return ctx.Load(p, "history", func(c context.Context, api ytmusic.Client) (any, error) { return api.History(c) })
}

// Receive implements receiver.
func (p *recentPage) Receive(msg tea.Msg, ctx *Context) tea.Cmd {
	l, ok := msg.(Loaded)
	if !ok {
		return nil
	}
	if l.Err != nil {
		p.state, p.err = stateFailed, loadErrorText(l.Err)
		ctx.Log.Warn("ui: history load failed", "err", l.Err)
		p.fill()
		return nil
	}
	items, _ := l.Value.([]ytmusic.HistoryItem)
	p.account = p.account[:0]
	for _, it := range items[:min(len(items), recentMax)] {
		p.account = append(p.account, it.Track)
	}
	p.state = stateLoaded
	p.fill()
	return nil
}

// rows composes the active tab: All = local rows first in their order,
// then account-only rows; one row per video ID, a track in both keeps its
// local position (recently_played.py _compose_all).
func (p *recentPage) rows() []ytmusic.Track {
	switch p.tab {
	case recentLocal:
		return p.local.tracks
	case recentYTM:
		return p.account
	}
	seen := map[string]bool{}
	var out []ytmusic.Track
	for _, src := range [][]ytmusic.Track{p.local.tracks, p.account} {
		for _, t := range src {
			if !seen[t.VideoID] {
				seen[t.VideoID] = true
				out = append(out, t)
			}
		}
	}
	return out
}

func (p *recentPage) fill() {
	p.shown = len(p.local.tracks)
	p.table.SetRows(p.rows())
	empty := "No play history yet."
	switch {
	case p.tab != recentLocal && p.state == stateLoading:
		empty = "Loading history…"
	case p.tab == recentYTM && p.state == stateFailed:
		empty = p.err
	}
	p.table.SetEmpty(empty)
}

func (p *recentPage) Action(a keys.Action, ctx *Context) (Page, tea.Cmd, bool) {
	switch a.Name {
	case "focus_next", "focus_prev":
		d := 1
		if a.Name == "focus_prev" {
			d = len(recentTabs) - 1
		}
		p.tab = (p.tab + d) % len(recentTabs)
		p.fill()
		return p, nil, true
	case "retry":
		if p.state == stateFailed {
			cmd := p.load(ctx)
			p.fill()
			return p, cmd, true
		}
		return p, nil, false
	}
	if cmd, ok := trackAction(p.table, a, trackSource{}); ok {
		return p, cmd, true
	}
	return p, nil, p.table.Action(a)
}

func (p *recentPage) View(width, height int, st *style.UI) string {
	if p.shown != len(p.local.tracks) {
		p.fill()
	}
	lines := []string{tabBar(recentTabs, p.tab, st), " " + st.Muted.Render(recentDescs[p.tab]), radioButton(st)}
	p.tableY = len(lines)
	body := max(height-len(lines), 0)
	if p.tab == recentAll && p.state == stateFailed && body > 1 {
		// Partial failure: the local rows still show; say what is missing.
		body--
		return components.Block(lines, width, min(len(lines), height)) + "\n" + p.table.View(width, body, st) + "\n" +
			components.Fit(" "+st.ErrorText.Render("Account history unavailable: "+p.err), width)
	}
	head := components.Block(lines, width, min(len(lines), height))
	if body == 0 {
		return head
	}
	return head + "\n" + p.table.View(width, body, st)
}

// Click implements mouser: tabs, Start Radio, the table.
func (p *recentPage) Click(ev mouseEv, ctx *Context) (Page, tea.Cmd) {
	switch {
	case ev.y == 0:
		if t := tabHit(recentTabs, ev.x); t >= 0 && !ev.right {
			p.tab = t
			p.fill()
		}
		return p, nil
	case ev.y == 2 && !ev.right:
		return p, radioFromRows(p.table, "Radio: Recently Played")
	case ev.y < p.tableY:
		return p, nil
	}
	name := tableClick(p.table.Click(ev.x, ev.y-p.tableY, ev.right, ev.ctrl, ev.shift))
	if name == "" {
		return p, nil
	}
	_, cmd, _ := p.Action(keys.Action{Name: name}, ctx)
	return p, cmd
}

// Hovered implements hoverer.
func (p *recentPage) Hovered() string { return tableHovered(p.table) }
