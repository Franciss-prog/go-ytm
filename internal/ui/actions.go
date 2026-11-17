package ui

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/ui/components"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// Track and playlist actions (spec 05 "Popups"): the actions popup, the
// playlist picker with create-new and the duplicate prompt, removing rows
// from an owned playlist and deleting a playlist. Flows and wording follow
// ytm-player's popups/actions.py and popups/playlist_picker.py.

const musicURL = "https://music.youtube.com/"

// trackSource describes where a TrackTable's rows come from.
type trackSource struct {
	queue    string // queue context id for PlayMsg ("" none)
	playlist string // owned playlist id: rows can be removed from it
	name     string // that playlist's title, for messages
}

// trackAction handles the actions every track list shares. It reports
// whether a was one of them.
func trackAction(t *components.TrackTable, a keys.Action, src trackSource) (tea.Cmd, bool) {
	switch a.Name {
	case "select":
		rows, cur := t.Ordered()
		if len(rows) == 0 {
			return nil, true
		}
		return msgCmd(PlayMsg{Tracks: rows, Start: cur, Context: src.queue}), true
	case "play_next", "add_to_queue":
		ts := t.TargetTracks()
		if len(ts) == 0 {
			return nil, true
		}
		t.ClearMarks()
		return msgCmd(EnqueueMsg{Tracks: ts, Next: a.Name == "play_next"}), true
	case "track_actions":
		if p := trackActionsPopup(t, src, false); p != nil {
			return msgCmd(openPopupMsg{p}), true
		}
		return nil, true
	case "add_to_playlist":
		ts := t.TargetTracks()
		if len(ts) == 0 {
			return nil, true
		}
		return func() tea.Msg { return openPickerMsg{tracks: ts, table: t} }, true
	case "delete_item":
		if src.playlist == "" {
			return nil, false
		}
		return msgCmd(openPopupMsg{removeConfirm(t, src)}), true
	}
	return nil, false
}

// pageAction is a menu item that runs a key action on the current page.
func pageAction(name string) func(*Context) (popup, tea.Cmd) {
	return func(*Context) (popup, tea.Cmd) {
		return nil, msgCmd(actionMsg{keys.Action{Name: name}})
	}
}

// trackActionsPopup lists what can be done with the table's target rows
// (the marked rows, or the cursor row). inQueue swaps queueing for
// removal from the queue. nil when there is no row.
func trackActionsPopup(t *components.TrackTable, src trackSource, inQueue bool) popup {
	ts := t.TargetTracks()
	if len(ts) == 0 {
		return nil
	}
	one := len(ts) == 1
	title := plural(len(ts), "track")
	if one {
		title = ts[0].Title
	}
	var items []menuItem
	add := func(label string, run func(*Context) (popup, tea.Cmd)) {
		items = append(items, menuItem{label: label, run: run})
	}
	switch {
	case one:
		add("Play", pageAction("select"))
	case !inQueue:
		add("Play", func(*Context) (popup, tea.Cmd) {
			t.ClearMarks()
			return nil, msgCmd(PlayMsg{Tracks: ts, Context: src.queue})
		})
	}
	if inQueue {
		add("Remove from queue", pageAction("delete_item"))
	} else {
		add("Play next", pageAction("play_next"))
		add("Add to queue", pageAction("add_to_queue"))
	}
	if one {
		tr := ts[0]
		for _, ar := range tr.Artists {
			if ar.ID != "" {
				add("Go to artist", func(ctx *Context) (popup, tea.Cmd) {
					return nil, msgCmd(openMsg{newArtistPage(ar.ID, ar.Name, ctx.Columns)})
				})
				break
			}
		}
		if tr.Album != nil && tr.Album.ID != "" {
			add("Go to album", func(ctx *Context) (popup, tea.Cmd) {
				return nil, msgCmd(openMsg{newAlbumPage(tr.Album.ID, tr.Album.Name, ctx.Columns)})
			})
		}
	}
	add("Add to playlist", pageAction("add_to_playlist"))
	if src.playlist != "" && !inQueue {
		add("Remove from playlist", pageAction("delete_item"))
	}
	if one {
		tr := ts[0]
		label, next, done := "Like", ytmusic.Like, "Added to your likes"
		if tr.LikeStatus == ytmusic.Like {
			label, next, done = "Unlike", ytmusic.LikeIndifferent, "Removed from your likes"
		}
		add(label, func(ctx *Context) (popup, tea.Cmd) {
			return nil, edit(ctx, strings.ToLower(label)+" “"+tr.Title+"”",
				func(c context.Context, api ytmusic.Client) error { return api.Rate(c, tr.VideoID, next) },
				func(m *Model) tea.Cmd {
					t.SetLikeStatus(tr.VideoID, next)
					return m.toasts.push(Toast{Severity: SevSuccess, Text: done})
				}, nil)
		})
		add("Copy link", copyLink(musicURL+"watch?v="+tr.VideoID))
	}
	return newMenu(title, items)
}

