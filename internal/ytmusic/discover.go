package ytmusic

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Discovery calls (M6): watch playlists (radio, up next), home, charts,
// explore, history and play reporting. Ported from ytmusicapi at the
// pinned commit: mixins/watch.py get_watch_playlist, parsers/watch.py;
// mixins/browsing.py get_home, get_song, parsers/browsing.py
// parse_mixed_content; mixins/charts.py get_charts; mixins/explore.py
// get_explore; mixins/library.py get_history, add_history_item.

// WatchRequest asks for a watch playlist: the queue YouTube Music builds
// when a track or playlist is played. At least one of VideoID and
// PlaylistID is required.
type WatchRequest struct {
	VideoID    string
	PlaylistID string // with VideoID "", plays the playlist; "RDAMPL<id>" = playlist radio
	Radio      bool   // a radio (different each time) instead of a fixed queue
	Shuffle    bool   // shuffled playlist (ignored with Radio)
	Limit      int    // minimum tracks to fetch; <= 0 means 25
}

// WatchPlaylist is a watch queue. LyricsID and RelatedID are browse ids for
// the track's lyrics and related pages ("" when YouTube offers none).
type WatchPlaylist struct {
	Tracks     []Track `json:"tracks"`
	PlaylistID string  `json:"playlist_id,omitempty"`
	LyricsID   string  `json:"lyrics_id,omitempty"`
	RelatedID  string  `json:"related_id,omitempty"`
}

// HomeShelf is one row of the home feed ("Listen again", "Mixed for you").
// Items hold songs, albums, artists and playlists (radio mixes are
// playlists whose ID is a watch playlist id such as "RDAMPL…").
type HomeShelf struct {
	Title string         `json:"title"`
	Items []SearchResult `json:"items"`
}

// Charts is the charts page for one region.
type Charts struct {
	Country   string        `json:"country"`   // as displayed, e.g. "United States"
	Playlists []PlaylistRef `json:"playlists"` // chart playlists in page order (videos, daily, weekly, genres)
	Artists   []ArtistRef   `json:"artists"`
}

// Explore is the explore page: new releases and the trending chart.
type Explore struct {
	NewReleases []AlbumRef   `json:"new_releases"`
	Trending    Shelf[Track] `json:"trending"` // BrowseID: the chart's playlist id (no "VL")
	NewVideos   []Track      `json:"new_videos"`
}

// HistoryItem is a play in the account history. Played is the shelf it was
// listed under, as displayed ("Today", "Yesterday", "Last week").
type HistoryItem struct {
	Track
	Played string `json:"played"`
}

const (
	ppvr  = "playlistPanelVideoRenderer"
	ppvwr = "playlistPanelVideoWrapperRenderer"
)

