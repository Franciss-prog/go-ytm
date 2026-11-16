// Package fakempv is a scripted stand-in for mpv's JSON IPC server, used by
// tests so CI never needs a real mpv. It models the subset of mpv behavior the
// player relies on (verified shapes from mpv v0.41.0, see spec 02):
// observe_property emits the current value immediately; loadfile replies with
// playlist_entry_id and then emits start-file, file-loaded and
// playback-restart; end-file carries reason and playlist_entry_id.
package fakempv

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultDuration is the duration reported for every loaded file unless
// overridden with SetDuration.
const DefaultDuration = 180.0

// Hook can intercept a command before the fake handles it. Returning
// handled=true suppresses the default handling (and the reply, unless the
// hook sends one with Reply).
type Hook func(s *Server, id int64, cmd []any) (handled bool)

type entry struct {
	id  int64
	url string
}

// Server is one fake mpv instance. Use New, then Serve a connection.
type Server struct {
	mu        sync.Mutex
	w         io.Writer
	closer    io.Closer
	props     map[string]any
	observed  map[string][]int64
	playlist  []entry
	current   int // index into playlist, -1 when idle
	nextEntry int64
	durations map[string]float64
	failURLs  map[string]bool
	log       []string
	hook      Hook
	exited    chan struct{}
	exitOnce  sync.Once
}

// New returns a fake in the idle state with mpv's defaults.
func New() *Server {
	return &Server{
		props: map[string]any{
			"pause": false, "idle-active": true, "volume": 100.0, "mute": false,
			"paused-for-cache": false, "time-pos": nil, "duration": nil,
			"playlist-pos": -1.0, "playlist-current-pos": -1.0, "demuxer-cache-time": nil,
		},
		observed:  map[string][]int64{},
		current:   -1,
		durations: map[string]float64{},
		failURLs:  map[string]bool{},
		exited:    make(chan struct{}),
	}
}

// SetHook installs a command interceptor.
func (s *Server) SetHook(h Hook) {
	s.mu.Lock()
	s.hook = h
	s.mu.Unlock()
}

// SetDuration sets the duration reported for url.
func (s *Server) SetDuration(url string, seconds float64) {
	s.mu.Lock()
	s.durations[url] = seconds
	s.mu.Unlock()
}

// FailURL makes loading url end with end-file reason=error.
func (s *Server) FailURL(url string) {
	s.mu.Lock()
	s.failURLs[url] = true
	s.mu.Unlock()
}

// Exited is closed when the fake "process" ends (quit or Crash).
func (s *Server) Exited() <-chan struct{} { return s.exited }

// Commands returns the names of every command received, in order.
func (s *Server) Commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.log...)
}

// CountCommand returns how many times cmd was received.
func (s *Server) CountCommand(cmd string) int {
	n := 0
	for _, c := range s.Commands() {
		if c == cmd {
			n++
		}
	}
	return n
}

// Prop returns a property's current value.
func (s *Server) Prop(name string) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.props[name]
}

// PlaylistURLs returns the URLs in the fake playlist.
func (s *Server) PlaylistURLs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.playlist))
	for i, e := range s.playlist {
		out[i] = e.url
	}
	return out
}

// Serve handles one client connection until it closes or the fake exits.
func (s *Server) Serve(rwc io.ReadWriteCloser) {
	s.mu.Lock()
	s.w, s.closer = rwc, rwc
	s.mu.Unlock()
	sc := bufio.NewScanner(rwc)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		var req struct {
			Command   json.RawMessage `json:"command"`
			RequestID *int64          `json:"request_id"`
		}
		var cmd []any
		if err := json.Unmarshal(sc.Bytes(), &req); err == nil {
			cmd = decodeCommand(req.Command)
		}
		if len(cmd) == 0 {
			s.reply(req.RequestID, "invalid parameter", nil)
			continue
		}
		s.handle(req.RequestID, cmd)
	}
	s.exit()
}

// Crash simulates mpv dying: the connection drops and the process exits.
func (s *Server) Crash() { s.exit() }

func (s *Server) exit() {
	s.exitOnce.Do(func() {
		s.mu.Lock()
		c := s.closer
		s.mu.Unlock()
		if c != nil {
			_ = c.Close()
		}
		close(s.exited)
	})
}

// SendRaw writes an arbitrary line (for garbage/oversize tests).
func (s *Server) SendRaw(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeLocked([]byte(line + "\n"))
}

