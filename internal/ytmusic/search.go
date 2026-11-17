package ytmusic

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// SearchFilter restricts a search to one kind of result.
type SearchFilter string

// Search filters accepted by `ytm search --filter`. FilterAll is the
// unfiltered mixed search.
const (
	FilterAll       SearchFilter = ""
	FilterSongs     SearchFilter = "songs"
	FilterVideos    SearchFilter = "videos"
	FilterAlbums    SearchFilter = "albums"
	FilterArtists   SearchFilter = "artists"
	FilterPlaylists SearchFilter = "playlists"
)

// Filters lists the named filters in display order.
var Filters = []SearchFilter{FilterSongs, FilterVideos, FilterAlbums, FilterArtists, FilterPlaylists}

// ParseFilter validates a filter name ("" and "all" mean FilterAll).
func ParseFilter(s string) (SearchFilter, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "all" {
		return FilterAll, nil
	}
	for _, f := range Filters {
		if string(f) == s {
			return f, nil
		}
	}
	return "", fmt.Errorf("unknown search filter %q (want one of songs, videos, albums, artists, playlists)", s)
}

// searchParams returns the InnerTube `params` value for a filter.
//
// Ported from ytmusicapi parsers/search.py get_search_params (MIT,
// sigma67/ytmusicapi@86fcd17, 2026-11-12) for scope=None, ignore_spelling=False.
func searchParams(f SearchFilter) string {
	const (
		filtered = "EgWKAQ"
		suffix   = "AWoMEA4QChADEAQQCRAF"
	)
	switch f {
	case FilterSongs:
		return filtered + "II" + suffix
	case FilterVideos:
		return filtered + "IQ" + suffix
	case FilterAlbums:
		return filtered + "IY" + suffix
	case FilterArtists:
		return filtered + "Ig" + suffix
	case FilterPlaylists:
		return "Eg-KAQwIABAAGAAgACgB" + "MABqChAEEAMQCRAFEAo%3D"
	}
	return ""
}

// resultType maps a filter to the type of every result it returns.
func (f SearchFilter) resultType() ResultType {
	switch f {
	case FilterSongs:
		return TypeSong
	case FilterVideos:
		return TypeVideo
	case FilterAlbums:
		return TypeAlbum
	case FilterArtists:
		return TypeArtist
	case FilterPlaylists:
		return TypePlaylist
	}
	return ""
}

// parser turns InnerTube JSON into normalized values and records shapes it
// did not understand (so they show up in the debug log instead of crashing).
type parser struct {
	unknown []string
}

func (p *parser) miss(format string, args ...any) {
	if len(p.unknown) < 50 {
		p.unknown = append(p.unknown, fmt.Sprintf(format, args...))
	}
}

// parseSearch parses the first page of a search response.
func (p *parser) parseSearch(resp node, f SearchFilter) SearchResults {
	var out SearchResults
	contents := resp.get("contents")
	if !contents.exists() {
		return out // no results
	}
	if tabs := contents.get("tabbedSearchResultsRenderer", "tabs"); tabs.exists() {
		contents = tabs.get(0, "tabRenderer", "content")
	}
	for _, sec := range contents.get("sectionListRenderer", "contents").list() {
		switch {
		case sec.has("musicCardShelfRenderer"):
			card := sec.get("musicCardShelfRenderer")
			top, ok := p.parseTopResult(card)
			if ok {
				out.Items = append(out.Items, top)
			}
			for _, it := range card.get("contents").list() {
				if r, ok := p.parseItem(it, FilterAll, top); ok {
					out.Items = append(out.Items, r)
				}
			}
		case sec.has("musicShelfRenderer"):
			shelf := sec.get("musicShelfRenderer")
			for _, it := range shelf.get("contents").list() {
				if r, ok := p.parseItem(it, f, SearchResult{}); ok {
					out.Items = append(out.Items, r)
				}
			}
			out.Continuation = shelf.get("continuations", 0, "nextContinuationData", "continuation").str()
		case sec.has("itemSectionRenderer"):
			for _, it := range sec.get("itemSectionRenderer", "contents").list() {
				if r, ok := p.parseItem(it, f, SearchResult{}); ok {
					out.Items = append(out.Items, r)
				}
			}
		default:
			p.miss("search section %q", sec.firstKey())
		}
	}
	return out
}

