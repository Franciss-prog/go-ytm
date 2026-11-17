package queue

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// Cases ported from ytm-player tests/test_services/test_queue.py and
// test_queue_entries.py (commit d1681465e03d0d9397253b08e0e5c6537c0eccfc).

func tr(id string) ytmusic.Track { return ytmusic.Track{VideoID: id, Title: "T " + id} }

func five() []ytmusic.Track {
	return []ytmusic.Track{tr("vid_01"), tr("vid_02"), tr("vid_03"), tr("vid_04"), tr("vid_05")}
}

func newQ(t *testing.T, seed uint64) *Queue {
	t.Helper()
	return New(rand.New(rand.NewPCG(seed, seed+1)))
}

func filled(t *testing.T, seed uint64) *Queue {
	q := newQ(t, seed)
	q.AddMany(five())
	return q
}

func vid(t ytmusic.Track, ok bool) string {
	if !ok {
		return "<none>"
	}
	return t.VideoID
}

func ids(q *Queue) []string {
	var out []string
	for _, tr := range q.Tracks() {
		out = append(out, tr.VideoID)
	}
	return out
}

func TestEmpty(t *testing.T) {
	q := newQ(t, 1)
	if q.Len() != 0 || q.CurrentIndex() != -1 {
		t.Fatal("not empty")
	}
	for name, f := range map[string]func() (ytmusic.Track, bool){
		"current": q.Current, "next": q.Next, "previous": q.Previous, "peek": q.PeekNext, "random": q.PlayRandom,
	} {
		if _, ok := f(); ok {
			t.Errorf("%s on empty queue returned a track", name)
		}
	}
}

func TestRepeatModes(t *testing.T) {
	q := newQ(t, 1)
	q.Add(tr("a"))
	q.JumpTo(0)
	if _, ok := q.Next(); ok {
		t.Error("repeat off: next at end should be none")
	}
	q.SetRepeat(RepeatOne)
	if got := vid(q.Next()); got != "a" {
		t.Errorf("repeat one: next = %s", got)
	}
	q.SetRepeat(RepeatAll)
	q.Add(tr("b"))
	q.JumpTo(1)
	if got := vid(q.Next()); got != "a" {
		t.Errorf("repeat all wrap = %s", got)
	}
	if got := vid(q.Previous()); got != "b" {
		t.Errorf("repeat all backward wrap = %s", got)
	}
}

func TestCycleRepeat(t *testing.T) {
	q := newQ(t, 1)
	var got []string
	for range 4 {
		got = append(got, q.CycleRepeat().String())
	}
	if want := []string{"all", "one", "off", "all"}; !slices.Equal(got, want) {
		t.Fatalf("cycle = %v, want %v", got, want)
	}
	if ParseRepeat("one") != RepeatOne || ParseRepeat("x") != RepeatOff {
		t.Fatal("ParseRepeat")
	}
}

func TestLinearNavigation(t *testing.T) {
	q := filled(t, 1)
	q.JumpTo(0)
	for _, want := range []string{"vid_02", "vid_03", "vid_04", "vid_05", "<none>"} {
		if got := vid(q.Next()); got != want {
			t.Fatalf("next = %s, want %s", got, want)
		}
	}
	for _, want := range []string{"vid_04", "vid_03", "vid_02", "vid_01", "<none>"} {
		if got := vid(q.Previous()); got != want {
			t.Fatalf("previous = %s, want %s", got, want)
		}
	}
}

func TestShuffleKeepsCurrentFirstAndPlaysAll(t *testing.T) {
	for seed := range uint64(20) {
		q := filled(t, seed)
		q.JumpTo(2)
		q.ToggleShuffle()
		if q.CurrentIndex() != 0 || vid(q.Current()) != "vid_03" {
			t.Fatalf("seed %d: current after shuffle on = %d %s", seed, q.CurrentIndex(), vid(q.Current()))
		}
		seen := map[string]bool{"vid_03": true}
		for {
			tr, ok := q.Next()
			if !ok {
				break
			}
			seen[tr.VideoID] = true
		}
		if len(seen) != 5 {
			t.Fatalf("seed %d: shuffle played %d of 5", seed, len(seen))
		}
		last := vid(q.Current())
		q.ToggleShuffle()
		if vid(q.Current()) != last || q.Shuffle() {
			t.Fatalf("seed %d: shuffle off moved current from %s to %s", seed, last, vid(q.Current()))
		}
		if !slices.Equal(ids(q), []string{"vid_01", "vid_02", "vid_03", "vid_04", "vid_05"}) {
			t.Fatalf("seed %d: insertion order lost: %v", seed, ids(q))
		}
	}
}

