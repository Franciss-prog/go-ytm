package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Franciss-Prog/go-ytm/internal/player"
	"github.com/Franciss-Prog/go-ytm/internal/style"
)

// The frame (spec 09): tab bar and its rule, titled panels, the one-row
// mini player and the key-hint footer. Everything here is foreground-only
// so transparent mode stays free of background SGRs.

// tabDef is one tab of the tab bar.
type tabDef struct {
	name string
	page PageID
}

// tabDefs are the tabs in display order. Browse joins in M6.
var tabDefs = []tabDef{{"queue", PageQueue}, {"library", PageLibrary}, {"track", PageTrack}, {"search", PageSearch}}

// tabOf maps a page to the tab it belongs under; "" keeps the current tab
// (Context pages, Help).
func tabOf(id PageID) PageID {
	switch id {
	case PageQueue, PageLibrary, PageSearch, PageTrack:
		return id
	case PageLiked, PageRecent:
		return PageLibrary
	}
	return ""
}

// tabSpan is where a tab name was drawn on the tab bar (mouse).
type tabSpan struct {
	x, w int
	page PageID
}

// barButton is a clickable glyph (shuffle, repeat, like, play/pause) at
// screen row y.
type barButton struct {
	x, y, w int
	action  string
}

// frameGeom records where the frame drew its interactive parts in the last
// View, in screen coordinates. progressW/volumeW 0 = not drawn.
type frameGeom struct {
	tabs                            []tabSpan
	buttons                         []barButton
	progressY, progressX, progressW int
	volumeY, volumeX, volumeW       int
}

// layout is the screen split for the current size (spec 09 breakpoints).
type layout struct {
	tabs, rule, mini, hints bool
	// border: panels draw their rounded border (off when too small).
	border bool
	// top is the first screen row of the panels; panelH their height.
	top, panelH int
	// sideW, panelW, lyricsW are outer panel widths (0 = hidden).
	sideW, panelW, lyricsW int
	lyricsOverlay          bool
	// mainW×mainH is the page's size; mainX0/mainY0 its screen origin.
	mainW, mainH   int
	mainX0, mainY0 int
}

func (m *Model) layout() layout {
	w, h := m.width, m.height
	// The track page shows its own progress, so no mini player there.
	l := layout{tabs: true, rule: true, mini: m.current != PageTrack, hints: m.o.Config.UI.ShowHints}
	if h < 12 {
		l.rule, l.hints = false, false
	}
	chrome := 0
	for _, on := range []bool{l.tabs, l.rule, l.mini, l.hints} {
		if on {
			chrome++
		}
	}
	if l.tabs {
		l.top++
	}
	if l.rule {
		l.top++
	}
	l.panelH = max(h-chrome, 0)
	l.panelW = w
	if m.sidebar && w >= 80 {
		l.sideW = min(m.o.Config.UI.SidebarWidth+2, w/3)
		l.panelW -= l.sideW
	}
	if m.lyrics {
		l.lyricsW = min(m.o.Config.UI.LyricsWidth+2, max(l.panelW-20, 0))
		if w >= 100 {
			l.panelW -= l.lyricsW
		} else {
			l.lyricsOverlay = true
		}
	}
	l.border = l.panelH >= 3 && l.panelW >= 10
	in := 0
	if l.border {
		in = 1
	}
	l.mainW, l.mainH = max(l.panelW-2*in, 0), max(l.panelH-2*in, 0)
	l.mainX0, l.mainY0 = l.sideW+in, l.top+in
	return l
}

// inset is the border thickness (0 or 1).
func (l layout) inset() int {
	if l.border {
		return 1
	}
	return 0
}

// mainX maps screen column x to a page column; false outside the page
// (sidebar, borders, the lyrics pane).
func (l layout) mainX(x int) (int, bool) {
	x -= l.mainX0
	w := l.mainW
	if l.lyricsOverlay {
		w -= l.lyricsW
	}
	return x, x >= 0 && x < w
}

// mainY maps screen row y to a page row; false outside the page.
func (l layout) mainY(y int) (int, bool) {
	y -= l.mainY0
	return y, y >= 0 && y < l.mainH
}

// sideXY maps a screen cell to sidebar content coordinates.
func (l layout) sideXY(x, y int) (int, int, bool) {
	in := l.inset()
	x, y = x-in, y-l.top-in
	return x, y, l.sideW > 0 && x >= 0 && x < l.sideW-2*in && y >= 0 && y < l.panelH-2*in
}