// parseSearchContinuation parses a `musicShelfContinuation` page.
func (p *parser) parseSearchContinuation(resp node, f SearchFilter) SearchResults {
	var out SearchResults
	shelf := resp.get("continuationContents", "musicShelfContinuation")
	if !shelf.exists() {
		return out
	}
	for _, it := range shelf.get("contents").list() {
		if r, ok := p.parseItem(it, f, SearchResult{}); ok {
			out.Items = append(out.Items, r)
		}
	}
	out.Continuation = shelf.get("continuations", 0, "nextContinuationData", "continuation").str()
	return out
}

// Page types from browseEndpointContextMusicConfig.
const (
	pageAlbum       = "MUSIC_PAGE_TYPE_ALBUM"
	pageArtist      = "MUSIC_PAGE_TYPE_ARTIST"
	pagePlaylist    = "MUSIC_PAGE_TYPE_PLAYLIST"
	pageUserChannel = "MUSIC_PAGE_TYPE_USER_CHANNEL"
)

// Music video types (watchEndpointMusicConfig.musicVideoType).
const (
	videoTypeATV     = "MUSIC_VIDEO_TYPE_ATV" // audio track
	videoTypeEpisode = "MUSIC_VIDEO_TYPE_PODCAST_EPISODE"
)

func pageType(browse node) string {
	return browse.get("browseEndpointContextSupportedConfigs", "browseEndpointContextMusicConfig", "pageType").str()
}

func musicVideoType(watch node) string {
	return watch.get("watchEndpointMusicSupportedConfigs", "watchEndpointMusicConfig", "musicVideoType").str()
}

// classify decides the result type of an unfiltered item from its
// navigation target (language independent, unlike the subtitle label).
// Podcasts, episodes and user profiles are out of scope and return "".
func classify(browse node, videoType string) ResultType {
	if browse.exists() {
		switch pageType(browse) {
		case pageAlbum:
			return TypeAlbum
		case pageArtist:
			return TypeArtist
		case pagePlaylist:
			return TypePlaylist
		case "":
			// Fall back to browseId prefixes (ytmusicapi parse_search_result).
			id := browse.get("browseId").str()
			switch {
			case strings.HasPrefix(id, "MPRE"):
				return TypeAlbum
			case strings.HasPrefix(id, "VL"), strings.HasPrefix(id, "RD"):
				return TypePlaylist
			case strings.HasPrefix(id, "MPLA"):
				return TypeArtist
			}
		}
		return ""
	}
	switch videoType {
	case videoTypeATV:
		return TypeSong
	case videoTypeEpisode:
		return ""
	}
	return TypeVideo
}

const mrlir = "musicResponsiveListItemRenderer"

func flexRuns(item node, i int) []node {
	return item.get("flexColumns", i, "musicResponsiveListItemFlexColumnRenderer", "text", "runs").list()
}

