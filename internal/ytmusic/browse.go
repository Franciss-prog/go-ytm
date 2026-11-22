package ytmusic

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Browse pages: album, playlist, artist, artist discography and the
// signed-in library. Paths and column rules are ported from ytmusicapi
// (pinned commit, see the package doc): mixins/browsing.py get_album,
// get_artist, get_artist_albums; mixins/playlists.py get_playlist;
// mixins/library.py; parsers/albums.py, parsers/playlists.py,
// parsers/browsing.py, parsers/library.py; continuations.py.

const mtrir = "musicTwoRowItemRenderer"

// ErrInvalidID means an id has the wrong form for the call.
var ErrInvalidID = errors.New("ytmusic: invalid id")

// --- shared paths -----------------------------------------------------------

func twoColumn(resp node) node { return resp.get("contents", "twoColumnBrowseResultsRenderer") }

func singleColumnSections(resp node) []node {
	return resp.get("contents", "singleColumnBrowseResultsRenderer", "tabs", 0, "tabRenderer", "content", "sectionListRenderer", "contents").list()
}

func thumbs(n node) string {
	return largestThumbnail(n.get("thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails"))
}

func twoRowThumb(n node) string {
	return largestThumbnail(n.get("thumbnailRenderer", "musicThumbnailRenderer", "thumbnail", "thumbnails"))
}

// artistsFromRuns reads "A, B & C" runs: every even run is an artist
// (parsers/songs.py parse_artists_runs).
func artistsFromRuns(runs []node) []Artist {
	var out []Artist
	for i := 0; i < len(runs); i += 2 {
		name := runs[i].get("text").str()
		if name == "" {
			continue
		}
		out = append(out, Artist{Name: name, ID: runs[i].get("navigationEndpoint", "browseEndpoint", "browseId").str()})
	}
	return out
}

func parseLikeStatus(s string) LikeStatus {
	switch s {
	case "LIKE":
		return Like
	case "DISLIKE":
		return Dislike
	case "INDIFFERENT":
		return LikeIndifferent
	}
	return LikeUnknown
}

var digitsRE = regexp.MustCompile(`\d+`)

// countIn returns the number in "1,204 songs" (0 if none).
func countIn(s string) int {
	n, _ := strconv.Atoi(strings.Join(digitsRE.FindAllString(s, -1), ""))
	return n
}

// continuationToken returns the 2025-style token on the last item of a
// list (continuations.py get_continuation_token).
func continuationToken(items []node) string {
	if len(items) == 0 {
		return ""
	}
	last := items[len(items)-1]
	if t := last.get("continuationItemRenderer", "continuationEndpoint", "continuationCommand", "token").str(); t != "" {
		return t
	}
	for _, c := range last.get("continuationItemRenderer", "continuationEndpoint", "commandExecutorCommand", "commands").list() {
		if c.get("continuationCommand", "request").str() == "CONTINUATION_REQUEST_TYPE_BROWSE" {
			return c.get("continuationCommand", "token").str()
		}
	}
	return ""
}

// nextContinuation is the older "continuations" token on grids and shelves.
func nextContinuation(n node) string {
	return n.get("continuations", 0, "nextContinuationData", "continuation").str()
}

func ctokenQuery(tok string) string { return "&ctoken=" + tok + "&continuation=" + tok + "&type=next" }

// --- track rows -------------------------------------------------------------

// parsePlaylistItems parses musicResponsiveListItemRenderer rows, skipping
// other entries (the continuation item) and deleted songs.
func (p *parser) parsePlaylistItems(items []node, isAlbum bool) []Track {
	var out []Track
	for _, it := range items {
		data := it.get(mrlir)
		if !data.exists() {
			if k := it.firstKey(); k != "continuationItemRenderer" {
				p.miss("playlist item %q", k)
			}
			continue
		}
		if t, ok := p.parsePlaylistItem(data, isAlbum); ok {
			out = append(out, t)
		}
	}
	return out
}

