package ui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/Franciss-Prog/go-ytm/internal/cache"
	"github.com/Franciss-Prog/go-ytm/internal/stream"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

// Downloads saves tracks for offline play (*cache.Cache). Download blocks:
// the model calls it only from commands.
type Downloads interface {
	Download(ctx context.Context, id string) (cache.Result, error)
}

// DownloadMsg asks for tracks to be downloaded (actions popup).
type DownloadMsg struct{ Tracks []ytmusic.Track }

// downloadedMsg is one finished download; rest are still to do.
type downloadedMsg struct {
	track ytmusic.Track
	res   cache.Result
	err   error
	rest  []ytmusic.Track
}

// download runs the downloads one after another (ytm-player
// download_multiple), toasting each result.
func (m *Model) download(msg DownloadMsg) tea.Cmd {
	if len(msg.Tracks) == 0 {
		return nil
	}
	d := m.o.Downloads
	if d == nil {
		return m.toasts.push(Toast{Severity: SevWarn, Text: "Downloads are unavailable."})
	}
	what := "“" + msg.Tracks[0].Title + "”"
	if len(msg.Tracks) > 1 {
		what = plural(len(msg.Tracks), "track")
	}
	return tea.Batch(m.toasts.push(Toast{Severity: SevInfo, Text: "Downloading " + what}), m.downloadNext(msg.Tracks))
}

// downloadNext downloads ts[0]; its result starts the rest.
func (m *Model) downloadNext(ts []ytmusic.Track) tea.Cmd {
	if len(ts) == 0 || m.o.Downloads == nil {
		return nil
	}
	d, ctx := m.o.Downloads, m.o.Context
	return func() tea.Msg {
		res, err := d.Download(ctx, ts[0].VideoID)
		return downloadedMsg{track: ts[0], res: res, err: err, rest: ts[1:]}
	}
}

// downloaded toasts one result (details of a failure go to the log) and
// starts the next download.
func (m *Model) downloaded(msg downloadedMsg) tea.Cmd {
	if errors.Is(msg.err, context.Canceled) {
		return nil
	}
	return tea.Batch(m.downloadToast(msg), m.downloadNext(msg.rest))
}

func (m *Model) downloadToast(msg downloadedMsg) tea.Cmd {
	name := "“" + msg.track.Title + "”"
	t := Toast{Severity: SevSuccess, Text: "Downloaded " + name}
	switch {
	case errors.Is(msg.err, cache.ErrBusy):
		t = Toast{Severity: SevInfo, Text: "Already downloading " + name}
	case errors.Is(msg.err, cache.ErrDisabled):
		t = Toast{Severity: SevWarn, Text: "The audio cache is off; set [cache] max_size_mb to download."}
	case errors.Is(msg.err, stream.ErrYtDlpNotFound):
		t = Toast{Severity: SevError, Text: "yt-dlp not found. Run `ytm doctor`."}
	case msg.err != nil:
		m.log.Warn("ui: download failed", "id", msg.track.VideoID, "err", msg.err)
		t = Toast{Severity: SevError, Text: "Download failed: " + name}
	case msg.res.Evicted:
		t = Toast{Severity: SevWarn, Text: name + " is bigger than the cache ([cache] max_size_mb); not kept."}
	case msg.res.Already:
		t = Toast{Severity: SevInfo, Text: "Already downloaded " + name}
	}
	return m.toasts.push(t)
}
