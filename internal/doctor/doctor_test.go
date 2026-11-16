package doctor

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"github.com/Franciss-Prog/go-ytm/internal/config"
	"github.com/Franciss-Prog/go-ytm/internal/platform"
	"github.com/Franciss-Prog/go-ytm/internal/style"
)

func baseDeps(t *testing.T) Deps {
	home := t.TempDir()
	env := platform.Env{
		GOOS: "linux", UID: 1000,
		Getenv:  func(k string) string { return map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000"}[k] },
		HomeDir: func() (string, error) { return home, nil },
	}
	p, err := platform.Resolve(env, "")
	if err != nil {
		t.Fatal(err)
	}
	return Deps{
		LookPath: func(n string) (string, error) { return "/usr/bin/" + n, nil },
		Version: func(_ context.Context, path string, _ ...string) (string, error) {
			if strings.HasSuffix(path, "mpv") {
				return "mpv v0.41.0 Copyright", nil
			}
			return "2026.10.26", nil
		},
		Stat:     os.Stat,
		Paths:    p,
		Env:      env,
		Config:   config.Default(),
		Terminal: colorprofile.TrueColor,
	}
}

func find(t *testing.T, r style.Report, section, label string) style.Item {
	t.Helper()
	for _, s := range r.Sections {
		if s.Name != section {
			continue
		}
		for _, it := range s.Items {
			if it.Label == label {
				return it
			}
		}
	}
	t.Fatalf("item %s/%s not found", section, label)
	return style.Item{}
}

func TestAllGood(t *testing.T) {
	r := Run(context.Background(), baseDeps(t))
	if f, w := r.Counts(); f != 0 || w != 0 {
		t.Fatalf("fails=%d warns=%d: %+v", f, w, r)
	}
	if it := find(t, r, "Environment", "mpv"); it.Value != "v0.41.0" || it.Detail != "/usr/bin/mpv" {
		t.Errorf("mpv item: %+v", it)
	}
	if it := find(t, r, "Paths", "config"); it.Status != style.StatusInfo || !strings.HasPrefix(it.Value, "~/") {
		t.Errorf("config dir item: %+v", it)
	}
}

func TestMissingBinaries(t *testing.T) {
	d := baseDeps(t)
	d.LookPath = func(string) (string, error) { return "", errors.New("nope") }
	r := Run(context.Background(), d)
	if it := find(t, r, "Environment", "mpv"); it.Status != style.StatusFail || !strings.Contains(it.Detail, "mpv.io") {
		t.Errorf("mpv: %+v", it)
	}
	if f, _ := r.Counts(); f != 2 {
		t.Errorf("want 2 failures, got %d", f)
	}
}

func TestYtDlpOverrideMissing(t *testing.T) {
	d := baseDeps(t)
	d.YtDlpBinary = filepath.Join(t.TempDir(), "yt-dlp")
	if it := find(t, Run(context.Background(), d), "Environment", "yt-dlp"); it.Status != style.StatusFail {
		t.Errorf("override: %+v", it)
	}
}

func TestVersionFailureIsWarning(t *testing.T) {
	d := baseDeps(t)
	d.Version = func(context.Context, string, ...string) (string, error) { return "", errors.New("boom") }
	if it := find(t, Run(context.Background(), d), "Environment", "mpv"); it.Status != style.StatusWarn {
		t.Errorf("%+v", it)
	}
}

func TestTerminalProfiles(t *testing.T) {
	for p, st := range map[colorprofile.Profile]style.Status{
		colorprofile.TrueColor: style.StatusOK, colorprofile.ANSI256: style.StatusWarn,
		colorprofile.ANSI: style.StatusWarn, colorprofile.NoTTY: style.StatusInfo,
	} {
		if got := terminalItem(p).Status; got != st {
			t.Errorf("%v: %v want %v", p, got, st)
		}
	}
}

func TestLongSocketPathFails(t *testing.T) {
	d := baseDeps(t)
	d.Paths.Runtime = "/" + strings.Repeat("x", 120)
	if it := find(t, Run(context.Background(), d), "Paths", "ipc socket"); it.Status != style.StatusFail {
		t.Errorf("%+v", it)
	}
}

func TestPathIsFile(t *testing.T) {
	d := baseDeps(t)
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	d.Paths.Cache = f
	if it := find(t, Run(context.Background(), d), "Paths", "cache"); it.Status != style.StatusFail {
		t.Errorf("%+v", it)
	}
}

func TestConfigDiagnostics(t *testing.T) {
	d := baseDeps(t)
	if err := os.MkdirAll(d.Paths.Config, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.Paths.ConfigFile(), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	d.Diags = config.Diagnostics{
		{Line: 3, Key: "playback.default_volume", Severity: config.SevWarning, Message: "clamped"},
		{Line: 6, Key: "theme.source", Severity: config.SevError, Message: "bad"},
		{Line: 7, Key: "cache.enabled", Severity: config.SevInfo, Message: "ignored"},
	}
	r := Run(context.Background(), d)
	// Header row is informational; each diagnostic carries its own status
	// so a config error is counted once.
	if it := find(t, r, "Config", "config.toml"); it.Status != style.StatusInfo {
		t.Errorf("%+v", it)
	}
	if f, w := r.Counts(); f != 1 || w != 1 {
		t.Errorf("fails=%d warns=%d", f, w)
	}
}

func TestStatError(t *testing.T) {
	d := baseDeps(t)
	d.Stat = func(string) (fs.FileInfo, error) { return nil, fs.ErrPermission }
	if f, _ := Run(context.Background(), d).Counts(); f == 0 {
		t.Error("permission errors must fail")
	}
}

func TestShortVersion(t *testing.T) {
	for in, want := range map[string]string{
		"mpv v0.41.0-dirty Copyright © 2000": "v0.41.0-dirty",
		"2026.10.26":                         "2026.10.26",
		"":                                   "",
	} {
		if got := shortVersion("mpv", in); got != want {
			t.Errorf("%q → %q want %q", in, got, want)
		}
	}
}
