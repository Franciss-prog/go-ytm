package player

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestLauncherArgs(t *testing.T) {
	l := &MPVLauncher{ExtraArgs: []string{"--ao=null"}}
	args := l.Args("/run/x.sock", LaunchOptions{Volume: 250})
	for _, want := range []string{"--no-config", "--idle=yes", "--input-ipc-server=/run/x.sock", "--volume=100", "--ytdl=no", "--ao=null"} {
		if !slices.Contains(args, want) {
			t.Errorf("args missing %q: %v", want, args)
		}
	}
	if args[len(args)-1] != "--ao=null" {
		t.Error("extra args must come last so they can override defaults")
	}
	l.UseConfig = true
	if slices.Contains(l.Args("s", LaunchOptions{}), "--no-config") {
		t.Error("--no-config passed with UseConfig")
	}
}

func TestLaunchMPVNotFound(t *testing.T) {
	l := &MPVLauncher{Binary: "definitely-not-mpv-xyz", RuntimeDir: t.TempDir()}
	if _, err := l.Launch(context.Background(), LaunchOptions{}); !errors.Is(err, ErrMPVNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "ytm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

func TestPrepareEndpointUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("named pipes: nothing to prepare")
	}
	ctx := context.Background()

	long := filepath.Join(shortTempDir(t), strings.Repeat("d", 120))
	if _, err := prepareEndpoint(ctx, long, 1); err == nil || !strings.Contains(err.Error(), "XDG_RUNTIME_DIR") {
		t.Fatalf("long path err = %v", err)
	}

	dir := filepath.Join(shortTempDir(t), "rt")
	path, err := prepareEndpoint(ctx, dir, 42)
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("runtime dir mode: %v %v", fi.Mode(), err)
	}

	// A stale socket file (nobody listening) is removed.
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = ln.Close()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("stale socket not created: %v", err)
	}
	if _, err := prepareEndpoint(ctx, dir, 42); err != nil {
		t.Fatalf("stale socket: %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale socket not removed")
	}

	// A live socket is left alone.
	ln, err = net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		if c, err := ln.Accept(); err == nil {
			_ = c.Close()
		}
	}()
	if _, err := prepareEndpoint(ctx, dir, 42); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("live socket err = %v", err)
	}
}
