// Package doctor gathers diagnostics for `ytm doctor`.
//
// M0 ships the environment, paths, config and terminal sections. Auth,
// theme, integrations and recent-error sections arrive with their
// milestones (see docs/specs/06-cli-ipc.md).
package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/colorprofile"

	"github.com/Franciss-Prog/go-ytm/internal/buildinfo"
	"github.com/Franciss-Prog/go-ytm/internal/config"
	"github.com/Franciss-Prog/go-ytm/internal/platform"
	"github.com/Franciss-Prog/go-ytm/internal/style"
)

// Deps are the side-effecting lookups doctor needs; fakes are used in tests.
type Deps struct {
	LookPath func(name string) (string, error)
	// Version runs `<path> <args...>` and returns the first output line.
	Version  func(ctx context.Context, path string, args ...string) (string, error)
	Stat     func(path string) (fs.FileInfo, error)
	Build    buildinfo.Info
	Paths    platform.Paths
	Env      platform.Env
	Config   config.Config
	Diags    config.Diagnostics
	Terminal colorprofile.Profile // detected profile of stdout (NoTTY when piped)
	// YtDlpBinary overrides PATH lookup ([yt_dlp] binary).
	YtDlpBinary string
}

// RealDeps returns Deps backed by the OS.
func RealDeps() Deps {
	return Deps{
		LookPath: exec.LookPath,
		Version:  runVersion,
		Stat:     os.Stat,
		Build:    buildinfo.Get(),
		Env:      platform.DefaultEnv(),
	}
}

func runVersion(ctx context.Context, path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, args...).Output()
	if err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(line), nil
}

// Run builds the report.
func Run(ctx context.Context, d Deps) style.Report {
	b := d.Build
	return style.Report{
		Title: "ytm-go doctor",
		Meta:  fmt.Sprintf("%s · %s/%s", b.Version, b.OS, b.Arch),
		Sections: []style.Section{
			environment(ctx, d),
			paths(d),
			configSection(d),
		},
	}
}

func environment(ctx context.Context, d Deps) style.Section {
	// Probe binaries concurrently: yt-dlp is a Python program and takes a
	// few hundred milliseconds just to print its version.
	var mpv, ytdlp style.Item
	var wg sync.WaitGroup
	wg.Go(func() {
		mpv = binary(ctx, d, "mpv", "", []string{"--version"}, "install mpv (https://mpv.io) — needed for playback")
	})
	wg.Go(func() {
		ytdlp = binary(ctx, d, "yt-dlp", d.YtDlpBinary, []string{"--version"}, "install yt-dlp — needed to resolve audio streams")
	})
	wg.Wait()
	return style.Section{Name: "Environment", Items: []style.Item{mpv, ytdlp, terminalItem(d.Terminal)}}
}

func binary(ctx context.Context, d Deps, name, override string, args []string, fix string) style.Item {
	it := style.Item{Label: name}
	path := override
	if path == "" {
		p, err := d.LookPath(name)
		if err != nil {
			it.Status = style.StatusFail
			it.Value = "not found"
			it.Detail = fix
			return it
		}
		path = p
	} else if _, err := d.Stat(path); err != nil {
		it.Status = style.StatusFail
		it.Value = "not found"
		it.Detail = fmt.Sprintf("[yt_dlp] binary = %q does not exist", path)
		return it
	}
	v, err := d.Version(ctx, path, args...)
	if err != nil {
		it.Status = style.StatusWarn
		it.Value = "unknown version"
		it.Detail = fmt.Sprintf("%s (%v)", path, err)
		return it
	}
	it.Status = style.StatusOK
	it.Value = shortVersion(name, v)
	it.Detail = path
	return it
}

// shortVersion trims "mpv v0.41.0-dirty Copyright …" to "v0.41.0-dirty".
func shortVersion(name, line string) string {
	f := strings.Fields(line)
	if len(f) >= 2 && strings.EqualFold(f[0], name) {
		return f[1]
	}
	if len(f) >= 1 {
		return f[0]
	}
	return line
}

