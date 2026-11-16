// Package mpvipc speaks mpv's JSON IPC protocol over any byte stream.
//
// Protocol (verified against mpv v0.41.0, see docs/specs/02-player-mpv-ipc.md):
// requests are one JSON object per line, {"command": [...], "request_id": N};
// replies are {"request_id": N, "error": "success"|<msg>, "data": ...}; events
// ({"event": ...}) arrive interleaved with replies.
//
// The package is mpv-agnostic beyond the wire format: it correlates replies
// with requests and demultiplexes events. It never blocks its reader on a slow
// consumer: events are queued without bound and pumped to Events().
package mpvipc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
)

// MaxLine is the longest line accepted from mpv. A longer line resets the
// connection (defensive: mpv never sends anything near this size).
const MaxLine = 1 << 20

// ErrClosed is returned for requests on a closed connection.
var ErrClosed = errors.New("mpv connection closed")

// Error is a non-success reply from mpv, e.g. "property not found".
type Error struct {
	Command string
	Msg     string
}

func (e *Error) Error() string { return fmt.Sprintf("mpv %s: %s", e.Command, e.Msg) }

// Reply is the outcome of one request.
type Reply struct {
	ID   int64
	Data json.RawMessage
	Err  error
}

// Event is an asynchronous message from mpv. Common fields are decoded; Raw
// holds the full line.
type Event struct {
	Name string `json:"event"`
	// property-change
	ID       int64           `json:"id"`
	Property string          `json:"name"`
	Data     json.RawMessage `json:"data"`
	// start-file / end-file
	EntryID   int64  `json:"playlist_entry_id"`
	Reason    string `json:"reason"`
	FileError string `json:"file_error"`

	Raw json.RawMessage `json:"-"`
}

// Conn is a client connection. Create with New; always call Close.
type Conn struct {
	rwc    io.ReadWriteCloser
	log    *slog.Logger
	nextID atomic.Int64

	writes chan []byte
	events chan Event

	mu      sync.Mutex
	pending map[int64]pendingReq
	queue   []Event
	wake    chan struct{} // cap 1: queue non-empty
	err     error

	done      chan struct{} // closed when the connection is dead
	closeOnce sync.Once
	closed    chan struct{} // closed by Close (stops the pump)
	wg        sync.WaitGroup
}

type pendingReq struct {
	cmd  string
	done chan<- Reply
}

// New starts the reader, writer and event pump goroutines on rwc.
func New(rwc io.ReadWriteCloser, log *slog.Logger) *Conn {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	c := &Conn{
		rwc:     rwc,
		log:     log,
		writes:  make(chan []byte, 64),
		events:  make(chan Event),
		pending: map[int64]pendingReq{},
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
		closed:  make(chan struct{}),
	}
	c.wg.Add(3)
	go c.readLoop()
	go c.writeLoop()
	go c.pump()
	return c
}

// Events delivers mpv events in arrival order. It is closed after Close.
func (c *Conn) Events() <-chan Event { return c.events }

// Done is closed when the connection dies (EOF, I/O error, protocol
// violation, or Close).
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err returns why the connection died, or nil while alive.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Named is a command with named arguments, sent as a JSON object
// ({"name": "loadfile", "url": ..., "options": ...}). Named arguments do not
// depend on positional order, which mpv has changed between versions.
type Named map[string]any

// Send queues a request. args is either positional (["seek", 5, "relative"])
// or a single Named. The reply is delivered on done, which must have room for
// it (the reader never blocks; a reply that does not fit is dropped and
// logged). It returns the request ID so the caller can Forget it.
func (c *Conn) Send(ctx context.Context, done chan<- Reply, args ...any) (int64, error) {
	id := c.nextID.Add(1)
	var command any = args
	cmd := ""
	if len(args) > 0 {
		cmd = fmt.Sprint(args[0])
	}
	if len(args) == 1 {
		if n, ok := args[0].(Named); ok {
			command = n
			cmd = fmt.Sprint(n["name"])
		}
	}
	line, err := json.Marshal(struct {
		Command   any   `json:"command"`
		RequestID int64 `json:"request_id"`
	}{command, id})
	if err != nil {
		return 0, fmt.Errorf("encode mpv command: %w", err)
	}
	line = append(line, '\n')

	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return 0, ErrClosed
	}
	c.pending[id] = pendingReq{cmd: cmd, done: done}
	c.mu.Unlock()

	select {
	case c.writes <- line:
		return id, nil
	case <-c.done:
	case <-ctx.Done():
	}
	c.Forget(id)
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	return 0, ErrClosed
}

