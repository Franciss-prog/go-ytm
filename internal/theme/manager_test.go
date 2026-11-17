package theme

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"go.uber.org/goleak"

	"github.com/Franciss-Prog/go-ytm/internal/clock"
)

func TestManager(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	wal := filepath.Join(dir, "colors.json")
	writeAtomic(t, wal, walA)

	sends := make(chan Result, 16)
	clk := clock.NewFake(time.Unix(0, 0))
	spec := Spec{Source: "static", Name: "nord", ConfigDir: dir, PywalPath: wal}
	m := NewManager(spec, func(r Result) { sends <- r }, clk, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.Run(ctx, Resolve(spec)) }()
	recv := func() Result {
		t.Helper()
		select {
		case r := <-sends:
			return r
		case <-time.After(2 * time.Second):
			t.Fatal("nothing sent")
			return Result{}
		}
	}

	// Reload without watching resolves directly.
	m.Reload()
	if r := recv(); r.Cause != CauseReload || r.Theme.Name != "nord" {
		t.Errorf("reload: %v %s", r.Cause, r.Theme.Name)
	}

	// Switch to pywal with watching: sent as a settings change.
	s2 := spec
	s2.Source, s2.Watch = "pywal", true
	m.Set(s2)
	if r := recv(); r.Cause != CauseSettings || FormatColor(r.Theme.Bg) != "#101010" {
		t.Errorf("set: %v %s", r.Cause, FormatColor(r.Theme.Bg))
	}

	// A forced reload round trip proves the restarted watcher has armed.
	m.Reload()
	recv()
	// File change reaches the UI through the restarted watcher.
	writeAtomic(t, wal, walB)
	stop := make(chan struct{})
	tm := time.AfterFunc(2*time.Second, func() { close(stop) })
	defer tm.Stop()
	if !clk.BlockUntil(1, stop) {
		t.Fatal("watcher did not arm a debounce timer")
	}
	// A rename emits several events on kqueue and Windows; a late one
	// re-arms the debounce timer after an Advance, so keep firing it until
	// the result arrives (as watchRig.step does).
	var r Result
	for got := false; !got; {
		clk.Advance(DefaultDebounce)
		select {
		case r = <-sends:
			got = true
		case <-time.After(50 * time.Millisecond):
		case <-stop:
			t.Fatal("nothing sent after file change")
		}
	}
	if r.Cause != CauseFile || FormatColor(r.Theme.Bg) != "#fafafa" {
		t.Errorf("file: %v %s", r.Cause, FormatColor(r.Theme.Bg))
	}

	// Reload while watching goes through the watcher, forced.
	m.Reload()
	if r := recv(); r.Cause != CauseReload {
		t.Errorf("reload while watching: %v", r.Cause)
	}

	// Terminal bg only matters in transparent mode.
	m.SetTerminalBg(lipgloss.Color("#ffffff"))
	s3 := s2
	s3.Source, s3.Transparent, s3.EnforceContrast, s3.Watch = "static", true, true, false
	s3.Name = "ytm-dark"
	m.Set(s3)
	if r := recv(); r.Against != "#ffffff" {
		t.Errorf("against = %q, want the terminal bg", r.Against)
	}
	cancel()
	<-done
	_ = os.Remove(wal)
}
