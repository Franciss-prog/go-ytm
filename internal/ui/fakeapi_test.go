package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// fakeAPI is an in-memory ytmusic.Client. Errors in errs (by method name)
// are returned instead of data; calls records "Method:arg".
type fakeAPI struct {
	mu    sync.Mutex
	errs  map[string]error
	calls []string

	playlists []ytmusic.PlaylistRef
	albums    []ytmusic.AlbumRef
	artists   []ytmusic.ArtistRef
	// pages holds playlist tracks; served pageSize at a time.
	pages    map[string][]ytmusic.Track
	pageSize int
	search   []ytmusic.SearchResult
	suggest  map[string][]string
	edits    []string
}

func newFakeAPI() *fakeAPI {
	ts := scenarioTracks()
	return &fakeAPI{
		errs:     map[string]error{},
		pageSize: 4,
		playlists: []ytmusic.PlaylistRef{
			{Title: "Liked Music", ID: "LM", Count: "1,204 songs"},
			{Title: "Late night drives", ID: "PLnight", Count: "87 songs", Owned: true},
			{Title: "Focus", ID: "PLfocus", Count: "43 songs", Owned: true},
		},
		albums: []ytmusic.AlbumRef{
			{Name: "Random Access Memories", ID: "MPREb_ram", Kind: "Album", Year: "2013", Artists: []ytmusic.Artist{{Name: "Daft Punk"}}},
			{Name: "Discovery", ID: "MPREb_disc", Kind: "Album", Year: "2001", Artists: []ytmusic.Artist{{Name: "Daft Punk"}}},
		},
		artists: []ytmusic.ArtistRef{{Name: "Daft Punk", ID: "UCdp", Audience: "65.9M monthly audience"}},
		pages:   map[string][]ytmusic.Track{"PLnight": ts, "PLfocus": ts[:2]},
		search: []ytmusic.SearchResult{
			{Type: ytmusic.TypeArtist, Top: true, Artist: &ytmusic.ArtistRef{Name: "Daft Punk", ID: "UCdp", Audience: "65.9M monthly audience"}},
			{Type: ytmusic.TypeSong, Track: &ts[0]},
			{Type: ytmusic.TypeSong, Track: &ts[1]},
			{Type: ytmusic.TypeVideo, Track: &ytmusic.Track{VideoID: "v1", Title: "Digital Love", Artists: ts[0].Artists, Duration: 299 * time.Second, IsVideo: true, Available: true}},
			{Type: ytmusic.TypeAlbum, Album: &ytmusic.AlbumRef{Name: "Discovery", ID: "MPREb_disc", Kind: "Album", Year: "2001"}},
			{Type: ytmusic.TypePlaylist, Playlist: &ytmusic.PlaylistRef{Title: "This Is Daft Punk", ID: "PLthis", Author: "YouTube Music"}},
		},
		suggest: map[string][]string{"daft": {"daft punk", "daft punk get lucky"}},
	}
}

func (f *fakeAPI) rec(method, arg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method+":"+arg)
	return f.errs[method]
}

func (f *fakeAPI) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeAPI) Search(_ context.Context, q string, flt ytmusic.SearchFilter, _ int) (ytmusic.SearchResults, error) {
	if err := f.rec("Search", q+"|"+string(flt)); err != nil {
		return ytmusic.SearchResults{}, err
	}
	if q == "nothing" {
		return ytmusic.SearchResults{}, nil
	}
	var out []ytmusic.SearchResult
	for _, r := range f.search {
		if flt == "" || (flt == ytmusic.FilterSongs && r.Type == ytmusic.TypeSong) {
			out = append(out, r)
		}
	}
	return ytmusic.SearchResults{Items: out}, nil
}

func (f *fakeAPI) SearchSuggestions(_ context.Context, q string) ([]string, error) {
	if err := f.rec("SearchSuggestions", q); err != nil {
		return nil, err
	}
	return f.suggest[q], nil
}

func (f *fakeAPI) Account(context.Context) (ytmusic.AccountInfo, error) {
	return ytmusic.AccountInfo{}, f.rec("Account", "")
}

func (f *fakeAPI) Album(_ context.Context, id string) (ytmusic.Album, error) {
	if err := f.rec("Album", id); err != nil {
		return ytmusic.Album{}, err
	}
	ts := scenarioTracks()[:4]
	return ytmusic.Album{
		AlbumRef:   ytmusic.AlbumRef{Name: "Random Access Memories", ID: id, Kind: "Album", Year: "2013", Artists: ts[0].Artists, PlaylistID: "OLAK5uy_ram"},
		TrackCount: len(ts), Duration: "26 minutes", Tracks: ts,
	}, nil
}