// Forget drops interest in a request; a late reply is discarded.
func (c *Conn) Forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// Call sends a request and waits for its reply.
func (c *Conn) Call(ctx context.Context, args ...any) (json.RawMessage, error) {
	ch := make(chan Reply, 1)
	id, err := c.Send(ctx, ch, args...)
	if err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		return r.Data, r.Err
	case <-ctx.Done():
		c.Forget(id)
		return nil, ctx.Err()
	}
}

// Close tears the connection down and waits for its goroutines.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		c.fail(ErrClosed)
		close(c.closed)
	})
	c.wg.Wait()
	return nil
}

// fail marks the connection dead, closes the stream and fails pending calls.
func (c *Conn) fail(err error) {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return
	}
	c.err = err
	pend := c.pending
	c.pending = map[int64]pendingReq{}
	close(c.done)
	c.mu.Unlock()
	_ = c.rwc.Close()
	for id, p := range pend {
		select {
		case p.done <- Reply{ID: id, Err: ErrClosed}:
		default:
		}
	}
}

func (c *Conn) readLoop() {
	defer c.wg.Done()
	sc := bufio.NewScanner(c.rwc)
	sc.Buffer(make([]byte, 0, 64*1024), MaxLine)
	for sc.Scan() {
		c.handleLine(sc.Bytes())
	}
	err := sc.Err()
	switch {
	case err == nil:
		err = io.EOF
	case errors.Is(err, bufio.ErrTooLong):
		c.log.Warn("mpv sent an oversized line; resetting connection", "limit", MaxLine)
	}
	c.fail(fmt.Errorf("mpv ipc read: %w", err))
}

// wireMsg is the union of reply and event shapes.
type wireMsg struct {
	Event     string          `json:"event"`
	RequestID *int64          `json:"request_id"`
	Error     string          `json:"error"`
	Data      json.RawMessage `json:"data"`
}

func (c *Conn) handleLine(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	var m wireMsg
	if err := json.Unmarshal(line, &m); err != nil {
		c.log.Warn("mpv sent malformed JSON; ignoring line", "err", err, "len", len(line))
		return
	}
	if m.Event != "" {
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			c.log.Warn("mpv event decode", "err", err)
			return
		}
		ev.Raw = append(json.RawMessage(nil), line...)
		c.mu.Lock()
		c.queue = append(c.queue, ev)
		c.mu.Unlock()
		select {
		case c.wake <- struct{}{}:
		default:
		}
		return
	}
	if m.RequestID == nil {
		c.log.Debug("mpv message without event or request_id", "line", string(line))
		return
	}
	id := *m.RequestID
	c.mu.Lock()
	p, ok := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if !ok {
		c.log.Debug("dropping reply for unknown request", "request_id", id)
		return
	}
	r := Reply{ID: id, Data: append(json.RawMessage(nil), m.Data...)}
	if m.Error != "success" {
		r.Err = &Error{Command: p.cmd, Msg: m.Error}
	}
	select {
	case p.done <- r:
	default:
		c.log.Warn("reply channel full; dropping reply", "request_id", id, "command", p.cmd)
	}
}

func (c *Conn) writeLoop() {
	defer c.wg.Done()
	for {
		select {
		case line := <-c.writes:
			if _, err := c.rwc.Write(line); err != nil {
				c.fail(fmt.Errorf("mpv ipc write: %w", err))
				return
			}
		case <-c.done:
			return
		}
	}
}

// pump moves queued events to the unbuffered Events channel.
func (c *Conn) pump() {
	defer c.wg.Done()
	defer close(c.events)
	for {
		c.mu.Lock()
		var ev Event
		have := len(c.queue) > 0
		if have {
			ev = c.queue[0]
			c.queue[0] = Event{}
			c.queue = c.queue[1:]
		}
		c.mu.Unlock()
		if !have {
			select {
			case <-c.wake:
				continue
			case <-c.closed:
				return
			}
		}
		select {
		case c.events <- ev:
		case <-c.closed:
			return
		}
	}
}