// copyLink sets the clipboard (OSC 52; terminals without it ignore the
// sequence, so the toast says what was copied).
func copyLink(url string) func(*Context) (popup, tea.Cmd) {
	return func(*Context) (popup, tea.Cmd) {
		return nil, tea.Batch(tea.SetClipboard(url), msgCmd(Toast{Severity: SevInfo, Text: "Copied " + url}))
	}
}

// removeConfirm asks before removing the target rows from an owned
// playlist, then removes them from the table once YouTube Music agrees.
func removeConfirm(t *components.TrackTable, src trackSource) popup {
	var ts []ytmusic.Track
	for _, tr := range t.TargetTracks() {
		if tr.SetVideoID != "" {
			ts = append(ts, tr)
		}
	}
	if len(ts) == 0 {
		return newMenu("Remove from playlist", nil).withStatus("These rows can't be removed from this playlist.")
	}
	what := "“" + ts[0].Title + "”"
	if len(ts) > 1 {
		what = plural(len(ts), "track")
	}
	gone := make(map[string]bool, len(ts))
	for _, tr := range ts {
		gone[tr.SetVideoID] = true
	}
	id, name := src.playlist, src.name
	return &confirmPopup{
		title: "Remove from playlist", yes: "remove",
		text: "Remove " + what + " from “" + name + "”?",
		onYes: func(ctx *Context) (popup, tea.Cmd) {
			return nil, edit(ctx, "remove from “"+name+"”",
				func(c context.Context, api ytmusic.Client) error { return api.RemovePlaylistItems(c, id, ts) },
				func(m *Model) tea.Cmd {
					t.RemoveWhere(func(tr ytmusic.Track) bool { return gone[tr.SetVideoID] })
					return m.toasts.push(Toast{Severity: SevSuccess, Text: "Removed " + what + " from “" + name + "”"})
				}, nil)
		},
	}
}

func (p *menuPopup) withStatus(s string) *menuPopup {
	p.status = s
	return p
}

// openPickerMsg opens the playlist picker for tracks; marks on table are
// cleared once they were added.
type openPickerMsg struct {
	tracks []ytmusic.Track
	table  *components.TrackTable
}

// pickerPopup chooses a playlist the account owns to add tracks to, or
// creates a new one with them.
type pickerPopup struct {
	*menuPopup
	tracks []ytmusic.Track
	table  *components.TrackTable
}

func (m *Model) openPicker(msg openPickerMsg) tea.Cmd {
	p := &pickerPopup{tracks: msg.tracks, table: msg.table}
	what := "“" + msg.tracks[0].Title + "”"
	if len(msg.tracks) > 1 {
		what = plural(len(msg.tracks), "track")
	}
	p.menuPopup = newMenu("Add "+what+" to playlist", []menuItem{{label: "+ New playlist", run: p.create}})
	p.fixed, p.filterable = 1, true
	p.status = "Loading playlists…"
	p.hint = "enter add · esc close"
	m.popup = p
	ctx := m.ctx()
	api, root := ctx.API, ctx.Root
	return func() tea.Msg {
		if api == nil {
			return popupMsg{to: p, msg: Loaded{Err: errNoAPI}}
		}
		v, err := api.LibraryPlaylists(root, 0)
		return popupMsg{to: p, msg: Loaded{Key: "playlists", Value: v, Err: err}}
	}
}

// Key implements popup; the embedded menu would return itself and drop
// the picker (and its pending results).
func (p *pickerPopup) Key(msg tea.KeyPressMsg, ctx *Context) (popup, tea.Cmd) {
	next, cmd := p.menuPopup.Key(msg, ctx)
	if next == popup(p.menuPopup) {
		return p, cmd
	}
	return next, cmd
}

// Receive implements popupReceiver.
func (p *pickerPopup) Receive(msg tea.Msg, _ *Context) (popup, tea.Cmd) {
	l, ok := msg.(Loaded)
	if !ok {
		return p, nil
	}
	if l.Err != nil {
		p.status = loadErrorText(l.Err)
		if p.status == "" {
			p.status = "Couldn't load your playlists."
		}
		return p, nil
	}
	items := p.items[:1]
	for _, pl := range l.Value.([]ytmusic.PlaylistRef) {
		// Only owned playlists take edits (Liked Music fills by liking).
		if !pl.Owned {
			continue
		}
		items = append(items, menuItem{label: pl.Title, right: pl.Count, run: p.adder(pl.ID, pl.Title, false)})
	}
	p.status = plural(len(items)-1, "playlist")
	p.setItems(items)
	return p, nil
}

func (p *pickerPopup) ids() []string {
	out := make([]string, len(p.tracks))
	for i, t := range p.tracks {
		out[i] = t.VideoID
	}
	return out
}

func (p *pickerPopup) done(m *Model) {
	if p.table != nil {
		p.table.ClearMarks()
	}
	if m.popup == p {
		m.popup = nil
	}
}