func flexItem(data node, i int) node {
	return data.get("flexColumns", i, "musicResponsiveListItemFlexColumnRenderer")
}

// parsePlaylistItem ports parsers/playlists.py parse_playlist_item: the
// column holding each field is found from its navigation target.
func (p *parser) parsePlaylistItem(data node, isAlbum bool) (Track, bool) {
	t := Track{Available: data.get("musicItemRendererDisplayPolicy").str() != "MUSIC_ITEM_RENDERER_DISPLAY_POLICY_GREY_OUT"}
	for _, it := range data.get("menu", "menuRenderer", "items").list() {
		if pe := it.get("menuServiceItemRenderer", "serviceEndpoint", "playlistEditEndpoint"); pe.exists() {
			t.SetVideoID = pe.get("actions", 0, "setVideoId").str()
			t.VideoID = pe.get("actions", 0, "removedVideoId").str()
		}
	}
	play := data.get("overlay", "musicItemThumbnailOverlayRenderer", "content", "musicPlayButtonRenderer", "playNavigationEndpoint", "watchEndpoint")
	if id := play.get("videoId").str(); id != "" {
		t.VideoID = id
		t.LikeStatus = parseLikeStatus(data.get("menu", "menuRenderer", "topLevelButtons", 0, "likeButtonRenderer", "likeStatus").str())
	}
	if t.VideoID == "" {
		t.VideoID = data.get("playlistItemData", "videoId").str()
	}
	if t.SetVideoID == "" {
		t.SetVideoID = data.get("playlistItemData", "playlistSetVideoId").str()
	}

	preset := !t.Available || isAlbum
	titleIdx, artistIdx, albumIdx, durationIdx, unknownIdx := -1, -1, -1, -1, -1
	if preset {
		titleIdx, artistIdx, albumIdx = 0, 1, 2
	}
	var userIdx []int
	cols := data.get("flexColumns").list()
	for i := range cols {
		run := flexItem(data, i).get("text", "runs", 0)
		nav := run.get("navigationEndpoint")
		if !nav.exists() {
			if txt := run.get("text").str(); txt != "" {
				if _, ok := parseDuration(txt); ok {
					durationIdx = i
				} else if unknownIdx < 0 {
					unknownIdx = i
				}
			}
			continue
		}
		switch {
		case nav.has("watchEndpoint"):
			titleIdx = i
		case nav.has("browseEndpoint"):
			switch pageType(nav.get("browseEndpoint")) {
			case pageArtist, "MUSIC_PAGE_TYPE_UNKNOWN":
				artistIdx = i
			case pageAlbum, "MUSIC_PAGE_TYPE_AUDIOBOOK":
				albumIdx = i
			case pageUserChannel:
				userIdx = append(userIdx, i)
			case "MUSIC_PAGE_TYPE_NON_MUSIC_AUDIO_TRACK_PAGE":
				titleIdx = i
			}
		}
	}
	if artistIdx < 0 && unknownIdx >= 0 {
		artistIdx = unknownIdx
	}
	if artistIdx < 0 && len(userIdx) > 0 {
		artistIdx = userIdx[len(userIdx)-1]
	}
	if titleIdx >= 0 {
		t.Title = flexItem(data, titleIdx).get("text", "runs", 0, "text").str()
	}
	if t.Title == "Song deleted" {
		return Track{}, false
	}
	if artistIdx >= 0 {
		t.Artists = artistsFromRuns(flexItem(data, artistIdx).get("text", "runs").list())
	}
	if albumIdx >= 0 {
		if r := flexItem(data, albumIdx).get("text", "runs", 0); r.exists() {
			if id := r.get("navigationEndpoint", "browseEndpoint", "browseId").str(); id != "" || !isAlbum {
				t.Album = &AlbumRef{Name: r.get("text").str(), ID: id}
			}
		}
	}
	if isAlbum {
		t.Views = viewsCount(flexItem(data, 2).get("text", "runs", 0, "text").str())
	} else if unknownIdx >= 0 && unknownIdx != artistIdx {
		t.Views = viewsCount(flexItem(data, unknownIdx).get("text", "runs", 0, "text").str())
	}
	dur := ""
	if durationIdx > 0 {
		dur = flexItem(data, durationIdx).get("text", "runs", 0, "text").str()
	}
	if fc := data.get("fixedColumns", 0, "musicResponsiveListItemFixedColumnRenderer", "text"); fc.exists() {
		dur = fc.runsText()
	}
	if s, ok := parseDuration(dur); ok {
		t.Duration = time.Duration(s) * time.Second
	}
	t.ThumbnailURL = thumbs(data)
	t.Explicit = hasExplicitBadge(data.get("badges"))
	t.VideoType = musicVideoType(play)
	if t.VideoType == "" {
		t.VideoType = musicVideoType(data.get("menu", "menuRenderer", "items", 0, "menuNavigationItemRenderer", "navigationEndpoint", "watchEndpoint"))
	}
	t.IsVideo = t.VideoType != "" && t.VideoType != videoTypeATV
	if t.VideoID == "" {
		p.miss("playlist item without videoId: %q", t.Title)
	}
	return t, true
}

