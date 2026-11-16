package player

import "sync"

// outbox decouples the controller loop from a slow Events consumer. The
// queue is unbounded but StateChanged events coalesce at the tail, so its
// length is bounded by the number of discrete events (track starts/ends,
// crashes) the consumer has not read yet.
type outbox struct {
	mu     sync.Mutex
	q      []Event
	wake   chan struct{}
	ch     chan Event
	exited chan struct{}
}

func newOutbox() *outbox {
	return &outbox{wake: make(chan struct{}, 1), ch: make(chan Event), exited: make(chan struct{})}
}

func (o *outbox) send(ev Event) {
	o.mu.Lock()
	if _, isState := ev.(StateChanged); isState && len(o.q) > 0 {
		if _, tailState := o.q[len(o.q)-1].(StateChanged); tailState {
			o.q[len(o.q)-1] = ev
			o.mu.Unlock()
			return
		}
	}
	o.q = append(o.q, ev)
	o.mu.Unlock()
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

// run delivers events until stop is closed, then closes ch.
func (o *outbox) run(stop <-chan struct{}) {
	defer close(o.exited)
	defer close(o.ch)
	for {
		o.mu.Lock()
		var ev Event
		if len(o.q) > 0 {
			ev = o.q[0]
			o.q[0] = nil
			o.q = o.q[1:]
		}
		o.mu.Unlock()
		if ev == nil {
			select {
			case <-o.wake:
				continue
			case <-stop:
				return
			}
		}
		select {
		case o.ch <- ev:
		case <-stop:
			return
		}
	}
}
