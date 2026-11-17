package theme

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/Franciss-Prog/go-ytm/internal/clock"
)

// DefaultDebounce coalesces the burst of events a palette tool makes while
// writing (spec 08, "Live reload").
const DefaultDebounce = 100 * time.Millisecond

// Watcher reloads the theme when its files change. It watches the parent
// directories (pywal and matugen replace files by rename, which would end
// a watch on the file itself); a directory that does not exist yet is
// covered by watching its nearest existing ancestor and re-arming when it
// appears.
type Watcher struct {
	Paths    []string
	Debounce time.Duration
	Clock    clock.Clock
	// Reload resolves the theme (runs on the watcher goroutine, never on
	// the UI goroutine).
	Reload func() Result
	// Send delivers a result that differs from the last one sent (by
	// theme hash, or error text). It must not block for long.
	Send func(Result)
	// Last is the result already applied at start (for deduplication).
	Last Result
	Log  *slog.Logger

	trigger chan struct{}
}

// NewWatcher returns a watcher with defaults filled in.
func NewWatcher(paths []string, reload func() Result, send func(Result), last Result) *Watcher {
	return &Watcher{
		Paths: paths, Debounce: DefaultDebounce, Clock: clock.Real{},
		Reload: reload, Send: send, Last: last,
		Log:     slog.New(slog.DiscardHandler),
		trigger: make(chan struct{}, 1),
	}
}

// Trigger requests an immediate reload whose result is always sent, even
// when unchanged (SIGUSR1, `ytm theme reload`, the command palette). It
// never blocks; triggers coalesce.
func (w *Watcher) Trigger() {
	select {
	case w.trigger <- struct{}{}:
	default:
	}
}

func resultKey(r Result) string {
	if r.Err != nil {
		return "err:" + r.Err.Error()
	}
	return fmt.Sprintf("ok:%x", r.Theme.Hash())
}

// Run watches until ctx ends. It returns an error only when the OS file
// watcher cannot be created.
func (w *Watcher) Run(ctx context.Context) error {
	if w.trigger == nil {
		w.trigger = make(chan struct{}, 1)
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("theme watcher: %w", err)
	}
	defer fsw.Close()

	watched := map[string]bool{}
	arm := func() {
		for _, p := range w.Paths {
			dir := nearestDir(filepath.Dir(p))
			if dir == "" || watched[dir] {
				continue
			}
			if err := fsw.Add(dir); err != nil {
				w.Log.Warn("theme: cannot watch directory", "dir", dir, "err", err)
				continue
			}
			watched[dir] = true
			w.Log.Debug("theme: watching", "dir", dir)
		}
	}
	arm()

	last := resultKey(w.Last)
	var timer clock.Timer
	var fire <-chan time.Time
	reload := func(force bool) {
		r := w.Reload()
		k := resultKey(r)
		if !force && k == last {
			w.Log.Debug("theme: unchanged after file event")
			return
		}
		last = k
		w.Send(r)
	}

	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return nil
		case ev, ok := <-fsw.Events:
			if !ok {
				return nil
			}
			if !w.relevant(ev.Name) {
				continue
			}
			if ev.Has(fsnotify.Create) {
				arm() // a missing directory on a watched path may have appeared
			}
			if timer != nil {
				timer.Stop()
			}
			timer = w.Clock.NewTimer(w.Debounce)
			fire = timer.C()
		case err, ok := <-fsw.Errors:
			if !ok {
				return nil
			}
			w.Log.Warn("theme: watch error", "err", err)
		case <-fire:
			timer, fire = nil, nil
			reload(false)
		case <-w.trigger:
			reload(true)
		}
	}
}

// relevant reports whether an event on name concerns a watched file: the
// file itself, or a directory on the way to it.
func (w *Watcher) relevant(name string) bool {
	name = filepath.Clean(name)
	for _, p := range w.Paths {
		p = filepath.Clean(p)
		if name == p || strings.HasPrefix(filepath.Dir(p)+string(filepath.Separator), name+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// nearestDir returns dir or its closest existing ancestor ("" if none).
func nearestDir(dir string) string {
	for {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
