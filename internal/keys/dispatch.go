package keys

import (
	"strconv"
	"strings"
)

// MaxCount caps count prefixes (spec 04).
const MaxCount = 9999

// Action is a resolved key press.
type Action struct {
	Name string
	// Count is the typed prefix, 0 when none. Only Counted actions use it.
	Count int
}

// N returns the count, or 1 when none was typed.
func (a Action) N() int {
	if a.Count <= 0 {
		return 1
	}
	return a.Count
}

// Result is what one key press did.
type Result struct {
	// Action is set when a binding completed.
	Action *Action
	// Pending reports that a chord or count is in progress; the caller
	// arms the chord timeout and calls Timeout when it fires (with Seq).
	Pending bool
	// Unbound is the key that matched nothing at the top level; pages may
	// still use it (e.g. digits when no count applies).
	Unbound string
}

// Dispatcher turns key tokens into actions. It is a plain state machine
// with no clock or goroutine: the TUI arms a tea.Tick for the chord
// timeout and calls Timeout(seq). Not safe for concurrent use; it lives in
// the Bubble Tea model.
type Dispatcher struct {
	km    *Keymap
	cur   *node    // nil when no chord is pending
	keys  []string // pending chord keys, for display
	count int
	seq   uint64 // bumped on every state change, to discard stale timeouts
}

// NewDispatcher returns a dispatcher over km.
func NewDispatcher(km *Keymap) *Dispatcher { return &Dispatcher{km: km} }

// SetKeymap swaps the keymap and clears pending state (keymap reload).
func (d *Dispatcher) SetKeymap(km *Keymap) { d.km = km; d.Reset() }

// Seq identifies the current pending state for Timeout.
func (d *Dispatcher) Seq() uint64 { return d.seq }

// Reset drops any pending chord or count.
func (d *Dispatcher) Reset() {
	d.cur, d.keys, d.count = nil, nil, 0
	d.seq++
}

// Timeout cancels the pending chord/count if seq is still current.
func (d *Dispatcher) Timeout(seq uint64) {
	if seq == d.seq {
		d.Reset()
	}
}

// Pending renders the pending state for the status area: "15", "g-",
// "5g-", or "".
func (d *Dispatcher) Pending() string {
	var b strings.Builder
	if d.count > 0 {
		b.WriteString(strconv.Itoa(d.count))
	}
	if len(d.keys) > 0 {
		b.WriteString(strings.Join(d.keys, " "))
		b.WriteString("-")
	}
	return b.String()
}

// Feed processes one key token (see FromTea).
func (d *Dispatcher) Feed(key string) Result {
	if key == "" {
		return Result{}
	}
	if d.cur == nil && isDigit(key) && (d.count > 0 || key != "0") {
		if _, bound := d.km.root.children[key]; !bound || d.count > 0 {
			d.count = min(d.count*10+int(key[0]-'0'), MaxCount)
			d.seq++
			return Result{Pending: true}
		}
	}
	from := d.cur
	if from == nil {
		from = d.km.root
	}
	next, ok := from.children[key]
	if !ok {
		if d.cur != nil {
			// A non-matching key cancels the chord and is processed normally.
			d.Reset()
			return d.Feed(key)
		}
		d.Reset()
		return Result{Unbound: key}
	}
	if next.action != "" {
		a := &Action{Name: next.action}
		if info, ok := Lookup(next.action); ok && info.Counted {
			a.Count = d.count
		}
		d.Reset()
		return Result{Action: a}
	}
	d.cur = next
	d.keys = append(d.keys, key)
	d.seq++
	return Result{Pending: true}
}

func isDigit(k string) bool { return len(k) == 1 && k[0] >= '0' && k[0] <= '9' }
