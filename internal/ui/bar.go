package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

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

// barState is everything the playback bar draws.
type barState struct {
	st      player.State
	now     NowPlaying
	liked   bool
	shuffle bool
	repeat  string // "off" | "all" | "one"
	style   string // [ui] progress_style: block|line
}

// barGeom records where the progress bar and volume were drawn, for mouse
// hit-testing (row is relative to the bar's first row).
type barGeom struct {
	progressRow, progressX, progressW int
	volumeRow, volumeX, volumeW       int
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

// progress draws a width-cell progress bar.
func (b barState) progress(width int, u *style.UI) string {
	if width <= 0 {
		return ""
	}
	frac := 0.0
	if b.st.Duration > 0 {
		frac = min(max(float64(b.st.Position)/float64(b.st.Duration), 0), 1)
	}
	filled := int(frac * float64(width))
	if b.style == "line" {
		head := ""
		if filled < width && b.st.Duration > 0 {
			head = "╸"
		}
		empty := width - filled - ansi.StringWidth(head)
		return u.ProgressFilled.Render(strings.Repeat("━", filled)+head) + u.ProgressEmpty.Render(strings.Repeat("─", empty))
	}
	return u.ProgressFilled.Render(strings.Repeat("█", filled)) + u.ProgressEmpty.Render(strings.Repeat("░", width-filled))
}

// view renders the bar: 3 rows normally, 1 row when compact.
func (b barState) view(width int, compact bool, u *style.UI) (string, barGeom) {
	ic := u.Opts.Icons
	var g barGeom
	vol := fmt.Sprintf("vol %d%%", b.st.Volume)
	if b.st.Muted {
		vol = "muted"
	}
	if b.st.Item.IsZero() {
		line := " " + u.Muted.Render("Nothing playing")
		if compact {
			return fit(line, width), g
		}
		volCell := u.BarTime.Render(vol) + " "
		volW := ansi.StringWidth(volCell)
		g.volumeRow, g.volumeX, g.volumeW = 2, width-volW, volW
		return strings.Join([]string{fit(line, width), fit("", width), fit("", width-volW) + volCell}, "\n"), g
	}
	title, sub := b.title()
	icon := u.BarIconActive.Render(b.playIcon(ic))
	pos, dur := fmtDur(b.st.Position), fmtDur(b.st.Duration)

	if compact {
		left := " " + icon + " " + u.BarTitle.Render(title)
		right := " " + u.BarTime.Render(pos+"/"+dur) + " "
		return fit(fit(left, width-ansi.StringWidth(right))+right, width), g
	}

	flag := func(on bool, glyph string) string {
		if on {
			return u.BarIconActive.Render(glyph)
		}
		return u.BarIcon.Render(glyph)
	}
	rep := ic.Repeat
	if b.repeat == "one" {
		rep += "1"
	}
	controls := flag(b.liked, ic.Heart) + "  " + flag(b.shuffle, ic.Shuffle) + "  " + flag(b.repeat != "off" && b.repeat != "", rep) + "  " + icon + " "
	cw := ansi.StringWidth(controls)
	row1 := fit(" "+u.BarTitle.Render(title), width-cw) + controls
	row2 := " " + u.BarArtist.Render(sub)
	if b.st.Status == player.Error && b.st.Err != "" {
		row2 = " " + u.ErrorText.Render(ic.Fail+" "+b.st.Err)
	}

	left := " " + u.BarTime.Render(pos) + " "
	right := " " + u.BarTime.Render(dur) + "   " + u.BarTime.Render(vol) + " "
	pw := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	g.progressRow, g.progressX, g.progressW = 2, ansi.StringWidth(left), max(pw, 0)
	volCell := u.BarTime.Render(vol) + " "
	g.volumeRow, g.volumeX, g.volumeW = 2, width-ansi.StringWidth(volCell), ansi.StringWidth(volCell)
	row3 := left + b.progress(pw, u) + right
	return strings.Join([]string{fit(row1, width), fit(row2, width), fit(row3, width)}, "\n"), g
}