func (f *fakeAPI) Playlist(_ context.Context, id string) (ytmusic.Playlist, error) {
	if err := f.rec("Playlist", id); err != nil {
		return ytmusic.Playlist{}, err
	}
	all := f.pages[id]
	first, cont := all, ""
	if len(all) > f.pageSize {
		first, cont = all[:f.pageSize], fmt.Sprintf("%s@%d", id, f.pageSize)
	}
	return ytmusic.Playlist{
		PlaylistRef: ytmusic.PlaylistRef{Title: "Late night drives", ID: id, Author: "Text 1", Owned: true},
		Privacy:     ytmusic.PrivacyPrivate, TrackCount: len(all), Duration: "43 minutes",
		Tracks: first, Continuation: cont,
	}, nil
}

func (f *fakeAPI) PlaylistMore(_ context.Context, token string) ([]ytmusic.Track, string, error) {
	if err := f.rec("PlaylistMore", token); err != nil {
		return nil, "", err
	}
	id, at, _ := strings.Cut(token, "@")
	var n int
	_, _ = fmt.Sscan(at, &n)
	all := f.pages[id]
	end := min(n+f.pageSize, len(all))
	next := ""
	if end < len(all) {
		next = fmt.Sprintf("%s@%d", id, end)
	}
	return all[n:end], next, nil
}

func (f *fakeAPI) Artist(_ context.Context, id string) (ytmusic.ArtistPage, error) {
	if err := f.rec("Artist", id); err != nil {
		return ytmusic.ArtistPage{}, err
	}
	ts := scenarioTracks()
	return ytmusic.ArtistPage{
		ArtistRef: ytmusic.ArtistRef{Name: "Daft Punk", ID: id, Audience: "65.9M monthly audience"},
		TopSongs:  ytmusic.Shelf[ytmusic.Track]{Items: ts[:3]},
		Albums:    ytmusic.Shelf[ytmusic.AlbumRef]{Items: f.albums[:1], BrowseID: "MPAD" + id, Params: "p"},
		Singles:   ytmusic.Shelf[ytmusic.AlbumRef]{Items: []ytmusic.AlbumRef{{Name: "Get Lucky (Radio Edit)", ID: "MPREb_gl", Kind: "Single", Year: "2013"}}},
	}, nil
}

func (f *fakeAPI) ArtistAlbums(_ context.Context, browseID, _ string, _ int) ([]ytmusic.AlbumRef, error) {
	if err := f.rec("ArtistAlbums", browseID); err != nil {
		return nil, err
	}
	return f.albums, nil
}

func (f *fakeAPI) LibraryPlaylists(context.Context, int) ([]ytmusic.PlaylistRef, error) {
	return f.playlists, f.rec("LibraryPlaylists", "")
}

func (f *fakeAPI) LibraryAlbums(context.Context, int) ([]ytmusic.AlbumRef, error) {
	return f.albums, f.rec("LibraryAlbums", "")
}

func (f *fakeAPI) LibraryArtists(context.Context, int) ([]ytmusic.ArtistRef, error) {
	return f.artists, f.rec("LibraryArtists", "")
}

func (f *fakeAPI) edit(s string) {
	f.mu.Lock()
	f.edits = append(f.edits, s)
	f.mu.Unlock()
}

func (f *fakeAPI) CreatePlaylist(_ context.Context, title, _, privacy string, ids []string) (string, error) {
	if err := f.rec("CreatePlaylist", title); err != nil {
		return "", err
	}
	f.edit(fmt.Sprintf("create %s %s %v", title, privacy, ids))
	return "PLnew", nil
}

func (f *fakeAPI) DeletePlaylist(_ context.Context, id string) error {
	if err := f.rec("DeletePlaylist", id); err != nil {
		return err
	}
	f.edit("delete " + id)
	return nil
}

func (f *fakeAPI) AddPlaylistItems(_ context.Context, id string, ids []string, dup bool) error {
	if err := f.rec("AddPlaylistItems", id); err != nil {
		return err
	}
	f.edit(fmt.Sprintf("add %s %v dup=%v", id, ids, dup))
	return nil
}

func (f *fakeAPI) RemovePlaylistItems(_ context.Context, id string, ts []ytmusic.Track) error {
	if err := f.rec("RemovePlaylistItems", id); err != nil {
		return err
	}
	var ids []string
	for _, t := range ts {
		ids = append(ids, t.VideoID)
	}
	f.edit(fmt.Sprintf("remove %s %v", id, ids))
	return nil
}

func (f *fakeAPI) Rate(_ context.Context, id string, s ytmusic.LikeStatus) error {
	if err := f.rec("Rate", id); err != nil {
		return err
	}
	f.edit("rate " + id + " " + s.String())
	return nil
}

var _ ytmusic.Client = (*fakeAPI)(nil)

func errUnauth() error { return ytmusic.ErrUnauthenticated }
