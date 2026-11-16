package platform

import (
	"path/filepath"
	"strings"
	"testing"
)

func env(goos string, vars map[string]string) Env {
	return Env{
		GOOS:    goos,
		Getenv:  func(k string) string { return vars[k] },
		HomeDir: func() (string, error) { return "/home/u", nil },
		UID:     1000,
	}
}

func TestResolveLinux(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]string
		want Paths
	}{
		{
			name: "defaults",
			vars: nil,
			want: Paths{
				Config:  "/home/u/.config/ytm-go",
				Data:    "/home/u/.local/share/ytm-go",
				Cache:   "/home/u/.cache/ytm-go",
				State:   "/home/u/.local/state/ytm-go",
				Runtime: "/tmp/ytm-go-1000",
			},
		},
		{
			name: "xdg overrides",
			vars: map[string]string{
				"XDG_CONFIG_HOME": "/x/c", "XDG_DATA_HOME": "/x/d", "XDG_CACHE_HOME": "/x/k",
				"XDG_STATE_HOME": "/x/s", "XDG_RUNTIME_DIR": "/run/user/1000",
			},
			want: Paths{Config: "/x/c/ytm-go", Data: "/x/d/ytm-go", Cache: "/x/k/ytm-go", State: "/x/s/ytm-go", Runtime: "/run/user/1000/ytm-go"},
		},
		{
			name: "relative xdg ignored per spec",
			vars: map[string]string{"XDG_CONFIG_HOME": "rel/dir"},
			want: Paths{
				Config:  "/home/u/.config/ytm-go",
				Data:    "/home/u/.local/share/ytm-go",
				Cache:   "/home/u/.cache/ytm-go",
				State:   "/home/u/.local/state/ytm-go",
				Runtime: "/tmp/ytm-go-1000",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if filepath.Separator != '/' {
				t.Skip("unix paths")
			}
			got, err := Resolve(env("linux", tt.vars), "")
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestResolveConfigOverride(t *testing.T) {
	if filepath.Separator != '/' {
		t.Skip("unix paths")
	}
	got, err := Resolve(env("linux", map[string]string{"YTM_GO_CONFIG_DIR": "/env/dir"}), "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Config != "/env/dir" {
		t.Errorf("env override: got %q", got.Config)
	}
	got, err = Resolve(env("linux", map[string]string{"YTM_GO_CONFIG_DIR": "/env/dir"}), "~/flag")
	if err != nil {
		t.Fatal(err)
	}
	if got.Config != "/home/u/flag" {
		t.Errorf("flag beats env and expands ~: got %q", got.Config)
	}
}

func TestResolveWindowsAndDarwin(t *testing.T) {
	w, err := Resolve(env("windows", map[string]string{"APPDATA": filepath.FromSlash("/C/AppData/Roaming"), "LOCALAPPDATA": filepath.FromSlash("/C/AppData/Local")}), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(w.Config, filepath.Join("Roaming", "ytm-go")) {
		t.Errorf("windows config: %q", w.Config)
	}
	if got := w.IPCSocket(env("windows", nil)); got != `\\.\pipe\ytm-go-ipc` {
		t.Errorf("windows pipe: %q", got)
	}
	d, err := Resolve(env("darwin", nil), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Cache, filepath.Join("Library", "Caches")) {
		t.Errorf("darwin cache: %q", d.Cache)
	}
}

func TestCheckSocketPath(t *testing.T) {
	if err := CheckSocketPath("/run/user/1000/ytm-go/ipc.sock"); err != nil {
		t.Errorf("short path rejected: %v", err)
	}
	long := "/" + strings.Repeat("a", MaxSocketPath)
	if err := CheckSocketPath(long); err == nil {
		t.Error("long path accepted")
	}
}

func TestExpandHome(t *testing.T) {
	for in, want := range map[string]string{"~": "/h", "~/a": "/h" + string(filepath.Separator) + "a", "/abs": "/abs", "~x": "~x"} {
		if got := ExpandHome(in, "/h"); got != want {
			t.Errorf("ExpandHome(%q) = %q, want %q", in, got, want)
		}
	}
}
