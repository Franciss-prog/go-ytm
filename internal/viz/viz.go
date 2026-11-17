package viz

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"runtime"
)

// Hop is how many new samples each update reads: 48000/1600 = 30
// updates per second.
const Hop = SampleRate / 30

// Source opens the captured audio: mono signed 16-bit little-endian PCM
// at SampleRate. Closing the reader stops the capture.
type Source interface {
	Open(ctx context.Context) (io.ReadCloser, error)
}

// PWCat captures the default sink's monitor with PipeWire's pw-cat
// (`stream.capture.sink=true` records what the sink plays; verified with
// pw-cat 1.6.11: 96 kB/s mono s16 at 48 kHz).
type PWCat struct {
	Binary string // "" = "pw-cat"
}

// Args are pw-cat's arguments (exported for tests and `ytm doctor`).
func (PWCat) Args() []string {
	return []string{
		"--record", "--raw", "--format", "s16", "--rate", fmt.Sprint(SampleRate), "--channels", "1",
		"-P", "{ stream.capture.sink=true }", "-",
	}
}

// Open implements Source.
func (p PWCat) Open(ctx context.Context) (io.ReadCloser, error) {
	bin := p.Binary
	if bin == "" {
		bin = "pw-cat"
	}
	cmd := exec.CommandContext(ctx, bin, p.Args()...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("viz: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("viz: start pw-cat: %w", err)
	}
	return &proc{ReadCloser: out, cmd: cmd}, nil
}

type proc struct {
	io.ReadCloser
	cmd *exec.Cmd
}

// Close stops pw-cat and reaps it.
func (p *proc) Close() error {
	_ = p.cmd.Process.Kill()
	_ = p.ReadCloser.Close()
	_ = p.cmd.Wait() // killed: the exit error is expected
	return nil
}

// Available reports whether the visualizer can run here: Linux with pw-cat
// on PATH.
func Available(lookPath func(string) (string, error)) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	_, err := lookPath("pw-cat")
	return err == nil
}

// Visualizer owns the capture: Run is its only goroutine (started by the
// app, ends with ctx); SetActive starts and stops capturing. Levels go to
// send, at most 30 times per second, a fresh slice each time; a nil
// slice means "stopped".
type Visualizer struct {
	src    Source
	bands  int
	send   func([]float64)
	log    *slog.Logger
	active chan bool
}

// New returns a visualizer for n bands.
func New(src Source, bands int, send func([]float64), log *slog.Logger) *Visualizer {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Visualizer{src: src, bands: bands, send: send, log: log, active: make(chan bool, 1)}
}

// SetActive asks Run to capture (true) or stop. It never blocks; the last
// call wins.
func (v *Visualizer) SetActive(on bool) {
	for {
		select {
		case v.active <- on:
			return
		default:
			select {
			case <-v.active: // replace the pending request
			default:
			}
		}
	}
}

// Run serves SetActive until ctx ends.
func (v *Visualizer) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case on := <-v.active:
			if on {
				v.session(ctx)
			}
		}
	}
}

// session captures until SetActive(false), ctx ends or the capture fails.
func (v *Visualizer) session(ctx context.Context) {
	cctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { // owned: session waits for done on every path
		defer close(done)
		if err := v.capture(cctx); err != nil && cctx.Err() == nil {
			v.log.Warn("viz: capture stopped", "err", err)
		}
	}()
	defer func() {
		cancel()
		<-done
		v.send(nil)
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case on := <-v.active:
			if !on {
				return
			}
		}
	}
}

func (v *Visualizer) capture(ctx context.Context) error {
	r, err := v.src.Open(ctx)
	if err != nil {
		return err
	}
	defer r.Close() //nolint:errcheck // Close reaps the process
	a := NewAnalyzer(v.bands)
	window := make([]float64, FFTSize)
	raw := make([]byte, Hop*2)
	for {
		if _, err := io.ReadFull(r, raw); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return fmt.Errorf("viz: capture ended: %w", err)
			}
			return fmt.Errorf("viz: read: %w", err)
		}
		copy(window, window[Hop:])
		for i := range Hop {
			window[FFTSize-Hop+i] = float64(int16(binary.LittleEndian.Uint16(raw[2*i:]))) / 32768
		}
		levels := a.Process(window)
		v.send(append([]float64(nil), levels...))
		if ctx.Err() != nil {
			return nil
		}
	}
}
