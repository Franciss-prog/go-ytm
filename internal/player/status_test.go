package player

import "testing"

func TestNextStatus(t *testing.T) {
	tests := []struct {
		from Status
		tr   trigger
		on   bool
		want Status
	}{
		{Stopped, trLoad, false, Loading},
		{Error, trLoad, false, Loading},
		{Playing, trLoad, false, Loading},
		{Loading, trPlaybackRestart, false, Playing},
		{Loading, trPlaybackRestart, true, Paused},
		{Playing, trPlaybackRestart, false, Playing}, // after a seek
		{Paused, trPlaybackRestart, true, Paused},
		{Loading, trEndError, false, Error},
		{Playing, trEndError, false, Error},
		{Playing, trPause, true, Paused},
		{Paused, trPause, false, Playing},
		{Loading, trPause, true, Loading},
		{Stopped, trPause, false, Stopped},
		{Buffering, trPause, true, Paused},
		{Playing, trCache, true, Buffering},
		{Buffering, trCache, false, Playing},
		{Paused, trCache, true, Paused},
		{Playing, trStartFile, false, Loading}, // gapless next
		{Paused, trStartFile, false, Loading},
		{Error, trStartFile, false, Error},
		{Playing, trIdle, false, Stopped},
		{Paused, trIdle, false, Stopped},
		{Buffering, trIdle, false, Stopped},
		{Loading, trIdle, false, Loading}, // idle before start-file of a new load
		{Error, trIdle, false, Error},
		{Playing, trCrash, false, Crashed},
		{Paused, trCrash, false, Crashed},
		{Loading, trCrash, false, Crashed},
		{Buffering, trCrash, false, Crashed},
		{Stopped, trCrash, false, Stopped},
		{Error, trCrash, false, Stopped},
		{Crashed, trRestarted, false, Loading},
		{Playing, trRestarted, false, Playing},
		{Crashed, trGaveUp, false, Stopped},
		{Crashed, trEndError, false, Crashed},
	}
	for _, tt := range tests {
		if got := nextStatus(tt.from, tt.tr, tt.on); got != tt.want {
			t.Errorf("nextStatus(%v, %d, %v) = %v, want %v", tt.from, tt.tr, tt.on, got, tt.want)
		}
	}
}

func TestStatusString(t *testing.T) {
	if Buffering.String() != "buffering" || Status(99).String() != "Status(99)" {
		t.Fatal("String mismatch")
	}
}
