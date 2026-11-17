package theme

import (
	"image/color"
	"math/rand"
	"testing"
	"testing/quick"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucasb-eyer/go-colorful"
)

func TestParseColor(t *testing.T) {
	tests := []struct {
		in   string
		want string // FormatColor of the result; "" = error
	}{
		{"#ff4e45", "#ff4e45"},
		{"#FF4E45", "#ff4e45"},
		{" #abc ", "#aabbcc"},
		{"ansi:0", "ansi:0"},
		{"ANSI:15", "ansi:15"},
		{"default", "default"},
		{"ansi:16", ""},
		{"ansi:-1", ""},
		{"#ggg", ""},
		{"#12345", ""},
		{"red", ""},
		{"", ""},
	}
	for _, tt := range tests {
		c, err := ParseColor(tt.in)
		if tt.want == "" {
			if err == nil {
				t.Errorf("ParseColor(%q) = %v, want error", tt.in, c)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseColor(%q): %v", tt.in, err)
			continue
		}
		if got := FormatColor(c); got != tt.want {
			t.Errorf("ParseColor(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestRGBUnknown(t *testing.T) {
	for _, c := range []color.Color{nil, Default, ansi.Red, ansi.IndexedColor(200)} {
		if _, ok := RGB(c); ok {
			t.Errorf("RGB(%T) reported a concrete color", c)
		}
	}
	if _, ok := RGB(lipgloss.Color("#123456")); !ok {
		t.Error("RGB(hex) not concrete")
	}
}

func TestContrastKnown(t *testing.T) {
	w, k := colorful.Color{R: 1, G: 1, B: 1}, colorful.Color{}
	if got := Contrast(w, k); got < 20.99 || got > 21.01 {
		t.Errorf("white/black = %.3f, want 21", got)
	}
	if got := Contrast(k, k); got != 1 {
		t.Errorf("black/black = %.3f, want 1", got)
	}
	// #767676 on white is the classic 4.54:1.
	g, _ := colorful.Hex("#767676")
	if got := Contrast(g, w); got < 4.53 || got > 4.55 {
		t.Errorf("#767676/white = %.3f, want 4.54", got)
	}
}

func TestIsDarkBg(t *testing.T) {
	for in, want := range map[string]bool{"#000000": true, "#0f0f0f": true, "#ffffff": false, "#eceff4": false, "#808080": false, "#5a5a5a": true} {
		c, _ := ParseColor(in)
		if got := IsDarkBg(c); got != want {
			t.Errorf("IsDarkBg(%s) = %v, want %v", in, got, want)
		}
	}
	if !IsDarkBg(Default) {
		t.Error("unknown bg should count as dark")
	}
}

func TestMixUnknownKeepsA(t *testing.T) {
	a := lipgloss.Color("#102030")
	if got := Mix(a, Default, 0.5); got != a {
		t.Errorf("Mix with default = %v, want a", got)
	}
	got := FormatColor(Mix(lipgloss.Color("#000000"), lipgloss.Color("#ffffff"), 0.5))
	if got == "#000000" || got == "#ffffff" {
		t.Errorf("Mix black/white 50%% = %s", got)
	}
}

func TestCompleteFillsEverything(t *testing.T) {
	for _, base := range []Theme{
		{Bg: lipgloss.Color("#0f0f0f"), Fg: lipgloss.Color("#f1f1f1"), Accent: lipgloss.Color("#ff4e45")},
		{Bg: lipgloss.Color("#fafafa"), Fg: lipgloss.Color("#202020"), Accent: lipgloss.Color("#0066cc")},
		{}, // nothing known: terminal colors
	} {
		th := base
		Complete(&th)
		for name, c := range th.Tokens() {
			if c == nil {
				t.Errorf("%v: token %s nil after Complete", FormatColor(base.Bg), name)
			}
		}
	}
	light := Theme{Bg: lipgloss.Color("#fafafa"), Fg: lipgloss.Color("#202020")}
	Complete(&light)
	if light.Dark {
		t.Error("light bg marked dark")
	}
}

type colorTriple struct{ Bg, Fg, Accent colorful.Color }

func (colorTriple) Generate(r *rand.Rand, _ int) colorTriple {
	rc := func() colorful.Color {
		return quant(colorful.Color{R: r.Float64(), G: r.Float64(), B: r.Float64()})
	}
	return colorTriple{rc(), rc(), rc()}
}

// TestContrastProperty: for random palettes every enforced token meets its
// ratio (or is the documented black/white fallback), bg is untouched, and
// hue is kept within 2° wherever the result still has visible chroma.
func TestContrastProperty(t *testing.T) {
	cfg := &quick.Config{MaxCount: 3000, Rand: rand.New(rand.NewSource(1))}
	prop := func(p colorTriple) bool {
		th := Theme{Bg: toColor(p.Bg), Fg: toColor(p.Fg), Accent: toColor(p.Accent)}
		Complete(&th)
		out, adj := EnforceContrast(th, th.Bg)
		if FormatColor(out.Bg) != FormatColor(th.Bg) {
			t.Logf("bg changed")
			return false
		}
		bg := mustRGBTest(out.Bg)
		check := func(name string, c color.Color, against colorful.Color, target float64) bool {
			got := Contrast(mustRGBTest(c), against)
			if got+1e-9 < target {
				t.Logf("%s on %s: %s contrast %.3f < %.1f", name, against.Hex(), FormatColor(c), got, target)
				return false
			}
			return true
		}
		ok := check("fg", out.Fg, bg, MinTextContrast) &&
			check("accent", out.Accent, bg, MinAccentContrast) &&
			check("muted", out.Muted, bg, MinAccentContrast) &&
			check("error", out.Error, bg, MinAccentContrast) &&
			check("selected_fg", out.SelectedFg, mustRGBTest(out.SelectedBg), MinAccentContrast)
		if !ok {
			return false
		}
		for _, a := range adj {
			if a.Fallback {
				continue
			}
			from, _ := colorful.Hex(a.From)
			to, _ := colorful.Hex(a.To)
			_, c1, h1 := from.OkLch()
			_, c2, h2 := to.OkLch()
			if c1 > 0.05 && c2 > 0.05 && hueDist(h1, h2) > 2 {
				t.Logf("%s hue %.1f → %.1f", a.Token, h1, h2)
				return false
			}
		}
		return true
	}
	if err := quick.Check(prop, cfg); err != nil {
		t.Fatal(err)
	}
}

func TestEnforceContrastSkipsUnknown(t *testing.T) {
	th := Theme{Bg: Default, Fg: Default, Accent: ansi.Blue}
	Complete(&th)
	out, adj := EnforceContrast(th, th.Bg)
	if len(adj) != 0 || out.Accent != ansi.Blue {
		t.Errorf("unknown bg adjusted: %v", adj)
	}
}

func TestEnforceContrastNoopWhenFine(t *testing.T) {
	th, _ := Builtin("ytm-dark")
	if _, adj := EnforceContrast(th, th.Bg); len(adj) != 0 {
		t.Errorf("ytm-dark adjusted: %v", adj)
	}
}

func mustRGBTest(c color.Color) colorful.Color { r, _ := RGB(c); return r }

func BenchmarkEnforceContrast(b *testing.B) {
	th := Theme{Bg: lipgloss.Color("#3a3a3a"), Fg: lipgloss.Color("#555555"), Accent: lipgloss.Color("#444488")}
	Complete(&th)
	for b.Loop() {
		EnforceContrast(th, th.Bg)
	}
}
