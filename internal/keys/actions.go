package keys

// Category groups actions on the Help page.
type Category string

// Categories, in Help page order.
const (
	CatPlayback   Category = "Playback"
	CatNavigation Category = "Navigation"
	CatPages      Category = "Pages"
	CatLists      Category = "Lists & queue"
	CatApp        Category = "App"
)

// Categories lists all categories in display order.
var Categories = []Category{CatPlayback, CatNavigation, CatPages, CatLists, CatApp}

// ActionInfo describes one bindable action.
type ActionInfo struct {
	Name     string
	Category Category
	Help     string
	Defaults []string // bindings, each a space-separated key sequence
	// Counted actions take a count prefix ("15J"); others ignore it.
	Counted bool
}

// Actions is every action with its default bindings: the original
// ytm-player keymap plus the approved additions settings, toggle_transparency,
// command_palette and theme_picker (spec 04). Order is Help page order.
var Actions = []ActionInfo{
	{Name: "play_pause", Category: CatPlayback, Help: "Play / pause", Defaults: []string{"space"}},
	{Name: "next_track", Category: CatPlayback, Help: "Next track", Defaults: []string{"n"}},
	{Name: "previous_track", Category: CatPlayback, Help: "Previous track", Defaults: []string{"p"}},
	{Name: "play_random", Category: CatPlayback, Help: "Play a random track from the queue", Defaults: []string{"."}},
	{Name: "cycle_repeat", Category: CatPlayback, Help: "Cycle repeat (off, all, one)", Defaults: []string{"C-r"}},
	{Name: "toggle_shuffle", Category: CatPlayback, Help: "Toggle shuffle", Defaults: []string{"C-s"}},
	{Name: "volume_up", Category: CatPlayback, Help: "Volume up", Defaults: []string{"+"}, Counted: true},
	{Name: "volume_down", Category: CatPlayback, Help: "Volume down", Defaults: []string{"-"}, Counted: true},
	{Name: "mute", Category: CatPlayback, Help: "Mute", Defaults: []string{"_"}},
	{Name: "seek_forward", Category: CatPlayback, Help: "Seek forward", Defaults: []string{">"}, Counted: true},
	{Name: "seek_backward", Category: CatPlayback, Help: "Seek backward", Defaults: []string{"<"}, Counted: true},
	{Name: "seek_start", Category: CatPlayback, Help: "Restart track", Defaults: []string{"^"}},
	{Name: "like_toggle", Category: CatPlayback, Help: "Like / unlike current track", Defaults: []string{"l"}},
	{Name: "discovery_mix", Category: CatPlayback, Help: "Discovery mix", Defaults: []string{"D"}},

	{Name: "move_down", Category: CatNavigation, Help: "Move down", Defaults: []string{"j", "down", "C-n"}, Counted: true},
	{Name: "move_up", Category: CatNavigation, Help: "Move up", Defaults: []string{"k", "up", "C-p"}, Counted: true},
	{Name: "page_down", Category: CatNavigation, Help: "Page down", Defaults: []string{"C-f", "page_down"}, Counted: true},
	{Name: "page_up", Category: CatNavigation, Help: "Page up", Defaults: []string{"C-b", "page_up"}, Counted: true},
	{Name: "go_top", Category: CatNavigation, Help: "Go to top (or row N with a count)", Defaults: []string{"g g", "home"}, Counted: true},
	{Name: "go_bottom", Category: CatNavigation, Help: "Go to bottom (or row N with a count)", Defaults: []string{"G", "end"}, Counted: true},
	{Name: "select", Category: CatNavigation, Help: "Select / open", Defaults: []string{"enter"}},
	{Name: "focus_next", Category: CatNavigation, Help: "Focus next panel", Defaults: []string{"tab"}},
	{Name: "focus_prev", Category: CatNavigation, Help: "Focus previous panel", Defaults: []string{"S-tab"}},
	{Name: "focus_pane_left", Category: CatNavigation, Help: "Focus pane to the left", Defaults: []string{"C-w h"}},
	{Name: "focus_pane_right", Category: CatNavigation, Help: "Focus pane to the right", Defaults: []string{"C-w l"}},
	{Name: "focus_pane_cycle", Category: CatNavigation, Help: "Cycle pane focus", Defaults: []string{"C-w w"}},
	{Name: "retry", Category: CatNavigation, Help: "Retry a failed load", Defaults: []string{"r"}},
	{Name: "go_back", Category: CatNavigation, Help: "Back", Defaults: []string{"backspace"}},
	{Name: "go_forward", Category: CatNavigation, Help: "Forward", Defaults: []string{"S-backspace"}},

	{Name: "library", Category: CatPages, Help: "Library", Defaults: []string{"g l"}},
	{Name: "search", Category: CatPages, Help: "Search", Defaults: []string{"g s"}},
	{Name: "browse", Category: CatPages, Help: "Browse", Defaults: []string{"g b"}},
	{Name: "liked_songs", Category: CatPages, Help: "Liked songs", Defaults: []string{"g y"}},
	{Name: "recently_played", Category: CatPages, Help: "Recently played", Defaults: []string{"g r"}},
	{Name: "queue", Category: CatPages, Help: "Queue", Defaults: []string{"z"}},
	{Name: "lyrics", Category: CatPages, Help: "Toggle lyrics sidebar", Defaults: []string{"g L"}},
	{Name: "current_context", Category: CatPages, Help: "Open what is playing (album / playlist)", Defaults: []string{"g space"}},
	{Name: "jump_to_current", Category: CatPages, Help: "Jump to the playing track", Defaults: []string{"g c"}},
	{Name: "help", Category: CatPages, Help: "Help", Defaults: []string{"?", "C-h"}},

	{Name: "play_next", Category: CatLists, Help: "Play next", Defaults: []string{"X", "C-x"}},
	{Name: "add_to_queue", Category: CatLists, Help: "Add to queue", Defaults: []string{"Z", "C-z"}},
	{Name: "add_to_playlist", Category: CatLists, Help: "Add to playlist", Defaults: []string{"A"}},
	{Name: "delete_item", Category: CatLists, Help: "Remove item", Defaults: []string{"delete", "d d"}},
	{Name: "track_actions", Category: CatLists, Help: "Track actions", Defaults: []string{"a"}},
	{Name: "mark_toggle", Category: CatLists, Help: "Mark / unmark", Defaults: []string{"v"}},
	{Name: "mark_range", Category: CatLists, Help: "Mark range", Defaults: []string{"V"}},
	{Name: "mark_clear", Category: CatLists, Help: "Clear marks", Defaults: []string{"escape"}},
	{Name: "reorder_down", Category: CatLists, Help: "Move item down", Defaults: []string{"J"}, Counted: true},
	{Name: "reorder_up", Category: CatLists, Help: "Move item up", Defaults: []string{"K"}, Counted: true},
	{Name: "filter", Category: CatLists, Help: "Filter", Defaults: []string{"/"}},
	{Name: "sort_title", Category: CatLists, Help: "Sort by title", Defaults: []string{"s t"}},
	{Name: "sort_artist", Category: CatLists, Help: "Sort by artist", Defaults: []string{"s a"}},
	{Name: "sort_album", Category: CatLists, Help: "Sort by album", Defaults: []string{"s A"}},
	{Name: "sort_duration", Category: CatLists, Help: "Sort by duration", Defaults: []string{"s d"}},
	{Name: "sort_date", Category: CatLists, Help: "Sort by date added", Defaults: []string{"s D"}},
	{Name: "reverse_sort", Category: CatLists, Help: "Reverse sort", Defaults: []string{"s r"}},
	{Name: "pick_country", Category: CatLists, Help: "Pick chart region", Defaults: []string{"c"}},
	{Name: "toggle_search_mode", Category: CatLists, Help: "Toggle search mode (music / all)"},

	{Name: "toggle_sidebar", Category: CatApp, Help: "Toggle playlist sidebar", Defaults: []string{"C-e"}},
	{Name: "toggle_album_art", Category: CatApp, Help: "Toggle album art", Defaults: []string{"C-a"}},
	{Name: "toggle_transliteration", Category: CatApp, Help: "Toggle lyrics transliteration", Defaults: []string{"T"}},
	{Name: "settings", Category: CatApp, Help: "Settings", Defaults: []string{","}},
	{Name: "toggle_transparency", Category: CatApp, Help: "Toggle transparent background", Defaults: []string{"C-t"}},
	{Name: "command_palette", Category: CatApp, Help: "Command palette", Defaults: []string{":"}},
	{Name: "theme_picker", Category: CatApp, Help: "Pick a theme", Defaults: []string{"g t"}},
	{Name: "quit", Category: CatApp, Help: "Quit", Defaults: []string{"q", "C-q"}},
}

var actionIndex = func() map[string]ActionInfo {
	m := make(map[string]ActionInfo, len(Actions))
	for _, a := range Actions {
		m[a.Name] = a
	}
	return m
}()

// Lookup returns an action's description.
func Lookup(name string) (ActionInfo, bool) {
	a, ok := actionIndex[name]
	return a, ok
}

// ActionNames lists every action name.
func ActionNames() []string {
	out := make([]string, len(Actions))
	for i, a := range Actions {
		out[i] = a.Name
	}
	return out
}
