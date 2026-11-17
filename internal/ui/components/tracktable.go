package components

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// SortKey orders a TrackTable.
type SortKey int

// Sort keys. SortNone and SortDate both keep source order ("date added"
// for playlists, playback order for the queue); SortDate exists so that
// "s D" then "s r" lists newest first.
const (
	SortNone SortKey = iota
	SortTitle
	SortArtist
	SortAlbum
	SortDuration
	SortDate
)

var sortNames = [...]string{"", "title", "artist", "album", "duration", "date"}

// String is the name shown in the status line.
func (k SortKey) String() string { return sortNames[k] }

var sortActions = map[string]SortKey{
	"sort_title": SortTitle, "sort_artist": SortArtist, "sort_album": SortAlbum,
	"sort_duration": SortDuration, "sort_date": SortDate,
}

// Columns are the [ui] col_* widths; 0 means flexible.
type Columns struct {
	Index, Title, Artist, Album, Duration int
}

// TrackTable is the shared track list (docs/specs/05-ui-pages.md): cursor,
// marks, range marks, fuzzy filter, sort, playing and autoplay markers,
// dimmed unavailable rows and a progressive-loading footer. It renders
// only visible rows and caches each row string, so scrolling 1,000+ rows
// costs O(visible).
//
// Rows are addressed two ways: source index (position in SetRows order,
// stable under filter and sort) and view index (position on screen order).
type TrackTable struct {
	rows     []ytmusic.Track
	autoplay []bool
	view     []int // view index → source index

	cursor int // view index
	top    int // first visible view index

	marks  map[int]bool // source indices
	anchor int          // source index of the last mark toggle, -1 none

	filter    string
	filtering bool

	sort SortKey
	desc bool

	playing int // source index, -1 none

	loaded, total int // progressive loading; total 0 = done
	empty         string

	cols  Columns
	cache []rowCache
	dataV uint64
	lastH int // body height of the last View, for paging
}

type rowKey struct {
	width                    int
	sel, marked, playing, ap bool
	styleV, dataV            uint64
}

type rowCache struct {
	key rowKey
	s   string
	ok  bool
}

// NewTrackTable returns an empty table. empty is shown when it has no rows.
func NewTrackTable(cols Columns, empty string) *TrackTable {
	return &TrackTable{cols: cols, empty: empty, anchor: -1, playing: -1, marks: map[int]bool{}, lastH: 10}
}

// SetRows replaces the rows, keeping filter and sort, dropping marks and
// moving the cursor to the first row.
func (t *TrackTable) SetRows(rows []ytmusic.Track) {
	t.rows = rows
	t.autoplay = nil
	t.marks = map[int]bool{}
	t.anchor, t.playing = -1, -1
	t.cursor, t.top = 0, 0
	t.invalidate()
	t.rebuild()
}

// ReplaceRows swaps the rows for an updated version of the same list (the
// queue after a reorder), keeping the cursor on source index keep (-1:
// clamp the current cursor). Marks are dropped.
func (t *TrackTable) ReplaceRows(rows []ytmusic.Track, keep int) {
	cur := t.cursor
	t.rows = rows
	t.autoplay = nil
	t.marks = map[int]bool{}
	t.anchor = -1
	t.invalidate()
	t.rebuild()
	if keep >= 0 {
		t.SelectSource(keep)
		return
	}
	t.cursor = min(cur, max(len(t.view)-1, 0))
}

// AppendRows adds a continuation page, keeping cursor, marks and scroll.
func (t *TrackTable) AppendRows(rows []ytmusic.Track) {
	sel := t.CursorSource()
	t.rows = append(t.rows, rows...)
	t.cache = append(t.cache, make([]rowCache, len(rows))...)
	t.rebuild()
	if sel >= 0 {
		t.SelectSource(sel)
	}
}

// SetAutoplay marks rows added by autoplay (parallel to the rows).
func (t *TrackTable) SetAutoplay(flags []bool) {
	t.autoplay = flags
}

// SetPlaying marks the playing row by source index (-1: none).
func (t *TrackTable) SetPlaying(src int) { t.playing = src }

// SetPlayingID marks the first row with videoID as playing ("" clears).
func (t *TrackTable) SetPlayingID(videoID string) {
	t.playing = -1
	if videoID == "" {
		return
	}
	for i, r := range t.rows {
		if r.VideoID == videoID {
			t.playing = i
			return
		}
	}
}

// SetLoading shows "Loading… loaded/total" in the footer; total 0 hides
// it. total < 0 means unknown.
func (t *TrackTable) SetLoading(loaded, total int) { t.loaded, t.total = loaded, total }

