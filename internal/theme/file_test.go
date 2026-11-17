package theme

import (
	"strings"
	"testing"
)

func TestParseFile(t *testing.T) {
	data := "\xef\xbb\xbf" + `
[colors]
bg = "#101010"
FG = "#eeeeee"
accent = "ansi:4"
selected_item = "#333333"   # legacy alias
bogus = "#000000"
muted = "grey"

[overrides]
lyrics_current = "#ff0000"
`
	f, err := ParseFile([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if FormatColor(f.Colors.Bg) != "#101010" || FormatColor(f.Colors.Fg) != "#eeeeee" || FormatColor(f.Colors.Accent) != "ansi:4" {
		t.Errorf("colors = %s %s %s", FormatColor(f.Colors.Bg), FormatColor(f.Colors.Fg), FormatColor(f.Colors.Accent))
	}
	if FormatColor(f.Colors.SelectedBg) != "#333333" {
		t.Error("alias selected_item not mapped")
	}
	if f.Colors.Muted != nil {
		t.Error("invalid muted kept")
	}
	if FormatColor(f.Overrides.LyricsCurrent) != "#ff0000" {
		t.Error("override lost")
	}
	notes := strings.Join(f.Notes, "\n")
	for _, want := range []string{"selected_item is deprecated", "bogus: unknown token", "muted: invalid color"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes missing %q:\n%s", want, notes)
		}
	}
	th, err := FromFile(f, "mine", "file")
	if err != nil {
		t.Fatal(err)
	}
	ApplyOverrides(&th, f.Overrides)
	if FormatColor(th.LyricsCurrent) != "#ff0000" || th.Muted == nil || !th.Dark {
		t.Errorf("FromFile/overrides: lyrics=%s muted=%v dark=%v", FormatColor(th.LyricsCurrent), th.Muted, th.Dark)
	}
}

func TestParseFileErrors(t *testing.T) {
	if _, err := ParseFile([]byte("[colors\nbg=")); err == nil {
		t.Error("syntax error accepted")
	}
	f, _ := ParseFile([]byte("[colors]\nbg = \"#000000\"\n"))
	if _, err := FromFile(f, "x", "file"); err == nil || !strings.Contains(err.Error(), "fg, accent") {
		t.Errorf("missing required: %v", err)
	}
}
