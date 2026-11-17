package keys

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/config"
)

func TestParseKey(t *testing.T) {
	tests := []struct{ in, want, err string }{
		{in: "a", want: "a"},
		{in: "A", want: "A"},
		{in: "S-a", want: "A"},
		{in: "C-a", want: "C-a"},
		{in: "C-A", want: "C-S-a"},
		{in: "c-x", want: "C-x"},
		{in: "S-C-tab", want: "C-S-tab"},
		{in: "M-x", want: "M-x"},
		{in: "esc", want: "escape"},
		{in: "PgDown", want: "page_down"},
		{in: "page_up", want: "page_up"},
		{in: "F12", want: "f12"},
		{in: "S-backspace", want: "S-backspace"},
		{in: "?", want: "?"},
		{in: "-", want: "-"},
		{in: "+", want: "+"},
		{in: "^", want: "^"},
		{in: "é", want: "é"},
		{in: "", err: "empty"},
		{in: "X-a", err: "modifier"},
		{in: "f13", err: "unknown key"},
		{in: "ctrl+a", err: "unknown key"},
	}
	for _, tt := range tests {
		got, err := ParseKey(tt.in)
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("%q: err = %v, want %q", tt.in, err, tt.err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("%q: got %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

func FuzzParseKey(f *testing.F) {
	for _, s := range []string{"a", "C-a", "S-tab", "g l", "C-S-M-x", "f1", "--", "S-"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		k, err := ParseKey(s)
		if err != nil {
			return
		}
		// Canonical forms are fixed points.
		again, err := ParseKey(k)
		if err != nil || again != k {
			t.Fatalf("%q → %q → %q, %v", s, k, again, err)
		}
	})
}

func TestFromTea(t *testing.T) {
	tests := []struct {
		k    tea.Key
		want string
	}{
		{tea.Key{Code: tea.KeySpace, Text: " "}, "space"},
		{tea.Key{Code: tea.KeyEnter}, "enter"},
		{tea.Key{Code: tea.KeyEscape}, "escape"},
		{tea.Key{Code: 'a', Mod: tea.ModCtrl}, "C-a"},
		{tea.Key{Code: tea.KeyTab, Mod: tea.ModShift}, "S-tab"},
		{tea.Key{Code: tea.KeyBackspace, Mod: tea.ModShift}, "S-backspace"},
		{tea.Key{Code: tea.KeyPgDown}, "page_down"},
		{tea.Key{Code: tea.KeyPgUp}, "page_up"},
		{tea.Key{Code: 'a', ShiftedCode: 'A', Text: "A", Mod: tea.ModShift}, "A"},
		{tea.Key{Code: 'x', Mod: tea.ModAlt}, "M-x"},
		{tea.Key{Code: '/', Text: "?", ShiftedCode: '?', Mod: tea.ModShift}, "?"},
		{tea.Key{Code: '+', Text: "+"}, "+"},
		{tea.Key{Code: tea.KeyF5}, "f5"},
		{tea.Key{Code: tea.KeyDelete}, "delete"},
		{tea.Key{Code: 'j', Text: "j"}, "j"},
		{tea.Key{Code: '5', Text: "5"}, "5"},
	}
	for _, tt := range tests {
		got := FromTea(tt.k)
		if got != tt.want {
			t.Errorf("%q: got %q, want %q", tt.k.Keystroke(), got, tt.want)
		}
		if _, err := ParseKey(got); err != nil {
			t.Errorf("%q: FromTea gave non-canonical %q: %v", tt.k.Keystroke(), got, err)
		}
	}
}

func TestDefaultKeymapClean(t *testing.T) {
	_, diags := build(nil, "")
	if len(diags) != 0 {
		t.Fatalf("default keymap has diagnostics: %v", diags)
	}
	km := Default()
	for _, a := range Actions {
		if got := km.Bindings(a.Name); len(got) != len(a.Defaults) {
			t.Errorf("%s: bindings %v, defaults %v", a.Name, got, a.Defaults)
		}
	}
}

func feedAll(d *Dispatcher, keys ...string) []Result {
	var out []Result
	for _, k := range keys {
		out = append(out, d.Feed(k))
	}
	return out
}

func last(rs []Result) Result { return rs[len(rs)-1] }

func TestDispatch(t *testing.T) {
	tests := []struct {
		keys    []string
		action  string
		count   int
		unbound string
	}{
		{keys: []string{"j"}, action: "move_down"},
		{keys: []string{"g", "l"}, action: "library"},
		{keys: []string{"g", "g"}, action: "go_top"},
		{keys: []string{"1", "5", "J"}, action: "reorder_down", count: 15},
		{keys: []string{"5", "g", "g"}, action: "go_top", count: 5},
		{keys: []string{"3", "n"}, action: "next_track"}, // not counted: count dropped
		{keys: []string{"C-w", "h"}, action: "focus_pane_left"},
		{keys: []string{"d", "d"}, action: "delete_item"},
		{keys: []string{"s", "A"}, action: "sort_album"},
		{keys: []string{"g", "j"}, action: "move_down"}, // chord cancelled, j processed
		{keys: []string{"0"}, unbound: "0"},
		{keys: []string{"1", "0", "j"}, action: "move_down", count: 10},
		{keys: []string{"9", "9", "9", "9", "9", "j"}, action: "move_down", count: MaxCount},
		{keys: []string{"x"}, unbound: "x"},
	}
	for _, tt := range tests {
		d := NewDispatcher(Default())
		r := last(feedAll(d, tt.keys...))
		if tt.unbound != "" {
			if r.Unbound != tt.unbound || r.Action != nil {
				t.Errorf("%v: got %+v", tt.keys, r)
			}
			continue
		}
		if r.Action == nil || r.Action.Name != tt.action || r.Action.Count != tt.count {
			t.Errorf("%v: got %+v, want %s×%d", tt.keys, r.Action, tt.action, tt.count)
		}
		if d.Pending() != "" {
			t.Errorf("%v: pending %q left over", tt.keys, d.Pending())
		}
	}
}

func TestDispatchPendingAndTimeout(t *testing.T) {
	d := NewDispatcher(Default())
	if r := d.Feed("1"); !r.Pending {
		t.Fatal("count not pending")
	}
	d.Feed("5")
	d.Feed("g")
	if got := d.Pending(); got != "15g-" {
		t.Errorf("pending = %q", got)
	}
	stale := d.Seq() - 1
	d.Timeout(stale) // an older timer must not cancel the newer state
	if d.Pending() == "" {
		t.Fatal("stale timeout cleared state")
	}
	d.Timeout(d.Seq())
	if d.Pending() != "" {
		t.Fatal("timeout did not clear")
	}
	if r := d.Feed("l"); r.Action == nil || r.Action.Name != "like_toggle" {
		t.Errorf("after timeout, l = %+v", r)
	}
}

func TestLoadOverrides(t *testing.T) {
	src := `
[keys]
like_toggle = ["C-l"]
toggle_transparency = []
next_track = "N"
quit = ["q", "C-c"]
`
	km, diags := Load([]byte(src), "keymap.toml")
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	d := NewDispatcher(km)
	check := func(key, want string) {
		t.Helper()
		r := d.Feed(key)
		got := ""
		if r.Action != nil {
			got = r.Action.Name
		}
		if got != want {
			t.Errorf("%s → %q, want %q", key, got, want)
		}
	}
	check("C-l", "like_toggle")
	check("l", "") // default replaced
	check("C-t", "")
	check("N", "next_track")
	check("n", "")
	check("C-c", "quit")
	if got := km.Bindings("toggle_transparency"); len(got) != 0 {
		t.Errorf("unbound action still has %v", got)
	}
}

func TestLoadDiagnostics(t *testing.T) {
	src := `[keys]
like_togle = ["x"]
play_pause = ["C-"]
volume_up = 3
mute = ["g"]
library = ["Q"]
help = ["Q"]

[bogus]
`
	_, diags := Load([]byte(src), "keymap.toml")
	want := map[string]string{
		"keys.like_togle": `unknown action (did you mean "like_toggle"?)`,
		"keys.play_pause": `invalid binding "C-"`,
		"keys.volume_up":  "expected a string or a list",
		"keys.mute":       `"g" is a prefix of`,
		"keys.help":       `"Q" is also bound to library; help wins`,
		"bogus":           "unknown section",
	}
	for key, frag := range want {
		found := slices.ContainsFunc(diags, func(d config.Diagnostic) bool {
			return d.Key == key && strings.Contains(d.Message, frag) && d.Line > 0
		})
		if !found {
			t.Errorf("no diagnostic for %s containing %q in:\n%v", key, frag, diags)
		}
	}
}

func TestLoadUserReplacesDefaultBinding(t *testing.T) {
	km, diags := Load([]byte("[keys]\nhelp = [\"n\"]\n"), "k.toml")
	if len(diags) != 1 || diags[0].Severity != config.SevInfo || !strings.Contains(diags[0].Message, "next_track") {
		t.Fatalf("diags = %v", diags)
	}
	if r := NewDispatcher(km).Feed("n"); r.Action == nil || r.Action.Name != "help" {
		t.Errorf("n → %+v", r.Action)
	}
}

func TestLoadSyntaxError(t *testing.T) {
	km, diags := Load([]byte("[keys\n"), "k.toml")
	if len(diags) != 1 || diags[0].Severity != config.SevError || diags[0].Line != 1 {
		t.Fatalf("diags = %v", diags)
	}
	if r := NewDispatcher(km).Feed("j"); r.Action == nil {
		t.Error("defaults not used after syntax error")
	}
}