// panelSpec describes one titled panel.
type panelSpec struct {
	title   string
	counter string // "3/20", bottom right
	info    string // selection info, bottom left
	focused bool
}

// panel draws body (already w-2 × h-2 when bordered) inside a rounded
// border with "│ title │" on the top edge and "│ counter │" on the bottom.
// It returns exactly h lines of exactly w cells.
func panel(st *style.UI, p panelSpec, body string, w, h int, border bool) []string {
	if !border {
		return splitFixed(body, w, h)
	}
	b := st.Border.Render
	inner := splitFixed(body, w-2, h-2)
	out := make([]string, 0, h)

	ts := st.PanelDim
	if p.focused {
		ts = st.PanelTitle
	}
	top, used := b("╭"), 2 // corners
	if p.title != "" && w >= 10 {
		t := ansi.Truncate(p.title, w-6, "…")
		top += b("│") + " " + ts.Render(t) + " " + b("│")
		used += ansi.StringWidth(t) + 4
	}
	out = append(out, top+b(strings.Repeat("─", max(w-used, 0))+"╮"))

	for _, l := range inner {
		out = append(out, b("│")+l+b("│"))
	}

	right, rw := "", 0
	if p.counter != "" && ansi.StringWidth(p.counter)+8 <= w {
		right = b("│") + " " + st.Muted.Render(p.counter) + " " + b("│──")
		rw = ansi.StringWidth(p.counter) + 6
	}
	left, lw := "", 0
	if room := w - 2 - rw - 3; p.info != "" && room > 4 {
		t := ansi.Truncate(p.info, room-2, "…")
		left = " " + st.InfoBar.Render(t) + " "
		lw = ansi.StringWidth(t) + 2
	}
	out = append(out, b("╰")+left+b(strings.Repeat("─", max(w-2-lw-rw, 0)))+right+b("╯"))
	return out
}

// counter is a page that reports its cursor position for the panel's
// bottom-right "3/20" (1-based cursor, total rows shown).
type counter interface{ Count() (cursor, total int) }

func countText(p any) string {
	c, ok := p.(counter)
	if !ok {
		return ""
	}
	cur, n := c.Count()
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", cur, n)
}

// panelTitler is a page with its own panel title ("album · Discovery").
type panelTitler interface{ PanelTitle() string }

func (m *Model) panelTitle() string {
	if t, ok := m.pages[m.current].(panelTitler); ok {
		return t.PanelTitle()
	}
	return strings.ToLower(m.pageTitle())
}

// tabBar renders " ytm  queue  library  search      g-  2/30  ⤮ ⟳  vol ▪▪▪▪▫ 80%"
// and records tab, button and volume positions.
func (m *Model) tabBar() string {
	st, ic := m.st, m.st.Opts.Icons
	var b strings.Builder
	x := 0
	put := func(s string) {
		b.WriteString(s)
		x += ansi.StringWidth(s)
	}
	put(" " + st.AppName.Render("ytm"))
	m.geom.tabs = m.geom.tabs[:0]
	for _, t := range tabDefs {
		active := t.page == m.tab
		if m.width < 80 && !active {
			continue
		}
		put("  ")
		s := st.Tab
		if active {
			s = st.TabActive
		}
		m.geom.tabs = append(m.geom.tabs, tabSpan{x: x, w: ansi.StringWidth(t.name), page: t.page})
		put(s.Render(t.name))
	}
	left := b.String()

	// Right side, built as segments so clickable ones get positions.
	type seg struct {
		s      string
		action string // "" = not a button
		vol    bool
	}
	var segs []seg
	if p := m.disp.Pending(); p != "" {
		segs = append(segs, seg{s: st.Pending.Render(p)})
	}
	if n := m.pb.q.Len(); n > 0 {
		pos := "–"
		if i := m.pb.q.CurrentIndex(); i >= 0 {
			pos = fmt.Sprint(i + 1)
		}
		segs = append(segs, seg{s: st.Muted.Render(pos + "/" + fmt.Sprint(n))})
	}
	flag := func(on bool, glyph string) string {
		if on {
			return st.BarIconActive.Render(glyph)
		}
		return st.BarIcon.Render(glyph)
	}
	rep := ic.Repeat
	if m.bar.repeat == "one" {
		rep += "1"
	}
	segs = append(segs,
		seg{s: flag(m.bar.shuffle, ic.Shuffle), action: "toggle_shuffle"},
		seg{s: flag(m.bar.repeat != "off" && m.bar.repeat != "", rep), action: "cycle_repeat"},
		seg{s: m.volume(), vol: true},
	)
	right, rw := "", 0
	for i, sg := range segs {
		if i > 0 {
			right += "  "
			rw += 2
		}
		right += sg.s
		rw += ansi.StringWidth(sg.s)
	}
	right += " "
	rw++

	m.geom.volumeW = 0
	gap := m.width - x - rw
	if gap < 1 {
		return fit(left, m.width)
	}
	at := x + gap
	for i, sg := range segs {
		if i > 0 {
			at += 2
		}
		w := ansi.StringWidth(sg.s)
		switch {
		case sg.action != "":
			m.geom.buttons = append(m.geom.buttons, barButton{x: at, y: 0, w: w, action: sg.action})
		case sg.vol:
			m.geom.volumeY, m.geom.volumeX, m.geom.volumeW = 0, at, w
		}
		at += w
	}
	return left + strings.Repeat(" ", gap) + right
}

