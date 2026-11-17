package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/style"
)

// help lists every action with its live bindings, grouped by category and
// filterable with "/". Rebinds in keymap.toml show up here.
type help struct {
	top       int
	filter    string
	filtering bool
	keymap    *keys.Keymap // set on first use
	path      string
}

func newHelp() *help { return &help{} }

func (h *help) ID() PageID      { return PageHelp }
func (h *help) Capturing() bool { return h.filtering }

func (h *help) Info() string {
	if h.filter != "" {
		return fmt.Sprintf("filter: %s", h.filter)
	}
	return ""
}

func (h *help) bind(ctx *Context) {
	h.keymap, h.path = ctx.Keymap, ctx.KeymapPath
}

func (h *help) Action(a keys.Action, ctx *Context) (Page, tea.Cmd, bool) {
	h.bind(ctx)
	page := max(ctx.Height-1, 1)
	switch a.Name {
	case "move_down":
		h.top += a.N()
	case "move_up":
		h.top -= a.N()
	case "page_down":
		h.top += page * a.N()
	case "page_up":
		h.top -= page * a.N()
	case "go_top":
		h.top = 0
	case "go_bottom":
		h.top = 1 << 30
	case "filter":
		h.filtering = true
	case "mark_clear":
		if h.filter == "" {
			return h, nil, false
		}
		h.filter, h.top = "", 0
	default:
		return h, nil, false
	}
	h.top = max(h.top, 0) // upper bound applied in View
	return h, nil, true
}

func (h *help) Key(msg tea.KeyPressMsg, ctx *Context) (Page, tea.Cmd) {
	h.bind(ctx)
	k := msg.Key()
	switch k.Code {
	case tea.KeyEscape:
		h.filtering, h.filter = false, ""
	case tea.KeyEnter:
		h.filtering = false
	case tea.KeyBackspace:
		if r := []rune(h.filter); len(r) > 0 {
			h.filter = string(r[:len(r)-1])
		}
	default:
		if k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			h.filter += k.Text
		}
	}
	h.top = 0
	return h, nil
}

// lines renders the full (unscrolled) page.
func (h *help) lines(st *style.UI) []string {
	km := h.keymap
	if km == nil {
		km = keys.Default()
	}
	q := strings.ToLower(h.filter)
	var out []string
	out = append(out, " "+st.Title.Render("Keys"))
	if h.filter == "" {
		out = append(out,
			" "+st.Muted.Render("Chords are typed in sequence (g then l). Digits before a motion repeat it (15j)."),
			" "+st.Muted.Render("Rebind in ")+st.Accent.Render(h.keymapPath())+st.Muted.Render(" — this page follows your bindings."),
		)
	}
	for _, cat := range keys.Categories {
		var rows []string
		for _, a := range keys.Actions {
			if a.Category != cat {
				continue
			}
			b := km.Bindings(a.Name)
			bound := strings.Join(b, ", ")
			if bound == "" {
				bound = "(unbound)"
			}
			if q != "" && !strings.Contains(strings.ToLower(a.Help+" "+a.Name+" "+bound), q) {
				continue
			}
			rows = append(rows, "   "+st.HelpKey.Render(fit(bound, 16))+" "+st.HelpDesc.Render(a.Help))
		}
		if len(rows) == 0 {
			continue
		}
		out = append(out, "", " "+st.Section.Render(string(cat)))
		out = append(out, rows...)
	}
	if q != "" && len(out) == 1 {
		out = append(out, "", " "+st.Muted.Render("No action matches “"+h.filter+"”."))
	}
	return out
}

func (h *help) keymapPath() string {
	if h.path == "" {
		return "keymap.toml"
	}
	return h.path
}

func (h *help) View(width, height int, st *style.UI) string {
	all := h.lines(st)
	body := height
	if h.filtering || h.filter != "" {
		body--
	}
	maxTop := max(len(all)-body, 0)
	h.top = min(h.top, maxTop)
	lines := all[h.top:]
	if len(lines) > body {
		lines = lines[:body]
	}
	if body < height {
		cursor := ""
		if h.filtering {
			cursor = "█"
		}
		lines = append(append([]string{}, lines...), make([]string, body-len(lines))...)
		lines = append(lines, " "+st.Accent.Render("/")+st.Text.Render(h.filter+cursor))
	}
	return block(lines, width, height)
}
