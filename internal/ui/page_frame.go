package ui

import (
	"strings"

	"github.com/Franciss-Prog/go-ytm/internal/queue"
	"github.com/Franciss-Prog/go-ytm/internal/ui/components"
)

// Frame hooks of each page (spec 09): panel title, "cursor/total" counter
// and footer hints.

func tableCount(t *components.TrackTable) (int, int) {
	if t.Visible() == 0 {
		return 0, 0
	}
	return t.Cursor() + 1, t.Visible()
}

func listCount(l *components.List) (int, int) {
	if l.Shown() == 0 {
		return 0, 0
	}
	return l.Cursor() + 1, l.Shown()
}

var trackHints = []hint{
	hk("play", "select"), hk("queue", "add_to_queue"), hk("playlist", "add_to_playlist"),
	hk("actions", "track_actions"), hk("mark", "mark_toggle"), hk("filter", "filter"),
}

// Queue.

// PanelTitle implements panelTitler: "queue · 26 tracks · 1 hour 40 minutes · shuffle".
func (p *queuePage) PanelTitle() string {
	p.sync(false)
	parts := []string{"queue"}
	if n := p.q.Len(); n > 0 {
		parts = append(parts, plural(n, "track"), components.FormatDuration(p.total))
		if p.q.Shuffle() {
			parts = append(parts, "shuffle")
		}
		if r := p.q.Repeat(); r != queue.RepeatOff {
			parts = append(parts, "repeat "+r.String())
		}
	}
	return strings.Join(parts, " · ")
}

// Count implements counter.
func (p *queuePage) Count() (int, int) { return tableCount(p.table) }

// Hints implements hinter.
func (p *queuePage) Hints() []hint {
	return []hint{
		hk("play", "select"), hk("reorder", "reorder_down", "reorder_up"), hk("remove", "delete_item"),
		hk("mark", "mark_toggle"), hk("filter", "filter"), hk("random", "play_random"),
	}
}

// Context (album / artist / playlist).

// PanelTitle implements panelTitler: "album · Discovery".
func (p *contextPage) PanelTitle() string {
	k := strings.ToLower(kindNames[p.kind])
	if p.name == "" {
		return k
	}
	return k + " · " + p.name
}

// Count implements counter.
func (p *contextPage) Count() (int, int) {
	if p.focus == 1 {
		return listCount(p.releases)
	}
	return tableCount(p.table)
}

// Hints implements hinter.
func (p *contextPage) Hints() []hint {
	if p.focus == 1 {
		return []hint{hk("open", "select"), hk("tracks", "focus_next"), hk("back", "go_back")}
	}
	return append(append([]hint{}, trackHints...), hk("back", "go_back"))
}

// Library.

// Count implements counter (the focused column).
func (p *libraryPage) Count() (int, int) {
	if c := p.col(); c.table != nil {
		return tableCount(c.table)
	} else if c.state == stateLoaded {
		return listCount(c.list)
	}
	return 0, 0
}

// PanelTitle implements panelTitler: "library · Artists · John Mayer".
func (p *libraryPage) PanelTitle() string {
	parts := []string{"library"}
	for _, c := range p.cols[1 : p.focus+1] {
		parts = append(parts, c.title)
	}
	return strings.Join(parts, " · ")
}

// Hints implements hinter.
func (p *libraryPage) Hints() []hint {
	if p.col().table != nil {
		return []hint{
			hk("play", "select"), hk("play all", "play_all"), hk("level", "column_left"),
			hk("queue", "add_to_queue"), hk("playlist", "add_to_playlist"), hk("actions", "track_actions"),
			hk("filter", "filter"),
		}
	}
	return []hint{hk("open", "select"), hk("level", "column_left", "column_right"), hk("play all", "play_all"), hk("filter", "filter")}
}

// Search.

// Count implements counter.
func (p *searchPage) Count() (int, int) {
	if p.state != stateLoaded {
		return 0, 0
	}
	switch p.panel {
	case panelSongs:
		return tableCount(p.songs)
	case panelVideos:
		return tableCount(p.videos)
	}
	if l := p.lists[p.panel]; l != nil {
		return listCount(l)
	}
	return 0, 0
}

// Hints implements hinter. While the input is focused keys are raw, so the
// hints are literal.
func (p *searchPage) Hints() []hint {
	if p.editing {
		return []hint{{key: "enter", label: "search"}, {key: "↑/↓", label: "suggestions"}, {key: "esc", label: "results"}}
	}
	return append([]hint{hk("panel", "focus_next"), hk("edit", "search")}, trackHints...)
}

// Help.

// Hints implements hinter.
func (h *help) Hints() []hint {
	if h.filtering {
		return []hint{{key: "esc", label: "done"}}
	}
	return []hint{hk("filter", "filter"), hk("back", "go_back"), hk("settings", "settings")}
}

// Track page.

// Hints implements hinter: playback keys only (the card has no
// page-scoped keys).
func (p *trackPage) Hints() []hint {
	return []hint{
		hk("play", "play_pause"), hk("seek", "seek_backward", "seek_forward"),
		hk("vol", "volume_down", "volume_up"), hk("next", "next_track"), hk("prev", "previous_track"),
	}
}
