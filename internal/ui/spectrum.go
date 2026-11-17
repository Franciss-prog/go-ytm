package ui

import (
	"strings"

	"github.com/Franciss-Prog/go-ytm/internal/player"
	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/viz"
)

// Spectrum is the visualizer as the UI drives it (viz.Visualizer).
// SetActive must not block.
type Spectrum interface{ SetActive(on bool) }

// SpectrumMsg carries band levels in [0, 1] (nil: capture stopped).
type SpectrumMsg struct{ Levels []float64 }

// spectrumHeight is the spectrum's height on the track page (spec 09).
const spectrumHeight = 6

// specState is what the track page reads.
type specState struct {
	enabled bool      // a Spectrum exists and [ui] visualizer is on
	active  bool      // last SetActive sent
	levels  []float64 // latest levels
}

// syncSpectrum runs after every Update: capture only while the track page
// shows and something plays.
func (m *Model) syncSpectrum() {
	s := &m.spec
	s.enabled = m.o.Spectrum != nil && m.o.Config.UI.Visualizer
	want := s.enabled && m.current == PageTrack && m.bar.st.Status == player.Playing
	if want != s.active {
		s.active = want
		m.o.Spectrum.SetActive(want)
		if !want {
			s.levels = nil
		}
	}
}

// spectrumLines draws levels as h rows of eighth blocks over w columns.
func spectrumLines(levels []float64, w, h int, st *style.UI) []string {
	cols := viz.Resample(levels, w)
	const ramp = " ▁▂▃▄▅▆▇█"
	glyphs := []rune(ramp)
	out := make([]string, h)
	for row := range h {
		var b strings.Builder
		base := (h - 1 - row) * 8 // eighths below this row
		for _, v := range cols {
			e := int(v*float64(h*8)+0.5) - base
			b.WriteRune(glyphs[min(max(e, 0), 8)])
		}
		out[row] = st.Accent.Render(b.String())
	}
	return out
}
