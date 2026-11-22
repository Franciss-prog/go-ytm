// Package store persists local state: play and search history and the
// audio cache index in SQLite (ytm.db), the last session (session.json)
// and per-playlist shuffle locks (shuffle_prefs.json). Spec 01,
// "Persistence".
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver "sqlite" (no cgo, CLAUDE.md)

	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// DBFile is the SQLite database name inside the data dir.
const DBFile = "ytm.db"

// MaxPlays caps play_history (ytm-player's max_history default).
const MaxPlays = 10000

const schema = `
CREATE TABLE IF NOT EXISTS play_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	video_id TEXT NOT NULL,
	title TEXT NOT NULL,
	artist TEXT NOT NULL DEFAULT '',
	listened_seconds INTEGER NOT NULL,
	track TEXT NOT NULL,
	played_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_play_video ON play_history(video_id, played_at);
CREATE TABLE IF NOT EXISTS search_history (
	query TEXT NOT NULL,
	filter TEXT NOT NULL,
	search_count INTEGER NOT NULL DEFAULT 1,
	last_searched INTEGER NOT NULL,
	PRIMARY KEY (query, filter)
);
CREATE TABLE IF NOT EXISTS cache_index (
	video_id TEXT PRIMARY KEY,
	file_path TEXT NOT NULL,
	file_size INTEGER NOT NULL,
	last_accessed INTEGER NOT NULL
);
`

// DB is the SQLite store. Methods are safe for concurrent use (one
// connection; database/sql serializes callers) and must not run on the
// Bubble Tea goroutine.
type DB struct {
	db       *sql.DB
	now      func() time.Time
	maxPlays int
}

// Open opens (creating) dir/ytm.db with the schema applied.
func Open(ctx context.Context, dir string) (*DB, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	path := filepath.Join(dir, DBFile)
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: schema %s: %w", path, err)
	}
	_ = os.Chmod(path, 0o600)
	return &DB{db: db, now: time.Now, maxPlays: MaxPlays}, nil
}

// Close closes the database.
func (d *DB) Close() error { return d.db.Close() }

// --- play history -----------------------------------------------------------

// LogPlay records a play of t that lasted listened (callers apply
// history_min_listen_seconds) and trims the log to MaxPlays.
func (d *DB) LogPlay(ctx context.Context, t ytmusic.Track, listened time.Duration) error {
	b, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("store: log play: %w", err)
	}
	artist := ""
	if len(t.Artists) > 0 {
		artist = t.Artists[0].Name
	}
	if _, err := d.db.ExecContext(ctx,
		`INSERT INTO play_history (video_id, title, artist, listened_seconds, track, played_at) VALUES (?, ?, ?, ?, ?, ?)`,
		t.VideoID, t.Title, artist, int(listened/time.Second), string(b), d.now().UnixMilli()); err != nil {
		return fmt.Errorf("store: log play: %w", err)
	}
	if _, err := d.db.ExecContext(ctx,
		`DELETE FROM play_history WHERE id <= (SELECT id FROM play_history ORDER BY id DESC LIMIT 1 OFFSET ?)`, d.maxPlays); err != nil {
		return fmt.Errorf("store: trim plays: %w", err)
	}
	return nil
}

// RecentPlays returns up to limit tracks, newest play first, one row per
// video (its latest play), as ytm-player's get_recently_played.
func (d *DB) RecentPlays(ctx context.Context, limit int) ([]ytmusic.Track, error) {
	// Bare column with a single MAX(): SQLite takes it from the max row.
	rows, err := d.db.QueryContext(ctx,
		`SELECT track, MAX(played_at) AS at FROM play_history GROUP BY video_id ORDER BY at DESC, MAX(id) DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: recent plays: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ytmusic.Track
	for rows.Next() {
		var raw string
		var at int64
		if err := rows.Scan(&raw, &at); err != nil {
			return nil, fmt.Errorf("store: recent plays: %w", err)
		}
		var t ytmusic.Track
		if err := json.Unmarshal([]byte(raw), &t); err != nil {
			continue // a row from a future version: skip it, keep the rest
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: recent plays: %w", err)
	}
	return out, nil
}

// --- search history ---------------------------------------------------------

// LogSearch records a submitted search (repeats bump its count and time).
func (d *DB) LogSearch(ctx context.Context, query, filter string) error {
	if _, err := d.db.ExecContext(ctx,
		`INSERT INTO search_history (query, filter, last_searched) VALUES (?, ?, ?)
		 ON CONFLICT(query, filter) DO UPDATE SET search_count = search_count + 1, last_searched = excluded.last_searched`,
		query, filter, d.now().UnixMilli()); err != nil {
		return fmt.Errorf("store: log search: %w", err)
	}
	return nil
}

// RecentSearches returns up to limit distinct queries, newest first.
func (d *DB) RecentSearches(ctx context.Context, limit int) ([]string, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT query, MAX(last_searched) AS at FROM search_history GROUP BY query ORDER BY at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: recent searches: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var q string
		var at int64
		if err := rows.Scan(&q, &at); err != nil {
			return nil, fmt.Errorf("store: recent searches: %w", err)
		}
		out = append(out, q)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: recent searches: %w", err)
	}
	return out, nil
}

// --- cache index ------------------------------------------------------------

// CacheEntry is one audio file in the cache index.
type CacheEntry struct {
	VideoID string
	Path    string
	Size    int64
}

// CacheGet returns the entry for id and marks it used.
func (d *DB) CacheGet(ctx context.Context, id string) (CacheEntry, bool, error) {
	e := CacheEntry{VideoID: id}
	err := d.db.QueryRowContext(ctx, `SELECT file_path, file_size FROM cache_index WHERE video_id = ?`, id).Scan(&e.Path, &e.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return CacheEntry{}, false, nil
	}
	if err != nil {
		return CacheEntry{}, false, fmt.Errorf("store: cache get: %w", err)
	}
	if _, err := d.db.ExecContext(ctx, `UPDATE cache_index SET last_accessed = ? WHERE video_id = ?`, d.now().UnixMilli(), id); err != nil {
		return CacheEntry{}, false, fmt.Errorf("store: cache touch: %w", err)
	}
	return e, true, nil
}

// CachePut indexes (or re-indexes) a file.
func (d *DB) CachePut(ctx context.Context, e CacheEntry) error {
	if _, err := d.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO cache_index (video_id, file_path, file_size, last_accessed) VALUES (?, ?, ?, ?)`,
		e.VideoID, e.Path, e.Size, d.now().UnixMilli()); err != nil {
		return fmt.Errorf("store: cache put: %w", err)
	}
	return nil
}

// CacheDelete drops ids from the index (the caller removes the files).
func (d *DB) CacheDelete(ctx context.Context, ids ...string) error {
	for _, id := range ids {
		if _, err := d.db.ExecContext(ctx, `DELETE FROM cache_index WHERE video_id = ?`, id); err != nil {
			return fmt.Errorf("store: cache delete: %w", err)
		}
	}
	return nil
}

// CacheEntries returns every entry, least recently used first.
func (d *DB) CacheEntries(ctx context.Context) ([]CacheEntry, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT video_id, file_path, file_size FROM cache_index ORDER BY last_accessed, rowid`)
	if err != nil {
		return nil, fmt.Errorf("store: cache list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []CacheEntry
	for rows.Next() {
		var e CacheEntry
		if err := rows.Scan(&e.VideoID, &e.Path, &e.Size); err != nil {
			return nil, fmt.Errorf("store: cache list: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: cache list: %w", err)
	}
	return out, nil
}
