package ui

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/ui/components"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// Popup size limits (spec 05: centered, max 60 × 20, border included).
const (
	popupMaxW = 60
	popupMaxH = 20
	popupMinW = 28
)

// popup is a modal box over the main area. It owns raw key input while
// open; returning a nil popup closes it, another value replaces it (a
// flow moves from picker to input to confirm without a stack).
type popup interface {
	Key(msg tea.KeyPressMsg, ctx *Context) (popup, tea.Cmd)
	// View returns the title and body lines for an inner width of w
	// cells and at most h body lines.
	View(w, h int, st *style.UI) (title string, body []string)
}

// popupReceiver is a popup that takes async results (popupMsg).
type popupReceiver interface {
	Receive(msg tea.Msg, ctx *Context) (popup, tea.Cmd)
}

// openPopupMsg opens p, replacing any open popup.
type openPopupMsg struct{ p popup }

// popupMsg carries an async result to popup to; dropped when another
// popup (or none) is open by then.
type popupMsg struct {
	to  popup
	msg tea.Msg
}

// actionMsg runs a key action on the current page, as if typed (actions
// popup items reuse the pages' own handling).
type actionMsg struct{ a keys.Action }

// editDoneMsg reports an account write. ok runs on success (in Update, so
// it may change pages); failed, when set, replaces the error toast.
type editDoneMsg struct {
	what   string // "add to “Focus”": completes "Couldn't …"
	err    error
	ok     func(m *Model) tea.Cmd
	failed func(m *Model, err error) tea.Cmd
}

// edit runs an account write off the Update loop.
func edit(
	ctx *Context, what string, f func(c context.Context, api ytmusic.Client) error,
	ok func(m *Model) tea.Cmd, failed func(m *Model, err error) tea.Cmd,
) tea.Cmd {
	api, root := ctx.API, ctx.Root
	return func() tea.Msg {
		err := errNoAPI
		if api != nil {
			err = f(root, api)
		}
		return editDoneMsg{what: what, err: err, ok: ok, failed: failed}
	}
}

func (m *Model) editDone(msg editDoneMsg) tea.Cmd {
	if msg.err == nil {
		if msg.ok != nil {
			return msg.ok(m)
		}
		return nil
	}
	m.log.Error("ui: edit failed", "what", msg.what, "err", msg.err)
	if msg.failed != nil {
		return msg.failed(m, msg.err)
	}
	return m.toasts.push(Toast{Severity: SevError, Text: "Couldn't " + msg.what + ": " + editErrorText(msg.err)})
}

// editErrorText is the short reason shown for a failed write.
func editErrorText(err error) string {
	switch {
	case errors.Is(err, ytmusic.ErrUnauthenticated):
		return "not signed in (run `ytm setup`)"
	case errors.Is(err, ytmusic.ErrRateLimited):
		return "rate limited, try again later"
	case errors.Is(err, ytmusic.ErrEditFailed):
		return "YouTube Music refused it"
	case errors.Is(err, errNoAPI):
		return "YouTube Music is unavailable"
	}
	return "see the log"
}

func (m *Model) popupKey(msg tea.KeyPressMsg) tea.Cmd {
	p, cmd := m.popup.Key(msg, m.ctx())
	m.popup = p
	return cmd
}

func (m *Model) deliverPopup(pm popupMsg) tea.Cmd {
	if m.popup == nil || m.popup != pm.to {
		return nil
	}
	r, ok := m.popup.(popupReceiver)
	if !ok {
		return nil
	}
	p, cmd := r.Receive(pm.msg, m.ctx())
	m.popup = p
	return cmd
}

// overlayPopup draws the open popup centered over the main rows.
func (m *Model) overlayPopup(rows []string, width int, st *style.UI) {
	// Border (2) and padding (2) around the inner width.
	maxW := min(popupMaxW, width) - 4
	maxH := min(popupMaxH, len(rows)) - 2
	if maxW < 4 || maxH < 1 {
		return
	}
	title, body := m.popup.View(maxW, max(maxH-2, 0), st)
	lines := []string{st.PopupTitle.Render(title), ""}
	if title == "" {
		lines = nil
	}
	lines = append(lines, body...)
	lines = lines[:min(len(lines), maxH)]
	w := popupMinW - 4
	for _, l := range lines {
		w = max(w, ansi.StringWidth(l))
	}
	w = min(w, maxW)
	for i, l := range lines {
		lines[i] = fit(l, w)
	}
	overlayBox(rows, width, st.Popup.Render(strings.Join(lines, "\n")))
}

