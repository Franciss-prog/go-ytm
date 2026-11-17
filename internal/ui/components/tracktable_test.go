package components

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/theme"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

func testStyles(transparent bool, sel style.Selection) *style.UI {
	return style.NewUI(theme.Fallback(), style.UIOptions{
		Transparent: transparent, Selection: sel, Icons: style.UnicodeIcons,
	}, 1)
}

func track(id, title, artist, album string, secs int) ytmusic.Track {
	t := ytmusic.Track{
		VideoID: id, Title: title, Artists: []ytmusic.Artist{{Name: artist}},
		Duration: time.Duration(secs) * time.Second, Available: true,
	}
	if album != "" {
		t.Album = &ytmusic.AlbumRef{Name: album}
	}
	return t
}

func daftPunk() []ytmusic.Track {
	return []ytmusic.Track{
		track("a", "Get Lucky", "Daft Punk", "Random Access Memories", 369),
		track("b", "Instant Crush", "Daft Punk", "Random Access Memories", 337),
		track("c", "Lose Yourself to Dance", "Daft Punk", "Random Access Memories", 353),
		track("d", "Giorgio by Moroder", "Daft Punk", "Random Access Memories", 544),
		track("e", "Around the World", "Daft Punk", "Homework", 429),
		track("f", "One More Time", "Daft Punk", "Discovery", 320),
	}
}

func act(t *TrackTable, names ...string) {
	for _, n := range names {
		t.Action(keys.Action{Name: n})
	}
}

func typeText(t *TrackTable, s string) {
	for _, r := range s {
		t.Key(tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)}))
	}
}

func titles(t *TrackTable) []string {
	var out []string
	for _, i := range t.view {
		out = append(out, t.rows[i].Title)
	}
	return out
}

func TestTrackTableMotion(t *testing.T) {
	tt := NewTrackTable(Columns{}, "empty")
	tt.SetRows(daftPunk())
	tests := []struct {
		a    keys.Action
		want int
	}{
		{keys.Action{Name: "move_down"}, 1},
		{keys.Action{Name: "move_down", Count: 3}, 4},
		{keys.Action{Name: "move_down", Count: 30}, 5},
		{keys.Action{Name: "move_up", Count: 2}, 3},
		{keys.Action{Name: "go_top"}, 0},
		{keys.Action{Name: "go_bottom"}, 5},
		{keys.Action{Name: "go_top", Count: 3}, 2},
		{keys.Action{Name: "go_bottom", Count: 5}, 4},
		{keys.Action{Name: "move_up", Count: 99}, 0},
	}
	for _, tc := range tests {
		if !tt.Action(tc.a) {
			t.Fatalf("%v not handled", tc.a)
		}
		if tt.Cursor() != tc.want {
			t.Fatalf("after %v: cursor %d, want %d", tc.a, tt.Cursor(), tc.want)
		}
	}
	if tt.Action(keys.Action{Name: "quit"}) {
		t.Fatal("quit handled by table")
	}
}

func TestTrackTableMarks(t *testing.T) {
	tt := NewTrackTable(Columns{}, "")
	tt.SetRows(daftPunk())
	act(tt, "mark_toggle") // marks 0, cursor → 1
	if tt.Cursor() != 1 || !tt.Marked(0) {
		t.Fatalf("v: cursor %d marked0 %v", tt.Cursor(), tt.Marked(0))
	}
	tt.Action(keys.Action{Name: "move_down", Count: 2}) // cursor 3
	act(tt, "mark_range")                               // anchor 0 → 3
	if got := tt.Targets(); fmt.Sprint(got) != "[0 1 2 3]" {
		t.Fatalf("range targets %v", got)
	}
	act(tt, "mark_clear")
	if tt.MarkCount() != 0 {
		t.Fatal("escape kept marks")
	}
	if got := tt.Targets(); fmt.Sprint(got) != "[3]" {
		t.Fatalf("no marks: targets %v, want cursor [3]", got)
	}
	// Range without an anchor marks the cursor and anchors there.
	act(tt, "mark_range", "move_down", "mark_range")
	if got := tt.Targets(); fmt.Sprint(got) != "[3 4]" {
		t.Fatalf("anchorless range %v", got)
	}
	// Escape with nothing to clear is not handled (falls through to root).
	act(tt, "mark_clear")
	if tt.Action(keys.Action{Name: "mark_clear"}) {
		t.Fatal("empty escape handled")
	}
}