// videoIDRE matches a YouTube video id (same rule as the stream resolver).
var videoIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// WatchPlaylist implements Client.
func (a *API) WatchPlaylist(ctx context.Context, r WatchRequest) (WatchPlaylist, error) {
	if r.VideoID == "" && r.PlaylistID == "" {
		return WatchPlaylist{}, fmt.Errorf("%w: watch playlist needs a video or playlist id", ErrInvalidID)
	}
	limit := r.Limit
	if limit <= 0 {
		limit = 25
	}
	body := map[string]any{
		"enablePersistentPlaylistPanel": true,
		"isAudioOnly":                   true,
		"tunerSettingValue":             "AUTOMIX_SETTING_NORMAL",
	}
	pl := strings.TrimPrefix(r.PlaylistID, "VL")
	if r.VideoID != "" {
		body["videoId"] = r.VideoID
		if pl == "" {
			pl = "RDAMVM" + r.VideoID
		}
		if !r.Radio && !r.Shuffle {
			body["watchEndpointMusicSupportedConfigs"] = map[string]any{"watchEndpointMusicConfig": map[string]any{
				"hasPersistentPlaylistPanel": true, "musicVideoType": videoTypeATV,
			}}
		}
	}
	body["playlistId"] = pl
	if r.Shuffle {
		body["params"] = "wAEB8gECKAE%3D"
	}
	if r.Radio {
		body["params"] = "wAEB"
	}
	resp, err := a.read(ctx, "next", body, "")
	if err != nil {
		return WatchPlaylist{}, err
	}
	var p parser
	wn := resp.get("contents", "singleColumnMusicWatchNextResultsRenderer", "tabbedRenderer", "watchNextTabbedResultsRenderer")
	out := WatchPlaylist{}
	for _, tab := range wn.get("tabs").list() {
		be := tab.get("tabRenderer", "endpoint", "browseEndpoint")
		switch pageType(be) {
		case "MUSIC_PAGE_TYPE_TRACK_LYRICS":
			out.LyricsID = be.get("browseId").str()
		case "MUSIC_PAGE_TYPE_TRACK_RELATED":
			out.RelatedID = be.get("browseId").str()
		}
	}
	panel := wn.get("tabs", 0, "tabRenderer", "content", "musicQueueRenderer", "content", "playlistPanelRenderer")
	if !panel.exists() {
		return WatchPlaylist{}, fmt.Errorf("ytmusic: watch playlist %q: no queue in the response (private playlist?)", pl)
	}
	out.PlaylistID = panel.get("playlistId").str()
	out.Tracks = p.parseWatchItems(panel.get("contents").list())
	// Radios page with "nextRadioContinuationData", playlists with
	// "nextContinuationData" (get_continuations ctoken_path).
	isPlaylist := strings.HasPrefix(pl, "PL") || strings.HasPrefix(pl, "OLA")
	for len(out.Tracks) < limit {
		tok := panel.get("continuations", 0, "nextRadioContinuationData", "continuation").str()
		if isPlaylist || tok == "" {
			tok = nextContinuation(panel)
		}
		if tok == "" {
			break
		}
		resp, err := a.read(ctx, "next", body, ctokenQuery(tok))
		if err != nil {
			return WatchPlaylist{}, err
		}
		panel = resp.get("continuationContents", "playlistPanelContinuation")
		page := p.parseWatchItems(panel.get("contents").list())
		if len(page) == 0 {
			break
		}
		out.Tracks = append(out.Tracks, page...)
	}
	a.logShapes("watch playlist", p)
	return out, nil
}

// parseWatchItems ports parsers/watch.py parse_watch_playlist; the
// song/video counterpart of wrapped items is dropped.
func (p *parser) parseWatchItems(items []node) []Track {
	var out []Track
	for _, it := range items {
		if w := it.get(ppvwr); w.exists() {
			it = w.get("primaryRenderer")
		}
		d := it.get(ppvr)
		if !d.exists() {
			if k := it.firstKey(); k != "automixPreviewVideoRenderer" && k != "" {
				p.miss("watch item %q", k)
			}
			continue
		}
		if d.has("unplayableText") {
			continue
		}
		watch := d.get("navigationEndpoint", "watchEndpoint")
		t := Track{
			VideoID:      d.get("videoId").str(),
			Title:        d.get("title").runsText(),
			ThumbnailURL: largestThumbnail(d.get("thumbnail", "thumbnails")),
			VideoType:    musicVideoType(watch),
			Available:    true,
			SetVideoID:   d.get("playlistSetVideoId").str(),
		}
		t.IsVideo = t.VideoType != "" && t.VideoType != videoTypeATV
		if s, ok := parseDuration(d.get("lengthText").runsText()); ok {
			t.Duration = time.Duration(s) * time.Second
		}
		for _, m := range d.get("menu", "menuRenderer", "items").list() {
			if le := m.get("toggleMenuServiceItemRenderer", "defaultServiceEndpoint", "likeEndpoint"); le.exists() {
				// The default action is what a press does: "LIKE" means not liked yet.
				if le.get("status").str() == "LIKE" {
					t.LikeStatus = LikeIndifferent
				} else {
					t.LikeStatus = Like
				}
			}
		}
		sr := parseSongRuns(d.get("longBylineText", "runs").list())
		t.Artists, t.Album, t.Views = sr.artists, sr.album, sr.views
		if t.VideoID == "" {
			p.miss("watch item without videoId: %q", t.Title)
			continue
		}
		out = append(out, t)
	}
	return out
}

