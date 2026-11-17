// Package queue is the playback queue model: order, shuffle, repeat, play
// random, play next, reordering and autoplay marks. It is pure (no I/O, no
// locks): the UI goroutine owns it.
//
// Semantics are ported from ytm-player (MIT,
// https://github.com/peternaame-boop/ytm-player, src/ytm_player/services/queue.py
// at commit d1681465e03d0d9397253b08e0e5c6537c0eccfc), including its
// "removed current" boundary: removing the playing entry leaves no current
// entry, and Next selects the removed entry's successor.
package queue

import (
	"math/rand/v2"
	"slices"

	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// Repeat is the repeat mode.
type Repeat int

// Repeat modes, in cycle order.
const (
	RepeatOff Repeat = iota
	RepeatAll
	RepeatOne
)

// String returns "off", "all" or "one" (IPC status, config).
func (r Repeat) String() string {
	switch r {
	case RepeatAll:
		return "all"
	case RepeatOne:
		return "one"
	}
	return "off"
}

// ParseRepeat parses a String value; anything else is RepeatOff.
func ParseRepeat(s string) Repeat {
	switch s {
	case "all":
		return RepeatAll
	case "one":
		return RepeatOne
	}
	return RepeatOff
}

// EntryID names one occurrence of a track in the queue. It stays with the
// entry through moves and shuffle and dies when the entry is removed; two
// occurrences of the same track never share an ID. Zero is never used.
type EntryID uint64

// Entry is one queue occurrence.
type Entry struct {
	ID       EntryID
	Track    ytmusic.Track
	Autoplay bool // appended by autoplay (↻)
}

// Queue is an ordered playback queue with shuffle and repeat. The zero
// value is not usable; call New.
type Queue struct {
	rng *rand.Rand

	tracks []ytmusic.Track // insertion order ("real" order)
	ids    []EntryID       // parallel to tracks
	nextID EntryID

	gen uint64 // lifetime: bumped by Clear
	rev uint64 // bumped by every change (render caches)

	// A removed playing entry finishes outside the queue; removedNext is
	// its successor (0 = none, the end).
	removedCurrent bool
	removedNext    EntryID

	cur     int // index into tracks when not shuffled
	repeat  Repeat
	shuffle bool
	order   []int // shuffle: playback position → index into tracks
	pos     int   // shuffle position

	context string // collection the queue came from ("" = ephemeral)

	autoplay  map[EntryID]bool
	dismissed map[string]bool // video IDs removed from autoplay batches
}

// New returns an empty queue. rng drives shuffle and play random; nil uses
// a randomly seeded source.
func New(rng *rand.Rand) *Queue {
	if rng == nil {
		rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}
	return &Queue{rng: rng, cur: -1, pos: -1, autoplay: map[EntryID]bool{}, dismissed: map[string]bool{}}
}

// --- read-only views --------------------------------------------------------

// Len is the number of entries.
func (q *Queue) Len() int { return len(q.tracks) }

// Generation changes only on Clear: results computed for an older queue
// (refills, deferred advances) can be dropped.
func (q *Queue) Generation() uint64 { return q.gen }

// Rev changes on every mutation, including navigation.
func (q *Queue) Rev() uint64 { return q.rev }

// Repeat returns the repeat mode.
func (q *Queue) Repeat() Repeat { return q.repeat }

// Shuffle reports whether shuffle is on.
func (q *Queue) Shuffle() bool { return q.shuffle }

// Context is the ID of the collection the queue was filled from.
func (q *Queue) Context() string { return q.context }

// SetContext records the collection the queue was filled from.
func (q *Queue) SetContext(id string) { q.context = id; q.rev++ }

// Entries returns the entries in playback order (shuffle-aware).
func (q *Queue) Entries() []Entry {
	out := make([]Entry, 0, len(q.tracks))
	for _, i := range q.visible() {
		out = append(out, Entry{ID: q.ids[i], Track: q.tracks[i], Autoplay: q.autoplay[q.ids[i]]})
	}
	return out
}

// Tracks returns the tracks in playback order.
func (q *Queue) Tracks() []ytmusic.Track {
	out := make([]ytmusic.Track, 0, len(q.tracks))
	for _, i := range q.visible() {
		out = append(out, q.tracks[i])
	}
	return out
}

// CurrentIndex is the current entry's position in playback order, or -1.
func (q *Queue) CurrentIndex() int {
	if q.removedCurrent {
		return -1
	}
	if q.shuffle {
		return q.pos
	}
	return q.cur
}

// Current returns the current track.
func (q *Queue) Current() (ytmusic.Track, bool) {
	if q.removedCurrent {
		return ytmusic.Track{}, false
	}
	return q.at(q.realIndex())
}

// CurrentEntry returns the current entry.
func (q *Queue) CurrentEntry() (Entry, bool) {
	if q.removedCurrent {
		return Entry{}, false
	}
	r := q.realIndex()
	if r < 0 || r >= len(q.tracks) {
		return Entry{}, false
	}
	return Entry{ID: q.ids[r], Track: q.tracks[r], Autoplay: q.autoplay[q.ids[r]]}, true
}

// Remaining is the number of entries after the current position.
func (q *Queue) Remaining() int {
	if q.removedCurrent {
		return len(q.tracks) - q.removedNextPosition()
	}
	if q.shuffle {
		return len(q.order) - q.pos - 1
	}
	return len(q.tracks) - q.cur - 1
}

// PlayedTracks returns the tracks up to and including the current one in
// insertion order (autoplay seeds).
func (q *Queue) PlayedTracks() []ytmusic.Track {
	r := q.realIndex()
	if q.removedCurrent || r < 0 {
		return nil
	}
	return slices.Clone(q.tracks[:r+1])
}

// --- helpers ----------------------------------------------------------------

func (q *Queue) at(real int) (ytmusic.Track, bool) {
	if real < 0 || real >= len(q.tracks) {
		return ytmusic.Track{}, false
	}
	return q.tracks[real], true
}

func (q *Queue) visible() []int {
	if q.shuffle {
		return q.order
	}
	out := make([]int, len(q.tracks))
	for i := range out {
		out[i] = i
	}
	return out
}

func (q *Queue) realIndex() int {
	if q.shuffle && q.pos >= 0 && q.pos < len(q.order) {
		return q.order[q.pos]
	}
	return q.cur
}

func (q *Queue) removedNextPosition() int {
	if q.removedNext != 0 {
		if real := slices.Index(q.ids, q.removedNext); real >= 0 {
			if q.shuffle {
				return slices.Index(q.order, real)
			}
			return real
		}
	}
	return len(q.tracks)
}

func (q *Queue) newIDs(n int) []EntryID {
	out := make([]EntryID, n)
	for i := range out {
		q.nextID++
		out[i] = q.nextID
	}
	return out
}

// randInt returns a uniform integer in [a, b] (Python random.randint).
func (q *Queue) randInt(a, b int) int { return a + q.rng.IntN(b-a+1) }

func (q *Queue) shuffleInts(s []int) {
	q.rng.Shuffle(len(s), func(i, j int) { s[i], s[j] = s[j], s[i] })
}

func (q *Queue) rebuildShuffle(keepCurrent bool) {
	idx := make([]int, len(q.tracks))
	for i := range idx {
		idx[i] = i
	}
	real := q.realIndex()
	if keepCurrent && !q.removedCurrent && real >= 0 && real < len(q.tracks) {
		idx = slices.Delete(idx, real, real+1)
		q.shuffleInts(idx)
		q.order = append([]int{real}, idx...)
		q.pos = 0
		return
	}
	q.shuffleInts(idx)
	q.order = idx
	q.pos = -1
}

// clearRemoved leaves the removed-current state (a new entry is selected).
func (q *Queue) clearRemoved() { q.removedCurrent, q.removedNext = false, 0 }

// --- mutation ---------------------------------------------------------------

// Add appends a track (under shuffle, at a random future position).
func (q *Queue) Add(t ytmusic.Track) EntryID { return q.Insert(len(q.tracks), t) }

// Insert adds a track at position pos in insertion order (pos ≥ Len
// appends). Under shuffle it lands at a random future playback position.
func (q *Queue) Insert(pos int, t ytmusic.Track) EntryID {
	q.rev++
	id := q.newIDs(1)[0]
	if q.removedCurrent && q.removedNext == 0 {
		q.removedNext = id
	}
	var newIdx int
	if pos >= len(q.tracks) {
		q.tracks = append(q.tracks, t)
		q.ids = append(q.ids, id)
		newIdx = len(q.tracks) - 1
	} else {
		pos = max(0, pos)
		q.tracks = slices.Insert(q.tracks, pos, t)
		q.ids = slices.Insert(q.ids, pos, id)
		newIdx = pos
		if !q.shuffle && pos <= q.cur {
			q.cur++
		}
	}
	if q.shuffle {
		insertAt := 0
		if q.pos >= 0 {
			insertAt = q.pos + 1
		}
		for i, v := range q.order {
			if v >= newIdx {
				q.order[i] = v + 1
			}
		}
		future := len(q.order)
		if insertAt < len(q.order) {
			future = q.randInt(insertAt+1, len(q.order))
		}
		q.order = slices.Insert(q.order, future, newIdx)
	}
	return id
}

// AddMany appends tracks (under shuffle, each at a random future position;
// on an empty queue the whole order is rebuilt).
func (q *Queue) AddMany(ts []ytmusic.Track) []EntryID {
	if len(ts) == 0 {
		return nil
	}
	q.rev++
	wasEmpty := len(q.tracks) == 0
	start := len(q.tracks)
	ids := q.newIDs(len(ts))
	q.tracks = append(q.tracks, ts...)
	q.ids = append(q.ids, ids...)
	if q.removedCurrent && q.removedNext == 0 {
		q.removedNext = ids[0]
	}
	if q.shuffle {
		if wasEmpty {
			q.rebuildShuffle(false)
		} else {
			after := 0
			if q.pos >= 0 {
				after = q.pos + 1
			}
			newIdx := make([]int, len(ts))
			for i := range newIdx {
				newIdx[i] = start + i
			}
			q.shuffleInts(newIdx)
			for _, ni := range newIdx {
				p := q.randInt(after, len(q.order))
				q.order = slices.Insert(q.order, p, ni)
			}
		}
	}
	return ids
}

// AddNext inserts tracks right after the current one, in the given order
// ("play next"). Duplicates are inserted as is.
func (q *Queue) AddNext(ts ...ytmusic.Track) []EntryID {
	if len(ts) == 0 {
		return nil
	}
	q.rev++
	if q.removedCurrent {
		p := q.removedNextPosition()
		ids := q.newIDs(len(ts))
		if q.shuffle {
			start := len(q.tracks)
			q.tracks = append(q.tracks, ts...)
			q.ids = append(q.ids, ids...)
			q.order = slices.Insert(q.order, p, seq(start, len(ts))...)
		} else {
			q.tracks = slices.Insert(q.tracks, p, ts...)
			q.ids = slices.Insert(q.ids, p, ids...)
		}
		q.removedNext = ids[0]
		return ids
	}
	ids := q.newIDs(len(ts))
	if q.shuffle {
		start := len(q.tracks)
		q.tracks = append(q.tracks, ts...)
		q.ids = append(q.ids, ids...)
		q.order = slices.Insert(q.order, q.pos+1, seq(start, len(ts))...)
		return ids
	}
	p := 0
	if q.cur >= 0 {
		p = q.cur + 1
	}
	q.tracks = slices.Insert(q.tracks, p, ts...)
	q.ids = slices.Insert(q.ids, p, ids...)
	return ids
}

func seq(start, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = start + i
	}
	return out
}

