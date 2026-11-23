package ui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/lyrics"
	"github.com/Franciss-Prog/go-ytm/internal/player"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// Lyrics pane (M8, spec 05 Sidebars): synced lines follow playback with
// the current line centered; a manual scroll pauses that for 5 s.

// LyricsFinder finds a track's lyrics (*lyrics.Finder in production). It
// blocks: the model calls it only from commands.
type LyricsFinder interface {
	Find(ctx context.Context, t lyrics.Track) (*lyrics.Lyrics, error)
}

// lyricsPause is how long a manual scroll stops the auto-scroll.
const lyricsPause = 5 * time.Second

// lyricsPage is the C-f/C-b scroll step, in rows.
const lyricsPage = 10

type lyricsLoadedMsg struct {
	videoID string
	l       *lyrics.Lyrics
	err     error
}

// lyricsResumeMsg ends a manual-scroll pause (ignored if seq is stale).
type lyricsResumeMsg struct{ seq uint64 }

type lyricsState struct {
	find     LyricsFinder // nil: no lyrics
	translit bool         // show a transliteration under non-ASCII lines

	videoID string // track the state is for ("" none)
	state   loadState
	l       *lyrics.Lyrics
	cancel  context.CancelFunc

	manual bool   // auto-scroll paused by a manual scroll
	seq    uint64 // bumps per manual scroll
	top    int    // first row drawn last time
	maxTop int    // largest useful top at the last draw

	// starts maps each drawn row to its lyric line (-1: none), for clicks.
	starts []int
}