// Radio returns a radio mix seeded by videoIDs: one radio per seed, merged
// without duplicates and cut to limit (ytm-player services/ytmusic.py
// get_radio). A single seed keeps YouTube's order (the seed leads); several
// seeds are shuffled. Failed seeds are skipped; the error is returned only
// when every seed failed.
func Radio(ctx context.Context, c interface {
	WatchPlaylist(context.Context, WatchRequest) (WatchPlaylist, error)
}, videoIDs []string, limit int) ([]Track, error) {
	if limit <= 0 {
		limit = 25
	}
	type res struct {
		i  int
		ts []Track
		e  error
	}
	ch := make(chan res, len(videoIDs))
	for i, id := range videoIDs {
		go func() {
			w, err := c.WatchPlaylist(ctx, WatchRequest{VideoID: id, Radio: true, Limit: limit})
			ch <- res{i, w.Tracks, err}
		}()
	}
	per := make([][]Track, len(videoIDs))
	var lastErr error
	for range videoIDs {
		r := <-ch
		per[r.i] = r.ts
		if r.e != nil {
			lastErr = r.e
		}
	}
	seen := map[string]bool{}
	var pool []Track
	for _, ts := range per {
		for _, t := range ts {
			if !seen[t.VideoID] {
				seen[t.VideoID] = true
				pool = append(pool, t)
			}
		}
	}
	if len(pool) == 0 && lastErr != nil {
		return nil, lastErr
	}
	if len(videoIDs) > 1 {
		rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] }) //nolint:gosec // not security
	}
	return capLen(pool, limit), nil
}

// Home implements Client: the first page of the home feed plus
// continuation pages until at least shelves rows are loaded.
func (a *API) Home(ctx context.Context, shelves int) ([]HomeShelf, error) {
	body := map[string]any{"browseId": "FEmusic_home"}
	resp, err := a.read(ctx, "browse", body, "")
	if err != nil {
		return nil, err
	}
	var p parser
	list := resp.get("contents", "singleColumnBrowseResultsRenderer", "tabs", 0, "tabRenderer", "content", "sectionListRenderer")
	out := p.parseMixed(list.get("contents").list())
	for len(out) < shelves {
		tok := nextContinuation(list)
		if tok == "" {
			break
		}
		resp, err := a.read(ctx, "browse", body, ctokenQuery(tok))
		if err != nil {
			return nil, err
		}
		list = resp.get("continuationContents", "sectionListContinuation")
		page := p.parseMixed(list.get("contents").list())
		if len(page) == 0 {
			break
		}
		out = append(out, page...)
	}
	a.logShapes("home", p)
	return out, nil
}

// parseMixed ports parsers/browsing.py parse_mixed_content (carousels of
// two-row cards and flat song rows). Shelves left empty are dropped.
func (p *parser) parseMixed(rows []node) []HomeShelf {
	var out []HomeShelf
	for _, row := range rows {
		shelf := row.get("musicCarouselShelfRenderer")
		if !shelf.exists() {
			if k := row.firstKey(); k != "musicTastebuilderShelfRenderer" && k != "musicDescriptionShelfRenderer" {
				p.miss("home row %q", k)
			}
			continue
		}
		hdr := shelf.get("header", "musicCarouselShelfBasicHeaderRenderer")
		hs := HomeShelf{Title: firstNonEmpty(hdr.get("title").runsText(), hdr.get("strapline").runsText())}
		for _, it := range shelf.get("contents").list() {
			if r, ok := p.parseCard(it); ok {
				hs.Items = append(hs.Items, r)
			}
		}
		if len(hs.Items) > 0 {
			out = append(out, hs)
		}
	}
	return out
}

