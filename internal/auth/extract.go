package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Browsers lists the browsers `ytm setup` can read, in auto-detect order
// (docs/specs/03-ytmusic-api.md).
var Browsers = []string{"firefox", "zen", "chrome", "chromium", "brave", "edge", "vivaldi", "opera", "helium"}

// ErrNoSession means a browser was read but holds no signed-in YouTube session.
var ErrNoSession = errors.New("no signed-in YouTube session found")

// Extractor reads cookies from a browser profile.
type Extractor interface {
	Extract(ctx context.Context, browser string) ([]Cookie, error)
}

// Runner runs a command and returns its stderr; tests replace it.
type Runner func(ctx context.Context, name string, args ...string) (stderr []byte, err error)

// ExecRunner runs commands with os/exec.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = &stderr
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	return stderr.Bytes(), err
}

// YtDlp extracts cookies by running
//
//	yt-dlp --cookies-from-browser SPEC --cookies FILE
//
// with no URL. yt-dlp loads the browser jar, fails argument validation
// ("You must provide at least one URL", exit 2) and saves the jar to FILE
// on the way out (YoutubeDL.__exit__ → close → save_cookies, yt-dlp
// 2026.10.26). This takes ~0.25 s and makes no network requests; passing a
// URL instead costs a page fetch (~2.8 s) and still exits non-zero for
// music.youtube.com ("Unsupported URL"). Success is therefore judged by
// the cookie file, not the exit status.
type YtDlp struct {
	Binary string // default "yt-dlp"
	Run    Runner // default ExecRunner
	GOOS   string
	Home   string
	Getenv func(string) string
	// TempDir is where the transient cookie file is written (default os.TempDir).
	TempDir string
}

// Extract implements Extractor. Zen and Helium are not yt-dlp browsers;
// they are read as Firefox/Chromium profiles at their own paths. On Linux,
// Chromium-family browsers that yield no session are retried with each
// keyring backend, because yt-dlp guesses the keyring from the desktop
// session and picks basictext on unrecognized desktops (ytm-player
// services/auth.py _extract_browser_jar).
func (y YtDlp) Extract(ctx context.Context, browser string) ([]Cookie, error) {
	specs, err := y.specs(browser)
	if err != nil {
		return nil, err
	}
	var first []Cookie
	var firstErr error
	readOne := false
	for i, spec := range specs {
		jar, err := y.extractSpec(ctx, spec)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			if i == 0 && browser != "zen" {
				// Not installed / no profile: keyring retries cannot help.
				return nil, err
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if HasSession(jar, time.Now()) {
			return jar, nil
		}
		if !readOne {
			first, readOne = jar, true
		}
	}
	if readOne {
		return first, nil // readable but signed out: the caller reports ErrNoSession
	}
	return nil, firstErr
}

func (y YtDlp) getenv(k string) string {
	if y.Getenv != nil {
		return y.Getenv(k)
	}
	return os.Getenv(k)
}

// specs returns the --cookies-from-browser values to try, in order.
func (y YtDlp) specs(browser string) ([]string, error) {
	goos := y.GOOS
	if goos == "" {
		goos = runtimeGOOS
	}
	home := y.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	chromium := func(name, profile string) []string {
		base := name
		suffix := ""
		if profile != "" {
			suffix = ":" + profile
		}
		out := []string{base + suffix}
		if goos == "linux" {
			for _, k := range []string{"gnomekeyring", "kwallet6", "kwallet5", "kwallet", "basictext"} {
				out = append(out, base+"+"+k+suffix)
			}
		}
		return out
	}
	switch browser {
	case "firefox":
		return []string{"firefox"}, nil
	case "zen":
		var out []string
		for _, root := range zenRoots(goos, home, y.getenv) {
			if fi, err := os.Stat(root); err == nil && fi.IsDir() {
				out = append(out, "firefox:"+root)
			}
		}
		if len(out) == 0 {
			return nil, errors.New("no Zen profile directory found")
		}
		return out, nil
	case "helium":
		cfg := y.getenv("XDG_CONFIG_HOME")
		if cfg == "" {
			cfg = filepath.Join(home, ".config")
		}
		// Linux layout only (as in ytm-player); other platforms unverified.
		return chromium("chromium", filepath.Join(cfg, "net.imput.helium")), nil
	case "chrome", "chromium", "brave", "edge", "vivaldi", "opera":
		return chromium(browser, ""), nil
	}
	return nil, fmt.Errorf("unsupported browser %q (want one of %s)", browser, strings.Join(Browsers, ", "))
}

// zenRoots are Zen's profile roots (ytm-player services/auth.py _zen_profile_roots).
func zenRoots(goos, home string, getenv func(string) string) []string {
	switch goos {
	case "windows":
		var out []string
		for _, k := range []string{"APPDATA", "LOCALAPPDATA"} {
			if d := getenv(k); d != "" {
				out = append(out, filepath.Join(d, "zen"))
			}
		}
		return out
	case "darwin":
		return []string{filepath.Join(home, "Library", "Application Support", "zen")}
	}
	cfg := getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(home, ".config")
	}
	flatpak := filepath.Join(home, ".var", "app", "app.zen_browser.zen")
	cands := []string{
		filepath.Join(home, ".zen"), filepath.Join(cfg, "zen"),
		filepath.Join(flatpak, ".zen"), filepath.Join(flatpak, "zen"), filepath.Join(flatpak, "config", "zen"),
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range cands {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

func (y YtDlp) extractSpec(ctx context.Context, spec string) ([]Cookie, error) {
	dir, err := os.MkdirTemp(y.TempDir, "ytm-cookies-")
	if err != nil {
		return nil, fmt.Errorf("cookie temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	file := filepath.Join(dir, "cookies.txt")

	bin := y.Binary
	if bin == "" {
		bin = "yt-dlp"
	}
	run := y.Run
	if run == nil {
		run = ExecRunner
	}
	stderr, runErr := run(ctx, bin, "--ignore-config", "--no-warnings", "--cookies-from-browser", spec, "--cookies", file)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	f, err := os.Open(file)
	if err != nil {
		var ee *exec.Error
		if errors.As(runErr, &ee) || errors.Is(runErr, fs.ErrNotExist) {
			return nil, fmt.Errorf("run %s: %w", bin, runErr)
		}
		return nil, fmt.Errorf("yt-dlp could not read %s cookies: %s", spec, ytdlpError(stderr, runErr))
	}
	defer func() { _ = f.Close() }()
	return ParseNetscape(f)
}

// ytdlpError picks the most useful line from yt-dlp's stderr.
func ytdlpError(stderr []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(string(stderr)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); strings.HasPrefix(l, "ERROR:") {
			return strings.TrimSpace(strings.TrimPrefix(l, "ERROR:"))
		}
	}
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	if err != nil {
		return err.Error()
	}
	return "no cookies written"
}

// FileExtractor reads a Netscape cookies file ([yt_dlp] cookies_file).
func FileExtractor(path string) ([]Cookie, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open cookies file: %w", err)
	}
	defer func() { _ = f.Close() }()
	return ParseNetscape(f)
}
