package theme

import (
	"io/fs"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func fakeFS(files map[string]string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		if s, ok := files[p]; ok {
			return []byte(s), nil
		}
		return nil, fs.ErrNotExist
	}
}

func TestResolveSources(t *testing.T) {
	files := map[string]string{
		"/home/u/.cache/wal/colors.json": walA,
		"/cfg/matugen-colors.toml":       "mode = \"light\"\n[colors]\nsurface = \"#fafafa\"\non_surface = \"#111111\"\nprimary = \"#884400\"\n",
		"/cfg/theme.toml":                "[colors]\nbg = \"#000000\"\nfg = \"#ffffff\"\naccent = \"#00ff00\"\n[overrides]\nerror = \"#ff00ff\"\n",
	}
	base := Spec{ConfigDir: "/cfg", Home: "/home/u", Fallback: "nord", Name: "gruvbox-dark", ReadFile: fakeFS(files)}
	tests := []struct {
		source, wantName, wantBg string
		wantErr                  bool
	}{
		{"static", "gruvbox-dark", "#282828", false},
		{"pywal", "pywal", "#101010", false},
		{"matugen", "matugen", "#fafafa", false},
		{"file", "theme", "#000000", false},
		{"ansi", "ansi", "default", false},
	}
	for _, tt := range tests {
		s := base
		s.Source = tt.source
		r := Resolve(s)
		if (r.Err != nil) != tt.wantErr || r.Theme.Name != tt.wantName || FormatColor(r.Theme.Bg) != tt.wantBg {
			t.Errorf("%s: name=%s bg=%s err=%v", tt.source, r.Theme.Name, FormatColor(r.Theme.Bg), r.Err)
		}
		if FormatColor(r.Theme.Error) != "#ff00ff" {
			t.Errorf("%s: [overrides] not applied (error = %s)", tt.source, FormatColor(r.Theme.Error))
		}
	}
}

func TestResolveFallback(t *testing.T) {
	r := Resolve(Spec{Source: "pywal", Home: "/h", ConfigDir: "/c", Fallback: "nord", ReadFile: fakeFS(nil)})
	if r.Err == nil || r.Theme.Name != "nord" || !strings.Contains(r.Err.Error(), "/h/.cache/wal/colors.json") {
		t.Errorf("missing pywal: name=%s err=%v", r.Theme.Name, r.Err)
	}
	r = Resolve(Spec{Source: "static", Name: "nope", Fallback: "nope2", ReadFile: fakeFS(nil)})
	if r.Err == nil || r.Theme.Name != "ytm-dark" {
		t.Errorf("bad name and fallback: %s %v", r.Theme.Name, r.Err)
	}
}

func TestResolveContrastAgainst(t *testing.T) {
	files := map[string]string{"/c/theme.toml": "[colors]\nbg = \"#ffffff\"\nfg = \"#222222\"\naccent = \"#3355ff\"\n"}
	s := Spec{Source: "file", ConfigDir: "/c", EnforceContrast: true, ReadFile: fakeFS(files)}
	if r := Resolve(s); r.Against != "#ffffff" {
		t.Errorf("opaque against = %s", r.Against)
	}
	// Transparent with a static/file theme: the terminal's bg, else black.
	s.Transparent = true
	r := Resolve(s)
	if r.Against != "#000000" {
		t.Errorf("transparent, unknown terminal: against = %s", r.Against)
	}
	if c, _ := RGB(r.Theme.Fg); Contrast(c, mustRGBTest(lipgloss.Color("#000000"))) < MinTextContrast {
		t.Error("fg not raised against assumed black terminal")
	}
	s.TerminalBg = lipgloss.Color("#fdf6e3")
	if r := Resolve(s); r.Against != "#fdf6e3" {
		t.Errorf("transparent, OSC 11: against = %s", r.Against)
	}
	// ansi never adjusted.
	if r := Resolve(Spec{Source: "ansi", EnforceContrast: true, ReadFile: fakeFS(nil)}); r.Against != "" || len(r.Adjustments) != 0 {
		t.Error("ansi adjusted")
	}
}

func TestParseCOLORFGBG(t *testing.T) {
	for in, want := range map[string]string{"15;0": "#000000", "0;15": "#ffffff", "7;default;0": "#000000", "x": "", "1;99": ""} {
		got := ParseCOLORFGBG(in)
		if FormatColor(got) != want {
			t.Errorf("ParseCOLORFGBG(%q) = %s, want %q", in, FormatColor(got), want)
		}
	}
}
