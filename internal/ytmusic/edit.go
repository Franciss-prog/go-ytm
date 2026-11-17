package ytmusic

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Account writes: playlist edits and ratings. Request bodies follow
// ytmusicapi mixins/playlists.py (create_playlist, delete_playlist,
// add_playlist_items, remove_playlist_items) and mixins/library.py
// (rate_song) at the pinned commit. Writes are never retried or shared
// (not idempotent) but do get one silent session renewal on 401.

// ErrEditFailed means YouTube Music did not report success for a write.
// Adding a video that is already in the playlist fails this way unless
// duplicates are allowed.
var ErrEditFailed = errors.New("ytmusic: edit not applied")

func (a *API) write(ctx context.Context, endpoint string, body map[string]any) (node, error) {
	if a.credentials() == nil {
		return node{}, ErrUnauthenticated
	}
	var out node
	err := a.withRenew(ctx, func() error {
		var err error
		out, err = a.call(ctx, endpoint, body, "", false)
		return err
	})
	return out, err
}

// checkStatus treats a present, non-success "status" as a failed edit.
func checkStatus(op string, resp node) error {
	st := resp.get("status").str()
	if st == "" || strings.Contains(st, "SUCCEEDED") {
		return nil
	}
	return fmt.Errorf("%s: %s: %w", op, st, ErrEditFailed)
}

// CreatePlaylist implements Client. privacy is one of the Privacy*
// constants (default private).
func (a *API) CreatePlaylist(ctx context.Context, title, description, privacy string, videoIDs []string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("ytmusic: playlist title is empty")
	}
	if strings.ContainsAny(title, "<>") {
		return "", fmt.Errorf("ytmusic: playlist title %q contains < or >", title)
	}
	if privacy == "" {
		privacy = PrivacyPrivate
	}
	body := map[string]any{"title": title, "description": description, "privacyStatus": privacy}
	if len(videoIDs) > 0 {
		body["videoIds"] = videoIDs
	}
	resp, err := a.write(ctx, "playlist/create", body)
	if err != nil {
		return "", err
	}
	if id := resp.get("playlistId").str(); id != "" {
		return id, nil
	}
	if err := checkStatus("create playlist", resp); err != nil {
		return "", err
	}
	return "", fmt.Errorf("create playlist: no playlist id in response: %w", ErrEditFailed)
}

// DeletePlaylist implements Client.
func (a *API) DeletePlaylist(ctx context.Context, playlistID string) error {
	resp, err := a.write(ctx, "playlist/delete", map[string]any{"playlistId": strings.TrimPrefix(playlistID, "VL")})
	if err != nil {
		return err
	}
	return checkStatus("delete playlist", resp)
}

// AddPlaylistItems implements Client. Without allowDuplicates, a video
// listed twice is refused before sending, and YouTube Music refuses videos
// already in the playlist (ErrEditFailed, nothing added).
func (a *API) AddPlaylistItems(ctx context.Context, playlistID string, videoIDs []string, allowDuplicates bool) error {
	if len(videoIDs) == 0 {
		return errors.New("ytmusic: no videos to add")
	}
	seen := map[string]bool{}
	actions := make([]any, 0, len(videoIDs))
	for _, id := range videoIDs {
		if seen[id] && !allowDuplicates {
			return fmt.Errorf("ytmusic: video %s listed twice", id)
		}
		seen[id] = true
		act := map[string]any{"action": "ACTION_ADD_VIDEO", "addedVideoId": id}
		if allowDuplicates {
			// Skips the server's duplicate check (ytmusicapi duplicates=True).
			act["dedupeOption"] = "DEDUPE_OPTION_SKIP"
		}
		actions = append(actions, act)
	}
	resp, err := a.write(ctx, "browse/edit_playlist", map[string]any{"playlistId": strings.TrimPrefix(playlistID, "VL"), "actions": actions})
	if err != nil {
		return err
	}
	return checkStatus("add to playlist", resp)
}

// RemovePlaylistItems implements Client. Tracks need SetVideoID (present on
// rows of playlists the account owns).
func (a *API) RemovePlaylistItems(ctx context.Context, playlistID string, tracks []Track) error {
	var actions []any
	for _, t := range tracks {
		if t.VideoID == "" || t.SetVideoID == "" {
			continue
		}
		actions = append(actions, map[string]any{"setVideoId": t.SetVideoID, "removedVideoId": t.VideoID, "action": "ACTION_REMOVE_VIDEO"})
	}
	if len(actions) == 0 {
		return errors.New("ytmusic: cannot remove songs without a playlist item id (do you own this playlist?)")
	}
	resp, err := a.write(ctx, "browse/edit_playlist", map[string]any{"playlistId": strings.TrimPrefix(playlistID, "VL"), "actions": actions})
	if err != nil {
		return err
	}
	return checkStatus("remove from playlist", resp)
}

// Rate implements Client: Like, Dislike or LikeIndifferent (removes the
// rating).
func (a *API) Rate(ctx context.Context, videoID string, s LikeStatus) error {
	var ep string
	switch s {
	case Like:
		ep = "like/like"
	case Dislike:
		ep = "like/dislike"
	case LikeIndifferent:
		ep = "like/removelike"
	default:
		return fmt.Errorf("ytmusic: invalid rating %v", s)
	}
	_, err := a.write(ctx, ep, map[string]any{"target": map[string]any{"videoId": videoID}})
	return err
}
