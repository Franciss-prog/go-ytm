package ui

import (
	"strings"
	"testing"

	"github.com/Franciss-Prog/go-ytm/internal/player"
)

type fakeSpectrum struct{ calls []bool }

func (f *fakeSpectrum) SetActive(on bool) { f.calls = append(f.calls, on) }

func TestSpectrumFollowsPageAndPlayback(t *testing.T) {
	fs := &fakeSpectrum{}
	m, _ := newTestModel(t, 140, 40, func(o *Options) { o.Spectrum = fs })
	m.Queue().AddMany(scenarioTracks())
	m.Queue().JumpTo(0)
	update(m, PlayerEventMsg{playerState(playing)})
	if len(fs.calls) != 0 {
		t.Fatalf("captured off the track page: %v", fs.calls)
	}
	drain(m, press(m, ch('g'), ch('n')))
	if len(fs.calls) != 1 || !fs.calls[0] {
		t.Fatalf("track page + playing: %v", fs.calls)
	}
	// Levels draw as eighth blocks; a full band reaches the top row.
	levels := make([]float64, 96)
	for i := range levels {
		levels[i] = float64(i) / 95
	}
	update(m, SpectrumMsg{Levels: levels})
	out := plain(m)
	if !strings.Contains(out, "▃") || !strings.Contains(out, "▁") { // ▃ only appears in the spectrum
		t.Fatalf("no spectrum:\n%s", out)
	}
	// Pausing stops the capture and clears the bars.
	paused := playing
	paused.Status, paused.Paused = player.Paused, true
	update(m, PlayerEventMsg{playerState(paused)})
	if fs.calls[len(fs.calls)-1] || strings.Contains(plain(m), "▃") {
		t.Fatalf("pause: %v", fs.calls)
	}
	// Late levels after the stop are ignored.
	update(m, SpectrumMsg{Levels: levels})
	if strings.Contains(plain(m), "▃") {
		t.Error("levels drawn while stopped")
	}
	// visualizer = false never captures.
	fs2 := &fakeSpectrum{}
	m2, _ := newTestModel(t, 140, 40, func(o *Options) { o.Spectrum = fs2; o.Config.UI.Visualizer = false })
	update(m2, PlayerEventMsg{playerState(playing)})
	drain(m2, press(m2, ch('g'), ch('n')))
	if len(fs2.calls) != 0 {
		t.Errorf("visualizer off: %v", fs2.calls)
	}
}
