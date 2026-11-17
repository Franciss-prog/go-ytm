package style

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"github.com/Franciss-Prog/go-ytm/internal/theme"
)

func TestDecide(t *testing.T) {
	tests := []struct {
		name   string
		in     PolicyInput
		styled bool
	}{
		{"tty", PolicyInput{IsTTY: true, Env: []string{"TERM=xterm-256color"}}, true},
		{"piped", PolicyInput{IsTTY: false, Env: []string{"TERM=xterm-256color"}}, false},
		{"json on tty", PolicyInput{IsTTY: true, JSON: true, Env: []string{"TERM=xterm"}}, false},
		{"no-color flag", PolicyInput{IsTTY: true, NoColor: true, Env: []string{"TERM=xterm"}}, false},
		{"NO_COLOR", PolicyInput{IsTTY: true, Env: []string{"TERM=xterm", "NO_COLOR=1"}}, false},
		{"NO_COLOR empty is ignored", PolicyInput{IsTTY: true, Env: []string{"TERM=xterm", "NO_COLOR="}}, true},
		{"TERM=dumb", PolicyInput{IsTTY: true, Env: []string{"TERM=dumb"}}, false},
		{"CLICOLOR=0", PolicyInput{IsTTY: true, Env: []string{"TERM=xterm", "CLICOLOR=0"}}, false},
		{"CLICOLOR_FORCE piped", PolicyInput{IsTTY: false, Env: []string{"TERM=xterm", "CLICOLOR_FORCE=1"}}, true},
		{"CLICOLOR_FORCE loses to --json", PolicyInput{JSON: true, Env: []string{"CLICOLOR_FORCE=1"}}, false},
		{"CLICOLOR_FORCE loses to NO_COLOR", PolicyInput{Env: []string{"CLICOLOR_FORCE=1", "NO_COLOR=1"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Decide(tt.in)
			if m.Styled != tt.styled {
				t.Errorf("styled = %v, want %v", m.Styled, tt.styled)
			}
			if !m.Styled && m.Profile != colorprofile.NoTTY {
				t.Errorf("plain mode must strip everything, profile = %v", m.Profile)
			}
		})
	}
}

func TestSupportsUnicode(t *testing.T) {
	if !SupportsUnicode([]string{"LANG=en_US.UTF-8"}, "linux") {
		t.Error("UTF-8 LANG")
	}
	if SupportsUnicode([]string{"LANG=C"}, "linux") {
		t.Error("C locale")
	}
	if !SupportsUnicode([]string{"LC_ALL=en_US.utf8", "LANG=C"}, "linux") {
		t.Error("LC_ALL wins")
	}
	if !SupportsUnicode([]string{"WT_SESSION=x"}, "windows") {
		t.Error("windows terminal")
	}
}

func TestResolveIcons(t *testing.T) {
	if ResolveIcons("nerd", true, true).Name != "nerd" {
		t.Error("nerd")
	}
	if ResolveIcons("nerd", false, true).Name != "ascii" {
		t.Error("plain output must use ascii")
	}
	if ResolveIcons("unicode", true, false).Name != "ascii" {
		t.Error("no unicode support must use ascii")
	}
}

var ansiRE = regexp.MustCompile("\x1b\\[")

func sampleReport() Report {
	return Report{Title: "ytm-go doctor", Meta: "v1 · linux/amd64", Sections: []Section{
		{Name: "Environment", Items: []Item{
			{Label: "mpv", Status: StatusOK, Value: "v0.41.0", Detail: "/usr/bin/mpv"},
			{Label: "terminal", Status: StatusWarn, Value: "256 colors"},
		}},
		{Name: "Config", Items: []Item{{Label: "config.toml", Status: StatusFail, Detail: "bad"}}},
	}}
}

func render(t *testing.T, transparent bool, mode Mode) string {
	t.Helper()
	r := New(Options{Theme: theme.Fallback(), Transparent: transparent, Icons: UnicodeIcons, Mode: mode})
	var buf bytes.Buffer
	if _, err := mode.Writer(&buf).Write([]byte(r.Report(sampleReport()) + r.Hint("try", "ytm setup"))); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestTransparentEmitsNoBackground(t *testing.T) {
	out := render(t, true, Mode{Styled: true, Profile: colorprofile.TrueColor})
	if !ansiRE.MatchString(out) {
		t.Fatal("expected styled output")
	}
	if bg := BackgroundSGRs(out); len(bg) > 0 {
		t.Fatalf("background SGR in transparent mode: %q", bg)
	}
}

func TestOpaqueEmitsBackground(t *testing.T) {
	out := render(t, false, Mode{Styled: true, Profile: colorprofile.TrueColor})
	if len(BackgroundSGRs(out)) == 0 {
		t.Fatal("opaque mode should paint the title badge background")
	}
}

func TestPlainHasNoANSI(t *testing.T) {
	out := render(t, false, Mode{Styled: false, Profile: colorprofile.NoTTY})
	if ansiRE.MatchString(out) {
		t.Fatalf("plain output contains ANSI: %q", out)
	}
	if !strings.Contains(out, "1 problem, 1 warning") {
		t.Fatalf("summary missing:\n%s", out)
	}
}

func TestDegradesTo16Colors(t *testing.T) {
	out := render(t, true, Mode{Styled: true, Profile: colorprofile.ANSI})
	if strings.Contains(out, "38;2;") || strings.Contains(out, "38;5;") {
		t.Fatalf("16-color profile leaked truecolor/256 sequences: %q", out)
	}
}

func TestSectionStatus(t *testing.T) {
	s := Section{Items: []Item{{Status: StatusInfo}, {Status: StatusWarn}}}
	if s.Status() != StatusWarn {
		t.Error("worst status")
	}
	if (Section{Items: []Item{{Status: StatusInfo}}}).Status() != StatusOK {
		t.Error("info-only section is ok")
	}
}

func TestBackgroundSGRs(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"\x1b[1;38;2;255;107;107mX\x1b[m", 0},
		{"\x1b[38;5;100mX", 0},
		{"\x1b[49mX", 0},
		{"\x1b[48;2;1;2;3mX", 1},
		{"\x1b[1;41mX", 1},
		{"\x1b[38;5;1;107mX", 1},
		{"\x1b[48;5;236mX\x1b[0m\x1b[44m", 2},
		{"\x1b[2Jno sgr", 0},
	}
	for _, tt := range tests {
		if got := BackgroundSGRs(tt.in); len(got) != tt.want {
			t.Errorf("%q: got %v, want %d", tt.in, got, tt.want)
		}
	}
}

func BenchmarkStylesBuild(b *testing.B) {
	t := theme.Fallback()
	for b.Loop() {
		NewUI(t, UIOptions{Profile: colorprofile.ANSI256}, 1)
	}
}
