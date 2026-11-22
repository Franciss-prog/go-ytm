// Package lyrics finds a track's lyrics (YouTube Music first, LRCLIB as the
// synced fallback), parses LRC and tracks the current line.
package lyrics

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Line is one lyric line. Start is zero for unsynced lyrics.
type Line struct {
	Start time.Duration
	Text  string
}

// Lyrics is what the lyrics pane shows.
type Lyrics struct {
	Lines  []Line
	Synced bool   // Lines carry start times, sorted ascending
	Source string // display label: "YouTube Music · LyricFind", "LRCLIB"
}

// Index returns the line playing at pos: the last line whose Start <= pos,
// or -1 before the first line and for unsynced lyrics.
func (l *Lyrics) Index(pos time.Duration) int {
	if l == nil || !l.Synced {
		return -1
	}
	return sort.Search(len(l.Lines), func(i int) bool { return l.Lines[i].Start > pos }) - 1
}

var (
	lrcStamp  = regexp.MustCompile(`^\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]`)
	lrcOffset = regexp.MustCompile(`^\[offset:\s*([+-]?\d+)\s*\]$`)
)

// ParseLRC parses LRC text. A line with several leading stamps
// ("[00:12.00][01:30.50]chorus") yields one line per stamp; "[offset:±ms]"
// shifts every stamp (positive = earlier, per the LRC convention); tag and
// unstamped lines are dropped. The result is sorted by start time.
func ParseLRC(s string) []Line {
	var out []Line
	var offset time.Duration
	for _, raw := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if m := lrcOffset.FindStringSubmatch(line); m != nil {
			ms, _ := strconv.Atoi(m[1])
			offset = time.Duration(ms) * time.Millisecond
			continue
		}
		var starts []time.Duration
		for {
			m := lrcStamp.FindStringSubmatch(line)
			if m == nil {
				break
			}
			starts = append(starts, stamp(m[1], m[2], m[3]))
			line = line[len(m[0]):]
		}
		text := strings.TrimSpace(line)
		for _, st := range starts {
			out = append(out, Line{Start: st, Text: text})
		}
	}
	for i := range out {
		out[i].Start = max(out[i].Start-offset, 0)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

func stamp(min, sec, frac string) time.Duration {
	m, _ := strconv.Atoi(min)
	s, _ := strconv.Atoi(sec)
	d := time.Duration(m)*time.Minute + time.Duration(s)*time.Second
	if frac != "" {
		f, _ := strconv.Atoi(frac)
		// ".5" is 500 ms, ".50" is 500 ms, ".500" is 500 ms.
		for i := len(frac); i < 3; i++ {
			f *= 10
		}
		d += time.Duration(f) * time.Millisecond
	}
	return d
}

// titleNoise is ported from ytm-player (MIT) utils/formatting.py: bracketed
// "(Official Video)", "[feat. X]", "(Remastered 2011)", "(Live at …)" etc.
// Bare words outside brackets are kept.
var titleNoise = regexp.MustCompile(`(?i)\s*[(\[]\s*(?:` +
	`official\s*(?:music\s*)?(?:video|audio|lyric|lyrics)` +
	`|lyrics?\s*video|official|audio|video|hd|4k` +
	`|(?:feat\.?|ft\.?|featuring)\b(?:[^()\]]|\([^)]*\))*` +
	`|remaster(?:ed)?(?:\s+\d{4})?` +
	`|(?:[^)\]]+\s+)?remix(?:\s+[^)\]]*)?` +
	`|deluxe(?:\s+edition)?` +
	`|live(?:\s+[^)\]]*)?` +
	`|acoustic(?:\s+[^)\]]*)?` +
	`)\s*[)\]]\s*`)

var spaces = regexp.MustCompile(`\s{2,}`)

// SanitizeTitle strips video noise from a title for an LRCLIB lookup and a
// leading "Artist - " prefix. It returns title unchanged if nothing is left.
func SanitizeTitle(title, artist string) string {
	t := strings.TrimSpace(spaces.ReplaceAllString(titleNoise.ReplaceAllString(title, " "), " "))
	if p := artist + " - "; artist != "" && len(t) >= len(p) && strings.EqualFold(t[:len(p)], p) {
		t = strings.TrimSpace(t[len(p):])
	}
	if t == "" {
		return title
	}
	return t
}
