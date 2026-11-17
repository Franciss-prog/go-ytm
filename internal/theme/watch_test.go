package theme

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/clock"
)

const (
	walA = `{"special":{"background":"#101010","foreground":"#eeeeee"},"colors":{"color4":"#3366ff"}}`
	walB = `{"special":{"background":"#fafafa","foreground":"#111111"},"colors":{"color4":"#cc3300"}}`
)

type watchRig struct {
	t      *testing.T
	clk    *clock.Fake
	w      *Watcher
	path   string
	sends  chan Result
	cancel context.CancelFunc
	done   chan struct{}
}

func newWatchRig(t *testing.T, path string) *watchRig {
	t.Helper()
	spec := Spec{Source: "pywal", PywalPath: path, ConfigDir: filepath.Join(filepath.Dir(path), "cfg"), Fallback: "ytm-dark"}
	r := &watchRig{t: t, clk: clock.NewFake(time.Unix(0, 0)), path: path, sends: make(chan Result, 16), done: make(chan struct{})}
	r.w = NewWatcher(spec.WatchPaths(), func() Result { return Resolve(spec) }, func(res Result) { r.sends <- res }, Resolve(spec))
	r.w.Clock = r.clk
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go func() {
		defer close(r.done)
		_ = r.w.Run(ctx)
	}()
	// Run arms its watches before looping: a forced reload round trip
	// proves it is ready.
	r.w.Trigger()
	r.recv()
	t.Cleanup(func() { cancel(); <-r.done })
	return r
}

// step performs op, waits for the debounce timer, fires it and returns
// the result sent (nil if none within a short wait).
func (r *watchRig) step(op func()) *Result {
	r.t.Helper()
	op()
	stop := make(chan struct{})
	timer := time.AfterFunc(2*time.Second, func() { close(stop) })
	defer timer.Stop()
	if !r.clk.BlockUntil(1, stop) {
		r.t.Fatal("no debounce timer armed after file change")
	}
	r.clk.Advance(DefaultDebounce)
	select {
	case res := <-r.sends:
		return &res
	case <-time.After(300 * time.Millisecond):
		return nil
	}
}

func (r *watchRig) recv() Result {
	r.t.Helper()
	select {
	case res := <-r.sends:
		return res
	case <-time.After(2 * time.Second):
		r.t.Fatal("no result sent")
		return Result{}
	}
}

func writeAtomic(t *testing.T, path, data string) {
	t.Helper()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func bgOf(r *Result) string { return FormatColor(r.Theme.Bg) }

func TestWatchRenameAndPartialWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "colors.json")
	writeAtomic(t, path, walA)
	r := newWatchRig(t, path)

	got := r.step(func() { writeAtomic(t, path, walB) })
	if got == nil || got.Err != nil || bgOf(got) != "#fafafa" {
		t.Fatalf("after rename: %+v", got)
	}
	// Half-written file: an error result (UI keeps the current theme).
	got = r.step(func() { _ = os.WriteFile(path, []byte(walA[:20]), 0o644) })
	if got == nil || got.Err == nil {
		t.Fatalf("partial write: want error result, got %+v", got)
	}
	got = r.step(func() { _ = os.WriteFile(path, []byte(walA), 0o644) })
	if got == nil || got.Err != nil || bgOf(got) != "#101010" {
		t.Fatalf("completed write: %+v", got)
	}
}

func TestWatchIdenticalContentSendsNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "colors.json")
	writeAtomic(t, path, walA)
	r := newWatchRig(t, path)
	if got := r.step(func() { writeAtomic(t, path, walA) }); got != nil {
		t.Fatalf("identical rewrite sent %+v", got)
	}
	r.w.Trigger()
	if got := r.recv(); got.Err != nil || bgOf(&got) != "#101010" {
		t.Fatalf("forced reload: %+v", got)
	}
}

func TestWatchDeleteThenRecreate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "colors.json")
	writeAtomic(t, path, walA)
	r := newWatchRig(t, path)
	got := r.step(func() { _ = os.Remove(path) })
	if got == nil || got.Err == nil || got.Theme.Name != "ytm-dark" {
		t.Fatalf("delete: want error + fallback, got %+v", got)
	}
	got = r.step(func() { writeAtomic(t, path, walB) })
	if got == nil || got.Err != nil || bgOf(got) != "#fafafa" {
		t.Fatalf("recreate: %+v", got)
	}
}

func TestWatchDirectoryCreatedLater(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cache", "wal", "colors.json")
	r := newWatchRig(t, path)
	// mkdir cache → re-arm deeper; mkdir wal → re-arm; then the file.
	for _, d := range []string{filepath.Join(root, "cache"), filepath.Join(root, "cache", "wal")} {
		_ = r.step(func() {
			if err := os.Mkdir(d, 0o755); err != nil {
				t.Fatal(err)
			}
		})
	}
	got := r.step(func() { writeAtomic(t, path, walB) })
	if got == nil || got.Err != nil || bgOf(got) != "#fafafa" {
		t.Fatalf("file in new directory: %+v", got)
	}
}

func TestWatchRelevant(t *testing.T) {
	w := &Watcher{Paths: []string{"/a/b/c.json"}}
	for name, want := range map[string]bool{"/a/b/c.json": true, "/a/b": true, "/a": true, "/a/b/d.json": false, "/a/bb": false} {
		if got := w.relevant(name); got != want {
			t.Errorf("relevant(%s) = %v", name, got)
		}
	}
}