// syncLyrics runs after every Update: while the pane is shown it fetches
// the current track's lyrics.
func (m *Model) syncLyrics() tea.Cmd {
	s := &m.lyr
	if s.find == nil || !m.lyrics {
		return nil
	}
	t, _ := m.pb.q.Current()
	if t.VideoID == s.videoID {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.videoID, s.l, s.manual, s.top = t.VideoID, nil, false, 0
	if t.VideoID == "" {
		s.state = stateIdle
		return nil
	}
	return m.fetchLyrics(t)
}

func (m *Model) fetchLyrics(t ytmusic.Track) tea.Cmd {
	s := &m.lyr
	ctx, cancel := context.WithCancel(m.o.Context)
	s.state, s.cancel = stateLoading, cancel
	f := s.find
	q := lyrics.Track{VideoID: t.VideoID, Title: t.Title, Artist: firstArtist(t), Duration: t.Duration}
	if t.Album != nil {
		q.Album = t.Album.Name
	}
	return func() tea.Msg {
		defer cancel()
		l, err := f.Find(ctx, q)
		return lyricsLoadedMsg{videoID: t.VideoID, l: l, err: err}
	}
}

func firstArtist(t ytmusic.Track) string {
	if len(t.Artists) == 0 {
		return ""
	}
	return t.Artists[0].Name
}

func (m *Model) lyricsLoaded(msg lyricsLoadedMsg) {
	s := &m.lyr
	if msg.videoID != s.videoID {
		return // the track changed meanwhile
	}
	s.cancel = nil
	switch {
	case msg.l != nil:
		s.l, s.state = msg.l, stateLoaded
	case msg.err != nil:
		m.log.Warn("ui: lyrics", "video", msg.videoID, "err", msg.err)
		s.state = stateFailed
	default:
		s.state = stateLoaded
	}
}

// lyricsFocused reports that the visible lyrics pane owns the keyboard.
func (m *Model) lyricsFocused() bool {
	return m.focus == paneLyrics && m.layout().lyricsW > 0
}

// lyricsAction runs a on the focused lyrics pane. handled=false lets the
// page and then the global handler take it.
func (m *Model) lyricsAction(a keys.Action) (tea.Cmd, bool) {
	n := max(a.Count, 1)
	switch a.Name {
	case "move_down":
		return m.scrollLyrics(n), true
	case "move_up":
		return m.scrollLyrics(-n), true
	case "page_down":
		return m.scrollLyrics(lyricsPage * n), true
	case "page_up":
		return m.scrollLyrics(-lyricsPage * n), true
	case "go_top":
		return m.scrollLyrics(-m.lyr.maxTop - m.lyr.top), true
	case "go_bottom":
		return m.scrollLyrics(m.lyr.maxTop), true
	case "retry":
		if m.lyr.state != stateFailed {
			return nil, false
		}
		t, ok := m.pb.q.Current()
		if !ok {
			return nil, true
		}
		return m.fetchLyrics(t), true
	}
	return nil, false
}

// scrollLyrics moves the pane by d rows and pauses the auto-scroll.
func (m *Model) scrollLyrics(d int) tea.Cmd {
	s := &m.lyr
	if s.l == nil {
		return nil
	}
	s.top = max(min(s.top+d, s.maxTop), 0)
	s.manual = true
	s.seq++
	return after(m.o.Context, lyricsPause, lyricsResumeMsg{seq: s.seq})
}

func (m *Model) toggleTransliteration() tea.Cmd {
	m.lyr.translit = !m.lyr.translit
	text := "Transliteration off"
	if m.lyr.translit {
		text = "Transliteration on"
	}
	return m.toasts.push(Toast{Severity: SevInfo, Text: text})
}

// lyricsInfo is the pane's bottom-left label: where the lyrics came from.
func (m *Model) lyricsInfo() string {
	l := m.lyr.l
	if l == nil || m.lyr.state != stateLoaded {
		return ""
	}
	if !l.Synced {
		return l.Source + " · not synced"
	}
	return l.Source
}

// lyricsView draws the pane body (w×h).
func (m *Model) lyricsView(w, h int) string {
	st, s := m.st, &m.lyr
	s.starts = s.starts[:0]
	msg := func(text string) string { return block([]string{"", " " + st.Muted.Render(text)}, w, h) }
	switch {
	case s.find == nil:
		return msg("Lyrics unavailable.")
	case s.videoID == "":
		return msg("Nothing playing.")
	case s.state == stateLoading:
		return msg("Loading lyrics…")
	case s.state == stateFailed:
		return msg("Couldn't load lyrics. r retries.")
	case s.l == nil || len(s.l.Lines) == 0:
		return msg("No lyrics for this track.")
	}

	cur := s.l.Index(m.bar.st.Position)
	inner := max(w-2, 1) // one column of margin each side
	bidi := m.o.Config.UI.BidiMode != "passthrough"
	var rows, owner []int
	var texts []string
	first := make([]int, len(s.l.Lines))
	for i, ln := range s.l.Lines {
		first[i] = len(texts)
		add := func(text string, translit bool) {
			for _, r := range strings.Split(ansi.Wrap(text, inner, ""), "\n") {
				right := false
				// shortcut: "auto" reorders every RTL line, as no terminal is
				// detected as drawing BiDi itself; add detection if one is.
				if bidi && lyrics.HasRTL(r) {
					r, right = lyrics.Visual(r), true
				}
				texts = append(texts, r)
				owner = append(owner, i)
				kind := 0
				if translit {
					kind = 1
				}
				if right {
					kind |= 2
				}
				rows = append(rows, kind)
			}
		}
		add(ln.Text, false)
		if s.translit {
			if t := lyrics.Transliterate(ln.Text); t != "" {
				add(t, true)
			}
		}
	}
	s.maxTop = max(len(texts)-h, 0)
	if !s.manual && cur >= 0 {
		s.top = first[cur] - h/2
	}
	s.top = max(min(s.top, s.maxTop), 0)

	out := make([]string, 0, h)
	for r := s.top; r < len(texts) && len(out) < h; r++ {
		i := owner[r]
		style := st.LyricsUpcoming
		switch {
		case rows[r]&1 != 0:
			style = st.Muted
		case cur < 0:
		case i < cur:
			style = st.LyricsPlayed
		case i == cur:
			style = st.LyricsCurrent
		}
		text := texts[r]
		if rows[r]&2 != 0 {
			text = strings.Repeat(" ", max(inner-ansi.StringWidth(text), 0)) + text
		}
		out = append(out, " "+style.Render(text))
		s.starts = append(s.starts, i)
	}
	return block(out, w, h)
}

// lyricsClick seeks to the clicked line of synced lyrics (row y of the
// pane body) and resumes the auto-scroll.
func (m *Model) lyricsClick(y int) {
	m.focus = paneLyrics
	s := &m.lyr
	if s.l == nil || !s.l.Synced || y < 0 || y >= len(s.starts) || m.o.Player == nil {
		return
	}
	s.manual = false
	if err := m.o.Player.Seek(s.l.Lines[s.starts[y]].Start, player.SeekAbsolute); err != nil {
		m.log.Warn("ui: seek failed", "err", err)
	}
}

// lyricsXY maps a screen cell to lyrics pane body coordinates.
func (l layout) lyricsXY(x, y int) (int, int, bool) {
	in := l.inset()
	x0 := l.sideW + l.panelW
	if l.lyricsOverlay {
		x0 -= l.lyricsW
	}
	x, y = x-x0-in, y-l.top-in
	return x, y, l.lyricsW > 0 && x >= 0 && x < l.lyricsW-2*in && y >= 0 && y < l.panelH-2*in
}