// --- album ------------------------------------------------------------------

// Album implements Client.
func (a *API) Album(ctx context.Context, browseID string) (Album, error) {
	if !strings.HasPrefix(browseID, "MPRE") {
		return Album{}, fmt.Errorf("%w: album browse id must start with MPRE: %q", ErrInvalidID, browseID)
	}
	resp, err := a.read(ctx, "browse", map[string]any{"browseId": browseID}, "")
	if err != nil {
		return Album{}, err
	}
	var p parser
	al := p.parseAlbum(resp, browseID)
	a.logShapes("album", p)
	return al, nil
}

func (p *parser) parseAlbum(resp node, browseID string) Album {
	tc := twoColumn(resp)
	h := tc.get("tabs", 0, "tabRenderer", "content", "sectionListRenderer", "contents", 0, "musicResponsiveHeaderRenderer")
	if !h.exists() {
		p.miss("album without musicResponsiveHeaderRenderer")
	}
	al := Album{AlbumRef: AlbumRef{
		Name:         h.get("title").runsText(),
		ID:           browseID,
		ThumbnailURL: thumbs(h),
		Explicit:     hasExplicitBadge(h.get("subtitleBadges")),
	}}
	sub := h.get("subtitle", "runs").list()
	if len(sub) > 0 {
		al.Kind = sub[0].get("text").str()
	}
	if len(sub) > 2 {
		al.Year = parseSongRuns(sub[2:]).year
	}
	al.Artists = artistsFromRuns(h.get("straplineTextOne", "runs").list())
	al.Description = h.get("description", "musicDescriptionShelfRenderer", "description").runsText()
	second := h.get("secondSubtitle", "runs").list()
	if len(second) > 1 {
		al.TrackCount = countIn(second[0].get("text").str())
		if len(second) > 2 {
			al.Duration = second[2].get("text").str()
		}
	} else if len(second) == 1 {
		al.Duration = second[0].get("text").str()
	}
	al.LikeStatus = LikeIndifferent
	for _, b := range h.get("buttons").list() {
		if pb := b.get("musicPlayButtonRenderer", "playNavigationEndpoint"); pb.exists() {
			al.PlaylistID = firstNonEmpty(pb.get("watchPlaylistEndpoint", "playlistId").str(), pb.get("watchEndpoint", "playlistId").str())
		}
		if svc := b.get("toggleButtonRenderer", "defaultServiceEndpoint", "likeEndpoint", "status"); svc.exists() {
			// The button's default action is the opposite of the current
			// status (parsers/_utils.py parse_like_status).
			if svc.str() == "LIKE" {
				al.LikeStatus = LikeIndifferent
			} else {
				al.LikeStatus = Like
			}
		}
	}
	shelf := tc.get("secondaryContents", "sectionListRenderer", "contents", 0, "musicShelfRenderer", "contents")
	al.Tracks = p.parsePlaylistItems(shelf.list(), true)
	ref := al.AlbumRef
	ref.Artists = nil
	for i := range al.Tracks {
		r := ref
		al.Tracks[i].Album = &r
		if len(al.Tracks[i].Artists) == 0 {
			al.Tracks[i].Artists = al.Artists
		}
		if al.Tracks[i].ThumbnailURL == "" {
			al.Tracks[i].ThumbnailURL = al.ThumbnailURL
		}
	}
	return al
}