func TestTrackTableFilter(t *testing.T) {
	tt := NewTrackTable(Columns{}, "")
	tt.SetRows(daftPunk())
	act(tt, "go_bottom") // One More Time
	act(tt, "filter")
	if !tt.Filtering() {
		t.Fatal("not filtering")
	}
	typeText(tt, "OMT") // per field: "Random Access Memories" has o, m but no t
	if got := titles(tt); fmt.Sprint(got) != "[One More Time]" {
		t.Fatalf("filter titles %v", got)
	}
	if r, _ := tt.Selected(); r.Title != "One More Time" {
		t.Fatalf("cursor left the still-visible row: %q", r.Title)
	}
	typeText(tt, "x")
	if tt.Visible() != 0 {
		t.Fatal("no-match filter shows rows")
	}
	if _, ok := tt.Selected(); ok {
		t.Fatal("selection with no visible rows")
	}
	tt.Key(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	tt.Key(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if tt.Filtering() || tt.Filter() != "OMT" || tt.Visible() != 1 {
		t.Fatalf("enter: filtering %v filter %q visible %d", tt.Filtering(), tt.Filter(), tt.Visible())
	}
	// Album matches too (fuzzy subsequence).
	tt.SetFilter("hmwrk")
	if got := titles(tt); fmt.Sprint(got) != "[Around the World]" {
		t.Fatalf("album filter %v", got)
	}
	act(tt, "mark_clear")
	if tt.Filter() != "" || tt.Visible() != 6 {
		t.Fatal("escape did not clear filter")
	}
	act(tt, "filter")
	typeText(tt, "get")
	tt.Key(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if tt.Filtering() || tt.Filter() != "" {
		t.Fatal("escape in input did not clear")
	}
}

func TestTrackTableSort(t *testing.T) {
	tt := NewTrackTable(Columns{}, "")
	tt.SetRows(daftPunk())
	tt.Action(keys.Action{Name: "move_down", Count: 1}) // Instant Crush
	tests := []struct {
		action string
		want   string
	}{
		{"sort_title", "[Around the World Get Lucky Giorgio by Moroder Instant Crush Lose Yourself to Dance One More Time]"},
		{"reverse_sort", "[One More Time Lose Yourself to Dance Instant Crush Giorgio by Moroder Get Lucky Around the World]"},
		{"sort_duration", "[One More Time Instant Crush Lose Yourself to Dance Get Lucky Around the World Giorgio by Moroder]"},
		{"sort_duration", "[Giorgio by Moroder Around the World Get Lucky Lose Yourself to Dance Instant Crush One More Time]"},
		{"sort_album", "[One More Time Around the World Get Lucky Instant Crush Lose Yourself to Dance Giorgio by Moroder]"},
		{"sort_date", "[Get Lucky Instant Crush Lose Yourself to Dance Giorgio by Moroder Around the World One More Time]"},
		{"reverse_sort", "[One More Time Around the World Giorgio by Moroder Lose Yourself to Dance Instant Crush Get Lucky]"},
	}
	for _, tc := range tests {
		act(tt, tc.action)
		if got := fmt.Sprint(titles(tt)); got != tc.want {
			t.Fatalf("%s:\n got %s\nwant %s", tc.action, got, tc.want)
		}
		if r, _ := tt.Selected(); r.Title != "Instant Crush" {
			t.Fatalf("%s moved the cursor off its row: %q", tc.action, r.Title)
		}
	}
}

func TestTrackTableAppendKeepsState(t *testing.T) {
	tt := NewTrackTable(Columns{}, "")
	all := daftPunk()
	tt.SetRows(all[:3])
	tt.SetLoading(3, 6)
	act(tt, "move_down", "mark_toggle") // marks 1, cursor 2
	tt.AppendRows(all[3:])
	if tt.Len() != 6 || tt.Cursor() != 2 || !tt.Marked(1) {
		t.Fatalf("append: len %d cursor %d marked1 %v", tt.Len(), tt.Cursor(), tt.Marked(1))
	}
	tt.ReplaceRows(all[:5], 4)
	if tt.CursorSource() != 4 || tt.MarkCount() != 0 {
		t.Fatalf("replace: cursor src %d marks %d", tt.CursorSource(), tt.MarkCount())
	}
}

func TestTrackTableView(t *testing.T) {
	tt := NewTrackTable(Columns{Index: 4, Duration: 8}, "")
	rows := daftPunk()
	rows[4].Available = false
	tt.SetRows(rows)
	tt.SetAutoplay([]bool{false, false, false, false, false, true})
	tt.SetPlaying(3)
	act(tt, "move_down", "move_down")
	tt.ToggleMark(2)
	act(tt, "sort_title")
	st := testStyles(true, style.SelBar)
	out := tt.View(80, 9, st)
	lines := strings.Split(ansi.Strip(out), "\n")
	if len(lines) != 9 {
		t.Fatalf("%d lines", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 80 {
			t.Fatalf("line %d width %d: %q", i, w, l)
		}
	}
	want := []string{
		"#  Title ↑",                      // header with sort mark
		"▌●    3  Lose Yourself to Dance", // cursor + marked
		" ▶    4  Giorgio by Moroder",     // playing
		" ↻    6  One More Time",          // autoplay
		"sort: title ↑ · 1 marked",        // footer
		"      5  Around the World",       // unavailable
		"Random Access",                   // album column present at 80
	}
	joined := strings.Join(lines, "\n")
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Errorf("view lacks %q:\n%s", w, joined)
		}
	}
	if bg := style.BackgroundSGRs(out); len(bg) != 0 {
		t.Fatalf("transparent bar-mode table paints backgrounds: %v", bg)
	}
	// Narrow: album then artist columns drop, still exact width.
	for _, w := range []int{60, 40, 20, 8} {
		for _, l := range strings.Split(ansi.Strip(tt.View(w, 5, st)), "\n") {
			if ansi.StringWidth(l) != w {
				t.Fatalf("width %d: line %q", w, l)
			}
		}
	}
}

func TestTrackTableEmptyAndLoading(t *testing.T) {
	st := testStyles(false, style.SelSolid)
	tt := NewTrackTable(Columns{}, "This playlist is empty.")
	if !strings.Contains(ansi.Strip(tt.View(40, 4, st)), "This playlist is empty.") {
		t.Fatal("empty state missing")
	}
	tt.SetLoading(0, 1200)
	if !strings.Contains(ansi.Strip(tt.View(40, 4, st)), "Loading… 0/1,200") {
		t.Fatal("loading state missing")
	}
	tt.SetRows(daftPunk())
	tt.SetLoading(400, 1200)
	if !strings.Contains(ansi.Strip(tt.View(80, 10, st)), "Loading… 400/1,200") {
		t.Fatal("loading footer missing")
	}
	tt.SetFilter("zzz")
	if !strings.Contains(ansi.Strip(tt.View(80, 10, st)), "No track matches “zzz”.") {
		t.Fatal("no-match state missing")
	}
}

func TestTrackTableScrollAndHit(t *testing.T) {
	tt := NewTrackTable(Columns{}, "")
	tt.SetRows(manyTracks(100))
	st := testStyles(false, style.SelSolid)
	tt.View(80, 11, st) // body 10
	tt.Action(keys.Action{Name: "page_down"})
	if tt.Cursor() != 9 {
		t.Fatalf("page_down cursor %d", tt.Cursor())
	}
	tt.Action(keys.Action{Name: "move_down", Count: 5})
	out := ansi.Strip(tt.View(80, 11, st))
	if !strings.Contains(out, " 15  Song 15") || strings.Contains(out, " 1  Song 1 ") {
		t.Fatalf("not scrolled:\n%s", out)
	}
	if v := tt.HitRow(1); v != tt.top || tt.HitRow(0) != -1 || tt.HitRow(11) != -1 {
		t.Fatalf("HitRow: %d top %d", v, tt.top)
	}
	if !tt.InGutter(1, st) || tt.InGutter(2, st) {
		t.Fatal("gutter hit")
	}
	tt.Wheel(-3)
	if tt.Cursor() != 11 {
		t.Fatalf("wheel cursor %d", tt.Cursor())
	}
}

func TestFormatting(t *testing.T) {
	cases := map[string]string{
		FormatDuration(369 * time.Second):  "6:09",
		FormatDuration(3723 * time.Second): "1:02:03",
		FormatDuration(0):                  "",
		FormatCount(1200):                  "1,200",
		FormatCount(1234567):               "1,234,567",
		FormatCount(12):                    "12",
		FormatCount(-1200):                 "-1,200",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
	if !FuzzyMatch("Get Lucky", "glk") || FuzzyMatch("Get Lucky", "kg") {
		t.Fatal("fuzzy")
	}
}

func manyTracks(n int) []ytmusic.Track {
	out := make([]ytmusic.Track, n)
	for i := range out {
		out[i] = track(fmt.Sprint("id", i), fmt.Sprintf("Song %d", i+1), "Artist", "Album", 100+i)
	}
	return out
}

func BenchmarkTrackTableView1000(b *testing.B) {
	tt := NewTrackTable(Columns{Index: 4, Duration: 8}, "")
	tt.SetRows(manyTracks(1000))
	st := testStyles(false, style.SelSolid)
	tt.View(140, 40, st)
	b.ReportAllocs()
	for b.Loop() {
		tt.View(140, 40, st)
	}
}

func BenchmarkScroll1000(b *testing.B) {
	tt := NewTrackTable(Columns{Index: 4, Duration: 8}, "")
	tt.SetRows(manyTracks(1000))
	st := testStyles(false, style.SelSolid)
	tt.View(140, 40, st)
	down := keys.Action{Name: "move_down"}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if i%999 == 0 {
			tt.SetCursor(0)
		}
		tt.Action(down)
		tt.View(140, 40, st)
	}
}

func TestTrackTableHeaderClickHitsLabels(t *testing.T) {
	tt := NewTrackTable(Columns{}, "")
	tt.SetRows(daftPunk())
	st := testStyles(false, style.SelBar)
	head := strings.Split(ansi.Strip(tt.View(100, 10, st)), "\n")[0]
	for label, want := range map[string]SortKey{"Title": SortTitle, "Artist": SortArtist, "Album": SortAlbum, "Time": SortDuration} {
		tt.Action(keys.Action{Name: "sort_date"}) // reset to a different key
		i := strings.Index(head, label)
		if i < 0 {
			t.Fatalf("no %q in header %q", label, head)
		}
		x := ansi.StringWidth(head[:i])
		if r := tt.Click(x, 0, false, false, false); r != ClickSorted {
			t.Fatalf("%s: result %v", label, r)
		}
		if k, _ := tt.Sort(); k != want {
			t.Errorf("click on %q sorted by %v", label, k)
		}
	}
	if r := tt.Click(99, 0, true, false, false); r != ClickNone {
		t.Fatalf("right click on header: %v", r)
	}
}