// parseItem parses one musicResponsiveListItemRenderer. For filtered
// searches the type comes from the filter; otherwise from the item's
// navigation target. parent is the top-result card the item belongs to, if
// any (its tracks inherit the card's artist when they list none).
func (p *parser) parseItem(wrapper node, f SearchFilter, parent SearchResult) (SearchResult, bool) {
	item := wrapper.get(mrlir)
	if !item.exists() {
		if k := wrapper.firstKey(); k != "messageRenderer" {
			p.miss("search item %q", k)
		}
		return SearchResult{}, false
	}
	play := item.get("overlay", "musicItemThumbnailOverlayRenderer", "content", "musicPlayButtonRenderer", "playNavigationEndpoint")
	browse := item.get("navigationEndpoint", "browseEndpoint")
	vt := musicVideoType(play.get("watchEndpoint"))

	typ := f.resultType()
	if typ == "" {
		typ = classify(browse, vt)
		if typ == "" {
			return SearchResult{}, false // podcast, episode, profile
		}
	}

	title := ""
	if r := flexRuns(item, 0); len(r) > 0 {
		title = r[0].get("text").str()
	}
	runs := flexRuns(item, 1)
	// Unfiltered results (and album/artist results in any mode) prefix the
	// subtitle with an unlinked type label: "Song • …", "Album • …".
	label := ""
	if (f == FilterAll || typ == TypeAlbum || typ == TypeArtist) && len(runs) >= 1 && !runs[0].has("navigationEndpoint") {
		if len(runs) == 1 || isSeparator(runs[1]) {
			label = runs[0].get("text").str()
			runs = runs[min(2, len(runs)):]
		}
	}
	thumb := largestThumbnail(item.get("thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails"))
	explicit := hasExplicitBadge(item.get("badges"))

	switch typ {
	case TypeSong, TypeVideo:
		t := &Track{
			Title:        title,
			VideoID:      play.get("watchEndpoint", "videoId").str(),
			VideoType:    vt,
			IsVideo:      vt != videoTypeATV,
			ThumbnailURL: thumb,
			Explicit:     explicit,
			Available:    item.get("musicItemRendererDisplayPolicy").str() != "MUSIC_ITEM_RENDERER_DISPLAY_POLICY_GREY_OUT",
		}
		if t.VideoID == "" {
			t.VideoID = item.get("playlistItemData", "videoId").str()
		}
		if t.VideoID == "" {
			p.miss("track without videoId: %q", title)
			return SearchResult{}, false
		}
		sr := parseSongRuns(runs)
		t.Artists, t.Album, t.Duration, t.Views = sr.artists, sr.album, sr.duration, sr.views
		if len(t.Artists) == 0 && parent.Artist != nil {
			t.Artists = []Artist{{Name: parent.Artist.Name, ID: parent.Artist.ID}}
		}
		if extra := flexRuns(item, 2); len(extra) > 0 && t.Views == "" {
			t.Views = viewsCount(extra[0].get("text").str())
		}
		if typ == TypeSong && t.IsVideo {
			typ = TypeVideo
		}
		return SearchResult{Type: typ, Track: t}, true

	case TypeAlbum:
		a := &AlbumRef{
			Name:         title,
			ID:           browse.get("browseId").str(),
			Kind:         label,
			ThumbnailURL: thumb,
			Explicit:     explicit,
			PlaylistID:   firstNonEmpty(play.get("watchPlaylistEndpoint", "playlistId").str(), play.get("watchEndpoint", "playlistId").str()),
		}
		sr := parseSongRuns(runs)
		a.Artists, a.Year = sr.artists, sr.year
		return SearchResult{Type: TypeAlbum, Album: a}, true

	case TypeArtist:
		ar := &ArtistRef{Name: title, ID: browse.get("browseId").str(), ThumbnailURL: thumb}
		if len(runs) > 0 {
			ar.Audience = runs[0].get("text").str()
		}
		return SearchResult{Type: TypeArtist, Artist: ar}, true

	case TypePlaylist:
		pl := &PlaylistRef{Title: title, ID: strings.TrimPrefix(browse.get("browseId").str(), "VL"), ThumbnailURL: thumb}
		var texts []string
		for i, r := range runs {
			if i%2 == 0 {
				texts = append(texts, r.get("text").str())
			}
		}
		if n := len(texts); n >= 1 {
			pl.Count = texts[n-1]
			if n >= 2 {
				pl.Author = strings.Join(texts[:n-1], ", ")
			}
		}
		return SearchResult{Type: TypePlaylist, Playlist: pl}, true
	}
	return SearchResult{}, false
}

// parseTopResult parses the "Top result" musicCardShelfRenderer.
func (p *parser) parseTopResult(card node) (SearchResult, bool) {
	titleRun := card.get("title", "runs", 0)
	title := titleRun.get("text").str()
	nav := titleRun.get("navigationEndpoint")
	if !nav.exists() {
		nav = card.get("onTap")
	}
	runs := card.get("subtitle", "runs").list()
	label := ""
	if len(runs) >= 1 && !runs[0].has("navigationEndpoint") {
		label = runs[0].get("text").str()
		runs = runs[min(2, len(runs)):]
	}
	thumb := largestThumbnail(card.get("thumbnail", "musicThumbnailRenderer", "thumbnail", "thumbnails"))

	if w := nav.get("watchEndpoint"); w.exists() {
		vt := musicVideoType(w)
		if vt == videoTypeEpisode {
			return SearchResult{}, false
		}
		t := &Track{
			Title: title, VideoID: w.get("videoId").str(), VideoType: vt, IsVideo: vt != videoTypeATV,
			ThumbnailURL: thumb, Explicit: hasExplicitBadge(card.get("subtitleBadges")), Available: true,
		}
		sr := parseSongRuns(runs)
		t.Artists, t.Album, t.Duration, t.Views = sr.artists, sr.album, sr.duration, sr.views
		typ := TypeSong
		if t.IsVideo {
			typ = TypeVideo
		}
		return SearchResult{Type: typ, Top: true, Track: t}, t.VideoID != ""
	}
	browse := nav.get("browseEndpoint")
	switch classify(browse, "") {
	case TypeArtist:
		ar := &ArtistRef{Name: title, ID: browse.get("browseId").str(), ThumbnailURL: thumb}
		if len(runs) > 0 {
			ar.Audience = runs[0].get("text").str()
		}
		return SearchResult{Type: TypeArtist, Top: true, Artist: ar}, true
	case TypeAlbum:
		sr := parseSongRuns(runs)
		return SearchResult{Type: TypeAlbum, Top: true, Album: &AlbumRef{
			Name: title, ID: browse.get("browseId").str(), Kind: label, Artists: sr.artists, Year: sr.year,
			ThumbnailURL: thumb,
		}}, true
	case TypePlaylist:
		pl := &PlaylistRef{Title: title, ID: strings.TrimPrefix(browse.get("browseId").str(), "VL"), ThumbnailURL: thumb}
		if len(runs) > 0 {
			pl.Author = runs[0].get("text").str()
		}
		return SearchResult{Type: TypePlaylist, Top: true, Playlist: pl}, true
	}
	if title != "" {
		p.miss("top result %q (label %q)", title, label)
	}
	return SearchResult{}, false
}

