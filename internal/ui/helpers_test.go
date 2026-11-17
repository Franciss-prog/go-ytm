package ui

import (
	"context"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Franciss-Prog/go-ytm/internal/config"
	"github.com/Franciss-Prog/go-ytm/internal/player"
	"github.com/Franciss-Prog/go-ytm/internal/theme"
)

// fakePlayer records commands; State returns st.
type fakePlayer struct {
	mu    sync.Mutex
	st    player.State
	calls []string
	seeks []time.Duration
	modes []player.SeekMode
	items []player.Item // Load and Append arguments
	at    []time.Duration
}

func (f *fakePlayer) rec(s string) error {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
	return nil
}

func (f *fakePlayer) Load(_ context.Context, it player.Item, at time.Duration) error {
	f.mu.Lock()
	f.items, f.at = append(f.items, it), append(f.at, at)
	f.mu.Unlock()
	return f.rec("load")
}

func (f *fakePlayer) Append(it player.Item) error {
	f.mu.Lock()
	f.items, f.at = append(f.items, it), append(f.at, 0)
	f.mu.Unlock()
	return f.rec("append")
}

func (f *fakePlayer) ClearNext() error {
	return f.rec("clear_next")
}

func (f *fakePlayer) TogglePause() error {
	return f.rec("toggle_pause")
}

func (f *fakePlayer) Play() error {
	return f.rec("play")
}

func (f *fakePlayer) Pause() error {
	return f.rec("pause")
}

func (f *fakePlayer) Seek(d time.Duration, m player.SeekMode) error {
	f.mu.Lock()
	f.seeks, f.modes = append(f.seeks, d), append(f.modes, m)
	f.mu.Unlock()
	return f.rec("seek")
}

func (f *fakePlayer) SetVolume(v int) error {
	f.st.Volume = v
	return f.rec("volume")
}

func (f *fakePlayer) ToggleMute() error {
	return f.rec("mute")
}

func (f *fakePlayer) State() player.State {
	return f.st
}

func (f *fakePlayer) Events() <-chan player.Event {
	return nil
}

func (f *fakePlayer) Close(context.Context) error {
	return nil
}

func (f *fakePlayer) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

var _ player.Player = (*fakePlayer)(nil)

func newTestModel(t testing.TB, w, h int, mut ...func(*Options)) (*Model, *fakePlayer) {
	t.Helper()
	fp := &fakePlayer{st: player.State{Volume: 80}}
	o := Options{Config: config.Default(), Theme: theme.Fallback(), Player: fp, KeymapPath: "~/.config/ytm-go/keymap.toml"}
	for _, f := range mut {
		f(&o)
	}
	m := New(o)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m, fp
}

func press(m *Model, keys ...tea.Key) tea.Cmd {
	var last tea.Cmd
	for _, k := range keys {
		_, last = m.Update(tea.KeyPressMsg(k))
	}
	return last
}

func ch(r rune) tea.Key { return tea.Key{Code: r, Text: string(r)} }

func plain(m *Model) string { return ansi.Strip(m.View().Content) }

var playing = player.State{
	Status: player.Playing, Volume: 80,
	Item:     player.Item{ID: "abc", Title: "Instant Crush"},
	Position: 133 * time.Second, Duration: 337 * time.Second,
}

func playerState(s player.State) player.Event { return player.StateChanged{State: s} }
