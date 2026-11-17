package viz

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"math/cmplx"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func sine(freq, amp float64, n int) []float64 {
	s := make([]float64, n)
	for i := range n {
		s[i] = amp * math.Sin(2*math.Pi*freq*float64(i)/SampleRate)
	}
	return s
}

func TestFFTPeakBin(t *testing.T) {
	for _, bin := range []int{3, 64, 500} {
		x := make([]complex128, FFTSize)
		for i := range x {
			x[i] = complex(math.Cos(2*math.Pi*float64(bin)*float64(i)/FFTSize), 0)
		}
		FFT(x)
		best := 0
		for k := 1; k < FFTSize/2; k++ {
			if cmplx.Abs(x[k]) > cmplx.Abs(x[best]) {
				best = k
			}
		}
		if best != bin || math.Abs(cmplx.Abs(x[bin])-FFTSize/2) > 1e-6 {
			t.Errorf("bin %d: peak at %d (%.1f)", bin, best, cmplx.Abs(x[bin]))
		}
	}
}

func TestBandEdgesMonotonic(t *testing.T) {
	for _, n := range []int{1, 16, 48, 160} {
		e := BandEdges(n)
		if len(e) != n+1 || e[n] > FFTSize/2 {
			t.Fatalf("%d bands: %v", n, e)
		}
		for i := 1; i <= n; i++ {
			if e[i] <= e[i-1] {
				t.Fatalf("%d bands: edge %d not increasing: %v", n, i, e)
			}
		}
	}
}

func TestAnalyzerFindsTheTone(t *testing.T) {
	a := NewAnalyzer(32)
	lv := a.Process(sine(1000, 0.5, FFTSize))
	edges := BandEdges(32)
	bin := int(math.Round(1000 / (float64(SampleRate) / FFTSize)))
	loud := -1
	for b := range 32 {
		if bin >= edges[b] && bin < edges[b+1] {
			loud = b
		}
	}
	if lv[loud] < 0.85 {
		t.Errorf("tone band %d level %.2f", loud, lv[loud])
	}
	for b, v := range lv {
		if (b < loud-2 || b > loud+2) && v > 0.5 {
			t.Errorf("band %d level %.2f far from the tone", b, v)
		}
	}
	// Silence: levels decay instead of dropping at once.
	prev := lv[loud]
	lv = a.Process(make([]float64, FFTSize))
	if math.Abs(lv[loud]-prev*Decay) > 1e-9 {
		t.Errorf("decay %.3f, want %.3f", lv[loud], prev*Decay)
	}
}

func TestResample(t *testing.T) {
	got := Resample([]float64{0.1, 0.2, 0.3, 0.4}, 8)
	if len(got) != 8 || got[0] != 0.1 || got[7] != 0.4 {
		t.Errorf("%v", got)
	}
	if len(Resample(nil, 3)) != 3 {
		t.Error("nil levels")
	}
}

// pcm is a Source serving a 1 kHz tone forever (or fail when set).
type pcm struct {
	fail  error
	mu    sync.Mutex
	opens int
}

type toneReader struct {
	ctx context.Context
	i   int
}

func (r *toneReader) Read(p []byte) (int, error) {
	if r.ctx.Err() != nil {
		return 0, io.EOF
	}
	n := 0
	for ; n+1 < len(p); n += 2 {
		v := int16(16000 * math.Sin(2*math.Pi*1000*float64(r.i)/SampleRate))
		binary.LittleEndian.PutUint16(p[n:], uint16(v))
		r.i++
	}
	time.Sleep(time.Millisecond) // a real capture paces itself
	return n, nil
}

func (r *toneReader) Close() error { return nil }

func (s *pcm) Open(ctx context.Context) (io.ReadCloser, error) {
	s.mu.Lock()
	s.opens++
	s.mu.Unlock()
	if s.fail != nil {
		return nil, s.fail
	}
	return &toneReader{ctx: ctx}, nil
}

func TestVisualizerStartStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan []float64, 1000)
	v := New(&pcm{}, 16, func(l []float64) { got <- l }, nil)
	done := make(chan struct{})
	go func() { v.Run(ctx); close(done) }()

	v.SetActive(true)
	select {
	case l := <-got:
		if len(l) != 16 {
			t.Fatalf("levels %d", len(l))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no levels while active")
	}
	v.SetActive(false)
	// The stop sends nil once the capture goroutine is gone.
	deadline := time.After(2 * time.Second)
	for stopped := false; !stopped; {
		select {
		case l := <-got:
			stopped = l == nil
		case <-deadline:
			t.Fatal("no stop")
		}
	}
	cancel()
	<-done
}

func TestVisualizerFailedOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := &pcm{fail: errors.New("no pw-cat")}
	got := make(chan []float64, 4)
	v := New(src, 8, func(l []float64) { got <- l }, nil)
	done := make(chan struct{})
	go func() { v.Run(ctx); close(done) }()
	v.SetActive(true)
	if l := <-got; l != nil {
		t.Fatalf("levels from a failed capture: %v", l)
	}
	cancel()
	<-done
}

func TestSetActiveNeverBlocks(t *testing.T) {
	v := New(&pcm{}, 8, func([]float64) {}, nil)
	for range 10 { // nobody reads: must not block, last wins
		v.SetActive(true)
		v.SetActive(false)
	}
	if on := <-v.active; on {
		t.Error("last call did not win")
	}
}

func TestPWCatArgs(t *testing.T) {
	args := PWCat{}.Args()
	want := []string{"--record", "--raw", "--format", "s16", "--rate", "48000", "--channels", "1", "-P", "{ stream.capture.sink=true }", "-"}
	if !slices.Equal(args, want) {
		t.Errorf("args %q", args)
	}
	if Available(func(string) (string, error) { return "", errors.New("missing") }) {
		t.Error("available without pw-cat")
	}
}

// TestRealPWCat captures half a second from the real sink monitor
// (YTM_PIPEWIRE=1; Linux with PipeWire).
func TestRealPWCat(t *testing.T) {
	if os.Getenv("YTM_PIPEWIRE") != "1" {
		t.Skip("set YTM_PIPEWIRE=1 to capture from PipeWire")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	r, err := PWCat{}.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := io.Copy(io.Discard, r)
	_ = r.Close()
	// 0.5 s of mono s16 at 48 kHz = 48 000 bytes, minus startup.
	if n < 20000 || n > 60000 {
		t.Errorf("captured %d bytes in 0.5 s", n)
	}
}

func BenchmarkProcess(b *testing.B) {
	a := NewAnalyzer(96)
	s := sine(440, 0.5, FFTSize)
	b.ReportAllocs()
	for b.Loop() {
		a.Process(s)
	}
}