// --- playlist ---------------------------------------------------------------

// Playlist implements Client.
func (a *API) Playlist(ctx context.Context, id string) (Playlist, error) {
	id = strings.TrimPrefix(id, "VL")
	if id == "" {
		return Playlist{}, fmt.Errorf("%w: empty playlist id", ErrInvalidID)
	}
	var resp node
	err := a.withRenew(ctx, func() error {
		var err error
		resp, err = a.call(ctx, "browse", map[string]any{"browseId": "VL" + id}, "", true)
		// Liked Music needs a session; a stale one gets an empty public
		// page (200, logged_in=0) instead of 401.
		if err == nil && id == "LM" && signedOut(resp) {
			err = ErrUnauthenticated
		}
		return err
	})
	if err != nil {
		return Playlist{}, err
	}
	var p parser
	pl := p.parsePlaylist(resp, id)
	a.logShapes("playlist", p)
	return pl, nil
}

// PlaylistMore implements Client.
func (a *API) PlaylistMore(ctx context.Context, token string) ([]Track, string, error) {
	if token == "" {
		return nil, "", nil
	}
	resp, err := a.read(ctx, "browse", map[string]any{"continuation": token}, "")
	if err != nil {
		return nil, "", err
	}
	var p parser
	items := resp.get("onResponseReceivedActions", 0, "appendContinuationItemsAction", "continuationItems").list()
	tracks := p.parsePlaylistItems(items, false)
	a.logShapes("playlist continuation", p)
	if len(tracks) == 0 {
		return nil, "", nil
	}
	return tracks, continuationToken(items), nil
}

