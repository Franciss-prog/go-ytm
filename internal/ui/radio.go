package ui

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/queue"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/ui/components"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// Radio, autoplay, discovery mix and history reporting (M6), following
// ytm-player app/_playback.py (_fetch_and_play_radio, _refill_queue,
// _start_discovery_mix, _report_ytm_play) and services/ytmusic.py
// (get_radio, get_playlist_radio, get_discovery_mix).

const (
	radioLimit = 25 // tracks per radio fetch (get_radio default)
	// refillAt: autoplay fetches more when this few tracks are left
	// (_refill_queue: max(3, prefetch count); we prefetch one).
	refillAt   = 3
	radioSeeds = 5 // autoplay seeds: the last tracks played
)

// RadioMsg replaces the queue with a radio and plays it. Seeds start a
// track radio (several seeds: a shuffled mix); Playlist starts a playlist
// radio. Label names it in toasts and the Queue page ("" = derived).
type RadioMsg struct {
	Seeds    []ytmusic.Track
	Playlist string
	Mix      string // a ready-made radio playlist ("RD…") from the home feed
	Label    string
}

// radioLoadedMsg answers a radio fetch. autoplay appends (↻) instead of
// replacing; playAfter then starts the first new track (the queue had run
// out).
type radioLoadedMsg struct {
	seq       uint64
	gen       uint64 // queue generation the autoplay refill was for
	autoplay  bool
	playAfter bool
	seeds     []ytmusic.Track
	label     string
	tracks    []ytmusic.Track
	err       error
}

// discoveryMsg answers the discovery seed pick.
type discoveryMsg struct {
	seq    uint64
	source int
	seeds  []ytmusic.Track
	label  string
}

// historyReportedMsg answers a play report.
type historyReportedMsg struct {
	id  string
	err error
}

// radioState is the playback owner's radio bookkeeping.
type radioState struct {
	seq       uint64 // latest user radio / discovery request
	refilling uint64 // queue generation with an autoplay fetch in flight (0 none)
	source    string // "Generated from" label for the Queue page
	discovery int    // last discovery source (1..6), rotates per press

	// History reporting: the track loaded, where it started, and whether
	// it was reported.
	reportID string
	startPos float64
	reported bool
}

func ids(ts []ytmusic.Track) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		if t.VideoID != "" {
			out = append(out, t.VideoID)
		}
	}
	return out
}

// startRadio fetches a radio for msg and replaces the queue with it.
func (m *Model) startRadio(msg RadioMsg) tea.Cmd {
	api := m.o.API
	if api == nil {
		return m.toasts.push(Toast{Severity: SevError, Text: "YouTube Music is unavailable."})
	}
	seeds := msg.Seeds
	if msg.Playlist == "" && msg.Mix == "" && len(ids(seeds)) == 0 {
		return nil
	}
	label := msg.Label
	if label == "" && len(seeds) > 0 {
		label = "Radio from " + seeds[0].Title
	}
	m.pb.radio.seq++
	seq, ctx, pl, mix := m.pb.radio.seq, m.o.Context, strings.TrimPrefix(msg.Playlist, "VL"), msg.Mix
	fetch := func() tea.Msg {
		var ts []ytmusic.Track
		var err error
		switch {
		case mix != "":
			var w ytmusic.WatchPlaylist
			w, err = api.WatchPlaylist(ctx, ytmusic.WatchRequest{PlaylistID: mix, Limit: radioLimit})
			ts = w.Tracks
		case pl != "":
			var w ytmusic.WatchPlaylist
			w, err = api.WatchPlaylist(ctx, ytmusic.WatchRequest{PlaylistID: "RDAMPL" + pl, Radio: true, Limit: radioLimit})
			ts = w.Tracks
		default:
			ts, err = ytmusic.Radio(ctx, api, ids(seeds), radioLimit)
		}
		return radioLoadedMsg{seq: seq, seeds: seeds, label: label, tracks: ts, err: err}
	}
	return tea.Batch(m.toasts.push(Toast{Severity: SevInfo, Text: "Loading radio…"}), fetch)
}

// refill appends autoplay suggestions when the queue is running low
// (repeat off, autoplay on, one fetch per queue generation at a time).
// playAfter starts the first new track: the queue already ran out.
func (m *Model) refill(playAfter bool) tea.Cmd {
	q := m.pb.q
	if !m.o.Config.Playback.Autoplay || m.o.API == nil || q.Repeat() != queue.RepeatOff || q.Len() == 0 {
		return nil
	}
	if !playAfter && q.Remaining() > refillAt {
		return nil
	}
	gen := q.Generation() + 1 // +1: generation 0 is a real queue
	if m.pb.radio.refilling == gen {
		return nil
	}
	seeds := q.PlayedTracks()
	if len(seeds) > radioSeeds {
		seeds = seeds[len(seeds)-radioSeeds:]
	}
	if len(seeds) == 0 {
		return nil
	}
	m.pb.radio.refilling = gen
	api, ctx := m.o.API, m.o.Context
	return func() tea.Msg {
		ts, err := ytmusic.Radio(ctx, api, ids(seeds), radioLimit)
		return radioLoadedMsg{gen: gen, autoplay: true, playAfter: playAfter, tracks: ts, err: err}
	}
}