// parseCard parses one home/explore card by its navigation target.
func (p *parser) parseCard(it node) (SearchResult, bool) {
	if d := it.get(mrlir); d.exists() {
		t, ok := p.parseFlatSong(d)
		if !ok {
			return SearchResult{}, false
		}
		typ := TypeSong
		if t.IsVideo {
			typ = TypeVideo
		}
		return SearchResult{Type: typ, Track: &t}, true
	}
	d := it.get(mtrir)
	if !d.exists() {
		p.miss("card %q", it.firstKey())
		return SearchResult{}, false
	}
	nav := d.get("title", "runs", 0, "navigationEndpoint")
	switch pt := pageType(nav.get("browseEndpoint")); pt {
	case "":
		if pl := d.get("navigationEndpoint", "watchPlaylistEndpoint", "playlistId").str(); pl != "" {
			return SearchResult{Type: TypePlaylist, Playlist: &PlaylistRef{
				Title: d.get("title").runsText(), ID: pl, Author: d.get("subtitle").runsText(), ThumbnailURL: twoRowThumb(d),
			}}, true
		}
		watch := d.get("navigationEndpoint", "watchEndpoint")
		if watch.get("videoId").str() == "" {
			return SearchResult{}, false // deleted upload in "Listen again"
		}
		vs := p.parseTwoRowVideos([]node{it})
		if len(vs) == 0 {
			return SearchResult{}, false
		}
		t := vs[0]
		// Song cards carry "Song • Artist • Album" style subtitles; drop
		// the unlinked type label (parse_song_runs skip_type_spec).
		runs := d.get("subtitle", "runs").list()
		if len(runs) >= 3 && !runs[0].has("navigationEndpoint") && isSeparator(runs[1]) {
			switch runs[0].get("text").str() {
			case "Song", "Video", "Episode":
				runs = runs[2:]
			}
		}
		sr := parseSongRuns(runs)
		if len(sr.artists) > 0 {
			t.Artists, t.Album = sr.artists, sr.album
		}
		typ := TypeSong
		if t.IsVideo {
			typ = TypeVideo
		}
		return SearchResult{Type: typ, Track: &t}, true
	case "MUSIC_PAGE_TYPE_ALBUM", "MUSIC_PAGE_TYPE_AUDIOBOOK":
		al := p.parseTwoRowAlbums([]node{it})
		if len(al) == 0 {
			return SearchResult{}, false
		}
		return SearchResult{Type: TypeAlbum, Album: &al[0]}, true
	case "MUSIC_PAGE_TYPE_ARTIST", "MUSIC_PAGE_TYPE_USER_CHANNEL":
		return SearchResult{Type: TypeArtist, Artist: &ArtistRef{
			Name: d.get("title").runsText(), ID: nav.get("browseEndpoint", "browseId").str(),
			Audience: d.get("subtitle").runsText(), ThumbnailURL: twoRowThumb(d),
		}}, true
	case "MUSIC_PAGE_TYPE_PLAYLIST":
		pls := p.parseTwoRowPlaylists([]node{it})
		if len(pls) == 0 {
			return SearchResult{}, false
		}
		return SearchResult{Type: TypePlaylist, Playlist: &pls[0]}, true
	default:
		return SearchResult{}, false // podcasts, episodes
	}
}

