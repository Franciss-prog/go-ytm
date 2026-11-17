package theme

import (
	"errors"
	"fmt"
	"image/color"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
)

// Spec is everything needed to resolve the active theme: the [theme]
// config plus paths and what is known about the terminal.
type Spec struct {
	Source      string // static|pywal|matugen|ansi|file
	Name        string // static theme name
	Fallback    string // static theme used when the source fails
	PywalPath   string // resolved path ("" = <home>/.cache/wal/colors.json)
	MatugenPath string // resolved path ("" = <config>/matugen-colors.toml)
	File        string // resolved path ("" = <config>/theme.toml)
	ConfigDir   string
	Home        string
	ANSIAccent  int
	PywalMap    map[string]string

	EnforceContrast bool
	Transparent     bool
	// Watch enables live reload of the source and theme.toml files.
	Watch bool
	// TerminalBg is the terminal's background if known (OSC 11 answer or
	// COLORFGBG); used for contrast in transparent mode.
	TerminalBg color.Color

	// ReadFile reads a file (os.ReadFile when nil; tests fake it).
	ReadFile func(string) ([]byte, error)
}

// Result is a resolved theme and how it was made.
type Result struct {
	Theme Theme
	// Path is the palette file of the source ("" for static/ansi).
	Path string
	// Err is set when the source failed and Theme is the fallback.
	Err error
	// Notes are non-fatal problems (bad entries, unknown keys).
	Notes       []string
	Adjustments []Adjustment
	// Against is the background contrast was checked against ("" = none).
	Against string
	// Pywal metadata, for `ytm theme show`.
	Wallpaper, Alpha string
	// Cause says what produced this result (set by Watcher and Manager).
	Cause Cause
}

// Cause is why a theme was (re)resolved.
type Cause int

// Causes.
const (
	CauseInitial  Cause = iota
	CauseFile           // a watched file changed
	CauseReload         // manual reload (SIGUSR1, palette, IPC)
	CauseSettings       // [theme] settings changed (Settings popup, C-t)
)

// PalettePath is the file the source reads ("" for static and ansi).
func (s Spec) PalettePath() string {
	switch s.Source {
	case "pywal":
		if s.PywalPath != "" {
			return s.PywalPath
		}
		return filepath.Join(s.Home, ".cache", "wal", "colors.json")
	case "matugen":
		if s.MatugenPath != "" {
			return s.MatugenPath
		}
		return filepath.Join(s.ConfigDir, "matugen-colors.toml")
	case "file":
		return s.ThemeFile()
	}
	return ""
}

// ThemeFile is theme.toml: the palette for source = "file" and the
// [overrides] for every source.
func (s Spec) ThemeFile() string {
	if s.File != "" {
		return s.File
	}
	return filepath.Join(s.ConfigDir, "theme.toml")
}

// WatchPaths are the files whose changes should trigger a reload.
func (s Spec) WatchPaths() []string {
	out := []string{s.ThemeFile()}
	if p := s.PalettePath(); p != "" && p != s.ThemeFile() {
		out = append(out, p)
	}
	return out
}

// Resolve builds the active theme. It never fails: when the source is
// missing or invalid, Result.Err says why and Theme is the fallback.
func Resolve(s Spec) Result {
	read := s.ReadFile
	if read == nil {
		read = os.ReadFile
	}
	r := Result{Path: s.PalettePath()}
	t, err := s.source(read, &r)
	if err != nil {
		r.Err = err
		fb := s.Fallback
		if fb == "" {
			fb = "ytm-dark"
		}
		if t, err = Builtin(fb); err != nil {
			r.Notes = append(r.Notes, fmt.Sprintf("fallback: %v", err))
			t = Fallback()
		}
	}

	// [overrides] in theme.toml apply on top of any source.
	if data, err := read(s.ThemeFile()); err == nil {
		if f, err := ParseFile(data); err == nil {
			ApplyOverrides(&t, f.Overrides)
			if s.Source != "file" {
				r.Notes = append(r.Notes, f.Notes...)
			}
		} else if s.Source != "file" {
			r.Notes = append(r.Notes, err.Error())
		}
	}

	if s.EnforceContrast && t.Source != "ansi" {
		if against := s.contrastBg(t); against != nil {
			r.Against = FormatColor(against)
			t, r.Adjustments = EnforceContrast(t, against)
		}
	}
	r.Theme = t
	return r
}

func (s Spec) source(read func(string) ([]byte, error), r *Result) (Theme, error) {
	switch s.Source {
	case "", "static":
		return Builtin(s.Name)
	case "ansi":
		return ANSITheme(s.ANSIAccent), nil
	}
	path := r.Path
	data, err := read(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Theme{}, fmt.Errorf("%s colors not found at %s", s.Source, path)
	}
	if err != nil {
		return Theme{}, fmt.Errorf("read %s colors: %w", s.Source, err)
	}
	switch s.Source {
	case "pywal":
		p, err := ParsePywal(data)
		if err != nil {
			return Theme{}, err
		}
		r.Wallpaper, r.Alpha = p.Wallpaper, p.Alpha
		t, notes := FromPywal(p, s.PywalMap)
		r.Notes = append(r.Notes, notes...)
		return t, nil
	case "matugen":
		m, notes, err := ParseMatugen(data)
		r.Notes = append(r.Notes, notes...)
		if err != nil {
			return Theme{}, err
		}
		return FromMatugen(m), nil
	case "file":
		f, err := ParseFile(data)
		if err != nil {
			return Theme{}, err
		}
		r.Notes = append(r.Notes, f.Notes...)
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		return FromFile(f, name, "file")
	}
	return Theme{}, fmt.Errorf("unknown theme source %q", s.Source)
}

// contrastBg is the background text is checked against: the theme's bg
// when opaque; in transparent mode the palette's own bg for wallpaper
// sources (the terminal uses the same palette), else the terminal's
// reported bg, else black (assume dark). nil means unknown: skip.
func (s Spec) contrastBg(t Theme) color.Color {
	if !s.Transparent {
		if _, ok := RGB(t.Bg); ok {
			return t.Bg
		}
		return nil
	}
	if t.Source == "pywal" || t.Source == "matugen" {
		if _, ok := RGB(t.Bg); ok {
			return t.Bg
		}
	}
	if _, ok := RGB(s.TerminalBg); ok {
		return s.TerminalBg
	}
	return lipgloss.Color("#000000")
}

// ParseCOLORFGBG reads the COLORFGBG convention ("fg;bg" or "fg;x;bg",
// ANSI indexes) and returns the assumed sRGB background, or nil.
func ParseCOLORFGBG(v string) color.Color {
	parts := strings.Split(v, ";")
	if len(parts) < 2 {
		return nil
	}
	var n int
	if _, err := fmt.Sscanf(parts[len(parts)-1], "%d", &n); err != nil || n < 0 || n > 15 {
		return nil
	}
	return toColor(xterm16[n])
}