// radioLoaded applies a radio fetch.
func (m *Model) radioLoaded(msg radioLoadedMsg) tea.Cmd {
	q := m.pb.q
	if msg.autoplay {
		if msg.gen != q.Generation()+1 || m.pb.radio.refilling != msg.gen {
			return nil // the queue was replaced meanwhile
		}
		m.pb.radio.refilling = 0
		if msg.err != nil {
			m.log.Warn("ui: autoplay fetch failed", "err", msg.err)
		}
		n := q.AddRadio(msg.tracks, true)
		if n == 0 {
			if msg.playAfter {
				return m.toasts.push(Toast{Severity: SevInfo, Text: "End of queue"})
			}
			return nil
		}
		if msg.playAfter {
			if _, ok := q.Next(); ok {
				return m.startCurrent(0)
			}
		}
		return m.queueEdited()
	}
	if msg.seq != m.pb.radio.seq {
		return nil
	}
	if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
		m.log.Warn("ui: radio failed", "err", msg.err)
	}
	if len(msg.tracks) == 0 {
		if errors.Is(msg.err, context.Canceled) {
			return nil
		}
		return m.toasts.push(Toast{Severity: SevWarn, Text: "No radio suggestions available."})
	}
	q.Clear()
	q.SetShuffle(false)
	q.AddMany(msg.seeds)
	q.AddRadio(msg.tracks, false)
	q.SetContext("") // ephemeral: no shuffle lock key (TP-7)
	q.JumpTo(0)
	m.pb.radio.source = msg.label
	m.syncQueueFlags()
	return tea.Batch(m.toasts.push(Toast{Severity: SevSuccess, Text: "Playing: " + msg.label}), m.startCurrent(0))
}

// discoverySources are tried in rotation, starting after the last one
// that worked (get_discovery_mix): charts, trending, home, liked songs,
// a library artist, history.
const discoverySources = 6

// startDiscovery picks seeds from the next discovery source.
func (m *Model) startDiscovery() tea.Cmd {
	api := m.o.API
	if api == nil {
		return m.toasts.push(Toast{Severity: SevError, Text: "YouTube Music is unavailable."})
	}
	m.pb.radio.seq++
	seq, ctx, last, region := m.pb.radio.seq, m.o.Context, m.pb.radio.discovery, m.o.Config.UI.Region
	pick := func() tea.Msg {
		for i := 1; i <= discoverySources; i++ {
			src := (last+i-1)%discoverySources + 1
			seeds, label, err := discoverySeeds(ctx, api, src, region)
			if err != nil {
				if ctx.Err() != nil {
					break
				}
				continue
			}
			if len(seeds) > 0 {
				return discoveryMsg{seq: seq, source: src, seeds: seeds, label: label}
			}
		}
		return discoveryMsg{seq: seq}
	}
	return tea.Batch(m.toasts.push(Toast{Severity: SevInfo, Text: "Loading discovery mix…"}), pick)
}

func (m *Model) discovered(msg discoveryMsg) tea.Cmd {
	if msg.seq != m.pb.radio.seq {
		return nil
	}
	if len(msg.seeds) == 0 {
		return m.toasts.push(Toast{Severity: SevWarn, Text: "Discovery failed: no content available"})
	}
	m.pb.radio.discovery = msg.source
	return tea.Batch(m.startRadio(RadioMsg{Seeds: msg.seeds, Label: "Discovery (" + msg.label + ")"}), m.navigate(PageQueue))
}

// sample returns up to n random playable tracks of ts.
func sample(ts []ytmusic.Track, n int) []ytmusic.Track {
	var ok []ytmusic.Track
	for _, t := range ts {
		if t.VideoID != "" && t.Available {
			ok = append(ok, t)
		}
	}
	rand.Shuffle(len(ok), func(i, j int) { ok[i], ok[j] = ok[j], ok[i] }) //nolint:gosec // not security
	return ok[:min(n, len(ok))]
}