// adder adds the tracks to playlist id. Without dup, YouTube Music
// refuses videos already there; that answer turns into the original's
// "Add anyway?" question.
func (p *pickerPopup) adder(id, title string, dup bool) func(*Context) (popup, tea.Cmd) {
	return func(ctx *Context) (popup, tea.Cmd) {
		ids := p.ids()
		if !dup && hasDuplicates(ids) {
			// The selection itself holds a song twice: the client refuses
			// that without dup, so ask first.
			return p.duplicateQuestion(id, title), nil
		}
		p.busy, p.status = true, "Adding to “"+title+"”…"
		return p, edit(ctx, "add to “"+title+"”",
			func(c context.Context, api ytmusic.Client) error { return api.AddPlaylistItems(c, id, ids, dup) },
			func(m *Model) tea.Cmd {
				p.done(m)
				return tea.Batch(m.toasts.push(Toast{Severity: SevSuccess, Text: "Added " + plural(len(ids), "track") + " to “" + title + "”"}),
					m.playlistEdited(id))
			},
			func(m *Model, err error) tea.Cmd {
				p.busy, p.status = false, ""
				if !dup && errors.Is(err, ytmusic.ErrEditFailed) {
					m.popup = p.duplicateQuestion(id, title)
					return nil
				}
				return m.toasts.push(Toast{Severity: SevError, Text: "Couldn't add to “" + title + "”: " + editErrorText(err)})
			})
	}
}

func (p *pickerPopup) duplicateQuestion(id, title string) popup {
	text := "This track is already in “" + title + "”.\nAdd it anyway?"
	if len(p.tracks) > 1 {
		text = "Some of the " + plural(len(p.tracks), "track") + " are already in “" + title + "”.\nAdd them all anyway?"
	}
	return &confirmPopup{
		title: "Already in playlist", text: text, yes: "add anyway",
		onYes: func(ctx *Context) (popup, tea.Cmd) { return p.adder(id, title, true)(ctx) },
	}
}

func (p *pickerPopup) create(*Context) (popup, tea.Cmd) {
	return &createPopup{submit: func(ctx *Context, name, privacy string) (popup, tea.Cmd) {
		ids := unique(p.ids())
		return nil, edit(ctx, "create “"+name+"”",
			func(c context.Context, api ytmusic.Client) error {
				_, err := api.CreatePlaylist(c, name, "", privacy, ids)
				return err
			},
			func(m *Model) tea.Cmd {
				p.done(m)
				return tea.Batch(m.toasts.push(Toast{Severity: SevSuccess, Text: "Created “" + name + "” with " + plural(len(ids), "track")}),
					m.playlistsChanged())
			}, nil)
	}}, nil
}

func hasDuplicates(ids []string) bool {
	return len(unique(ids)) != len(ids)
}

func unique(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// playlistEdited reloads the playlist's open Context page (its rows and
// count changed) and the library list (counts).
func (m *Model) playlistEdited(id string) tea.Cmd {
	cmds := []tea.Cmd{m.playlistsChanged()}
	if p, ok := m.pages[PageContext].(*contextPage); ok && p.kind == kindPlaylist && samePlaylist(p.id, id) {
		p.state = stateIdle
		if m.current == PageContext {
			cmds = append(cmds, p.load(m.ctx()))
		}
	}
	return tea.Batch(cmds...)
}

// playlistsChanged makes the library's playlist list reload (now when it
// is on screen, else when next shown).
func (m *Model) playlistsChanged() tea.Cmd {
	var cmds []tea.Cmd
	m.side.state = stateIdle
	if m.sidebar {
		cmds = append(cmds, m.side.load(m.ctx()))
	}
	if lib, ok := m.pages[PageLibrary].(*libraryPage); ok {
		cmds = append(cmds, lib.playlistsChanged(m.ctx()))
	}
	return tea.Batch(cmds...)
}

func samePlaylist(a, b string) bool {
	return strings.TrimPrefix(a, "VL") == strings.TrimPrefix(b, "VL")
}

// playlistActionsPopup lists what can be done with a library playlist.
func playlistActionsPopup(pl ytmusic.PlaylistRef) popup {
	items := []menuItem{{label: "Open", run: pageAction("select")}}
	if pl.Owned {
		items = append(items, menuItem{label: "Delete playlist", run: pageAction("delete_item")})
	}
	items = append(items, menuItem{label: "Copy link", run: copyLink(musicURL + "playlist?list=" + strings.TrimPrefix(pl.ID, "VL"))})
	return newMenu(pl.Title, items)
}

// deletePlaylistConfirm asks before deleting an owned playlist.
func deletePlaylistConfirm(pl ytmusic.PlaylistRef) popup {
	return &confirmPopup{
		title: "Delete playlist", yes: "delete",
		text: "Delete “" + pl.Title + "”?\nThis can't be undone.",
		onYes: func(ctx *Context) (popup, tea.Cmd) {
			return nil, edit(ctx, "delete “"+pl.Title+"”",
				func(c context.Context, api ytmusic.Client) error { return api.DeletePlaylist(c, pl.ID) },
				func(m *Model) tea.Cmd {
					if lib, ok := m.pages[PageLibrary].(*libraryPage); ok {
						lib.dropPlaylist(pl.ID)
					}
					m.side.drop(pl.ID)
					return m.toasts.push(Toast{Severity: SevSuccess, Text: "Deleted “" + pl.Title + "”"})
				}, nil)
		},
	}
}