func TestShuffleOffKeepsTrackReachedByPrevious(t *testing.T) {
	q := filled(t, 3)
	q.JumpTo(0)
	q.ToggleShuffle()
	q.Next()
	q.Next()
	q.Previous()
	want := vid(q.Current())
	q.ToggleShuffle()
	if got := vid(q.Current()); got != want {
		t.Fatalf("current = %s, want %s", got, want)
	}
}

func TestShuffleRepeatAllReshufflesAtEnd(t *testing.T) {
	q := filled(t, 4)
	q.JumpTo(0)
	q.ToggleShuffle()
	q.SetRepeat(RepeatAll)
	q.JumpTo(4)
	if _, ok := q.PeekNext(); ok {
		t.Error("peek at end of shuffle order should be unknown")
	}
	if _, ok := q.Next(); !ok || q.CurrentIndex() != 0 {
		t.Fatalf("wrap: index %d", q.CurrentIndex())
	}
}

func TestInsertAndAddNext(t *testing.T) {
	q := filled(t, 1)
	q.JumpTo(2)
	q.Insert(1, tr("x"))
	if vid(q.Current()) != "vid_03" || q.CurrentIndex() != 3 {
		t.Fatalf("insert before current: %d %s", q.CurrentIndex(), vid(q.Current()))
	}
	q.AddNext(tr("n1"), tr("n2"))
	for _, want := range []string{"n1", "n2", "vid_04"} {
		if got := vid(q.Next()); got != want {
			t.Fatalf("next = %s, want %s", got, want)
		}
	}
	e := newQ(t, 1)
	e.AddNext(tr("a"), tr("b"))
	if !slices.Equal(ids(e), []string{"a", "b"}) {
		t.Fatalf("empty queue add next: %v", ids(e))
	}
}

func TestAddNextUnderShuffle(t *testing.T) {
	q := filled(t, 7)
	q.JumpTo(0)
	q.ToggleShuffle()
	q.AddNext(tr("n1"), tr("n2"))
	for _, want := range []string{"n1", "n2"} {
		if got := vid(q.Next()); got != want {
			t.Fatalf("next = %s, want %s", got, want)
		}
	}
}

func TestAddUnderShuffleLandsInFuture(t *testing.T) {
	for seed := range uint64(30) {
		q := filled(t, seed)
		q.JumpTo(0)
		q.ToggleShuffle()
		q.Next()
		q.Add(tr("new"))
		i := slices.Index(ids(q), "new")
		if i <= q.CurrentIndex() {
			t.Fatalf("seed %d: added at %d, current %d", seed, i, q.CurrentIndex())
		}
	}
}

func TestMove(t *testing.T) {
	q := filled(t, 1)
	q.JumpTo(1)
	q.Move(0, 3)
	if !slices.Equal(ids(q), []string{"vid_02", "vid_03", "vid_04", "vid_01", "vid_05"}) {
		t.Fatalf("order %v", ids(q))
	}
	if vid(q.Current()) != "vid_02" || q.CurrentIndex() != 0 {
		t.Fatalf("current %d %s", q.CurrentIndex(), vid(q.Current()))
	}
	q.Move(0, 99) // clamps
	if q.CurrentIndex() != 4 {
		t.Fatalf("moving current: index %d", q.CurrentIndex())
	}
	before := q.Entries()
	q.Move(4, 0)
	if q.Entries()[0].ID != before[4].ID {
		t.Fatal("entry id did not move with its track")
	}
}

func TestMoveUnderShuffle(t *testing.T) {
	q := filled(t, 2)
	q.JumpTo(0)
	q.ToggleShuffle()
	order := ids(q)
	q.Move(0, 2)
	if vid(q.Current()) != order[0] || q.CurrentIndex() != 2 {
		t.Fatalf("current %d %s", q.CurrentIndex(), vid(q.Current()))
	}
}

func TestRemoveAdjustsCurrent(t *testing.T) {
	q := filled(t, 1)
	q.JumpTo(3)
	q.Remove(1)
	if vid(q.Current()) != "vid_04" {
		t.Fatalf("remove before: %s", vid(q.Current()))
	}
	q.Remove(3)
	if vid(q.Current()) != "vid_04" || q.Len() != 3 {
		t.Fatalf("remove after: %s", vid(q.Current()))
	}
}

