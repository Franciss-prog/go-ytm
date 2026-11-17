// Package ui is the Bubble Tea front end: the root model, layout, key
// dispatch, navigation, playback bar, toasts and pages (docs/specs/05-ui-pages.md).
//
// Update never blocks: player commands are non-blocking enqueues
// (player.Player), and every other I/O runs in a tea.Cmd.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/style"
)

// PageID names a page; values match [general] startup_page.
type PageID string

// Pages.
const (
	PageLibrary  PageID = "library"
	PageSearch   PageID = "search"
	PageBrowse   PageID = "browse"
	PageQueue    PageID = "queue"
	PageLiked    PageID = "liked_songs"
	PageRecent   PageID = "recently_played"
	PageHelp     PageID = "help"
	pageNotFound PageID = ""
)

// PageIDs lists every page in header order.
var PageIDs = []PageID{PageLibrary, PageSearch, PageBrowse, PageQueue, PageLiked, PageRecent, PageHelp}

// pageActions maps navigation actions to pages.
var pageActions = map[string]PageID{
	"library": PageLibrary, "search": PageSearch, "browse": PageBrowse, "queue": PageQueue,
	"liked_songs": PageLiked, "recently_played": PageRecent, "help": PageHelp,
}

// Title is the page's display name.
func (p PageID) Title() string {
	switch p {
	case PageLibrary:
		return "Library"
	case PageSearch:
		return "Search"
	case PageBrowse:
		return "Browse"
	case PageQueue:
		return "Queue"
	case PageLiked:
		return "Liked Songs"
	case PageRecent:
		return "Recently Played"
	case PageHelp:
		return "Help"
	}
	return string(p)
}

// Page is one screen in the main area. Implementations are values owned by
// the root model; they return themselves updated, like tea.Model.
type Page interface {
	ID() PageID
	// Action handles a resolved key action. handled=false lets the root
	// apply its global handler.
	Action(a keys.Action, ctx *Context) (p Page, cmd tea.Cmd, handled bool)
	// Capturing reports that the page owns raw key input (a text field is
	// focused); the root then sends tea.KeyPressMsg to Key instead of the
	// keymap.
	Capturing() bool
	// Key handles a raw key press while Capturing.
	Key(msg tea.KeyPressMsg, ctx *Context) (Page, tea.Cmd)
	// View renders exactly the given size.
	View(width, height int, st *style.UI) string
	// Info is the selection info line ("" for none).
	Info() string
}

// Context is what pages may read from the root while handling input.
type Context struct {
	Keymap *keys.Keymap
	// KeymapPath is shown on the Help page.
	KeymapPath string
	// Height is the main area's current height (for paging).
	Height int
}

// Toast returns a command that shows t.
func (*Context) Toast(t Toast) tea.Cmd { return msgCmd(t) }

// placeholder stands in for pages that later milestones build.
type placeholder struct {
	id   PageID
	note string
}

func (p placeholder) ID() PageID      { return p.id }
func (p placeholder) Capturing() bool { return false }
func (p placeholder) Info() string    { return "" }

func (p placeholder) Action(keys.Action, *Context) (Page, tea.Cmd, bool) { return p, nil, false }

func (p placeholder) Key(tea.KeyPressMsg, *Context) (Page, tea.Cmd) { return p, nil }

func (p placeholder) View(width, height int, st *style.UI) string {
	lines := []string{"", " " + st.Title.Render(p.id.Title()), "", " " + st.Muted.Render(p.note)}
	return block(lines, width, height)
}

// newPages builds the initial page set.
func newPages() map[PageID]Page {
	const later = "This page is not built yet."
	return map[PageID]Page{
		PageLibrary: placeholder{PageLibrary, later},
		PageSearch:  placeholder{PageSearch, later + " Try `ytm search` in a shell meanwhile."},
		PageBrowse:  placeholder{PageBrowse, later},
		PageQueue:   placeholder{PageQueue, later},
		PageLiked:   placeholder{PageLiked, later},
		PageRecent:  placeholder{PageRecent, later},
		PageHelp:    newHelp(),
	}
}