// volume renders "vol ▪▪▪▪▫ 80%" (or "muted").
func (m *Model) volume() string {
	st := m.st
	if m.bar.st.Muted {
		return st.Muted.Render("vol muted")
	}
	v := min(max(m.bar.st.Volume, 0), 100)
	on := (v + 10) / 20
	return st.Muted.Render("vol ") + st.Accent.Render(strings.Repeat("▪", on)) +
		st.Border.Render(strings.Repeat("▪", 5-on)) + st.Muted.Render(fmt.Sprintf(" %d%%", v))
}

// tabRule renders the line under the tab bar, open under the active tab:
// "────╯ library ╰──────".
func (m *Model) tabRule() string {
	line := []rune(strings.Repeat("─", m.width))
	for _, t := range m.geom.tabs {
		if t.page != m.tab {
			continue
		}
		for i := t.x; i < t.x+t.w && i < len(line); i++ {
			line[i] = ' '
		}
		if t.x-1 >= 0 && t.x-1 < len(line) {
			line[t.x-1] = '╯'
		}
		if t.x+t.w < len(line) {
			line[t.x+t.w] = '╰'
		}
	}
	return m.st.Border.Render(string(line))
}

// miniPlayer renders the one-row player at screen row y:
// " ▌ ▶ Title — Artist        ♥  ──────●──────────  1:54 / 3:57 ".
func (m *Model) miniPlayer(y int) string {
	b, st, ic, width := m.bar, m.st, m.st.Opts.Icons, m.width
	m.geom.progressW = 0
	if b.st.Item.IsZero() {
		return fit(" "+st.Muted.Render("Nothing playing"), width)
	}
	title, _ := b.title()
	gutter := st.Muted.Render("▌")
	if b.st.Status == player.Playing {
		gutter = st.Accent.Render("▌")
	}
	icon := st.BarIconActive.Render(b.playIcon(ic))
	left := " " + gutter + " " + icon + " " + st.BarTitle.Render(title)
	if b.now.Artist != "" && b.now.VideoID == b.st.Item.ID {
		left += st.BarArtist.Render(" — " + b.now.Artist)
	}
	if b.st.Status == player.Error && b.st.Err != "" {
		left = " " + gutter + " " + st.ErrorText.Render(ic.Fail+" "+b.st.Err)
	}

	heart := st.BarIcon.Render(ic.Heart)
	if b.liked {
		heart = st.BarIconActive.Render(ic.Heart)
	}
	times := st.BarTime.Render(fmtDur(b.pos()) + " / " + fmtDur(b.st.Duration))
	pw := 0
	if width >= 60 {
		pw = width * 2 / 5
	}
	right := heart + "  "
	if pw > 0 {
		right += b.knobLine(pw, st) + "  "
	}
	right += times + " "
	rw := ansi.StringWidth(right)
	if rw >= width-10 { // too narrow: title and time only
		return fit(fit(left, width-ansi.StringWidth(times)-2)+" "+times+" ", width)
	}
	lw := width - rw
	hx := lw
	m.geom.buttons = append(m.geom.buttons,
		barButton{x: 3, y: y, w: ansi.StringWidth(icon), action: "play_pause"},
		barButton{x: hx, y: y, w: ansi.StringWidth(heart), action: "like_toggle"},
	)
	if pw > 0 {
		m.geom.progressY, m.geom.progressX, m.geom.progressW = y, hx+ansi.StringWidth(heart)+2, pw
	}
	return fit(left, lw) + right
}