func TestRemoveCurrentFinishesOutsideQueue(t *testing.T) {
	for _, shuffle := range []bool{false, true} {
		q := filled(t, 5)
		q.SetShuffle(shuffle)
		q.JumpTo(2)
		order := ids(q)
		q.Remove(2)
		if _, ok := q.Current(); ok || q.CurrentIndex() != -1 {
			t.Fatalf("shuffle=%v: current after removing it", shuffle)
		}
		if got := vid(q.PeekNext()); got != order[3] {
			t.Fatalf("shuffle=%v: peek = %s, want %s", shuffle, got, order[3])
		}
		if got := vid(q.Next()); got != order[3] {
			t.Fatalf("shuffle=%v: next = %s, want %s", shuffle, got, order[3])
		}
	}
}

func TestRemoveCurrentAtEnd(t *testing.T) {
	q := filled(t, 1)
	q.JumpTo(4)
	q.Remove(4)
	if _, ok := q.Next(); ok {
		t.Fatal("next after removing last should be none")
	}
	q.Add(tr("tail"))
	if got := vid(q.Next()); got != "tail" {
		t.Fatalf("append after removed last: %s", got)
	}
}

func TestRemoveOnlyTrack(t *testing.T) {
	for _, shuffle := range []bool{false, true} {
		q := newQ(t, 1)
		q.Add(tr("only"))
		q.JumpTo(0)
		q.SetShuffle(shuffle)
		q.Remove(0)
		if q.Len() != 0 || q.CurrentIndex() != -1 {
			t.Fatalf("shuffle=%v: len %d index %d", shuffle, q.Len(), q.CurrentIndex())
		}
	}
}

// TestFinishThenNext ports TestRemovedAudibleBoundary.test_finish_then_next.
func TestFinishThenNext(t *testing.T) {
	for _, shuffle := range []bool{false, true} {
		for _, rep := range []Repeat{RepeatOff, RepeatAll, RepeatOne} {
			for _, c := range []struct{ count, pos int }{{1, 0}, {4, 0}, {4, 1}, {4, 3}} {
				t.Run(fmt.Sprintf("shuffle=%v/%s/%d@%d", shuffle, rep, c.count, c.pos), func(t *testing.T) {
					q := newQ(t, 9)
					same := make([]ytmusic.Track, c.count)
					for i := range same {
						same[i] = tr("A")
					}
					q.AddMany(same)
					q.SetShuffle(shuffle)
					q.SetRepeat(rep)
					q.JumpTo(c.pos)
					before := q.Entries()
					q.RemoveEntry(before[c.pos].ID)
					if _, ok := q.Current(); ok || q.CurrentIndex() != -1 {
						t.Fatal("current survived removal")
					}
					var want EntryID
					if c.pos+1 < c.count {
						want = before[c.pos+1].ID
					} else if c.count > 1 && rep == RepeatAll {
						want = before[0].ID
					}
					_, ok := q.Next()
					if want == 0 {
						if ok {
							t.Fatal("next should be none")
						}
						return
					}
					if !ok || q.Entries()[q.CurrentIndex()].ID != want {
						t.Fatalf("next selected the wrong occurrence")
					}
				})
			}
		}
	}
}

func TestRemovingSuccessorAgainKeepsFollowingSurvivor(t *testing.T) {
	for _, shuffle := range []bool{false, true} {
		q := filled(t, 11)
		q.SetShuffle(shuffle)
		q.JumpTo(1)
		before := q.Entries()
		q.RemoveEntry(before[1].ID)
		q.RemoveEntry(before[2].ID)
		if got := vid(q.PeekNext()); got != before[3].Track.VideoID {
			t.Fatalf("shuffle=%v: peek %s", shuffle, got)
		}
		if got := vid(q.Next()); got != before[3].Track.VideoID {
			t.Fatalf("shuffle=%v: next %s", shuffle, got)
		}
	}
}

func TestPlayNextAtRemovedBoundary(t *testing.T) {
	for _, shuffle := range []bool{false, true} {
		q := newQ(t, 12)
		q.AddMany([]ytmusic.Track{tr("0"), tr("1"), tr("2"), tr("3")})
		q.SetShuffle(shuffle)
		q.JumpTo(1)
		before := q.Entries()
		q.RemoveEntry(before[1].ID)
		q.AddNext(tr("X"), tr("Y"))
		for _, want := range []string{"X", "Y", before[2].Track.VideoID} {
			if got := vid(q.Next()); got != want {
				t.Fatalf("shuffle=%v: next %s, want %s", shuffle, got, want)
			}
		}
	}
}