func (p *parser) parsePlaylist(resp node, id string) Playlist {
	tc := twoColumn(resp)
	shelf := tc.get("secondaryContents", "sectionListRenderer", "contents", 0, "musicPlaylistShelfRenderer")
	items := shelf.get("contents").list()
	pl := Playlist{PlaylistRef: PlaylistRef{ID: id}, Privacy: PrivacyPublic}
	pl.Tracks = p.parsePlaylistItems(items, false)
	pl.Continuation = continuationToken(items)

	headerData := tc.get("tabs", 0, "tabRenderer", "content", "sectionListRenderer", "contents", 0)
	if !headerData.exists() {
		// Album audio playlists (OLAK5uy_…) have no header
		// (parsers/playlists.py parse_audio_playlist).
		if t := shelf.get("targetId").str(); t != "" {
			pl.ID = strings.TrimPrefix(t, "VL")
		}
		if len(pl.Tracks) > 0 && pl.Tracks[0].Album != nil {
			pl.Title = pl.Tracks[0].Album.Name
		}
		pl.TrackCount = len(pl.Tracks)
		return pl
	}
	var h node
	if ed := headerData.get("musicEditablePlaylistDetailHeaderRenderer"); ed.exists() {
		pl.Owned = true
		pl.ID = firstNonEmpty(ed.get("playlistId").str(), id)
		pl.Privacy = ed.get("editHeader", "musicPlaylistEditHeaderRenderer", "privacy").str()
		h = ed.get("header", "musicResponsiveHeaderRenderer")
	} else {
		h = headerData.get("musicResponsiveHeaderRenderer")
		for _, b := range h.get("buttons").list() {
			if pid := b.get("musicPlayButtonRenderer", "playNavigationEndpoint", "watchEndpoint", "playlistId").str(); pid != "" {
				pl.ID = pid
			}
		}
	}
	if !h.exists() {
		p.miss("playlist header %q", headerData.firstKey())
	}
	pl.Title = h.get("title").runsText()
	pl.ThumbnailURL = thumbs(h)
	pl.Description = h.get("description", "musicDescriptionShelfRenderer", "description").runsText()
	if fp := h.get("facepile", "avatarStackViewModel"); fp.exists() {
		cmd := fp.get("rendererContext", "commandContext", "onTap", "innertubeCommand")
		if cmd.get("showEngagementPanelEndpoint", "identifier", "tag").str() != "PAplaylist_collaborate" {
			pl.Author = fp.get("text", "content").str()
			pl.AuthorID = cmd.get("browseEndpoint", "browseId").str()
		}
	}
	// secondSubtitle: [views •] count [• duration] (parse_playlist_header_meta).
	if runs := h.get("secondSubtitle", "runs").list(); len(runs) > 0 {
		views := 0
		if len(runs) > 3 {
			views = 2
			pl.Views = runs[0].get("text").str()
		}
		if len(runs) > 1 && views+2 < len(runs) {
			pl.Duration = runs[views+2].get("text").str()
		}
		if views < len(runs) {
			pl.Count = runs[views].get("text").str()
			pl.TrackCount = countIn(pl.Count)
		}
	}
	sub := h.get("subtitle", "runs").list()
	skip := 2
	if pl.Owned {
		skip = 4
	}
	if len(sub) > skip {
		pl.Year = parseSongRuns(sub[skip:]).year
	}
	return pl
}

// --- artist -----------------------------------------------------------------

// Artist implements Client.
func (a *API) Artist(ctx context.Context, channelID string) (ArtistPage, error) {
	channelID = strings.TrimPrefix(channelID, "MPLA")
	if !strings.HasPrefix(channelID, "UC") {
		return ArtistPage{}, fmt.Errorf("%w: artist channel id must start with UC: %q", ErrInvalidID, channelID)
	}
	resp, err := a.read(ctx, "browse", map[string]any{"browseId": channelID}, "")
	if err != nil {
		return ArtistPage{}, err
	}
	var p parser
	ar := p.parseArtist(resp, channelID)
	a.logShapes("artist", p)
	return ar, nil
}

