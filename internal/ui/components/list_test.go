package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Franciss-Prog/go-ytm/internal/keys"
	"github.com/Franciss-Prog/go-ytm/internal/style"
)

func wrapList() *List {
	l := NewList("empty")
	l.SetWrap(true)
	l.SetItems([]Item{
		{Left: "A very long playlist name that wraps", Right: "87", Key: "a"},
		{Left: "Short", Right: "3", Key: "b"},
		{Left: "Another long playlist title here", Key: "c"},
		{Left: "Last", Right: "1,204", Key: "d"},
	})
	return l
}

func TestListTruncatesByDefault(t *testing.T) {
	l := NewList("empty")
	l.SetItems([]Item{{Left: "A very long playlist name that wraps", Right: "87"}})
	lines := strings.Split(ansi.Strip(l.View(20, 3, testStyles(false, style.SelBar), true)), "\n")
	if !strings.Contains(lines[0], "…") || strings.TrimSpace(lines[1]) != "" {
		t.Fatalf("not truncated: %q", lines)
	}
}

func TestListWrapRowsAndHits(t *testing.T) {
	st := testStyles(false, style.SelBar)
	l := wrapList()
	out := ansi.Strip(l.View(20, 10, st, true))
	lines := strings.Split(out, "\n")
	for i, ln := range lines {
		if w := ansi.StringWidth(ln); w != 20 {
			t.Fatalf("line %d width %d: %q", i, w, ln)
		}
	}
	if strings.Contains(out, "…") {
		t.Fatalf("wrap mode truncated:\n%s", out)
	}
	if !strings.Contains(out, "87") || !strings.Contains(out, "1,204") {
		t.Fatalf("details missing:\n%s", out)
	}
	// Item 0 spans several rows; the row after it hits item 1.
	h0 := l.rowsOf(0)
	if h0 < 2 {
		t.Fatalf("item 0 has %d rows", h0)
	}
	for y, want := range map[int]int{0: 0, h0 - 1: 0, h0: 1, 99: -1} {
		if got := l.HitRow(y); got != want {
			t.Errorf("HitRow(%d) = %d, want %d", y, got, want)
		}
	}
}

func TestListWrapScrollKeepsCursorVisible(t *testing.T) {
	st := testStyles(false, style.SelBar)
	l := wrapList()
	l.View(20, 3, st, true)
	l.Action(keys.Action{Name: "go_bottom"})
	out := ansi.Strip(l.View(20, 3, st, true))
	if !strings.Contains(out, "Last") {
		t.Fatalf("cursor item off screen:\n%s", out)
	}
	l.Action(keys.Action{Name: "go_top"})
	out = ansi.Strip(l.View(20, 3, st, true))
	if !strings.HasPrefix(strings.TrimLeft(out, " ▌"), "A very") {
		t.Fatalf("top item not shown:\n%s", out)
	}
}

func TestListSelectKey(t *testing.T) {
	l := wrapList()
	if !l.SelectKey("c") || l.Cursor() != 2 || l.SelectKey("zz") {
		t.Fatalf("cursor %d", l.Cursor())
	}
}