func TestReorderKeepsSurvivingSuccessor(t *testing.T) {
	q := filled(t, 13)
	q.JumpTo(1)
	succ := q.Entries()[2]
	q.Remove(1)
	q.Move(1, 3)
	q.ToggleShuffle()
	if got := vid(q.Next()); got != succ.Track.VideoID || q.Entries()[q.CurrentIndex()].ID != succ.ID {
		t.Fatalf("next %s", got)
	}
}

func TestGeneration(t *testing.T) {
	q := newQ(t, 1)
	g := q.Generation()
	q.AddMany([]ytmusic.Track{tr("A"), tr("A")})
	q.JumpTo(0)
	q.Next()
	q.ToggleShuffle()
	q.Move(0, 1)
	q.Remove(0)
	if q.Generation() != g {
		t.Fatal("generation changed without Clear")
	}
	q.Clear()
	if q.Generation() != g+1 || q.Len() != 0 || !q.Shuffle() {
		t.Fatal("clear: generation/len/shuffle preference")
	}
}

func TestEntryIDsDistinctAndStable(t *testing.T) {
	q := newQ(t, 1)
	a := tr("A")
	q.Add(a)
	q.Add(a)
	q.Insert(0, a)
	q.AddMany([]ytmusic.Track{a, a})
	q.JumpTo(0)
	q.AddNext(a)
	es := q.Entries()
	seen := map[EntryID]bool{}
	for _, e := range es {
		if e.ID == 0 || seen[e.ID] {
			t.Fatalf("duplicate or zero id %d", e.ID)
		}
		seen[e.ID] = true
	}
	if q.RemoveEntry(es[2].ID) != true || q.RemoveEntry(es[2].ID) != false {
		t.Fatal("RemoveEntry twice")
	}
	if q.Len() != len(es)-1 {
		t.Fatal("RemoveEntry removed more than one")
	}
	if q.RemoveEntry(9999) {
		t.Fatal("unknown id removed something")
	}
}

func TestAutoplay(t *testing.T) {
	q := newQ(t, 1)
	q.AddMany([]ytmusic.Track{tr("u1"), tr("u2")})
	if n := q.AddRadio([]ytmusic.Track{tr("u1"), tr("r1"), tr("r2")}, true); n != 2 {
		t.Fatalf("added %d, want 2 (dedupe by video id)", n)
	}
	es := q.Entries()
	if es[0].Autoplay || !es[2].Autoplay || !q.IsAutoplay(es[3].ID) {
		t.Fatal("autoplay marks")
	}
	q.RemoveEntry(es[2].ID) // dismiss r1
	q.Remove(0)             // a user track: dismisses nothing
	if n := q.AddRadio([]ytmusic.Track{tr("r1"), tr("u1")}, true); n != 1 {
		t.Fatalf("refill added %d, want 1 (u1 ok, r1 dismissed)", n)
	}
	if n := q.AddRadio([]ytmusic.Track{tr("r1")}, false); n != 1 {
		t.Fatal("dismissal must only filter autoplay")
	}
	if q.Entries()[q.Len()-1].Autoplay {
		t.Fatal("plain radio tracks are not autoplay")
	}
	q.Clear()
	if n := q.AddRadio([]ytmusic.Track{tr("r1")}, true); n != 1 {
		t.Fatal("clear should forget dismissals")
	}
}

func TestAutoplayMarksFollowEntriesUnderShuffle(t *testing.T) {
	q := filled(t, 21)
	q.JumpTo(0)
	q.ToggleShuffle()
	q.AddRadio([]ytmusic.Track{tr("r1"), tr("r2")}, true)
	for _, e := range q.Entries() {
		if e.Autoplay != (e.Track.VideoID == "r1" || e.Track.VideoID == "r2") {
			t.Fatalf("%s autoplay=%v", e.Track.VideoID, e.Autoplay)
		}
	}
}

func TestAddManyOnEmptyShuffledQueueRebuilds(t *testing.T) {
	q := newQ(t, 1)
	q.SetShuffle(true)
	q.AddMany(five())
	if q.Len() != 5 || len(q.order) != 5 || q.CurrentIndex() != -1 {
		t.Fatalf("len %d order %d idx %d", q.Len(), len(q.order), q.CurrentIndex())
	}
	if got := vid(q.Next()); got == "<none>" {
		t.Fatal("next from fresh shuffled queue")
	}
}