// parseFlatSong ports parsers/browsing.py parse_song_flat: title (with the
// watch target) in column 0, "Artist • plays" in column 1, album in 2.
func (p *parser) parseFlatSong(d node) (Track, bool) {
	play := d.get("overlay", "musicItemThumbnailOverlayRenderer", "content", "musicPlayButtonRenderer", "playNavigationEndpoint")
	title := flexItem(d, 0).get("text", "runs", 0)
	t := Track{
		Title:        title.get("text").str(),
		VideoID:      firstNonEmpty(title.get("navigationEndpoint", "watchEndpoint", "videoId").str(), play.get("watchEndpoint", "videoId").str()),
		VideoType:    musicVideoType(play.get("watchEndpoint")),
		ThumbnailURL: thumbs(d),
		Explicit:     hasExplicitBadge(d.get("badges")),
		Available:    d.get("musicItemRendererDisplayPolicy").str() != "MUSIC_ITEM_RENDERER_DISPLAY_POLICY_GREY_OUT",
	}
	t.IsVideo = t.VideoType != "" && t.VideoType != videoTypeATV
	sr := parseSongRuns(flexRuns(d, 1))
	t.Artists, t.Album, t.Views, t.Duration = sr.artists, sr.album, sr.views, sr.duration
	if al := flexItem(d, 2).get("text", "runs", 0); al.get("navigationEndpoint").exists() {
		t.Album = &AlbumRef{Name: al.get("text").str(), ID: al.get("navigationEndpoint", "browseEndpoint", "browseId").str()}
	}
	if t.VideoID == "" {
		p.miss("flat song without videoId: %q", t.Title)
		return Track{}, false
	}
	return t, true
}

// Charts implements Client. country is an ISO 3166-1 alpha-2 code; "" or
// "ZZ" is the global chart.
func (a *API) Charts(ctx context.Context, country string) (Charts, error) {
	country = strings.ToUpper(strings.TrimSpace(country))
	if country == "" {
		country = "ZZ"
	}
	body := map[string]any{"browseId": "FEmusic_charts", "formData": map[string]any{"selectedValues": []string{country}}}
	resp, err := a.read(ctx, "browse", body, "")
	if err != nil {
		return Charts{}, err
	}
	var p parser
	secs := singleColumnSections(resp)
	out := Charts{Country: first(secs).get("musicShelfRenderer", "subheaders", 0, "musicSideAlignedItemRenderer", "startItems", 0,
		"musicSortFilterButtonRenderer", "title").runsText()}
	// Carousels carry no identity: playlists are two-row cards, artists
	// are list rows linking to a channel (charts.py is_playlist_carousel).
	for _, sec := range secs[min(1, len(secs)):] {
		items := sec.get("musicCarouselShelfRenderer", "contents").list()
		if len(items) == 0 {
			continue
		}
		switch {
		case items[0].get(mtrir).exists():
			out.Playlists = append(out.Playlists, p.parseTwoRowPlaylists(items)...)
		case out.Artists == nil && strings.HasPrefix(items[0].get(mrlir, "navigationEndpoint", "browseEndpoint", "browseId").str(), "UC"):
			out.Artists = p.parseLibraryArtists(items)
		}
	}
	a.logShapes("charts", p)
	return out, nil
}

// Explore implements Client.
func (a *API) Explore(ctx context.Context) (Explore, error) {
	resp, err := a.read(ctx, "browse", map[string]any{"browseId": "FEmusic_explore"}, "")
	if err != nil {
		return Explore{}, err
	}
	var p parser
	var out Explore
	for _, sec := range singleColumnSections(resp) {
		shelf := sec.get("musicCarouselShelfRenderer")
		id := shelf.get("header", "musicCarouselShelfBasicHeaderRenderer", "title", "runs", 0, "navigationEndpoint", "browseEndpoint", "browseId").str()
		items := shelf.get("contents").list()
		switch {
		case id == "FEmusic_new_releases_albums":
			out.NewReleases = p.parseTwoRowAlbums(items)
		case id == "FEmusic_new_releases_videos":
			out.NewVideos = p.parseTwoRowVideos(items)
		case strings.HasPrefix(id, "VLOLA"):
			out.Trending.BrowseID = id[2:]
			for _, it := range items {
				if t, ok := p.parseFlatSong(it.get(mrlir)); ok {
					out.Trending.Items = append(out.Trending.Items, t)
				}
			}
		}
	}
	a.logShapes("explore", p)
	return out, nil
}