type songRuns struct {
	artists  []Artist
	album    *AlbumRef
	duration time.Duration
	year     string
	views    string
}

var (
	durationRE = regexp.MustCompile(`^(\d+:)*\d+:\d+$`)
	yearRE     = regexp.MustCompile(`^\d{4}$`)
	// hl=en only: "22M views", "1.9B plays", "676 views".
	viewsRE = regexp.MustCompile(`^(\d[\d.,]*[KMB]?) (?:views|plays)$`)
)

// parseSongRuns reads "Artist • Album • 3:00"-style subtitle runs. Odd
// indexes are separators (" • ", " & ", ", "). Ported from ytmusicapi
// parsers/songs.py parse_song_runs, with stricter view detection.
func parseSongRuns(runs []node) songRuns {
	var out songRuns
	for i, r := range runs {
		if i%2 == 1 {
			continue
		}
		text := strings.TrimSpace(r.get("text").str())
		if r.has("navigationEndpoint") {
			id := r.get("navigationEndpoint", "browseEndpoint", "browseId").str()
			if strings.HasPrefix(id, "MPRE") || strings.Contains(id, "release_detail") {
				out.album = &AlbumRef{Name: text, ID: id}
			} else {
				out.artists = append(out.artists, Artist{Name: text, ID: id})
			}
			continue
		}
		switch {
		case durationRE.MatchString(text):
			if s, ok := parseDuration(text); ok {
				out.duration = time.Duration(s) * time.Second
			}
		case yearRE.MatchString(text):
			out.year = text
		case viewsCount(text) != "":
			out.views = viewsCount(text)
		case text != "":
			out.artists = append(out.artists, Artist{Name: text})
		}
	}
	return out
}

func viewsCount(s string) string {
	if m := viewsRE.FindStringSubmatch(strings.TrimSpace(s)); m != nil {
		return m[1]
	}
	return ""
}

func isSeparator(r node) bool {
	return strings.TrimSpace(r.get("text").str()) == "•"
}

func hasExplicitBadge(badges node) bool {
	for _, b := range badges.list() {
		if b.get("musicInlineBadgeRenderer", "icon", "iconType").str() == "MUSIC_EXPLICIT_BADGE" {
			return true
		}
	}
	return false
}

// largestThumbnail picks the widest thumbnail URL.
func largestThumbnail(thumbs node) string {
	best, bestW := "", -1.0
	for _, t := range thumbs.list() {
		w, _ := t.get("width").v.(float64)
		if u := t.get("url").str(); u != "" && w > bestW {
			best, bestW = u, w
		}
	}
	return best
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// parseSuggestions reads music/get_search_suggestions (first section only,
// as ytmusicapi does; the second section holds entity shortcuts).
func parseSuggestions(resp node) []string {
	var out []string
	for _, it := range resp.get("contents", 0, "searchSuggestionsSectionRenderer", "contents").list() {
		r := it.get("searchSuggestionRenderer")
		if !r.exists() {
			r = it.get("historySuggestionRenderer")
		}
		if q := r.get("navigationEndpoint", "searchEndpoint", "query").str(); q != "" {
			out = append(out, q)
		}
	}
	return out
}