func TestRemainingAndPlayed(t *testing.T) {
	q := filled(t, 1)
	q.JumpTo(1)
	if q.Remaining() != 3 {
		t.Fatalf("remaining %d", q.Remaining())
	}
	if p := q.PlayedTracks(); len(p) != 2 || p[1].VideoID != "vid_02" {
		t.Fatalf("played %v", p)
	}
	q.ToggleShuffle()
	if q.Remaining() != 4 {
		t.Fatalf("shuffled remaining %d", q.Remaining())
	}
}

func TestPlayRandom(t *testing.T) {
	q := filled(t, 1)
	for range 20 {
		tr, ok := q.PlayRandom()
		if !ok || tr.VideoID != vid(q.Current()) {
			t.Fatal("random selection is not current")
		}
	}
}

func TestRevBumps(t *testing.T) {
	q := newQ(t, 1)
	r := q.Rev()
	q.Add(tr("a"))
	q.JumpTo(0)
	if q.Rev() <= r {
		t.Fatal("rev did not change")
	}
}

// TestRandomOpsKeepInvariants runs random operation sequences and checks
// the internal structure after each step.
func TestRandomOpsKeepInvariants(t *testing.T) {
	for seed := range uint64(300) {
		rng := rand.New(rand.NewPCG(seed, 99))
		q := New(rand.New(rand.NewPCG(seed, 7)))
		for step := range 200 {
			n := q.Len()
			switch rng.IntN(14) {
			case 0:
				q.Add(tr(fmt.Sprint(rng.IntN(8))))
			case 1:
				q.Insert(rng.IntN(n+2)-1, tr(fmt.Sprint(rng.IntN(8))))
			case 2:
				q.AddMany([]ytmusic.Track{tr("m1"), tr("m2")})
			case 3:
				q.AddNext(tr("n"))
			case 4:
				q.Remove(rng.IntN(n + 1))
			case 5:
				q.Move(rng.IntN(n+1), rng.IntN(n+1))
			case 6:
				q.Next()
			case 7:
				q.Previous()
			case 8:
				q.ToggleShuffle()
			case 9:
				q.CycleRepeat()
			case 10:
				q.JumpTo(rng.IntN(n + 1))
			case 11:
				q.PlayRandom()
			case 12:
				q.AddRadio([]ytmusic.Track{tr(fmt.Sprint(rng.IntN(12)))}, true)
			case 13:
				if rng.IntN(10) == 0 {
					q.Clear()
				}
			}
			if err := q.check(); err != nil {
				t.Fatalf("seed %d step %d: %v", seed, step, err)
			}
		}
	}
}

func (q *Queue) check() error {
	if len(q.ids) != len(q.tracks) {
		return fmt.Errorf("ids %d tracks %d", len(q.ids), len(q.tracks))
	}
	seen := map[EntryID]bool{}
	for _, id := range q.ids {
		if id == 0 || seen[id] {
			return fmt.Errorf("bad id %d", id)
		}
		seen[id] = true
	}
	for id := range q.autoplay {
		if !seen[id] {
			return fmt.Errorf("autoplay mark on removed entry %d", id)
		}
	}
	if q.shuffle {
		s := slices.Sorted(slices.Values(q.order))
		for i, v := range s {
			if v != i {
				return fmt.Errorf("order not a permutation: %v", q.order)
			}
		}
		if len(s) != len(q.tracks) {
			return fmt.Errorf("order len %d tracks %d", len(s), len(q.tracks))
		}
		if q.pos < -1 || q.pos >= max(len(q.order), 1) {
			return fmt.Errorf("pos %d of %d", q.pos, len(q.order))
		}
	} else if q.cur < -1 || q.cur >= max(len(q.tracks), 1) {
		return fmt.Errorf("cur %d of %d", q.cur, len(q.tracks))
	}
	if q.removedCurrent && q.CurrentIndex() != -1 {
		return fmt.Errorf("removed current but index %d", q.CurrentIndex())
	}
	if !q.removedCurrent && q.Len() > 0 && q.CurrentIndex() >= q.Len() {
		return fmt.Errorf("index %d beyond %d", q.CurrentIndex(), q.Len())
	}
	if p := q.removedNextPosition(); p < 0 || p > q.Len() {
		return fmt.Errorf("removed next position %d", p)
	}
	return nil
}
