package theme

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func goldenTheme(t *testing.T, path string, th Theme, notes []string) {
	t.Helper()
	enforced, adj := EnforceContrast(th, th.Bg)
	var b bytes.Buffer
	b.Write(th.MarshalTOML())
	for _, n := range notes {
		fmt.Fprintf(&b, "# note: %s\n", n)
	}
	fmt.Fprintf(&b, "\n# after enforce_contrast (%d adjustments)\n", len(adj))
	for _, a := range adj {
		fmt.Fprintf(&b, "# %s\n", a)
	}
	b.WriteString(strings.ReplaceAll(string(enforced.MarshalTOML()), "[colors]", "[enforced]"))
	if *update {
		if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if !bytes.Equal(want, b.Bytes()) {
		t.Errorf("%s mismatch:\n--- got\n%s\n--- want\n%s", path, b.Bytes(), want)
	}
}

func TestPywalGolden(t *testing.T) {
	files, _ := filepath.Glob("testdata/pywal/*.json")
	if len(files) < 5 {
		t.Fatalf("want ≥ 5 pywal fixtures, have %d", len(files))
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			p, err := ParsePywal(data)
			if err != nil {
				t.Fatal(err)
			}
			th, notes := FromPywal(p, nil)
			goldenTheme(t, strings.TrimSuffix(f, ".json")+".golden.toml", th, notes)
		})
	}
}

func TestMatugenGolden(t *testing.T) {
	for _, mode := range []string{"dark", "light"} {
		t.Run(mode, func(t *testing.T) {
			data, err := os.ReadFile("testdata/matugen/" + mode + ".toml")
			if err != nil {
				t.Fatal(err)
			}
			m, notes, err := ParseMatugen(data)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range MatugenRoles {
				if m.Roles[r] == nil {
					t.Errorf("fixture lacks role %s", r)
				}
			}
			th := FromMatugen(m)
			if th.Dark != (mode == "dark") {
				t.Errorf("Dark = %v", th.Dark)
			}
			goldenTheme(t, "testdata/matugen/"+mode+".golden.toml", th, notes)
		})
	}
}

func TestPywalSparse(t *testing.T) {
	data, _ := os.ReadFile("testdata/pywal/matugen-pywalfox-sparse.json")
	p, err := ParsePywal(data)
	if err != nil {
		t.Fatal(err)
	}
	th, _ := FromPywal(p, nil)
	for tok, want := range map[string]string{"bg": "#1a1110", "fg": "#f1dfdc", "accent": "#ffb4a8"} {
		if got := FormatColor(*th.Token(tok)); got != want {
			t.Errorf("%s = %s, want %s", tok, got, want)
		}
	}
	if p.Alpha != "100" || !th.Dark {
		t.Errorf("alpha=%q dark=%v", p.Alpha, th.Dark)
	}
}

func TestPywalEdgeCases(t *testing.T) {
	tests := []struct {
		name, data string
		wantErr    bool
	}{
		{"empty file", "", true},
		{"half written", `{"colors": {"color0": "#000`, true},
		{"trailing garbage", `{"colors":{"color0":"#000000"}} xyz`, true},
		{"alpha only", `{"alpha": 90}`, true},
		{"all empty", `{"colors":{"color0":"","color15":""}}`, true},
		{"fg only", `{"colors":{"color7":"#cccccc"}}`, false},
		{"numeric alpha", `{"alpha": 90, "special":{"background":"#111111"}}`, false},
	}
	for _, tt := range tests {
		_, err := ParsePywal([]byte(tt.data))
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tt.name, err, tt.wantErr)
		}
	}
	_, err := ParsePywal([]byte(`{}`))
	if !errors.Is(err, ErrNoColors) {
		t.Errorf("{} error = %v, want ErrNoColors", err)
	}
}

func TestPywalMap(t *testing.T) {
	data, _ := os.ReadFile("testdata/pywal/full-16.json")
	p, _ := ParsePywal(data)
	th, notes := FromPywal(p, map[string]string{"accent": "color5", "muted": "foreground", "nope": "color1", "error": "color99"})
	if FormatColor(th.Accent) != FormatColor(p.Colors[5]) {
		t.Errorf("accent = %s, want color5 %s", FormatColor(th.Accent), FormatColor(p.Colors[5]))
	}
	if FormatColor(th.Muted) != FormatColor(th.Fg) {
		t.Error("muted ← foreground not applied")
	}
	if len(notes) != 2 {
		t.Errorf("notes = %v, want 2 (unknown token, bad reference)", notes)
	}
}

func TestPywalAccentLowSaturation(t *testing.T) {
	data, _ := os.ReadFile("testdata/pywal/low-saturation.json")
	p, _ := ParsePywal(data)
	th, _ := FromPywal(p, nil)
	// All greys: color4 is chosen even though it misses 3:1 (contrast raises it later).
	if FormatColor(th.Accent) != FormatColor(p.Colors[4]) {
		t.Errorf("accent = %s, want color4", FormatColor(th.Accent))
	}
}

func TestMatugenErrors(t *testing.T) {
	if _, _, err := ParseMatugen([]byte(`mode = "dark"`)); !errors.Is(err, ErrNoColors) {
		t.Errorf("no colors: %v", err)
	}
	m, notes, err := ParseMatugen([]byte("mode = \"{{mode}}\"\n[colors]\nsurface = \"#fafafa\"\nprimary = \"{{colors.primary.default.hex}}\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 {
		t.Errorf("notes = %v", notes)
	}
	if th := FromMatugen(m); th.Dark || th.Accent == nil {
		t.Errorf("guessing mode from surface: dark=%v accent=%v", th.Dark, th.Accent)
	}
}
