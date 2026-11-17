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
	// Drill marks an item that opens a deeper level: a muted "▸" ends its
	// row (spec 09).
	Drill bool
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
	lastW     int
	wrap      bool
	gutter    int // width of the selection gutter at the last View
}

// NewList returns an empty list showing empty when it has no items.
func NewList(empty string) *List { return &List{empty: empty, lastH: 10} }

// SetItems replaces the items, keeping the filter and moving to the top.
func (l *List) SetItems(items []Item) {
	l.items = items
	l.cursor, l.top = 0, 0
	l.rebuild()
}

// SetWrap makes labels wider than the list wrap onto more lines instead of
// being cut with "…" ([ui] sidebar_overflow = "wrap").
func (l *List) SetWrap(on bool) { l.wrap = on }

// Filter is the current filter text.
func (l *List) Filter() string { return l.filter }

// Shown is the number of items passing the filter.
func (l *List) Shown() int { return len(l.view) }

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

// Click applies a click at row y inside the list as last drawn: a click
// moves the cursor, a click on the cursor row returns ClickActivate, a
// right click moves the cursor and returns ClickMenu.
func (l *List) Click(y int, right bool) ClickResult {
	v := l.HitRow(y)
	switch {
	case v < 0:
		return ClickNone
	case right:
		l.SetCursor(v)
		return ClickMenu
	case v == l.cursor:
		return ClickActivate
	}
	l.SetCursor(v)
	return ClickMoved
}

// SelectKey moves the cursor to the visible item with key k and reports
// whether there is one.
func (l *List) SelectKey(k string) bool {
	for v, i := range l.view {
		if l.items[i].Key == k {
			l.SetCursor(v)
			return true
		}
	}
	return false
}

// HitRow maps a row y inside the list (as last drawn) to a visible
// position, or -1. Wrapped items span several rows.
func (l *List) HitRow(y int) int {
	if y < 0 || y >= l.lastH {
		return -1
	}
	for v := l.top; v < len(l.view); v++ {
		h := l.rowsOf(v)
		if y < h {
			return v
		}
		y -= h
	}
	return -1
}

// rowsOf is how many screen rows visible item v takes at the last width.
func (l *List) rowsOf(v int) int {
	if !l.wrap || l.lastW <= 0 {
		return 1
	}
	return len(l.split(l.items[l.view[v]], l.lastW))
}

// listRow is one screen row of an item: a label part and, on the row that
// carries it, the right-aligned detail.
type listRow struct {
	left, right string
	drill       bool
}

// split lays item it out for width cells: one row when truncating, the
// wrapped label (detail on its last row when it fits, else on its own row)
// when wrapping.
func (l *List) split(it Item, width int) []listRow {
	inner := width - l.gutter - 2
	if it.Drill {
		inner -= 2
	}
	if !l.wrap || inner <= 0 {
		return []listRow{{it.Left, it.Right, it.Drill}}
	}
	lines := strings.Split(ansi.Wrap(it.Left, inner, ""), "\n")
	rows := make([]listRow, len(lines))
	for i, s := range lines {
		rows[i].left = strings.TrimRight(s, " ")
	}
	rows[0].drill = it.Drill
	if it.Right == "" {
		return rows
	}
	last := &rows[len(rows)-1]
	if ansi.StringWidth(last.left)+1+ansi.StringWidth(it.Right) <= inner {
		last.right = it.Right
		return rows
	}
	return append(rows, listRow{right: it.Right})
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
	if l.wrap && l.lastW > 0 {
		l.scrollWrapped(h)
		return
	}
	l.top = min(l.top, l.cursor)
	if l.cursor >= l.top+h {
		l.top = l.cursor - h + 1
	}
	l.top = max(min(l.top, len(l.view)-h), 0)
}

// scrollWrapped keeps the cursor item fully on screen (as far as it fits)
// and the screen full when items have different heights.
func (l *List) scrollWrapped(h int) {
	if len(l.view) == 0 {
		l.top = 0
		return
	}
	l.top = max(min(l.top, l.cursor), 0)
	used := 0
	for v := l.top; v <= l.cursor; v++ {
		used += l.rowsOf(v)
	}
	for used > h && l.top < l.cursor {
		used -= l.rowsOf(l.top)
		l.top++
	}
	// Pull earlier items in while the rows below the top are short of a
	// screen (scrolled to the end).
	below := 0
	for v := l.top; v < len(l.view); v++ {
		below += l.rowsOf(v)
	}
	for l.top > 0 && below+l.rowsOf(l.top-1) <= h {
		l.top--
		below += l.rowsOf(l.top)
	}
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
	gw := ansi.StringWidth(st.SelectedGutter)
	// The last column holds the ↑/↓ scroll arrows (spec 09).
	rw := max(width-1, 1)
	l.lastW, l.gutter = rw, gw
	l.scroll()
	lines := make([]string, 0, height)
	if len(l.view) == 0 {
		lines = append(lines, " "+st.Muted.Render("Nothing matches “"+l.filter+"”."))
	}
	lastV := -1
	for v := l.top; v < len(l.view) && len(lines) < body; v++ {
		sel := focused && v == l.cursor
		path := !focused && v == l.cursor // unfocused: keep the cursor visible, unfilled
		for k, r := range l.split(l.items[l.view[v]], rw) {
			if len(lines) == body {
				break
			}
			lines = append(lines, l.renderRow(r, k == 0, sel, path, rw, gw, st)+" ")
		}
		lastV = v
	}
	if n := len(lines); n > 0 && len(l.view) > 0 {
		if l.top > 0 {
			lines[0] = Fit(lines[0], rw) + st.Muted.Render("↑")
		}
		if lastV < len(l.view)-1 {
			lines[n-1] = Fit(lines[n-1], rw) + st.Muted.Render("↓")
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

// renderRow draws one item row: gutter (cursor marker on the item's first
// row), label, and the muted detail aligned right.
func (l *List) renderRow(r listRow, first, sel, path bool, width, gw int, st *style.UI) string {
	drill := ""
	if r.drill {
		drill = " ▸"
		width -= 2
	}
	ds := st.Muted
	if sel {
		ds = st.Selected
	}
	return l.renderText(r, first, sel, path, width, gw, st) + ds.Render(drill)
}

func (l *List) renderText(r listRow, first, sel, path bool, width, gw int, st *style.UI) string {
	rw := ansi.StringWidth(r.right)
	inner := width - gw - 2
	// Truncating lists drop the detail when it would leave the label fewer
	// than 16 cells (narrow Library columns); wrapping lists give it a row.
	if !l.wrap && rw > 0 && inner-rw-1 < 16 {
		r.right, rw = "", 0
	}
	leftW := max(inner, 0)
	if rw > 0 {
		leftW = max(inner-rw-1, 0)
	}
	if sel {
		text := " " + Fit(r.left, leftW)
		if rw > 0 {
			text += " " + r.right
		}
		g := ""
		if first {
			g = st.SelectedGutter
		}
		return st.Selected.Render(Fit(g, gw)) + st.Selected.Render(Fit(text+" ", width-gw))
	}
	pad := strings.Repeat(" ", gw)
	ts := st.Text
	if path {
		ts = st.Accent
	}
	if rw > 0 {
		return pad + ts.Render(" "+Fit(r.left, leftW)+" ") + st.Muted.Render(Fit(r.right+" ", width-gw-leftW-2))
	}
	return pad + ts.Render(Fit(" "+Fit(r.left, leftW)+" ", width-gw))
}