// Remove removes the entry at playback position i. Removing an autoplay
// entry dismisses its video ID from later autoplay refills.
func (q *Queue) Remove(i int) {
	if i < 0 || i >= len(q.tracks) {
		return
	}
	q.rev++
	order := q.visible()
	real := order[i]
	id := q.ids[real]
	if q.autoplay[id] {
		delete(q.autoplay, id)
		if v := q.tracks[real].VideoID; v != "" {
			q.dismissed[v] = true
		}
	}
	curPos := q.cur
	if q.shuffle {
		curPos = q.pos
	}
	if (!q.removedCurrent && i == curPos) || (q.removedCurrent && id == q.removedNext) {
		q.removedCurrent = true
		q.removedNext = 0
		if i+1 < len(order) {
			q.removedNext = q.ids[order[i+1]]
		}
	}
	if q.shuffle {
		q.order = slices.Delete(q.order, i, i+1)
		for k, v := range q.order {
			if v > real {
				q.order[k] = v - 1
			}
		}
		q.tracks = slices.Delete(q.tracks, real, real+1)
		q.ids = slices.Delete(q.ids, real, real+1)
		if i < q.pos {
			q.pos--
		} else if i == q.pos && q.pos >= len(q.order) {
			q.pos = len(q.order) - 1
		}
		return
	}
	q.tracks = slices.Delete(q.tracks, i, i+1)
	q.ids = slices.Delete(q.ids, i, i+1)
	if i < q.cur {
		q.cur--
	} else if i == q.cur && q.cur >= len(q.tracks) {
		q.cur = len(q.tracks) - 1
	}
}

