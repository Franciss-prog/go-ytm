package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadEmptyIsDefault(t *testing.T) {
	cfg, diags := Load(nil, "config.toml")
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %v", diags)
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Fatal("empty config differs from defaults")
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, diags := LoadFile(filepath.Join(t.TempDir(), "nope.toml"))
	if len(diags) != 0 || !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("missing file: diags=%v", diags)
	}
}

func TestDefaultsRoundTrip(t *testing.T) {
	b, err := DefaultsTOML()
	if err != nil {
		t.Fatal(err)
	}
	cfg, diags := Load(b, "defaults.toml")
	if len(diags) != 0 {
		t.Fatalf("defaults produce diagnostics: %v", diags)
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Fatalf("round trip mismatch:\n%s", b)
	}
}

func TestLoadDiagnostics(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, c Config)
		want  []string // Diagnostic.String() values, in order
	}{
		{
			name:  "valid values",
			input: "[playback]\ndefault_volume = 55\nseek_step = 10\n[theme]\nsource = \"pywal\"\ntransparent = true\n",
			check: func(t *testing.T, c Config) {
				if c.Playback.DefaultVolume != 55 || c.Playback.SeekStep != 10 || c.Theme.Source != "pywal" || !c.Theme.Transparent {
					t.Errorf("values not applied: %+v %+v", c.Playback, c.Theme)
				}
			},
		},
		{
			name:  "clamp with line and col",
			input: "# comment\n[playback]\n  default_volume = 150\n",
			want:  []string{"config.toml:3:3: warning: playback.default_volume: 150 is above maximum 100; clamped to 100"},
			check: func(t *testing.T, c Config) {
				if c.Playback.DefaultVolume != 100 {
					t.Errorf("not clamped: %d", c.Playback.DefaultVolume)
				}
			},
		},
		{
			name:  "wrong type keeps default",
			input: "[playback]\ndefault_volume = \"loud\"\n",
			want:  []string{`config.toml:2:1: error: playback.default_volume: expected integer, got string; using default`},
			check: func(t *testing.T, c Config) {
				if c.Playback.DefaultVolume != 80 {
					t.Errorf("default not kept: %d", c.Playback.DefaultVolume)
				}
			},
		},
		{
			name:  "bad enum",
			input: "[theme]\nsource = \"wallpaper\"\n",
			want:  []string{`config.toml:2:1: error: theme.source: "wallpaper" is not one of static, pywal, matugen, ansi, file; using "static"`},
		},
		{
			name:  "enum case-insensitive",
			input: "[logging]\nlevel = \"debug\"\n[ui]\nicons = \"ASCII\"\n",
			check: func(t *testing.T, c Config) {
				if c.Logging.Level != "DEBUG" || c.UI.Icons != "ascii" {
					t.Errorf("not normalized: %q %q", c.Logging.Level, c.UI.Icons)
				}
			},
		},
		{
			name:  "unknown key with suggestion",
			input: "[playback]\nseek_stp = 3\n",
			want:  []string{`config.toml:2:1: warning: playback.seek_stp: unknown key (did you mean "seek_step"?)`},
		},
		{
			name:  "unknown section",
			input: "[playbak]\nx = 1\n",
			want:  []string{`config.toml:1:2: warning: playbak: unknown section (did you mean "playback"?)`},
		},
		{
			name:  "ytm-player aliases",
			input: "[ui]\ntheme = \"nord\"\nalbum_art = false\n",
			want: []string{
				`config.toml:2:1: warning: ui.theme: deprecated: use [theme] name = "nord"`,
				`config.toml:3:1: info: ui.album_art: deprecated: use [ui] album_art_mode = "off"`,
			},
			check: func(t *testing.T, c Config) {
				if c.Theme.Name != "nord" || c.UI.AlbumArtMode != "off" {
					t.Errorf("aliases not applied: %q %q", c.Theme.Name, c.UI.AlbumArtMode)
				}
			},
		},
		{
			name:  "ignored compat keys",
			input: "[cache]\nenabled = false\n",
			want:  []string{"config.toml:2:1: info: cache.enabled: the cache is always on (set max_size_mb = 0 to disable); ignored"},
		},
		{
			name:  "region normalization",
			input: "[ui]\nregion = \"es-ES\"\n",
			check: func(t *testing.T, c Config) {
				if c.UI.Region != "ES" {
					t.Errorf("region = %q", c.UI.Region)
				}
			},
		},
		{
			name:  "bad region",
			input: "[ui]\nregion = \"Spain\"\n",
			want:  []string{`config.toml:2:1: warning: ui.region: "Spain" is not a two-letter region code; using "ZZ" (global)`},
		},
		{
			name:  "cookie browser normalized and validated",
			input: "[yt_dlp]\ncookie_browser = \" FireFox \"\n",
			check: func(t *testing.T, c Config) {
				if c.YtDlp.CookieBrowser != "firefox" {
					t.Errorf("browser = %q", c.YtDlp.CookieBrowser)
				}
			},
		},
		{
			name:  "unknown cookie browser",
			input: "[yt_dlp]\ncookie_browser = \"firefx\"\n",
			want:  []string{`config.toml:2:1: warning: yt_dlp.cookie_browser: unknown browser "firefx" (did you mean "firefox"?); using auto-detect`},
		},
		{
			name:  "small cache clamped",
			input: "[cache]\nmax_size_mb = 10\n",
			want:  []string{"config.toml:2:1: warning: cache.max_size_mb: 10 MB is too small to hold a track; clamped to 64 (use 0 to disable)"},
		},
		{
			name:  "string array",
			input: "[playback]\nmpv_args = [\"--af=loudnorm\"]\n",
			check: func(t *testing.T, c Config) {
				if len(c.Playback.MPVArgs) != 1 || c.Playback.MPVArgs[0] != "--af=loudnorm" {
					t.Errorf("mpv_args = %v", c.Playback.MPVArgs)
				}
			},
		},
		{
			name:  "pywal map with unknown token",
			input: "[theme.pywal_map]\naccent = \"color5\"\naccnt = \"color3\"\n",
			want:  []string{`config.toml:3:1: warning: theme.pywal_map.accnt: unknown theme token (did you mean "accent"?)`},
			check: func(t *testing.T, c Config) {
				if c.Theme.PywalMap["accent"] != "color5" || len(c.Theme.PywalMap) != 1 {
					t.Errorf("pywal_map = %v", c.Theme.PywalMap)
				}
			},
		},
		{
			name:  "syntax error",
			input: "[playback]\ndefault_volume = = 3\n",
			want:  []string{"config.toml:2:18: error: unexpected character U+003D '=' at start of value; using defaults"},
		},
		{
			name:  "section not a table",
			input: "playback = 3\n",
			want:  []string{"config.toml:1:1: error: playback: expected a table [playback], got integer"},
		},
		{
			name:  "BOM and CRLF",
			input: "\xef\xbb\xbf[playback]\r\nseek_step = 7\r\n",
			check: func(t *testing.T, c Config) {
				if c.Playback.SeekStep != 7 {
					t.Errorf("seek_step = %d", c.Playback.SeekStep)
				}
			},
		},
		{
			name:  "invalid utf8",
			input: "[general]\nstartup_page = \"\xff\"\n",
			want:  []string{"config.toml:2:1: error: file is not valid UTF-8; using defaults"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, diags := Load([]byte(tt.input), "config.toml")
			var got []string
			for _, d := range diags {
				got = append(got, d.String())
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("diagnostics:\n got: %q\nwant: %q", got, tt.want)
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestLoadFileReadError(t *testing.T) {
	dir := t.TempDir()
	// A directory cannot be read as a file.
	_, diags := LoadFile(dir)
	if len(diags) != 1 || diags[0].Severity != SevError {
		t.Fatalf("want one error, got %v", diags)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.toml"), []byte("[ui]\nicons=\"nerd\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, diags := LoadFile(filepath.Join(dir, "c.toml"))
	if len(diags) != 0 || cfg.UI.Icons != "nerd" {
		t.Fatalf("file load: %v %q", diags, cfg.UI.Icons)
	}
}

func TestLevenshtein(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		d    int
	}{{"", "", 0}, {"abc", "abc", 0}, {"seek_stp", "seek_step", 1}, {"kitten", "sitting", 3}} {
		if got := levenshtein(tt.a, tt.b); got != tt.d {
			t.Errorf("levenshtein(%q,%q)=%d want %d", tt.a, tt.b, got, tt.d)
		}
	}
}

func FuzzLoad(f *testing.F) {
	f.Add([]byte("[playback]\ndefault_volume = 3\n"))
	f.Add([]byte("[theme.pywal_map]\naccent = 'color1'\n"))
	f.Add([]byte("x = [1, {a = 2}]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Load(data, "fuzz.toml") // must never panic
	})
}
