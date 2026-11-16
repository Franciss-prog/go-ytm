package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Franciss-Prog/go-ytm/internal/style"
	"github.com/Franciss-Prog/go-ytm/internal/ytmusic"
)

func newSearchCmd(a *App) *cobra.Command {
	var (
		asJSON bool
		filter string
		limit  int
	)
	cmd := &cobra.Command{
		Use:   "search QUERY...",
		Short: "Search YouTube Music",
		Long: "Searches YouTube Music and prints the results.\n" +
			"Works without signing in; a saved session (ytm setup) is used when present.",
		Example: "ytm search daft punk\n" +
			"ytm search -f songs -l 50 harder better faster\n" +
			"ytm search --json -f albums discovery | jq '.[0].id'",
		Args: usageArgs(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := ytmusic.ParseFilter(filter)
			if err != nil {
				return &ExitError{Code: ExitUsage, Err: err}
			}
			if limit < 1 || limit > 500 {
				return &ExitError{Code: ExitUsage, Err: errors.New("--limit must be between 1 and 500")}
			}
			q := strings.Join(args, " ")
			res, err := a.search(cmd.Context(), q, f, limit)
			if err != nil {
				return err
			}
			if asJSON {
				items := res.Items
				if items == nil {
					items = []ytmusic.SearchResult{}
				}
				return a.PrintJSON(items)
			}
			m := a.Mode(false)
			return a.Print(m, renderSearch(a.Renderer(m), q, f, res.Items))
		},
	}
	fs := cmd.Flags()
	fs.StringVarP(&filter, "filter", "f", "", "only `type`: songs, videos, albums, artists, playlists")
	fs.IntVarP(&limit, "limit", "l", 20, "maximum number of results")
	addJSONFlag(fs, &asJSON)
	_ = cmd.RegisterFlagCompletionFunc("filter", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		out := make([]string, len(ytmusic.Filters))
		for i, f := range ytmusic.Filters {
			out[i] = string(f)
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

// search runs the query with the saved session, falling back to an
// anonymous search when the session has expired and cannot be renewed.
func (a *App) search(ctx context.Context, q string, f ytmusic.SearchFilter, limit int) (ytmusic.SearchResults, error) {
	c, signedIn, err := a.signedInClient()
	if err != nil {
		return ytmusic.SearchResults{}, &ExitError{Code: ExitFailure, Err: err}
	}
	res, err := c.Search(ctx, q, f, limit)
	if err != nil && signedIn && errors.Is(err, ytmusic.ErrUnauthenticated) {
		a.Log.Info("search: saved session rejected; searching signed out", "err", err)
		a.warn("Your YouTube Music session has expired; showing signed-out results.", "Sign in again with", "ytm setup")
		res, err = a.newYT(a.ytOptions()).Search(ctx, q, f, limit)
	}
	if err != nil {
		return ytmusic.SearchResults{}, a.apiError(ctx, "search", err)
	}
	return res, nil
}

// apiError maps a ytmusic error to a short message and exit code; the full
// chain goes to the log.
func (a *App) apiError(ctx context.Context, op string, err error) error {
	a.Log.Error(op+" failed", "err", err)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var he *ytmusic.HTTPError
	switch {
	case errors.Is(err, ytmusic.ErrUnauthenticated):
		return &ExitError{Code: ExitUnauth, Err: errors.New("not signed in to YouTube Music; run `ytm setup`")}
	case errors.Is(err, ytmusic.ErrRateLimited) && errors.As(err, &he) && he.RetryAfter > 0:
		return &ExitError{Code: ExitFailure, Err: fmt.Errorf("YouTube Music is rate limiting requests; try again in %s", he.RetryAfter.Round(time.Second))}
	case errors.Is(err, ytmusic.ErrRateLimited):
		return &ExitError{Code: ExitFailure, Err: errors.New("YouTube Music is rate limiting requests; try again shortly")}
	case errors.As(err, &he):
		return &ExitError{Code: ExitFailure, Err: fmt.Errorf("YouTube Music returned HTTP %d; see the log (ytm config path) for details", he.Status)}
	}
	return &ExitError{Code: ExitFailure, Err: fmt.Errorf("could not reach YouTube Music: %w", err)}
}

// warn prints a warning with an optional command hint to stderr.
func (a *App) warn(msg, hint, cmd string) {
	m := a.modeFor(a.IO.Err, false)
	r := a.Renderer(m)
	s := r.Message(style.StatusWarn, msg)
	if cmd != "" {
		s += r.Hint(hint, cmd)
	}
	_, _ = io.WriteString(m.Writer(a.IO.Err), s)
}

func renderSearch(r *style.Renderer, q string, f ytmusic.SearchFilter, items []ytmusic.SearchResult) string {
	what := "all"
	if f != ytmusic.FilterAll {
		what = string(f)
	}
	head := r.Heading("Search · "+q, what+" · "+style.Plural(len(items), "result"))
	if len(items) == 0 {
		return head + r.Message(style.StatusInfo, "No results.")
	}
	cols := []style.Column{
		{Header: "#", Right: true},
		{Header: "Type"},
		{Header: "Title", Shrink: true, Min: 12},
		{Header: "Artist", Shrink: true, Min: 10},
		{Header: "Album / Info", Shrink: true, Min: 8},
		{Header: "Time", Right: true},
		{Header: "ID", Muted: true},
	}
	if f != ytmusic.FilterAll {
		cols = append(cols[:1], cols[2:]...)
	}
	rows := make([][]string, 0, len(items))
	for i, it := range items {
		row := append([]string{strconv.Itoa(i + 1)}, resultCells(it)...)
		if f != ytmusic.FilterAll {
			row = append(row[:1], row[2:]...)
		}
		rows = append(rows, row)
	}
	cols, rows = dropEmptyColumns(cols, rows)
	return head + r.Table(cols, rows, 0)
}

// dropEmptyColumns removes columns whose cells are all empty.
func dropEmptyColumns(cols []style.Column, rows [][]string) ([]style.Column, [][]string) {
	keep := make([]bool, len(cols))
	for _, row := range rows {
		for i, c := range row {
			if i < len(keep) && c != "" {
				keep[i] = true
			}
		}
	}
	var outCols []style.Column
	for i, c := range cols {
		if keep[i] {
			outCols = append(outCols, c)
		}
	}
	outRows := make([][]string, len(rows))
	for r, row := range rows {
		for i, c := range row {
			if i < len(keep) && keep[i] {
				outRows[r] = append(outRows[r], c)
			}
		}
	}
	return outCols, outRows
}

// resultCells returns type, title, artist, info, time and id cells.
func resultCells(it ytmusic.SearchResult) []string {
	switch {
	case it.Track != nil:
		t := it.Track
		album := ""
		if t.Album != nil {
			album = t.Album.Name
		}
		return []string{string(it.Type), t.Title, artistNames(t.Artists), album, formatDuration(t.Duration), t.VideoID}
	case it.Album != nil:
		al := it.Album
		kind := strings.ToLower(al.Kind)
		if kind == "" {
			kind = "album"
		}
		return []string{kind, al.Name, artistNames(al.Artists), al.Year, "", al.ID}
	case it.Artist != nil:
		return []string{"artist", it.Artist.Name, "", it.Artist.Audience, "", it.Artist.ID}
	case it.Playlist != nil:
		p := it.Playlist
		return []string{"playlist", p.Title, p.Author, p.Count, "", p.ID}
	}
	return []string{string(it.Type), "", "", "", "", ""}
}

func artistNames(as []ytmusic.Artist) string {
	names := make([]string, len(as))
	for i, a := range as {
		names[i] = a.Name
	}
	return strings.Join(names, ", ")
}

// formatDuration renders m:ss or h:mm:ss; zero (unknown) is empty.
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	s := int(d.Round(time.Second) / time.Second)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
