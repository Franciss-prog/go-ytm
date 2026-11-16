package mpvipc_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/Franciss-Prog/go-ytm/internal/player/fakempv"
	"github.com/Franciss-Prog/go-ytm/internal/player/mpvipc"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func setup(t *testing.T) (*mpvipc.Conn, *fakempv.Server) {
	t.Helper()
	cli, srv := net.Pipe()
	fake := fakempv.New()
	go fake.Serve(srv)
	c := mpvipc.New(cli, nil)
	t.Cleanup(func() {
		_ = c.Close()
		fake.Crash()
	})
	return c, fake
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestCallReplyAndError(t *testing.T) {
	c, _ := setup(t)
	data, err := c.Call(ctx(t), "get_property", "volume")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "100" {
		t.Fatalf("volume = %s", data)
	}
	_, err = c.Call(ctx(t), "get_property", "nope")
	var me *mpvipc.Error
	if !errors.As(err, &me) || me.Msg != "property not found" || me.Command != "get_property" {
		t.Fatalf("err = %v", err)
	}
}

func TestConcurrentCallsCorrelate(t *testing.T) {
	c, _ := setup(t)
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			want := float64(i % 101)
			// set then get on a property only this goroutine reads back is
			// racy across goroutines, so assert on reply shape instead.
			if _, err := c.Call(ctx(t), "set_property", "volume", want); err != nil {
				t.Error(err)
			}
			if _, err := c.Call(ctx(t), "get_property", "nope"); err == nil {
				t.Error("expected error reply")
			}
		})
	}
	wg.Wait()
}

func TestEventsDeliveredInOrder(t *testing.T) {
	c, fake := setup(t)
	if _, err := c.Call(ctx(t), "observe_property", 1, "pause"); err != nil {
		t.Fatal(err)
	}
	ev := next(t, c)
	if ev.Name != "property-change" || ev.Property != "pause" || string(ev.Data) != "false" || ev.ID != 1 {
		t.Fatalf("first event = %+v", ev)
	}
	if _, err := c.Call(ctx(t), "loadfile", "u1", "replace"); err != nil {
		t.Fatal(err)
	}
	var names []string
	for len(names) < 3 || names[len(names)-1] != "playback-restart" {
		names = append(names, next(t, c).Name)
	}
	if names[0] != "start-file" {
		t.Fatalf("events = %v", names)
	}
	fake.FinishTrack()
	for {
		ev := next(t, c)
		if ev.Name == "end-file" {
			if ev.Reason != "eof" || ev.EntryID != 1 {
				t.Fatalf("end-file = %+v", ev)
			}
			break
		}
	}
}

func TestGarbageLineIgnored(t *testing.T) {
	c, fake := setup(t)
	fake.SendRaw(`{not json`)
	fake.SendRaw(``)
	fake.SendRaw(`{"foo":1}`)
	if _, err := c.Call(ctx(t), "get_property", "pause"); err != nil {
		t.Fatalf("connection should survive garbage: %v", err)
	}
}

func TestOversizedLineResets(t *testing.T) {
	c, fake := setup(t)
	go fake.SendRaw(`{"event":"x","pad":"` + strings.Repeat("a", mpvipc.MaxLine) + `"}`)
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("connection not reset")
	}
	if _, err := c.Call(ctx(t), "get_property", "pause"); !errors.Is(err, mpvipc.ErrClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestPendingCallFailsOnCrash(t *testing.T) {
	c, fake := setup(t)
	fake.SetHook(func(*fakempv.Server, int64, []any) bool { return true }) // never reply
	errc := make(chan error, 1)
	go func() {
		_, err := c.Call(ctx(t), "get_property", "pause")
		errc <- err
	}()
	for fake.CountCommand("get_property") == 0 {
		time.Sleep(time.Millisecond)
	}
	fake.Crash()
	if err := <-errc; !errors.Is(err, mpvipc.ErrClosed) {
		t.Fatalf("err = %v", err)
	}
	if c.Err() == nil {
		t.Fatal("Err() nil after crash")
	}
}

func TestLateReplyDropped(t *testing.T) {
	c, fake := setup(t)
	var held int64
	var mu sync.Mutex
	fake.SetHook(func(_ *fakempv.Server, id int64, cmd []any) bool {
		if cmd[0] == "get_property" && cmd[1] == "pause" {
			mu.Lock()
			held = id
			mu.Unlock()
			return true
		}
		return false
	})
	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Call(short, "get_property", "pause"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	mu.Lock()
	id := held
	mu.Unlock()
	fake.Reply(id, "success", true) // late; must not confuse the next call
	data, err := c.Call(ctx(t), "get_property", "volume")
	if err != nil || string(data) != "100" {
		t.Fatalf("data=%s err=%v", data, err)
	}
}

func TestSendAfterClose(t *testing.T) {
	c, _ := setup(t)
	_ = c.Close()
	if _, err := c.Call(ctx(t), "get_property", "pause"); !errors.Is(err, mpvipc.ErrClosed) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := <-c.Events(); ok {
		t.Fatal("events channel open after Close")
	}
}

func next(t *testing.T, c *mpvipc.Conn) mpvipc.Event {
	t.Helper()
	select {
	case ev, ok := <-c.Events():
		if !ok {
			t.Fatal("events closed")
		}
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for event")
	}
	return mpvipc.Event{}
}

func BenchmarkRoundTrip(b *testing.B) {
	cli, srv := net.Pipe()
	fake := fakempv.New()
	go fake.Serve(srv)
	c := mpvipc.New(cli, nil)
	defer func() { _ = c.Close(); fake.Crash() }()
	bg := context.Background()
	b.ResetTimer()
	for b.Loop() {
		if _, err := c.Call(bg, "get_property", "volume"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEventDecode(b *testing.B) {
	line := []byte(`{"event":"property-change","id":3,"name":"time-pos","data":12.345678}`)
	b.SetBytes(int64(len(line)))
	for b.Loop() {
		var ev mpvipc.Event
		if err := json.Unmarshal(line, &ev); err != nil {
			b.Fatal(err)
		}
	}
}