func (p *parser) parseArtist(resp node, channelID string) ArtistPage {
	h := resp.get("header", "musicImmersiveHeaderRenderer")
	if !h.exists() {
		h = resp.get("header", "musicVisualHeaderRenderer")
		p.miss("artist header %q", resp.get("header").firstKey())
	}
	ar := ArtistPage{ArtistRef: ArtistRef{Name: h.get("title").runsText(), ID: channelID, ThumbnailURL: thumbs(h)}}
	ar.Audience = strings.TrimSuffix(h.get("monthlyListenerCount").runsText(), " monthly audience")
	sb := h.get("subscriptionButton", "subscribeButtonRenderer")
	ar.Subscribed, _ = sb.get("subscribed").v.(bool)
	ar.Subscribers = sb.get("subscriberCountText").runsText()
	ar.ShuffleID = h.get("playButton", "buttonRenderer", "navigationEndpoint", "watchEndpoint", "playlistId").str()
	rb := h.get("startRadioButton", "buttonRenderer", "navigationEndpoint")
	ar.RadioID = firstNonEmpty(rb.get("watchEndpoint", "playlistId").str(), rb.get("watchPlaylistEndpoint", "playlistId").str())

	sections := singleColumnSections(resp)
	if sections == nil {
		sections = twoColumn(resp).get("tabs", 0, "tabRenderer", "content", "sectionListRenderer", "contents").list()
	}
	for _, sec := range sections {
		switch {
		case sec.has("musicDescriptionShelfRenderer"):
			ar.Description = sec.get("musicDescriptionShelfRenderer", "description").runsText()
		case sec.has("musicShelfRenderer"):
			sh := sec.get("musicShelfRenderer")
			ar.TopSongs.BrowseID = sh.get("title", "runs", 0, "navigationEndpoint", "browseEndpoint", "browseId").str()
			ar.TopSongs.Items = p.parsePlaylistItems(sh.get("contents").list(), false)
		case sec.has("musicCarouselShelfRenderer"):
			c := sec.get("musicCarouselShelfRenderer")
			title := c.get("header", "musicCarouselShelfBasicHeaderRenderer", "title", "runs", 0)
			be := title.get("navigationEndpoint", "browseEndpoint")
			items := c.get("contents").list()
			// Section titles are English (hl=en), as ytmusicapi's
			// parse_channel_contents matches them.
			switch strings.ToLower(title.get("text").str()) {
			case "albums":
				ar.Albums = Shelf[AlbumRef]{Items: p.parseTwoRowAlbums(items), BrowseID: be.get("browseId").str(), Params: be.get("params").str()}
				for i := range ar.Albums.Items {
					if ar.Albums.Items[i].Kind == "" {
						ar.Albums.Items[i].Kind = "Album" // cards here show only the year
					}
				}
			case "singles & eps", "singles":
				ar.Singles = Shelf[AlbumRef]{Items: p.parseTwoRowAlbums(items), BrowseID: be.get("browseId").str(), Params: be.get("params").str()}
			case "videos":
				ar.Videos = Shelf[Track]{Items: p.parseTwoRowVideos(items), BrowseID: be.get("browseId").str()}
			}
		}
	}
	return ar
}

// parseTwoRowAlbums parses album cards (parsers/browsing.py parse_album
// and parsers/library.py parse_albums).
func (p *parser) parseTwoRowAlbums(items []node) []AlbumRef {
	var out []AlbumRef
	for _, it := range items {
		d := it.get(mtrir)
		if !d.exists() {
			if k := it.firstKey(); k != "continuationItemRenderer" {
				p.miss("album card %q", k)
			}
			continue
		}
		al := AlbumRef{
			Name:         d.get("title").runsText(),
			ID:           d.get("title", "runs", 0, "navigationEndpoint", "browseEndpoint", "browseId").str(),
			ThumbnailURL: twoRowThumb(d),
			Explicit:     hasExplicitBadge(d.get("subtitleBadges")),
		}
		play := d.get("thumbnailOverlay", "musicItemThumbnailOverlayRenderer", "content", "musicPlayButtonRenderer", "playNavigationEndpoint")
		al.PlaylistID = firstNonEmpty(play.get("watchPlaylistEndpoint", "playlistId").str(), play.get("watchEndpoint", "playlistId").str(),
			d.get("menu", "menuRenderer", "items", 0, "menuNavigationItemRenderer", "navigationEndpoint", "watchPlaylistEndpoint", "playlistId").str())
		runs := d.get("subtitle", "runs").list()
		for _, r := range runs {
			if id := r.get("navigationEndpoint", "browseEndpoint", "browseId").str(); id != "" {
				al.Artists = append(al.Artists, Artist{Name: r.get("text").str(), ID: id})
			}
		}
		if len(runs) > 0 {
			first := runs[0].get("text").str()
			if yearRE.MatchString(first) {
				al.Year = first
			} else {
				al.Kind = first
				for _, r := range runs[1:] {
					if y := r.get("text").str(); yearRE.MatchString(y) {
						al.Year = y
					}
				}
			}
		}
		out = append(out, al)
	}
	return out
}

