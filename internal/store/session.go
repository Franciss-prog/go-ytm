package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// SessionFile and ShufflePrefsFile live in the data dir.
const (
	SessionFile      = "session.json"
	ShufflePrefsFile = "shuffle_prefs.json"
)

// SessionVersion is session.json's schema_version; a file with another
// version is ignored (ytm-player _SESSION_SCHEMA_VERSION).
const SessionVersion = 1

// Session is the state restored on the next launch.
type Session struct {
	Version int             `json:"schema_version"`
	Volume  int             `json:"volume"`
	Repeat  string          `json:"repeat"`
	Shuffle bool            `json:"shuffle"`
	Tracks  []ytmusic.Track `json:"queue_tracks"` // playback order
	Index   int             `json:"queue_index"`  // -1: none current
	Context string          `json:"queue_context_id,omitempty"`
	Resume  *Resume         `json:"resume,omitempty"`
}

// Resume is the track and position playback stopped at.
type Resume struct {
	VideoID  string  `json:"video_id"`
	Position float64 `json:"position"` // seconds
}

// ResumeAt returns the resume position as a duration.
func (r Resume) ResumeAt() time.Duration { return time.Duration(r.Position * float64(time.Second)) }

// LoadSession reads dir/session.json. A missing, unreadable or foreign
// file yields ok = false (start fresh); err reports why, for the log.
func LoadSession(dir string) (s Session, ok bool, err error) {
	b, err := os.ReadFile(filepath.Join(dir, SessionFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("session: %w", err)
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return Session{}, false, fmt.Errorf("session: %w", err)
	}
	if s.Version != SessionVersion {
		return Session{}, false, fmt.Errorf("session: schema_version %d, want %d", s.Version, SessionVersion)
	}
	if s.Index < -1 || s.Index >= len(s.Tracks) {
		s.Index = -1
	}
	s.Volume = max(0, min(100, s.Volume))
	return s, true, nil
}

// SaveSession writes dir/session.json atomically.
func SaveSession(dir string, s Session) error {
	s.Version = SessionVersion
	b, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	return writeAtomic(dir, SessionFile, b)
}

// maxShufflePrefs caps shuffle_prefs.json (ytm-player _MAX_ENTRIES).
const maxShufflePrefs = 1000

// ShufflePrefs are per-playlist shuffle locks: a locked playlist always
// plays shuffled. The file maps playlist ID → true; order is not kept, so
// the cap drops arbitrary entries (ytm-player drops the least recently
// used). shortcut: no LRU order; upgrade if anyone reaches 1000 locks.
//
// Locked and Set only touch memory (safe on the UI goroutine); Save
// writes the file and may run concurrently with them.
type ShufflePrefs struct {
	dir  string
	mu   sync.Mutex // guards lock
	save sync.Mutex // orders Saves: a later Save writes a later state
	lock map[string]bool
}

// LoadShufflePrefs reads dir/shuffle_prefs.json (missing or broken: empty).
func LoadShufflePrefs(dir string) (*ShufflePrefs, error) {
	p := &ShufflePrefs{dir: dir, lock: map[string]bool{}}
	b, err := os.ReadFile(filepath.Join(dir, ShufflePrefsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return p, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &p.lock)
	}
	if err != nil {
		p.lock = map[string]bool{}
		return p, fmt.Errorf("shuffle prefs: %w", err)
	}
	return p, nil
}

// Locked reports whether playlist id has shuffle lock on.
func (p *ShufflePrefs) Locked(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return id != "" && p.lock[id]
}

// Set sets the lock for id in memory; Save persists it.
func (p *ShufflePrefs) Set(id string, on bool) {
	if id == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if on {
		p.lock[id] = true
	} else {
		delete(p.lock, id)
	}
	for k := range p.lock {
		if len(p.lock) <= maxShufflePrefs {
			break
		}
		if k != id {
			delete(p.lock, k)
		}
	}
}

// Save writes the current locks to dir/shuffle_prefs.json.
func (p *ShufflePrefs) Save() error {
	p.save.Lock()
	defer p.save.Unlock()
	p.mu.Lock()
	b, err := json.Marshal(p.lock)
	p.mu.Unlock()
	if err != nil {
		return fmt.Errorf("shuffle prefs: %w", err)
	}
	return writeAtomic(p.dir, ShufflePrefsFile, b)
}

// writeAtomic writes dir/name as 0600 via temp file + rename.
func writeAtomic(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	f, err := os.CreateTemp(dir, "."+name+".tmp-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}
