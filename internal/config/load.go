package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// Severity of a diagnostic.
type Severity string

// Severities, from most to least severe.
const (
	SevError   Severity = "error"
	SevWarning Severity = "warning"
	SevInfo    Severity = "info"
)

// Diagnostic is one problem found in a config file.
type Diagnostic struct {
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Col      int      `json:"col"`
	Key      string   `json:"key"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

func (d Diagnostic) String() string {
	var b strings.Builder
	b.WriteString(d.File)
	if d.Line > 0 {
		fmt.Fprintf(&b, ":%d:%d", d.Line, d.Col)
	}
	fmt.Fprintf(&b, ": %s: ", d.Severity)
	if d.Key != "" {
		b.WriteString(d.Key + ": ")
	}
	b.WriteString(d.Message)
	return b.String()
}

// Diagnostics is a list of diagnostics.
type Diagnostics []Diagnostic

// Count returns the number of diagnostics with the given severity.
func (ds Diagnostics) Count(s Severity) int {
	n := 0
	for _, d := range ds {
		if d.Severity == s {
			n++
		}
	}
	return n
}

// LoadFile reads and validates path. A missing file yields defaults and no
// diagnostics. Read errors other than "not found" are reported as diagnostics;
// LoadFile never fails, so the app can always start.
func LoadFile(path string) (Config, Diagnostics) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Default(), Diagnostics{{File: path, Severity: SevError, Message: fmt.Sprintf("cannot read file: %v; using defaults", err)}}
	}
	return Load(data, path)
}

// Load parses and validates TOML bytes. file is used only for diagnostics.
func Load(data []byte, file string) (Config, Diagnostics) {
	cfg := Default()
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(data) {
		line := 1 + bytes.Count(data[:firstInvalidUTF8(data)], []byte("\n"))
		return cfg, Diagnostics{{File: file, Line: line, Col: 1, Severity: SevError, Message: "file is not valid UTF-8; using defaults"}}
	}

	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		d := Diagnostic{File: file, Severity: SevError, Message: err.Error() + "; using defaults"}
		var de *toml.DecodeError
		if errors.As(err, &de) {
			d.Line, d.Col = de.Position()
			d.Message = strings.TrimPrefix(de.Error(), "toml: ") + "; using defaults"
		}
		return cfg, Diagnostics{d}
	}

	l := loader{file: file, pos: indexPositions(data)}
	l.apply(&cfg, raw)
	l.validate(&cfg)
	slices.SortStableFunc(l.diags, func(a, b Diagnostic) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return a.Col - b.Col
	})
	return cfg, l.diags
}

type position struct{ line, col int }

type loader struct {
	file  string
	pos   map[string]position
	diags Diagnostics
}

func (l *loader) add(sev Severity, key, format string, args ...any) {
	p := l.pos[key]
	if p == (position{}) {
		// fall back to the enclosing table
		if i := strings.LastIndexByte(key, '.'); i > 0 {
			p = l.pos[key[:i]]
		}
	}
	l.diags = append(l.diags, Diagnostic{
		File: l.file, Line: p.line, Col: p.col, Key: key, Severity: sev,
		Message: fmt.Sprintf(format, args...),
	})
}

// ignoredKeys are accepted for compatibility with ytm-player but have no effect.
var ignoredKeys = map[string]string{
	"general.playback_bar_position": "had no effect in ytm-player; ignored",
	"playback.prefer_audio":         "audio-only streams are always preferred; ignored",
	"cache.enabled":                 "the cache is always on (set max_size_mb = 0 to disable); ignored",
	"cache.prefetch_next":           "the next track is always prefetched; ignored",
}

func (l *loader) apply(cfg *Config, raw map[string]any) {
	root := reflect.ValueOf(cfg).Elem()
	sections := tagNames(root.Type())
	for _, secName := range sortedKeys(raw) {
		secVal := raw[secName]
		field, ok := fieldByTag(root, secName)
		if !ok {
			l.add(SevWarning, secName, "unknown section%s", suggest(secName, sections))
			continue
		}
		table, ok := secVal.(map[string]any)
		if !ok {
			l.add(SevError, secName, "expected a table [%s], got %s", secName, tomlType(secVal))
			continue
		}
		keys := tagNames(field.Type())
		for _, k := range sortedKeys(table) {
			full := secName + "." + k
			v := table[k]
			if msg, ok := ignoredKeys[full]; ok {
				l.add(SevInfo, full, "%s", msg)
				continue
			}
			if l.applyAlias(cfg, full, v) {
				continue
			}
			f, ok := fieldByTag(field, k)
			if !ok {
				l.add(SevWarning, full, "unknown key%s", suggest(k, keys))
				continue
			}
			if err := assign(f, v); err != nil {
				l.add(SevError, full, "%v; using default", err)
			}
		}
	}
}

// applyAlias handles keys renamed from ytm-player. Returns true if handled.
func (l *loader) applyAlias(cfg *Config, key string, v any) bool {
	switch key {
	case "ui.theme":
		s, ok := v.(string)
		if !ok {
			l.add(SevError, key, "expected string, got %s; ignored", tomlType(v))
			return true
		}
		cfg.Theme.Name = s
		l.add(SevWarning, key, "deprecated: use [theme] name = %q", s)
		return true
	case "ui.album_art":
		b, ok := v.(bool)
		if !ok {
			l.add(SevError, key, "expected boolean, got %s; ignored", tomlType(v))
			return true
		}
		cfg.UI.AlbumArtMode = "off"
		if b {
			cfg.UI.AlbumArtMode = "blocks"
		}
		l.add(SevInfo, key, "deprecated: use [ui] album_art_mode = %q", cfg.UI.AlbumArtMode)
		return true
	}
	return false
}

func assign(f reflect.Value, v any) error {
	switch f.Kind() {
	case reflect.String:
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("expected string, got %s", tomlType(v))
		}
		f.SetString(s)
	case reflect.Bool:
		b, ok := v.(bool)
		if !ok {
			return fmt.Errorf("expected boolean, got %s", tomlType(v))
		}
		f.SetBool(b)
	case reflect.Int:
		n, ok := v.(int64)
		if !ok {
			return fmt.Errorf("expected integer, got %s", tomlType(v))
		}
		f.SetInt(n)
	case reflect.Slice:
		arr, ok := v.([]any)
		if !ok {
			return fmt.Errorf("expected array of strings, got %s", tomlType(v))
		}
		out := make([]string, 0, len(arr))
		for _, e := range arr {
			s, ok := e.(string)
			if !ok {
				return fmt.Errorf("expected array of strings, found %s element", tomlType(e))
			}
			out = append(out, s)
		}
		f.Set(reflect.ValueOf(out))
	case reflect.Map:
		tbl, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("expected table, got %s", tomlType(v))
		}
		out := make(map[string]string, len(tbl))
		for k, e := range tbl {
			s, ok := e.(string)
			if !ok {
				return fmt.Errorf("key %q: expected string, got %s", k, tomlType(e))
			}
			out[k] = s
		}
		f.Set(reflect.ValueOf(out))
	default:
		return fmt.Errorf("unsupported field kind %s", f.Kind())
	}
	return nil
}

func (l *loader) validate(cfg *Config) {
	def := reflect.ValueOf(Default())
	root := reflect.ValueOf(cfg).Elem()
	for i := range root.NumField() {
		sec := root.Field(i)
		secName := root.Type().Field(i).Tag.Get("toml")
		for j := range sec.NumField() {
			sf := sec.Type().Field(j)
			f := sec.Field(j)
			key := secName + "." + sf.Tag.Get("toml")
			if opts := sf.Tag.Get("oneof"); opts != "" {
				allowed := strings.Split(opts, "|")
				val := f.String()
				norm := strings.TrimSpace(val)
				if strings.ToUpper(allowed[0]) == allowed[0] {
					norm = strings.ToUpper(norm)
				} else {
					norm = strings.ToLower(norm)
				}
				if slices.Contains(allowed, norm) {
					f.SetString(norm)
				} else {
					dv := def.Field(i).Field(j).String()
					l.add(SevError, key, "%q is not one of %s; using %q", val, strings.Join(allowed, ", "), dv)
					f.SetString(dv)
				}
			}
			if rng := sf.Tag.Get("range"); rng != "" {
				lo, hi := parseRange(rng)
				n := int(f.Int())
				switch {
				case n < lo:
					l.add(SevWarning, key, "%d is below minimum %d; clamped to %d", n, lo, lo)
					f.SetInt(int64(lo))
				case n > hi:
					l.add(SevWarning, key, "%d is above maximum %d; clamped to %d", n, hi, hi)
					f.SetInt(int64(hi))
				}
			}
		}
	}

	// Rules that do not fit the tag vocabulary.
	if n := cfg.Cache.MaxSizeMB; n > 0 && n < 64 {
		l.add(SevWarning, "cache.max_size_mb", "%d MB is too small to hold a track; clamped to 64 (use 0 to disable)", n)
		cfg.Cache.MaxSizeMB = 64
	}
	b := strings.ToLower(strings.TrimSpace(cfg.YtDlp.CookieBrowser))
	if b != "" && !slices.Contains(CookieBrowsers, b) {
		l.add(SevWarning, "yt_dlp.cookie_browser", "unknown browser %q%s; using auto-detect", cfg.YtDlp.CookieBrowser, suggest(b, CookieBrowsers))
		b = ""
	}
	cfg.YtDlp.CookieBrowser = b
	cfg.UI.Region = l.normalizeRegion(cfg.UI.Region)
	if strings.TrimSpace(cfg.Theme.Name) == "" {
		cfg.Theme.Name = Default().Theme.Name
	}
	if strings.TrimSpace(cfg.Theme.Fallback) == "" {
		cfg.Theme.Fallback = Default().Theme.Fallback
	}
	for k := range cfg.Theme.PywalMap {
		if !slices.Contains(ThemeTokens, k) {
			l.add(SevWarning, "theme.pywal_map."+k, "unknown theme token%s", suggest(k, ThemeTokens))
			delete(cfg.Theme.PywalMap, k)
		}
	}
}

// ThemeTokens lists the semantic palette token names (spec 08).
var ThemeTokens = []string{
	"bg", "fg", "surface", "surface_alt", "border", "accent", "accent_dim", "muted",
	"selected_bg", "selected_fg", "progress_filled", "progress_empty",
	"lyrics_played", "lyrics_current", "lyrics_upcoming", "error", "warn", "success",
}

// normalizeRegion accepts ISO 3166-1 alpha-2 codes and locale-style codes
// such as "es-ES" (→ "ES"); anything else falls back to "ZZ" (global).
func (l *loader) normalizeRegion(r string) string {
	s := strings.ToUpper(strings.TrimSpace(r))
	if i := strings.IndexAny(s, "-_"); i >= 0 {
		s = s[i+1:]
	}
	if len(s) == 2 && s[0] >= 'A' && s[0] <= 'Z' && s[1] >= 'A' && s[1] <= 'Z' {
		return s
	}
	l.add(SevWarning, "ui.region", "%q is not a two-letter region code; using \"ZZ\" (global)", r)
	return "ZZ"
}

func parseRange(s string) (int, int) {
	lo, hi, _ := strings.Cut(s, ",")
	a, _ := strconv.Atoi(lo)
	b, _ := strconv.Atoi(hi)
	return a, b
}

func fieldByTag(v reflect.Value, name string) (reflect.Value, bool) {
	t := v.Type()
	for i := range t.NumField() {
		if t.Field(i).Tag.Get("toml") == name {
			return v.Field(i), true
		}
	}
	return reflect.Value{}, false
}

func tagNames(t reflect.Type) []string {
	out := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		out = append(out, t.Field(i).Tag.Get("toml"))
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func tomlType(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case int64:
		return "integer"
	case float64:
		return "float"
	case []any:
		return "array"
	case map[string]any:
		return "table"
	default:
		return "date/time"
	}
}

func firstInvalidUTF8(b []byte) int {
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size <= 1 {
			return i
		}
		i += size
	}
	return len(b)
}

// indexPositions maps "section" and "section.key" to their line/column using
// the low-level go-toml parser (which exposes byte ranges).
func indexPositions(data []byte) map[string]position {
	out := map[string]position{}
	var p unstable.Parser
	p.Reset(data)
	var prefix []string
	for p.NextExpression() {
		e := p.Expression()
		switch e.Kind {
		case unstable.Table, unstable.ArrayTable:
			prefix = keyParts(e.Key())
			if k := e.Key(); k.Next() {
				out[strings.Join(prefix, ".")] = shapeOf(&p, k.Node())
			}
		case unstable.KeyValue:
			it := e.Key()
			parts := keyParts(e.Key())
			full := strings.Join(append(slices.Clone(prefix), parts...), ".")
			if it.Next() {
				out[full] = shapeOf(&p, it.Node())
			}
		}
	}
	return out
}

func keyParts(it unstable.Iterator) []string {
	var parts []string
	for it.Next() {
		parts = append(parts, string(it.Node().Data))
	}
	return parts
}

func shapeOf(p *unstable.Parser, n *unstable.Node) position {
	s := p.Shape(n.Raw)
	return position{line: s.Start.Line, col: s.Start.Column}
}

// suggest returns ` (did you mean "x"?)` for the closest candidate within
// edit distance 2, or "".
func suggest(word string, candidates []string) string {
	best, bestD := "", 3
	for _, c := range candidates {
		if d := levenshtein(word, c); d < bestD {
			best, bestD = c, d
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf(" (did you mean %q?)", best)
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// Pos is a 1-based line/column in a TOML file.
type Pos struct{ Line, Col int }

// Positions maps "section" and "section.key" to where they appear in a
// TOML document. Other loaders (keymap.toml) use it for diagnostics.
func Positions(data []byte) map[string]Pos {
	out := map[string]Pos{}
	for k, p := range indexPositions(data) {
		out[k] = Pos{Line: p.line, Col: p.col}
	}
	return out
}

// Suggest returns ` (did you mean "x"?)` for the closest candidate within
// edit distance 2, or "".
func Suggest(word string, candidates []string) string { return suggest(word, candidates) }