// Reply sends a reply for request id (for hooks that delay replies).
func (s *Server) Reply(id int64, errMsg string, data any) {
	s.reply(&id, errMsg, data)
}

// Emit sends an event object.
func (s *Server) Emit(ev map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emitLocked(ev)
}

// SetProp changes a property and notifies observers, as if mpv changed it.
func (s *Server) SetProp(name string, v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setPropLocked(name, v)
}

// FinishTrack ends the current file naturally (reason=eof) and advances to
// the next playlist entry, or goes idle.
func (s *Server) FinishTrack() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current < 0 {
		return
	}
	cur := s.playlist[s.current]
	s.emitLocked(map[string]any{"event": "end-file", "reason": "eof", "playlist_entry_id": cur.id})
	if s.current+1 < len(s.playlist) {
		s.startLocked(s.current+1, 0)
		return
	}
	s.current = -1
	s.goIdleLocked()
}

func (s *Server) handle(rid *int64, cmd []any) {
	name := fmt.Sprint(cmd[0])
	s.mu.Lock()
	s.log = append(s.log, name)
	hook := s.hook
	s.mu.Unlock()
	if hook != nil && rid != nil && hook(s, *rid, cmd) {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	ok := func(data any) { s.replyLocked(rid, "success", data) }
	fail := func(msg string) { s.replyLocked(rid, msg, nil) }
	arg := func(i int) string {
		if i < len(cmd) {
			return fmt.Sprint(cmd[i])
		}
		return ""
	}

	switch name {
	case "observe_property":
		id, _ := toInt(cmd[1])
		prop := arg(2)
		s.observed[prop] = append(s.observed[prop], id)
		ok(nil)
		s.notifyOne(prop, id)
	case "get_property":
		v, found := s.props[arg(1)]
		if !found {
			fail("property not found")
			return
		}
		ok(v)
	case "set_property":
		if _, found := s.props[arg(1)]; !found {
			fail("property not found")
			return
		}
		v := cmd[2]
		if f, isNum := toFloat(v); isNum {
			v = f
		}
		ok(nil)
		s.setPropLocked(arg(1), v)
	case "cycle":
		b, _ := s.props[arg(1)].(bool)
		ok(nil)
		s.setPropLocked(arg(1), !b)
	case "seek":
		if s.current < 0 {
			fail("error running command")
			return
		}
		secs, _ := toFloat(cmd[1])
		pos, _ := s.props["time-pos"].(float64)
		if strings.HasPrefix(arg(2), "absolute") {
			pos = secs
		} else {
			pos += secs
		}
		ok(nil)
		s.emitLocked(map[string]any{"event": "seek"})
		s.setPropLocked("time-pos", pos)
		s.emitLocked(map[string]any{"event": "playback-restart"})
	case "loadfile":
		s.loadLocked(rid, cmd)
	case "playlist-clear":
		if s.current >= 0 {
			s.playlist = []entry{s.playlist[s.current]}
			s.current = 0
		} else {
			s.playlist = nil
		}
		ok(nil)
		s.setPropLocked("playlist-pos", float64(s.current))
	case "playlist-remove":
		i, err := strconv.Atoi(arg(1))
		if err != nil || i < 0 || i >= len(s.playlist) || i == s.current {
			fail("invalid parameter")
			return
		}
		s.playlist = append(s.playlist[:i], s.playlist[i+1:]...)
		if i < s.current {
			s.current--
		}
		ok(nil)
	case "stop":
		ok(nil)
		if s.current >= 0 {
			s.emitLocked(map[string]any{"event": "end-file", "reason": "stop", "playlist_entry_id": s.playlist[s.current].id})
		}
		s.playlist, s.current = nil, -1
		s.goIdleLocked()
	case "quit":
		ok(nil)
		go s.exit()
	default:
		fail("invalid parameter")
	}
}

// loadLocked accepts both positional ["loadfile", url, flags, index, options]
// and the options string "k=v,k=v".
func (s *Server) loadLocked(rid *int64, cmd []any) {
	if len(cmd) < 2 {
		s.replyLocked(rid, "invalid parameter", nil)
		return
	}
	url := fmt.Sprint(cmd[1])
	flags := "replace"
	if len(cmd) > 2 {
		flags = fmt.Sprint(cmd[2])
	}
	start := 0.0
	if len(cmd) > 4 {
		for _, kv := range strings.Split(fmt.Sprint(cmd[4]), ",") {
			if v, ok := strings.CutPrefix(kv, "start="); ok {
				start, _ = strconv.ParseFloat(v, 64)
			}
		}
	}
	s.nextEntry++
	e := entry{id: s.nextEntry, url: url}
	s.replyLocked(rid, "success", map[string]any{"playlist_entry_id": e.id})
	switch flags {
	case "replace":
		if s.current >= 0 {
			s.emitLocked(map[string]any{"event": "end-file", "reason": "stop", "playlist_entry_id": s.playlist[s.current].id})
		}
		s.playlist = []entry{e}
		s.startLocked(0, start)
	case "append-play":
		s.playlist = append(s.playlist, e)
		if s.current < 0 {
			s.startLocked(len(s.playlist)-1, start)
		}
	default: // append
		s.playlist = append(s.playlist, e)
	}
}

func (s *Server) startLocked(i int, start float64) {
	s.current = i
	e := s.playlist[i]
	s.emitLocked(map[string]any{"event": "start-file", "playlist_entry_id": e.id})
	s.setPropLocked("playlist-pos", float64(i))
	if s.failURLs[e.url] {
		s.emitLocked(map[string]any{"event": "end-file", "reason": "error", "playlist_entry_id": e.id, "file_error": "loading failed"})
		s.playlist = append(s.playlist[:i], s.playlist[i+1:]...)
		s.current = -1
		s.goIdleLocked()
		return
	}
	d, ok := s.durations[e.url]
	if !ok {
		d = DefaultDuration
	}
	s.setPropLocked("idle-active", false)
	s.emitLocked(map[string]any{"event": "file-loaded"})
	s.setPropLocked("duration", d)
	s.setPropLocked("time-pos", start)
	s.emitLocked(map[string]any{"event": "playback-restart"})
}

func (s *Server) goIdleLocked() {
	s.setPropLocked("time-pos", nil)
	s.setPropLocked("duration", nil)
	s.setPropLocked("playlist-pos", -1.0)
	s.setPropLocked("idle-active", true)
	s.emitLocked(map[string]any{"event": "idle"})
}

func (s *Server) setPropLocked(name string, v any) {
	if old, ok := s.props[name]; ok && old == v {
		return
	}
	s.props[name] = v
	for _, id := range s.observed[name] {
		s.notifyOne(name, id)
	}
}

func (s *Server) notifyOne(name string, id int64) {
	ev := map[string]any{"event": "property-change", "id": id, "name": name}
	if v := s.props[name]; v != nil {
		ev["data"] = v
	}
	s.emitLocked(ev)
}

func (s *Server) emitLocked(ev map[string]any) {
	b, _ := json.Marshal(ev)
	s.writeLocked(append(b, '\n'))
}

func (s *Server) reply(rid *int64, errMsg string, data any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replyLocked(rid, errMsg, data)
}

func (s *Server) replyLocked(rid *int64, errMsg string, data any) {
	if rid == nil {
		return
	}
	m := map[string]any{"request_id": *rid, "error": errMsg}
	if data != nil {
		m["data"] = data
	}
	b, _ := json.Marshal(m)
	s.writeLocked(append(b, '\n'))
}

func (s *Server) writeLocked(b []byte) {
	if s.w == nil {
		return
	}
	// Writes to a dead connection fail silently, like a crashed mpv.
	_, _ = s.w.Write(b)
}

// decodeCommand accepts positional arrays and named-argument objects. Named
// loadfile is mapped onto mpv's positional order (url, flags, index, options).
func decodeCommand(raw json.RawMessage) []any {
	var pos []any
	if json.Unmarshal(raw, &pos) == nil {
		return pos
	}
	var named map[string]any
	if json.Unmarshal(raw, &named) != nil || named["name"] == nil {
		return nil
	}
	out := []any{named["name"]}
	if named["name"] == "loadfile" {
		flags := named["flags"]
		if flags == nil {
			flags = "replace"
		}
		opts := named["options"]
		if opts == nil {
			opts = ""
		}
		out = append(out, named["url"], flags, -1.0, opts)
	}
	return out
}

func toInt(v any) (int64, bool) {
	f, ok := toFloat(v)
	return int64(f), ok
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}

// Wait blocks until the fake exits or timeout elapses; it reports whether it
// exited.
func (s *Server) Wait(timeout time.Duration) bool {
	select {
	case <-s.exited:
		return true
	case <-time.After(timeout):
		return false
	}
}
