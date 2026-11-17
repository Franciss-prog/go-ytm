package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Franciss-Prog/go-ytm/internal/app"

	"github.com/charmbracelet/colorprofile"

	"github.com/Franciss-Prog/go-ytm/internal/buildinfo"
	"github.com/Franciss-Prog/go-ytm/internal/doctor"
)

var update = flag.Bool("update", false, "rewrite golden files")

type runOpts struct {
	tty    bool
	env    []string
	config string // contents of config.toml; empty = no file
	stdin  string
	inTTY  bool             // stdin is a terminal (prompts allowed)
	prep   func(cfg string) // runs before Main with the config dir
}

type result struct {
	stdout, stderr string
	code           int
	home           string
}

func fakeDoctorDeps() doctor.Deps {
	return doctor.Deps{
		LookPath: func(name string) (string, error) {
			if name == "mpv" {
				return "/usr/bin/mpv", nil
			}
			return "", errors.New("not found")
		},
		Version: func(context.Context, string, ...string) (string, error) {
			return "mpv v0.41.0 Copyright © 2000-2025 mpv/MPlayer/mplayer2 projects", nil
		},
		Stat:  os.Stat,
		Build: buildinfo.Info{Version: "v0.0.0-test", OS: "linux", Arch: "amd64"},
	}
}

func run(t *testing.T, o runOpts, args ...string) result {
	t.Helper()
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "ytm-go")
	if o.config != "" {
		if err := os.MkdirAll(cfgDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(o.config), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if o.prep != nil {
		o.prep(cfgDir)
	}
	env := append([]string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"XDG_RUNTIME_DIR=/run/user/1000",
		"LANG=en_US.UTF-8",
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	}, o.env...)

	prev := DoctorDeps
	DoctorDeps = fakeDoctorDeps
	t.Cleanup(func() { DoctorDeps = prev })

	var stdout, stderr bytes.Buffer
	io := IO{
		In: strings.NewReader(o.stdin), Out: &stdout, Err: &stderr, Env: env, GOOS: "linux",
		IsTTY:   func(io.Writer) bool { return o.tty },
		Profile: func(io.Writer) colorprofile.Profile { return colorprofile.TrueColor },
		InTTY:   func() bool { return o.inTTY },
	}
	code := Main(context.Background(), args, io)
	jsonHome, _ := json.Marshal(home)
	norm := func(s string) string {
		s = strings.ReplaceAll(s, string(jsonHome[1:len(jsonHome)-1]), "$HOME") // JSON-escaped (Windows)
		return strings.ReplaceAll(s, home, "$HOME")
	}
	return result{stdout: norm(stdout.String()), stderr: norm(stderr.String()), code: code, home: home}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		// Goldens are Linux reference renderings: the tests simulate GOOS=linux,
		// but path resolution uses the host's filepath rules, so paths differ.
		// Rendering/policy is still covered on Windows by the non-golden tests.
		t.Skip("golden files are Linux reference output")
	}
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing golden %s (run go test ./internal/cli -update)", path)
	}
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch (run with -update to accept)\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

const badConfig = `[playback]
default_volume = 150
seek_stp = 3

[theme]
source = "wallpaper"
transparent = true
`

func TestDoctorGolden(t *testing.T) {
	tests := []struct {
		name string
		opts runOpts
		args []string
		code int
	}{
		{"doctor.styled.txt", runOpts{tty: true}, []string{"doctor"}, 1},
		{"doctor.transparent.txt", runOpts{tty: true, config: "[theme]\ntransparent = true\n"}, []string{"doctor"}, 1},
		{"doctor.nocolor.txt", runOpts{tty: true, env: []string{"NO_COLOR=1"}}, []string{"doctor"}, 1},
		{"doctor.piped.txt", runOpts{tty: false}, []string{"doctor"}, 1},
		{"doctor.ascii.txt", runOpts{tty: true, config: "[ui]\nicons = \"ascii\"\n"}, []string{"doctor"}, 1},
		{"doctor.badconfig.txt", runOpts{tty: false, config: badConfig}, []string{"doctor"}, 1},
		{"doctor.json", runOpts{tty: true}, []string{"doctor", "--json"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := run(t, tt.opts, tt.args...)
			if r.code != tt.code {
				t.Errorf("exit code %d, want %d (stderr: %s)", r.code, tt.code, r.stderr)
			}
			golden(t, tt.name, r.stdout)
		})
	}
}