// History implements Client. It needs a signed-in session.
func (a *API) History(ctx context.Context) ([]HistoryItem, error) {
	if a.credentials() == nil {
		return nil, ErrUnauthenticated
	}
	resp, err := a.read(ctx, "browse", map[string]any{"browseId": "FEmusic_history"}, "")
	if err != nil {
		return nil, err
	}
	var p parser
	var out []HistoryItem
	for _, sec := range singleColumnSections(resp) {
		shelf := sec.get("musicShelfRenderer")
		if !shelf.exists() {
			// Signed out: a "Sign in to view your history" message.
			if sec.get("itemSectionRenderer", "contents", 0, "messageRenderer").exists() {
				return nil, ErrUnauthenticated
			}
			if msg := sec.get("musicNotifierShelfRenderer", "title").runsText(); msg != "" {
				return nil, fmt.Errorf("ytmusic: history: %s", msg)
			}
			continue
		}
		played := shelf.get("title").runsText()
		for _, t := range p.parsePlaylistItems(shelf.get("contents").list(), false) {
			out = append(out, HistoryItem{Track: t, Played: played})
		}
	}
	a.logShapes("history", p)
	return out, nil
}

// AddHistoryItem implements Client: it asks the player endpoint for the
// track's playback-tracking URL and pings it, which is how YouTube Music
// records a play (ytmusicapi add_history_item: 204 = recorded).
func (a *API) AddHistoryItem(ctx context.Context, videoID string) error {
	creds := a.credentials()
	if creds == nil {
		return ErrUnauthenticated
	}
	if !videoIDRE.MatchString(videoID) {
		return fmt.Errorf("%w: %q", ErrInvalidID, videoID)
	}
	days := int(a.o.Clock.Now().Unix()/86400) - 1 // signatureTimestamp, as get_song
	resp, err := a.read(ctx, "player", map[string]any{
		"playbackContext": map[string]any{"contentPlaybackContext": map[string]any{"signatureTimestamp": days}},
		"video_id":        videoID,
	}, "")
	if err != nil {
		return err
	}
	base := resp.get("playbackTracking", "videostatsPlaybackUrl", "baseUrl").str()
	if base == "" {
		return fmt.Errorf("ytmusic: %s has no playback tracking (unplayable?)", videoID)
	}
	u, err := url.Parse(base)
	// The URL comes from the response; cookies only go to YouTube.
	if err != nil || u.Scheme != "https" || (u.Hostname() != "youtube.com" && !strings.HasSuffix(u.Hostname(), ".youtube.com")) {
		return fmt.Errorf("ytmusic: unexpected playback tracking URL host for %s", videoID)
	}
	q := u.Query()
	q.Set("ver", "2")
	q.Set("c", "WEB_REMIX")
	q.Set("cpn", cpn())
	u.RawQuery = q.Encode()

	ctx, cancel := context.WithTimeout(ctx, a.o.Timeout)
	defer cancel()
	if err := a.lim.wait(ctx); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("ytmusic: history ping: %w", err)
	}
	if err := a.setHeaders(req.Header, creds); err != nil {
		return err
	}
	res, err := a.o.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("ytmusic: history ping: %w", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	_ = res.Body.Close()
	a.o.Logger.Debug("ytmusic: history ping", "id", videoID, "status", res.StatusCode)
	if res.StatusCode != http.StatusNoContent {
		return &HTTPError{Endpoint: "playback tracking", Status: res.StatusCode}
	}
	return nil
}

// cpn is the 16-character client playback nonce (add_history_item).
func cpn() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
	b := make([]byte, 16)
	for i := range b {
		b[i] = alphabet[rand.IntN(len(alphabet))] //nolint:gosec // nonce, not a secret
	}
	return string(b)
}