// discoverySeeds returns three seeds and a label from one source
// (get_discovery_mix's six sources).
func discoverySeeds(ctx context.Context, api ytmusic.Client, source int, region string) ([]ytmusic.Track, string, error) {
	const n = 3
	switch source {
	case 1: // charts: a random chart playlist of the region
		ch, err := api.Charts(ctx, region)
		if err != nil || len(ch.Playlists) == 0 {
			return nil, "", err
		}
		p := ch.Playlists[rand.IntN(len(ch.Playlists))] //nolint:gosec // not security
		pl, err := api.Playlist(ctx, p.ID)
		if err != nil {
			return nil, "", err
		}
		return sample(pl.Tracks, n), cmpOr(ch.Country, "Charts") + " " + p.Title, nil
	case 2: // trending
		ex, err := api.Explore(ctx)
		if err != nil {
			return nil, "", err
		}
		return sample(ex.Trending.Items, n), "Trending", nil
	case 3: // home feed songs
		shelves, err := api.Home(ctx, 3)
		if err != nil {
			return nil, "", err
		}
		var ts []ytmusic.Track
		for _, s := range shelves {
			for _, it := range s.Items {
				if it.Track != nil {
					ts = append(ts, *it.Track)
				}
			}
		}
		return sample(ts, n), "For You", nil
	case 4: // liked songs
		pl, err := api.Playlist(ctx, "LM")
		if err != nil {
			return nil, "", err
		}
		return sample(pl.Tracks, n), "Your Liked Songs", nil
	case 5: // a random library artist's top songs
		ars, err := api.LibraryArtists(ctx, 25)
		if err != nil || len(ars) == 0 {
			return nil, "", err
		}
		ar := ars[rand.IntN(len(ars))] //nolint:gosec // not security
		page, err := api.Artist(ctx, ar.ID)
		if err != nil {
			return nil, "", err
		}
		return sample(page.TopSongs.Items, n), "Artist: " + ar.Name, nil
	case 6: // the 20 most recent plays
		h, err := api.History(ctx)
		if err != nil {
			return nil, "", err
		}
		var ts []ytmusic.Track
		for _, it := range h[:min(20, len(h))] {
			ts = append(ts, it.Track)
		}
		return sample(ts, n), "Recently Played", nil
	}
	return nil, "", nil
}

// --- history reporting ------------------------------------------------------

// trackLoaded arms history reporting for the track mpv now plays.
func (m *Model) trackLoaded(id string, startAt float64) {
	m.pb.radio.reportID, m.pb.radio.startPos, m.pb.radio.reported = id, startAt, false
}

// maybeReport records the play once the same track has played past
// history_min_listen_seconds from where it started: in the local play log
// always, and in the account history when sync_history_to_ytmusic is on
// and signed in (_report_local_play, _report_ytm_play). mpv keeps sending
// positions without terminal focus.
func (m *Model) maybeReport() tea.Cmd {
	r := &m.pb.radio
	st := m.bar.st
	if r.reported || r.reportID == "" || st.Item.ID != r.reportID {
		return nil
	}
	if st.Position.Seconds()-r.startPos <= float64(m.o.Config.Playback.HistoryMinListenSeconds) {
		return nil
	}
	r.reported = true
	var save tea.Cmd
	if t, ok := m.pb.q.Current(); ok && t.VideoID == r.reportID {
		m.plays.add(t)
		if l := m.o.Local; l != nil {
			// shortcut: logs the listen time at the threshold, not the final one
			// (ytm-player updates it on track end); upgrade with M9's stats.
			listened := st.Position - time.Duration(r.startPos*float64(time.Second))
			save = localWrite(m.o.Context, m.log, "play", func(c context.Context) error { return l.LogPlay(c, t, listened) })
		}
	}
	if !m.o.Config.Playback.SyncHistoryToYTMusic || !m.o.SignedIn || m.o.API == nil {
		return save
	}
	api, ctx, id := m.o.API, m.o.Context, r.reportID
	return tea.Batch(save, func() tea.Msg { return historyReportedMsg{id: id, err: api.AddHistoryItem(ctx, id)} })
}

// historyReported logs a failed report. Best effort and not retried:
// positions arrive several times a second, so a retry per update would
// hammer the API on a persistent failure.
func (m *Model) historyReported(msg historyReportedMsg) {
	if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
		m.log.Info("ui: history report failed", "id", msg.id, "err", msg.err)
	}
}

// radioSeedsFromRows is how many random rows seed a page's Start Radio.
const radioSeedsFromRows = 5

// radioButton is the clickable "▶ Start Radio" line of the Liked Songs and
// Recently Played pages.
func radioButton(st *style.UI) string { return " " + st.Accent.Render("▶ Start Radio") }

// radioFromRows starts a radio seeded by random rows of t.
func radioFromRows(t *components.TrackTable, label string) tea.Cmd {
	seeds := sample(t.Rows(), radioSeedsFromRows)
	if len(seeds) == 0 {
		return nil
	}
	return msgCmd(RadioMsg{Seeds: seeds, Label: label})
}