// overlayBox draws a rendered box centered over rows.
func overlayBox(rows []string, width int, box string) {
	lines := strings.Split(box, "\n")
	bw := 0
	for _, l := range lines {
		bw = max(bw, ansi.StringWidth(l))
	}
	bw = min(bw, width)
	x := max((width-bw)/2, 0)
	y := max((len(rows)-len(lines))/2, 0)
	for i, l := range lines {
		if y+i >= len(rows) {
			break
		}
		rows[y+i] = overlayAt(rows[y+i], l, x, bw, width)
	}
}

// menuItem is one row of a menuPopup.
type menuItem struct {
	label string
	right string // muted detail (track count)
	run   func(ctx *Context) (popup, tea.Cmd)
}

// menuPopup is a list to pick from: j/k or arrows move, enter runs the
// item, esc closes. With filterable set, typing filters the items
// (case-insensitive substring, as the original's playlist picker) and
// only arrows move.
type menuPopup struct {
	title      string
	items      []menuItem
	fixed      int // leading items the filter never hides ("+ New playlist")
	filterable bool
	filter     []rune
	view       []int
	cursor     int
	top        int
	status     string // a line under the list ("Loading…", errors)
	busy       bool   // a request is running: only esc works
	hint       string
}

func newMenu(title string, items []menuItem) *menuPopup {
	p := &menuPopup{title: title, items: items, hint: "enter select · esc close"}
	p.rebuild()
	return p
}

func (p *menuPopup) setItems(items []menuItem) {
	p.items = items
	p.rebuild()
}

func (p *menuPopup) rebuild() {
	q := strings.ToLower(string(p.filter))
	p.view = p.view[:0]
	for i, it := range p.items {
		if i < p.fixed || q == "" || strings.Contains(strings.ToLower(it.label), q) {
			p.view = append(p.view, i)
		}
	}
	p.cursor = max(min(p.cursor, len(p.view)-1), 0)
}

func (p *menuPopup) move(d int) {
	if len(p.view) > 0 {
		p.cursor = (p.cursor + d + len(p.view)) % len(p.view)
	}
}

func (p *menuPopup) Key(msg tea.KeyPressMsg, ctx *Context) (popup, tea.Cmd) {
	k := msg.Key()
	if k.Code == tea.KeyEscape {
		if p.filterable && len(p.filter) > 0 && !p.busy {
			p.filter = nil
			p.rebuild()
			return p, nil
		}
		return nil, nil
	}
	if p.busy {
		return p, nil
	}
	switch msg.String() {
	case "down", "ctrl+n", "tab":
		p.move(1)
		return p, nil
	case "up", "ctrl+p", "shift+tab":
		p.move(-1)
		return p, nil
	case "enter":
		if len(p.view) == 0 {
			return p, nil
		}
		return p.items[p.view[p.cursor]].run(ctx)
	}
	if !p.filterable {
		switch msg.String() {
		case "j":
			p.move(1)
		case "k":
			p.move(-1)
		case "q":
			return nil, nil
		}
		return p, nil
	}
	switch {
	case k.Code == tea.KeyBackspace:
		if len(p.filter) > 0 {
			p.filter = p.filter[:len(p.filter)-1]
			p.rebuild()
		}
	case k.Mod&tea.ModCtrl != 0 && k.Code == 'u':
		p.filter = nil
		p.rebuild()
	case k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
		p.filter = append(p.filter, []rune(k.Text)...)
		p.rebuild()
	}
	return p, nil
}

func (p *menuPopup) View(w, h int, st *style.UI) (string, []string) {
	var head, foot []string
	if p.filterable {
		f := st.Muted.Render("type to filter")
		if len(p.filter) > 0 {
			f = st.Text.Render(string(p.filter) + "█")
		}
		head = append(head, st.Section.Render("/ ")+f, "")
	}
	if p.status != "" {
		foot = append(foot, "", st.Muted.Render(p.status))
	}
	if p.hint != "" {
		foot = append(foot, "", st.Muted.Render(p.hint))
	}
	listH := max(h-len(head)-len(foot), 1)
	if p.cursor < p.top {
		p.top = p.cursor
	}
	if p.cursor >= p.top+listH {
		p.top = p.cursor - listH + 1
	}
	p.top = max(min(p.top, len(p.view)-listH), 0)
	body := head
	gutterW := ansi.StringWidth(st.SelectedGutter)
	// Rows are as wide as the widest item, so short menus stay narrow.
	natural := 0
	for _, it := range p.items {
		n := gutterW + 1 + ansi.StringWidth(it.label)
		if it.right != "" {
			n += 2 + ansi.StringWidth(it.right)
		}
		natural = max(natural, n)
	}
	w = min(w, max(natural, popupMinW-4))
	for v := p.top; v < len(p.view) && v < p.top+listH; v++ {
		it := p.items[p.view[v]]
		right := ""
		if it.right != "" {
			right = "  " + it.right
		}
		labelW := max(w-gutterW-ansi.StringWidth(right)-1, 1)
		text := " " + components.Fit(it.label, labelW)
		if v == p.cursor {
			body = append(body, st.SelectedGutter+st.Selected.Render(text+right))
		} else {
			body = append(body, strings.Repeat(" ", gutterW)+st.Text.Render(text)+st.Muted.Render(right))
		}
	}
	if len(p.view) == 0 {
		body = append(body, st.Muted.Render(" No matches."))
	}
	return p.title, append(body, foot...)
}