// RemoveEntry removes the occurrence id names. It returns false and
// changes nothing when no entry has that ID any more.
func (q *Queue) RemoveEntry(id EntryID) bool {
	real := slices.Index(q.ids, id)
	if real < 0 {
		return false
	}
	i := real
	if q.shuffle {
		i = slices.Index(q.order, real)
	}
	q.Remove(i)
	return true
}

// Clear empties the queue and starts a new generation. Shuffle and repeat
// preferences and the context are kept; autoplay marks and dismissals
// are forgotten.
func (q *Queue) Clear() {
	q.gen++
	q.rev++
	q.clearRemoved()
	q.tracks, q.ids, q.order = nil, nil, nil
	q.cur, q.pos = -1, -1
	clear(q.autoplay)
	clear(q.dismissed)
}

// Move moves the entry at playback position from to position to.
func (q *Queue) Move(from, to int) {
	if from == to || from < 0 || from >= len(q.tracks) {
		return
	}
	to = max(0, min(to, len(q.tracks)-1))
	q.rev++
	if q.shuffle {
		item := q.order[from]
		q.order = slices.Insert(slices.Delete(q.order, from, from+1), to, item)
		q.pos = shift(q.pos, from, to)
		return
	}
	t, id := q.tracks[from], q.ids[from]
	q.tracks = slices.Insert(slices.Delete(q.tracks, from, from+1), to, t)
	q.ids = slices.Insert(slices.Delete(q.ids, from, from+1), to, id)
	q.cur = shift(q.cur, from, to)
}

