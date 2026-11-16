package player

// trigger is an input to the status state machine.
type trigger int

const (
	trLoad            trigger = iota // we issued loadfile
	trStartFile                      // mpv started a known entry (also gapless advance)
	trPlaybackRestart                // playback (re)started after load or seek
	trPause                          // pause property changed; on = paused
	trCache                          // paused-for-cache changed; on = buffering
	trEndError                       // end-file reason=error for a known entry
	trIdle                           // idle-active became true
	trCrash                          // socket EOF or process exit
	trRestarted                      // relaunch succeeded and reload issued
	trGaveUp                         // restart budget exhausted
)

// nextStatus is the pure transition function from the spec 02 diagram.
// Unlisted (status, trigger) pairs keep the current status.
func nextStatus(s Status, t trigger, on bool) Status {
	switch t {
	case trLoad:
		return Loading
	case trStartFile:
		if s == Crashed || s == Error {
			return s
		}
		return Loading
	case trPlaybackRestart:
		if s == Loading {
			if on {
				return Paused
			}
			return Playing
		}
	case trPause:
		switch {
		case on && (s == Playing || s == Buffering):
			return Paused
		case !on && s == Paused:
			return Playing
		}
	case trCache:
		switch {
		case on && s == Playing:
			return Buffering
		case !on && s == Buffering:
			return Playing
		}
	case trEndError:
		if s != Crashed {
			return Error
		}
	case trIdle:
		if s == Playing || s == Paused || s == Buffering {
			return Stopped
		}
	case trCrash:
		if s == Stopped || s == Error {
			return Stopped
		}
		return Crashed
	case trRestarted:
		if s == Crashed {
			return Loading
		}
	case trGaveUp:
		return Stopped
	}
	return s
}