// parseTwoRowVideos parses video cards (parsers/browsing.py parse_video).
func (p *parser) parseTwoRowVideos(items []node) []Track {
	var out []Track
	for _, it := range items {
		d := it.get(mtrir)
		if !d.exists() {
			continue
		}
		watch := d.get("navigationEndpoint", "watchEndpoint")
		t := Track{
			Title:        d.get("title").runsText(),
			VideoID:      watch.get("videoId").str(),
			VideoType:    musicVideoType(watch),
			ThumbnailURL: twoRowThumb(d),
			Available:    true,
		}
		t.IsVideo = t.VideoType != videoTypeATV
		runs := d.get("subtitle", "runs").list()
		end := len(runs)
		for i, r := range runs {
			if isSeparator(r) {
				end = i
				break
			}
		}
		t.Artists = artistsFromRuns(runs[:end])
		if len(runs) > 0 {
			t.Views = viewsCount(runs[len(runs)-1].get("text").str())
		}
		if t.VideoID == "" {
			p.miss("video card without videoId: %q", t.Title)
			continue
		}
		out = append(out, t)
	}
	return out
}

// ArtistAlbums implements Client.
func (a *API) ArtistAlbums(ctx context.Context, browseID, params string, limit int) ([]AlbumRef, error) {
	body := map[string]any{"browseId": browseID, "params": params}
	resp, err := a.read(ctx, "browse", body, "")
	if err != nil {
		return nil, err
	}
	var p parser
	sec := first(singleColumnSections(resp))
	grid := sec.get("gridRenderer")
	items := grid.get("items").list()
	if !grid.exists() {
		items = sec.get("musicCarouselShelfRenderer", "contents").list()
	}
	out := p.parseTwoRowAlbums(items)
	tok := nextContinuation(grid)
	for tok != "" && (limit <= 0 || len(out) < limit) {
		resp, err := a.read(ctx, "browse", body, ctokenQuery(tok))
		if err != nil {
			return nil, err
		}
		g := resp.get("continuationContents", "gridContinuation")
		page := p.parseTwoRowAlbums(g.get("items").list())
		if len(page) == 0 {
			break
		}
		out = append(out, page...)
		tok = nextContinuation(g)
	}
	a.logShapes("artist albums", p)
	return capLen(out, limit), nil
}

func first(ns []node) node {
	if len(ns) == 0 {
		return node{}
	}
	return ns[0]
}

func capLen[T any](s []T, limit int) []T {
	if limit > 0 && len(s) > limit {
		return s[:limit]
	}
	return s
}

// --- library ----------------------------------------------------------------

// libraryContents finds the renderer holding a library list
// (parsers/library.py get_library_contents).
func libraryContents(resp node, renderer string) node {
	sections := singleColumnSections(resp)
	if sections == nil {
		tabs := resp.get("contents", "singleColumnBrowseResultsRenderer", "tabs").list()
		i := 1
		if len(tabs) >= 3 {
			i = 2
		}
		return resp.get("contents", "singleColumnBrowseResultsRenderer", "tabs", i, "tabRenderer", "content", "sectionListRenderer", "contents", 0, renderer)
	}
	for _, s := range sections {
		if s.has("itemSectionRenderer") {
			if r := s.get("itemSectionRenderer", "contents", 0, renderer); r.exists() {
				return r
			}
		}
		if r := s.get(renderer); r.exists() {
			return r
		}
	}
	return node{}
}

// library browses a library list and follows its continuations.
func (a *API) library(ctx context.Context, browseID, renderer, contKey, itemsKey string, limit int, parse func(*parser, []node) int) error {
	if a.credentials() == nil {
		return ErrUnauthenticated
	}
	body := map[string]any{"browseId": browseID}
	resp, err := a.read(ctx, "browse", body, "")
	if err != nil {
		return err
	}
	var p parser
	defer func() { a.logShapes(browseID, p) }()
	r := libraryContents(resp, renderer)
	n := parse(&p, r.get(itemsKey).list())
	tok := nextContinuation(r)
	for tok != "" && (limit <= 0 || n < limit) {
		resp, err := a.read(ctx, "browse", body, ctokenQuery(tok))
		if err != nil {
			return err
		}
		c := resp.get("continuationContents", contKey)
		got := parse(&p, c.get(itemsKey).list())
		if got == 0 {
			break
		}
		n += got
		tok = nextContinuation(c)
	}
	return nil
}