// shift maps a position p across a move from → to.
func shift(p, from, to int) int {
	switch {
	case p == from:
		return to
	case from < p && p <= to:
		return p - 1
	case to <= p && p < from:
		return p + 1
	}
	return p
}

// --- navigation -------------------------------------------------------------

// Next advances and returns the new current track. Repeat one returns the
// current track again (also on a manual skip, as the original). ok=false
// at the end of the queue.
func (q *Queue) Next() (ytmusic.Track, bool) {
	if len(q.tracks) == 0 {
		return ytmusic.Track{}, false
	}
	if q.removedCurrent {
		p := q.removedNextPosition()
		if p == len(q.tracks) {
			if q.repeat != RepeatAll {
				return ytmusic.Track{}, false
			}
			p = 0
		}
		q.rev++
		q.clearRemoved()
		q.setPos(p)
		return q.at(q.realIndex())
	}
	if q.repeat == RepeatOne {
		return q.at(q.realIndex())
	}
	if q.shuffle {
		if n := q.pos + 1; n < len(q.order) {
			q.pos = n
		} else if q.repeat == RepeatAll {
			q.rebuildShuffle(false)
			q.pos = 0
		} else {
			return ytmusic.Track{}, false
		}
	} else {
		if n := q.cur + 1; n < len(q.tracks) {
			q.cur = n
		} else if q.repeat == RepeatAll {
			q.cur = 0
		} else {
			return ytmusic.Track{}, false
		}
	}
	q.rev++
	return q.at(q.realIndex())
}

// Previous steps back and returns the new current track. (Restarting the
// track when more than 3 s in is the caller's rule.)
func (q *Queue) Previous() (ytmusic.Track, bool) {
	if len(q.tracks) == 0 {
		return ytmusic.Track{}, false
	}
	if q.removedCurrent {
		p := q.removedNextPosition() - 1
		if p < 0 {
			if q.repeat != RepeatAll {
				return ytmusic.Track{}, false
			}
			p = len(q.tracks) - 1
		}
		q.rev++
		q.clearRemoved()
		q.setPos(p)
		return q.at(q.realIndex())
	}
	if q.repeat == RepeatOne {
		return q.at(q.realIndex())
	}
	if q.shuffle {
		if p := q.pos - 1; p >= 0 {
			q.pos = p
		} else if q.repeat == RepeatAll {
			q.pos = len(q.order) - 1
		} else {
			return ytmusic.Track{}, false
		}
	} else {
		if p := q.cur - 1; p >= 0 {
			q.cur = p
		} else if q.repeat == RepeatAll {
			q.cur = len(q.tracks) - 1
		} else {
			return ytmusic.Track{}, false
		}
	}
	q.rev++
	return q.at(q.realIndex())
}

func (q *Queue) setPos(p int) {
	if q.shuffle {
		q.pos = p
	} else {
		q.cur = p
	}
}

