// Package style owns every Lip Gloss style, icon set and spacing constant
// used by the CLI and TUI, and the policy deciding when output is styled.
//
// Command code never builds colors or styles itself; it asks a Renderer.
package style

import (
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/colorprofile"
	"golang.org/x/term"
)

// Mode describes how a stream should be written.
type Mode struct {
	Styled  bool                 // colors, borders, icons allowed
	Profile colorprofile.Profile // color depth to downsample to
}

// PolicyInput is everything the output policy depends on. It is a plain
// struct so the policy can be tested without a terminal.
type PolicyInput struct {
	IsTTY   bool
	Env     []string // os.Environ() style "K=V"
	NoColor bool     // --no-color flag
	JSON    bool     // --json flag
	// Detected is the profile colorprofile detected for the stream; used
	// only when output is styled. Zero value means "detect from Env".
	Detected colorprofile.Profile
}

// Decide applies the output policy from docs/specs/06-cli-ipc.md:
//
//	styled := (TTY || CLICOLOR_FORCE=1) && !NO_COLOR && !--no-color &&
//	          TERM != "dumb" && CLICOLOR != "0" && !--json
func Decide(in PolicyInput) Mode {
	env := envMap(in.Env)
	plain := Mode{Styled: false, Profile: colorprofile.NoTTY}
	if in.JSON || in.NoColor {
		return plain
	}
	if _, ok := env["NO_COLOR"]; ok && env["NO_COLOR"] != "" {
		return plain
	}
	if env["TERM"] == "dumb" || env["CLICOLOR"] == "0" {
		return plain
	}
	force := env["CLICOLOR_FORCE"] != "" && env["CLICOLOR_FORCE"] != "0"
	if !in.IsTTY && !force {
		return plain
	}
	p := in.Detected
	if p == 0 || p == colorprofile.NoTTY {
		p = colorprofile.Env(in.Env)
		if p <= colorprofile.Ascii {
			// Forced or TTY without detectable color support: still style
			// (bold, borders) but without colors.
			p = colorprofile.Ascii
		}
	}
	return Mode{Styled: true, Profile: p}
}

// DecideFor applies the policy to a real stream such as os.Stdout.
func DecideFor(w io.Writer, noColor, json bool) Mode {
	in := PolicyInput{Env: os.Environ(), NoColor: noColor, JSON: json}
	if f, ok := w.(interface{ Fd() uintptr }); ok {
		in.IsTTY = term.IsTerminal(int(f.Fd()))
	}
	if in.IsTTY {
		in.Detected = colorprofile.Detect(w, in.Env)
	}
	return Decide(in)
}

// Writer wraps w so that ANSI sequences are downsampled to m.Profile
// (or stripped entirely when output is plain).
func (m Mode) Writer(w io.Writer) io.Writer {
	return &colorprofile.Writer{Forward: w, Profile: m.Profile}
}

func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}

// SupportsUnicode guesses whether the terminal can display non-ASCII glyphs.
func SupportsUnicode(env []string, goos string) bool {
	m := envMap(env)
	if goos == "windows" {
		return m["WT_SESSION"] != "" || m["TERM_PROGRAM"] != "" || m["TERM"] != ""
	}
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := m[k]; v != "" {
			v = strings.ToLower(v)
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return false
}
