// Package platform resolves per-OS directories and other platform details.
package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
)

// AppName is the directory name used under every base directory.
const AppName = "ytm-go"

// MaxSocketPath is the longest Unix socket path we allow. Linux allows 108
// bytes (including the NUL), macOS 104; we use the stricter limit everywhere.
const MaxSocketPath = 103

// Env abstracts environment and OS lookups so path resolution is testable.
type Env struct {
	GOOS    string
	Getenv  func(string) string
	HomeDir func() (string, error)
	UID     int
}

// DefaultEnv returns an Env backed by the real process environment.
func DefaultEnv() Env {
	return Env{GOOS: runtime.GOOS, Getenv: os.Getenv, HomeDir: os.UserHomeDir, UID: os.Getuid()}
}

// Paths holds every directory the application uses.
type Paths struct {
	Config  string `json:"config"`
	Data    string `json:"data"`
	Cache   string `json:"cache"`
	State   string `json:"state"`
	Runtime string `json:"runtime"`
}

// ConfigFile returns the path of config.toml.
func (p Paths) ConfigFile() string { return filepath.Join(p.Config, "config.toml") }

// KeymapFile returns the path of keymap.toml.
func (p Paths) KeymapFile() string { return filepath.Join(p.Config, "keymap.toml") }

// ThemeFile returns the path of theme.toml.
func (p Paths) ThemeFile() string { return filepath.Join(p.Config, "theme.toml") }

// LogFile returns the path of the main log file.
func (p Paths) LogFile() string { return filepath.Join(p.State, "ytm.log") }

// IPCSocket returns the path (or pipe name on Windows) of the CLI↔TUI socket.
func (p Paths) IPCSocket(e Env) string {
	if e.GOOS == "windows" {
		return `\\.\pipe\` + AppName + "-ipc"
	}
	return filepath.Join(p.Runtime, "ipc.sock")
}

// Resolve computes all directories. configOverride, when non-empty, replaces
// the config directory (from --config-dir or YTM_GO_CONFIG_DIR).
func Resolve(e Env, configOverride string) (Paths, error) {
	home, err := e.HomeDir()
	if err != nil || home == "" {
		return Paths{}, fmt.Errorf("resolve home directory: %w", errors.Join(err, errors.New("no home directory")))
	}
	get := func(k, def string) string {
		if v := e.Getenv(k); v != "" && filepath.IsAbs(v) {
			return v
		}
		return def
	}
	var p Paths
	switch e.GOOS {
	case "windows":
		appData := get("APPDATA", filepath.Join(home, "AppData", "Roaming"))
		local := get("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
		p = Paths{
			Config:  filepath.Join(appData, AppName),
			Data:    filepath.Join(local, AppName),
			Cache:   filepath.Join(local, AppName, "cache"),
			State:   filepath.Join(local, AppName, "logs"),
			Runtime: filepath.Join(local, AppName, "run"),
		}
	case "darwin":
		tmp := get("TMPDIR", "/tmp")
		p = Paths{
			Config:  filepath.Join(get("XDG_CONFIG_HOME", filepath.Join(home, ".config")), AppName),
			Data:    filepath.Join(home, "Library", "Application Support", AppName),
			Cache:   filepath.Join(home, "Library", "Caches", AppName),
			State:   filepath.Join(home, "Library", "Logs", AppName),
			Runtime: filepath.Join(tmp, AppName),
		}
	default:
		rt := e.Getenv("XDG_RUNTIME_DIR")
		if rt == "" || !filepath.IsAbs(rt) {
			rt = "/tmp/" + AppName + "-" + strconv.Itoa(e.UID)
		} else {
			rt = filepath.Join(rt, AppName)
		}
		p = Paths{
			Config:  filepath.Join(get("XDG_CONFIG_HOME", filepath.Join(home, ".config")), AppName),
			Data:    filepath.Join(get("XDG_DATA_HOME", filepath.Join(home, ".local", "share")), AppName),
			Cache:   filepath.Join(get("XDG_CACHE_HOME", filepath.Join(home, ".cache")), AppName),
			State:   filepath.Join(get("XDG_STATE_HOME", filepath.Join(home, ".local", "state")), AppName),
			Runtime: rt,
		}
	}
	if configOverride == "" {
		configOverride = e.Getenv("YTM_GO_CONFIG_DIR")
	}
	if configOverride != "" {
		abs, err := filepath.Abs(ExpandHome(configOverride, home))
		if err != nil {
			return Paths{}, fmt.Errorf("config dir %q: %w", configOverride, err)
		}
		p.Config = abs
	}
	return p, nil
}

// ExpandHome replaces a leading "~" with home.
func ExpandHome(path, home string) string {
	if path == "~" {
		return home
	}
	if len(path) >= 2 && path[0] == '~' && (path[1] == '/' || path[1] == '\\') {
		return filepath.Join(home, path[2:])
	}
	return path
}

// CheckSocketPath returns an error when a Unix socket path exceeds the
// portable length limit.
func CheckSocketPath(path string) error {
	if len(path) > MaxSocketPath {
		return fmt.Errorf("socket path is %d bytes, limit is %d: %s (set XDG_RUNTIME_DIR to a shorter directory)", len(path), MaxSocketPath, path)
	}
	return nil
}
