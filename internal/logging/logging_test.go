package logging

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ytm.log")
	w, err := NewRotatingWriter(path, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	line := []byte(strings.Repeat("x", 39) + "\n") // 40 bytes
	for range 10 {
		if _, err := w.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ytm.log", "ytm.log.1", "ytm.log.2"} {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if st.Size() > 100 {
			t.Errorf("%s is %d bytes, over limit", name, st.Size())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "ytm.log.3")); !os.IsNotExist(err) {
		t.Error("backup_count not enforced")
	}
}

func TestRotationZeroBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ytm.log")
	w, err := NewRotatingWriter(path, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for range 5 {
		if _, err := w.Write([]byte(strings.Repeat("y", 29) + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Error("no backups expected")
	}
}

func TestConcurrentWrites(t *testing.T) {
	w, err := NewRotatingWriter(filepath.Join(t.TempDir(), "ytm.log"), 1000, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				_, _ = w.Write([]byte("concurrent line\n"))
			}
		})
	}
	wg.Wait()
}

func TestRedactionFileAndStderr(t *testing.T) {
	dir := t.TempDir()
	var stderr bytes.Buffer
	logger, closer, err := Setup(Options{
		File: filepath.Join(dir, "ytm.log"), Level: slog.LevelInfo, MaxBytes: 1 << 20,
		Debug: true, Stderr: &stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("request", "Cookie", "SAPISID=supersecret", "url", "https://music.youtube.com",
		slog.Group("headers", "Authorization", "SAPISIDHASH supersecret2"))
	logger.With("api_key", "supersecret3").Debug("lastfm")
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.ReadFile(filepath.Join(dir, "ytm.log"))
	if err != nil {
		t.Fatal(err)
	}
	for name, out := range map[string]string{"file": string(file), "stderr": stderr.String()} {
		if strings.Contains(out, "supersecret") {
			t.Errorf("%s leaked a secret:\n%s", name, out)
		}
		if !strings.Contains(out, "REDACTED") || !strings.Contains(out, "music.youtube.com") {
			t.Errorf("%s missing expected content:\n%s", name, out)
		}
	}
}

func TestSetupBadFileStillReturnsLogger(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	logger, closer, err := Setup(Options{File: filepath.Join(blocker, "sub", "ytm.log")})
	if err == nil {
		t.Error("expected error")
	}
	logger.Warn("still works")
	_ = closer.Close()
}

func TestLevelFromConfig(t *testing.T) {
	for in, want := range map[string]slog.Level{"DEBUG": slog.LevelDebug, "info": slog.LevelInfo, "WARNING": slog.LevelWarn, "CRITICAL": slog.LevelError, "": slog.LevelWarn} {
		if got := LevelFromConfig(in); got != want {
			t.Errorf("%q: %v want %v", in, got, want)
		}
	}
}
