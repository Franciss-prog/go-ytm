package clock

import (
	"testing"
	"time"
)

func TestFakeAdvanceFiresInOrder(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := NewFake(start)
	a := f.NewTimer(2 * time.Second)
	b := f.NewTimer(time.Second)
	c := f.NewTimer(5 * time.Second)

	f.Advance(2 * time.Second)
	if got := <-b.C(); !got.Equal(start.Add(time.Second)) {
		t.Fatalf("b fired at %v", got)
	}
	if got := <-a.C(); !got.Equal(start.Add(2 * time.Second)) {
		t.Fatalf("a fired at %v", got)
	}
	select {
	case <-c.C():
		t.Fatal("c fired early")
	default:
	}
	if f.Pending() != 1 {
		t.Fatalf("pending = %d, want 1", f.Pending())
	}
	if !c.Stop() {
		t.Fatal("Stop on active timer returned false")
	}
	if c.Stop() {
		t.Fatal("second Stop returned true")
	}
}

func TestFakeZeroDurationFiresImmediately(t *testing.T) {
	f := NewFake(time.Unix(0, 0))
	select {
	case <-f.NewTimer(0).C():
	default:
		t.Fatal("zero timer did not fire")
	}
}

func TestFakeBlockUntil(t *testing.T) {
	f := NewFake(time.Unix(0, 0))
	go func() { f.NewTimer(time.Second) }()
	if !f.BlockUntil(1, nil) {
		t.Fatal("BlockUntil returned false")
	}
	done := make(chan struct{})
	close(done)
	if f.BlockUntil(5, done) {
		t.Fatal("BlockUntil should give up when done is closed")
	}
}

func TestRealTimer(t *testing.T) {
	var c Clock = Real{}
	tm := c.NewTimer(time.Millisecond)
	<-tm.C()
	if tm.Stop() {
		t.Fatal("Stop after fire returned true")
	}
	if c.Now().IsZero() {
		t.Fatal("zero Now")
	}
}
