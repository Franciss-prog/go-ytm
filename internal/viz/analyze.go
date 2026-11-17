// Package viz is the spectrum visualizer (docs/specs/09-ytkew-look.md):
// it reads the audio sink's monitor, runs an FFT and reports log-spaced
// band levels. mpv gives no access to its samples, so the monitor (all
// system audio) is captured instead.
package viz

import (
	"math"
	"math/cmplx"
)

// Analysis constants (spec 09).
const (
	SampleRate = 48000
	FFTSize    = 2048
	MinFreq    = 45.0
	MaxFreq    = 16000.0
	// floorDB maps to level 0; full scale (0 dB) to 1.
	floorDB = -70.0
	// Decay is how much of the previous level a band keeps per frame when
	// the new one is lower.
	Decay = 0.86
)

// Analyzer turns sample windows into band levels in [0, 1]. Not safe for
// concurrent use.
type Analyzer struct {
	bands  int
	edges  []int // FFT bin edges, bands+1 entries
	window []float64
	buf    []complex128
	levels []float64
}

// NewAnalyzer returns an analyzer for n bands.
func NewAnalyzer(n int) *Analyzer {
	a := &Analyzer{bands: n, edges: BandEdges(n), window: hann(FFTSize), buf: make([]complex128, FFTSize), levels: make([]float64, n)}
	return a
}

// Process analyzes the last FFTSize samples (in [-1, 1]) and returns the
// decayed band levels (the slice is reused by the next call).
func (a *Analyzer) Process(samples []float64) []float64 {
	n := min(len(samples), FFTSize)
	clear(a.buf)
	off := len(samples) - n
	for i := range n {
		a.buf[i] = complex(samples[off+i]*a.window[i], 0)
	}
	FFT(a.buf)
	// A full-scale sine under a Hann window peaks at FFTSize/4.
	norm := float64(FFTSize) / 4
	for b := range a.bands {
		peak := 0.0
		for k := a.edges[b]; k < a.edges[b+1]; k++ {
			peak = max(peak, cmplx.Abs(a.buf[k])/norm)
		}
		lv := 0.0
		if peak > 0 {
			lv = (20*math.Log10(peak) - floorDB) / -floorDB
		}
		lv = min(max(lv, 0), 1)
		a.levels[b] = max(lv, a.levels[b]*Decay)
	}
	return a.levels
}

// BandEdges splits MinFreq..MaxFreq into n log-spaced bands and returns
// the FFT bin edges (n+1, strictly increasing, each band ≥ 1 bin).
func BandEdges(n int) []int {
	edges := make([]int, n+1)
	binHz := float64(SampleRate) / FFTSize
	ratio := math.Pow(MaxFreq/MinFreq, 1/float64(max(n, 1)))
	for i := range n + 1 {
		f := MinFreq * math.Pow(ratio, float64(i))
		edges[i] = int(math.Round(f / binHz))
		if i > 0 && edges[i] <= edges[i-1] {
			edges[i] = edges[i-1] + 1
		}
	}
	edges[n] = min(edges[n], FFTSize/2)
	return edges
}

func hann(n int) []float64 {
	w := make([]float64, n)
	for i := range n {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
	}
	return w
}

// FFT is an in-place iterative radix-2 Cooley–Tukey transform; len(x)
// must be a power of two.
func FFT(x []complex128) {
	n := len(x)
	for i, j := 1, 0; i < n; i++ { // bit-reversal permutation
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j |= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		step := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			w := complex(1, 0)
			for k := range size / 2 {
				u, v := x[start+k], x[start+k+size/2]*w
				x[start+k], x[start+k+size/2] = u+v, u-v
				w *= step
			}
		}
	}
}

// Resample maps band levels onto n display columns (nearest band).
func Resample(levels []float64, n int) []float64 {
	out := make([]float64, n)
	if len(levels) == 0 {
		return out
	}
	for i := range n {
		out[i] = levels[i*len(levels)/n]
	}
	return out
}