func TestConfigCheckGolden(t *testing.T) {
	r := run(t, runOpts{tty: true, config: badConfig}, "config", "check")
	if r.code != ExitFailure {
		t.Errorf("errors present: exit %d, want 1", r.code)
	}
	golden(t, "config_check.styled.txt", r.stdout)

	r = run(t, runOpts{config: badConfig}, "config", "check")
	golden(t, "config_check.piped.txt", r.stdout)

	r = run(t, runOpts{config: badConfig}, "config", "check", "--json")
	golden(t, "config_check.json", r.stdout)

	r = run(t, runOpts{config: "[playback]\nseek_step = 500\n"}, "config", "check")
	if r.code != ExitOK {
		t.Errorf("warnings only: exit %d, want 0", r.code)
	}

	r = run(t, runOpts{stdin: "[ui]\nicons = \"emoji\"\n"}, "config", "check", "-")
	if r.code != ExitFailure || !strings.Contains(r.stdout, "<stdin>:2:1") {
		t.Errorf("stdin check: code %d out %q", r.code, r.stdout)
	}

	r = run(t, runOpts{}, "config", "check")
	if r.code != ExitOK || !strings.Contains(r.stdout, "no problems") {
		t.Errorf("clean: code %d out %q", r.code, r.stdout)
	}
}

// Every command that supports --json must emit ANSI-free, valid JSON even
// when stdout is a color TTY.
func TestJSONHasNoANSI(t *testing.T) {
	for _, args := range [][]string{
		{"doctor", "--json"},
		{"config", "check", "--json"},
		{"config", "path", "--json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			r := run(t, runOpts{tty: true, config: badConfig, env: []string{"CLICOLOR_FORCE=1"}}, args...)
			if strings.Contains(r.stdout, "\x1b[") {
				t.Fatalf("ANSI in JSON output: %q", r.stdout)
			}
			if strings.Count(strings.TrimSpace(r.stdout), "\n") != 0 {
				t.Errorf("JSON must be a single line: %q", r.stdout)
			}
			var v any
			if err := json.Unmarshal([]byte(r.stdout), &v); err != nil {
				t.Fatalf("invalid JSON: %v\n%s", err, r.stdout)
			}
		})
	}
}

func TestPipedOutputHasNoANSI(t *testing.T) {
	for _, args := range [][]string{{"doctor"}, {"config", "check"}, {"config", "path"}, {"config", "defaults"}, {}} {
		r := run(t, runOpts{config: badConfig}, args...)
		if strings.Contains(r.stdout+r.stderr, "\x1b[") {
			t.Errorf("%v: ANSI in piped output:\n%s%s", args, r.stdout, r.stderr)
		}
	}
}

func TestNoColorFlag(t *testing.T) {
	r := run(t, runOpts{tty: true}, "doctor", "--no-color")
	if strings.Contains(r.stdout, "\x1b[") {
		t.Fatalf("--no-color still styled: %q", r.stdout)
	}
}

func TestRootNeedsTerminal(t *testing.T) {
	called := false
	prev := TUIRunner
	TUIRunner = func(context.Context, app.Options) error { called = true; return nil }
	t.Cleanup(func() { TUIRunner = prev })
	r := run(t, runOpts{})
	if r.code != ExitUsage || called || !strings.Contains(r.stderr, "interactive terminal") {
		t.Fatalf("root piped: code %d called %v stderr %q", r.code, called, r.stderr)
	}
}

func TestRootStartsTUI(t *testing.T) {
	var got app.Options
	prev := TUIRunner
	TUIRunner = func(_ context.Context, o app.Options) error { got = o; return nil }
	t.Cleanup(func() { TUIRunner = prev })
	r := run(t, runOpts{tty: true, inTTY: true, config: "[playback]\ndefault_volume = 150\n"}, "--trace-frames")
	if r.code != ExitOK {
		t.Fatalf("code %d stderr %q", r.code, r.stderr)
	}
	if !got.TraceFrames || got.Config.Playback.DefaultVolume != 100 || len(got.ConfigDiags) == 0 || got.Paths.Config == "" {
		t.Errorf("options not passed through: %+v", got)
	}
	r = run(t, runOpts{tty: true, inTTY: true, env: []string{"NO_COLOR=1"}})
	if r.code != ExitOK || !got.NoColor {
		t.Errorf("NO_COLOR not passed: code %d, %v", r.code, got.NoColor)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"bogus"}, {"--frob"}, {"doctor", "extra"}, {"config", "check", "a", "b"}} {
		r := run(t, runOpts{}, args...)
		if r.code != ExitUsage {
			t.Errorf("%v: exit %d, want %d", args, r.code, ExitUsage)
		}
		if r.stdout != "" {
			t.Errorf("%v: errors must go to stderr, stdout=%q", args, r.stdout)
		}
	}
}