// PeekNext returns the track Next would select, without moving. Under
// shuffle at the end of the order it returns false even with repeat all
// (the reshuffle is not known yet).
func (q *Queue) PeekNext() (ytmusic.Track, bool) {
	if len(q.tracks) == 0 {
		return ytmusic.Track{}, false
	}
	if q.removedCurrent {
		p := q.removedNextPosition()
		if p == len(q.tracks) {
			if q.repeat != RepeatAll {
				return ytmusic.Track{}, false
			}
			p = 0
		}
		if q.shuffle {
			return q.at(q.order[p])
		}
		return q.at(p)
	}
	if q.repeat == RepeatOne {
		return q.at(q.realIndex())
	}
	if q.shuffle {
		n := q.pos + 1
		if n >= len(q.order) {
			return ytmusic.Track{}, false
		}
		return q.at(q.order[n])
	}
	n := q.cur + 1
	if n >= len(q.tracks) {
		if q.repeat == RepeatAll {
			return q.at(0)
		}
		return ytmusic.Track{}, false
	}
	return q.at(n)
}

// PlayRandom jumps to a uniformly random entry.
func (q *Queue) PlayRandom() (ytmusic.Track, bool) {
	if len(q.tracks) == 0 {
		return ytmusic.Track{}, false
	}
	q.rev++
	q.clearRemoved()
	idx := q.rng.IntN(len(q.tracks))
	if q.shuffle {
		q.pos = max(0, slices.Index(q.order, idx))
	} else {
		q.cur = idx
	}
	return q.at(q.realIndex())
}

// JumpTo selects playback position i.
func (q *Queue) JumpTo(i int) (ytmusic.Track, bool) {
	if i < 0 || i >= len(q.tracks) {
		return ytmusic.Track{}, false
	}
	q.rev++
	q.clearRemoved()
	if q.shuffle {
		q.pos = i
		q.cur = q.order[i]
	} else {
		q.cur = i
	}
	return q.at(q.realIndex())
}

// JumpToEntry selects the occurrence id names.
func (q *Queue) JumpToEntry(id EntryID) (ytmusic.Track, bool) {
	real := slices.Index(q.ids, id)
	if real < 0 {
		return ytmusic.Track{}, false
	}
	q.rev++
	q.clearRemoved()
	q.cur = real
	if q.shuffle {
		q.pos = slices.Index(q.order, real)
	}
	return q.tracks[real], true
}

// --- repeat / shuffle -------------------------------------------------------

// SetRepeat sets the repeat mode.
func (q *Queue) SetRepeat(r Repeat) { q.repeat = r; q.rev++ }

// CycleRepeat steps off → all → one → off and returns the new mode.
func (q *Queue) CycleRepeat() Repeat {
	q.repeat = (q.repeat + 1) % 3
	q.rev++
	return q.repeat
}

// ToggleShuffle flips shuffle. Turning it on keeps the current entry first
// and shuffles the rest; turning it off returns to insertion order at the
// same entry.
func (q *Queue) ToggleShuffle() {
	q.rev++
	real := q.realIndex()
	q.shuffle = !q.shuffle
	if q.shuffle {
		q.rebuildShuffle(true)
		return
	}
	q.cur = -1
	if real >= 0 && real < len(q.tracks) {
		q.cur = real
	}
	q.order, q.pos = nil, -1
}

// SetShuffle sets shuffle to on (no-op when already there).
func (q *Queue) SetShuffle(on bool) {
	if q.shuffle != on {
		q.ToggleShuffle()
	}
}

// --- autoplay / radio -------------------------------------------------------

// AddRadio appends radio or autoplay suggestions, skipping video IDs already
// in the queue. With autoplay, the new entries are marked (↻) and video IDs
// the user removed from earlier autoplay batches are skipped. It returns
// the number added.
func (q *Queue) AddRadio(ts []ytmusic.Track, autoplay bool) int {
	skip := map[string]bool{}
	for _, t := range q.tracks {
		skip[t.VideoID] = true
	}
	var fresh []ytmusic.Track
	for _, t := range ts {
		if skip[t.VideoID] || (autoplay && q.dismissed[t.VideoID]) {
			continue
		}
		fresh = append(fresh, t)
	}
	ids := q.AddMany(fresh)
	if autoplay {
		for _, id := range ids {
			q.autoplay[id] = true
		}
	}
	return len(fresh)
}

// IsAutoplay reports whether autoplay appended the entry.
func (q *Queue) IsAutoplay(id EntryID) bool { return q.autoplay[id] }
