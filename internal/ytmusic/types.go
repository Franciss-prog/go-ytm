package ytmusic

import (
	"encoding/json"
	"fmt"
	"time"
)

// Artist is an artist credit on a track or album. ID is empty for credits
// that YouTube Music does not link (e.g. featured artists on some videos).
type Artist struct {
	Name string `json:"name"`
	ID   string `json:"id,omitempty"`
}

// LikeStatus is the signed-in user's rating of a track.
type LikeStatus int

// Like statuses. Unknown means the response did not say.
const (
	LikeUnknown LikeStatus = iota
	LikeIndifferent
	Like
	Dislike
)

// String returns the lowercase name used in JSON output.
func (l LikeStatus) String() string {
	switch l {
	case LikeIndifferent:
		return "indifferent"
	case Like:
		return "like"
	case Dislike:
		return "dislike"
	default:
		return "unknown"
	}
}

// MarshalJSON encodes the status as its name.
func (l LikeStatus) MarshalJSON() ([]byte, error) { return json.Marshal(l.String()) }

// Track is a playable song, video or episode, normalized across every
// renderer that can carry one (docs/specs/01-architecture.md).
type Track struct {
	VideoID      string        `json:"video_id"`
	Title        string        `json:"title"`
	Artists      []Artist      `json:"artists"`
	Album        *AlbumRef     `json:"album,omitempty"` // nil for videos and tracks without an album
	Duration     time.Duration `json:"-"`
	ThumbnailURL string        `json:"thumbnail_url,omitempty"`
	IsVideo      bool          `json:"is_video"` // UGC/OMV music videos, as opposed to ATV audio tracks
	LikeStatus   LikeStatus    `json:"like_status"`
	SetVideoID   string        `json:"set_video_id,omitempty"` // playlist item id
	Explicit     bool          `json:"explicit"`
	Available    bool          `json:"available"`       // false when YouTube Music greys the item out
	Views        string        `json:"views,omitempty"` // as displayed, e.g. "22M"
	VideoType    string        `json:"video_type,omitempty"`
}

// MarshalJSON adds duration_seconds alongside the other fields.
func (t Track) MarshalJSON() ([]byte, error) {
	type plain Track
	return json.Marshal(struct {
		plain
		DurationSeconds int `json:"duration_seconds"`
	}{plain(t), int(t.Duration / time.Second)})
}

// AlbumRef identifies an album, single or EP.
type AlbumRef struct {
	Name         string   `json:"name"`
	ID           string   `json:"id,omitempty"`          // browseId (MPREb_…)
	PlaylistID   string   `json:"playlist_id,omitempty"` // OLAK5uy_… audio playlist, when known
	Kind         string   `json:"kind,omitempty"`        // "Album", "Single", "EP" as displayed
	Artists      []Artist `json:"artists,omitempty"`
	Year         string   `json:"year,omitempty"`
	ThumbnailURL string   `json:"thumbnail_url,omitempty"`
	Explicit     bool     `json:"explicit,omitempty"`
}

// ArtistRef identifies an artist channel.
type ArtistRef struct {
	Name         string `json:"name"`
	ID           string `json:"id"`                 // channel id (UC…)
	Audience     string `json:"audience,omitempty"` // as displayed, e.g. "65.9M monthly audience"
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
}

// PlaylistRef identifies a playlist.
type PlaylistRef struct {
	Title        string `json:"title"`
	ID           string `json:"id"` // playlistId, without the "VL" browse prefix
	Author       string `json:"author,omitempty"`
	Count        string `json:"count,omitempty"` // as displayed: "35 songs", "676 views"
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
}

// ResultType names the kind of a search result.
type ResultType string

// Search result types.
const (
	TypeSong     ResultType = "song"
	TypeVideo    ResultType = "video"
	TypeAlbum    ResultType = "album"
	TypeArtist   ResultType = "artist"
	TypePlaylist ResultType = "playlist"
)

// SearchResult is one search hit. Exactly one of Track, Album, Artist and
// Playlist is set, matching Type.
type SearchResult struct {
	Type     ResultType
	Top      bool // the "Top result" card
	Track    *Track
	Album    *AlbumRef
	Artist   *ArtistRef
	Playlist *PlaylistRef
}

// Title returns the display title of whichever value is set.
func (r SearchResult) Title() string {
	switch {
	case r.Track != nil:
		return r.Track.Title
	case r.Album != nil:
		return r.Album.Name
	case r.Artist != nil:
		return r.Artist.Name
	case r.Playlist != nil:
		return r.Playlist.Title
	}
	return ""
}

// MarshalJSON flattens the result into its value with a "type" field
// (docs/specs/06-cli-ipc.md: `[Track|Album|Artist|Playlist]` with "type").
func (r SearchResult) MarshalJSON() ([]byte, error) {
	var v any
	switch {
	case r.Track != nil:
		v = r.Track
	case r.Album != nil:
		v = r.Album
	case r.Artist != nil:
		v = r.Artist
	case r.Playlist != nil:
		v = r.Playlist
	default:
		return nil, fmt.Errorf("ytmusic: empty %s search result", r.Type)
	}
	inner, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	head, err := json.Marshal(struct {
		Type ResultType `json:"type"`
		Top  bool       `json:"top,omitempty"`
	}{r.Type, r.Top})
	if err != nil {
		return nil, err
	}
	// Splice {"type":…} and the value's fields into one object.
	if len(inner) == 2 { // "{}"
		return head, nil
	}
	out := make([]byte, 0, len(head)+len(inner))
	out = append(out, head[:len(head)-1]...)
	out = append(out, ',')
	out = append(out, inner[1:]...)
	return out, nil
}

// SearchResults is one page of search results. Continuation is non-empty
// when a filtered search has more pages.
type SearchResults struct {
	Items        []SearchResult
	Continuation string
}

// AccountInfo describes the signed-in account (account/account_menu).
type AccountInfo struct {
	Name      string `json:"name"`
	Handle    string `json:"handle,omitempty"`
	ChannelID string `json:"channel_id,omitempty"` // empty when the menu has no unique "Your channel" link
	PhotoURL  string `json:"photo_url,omitempty"`
}