// confirmPopup asks a yes/no question: y or enter confirms, n or esc
// cancels.
type confirmPopup struct {
	title, text, yes string
	onYes            func(ctx *Context) (popup, tea.Cmd)
	onNo             func(ctx *Context) (popup, tea.Cmd) // nil: just close
}

func (p *confirmPopup) Key(msg tea.KeyPressMsg, ctx *Context) (popup, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		return p.onYes(ctx)
	case "n", "N", "esc", "q":
		if p.onNo != nil {
			return p.onNo(ctx)
		}
		return nil, nil
	}
	return p, nil
}

func (p *confirmPopup) View(w, _ int, st *style.UI) (string, []string) {
	var body []string
	for _, l := range strings.Split(p.text, "\n") {
		body = append(body, st.Text.Render(strings.TrimRight(components.Fit(l, w), " ")))
	}
	return p.title, append(body, "", st.Accent.Render("y/enter")+st.Muted.Render(" "+p.yes+" · ")+
		st.Accent.Render("n/esc")+st.Muted.Render(" cancel"))
}

// privacyChoices are the create-playlist privacy options (as the
// original's CreatePlaylistPopup: private first, the default).
var privacyChoices = []string{ytmusic.PrivacyPrivate, ytmusic.PrivacyPublic, ytmusic.PrivacyUnlisted}

// createPopup asks for a new playlist's name and privacy.
type createPopup struct {
	name    []rune
	privacy int
	focus   int // 0 name, 1 privacy
	err     string
	submit  func(ctx *Context, name, privacy string) (popup, tea.Cmd)
}

func (p *createPopup) Key(msg tea.KeyPressMsg, ctx *Context) (popup, tea.Cmd) {
	k := msg.Key()
	switch k.Code {
	case tea.KeyEscape:
		return nil, nil
	case tea.KeyTab, tea.KeyUp, tea.KeyDown:
		p.focus = 1 - p.focus
		return p, nil
	case tea.KeyEnter:
		name := strings.TrimSpace(string(p.name))
		switch {
		case name == "":
			p.err = "Enter a name."
			return p, nil
		case strings.ContainsAny(name, "<>"):
			// YouTube Music rejects these (ytmusicapi create_playlist).
			p.err = "The name can't contain < or >."
			return p, nil
		}
		return p.submit(ctx, name, privacyChoices[p.privacy])
	}
	if p.focus == 1 {
		switch k.Code {
		case tea.KeyLeft, 'h':
			p.privacy = (p.privacy + len(privacyChoices) - 1) % len(privacyChoices)
		case tea.KeyRight, 'l', tea.KeySpace:
			p.privacy = (p.privacy + 1) % len(privacyChoices)
		}
		return p, nil
	}
	p.err = ""
	switch {
	case k.Code == tea.KeyBackspace:
		if len(p.name) > 0 {
			p.name = p.name[:len(p.name)-1]
		}
	case k.Mod&tea.ModCtrl != 0 && k.Code == 'u':
		p.name = nil
	case k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
		p.name = append(p.name, []rune(k.Text)...)
	}
	return p, nil
}

func (p *createPopup) View(w, _ int, st *style.UI) (string, []string) {
	label := func(s string, on bool) string {
		if on {
			return st.Section.Render(s)
		}
		return st.Muted.Render(s)
	}
	cur := ""
	if p.focus == 0 {
		cur = "█"
	}
	name := string(p.name) + cur
	if n := ansi.StringWidth(name); n > w-10 {
		name = ansi.TruncateLeft(name, n-(w-10), "…")
	}
	priv := strings.ToLower(privacyChoices[p.privacy])
	if p.focus == 1 {
		priv = "‹ " + priv + " ›"
	}
	body := []string{
		label("Name     ", p.focus == 0) + st.Text.Render(name),
		label("Privacy  ", p.focus == 1) + st.Text.Render(priv),
	}
	if p.err != "" {
		body = append(body, "", st.ErrorText.Render(p.err))
	}
	return "New playlist", append(body, "", st.Muted.Render("tab switch · ←/→ privacy · enter create · esc cancel"))
}
