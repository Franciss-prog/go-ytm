package components

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/style"
)

// Item is one List row: Left is the label, Right a muted detail aligned to
// the right edge ("87 songs").
type Item struct {
	Left, Right string
	// Key identifies the item to the owning page (an id).
	Key string
}

// List is a cursor list with fuzzy filter, used for library playlists,
// albums, artists and shelves. Like TrackTable it never does I/O.
type List struct {
	items     []Item
	view      []int
	cursor    int
	top       int
	filter    string
	filtering bool
	empty     string
	lastH     int
}

// NewList returns an empty list showing empty when it has no items.
func NewList(empty string) *List { return &List{empty: empty, lastH: 10} }

// SetItems replaces the items, keeping the filter and moving to the top.
func (l *List) SetItems(items []Item) {
	l.items = items
	l.cursor, l.top = 0, 0
	l.rebuild()
}

// SetEmpty sets the empty-state text.
func (l *List) SetEmpty(s string) { l.empty = s }

// Len is the number of items (unfiltered).
func (l *List) Len() int { return len(l.items) }

// Filtering reports that the filter input owns the keyboard.
func (l *List) Filtering() bool { return l.filtering }

// Selected returns the item under the cursor and its index in SetItems
// order.
func (l *List) Selected() (Item, int, bool) {
	if l.cursor < 0 || l.cursor >= len(l.view) {
		return Item{}, -1, false
	}
	i := l.view[l.cursor]
	return l.items[i], i, true
}

// Cursor is the cursor's position among visible items.
func (l *List) Cursor() int { return l.cursor }

// SetCursor moves the cursor to visible position v (clamped).
func (l *List) SetCursor(v int) {
	l.cursor = max(min(v, len(l.view)-1), 0)
	l.scroll()
}

// HitRow maps a row y inside the list to a visible position, or -1.
func (l *List) HitRow(y int) int {
	v := l.top + y
	if y < 0 || y >= l.lastH || v >= len(l.view) {
		return -1
	}
	return v
}

func (l *List) rebuild() {
	l.view = l.view[:0]
	for i, it := range l.items {
		if FuzzyMatch(it.Left, l.filter) {
			l.view = append(l.view, i)
		}
	}
	l.cursor = max(min(l.cursor, len(l.view)-1), 0)
}

func (l *List) scroll() {
	h := max(l.lastH, 1)
	l.top = min(l.top, l.cursor)
	if l.cursor >= l.top+h {
		l.top = l.cursor - h + 1
	}
	l.top = max(min(l.top, len(l.view)-h), 0)
}

// Action handles motion and filter actions; it reports whether a was
// handled.
func (l *List) Action(a keys.Action) bool {
	page := max(l.lastH-1, 1)
	switch a.Name {
	case "move_down":
		l.SetCursor(l.cursor + a.N())
	case "move_up":
		l.SetCursor(l.cursor - a.N())
	case "page_down":
		l.SetCursor(l.cursor + page*a.N())
	case "page_up":
		l.SetCursor(l.cursor - page*a.N())
	case "go_top":
		l.SetCursor(a.Count - 1)
	case "go_bottom":
		if a.Count > 0 {
			l.SetCursor(a.Count - 1)
		} else {
			l.SetCursor(len(l.view) - 1)
		}
	case "filter":
		l.filtering = true
	case "mark_clear":
		if l.filter == "" {
			return false
		}
		l.setFilter("")
	default:
		return false
	}
	return true
}

// Key edits the filter while Filtering.
func (l *List) Key(msg tea.KeyPressMsg) {
	k := msg.Key()
	switch k.Code {
	case tea.KeyEscape:
		l.filtering = false
		l.setFilter("")
	case tea.KeyEnter:
		l.filtering = false
	case tea.KeyBackspace:
		if r := []rune(l.filter); len(r) > 0 {
			l.setFilter(string(r[:len(r)-1]))
		} else {
			l.filtering = false
		}
	default:
		if k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			l.setFilter(l.filter + k.Text)
		}
	}
}

func (l *List) setFilter(q string) {
	l.filter = q
	l.cursor, l.top = 0, 0
	l.rebuild()
}

// View renders the list into exactly width×height. focused draws the
// cursor row selected; an unfocused list shows no cursor.
func (l *List) View(width, height int, st *style.UI, focused bool) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if len(l.items) == 0 {
		return Block([]string{" " + st.Muted.Render(l.empty)}, width, height)
	}
	body := height
	if l.filtering || l.filter != "" {
		body--
	}
	l.lastH = max(body, 1)
	l.scroll()
	gw := ansi.StringWidth(st.SelectedGutter)
	lines := make([]string, 0, height)
	if len(l.view) == 0 {
		lines = append(lines, " "+st.Muted.Render("Nothing matches “"+l.filter+"”."))
	}
	for v := l.top; v < len(l.view) && len(lines) < body; v++ {
		it := l.items[l.view[v]]
		right := it.Right
		rw := ansi.StringWidth(right)
		inner := width - gw - 2
		leftW := max(inner-rw-1, 0)
		if rw == 0 {
			leftW = max(inner, 0)
		}
		text := " " + Fit(it.Left, leftW)
		if rw > 0 {
			text += " " + right
		}
		text = Fit(text+" ", width-gw)
		switch {
		case focused && v == l.cursor:
			lines = append(lines, st.Selected.Render(Fit(st.SelectedGutter, gw))+st.Selected.Render(text))
		case rw > 0:
			lines = append(lines, strings.Repeat(" ", gw)+st.Text.Render(" "+Fit(it.Left, leftW)+" ")+st.Muted.Render(Fit(right+" ", width-gw-leftW-2)))
		default:
			lines = append(lines, strings.Repeat(" ", gw)+st.Text.Render(text))
		}
	}
	if body < height {
		for len(lines) < body {
			lines = append(lines, "")
		}
		cur := ""
		if l.filtering {
			cur = "█"
		}
		lines = append(lines, " "+st.Accent.Render("/")+st.Text.Render(l.filter+cur))
	}
	return Block(lines, width, height)
}
