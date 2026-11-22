// Package ui is the Bubble Tea front end: the root model, layout, key
// dispatch, navigation, playback bar, toasts and pages (docs/specs/05-ui-pages.md).
//
// Update never blocks: player commands are non-blocking enqueues
// (player.Player), and every other I/O runs in a tea.Cmd.
package ui

import (
	"context"
	"errors"
	"log/slog"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/ui/components"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
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
var PageIDs = []PageID{PageLibrary, PageSearch, PageBrowse, PageQueue, PageTrack, PageLiked, PageRecent, PageHelp}

// pageActions maps navigation actions to pages.
var pageActions = map[string]PageID{
	"library": PageLibrary, "search": PageSearch, "browse": PageBrowse, "queue": PageQueue,
	"liked_songs": PageLiked, "recently_played": PageRecent, "help": PageHelp, "track_page": PageTrack,
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
	case PageTrack:
		return "Track"
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

	// API is the YouTube Music client (nil: unavailable).
	API ytmusic.Client
	// SignedIn reports whether API carries a saved session.
	SignedIn bool
	// Root ends with the program; async loads derive from it.
	Root context.Context
	// Columns are the TrackTable widths from [ui] col_*.
	Columns components.Columns
	// Styles are the current styles (mouse hit-testing).
	Styles *style.UI
	// Log is the debug log.
	Log *slog.Logger
	// Local is the on-disk history (nil: none).
	Local Local
	// ShuffleLocks are the per-playlist shuffle locks (nil: none).
	ShuffleLocks ShuffleLocks
}

// Load runs f in a command and delivers its result to page p as a
// Loaded message (dropped if p has left the history meanwhile).
func (c *Context) Load(p Page, key string, f func(ctx context.Context, api ytmusic.Client) (any, error)) tea.Cmd {
	api, root := c.API, c.Root
	return func() tea.Msg {
		if api == nil {
			return pageMsg{to: p, msg: Loaded{Key: key, Err: errNoAPI}}
		}
		v, err := f(root, api)
		return pageMsg{to: p, msg: Loaded{Key: key, Value: v, Err: err}}
	}
}

// Loaded is an async result for a page; Key says which request it answers.
type Loaded struct {
	Key   string
	Value any
	Err   error
}

var errNoAPI = errors.New("YouTube Music client unavailable")

// loadErrorText is the short message shown for a failed load.
func loadErrorText(err error) string {
	switch {
	case errors.Is(err, ytmusic.ErrUnauthenticated):
		return "Not signed in. Run `ytm setup` to see your library."
	case errors.Is(err, ytmusic.ErrRateLimited):
		return "YouTube Music is rate limiting; press r to retry later."
	case errors.Is(err, context.Canceled):
		return ""
	case errors.Is(err, errNoAPI):
		return "YouTube Music is unavailable."
	}
	return "Couldn't load from YouTube Music; press r to retry. Details are in the log."
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