func terminalItem(p colorprofile.Profile) style.Item {
	it := style.Item{Label: "terminal"}
	switch p {
	case colorprofile.TrueColor:
		it.Status, it.Value = style.StatusOK, "truecolor"
	case colorprofile.ANSI256:
		it.Status, it.Value, it.Detail = style.StatusWarn, "256 colors", "truecolor recommended; themes are approximated"
	case colorprofile.ANSI:
		it.Status, it.Value, it.Detail = style.StatusWarn, "16 colors", "truecolor recommended; themes are approximated"
	case colorprofile.Ascii:
		it.Status, it.Value, it.Detail = style.StatusWarn, "no colors", "NO_COLOR or a dumb terminal"
	default:
		it.Status, it.Value, it.Detail = style.StatusInfo, "not a terminal", "output is piped"
	}
	return it
}

func paths(d Deps) style.Section {
	s := style.Section{Name: "Paths"}
	dir := func(label, path string, mustExist bool) style.Item {
		it := style.Item{Label: label, Value: tildify(path, d.Env)}
		st, err := d.Stat(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			it.Status = style.StatusInfo
			if mustExist {
				it.Status = style.StatusWarn
			}
			it.Detail = "will be created on first use"
		case err != nil:
			it.Status, it.Detail = style.StatusFail, err.Error()
		case !st.IsDir():
			it.Status, it.Detail = style.StatusFail, "exists but is not a directory"
		default:
			it.Status = style.StatusOK
		}
		return it
	}
	s.Items = append(s.Items,
		dir("config", d.Paths.Config, false),
		dir("data", d.Paths.Data, false),
		dir("cache", d.Paths.Cache, false),
		dir("logs", d.Paths.State, false),
	)
	sock := d.Paths.IPCSocket(d.Env)
	it := style.Item{Label: "ipc socket", Value: tildify(sock, d.Env)}
	if d.Env.GOOS == "windows" {
		it.Status = style.StatusOK
	} else if err := platform.CheckSocketPath(sock); err != nil {
		it.Status, it.Detail = style.StatusFail, err.Error()
	} else {
		it.Status, it.Detail = style.StatusOK, fmt.Sprintf("%d/%d bytes", len(sock), platform.MaxSocketPath)
	}
	s.Items = append(s.Items, it)
	return s
}

func configSection(d Deps) style.Section {
	s := style.Section{Name: "Config"}
	file := d.Paths.ConfigFile()
	it := style.Item{Label: "config.toml", Value: tildify(file, d.Env)}
	if _, err := d.Stat(file); errors.Is(err, fs.ErrNotExist) {
		it.Status, it.Detail = style.StatusInfo, "not present; using defaults"
		s.Items = append(s.Items, it)
		return s
	}
	errs, warns := d.Diags.Count(config.SevError), d.Diags.Count(config.SevWarning)
	if errs+warns > 0 {
		// The individual diagnostics below carry the status; don't count twice.
		it.Status = style.StatusInfo
		it.Detail = "see below, or run `ytm config check`"
	} else {
		it.Status = style.StatusOK
	}
	s.Items = append(s.Items, it)
	for _, diag := range d.Diags {
		if diag.Severity == config.SevInfo {
			continue
		}
		st := style.StatusWarn
		if diag.Severity == config.SevError {
			st = style.StatusFail
		}
		label := "  -"
		if diag.Line > 0 {
			label = fmt.Sprintf("  line %d", diag.Line)
		}
		s.Items = append(s.Items, style.Item{Label: label, Status: st, Value: diag.Key, Detail: diag.Message})
	}
	s.Items = append(s.Items, style.Item{
		Label: "theme", Status: style.StatusOK,
		Value:  fmt.Sprintf("%s (%s)", d.Config.Theme.Name, d.Config.Theme.Source),
		Detail: transparentNote(d.Config.Theme.Transparent),
	})
	return s
}

func transparentNote(t bool) string {
	if t {
		return "transparent"
	}
	return ""
}

func tildify(path string, e platform.Env) string {
	if e.HomeDir == nil {
		return path
	}
	home, err := e.HomeDir()
	if err != nil || home == "" || !strings.HasPrefix(path, home) {
		return path
	}
	rest := path[len(home):]
	if rest == "" || rest[0] == '/' || rest[0] == '\\' {
		return "~" + rest
	}
	return path
}
