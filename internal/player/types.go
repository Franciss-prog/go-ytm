// Package player drives mpv as a subprocess over its JSON IPC (spec 02).
//
// A Controller owns one mpv process at a time, serializes commands to it,
// turns mpv events into a State snapshot and Events, and restarts mpv after a
// crash. Every public method is non-blocking: commands are queued and
// coalesced, so the UI never waits on mpv.
package player

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors.
var (
	// ErrMPVNotFound means the mpv binary is not on PATH.
	ErrMPVNotFound = errors.New("mpv not found")
	// ErrClosed is returned by commands after Close.
	ErrClosed = errors.New("player closed")
)

// Item is one playable track as the player sees it.
type Item struct {
	// ID identifies the track to the rest of the app (the YouTube video ID).
	ID string
	// URL is what mpv opens (a resolved stream URL or local file).
	URL string
	// Title is used for logs only.
	Title string
}

// IsZero reports whether no item is set.
func (i Item) IsZero() bool { return i.ID == "" && i.URL == "" }

// Status is the player's coarse state (see the state diagram in spec 02).
type Status int

// Statuses.
const (
	Stopped Status = iota
	Loading
	Playing
	Paused
	Buffering
	Error
	Crashed
)

var statusNames = [...]string{"stopped", "loading", "playing", "paused", "buffering", "error", "crashed"}

func (s Status) String() string {
	if int(s) < len(statusNames) {
		return statusNames[s]
	}
	return fmt.Sprintf("Status(%d)", int(s))
}

// State is an immutable snapshot of the player.
type State struct {
	Status     Status
	Item       Item // current track; zero when stopped
	Next       Item // preloaded gapless track; zero when none
	Position   time.Duration
	Duration   time.Duration
	CacheAhead time.Duration // demuxer-cache-time, 0 when unknown
	Volume     int
	Muted      bool
	Paused     bool // mpv's pause flag (also true while Loading paused)
	Err        string
}

// SeekMode selects relative or absolute seeking.
type SeekMode int

// Seek modes.
const (
	SeekRelative SeekMode = iota
	SeekAbsolute
)

// EndReason says why a track ended.
type EndReason int

// End reasons.
const (
	// EndNatural is a normal end of file: the queue advances, history counts.
	EndNatural EndReason = iota
	// EndError means mpv could not play the file (often an expired URL).
	EndError
)

// Event is emitted on Controller.Events.
type Event interface{ playerEvent() }

// TrackStarted fires when mpv starts a file we loaded or preloaded.
type TrackStarted struct{ Item Item }

// TrackEnded fires when a file ends naturally or with an error.
type TrackEnded struct {
	Item   Item
	Reason EndReason
	Err    string
}

// StateChanged carries a new snapshot. Position-only changes are throttled.
type StateChanged struct{ State State }

// EngineCrashed fires when mpv dies unexpectedly. GaveUp is set when restarts
// failed 3 times within 30 s; the player is then Stopped.
type EngineCrashed struct {
	Err    error
	GaveUp bool
}

// Restarted fires after mpv was relaunched following a crash.
type Restarted struct{ Resumed Item }

// EngineError fires when mpv could not be started on demand.
type EngineError struct{ Err error }

func (TrackStarted) playerEvent()  {}
func (TrackEnded) playerEvent()    {}
func (StateChanged) playerEvent()  {}
func (EngineCrashed) playerEvent() {}
func (Restarted) playerEvent()     {}
func (EngineError) playerEvent()   {}

// Player is the interface the rest of the app depends on.
type Player interface {
	// Load replaces the current track and plays it from startAt.
	Load(ctx context.Context, item Item, startAt time.Duration) error
	// Append preloads item as the gapless next track.
	Append(item Item) error
	// ClearNext drops preloaded entries.
	ClearNext() error
	TogglePause() error
	Play() error
	Pause() error
	Seek(d time.Duration, mode SeekMode) error
	SetVolume(v int) error
	ToggleMute() error
	State() State
	Events() <-chan Event
	Close(ctx context.Context) error
}