// LibraryPlaylists implements Client.
func (a *API) LibraryPlaylists(ctx context.Context, limit int) ([]PlaylistRef, error) {
	var out []PlaylistRef
	err := a.library(ctx, "FEmusic_liked_playlists", "gridRenderer", "gridContinuation", "items", limit, func(p *parser, items []node) int {
		got := p.parseTwoRowPlaylists(items)
		out = append(out, got...)
		return len(got)
	})
	return capLen(out, limit), err
}

// LibraryAlbums implements Client.
func (a *API) LibraryAlbums(ctx context.Context, limit int) ([]AlbumRef, error) {
	var out []AlbumRef
	err := a.library(ctx, "FEmusic_liked_albums", "gridRenderer", "gridContinuation", "items", limit, func(p *parser, items []node) int {
		got := p.parseTwoRowAlbums(items)
		out = append(out, got...)
		return len(got)
	})
	return capLen(out, limit), err
}

// LibraryArtists implements Client.
func (a *API) LibraryArtists(ctx context.Context, limit int) ([]ArtistRef, error) {
	var out []ArtistRef
	err := a.library(ctx, "FEmusic_library_corpus_track_artists", "musicShelfRenderer", "musicShelfContinuation", "contents", limit, func(p *parser, items []node) int {
		got := p.parseLibraryArtists(items)
		out = append(out, got...)
		return len(got)
	})
	return capLen(out, limit), err
}

// parseTwoRowPlaylists parses playlist cards (parsers/browsing.py
// parse_playlist). Cards without a playlist target ("New playlist") are
// skipped.
func (p *parser) parseTwoRowPlaylists(items []node) []PlaylistRef {
	var out []PlaylistRef
	for _, it := range items {
		d := it.get(mtrir)
		if !d.exists() {
			continue
		}
		browse := d.get("title", "runs", 0, "navigationEndpoint", "browseEndpoint", "browseId").str()
		if !strings.HasPrefix(browse, "VL") {
			continue
		}
		pl := PlaylistRef{Title: d.get("title").runsText(), ID: browse[2:], ThumbnailURL: twoRowThumb(d)}
		for _, m := range d.get("menu", "menuRenderer", "items").list() {
			if m.get("menuNavigationItemRenderer", "navigationEndpoint", "playlistEditorEndpoint", "playlistId").str() == pl.ID {
				pl.Owned = true
			}
		}
		runs := d.get("subtitle", "runs").list()
		if len(runs) == 3 && digitsRE.MatchString(runs[2].get("text").str()) {
			pl.Author = runs[0].get("text").str()
			pl.Count = runs[2].get("text").str()
		} else if len(runs) > 0 {
			pl.Author = d.get("subtitle").runsText()
		}
		out = append(out, pl)
	}
	return out
}

// parseLibraryArtists parses library artist rows (parsers/library.py
// parse_artists); Audience holds the subtitle ("55 songs").
func (p *parser) parseLibraryArtists(items []node) []ArtistRef {
	var out []ArtistRef
	for _, it := range items {
		d := it.get(mrlir)
		if !d.exists() {
			continue
		}
		out = append(out, ArtistRef{
			Name:         flexItem(d, 0).get("text").runsText(),
			ID:           d.get("navigationEndpoint", "browseEndpoint", "browseId").str(),
			Audience:     flexItem(d, 1).get("text").runsText(),
			ThumbnailURL: thumbs(d),
		})
	}
	return out
}