// knobLine draws "─────●──────" with the knob at the play position.
func (b barState) knobLine(width int, u *style.UI) string {
	if width <= 0 {
		return ""
	}
	if b.st.Duration <= 0 {
		return u.ProgressEmpty.Render(strings.Repeat("─", width))
	}
	frac := min(max(float64(b.pos())/float64(b.st.Duration), 0), 1)
	k := int(frac * float64(width-1))
	return u.ProgressFilled.Render(strings.Repeat("─", k)+"●") + u.ProgressEmpty.Render(strings.Repeat("─", width-k-1))
}

// hint is one "key action" pair of the footer. Keys come from the live
// keymap (actions, joined with "/"), or key is a literal for raw input
// modes (a focused text field).
type hint struct {
	actions []string
	key     string
	label   string
}

// hk builds a hint from actions.
func hk(label string, actions ...string) hint { return hint{actions: actions, label: label} }

// hinter is a page with its own footer hints.
type hinter interface{ Hints() []hint }

var (
	defaultHints = []hint{hk("play", "play_pause"), hk("next", "next_track"), hk("prev", "previous_track"), hk("seek", "seek_forward")}
	sidebarHints = []hint{hk("open", "select"), hk("main", "focus_pane_right"), hk("hide", "toggle_sidebar"), hk("filter", "filter")}
)

// hintLine renders the footer: page hints joined by " · ", truncated from
// the right, and "? help" last.
func (m *Model) hintLine() string {
	st := m.st
	hs := defaultHints
	switch {
	case m.sideFocused():
		hs = sidebarHints
	default:
		if hp, ok := m.pages[m.current].(hinter); ok {
			hs = hp.Hints()
		}
	}
	help := ""
	if ks := m.o.Keymap.Bindings("help"); len(ks) > 0 {
		help = st.HintText.Render(ks[0] + " help")
	}
	room := m.width - 2 - ansi.StringWidth(help) - 3
	sep := st.Border.Render(" · ")
	var b strings.Builder
	used := 0
	for _, x := range hs {
		k := x.key
		if k == "" {
			var ks []string
			for _, a := range x.actions {
				if bs := m.o.Keymap.Bindings(a); len(bs) > 0 {
					ks = append(ks, bs[0])
				}
			}
			k = strings.Join(ks, "/")
		}
		if k == "" {
			continue
		}
		piece := st.HintKey.Render(k) + " " + st.HintText.Render(x.label)
		pw := ansi.StringWidth(piece)
		if used > 0 {
			pw += 3
		}
		if used+pw > room {
			break
		}
		if used > 0 {
			b.WriteString(sep)
		}
		b.WriteString(piece)
		used += pw
	}
	line := " " + b.String()
	if help != "" {
		line += "   " + help
	}
	return fit(line, m.width)
}

// sideTitle is the sidebar panel's title.
const sideTitle = "playlists"

// tabBar renders an in-page tab strip, " songs · albums · artists ", with
// the active tab marked (see tabHit).
func tabBar(names []string, active int, st *style.UI) string {
	var b strings.Builder
	for i, n := range names {
		if i > 0 {
			b.WriteString(st.Border.Render("·"))
		}
		b.WriteString(" ")
		if i == active {
			b.WriteString(st.TabActive.Render(strings.ToLower(n)))
		} else {
			b.WriteString(st.Tab.Render(strings.ToLower(n)))
		}
		b.WriteString(" ")
	}
	return b.String()
}

// splitter is a page drawn as two panels side by side (the track page).
// Page coordinates for clicks: the left panel's content starts at x 0,
// the right one's at x = Split(w) (the left panel's outer width).
type splitter interface {
	// Split returns the left panel's outer width for a main area w wide:
	// w = only the left panel, 0 = only the right one.
	Split(w int) int
	Panels() (left, right panelSpec)
	ViewLeft(w, h int, st *style.UI) string
	ViewRight(w, h int, st *style.UI) string
}

// progressor is a page that draws a progress line (page coordinates of
// the last View; w 0 = none), so clicks seek and the wheel scrubs.
type progressor interface{ Progress() (x, y, w int) }

// splitPanels draws a splitter's two panels into w×h.
func splitPanels(st *style.UI, sp splitter, w, h int) []string {
	lw := sp.Split(w)
	ls, rs := sp.Panels()
	switch lw {
	case w:
		return panel(st, ls, sp.ViewLeft(w-2, h-2, st), w, h, true)
	case 0:
		return panel(st, rs, sp.ViewRight(w-2, h-2, st), w, h, true)
	}
	left := panel(st, ls, sp.ViewLeft(lw-2, h-2, st), lw, h, true)
	right := panel(st, rs, sp.ViewRight(w-lw-2, h-2, st), w-lw, h, true)
	for i := range left {
		left[i] += right[i]
	}
	return left
}