// SetEmpty sets the empty-state text.
func (t *TrackTable) SetEmpty(s string) { t.empty = s }

// Len is the number of rows (unfiltered).
func (t *TrackTable) Len() int { return len(t.rows) }

// Rows returns the rows in source order. Do not modify.
func (t *TrackTable) Rows() []ytmusic.Track { return t.rows }

// Visible is the number of rows that pass the filter.
func (t *TrackTable) Visible() int { return len(t.view) }

// Filtering reports that the filter input owns the keyboard.
func (t *TrackTable) Filtering() bool { return t.filtering }

// Filter returns the filter text.
func (t *TrackTable) Filter() string { return t.filter }

// Sort returns the sort key and direction.
func (t *TrackTable) Sort() (SortKey, bool) { return t.sort, t.desc }

// Sorted reports whether rows are shown out of source order.
func (t *TrackTable) Sorted() bool { return t.sort != SortNone && (t.sort != SortDate || t.desc) }

// Cursor is the cursor's view index.
func (t *TrackTable) Cursor() int { return t.cursor }

// CursorSource is the cursor's source index, or -1 with no visible rows.
func (t *TrackTable) CursorSource() int {
	if t.cursor < 0 || t.cursor >= len(t.view) {
		return -1
	}
	return t.view[t.cursor]
}

// Selected returns the track under the cursor.
func (t *TrackTable) Selected() (ytmusic.Track, bool) {
	i := t.CursorSource()
	if i < 0 {
		return ytmusic.Track{}, false
	}
	return t.rows[i], true
}

// MarkCount is the number of marked rows.
func (t *TrackTable) MarkCount() int { return len(t.marks) }

// Marked reports whether source index i is marked.
func (t *TrackTable) Marked(i int) bool { return t.marks[i] }

// Targets returns the source indices an action applies to: the marked
// rows in view order (marks hidden by the filter included, after the
// visible ones), or else the cursor row.
func (t *TrackTable) Targets() []int {
	if len(t.marks) == 0 {
		if i := t.CursorSource(); i >= 0 {
			return []int{i}
		}
		return nil
	}
	out := make([]int, 0, len(t.marks))
	seen := make(map[int]bool, len(t.marks))
	for _, i := range t.view {
		if t.marks[i] {
			out = append(out, i)
			seen[i] = true
		}
	}
	for i := range t.rows {
		if t.marks[i] && !seen[i] {
			out = append(out, i)
		}
	}
	return out
}

// TargetTracks is Targets as tracks.
func (t *TrackTable) TargetTracks() []ytmusic.Track {
	idx := t.Targets()
	out := make([]ytmusic.Track, len(idx))
	for k, i := range idx {
		out[k] = t.rows[i]
	}
	return out
}

// Ordered returns the visible rows in screen order and the cursor's
// position among them (playing from a sorted or filtered list follows
// what is shown).
func (t *TrackTable) Ordered() ([]ytmusic.Track, int) {
	out := make([]ytmusic.Track, len(t.view))
	for v, i := range t.view {
		out[v] = t.rows[i]
	}
	return out, t.cursor
}

// ClearMarks drops every mark.
func (t *TrackTable) ClearMarks() { t.marks, t.anchor = map[int]bool{}, -1 }

// SelectSource moves the cursor to source index i when it is visible.
func (t *TrackTable) SelectSource(i int) bool {
	for v, s := range t.view {
		if s == i {
			t.SetCursor(v)
			return true
		}
	}
	return false
}

// SetCursor moves the cursor to view index v (clamped) and scrolls to it.
func (t *TrackTable) SetCursor(v int) {
	t.cursor = max(min(v, len(t.view)-1), 0)
	t.scroll(t.lastH)
}

// ToggleMark toggles the mark on view index v.
func (t *TrackTable) ToggleMark(v int) {
	if v < 0 || v >= len(t.view) {
		return
	}
	i := t.view[v]
	if t.marks[i] {
		delete(t.marks, i)
	} else {
		t.marks[i] = true
	}
	t.anchor = i
}

// MarkRange marks every row between the anchor (last toggled row) and
// view index v, inclusive. Without an anchor it marks v and anchors there.
func (t *TrackTable) MarkRange(v int) {
	if v < 0 || v >= len(t.view) {
		return
	}
	a := slices.Index(t.view, t.anchor)
	if a < 0 {
		t.marks[t.view[v]] = true
		t.anchor = t.view[v]
		return
	}
	for k := min(a, v); k <= max(a, v); k++ {
		t.marks[t.view[k]] = true
	}
	t.anchor = t.view[v]
}

