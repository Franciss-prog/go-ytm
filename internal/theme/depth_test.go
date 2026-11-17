package theme

import (
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucasb-eyer/go-colorful"
)

func TestNearest(t *testing.T) {
	tests := []struct {
		hex  string
		n256 ansi.IndexedColor
		n16  ansi.BasicColor
	}{
		{"#000000", 16, 0},
		{"#ffffff", 231, 15},
		{"#ff0000", 196, 9},
		{"#ff4e45", 203, 9},
		{"#0f0f0f", 233, 0},
		{"#808080", 244, 8},
		{"#5f87af", 67, 8},
		{"#00cdcd", 44, 6},
	}
	for _, tt := range tests {
		c, _ := colorful.Hex(tt.hex)
		if got := Nearest256(c); got != tt.n256 {
			t.Errorf("Nearest256(%s) = %d, want %d", tt.hex, got, tt.n256)
		}
		if got := Nearest16(c, nil); got != tt.n16 {
			t.Errorf("Nearest16(%s) = %d, want %d", tt.hex, got, tt.n16)
		}
	}
	if xterm256(196).Hex() != "#ff0000" || xterm256(232).Hex() != "#080808" || xterm256(255).Hex() != "#eeeeee" {
		t.Error("xterm256 table")
	}
}

func TestDownsample(t *testing.T) {
	th, _ := Builtin("ytm-dark")
	tc := Downsample(th, colorprofile.TrueColor)
	if FormatColor(tc.Accent) != "#ff4e45" {
		t.Error("truecolor changed tokens")
	}
	d256 := Downsample(th, colorprofile.ANSI256)
	if FormatColor(d256.Accent) != "ansi256:203" {
		t.Errorf("256 accent = %s", FormatColor(d256.Accent))
	}
	d16 := Downsample(th, colorprofile.ANSI)
	if FormatColor(d16.Accent) != "ansi:9" {
		t.Errorf("16 accent = %s", FormatColor(d16.Accent))
	}
	if FormatColor(th.Accent) != "#ff4e45" {
		t.Error("Downsample mutated its input")
	}
	a := ANSITheme(5)
	if got := Downsample(a, colorprofile.ANSI); FormatColor(got.Accent) != "ansi:5" || !IsDefault(got.Bg) {
		t.Error("ansi theme altered")
	}
	if ParseDepth("256", colorprofile.TrueColor) != colorprofile.ANSI256 || ParseDepth("auto", colorprofile.ANSI) != colorprofile.ANSI {
		t.Error("ParseDepth")
	}
}

func TestANSITheme(t *testing.T) {
	a := ANSITheme(99)
	if a.Accent != ansi.Blue || !a.IsReverseSelection() {
		t.Error("ANSITheme defaults")
	}
	for n, c := range a.Tokens() {
		if _, ok := RGB(c); ok {
			t.Errorf("ansi theme token %s is RGB", n)
		}
	}
}
