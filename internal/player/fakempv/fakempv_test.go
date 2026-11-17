package fakempv

import (
	"net"
	"testing"
	"time"
)

// A fake killed before its Serve goroutine runs must still let Serve
// return (Windows CI leaked one: controller shutdown kills an engine
// whose conn was never used).
func TestServeAfterCrashReturns(t *testing.T) {
	s := New()
	s.Crash()
	cli, side := net.Pipe()
	defer cli.Close()
	done := make(chan struct{})
	go func() { defer close(done); s.Serve(side) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve blocked after Crash")
	}
}