// Action handles list actions (motion, marks, filter, sort), paging by the
// body height of the last View. It reports whether a was handled.
func (t *TrackTable) Action(a keys.Action) bool {
	page := max(t.lastH-1, 1)
	switch a.Name {
	case "move_down":
		t.SetCursor(t.cursor + a.N())
	case "move_up":
		t.SetCursor(t.cursor - a.N())
	case "page_down":
		t.SetCursor(t.cursor + page*a.N())
	case "page_up":
		t.SetCursor(t.cursor - page*a.N())
	case "go_top":
		t.SetCursor(a.Count - 1) // no count: row 0
	case "go_bottom":
		if a.Count > 0 {
			t.SetCursor(a.Count - 1)
		} else {
			t.SetCursor(len(t.view) - 1)
		}
	case "mark_toggle":
		t.ToggleMark(t.cursor)
		t.SetCursor(t.cursor + 1)
	case "mark_range":
		t.MarkRange(t.cursor)
	case "mark_clear":
		switch {
		case len(t.marks) > 0:
			t.ClearMarks()
		case t.filter != "":
			t.SetFilter("")
		default:
			return false
		}
	case "filter":
		t.filtering = true
	case "reverse_sort":
		if t.sort == SortNone {
			t.sort = SortDate
		}
		t.desc = !t.desc
		t.resort()
	default:
		k, ok := sortActions[a.Name]
		if !ok {
			return false
		}
		if t.sort == k {
			t.desc = !t.desc
		} else {
			t.sort, t.desc = k, false
		}
		t.resort()
	}
	return true
}

// Key edits the filter while Filtering: text appends, backspace deletes,
// enter keeps the filter, escape clears it.
func (t *TrackTable) Key(msg tea.KeyPressMsg) {
	k := msg.Key()
	switch k.Code {
	case tea.KeyEscape:
		t.filtering = false
		t.SetFilter("")
	case tea.KeyEnter:
		t.filtering = false
	case tea.KeyBackspace:
		if r := []rune(t.filter); len(r) > 0 {
			t.SetFilter(string(r[:len(r)-1]))
		} else {
			t.filtering = false
		}
	default:
		if k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			t.SetFilter(t.filter + k.Text)
		}
	}
}

// SetFilter sets the filter text and keeps the cursor on the same row
// when it still matches.
func (t *TrackTable) SetFilter(q string) {
	sel := t.CursorSource()
	t.filter = q
	t.rebuild()
	if sel < 0 || !t.SelectSource(sel) {
		t.cursor, t.top = 0, 0
	}
}

func (t *TrackTable) resort() {
	sel := t.CursorSource()
	t.rebuild()
	if sel >= 0 {
		t.SelectSource(sel)
	}
}

func (t *TrackTable) invalidate() {
	t.dataV++
	t.cache = make([]rowCache, len(t.rows))
}

// rebuild recomputes the view from rows, filter and sort.
func (t *TrackTable) rebuild() {
	t.view = t.view[:0]
	for i, r := range t.rows {
		if t.filter == "" || FuzzyMatch(r.Title, t.filter) || FuzzyMatch(ArtistNames(r.Artists), t.filter) ||
			FuzzyMatch(albumName(r), t.filter) {
			t.view = append(t.view, i)
		}
	}
	if t.sort != SortNone {
		slices.SortStableFunc(t.view, t.compare)
	}
	t.cursor = max(min(t.cursor, len(t.view)-1), 0)
}

func (t *TrackTable) compare(a, b int) int {
	ra, rb := t.rows[a], t.rows[b]
	var c int
	switch t.sort {
	case SortTitle:
		c = cmpFold(ra.Title, rb.Title)
	case SortArtist:
		c = cmpFold(ArtistNames(ra.Artists), ArtistNames(rb.Artists))
	case SortAlbum:
		c = cmpFold(albumName(ra), albumName(rb))
	case SortDuration:
		c = cmp.Compare(ra.Duration, rb.Duration)
	}
	if c == 0 {
		c = cmp.Compare(a, b)
	}
	if t.desc {
		c = -c
	}
	return c
}

func cmpFold(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) }

func albumName(r ytmusic.Track) string {
	if r.Album == nil {
		return ""
	}
	return r.Album.Name
}

// ArtistNames joins artist names with ", ".
func ArtistNames(as []ytmusic.Artist) string {
	switch len(as) {
	case 0:
		return ""
	case 1:
		return as[0].Name
	}
	names := make([]string, len(as))
	for i, a := range as {
		names[i] = a.Name
	}
	return strings.Join(names, ", ")
}

