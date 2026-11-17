package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestPatchBytes(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		changes []Change
		want    string
	}{
		{
			name:    "replace keeps comments and spacing",
			in:      "# top\n[theme]\n# why\ntransparent = false   # trailing\n\nsource = \"static\"\n",
			changes: []Change{{"theme.transparent", true}, {"theme.source", "pywal"}},
			want:    "# top\n[theme]\n# why\ntransparent = true   # trailing\n\nsource = \"pywal\"\n",
		},
		{
			name:    "literal string and no spaces",
			in:      "[theme]\nname='nord'\n",
			changes: []Change{{"theme.name", "gruvbox-dark"}},
			want:    "[theme]\nname=\"gruvbox-dark\"\n",
		},
		{
			name:    "missing key appended to its table, before the next table",
			in:      "[theme]\nsource = \"static\"\n\n[ui]\nmouse = true\n",
			changes: []Change{{"theme.popup_fill", true}},
			want:    "[theme]\nsource = \"static\"\npopup_fill = true\n\n[ui]\nmouse = true\n",
		},
		{
			name:    "empty table",
			in:      "[theme] # mine\n[ui]\n",
			changes: []Change{{"theme.watch", false}},
			want:    "[theme] # mine\nwatch = false\n[ui]\n",
		},
		{
			name:    "missing table created at EOF",
			in:      "[ui]\nmouse = true",
			changes: []Change{{"theme.transparent", true}},
			want:    "[ui]\nmouse = true\n\n[theme]\ntransparent = true\n",
		},
		{
			name:    "empty file",
			in:      "",
			changes: []Change{{"theme.transparent", true}},
			want:    "[theme]\ntransparent = true\n",
		},
		{
			name:    "dotted key at root",
			in:      "theme.transparent = false # x\n",
			changes: []Change{{"theme.transparent", true}},
			want:    "theme.transparent = true # x\n",
		},
		{
			name:    "unrelated inline table and arrays kept",
			in:      "[playback]\nmpv_args = [\"--a\", \"--b\"]\n[theme]\npywal_map = { accent = \"color5\" }\ntransparent = false\n",
			changes: []Change{{"theme.transparent", true}, {"playback.mpv_args", []string{"--c"}}},
			want:    "[playback]\nmpv_args = [\"--c\"]\n[theme]\npywal_map = { accent = \"color5\" }\ntransparent = true\n",
		},
		{
			name:    "CRLF preserved",
			in:      "[theme]\r\ntransparent = false\r\n",
			changes: []Change{{"theme.transparent", true}, {"theme.watch", false}},
			want:    "[theme]\r\ntransparent = true\r\nwatch = false\r\n",
		},
		{
			name:    "integer",
			in:      "[theme]\nansi_accent = 4\n",
			changes: []Change{{"theme.ansi_accent", 5}},
			want:    "[theme]\nansi_accent = 5\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PatchBytes([]byte(tt.in), tt.changes)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tt.want)
			}
			cfg, diags := Load(got, "x")
			if diags.Count(SevError) > 0 {
				t.Errorf("patched file has errors: %v (%v)", diags, cfg.Theme)
			}
		})
	}
}

func TestPatchBytesErrors(t *testing.T) {
	for _, tt := range []struct {
		in  string
		key string
	}{
		{"theme = { transparent = false }\n", "theme.transparent"},
		{"[theme\n", "theme.transparent"},
		{"", "transparent"},
	} {
		if _, err := PatchBytes([]byte(tt.in), []Change{{tt.key, true}}); err == nil {
			t.Errorf("PatchBytes(%q, %s): want error", tt.in, tt.key)
		}
	}
	if _, err := PatchBytes(nil, []Change{{"theme.x", 1.5}}); err == nil {
		t.Error("float accepted")
	}
}

func TestPatchFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.toml")
	if err := Patch(path, []Change{{"theme.transparent", true}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Patch(path, []Change{{"theme.transparent", false}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "[theme]\ntransparent = false\n" {
		t.Errorf("content %q", b)
	}
	fi, _ := os.Stat(path)
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o640 {
		t.Errorf("mode %v, want 0640 kept", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("temp files left: %v", entries)
	}
	// A failing patch leaves the file alone.
	if err := os.WriteFile(path, []byte("theme = { transparent = true }\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Patch(path, []Change{{"theme.transparent", false}}); err == nil || !strings.Contains(err.Error(), "inline table") {
		t.Errorf("err = %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "theme = { transparent = true }\n" {
		t.Errorf("file changed on error: %q", b)
	}
}

func TestQuoteRoundTrip(t *testing.T) {
	for _, s := range []string{"", "plain", `a "q" \ b`, "tab\tnl\ncr\r", "\x00\x1b\x7f", "ünï — ✓", `C:\path\x`} {
		out, err := PatchBytes(nil, []Change{{"ui.region", s}})
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		var v struct {
			UI struct {
				Region string `toml:"region"`
			} `toml:"ui"`
		}
		if err := toml.Unmarshal(out, &v); err != nil || v.UI.Region != s {
			t.Errorf("%q round-tripped to %q (%v)", s, v.UI.Region, err)
		}
	}
}

// SettingsKeys are the keys the Settings popup persists (spec 08).
var settingsChanges = []Change{
	{"theme.source", "matugen"}, {"theme.name", "nord"}, {"theme.transparent", true},
	{"theme.transparent_selection", "underline"}, {"theme.popup_fill", true},
	{"ui.album_art_mode", "off"}, {"theme.enforce_contrast", false}, {"theme.watch", false},
}

func TestPatchEverySettingsKey(t *testing.T) {
	defaults, err := DefaultsTOML()
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{"defaults": defaults, "empty": nil, "comments only": []byte("# my config\n")}
	for name, in := range inputs {
		for _, c := range settingsChanges {
			out, err := PatchBytes(in, []Change{c})
			if err != nil {
				t.Fatalf("%s %s: %v", name, c.Key, err)
			}
			cfg, diags := Load(out, "x")
			if len(diags) > 0 {
				t.Errorf("%s %s: diagnostics %v", name, c.Key, diags)
			}
			want, got := Default(), cfg
			section, key, _ := strings.Cut(c.Key, ".")
			v, ok := fieldByTag(reflect.ValueOf(&want).Elem(), section)
			if !ok {
				t.Fatalf("no section %s", section)
			}
			f, ok := fieldByTag(v, key)
			if !ok {
				t.Fatalf("no key %s", c.Key)
			}
			f.Set(reflect.ValueOf(c.Value))
			if !reflect.DeepEqual(want, got) {
				t.Errorf("%s %s: loaded config differs from defaults+change", name, c.Key)
			}
		}
	}
}