func TestVersionAndHelp(t *testing.T) {
	r := run(t, runOpts{}, "--version")
	if r.code != 0 || !strings.HasPrefix(r.stdout, "ytm ") {
		t.Errorf("version: %d %q", r.code, r.stdout)
	}
	r = run(t, runOpts{}, "--help")
	for _, want := range []string{"USAGE", "doctor", "config", "--no-color", "--pprof"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("help missing %q:\n%s", want, r.stdout)
		}
	}
}

func TestConfigDefaultsAndPath(t *testing.T) {
	r := run(t, runOpts{}, "config", "defaults")
	if r.code != 0 || !strings.Contains(r.stdout, "[theme]") || !strings.Contains(r.stdout, "default_volume = 80") {
		t.Fatalf("defaults: %d\n%s", r.code, r.stdout)
	}
	r = run(t, runOpts{}, "config", "path")
	if strings.TrimSpace(r.stdout) != filepath.FromSlash("$HOME/.config/ytm-go/config.toml") {
		t.Errorf("path: %q", r.stdout)
	}
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	r = run(t, runOpts{}, "--config-dir", elsewhere, "config", "path")
	if strings.TrimSpace(r.stdout) != filepath.Join(elsewhere, "config.toml") {
		t.Errorf("--config-dir: %q", r.stdout)
	}
}

func TestConfigOpensEditor(t *testing.T) {
	var gotName string
	var gotArgs []string
	home := t.TempDir()
	a := NewApp(IO{
		Out: io.Discard, Err: io.Discard, GOOS: "linux",
		Env: []string{"HOME=" + home, "XDG_CONFIG_HOME=" + home, "XDG_STATE_HOME=" + home, "EDITOR=nvim -p"},
	})
	a.RunCommand = func(_ context.Context, name string, args ...string) error {
		gotName, gotArgs = name, args
		return nil
	}
	root := NewRootCmd(a)
	root.SetArgs([]string{"config"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	file := filepath.Join(home, "ytm-go", "config.toml")
	if gotName != "nvim" || len(gotArgs) != 2 || gotArgs[0] != "-p" || gotArgs[1] != file {
		t.Errorf("editor call: %q %q", gotName, gotArgs)
	}
	b, err := os.ReadFile(file)
	if err != nil || !strings.HasPrefix(string(b), "# ytm-go configuration") {
		t.Errorf("config file not created with header: %v %q", err, b)
	}
	st, _ := os.Stat(file)
	if st != nil && st.Mode().Perm() != 0o600 && filepath.Separator == '/' {
		t.Errorf("config file mode %v, want 0600", st.Mode().Perm())
	}
}

func TestPprofFlag(t *testing.T) {
	r := run(t, runOpts{}, "--pprof=127.0.0.1:0", "config", "path")
	if r.code != 0 || !strings.Contains(r.stderr, "/debug/pprof/") {
		t.Fatalf("pprof: code %d stderr %q", r.code, r.stderr)
	}
}

func TestThemeShowGolden(t *testing.T) {
	sparse, err := os.ReadFile("../theme/testdata/pywal/matugen-pywalfox-sparse.json")
	if err != nil {
		t.Fatal(err)
	}
	pywal := runOpts{
		config: "[theme]\nsource = \"pywal\"\npywal_path = \"~/wal/colors.json\"\n",
		prep: func(cfg string) {
			home := filepath.Dir(filepath.Dir(cfg))
			_ = os.MkdirAll(filepath.Join(home, "wal"), 0o700)
			_ = os.WriteFile(filepath.Join(home, "wal", "colors.json"), sparse, 0o600)
			_ = os.WriteFile(filepath.Join(cfg, "theme.toml"), []byte("[overrides]\nmuted = \"#3a3a3a\"\n"), 0o600)
		},
	}
	r := run(t, pywal, "theme", "show")
	if r.code != ExitOK {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	golden(t, "theme_show.piped.txt", r.stdout)
	r = run(t, pywal, "theme", "show", "--json")
	golden(t, "theme_show.json", r.stdout)
	pywal.tty = true
	r = run(t, pywal, "theme", "show")
	golden(t, "theme_show.styled.txt", r.stdout)

	// Missing source: fallback, still exit 0, error reported.
	r = run(t, runOpts{config: "[theme]\nsource = \"matugen\"\nfallback = \"nord\"\n"}, "theme", "show", "--json")
	if r.code != ExitOK || !strings.Contains(r.stdout, `"name":"nord"`) || !strings.Contains(r.stdout, `"error":"matugen colors not found`) {
		t.Errorf("fallback: %d %s", r.code, r.stdout)
	}
	r = run(t, pywal, "doctor")
	if !strings.Contains(r.stdout, "matugen-written") {
		t.Errorf("doctor lacks matugen hint:\n%s", r.stdout)
	}
}
