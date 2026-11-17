package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/player"
	"github.com/Franciss-Prog/go-ytm/internal/style"
)

// NowPlaying is display metadata for the current track. Pages that start
// playback (M5) fill it; until then the bar falls back to the player item.
type NowPlaying struct {
	VideoID string
	Title   string
	Artist  string
	Album   string
}

// barState is everything the mini player and tab bar show about playback.
type barState struct {
	st      player.State
	now     NowPlaying
	liked   bool
	shuffle bool
	repeat  string // "off" | "all" | "one"
	style   string // [ui] progress_style: block|line (track page)

	// Wheel scrubbing: the bar previews scrubPos until the seek commits.
	scrubbing bool
	scrubPos  time.Duration
}

// pos is the position to draw: the scrub preview while scrubbing.
func (b barState) pos() time.Duration {
	if b.scrubbing {
		return b.scrubPos
	}
	return b.st.Position
}

func fmtDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Round(time.Second) / time.Second)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func (b barState) title() (title, sub string) {
	if b.now.VideoID != "" && b.now.VideoID == b.st.Item.ID {
		parts := []string{}
		if b.now.Artist != "" {
			parts = append(parts, b.now.Artist)
		}
		if b.now.Album != "" {
			parts = append(parts, b.now.Album)
		}
		return b.now.Title, strings.Join(parts, " · ")
	}
	if b.st.Item.Title != "" {
		return b.st.Item.Title, ""
	}
	return b.st.Item.ID, ""
}

func (b barState) playIcon(ic style.IconSet) string {
	switch b.st.Status {
	case player.Playing:
		return ic.Play
	case player.Loading, player.Buffering:
		return ic.Ellipsis
	default:
		return ic.Pause
	}
}
