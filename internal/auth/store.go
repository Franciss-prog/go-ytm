package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// Credential file names in the config dir (kept from ytm-player).
const (
	AuthFile          = "auth.json"
	AccountFile       = "account.json"
	StreamCookiesFile = "stream_cookies.txt"
)

// ErrNotConfigured means `ytm setup` has not been run.
var ErrNotConfigured = errors.New("not signed in; run `ytm setup`")

// Account is the identity recorded at setup, used to make sure a silent
// renewal signs in as the same account.
type Account struct {
	SchemaVersion int       `json:"schema_version"`
	Name          string    `json:"name"`
	Handle        string    `json:"handle,omitempty"`
	ChannelID     string    `json:"channel_id,omitempty"`
	Slot          int       `json:"slot"`
	Source        string    `json:"source"` // browser name, "cookies_file" or "manual"
	SavedAt       time.Time `json:"saved_at"`
}

// Session is everything `ytm setup` stores.
type Session struct {
	Credentials ytmusic.Credentials
	Account     Account
	StreamJar   []Cookie // may be empty (manual setup without a jar)
}

// authHeaders is the auth.json shape: request headers with lowercase
// names, like ytmusicapi's browser auth file.
type authHeaders struct {
	Cookie    string `json:"cookie"`
	UserAgent string `json:"user-agent,omitempty"`
	AuthUser  string `json:"x-goog-authuser"`
	Origin    string `json:"origin"`
}

// Store reads and writes credential files in Dir (the config dir).
type Store struct{ Dir string }

func (s Store) path(name string) string { return filepath.Join(s.Dir, name) }

// Save writes all credential files atomically, each with mode 0600 in a
// 0700 directory. account.json is written last: it marks a complete setup.
func (s Store) Save(sess Session) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if len(sess.StreamJar) > 0 {
		var b bytes.Buffer
		if err := WriteNetscape(&b, sess.StreamJar); err != nil {
			return err
		}
		if err := writeFileAtomic(s.path(StreamCookiesFile), b.Bytes()); err != nil {
			return err
		}
	}
	auth, err := json.MarshalIndent(authHeaders{
		Cookie: sess.Credentials.Cookie, UserAgent: sess.Credentials.UserAgent,
		AuthUser: strconv.Itoa(sess.Credentials.AuthUser), Origin: ytmusic.Origin,
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(s.path(AuthFile), append(auth, '\n')); err != nil {
		return err
	}
	acct := sess.Account
	acct.SchemaVersion = 1
	acct.Slot = sess.Credentials.AuthUser
	raw, err := json.MarshalIndent(acct, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path(AccountFile), append(raw, '\n'))
}

// Load reads auth.json and account.json. A missing auth.json is
// ErrNotConfigured; a missing account.json is tolerated (empty Account).
func (s Store) Load() (Session, error) {
	raw, err := os.ReadFile(s.path(AuthFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Session{}, ErrNotConfigured
	}
	if err != nil {
		return Session{}, fmt.Errorf("read %s: %w", AuthFile, err)
	}
	var h authHeaders
	if err := json.Unmarshal(raw, &h); err != nil {
		return Session{}, fmt.Errorf("parse %s: %w", s.path(AuthFile), err)
	}
	if h.Cookie == "" {
		return Session{}, fmt.Errorf("%s has no cookie: %w", s.path(AuthFile), ErrNotConfigured)
	}
	slot, _ := strconv.Atoi(h.AuthUser)
	sess := Session{Credentials: ytmusic.Credentials{Cookie: h.Cookie, UserAgent: h.UserAgent, AuthUser: slot}}
	if raw, err := os.ReadFile(s.path(AccountFile)); err == nil {
		if err := json.Unmarshal(raw, &sess.Account); err != nil {
			return Session{}, fmt.Errorf("parse %s: %w", s.path(AccountFile), err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Session{}, fmt.Errorf("read %s: %w", AccountFile, err)
	}
	return sess, nil
}

// Exists reports whether auth.json exists.
func (s Store) Exists() bool {
	_, err := os.Stat(s.path(AuthFile))
	return err == nil
}

// writeFileAtomic writes data to a 0600 temp file in the target dir, syncs
// it, and renames it over path.
func writeFileAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(0o600); err != nil && runtimeGOOS != "windows" {
		return fmt.Errorf("chmod %s: %w", filepath.Base(path), err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", filepath.Base(path), err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	ok = true
	return nil
}