// FormatDuration renders 6:09 or 1:02:03; zero renders "".
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	s := int(d / time.Second)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// FormatCount renders n with thousands separators ("1,200").
func FormatCount(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return "-" + FormatCount(-n)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// Info is the selection info line: the focused row's full title.
func (t *TrackTable) Info() string {
	r, ok := t.Selected()
	if !ok {
		return ""
	}
	parts := []string{r.Title}
	if a := ArtistNames(r.Artists); a != "" {
		parts = append(parts, a)
	}
	if a := albumName(r); a != "" {
		parts = append(parts, a)
	}
	return strings.Join(parts, " · ")
}

// scroll keeps the cursor inside a body of height h.
func (t *TrackTable) scroll(h int) {
	h = max(h, 1)
	t.top = min(t.top, t.cursor)
	if t.cursor >= t.top+h {
		t.top = t.cursor - h + 1
	}
	t.top = max(min(t.top, len(t.view)-h), 0)
}

// layout is the column geometry for one width.
type layout struct {
	gutter, index, title, artist, album, dur int
}

func (t *TrackTable) layout(width int, st *style.UI) layout {
	l := layout{gutter: ansi.StringWidth(st.SelectedGutter) + 2, index: t.cols.Index, dur: t.cols.Duration}
	if l.index == 0 {
		l.index = max(len(strconv.Itoa(len(t.rows))), 2)
	}
	if l.dur == 0 {
		l.dur = 7
	}
	free := width - l.gutter - l.index - l.dur - 1 // spaces: after index, between text columns
	cols := 3
	switch {
	case width < 45:
		cols = 1
	case width < 70:
		cols = 2
	}
	free -= cols // separators before title, artist, album
	if free < cols {
		l.title = max(free, 0)
		return l
	}
	fixed := []int{t.cols.Title, t.cols.Artist, t.cols.Album}[:cols]
	weights := []int{2, 1, 1}[:cols]
	flexW, used := 0, 0
	for i, f := range fixed {
		if f > 0 {
			used += f
		} else {
			flexW += weights[i]
		}
	}
	widths := make([]int, 3)
	rest := max(free-used, 0)
	given := 0
	for i, f := range fixed {
		if f > 0 {
			widths[i] = min(f, free)
			continue
		}
		widths[i] = rest * weights[i] / max(flexW, 1)
		given += widths[i]
	}
	for i := range fixed { // rounding remainder to the first flex column
		if fixed[i] == 0 {
			widths[i] += rest - given
			break
		}
	}
	l.title, l.artist, l.album = widths[0], widths[1], widths[2]
	return l
}

// HitRow maps a row y inside the table (0 = header) to a view index, or -1.
func (t *TrackTable) HitRow(y int) int {
	v := t.top + y - 1
	if y < 1 || y > t.lastH || v >= len(t.view) {
		return -1
	}
	return v
}

// InGutter reports whether column x is in the mark gutter.
func (t *TrackTable) InGutter(x int, st *style.UI) bool {
	return x >= 0 && x < ansi.StringWidth(st.SelectedGutter)+2
}

// Wheel scrolls the cursor by n rows (negative: up).
func (t *TrackTable) Wheel(n int) { t.SetCursor(t.cursor + n) }

// View renders the table into exactly width×height.
func (t *TrackTable) View(width, height int, st *style.UI) string {
	if height <= 0 || width <= 0 {
		return ""
	}
	if len(t.rows) == 0 {
		lines := []string{"", " " + st.Muted.Render(t.empty)}
		if t.total != 0 {
			lines[1] = " " + st.Muted.Render(t.loadingText())
		}
		return Block(lines, width, height)
	}
	footer := t.footer(width, st)
	body := height - 1 // header
	if footer != "" {
		body--
	}
	body = max(body, 0)
	t.lastH = max(body, 1)
	t.scroll(t.lastH)

	l := t.layout(width, st)
	lines := make([]string, 0, height)
	lines = append(lines, t.header(l, width, st))
	if len(t.view) == 0 {
		lines = append(lines, " "+st.Muted.Render("No track matches “"+t.filter+"”."))
	}
	for v := t.top; v < len(t.view) && v < t.top+body; v++ {
		lines = append(lines, t.row(v, l, width, st))
	}
	for len(lines) < height-1 {
		lines = append(lines, "")
	}
	if footer != "" {
		lines = append(lines[:height-1], footer)
	}
	return Block(lines, width, height)
}

func (t *TrackTable) header(l layout, width int, st *style.UI) string {
	ind := func(k SortKey, s string) string {
		if t.sort == k && t.Sorted() {
			d := 0
			if t.desc {
				d = 1
			}
			return s + " " + st.Opts.Icons.Sort[d]
		}
		return s
	}
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", l.gutter))
	b.WriteString(FitRight("#", l.index) + " ")
	b.WriteString(" " + Fit(ind(SortTitle, "Title"), l.title))
	if l.artist > 0 {
		b.WriteString(" " + Fit(ind(SortArtist, "Artist"), l.artist))
	}
	if l.album > 0 {
		b.WriteString(" " + Fit(ind(SortAlbum, "Album"), l.album))
	}
	b.WriteString(FitRight(ind(SortDuration, "Time"), l.dur))
	return st.TableHeader.Render(Fit(b.String(), width))
}

func (t *TrackTable) row(v int, l layout, width int, st *style.UI) string {
	i := t.view[v]
	ap := i < len(t.autoplay) && t.autoplay[i]
	k := rowKey{
		width: width, sel: v == t.cursor, marked: t.marks[i], playing: i == t.playing, ap: ap,
		styleV: st.Version, dataV: t.dataV,
	}
	if c := t.cache[i]; c.ok && c.key == k {
		return c.s
	}
	s := t.renderRow(i, k, l, width, st)
	t.cache[i] = rowCache{key: k, s: s, ok: true}
	return s
}

func (t *TrackTable) renderRow(i int, k rowKey, l layout, width int, st *style.UI) string {
	r := t.rows[i]
	ic := st.Opts.Icons
	glyph := " "
	switch {
	case k.playing:
		glyph = ic.Play
	case k.marked:
		glyph = ic.Mark
	case k.ap:
		glyph = ic.Autoplay
	}
	gw := ansi.StringWidth(st.SelectedGutter)
	sel := ""
	if k.sel {
		sel = st.SelectedGutter
	}
	gutter := Fit(sel, gw) + Fit(glyph, 1) + " "

	idx := FitRight(strconv.Itoa(i+1), l.index) + " "
	title := " " + Fit(r.Title, l.title)
	var artist, album string
	if l.artist > 0 {
		artist = " " + Fit(ArtistNames(r.Artists), l.artist)
	}
	if l.album > 0 {
		album = " " + Fit(albumName(r), l.album)
	}
	dur := FitRight(FormatDuration(r.Duration), l.dur)
	rest := width - l.gutter - ansi.StringWidth(idx+title+artist+album+dur)
	pad := strings.Repeat(" ", max(rest, 0))

	g := gutter
	if glyph != " " {
		g = Fit(sel, gw) + st.MarkGlyph.Render(Fit(glyph, 1)) + " "
	}
	if k.sel {
		return Fit(st.Selected.Render(Fit(sel, gw))+st.Selected.Render(ansi.Strip(Fit(glyph, 1))+" "+idx+title+artist+album+dur+pad), width)
	}
	if !r.Available || r.VideoID == "" {
		return Fit(g+st.RowDim.Render(idx+title+artist+album+dur), width)
	}
	ts := st.Text
	if k.playing {
		ts = st.RowPlaying
	}
	return Fit(g+st.Muted.Render(idx)+ts.Render(title)+st.Muted.Render(artist+album+dur), width)
}

func (t *TrackTable) loadingText() string {
	if t.total < 0 {
		return "Loading… " + FormatCount(t.loaded)
	}
	return "Loading… " + FormatCount(t.loaded) + "/" + FormatCount(t.total)
}

// footer is the filter / sort / marks / loading status line ("" if none).
func (t *TrackTable) footer(width int, st *style.UI) string {
	var parts []string
	if t.filter != "" {
		parts = append(parts, fmt.Sprintf("filter: %s of %s", FormatCount(len(t.view)), FormatCount(len(t.rows))))
	}
	if t.Sorted() {
		d := 0
		if t.desc {
			d = 1
		}
		parts = append(parts, "sort: "+t.sort.String()+" "+st.Opts.Icons.Sort[d])
	}
	if n := len(t.marks); n > 0 {
		parts = append(parts, FormatCount(n)+" marked")
	}
	if t.total != 0 {
		parts = append(parts, t.loadingText())
	}
	if !t.filtering && t.filter == "" && len(parts) == 0 {
		return ""
	}
	left := ""
	if t.filtering || t.filter != "" {
		cur := ""
		if t.filtering {
			cur = "█"
		}
		left = " " + st.Accent.Render("/") + st.Text.Render(t.filter+cur)
	}
	right := st.Muted.Render(strings.Join(parts, " · ") + " ")
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return Fit(left+" "+right, width)
	}
	return left + strings.Repeat(" ", gap) + right
}
