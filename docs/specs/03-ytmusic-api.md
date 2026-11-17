# 03 — YouTube Music API, auth, and stream resolution

Status: **M2 implemented** (client core, auth, search), **M3 implemented** (stream) · Milestones: M2 (client, auth, search), M3 (stream), M6 (browse/radio/history)

## Goals

- Internal `internal/ytmusic` package speaking InnerTube (`music.youtube.com/youtubei/v1/*`)
  for exactly the endpoints the app needs, normalized to one `Track` type (see 01).
- Cookie-based auth equivalent to the original `ytm setup`, multi-account slot aware, brand accounts.
- yt-dlp-based stream resolution with prefetch and an expiring URL cache.
- Everything behind interfaces; parsers tested against recorded JSON fixtures.

## Non-goals

- Implementing YouTube signature/n-param deciphering (yt-dlp owns that; it changes weekly).
- OAuth device flow, uploads, podcasts.

## Library evaluation (verified 2026-11-16)

| Candidate | Finding | Verdict |
|---|---|---|
| `github.com/raitonoberu/ytmusic` | search only; last push 2024-03-24; no tagged versions | reject |
| `github.com/kkdai/youtube/v2` v2.10.8 | YouTube video client + deciphering, not Music browse | reject |
| GitHub search "ytmusic language:go" | top results are TUIs/backends with < 25 stars, not libraries | reject |
| `sigma67/ytmusicapi` (Python, **MIT**, active, pushed 2026-11-12) | the de-facto reference; the original app uses it | **port the request shapes + parsers we need**, with attribution in `internal/ytmusic/NOTICE` |

So: write `internal/ytmusic`. Constants (search filter `params`, browse IDs like
`FEmusic_liked_playlists`, `FEmusic_history`, `FEmusic_home`, `FEmusic_charts`) are copied from
ytmusicapi source **at implementation time** (pinned commit noted in code comments), not from memory.

## Client interface

The Go interface grows per milestone: M2 ships `Search`, `SearchSuggestions` and `Account`; the
rest land with the milestone that first uses them. Target shape:

```go
type Client interface {
    Search(ctx, q string, f SearchFilter, limit int) (SearchResults, error)
    SearchSuggestions(ctx, q string) ([]string, error)
    LibraryPlaylists(ctx, limit int) ([]PlaylistRef, error)
    LibraryAlbums(ctx, limit int) ([]AlbumRef, error)
    LibraryArtists(ctx, limit int) ([]ArtistRef, error)
    LikedSongs(ctx, limit int) (Page[Track], error)
    Playlist(ctx, id string) (Playlist, error)                    // first page
    PlaylistContinuation(ctx, token string) (Page[Track], error)  // progressive loading
    Album(ctx, id string) (Album, error)
    Artist(ctx, id string) (Artist, error)
    ArtistAlbums(ctx, channelID, params string) ([]AlbumRef, error)
    Home(ctx, shelves int) ([]Shelf, error)
    Charts(ctx, country string) (Charts, error)
    NewReleases(ctx) ([]AlbumRef, error)
    WatchPlaylist(ctx, req WatchRequest) (WatchPlaylist, error)   // radio, playlist radio, related, lyrics browseId
    Lyrics(ctx, browseID string) (*Lyrics, error)                 // may include timed lyrics
    Song(ctx, videoID string) (SongInfo, error)                   // /player: playbackTracking for history
    History(ctx) ([]HistoryItem, error)
    AddHistoryItem(ctx, videoID string) error
    Rate(ctx, videoID string, r LikeStatus) error
    CreatePlaylist(ctx, title, desc string, privacy Privacy, videoIDs []string) (string, error)
    EditPlaylist / DeletePlaylist / AddPlaylistItems / RemovePlaylistItems
    AddToLibrary / RemoveFromLibrary / Subscribe / Unsubscribe
    Account(ctx) (AccountInfo, error)                             // name/handle for setup verification
}
```

### Endpoint map

| Method | InnerTube endpoint | Auth required |
|---|---|---|
| Search | `search` (+ `params` per filter) | no |
| SearchSuggestions | `music/get_search_suggestions` | no |
| Library*/LikedSongs/History | `browse` (`FEmusic_liked_*`, `VLLM`, `FEmusic_history`) | yes |
| Playlist/Album/Artist/Home/Charts/NewReleases | `browse` | no (private playlists: yes) |
| Continuations | `browse` with `continuation`/`ctoken` | as parent |
| WatchPlaylist / radio | `next` (`playlistId=RDAMVM<id>` for radio) | no |
| Lyrics | `browse` (lyrics browseId from `next`) | no |
| Song | `player` | no |
| AddHistoryItem | GET `playbackTracking.videostatsPlaybackUrl` (+ `ver=2&c=WEB_REMIX&cpn=<16 rand>`) | yes |
| Rate | `like/like`, `like/dislike`, `like/removelike` | yes |
| Playlist mutations | `playlist/create`, `browse/edit_playlist`, `playlist/delete` | yes |
| Library add/remove, subscribe | `feedback`, `subscription/subscribe`, `subscription/unsubscribe` | yes |

### Request shape

`POST https://music.youtube.com/youtubei/v1/<endpoint>?alt=json&prettyPrint=false` with body
`{"context":{"client":{"clientName":"WEB_REMIX","clientVersion":"1.<YYYYMMDD>.01.00","hl":"en","gl":<region>}, "user":{"onBehalfOfUser":<brand id>?}}, ...}`.
Client version date = today's UTC date, as ytmusicapi does (`helpers.py initialize_context`); no
build-time constant or refresh job needed (decided M2).

Headers when authenticated: `Cookie`, `Authorization: SAPISIDHASH <ts>_<sha1(ts+" "+SAPISID+" "+origin)>`,
`X-Goog-AuthUser: <slot>`, `X-Origin`/`Origin: https://music.youtube.com`, browser UA. No `key`
query param: ytmusicapi adds the public web key for browser auth, but yt-dlp's cookie-authenticated
InnerTube calls omit it (`_base.py _call_api`), and a hard-coded Google key trips secret scanners.
The SAPISID value is read from the
`__Secure-3PAPISID` cookie (ytmusicapi `sapisid_from_cookie`), falling back to `SAPISID`. Every request
sends `SOCS=CAI` (EU consent bypass, as ytmusicapi). `Search(limit)` follows `musicShelfContinuation`
pages for filtered searches only; unfiltered results carry no duration (YouTube omits it there).

### Normalization

One `normalize` package with pure functions per renderer (`musicResponsiveListItemRenderer`,
`musicTwoRowItemRenderer`, `playlistPanelVideoRenderer`, …) → `Track`/`AlbumRef`/etc.
Parsers use a tiny JSON path helper over `map[string]any` / `gjson`-like navigation that **never
panics** on missing keys and records "unknown shape" counters to the debug log (YouTube changes
shapes; we want to see it, not crash).

### Rate limits & resilience

- Client-side token bucket: 5 req/s burst 10 (InnerTube has no documented limit; this prevents
  hammering during fast scrolling/prefetching).
- `[playback] api_timeout` (default 15 s) per call. Retries: 2 with jittered backoff on 5xx/network
  errors for idempotent reads only. 429 → `ErrRateLimited` + `Retry-After`.
- 401/403 or "sign in" responses → `ErrUnauthenticated` → try **silent renewal** once (re-extract
  cookies from the configured source, verify same account), else toast "run `ytm setup`".
- Per-request `singleflight` for identical in-flight reads (e.g. two pages asking for the same playlist).
- Small in-memory TTL cache (60 s) for browse pages to make back/forward instant (lands with the
  first browse endpoint, M5/M6; M2 has none).
- Shared (singleflight) calls run under the client's own context and the per-call timeout, not the
  first caller's; `API.Close` cancels them and closes idle connections.

## Auth (`ytm setup`, M2)

Files in config dir (all `0600`), names kept from the original for familiarity:

| File | Content |
|---|---|
| `auth.json` | headers: cookie, user-agent, x-goog-authuser, (brand) |
| `account.json` | `{name, handle, channel_id, slot, browser}` — used to verify renewals target the same account |
| `stream_cookies.txt` | Netscape cookies for yt-dlp (`use_session_cookies`) |

Sources, in order (same precedence as the original):
1. `[yt_dlp] cookies_file` if set.
2. `--browser` flag, else `[yt_dlp] cookie_browser`, else auto-detect (firefox, zen, chrome, chromium,
   brave, edge, vivaldi, opera, helium).
3. `--manual`: paste raw request headers copied from devtools (Huh multi-line text field / stdin).

**Extraction mechanism (decided M2):** run `yt-dlp --ignore-config --no-warnings
--cookies-from-browser <spec> --cookies <tmpfile>` **with no URL** and parse the Netscape file.
yt-dlp loads the jar, fails argument validation (exit 2) and saves the jar on exit
(`YoutubeDL.__exit__ → close → save_cookies`, read in 2026.10.26 source); success is judged by the
file. Measured: 0.25 s and no network, vs 2.8 s plus a page fetch (and still a non-zero exit,
"Unsupported URL") when passing `https://music.youtube.com`. yt-dlp handles Chromium decryption and
Firefox containers. Zen (not a yt-dlp browser) is read as `firefox:<zen root>` for each existing
root; Helium as `chromium:<XDG_CONFIG_HOME>/net.imput.helium` (Linux layout only, as the original).
On Linux, Chromium-family browsers that yield no session are retried with each keyring
(`gnomekeyring`, `kwallet6`, `kwallet5`, `kwallet`, `basictext`), mirroring ytm-player. kooky is
rejected (new dependency, no gain). Proven by `TestRealYtDlpExportsWithoutURL` against a throwaway
Firefox profile.

Cookie header: unexpired `youtube.com`/`music.youtube.com` cookies minus `ST-*` (multi-KB tracking
cookies that push the header past the server limit, per ytm-player). `stream_cookies.txt`: all
`youtube.com`/`google.com` cookies.

Account slots: probe `X-Goog-AuthUser` 0..`--slots`-1 (default 10, max 30), four at a time, with
`Account()`; an account seen in several slots is listed once (lowest slot). Huh select lists the
accounts found (name + handle + slot). The channel id comes from the account menu's single
`ACCOUNT_BOX` link to a `UC…` `MUSIC_PAGE_TYPE_USER_CHANNEL` page (ytm-player rule); without it,
silent renewal is disabled and setup says so. Brand accounts: `[general] brand_account_id` is sent
as `onBehalfOfUser`; **listing** brand accounts in setup is deferred until a signed-in
`account_menu`/accounts-list response can be captured (M2 known gap).

`account.json`: `{schema_version, name, handle, channel_id, slot, source, saved_at}` where `source`
is a browser name, `cookies_file` or `manual`. Silent renewal (`auth.Renewer`) re-reads `source`,
checks the recorded slot first, then any slot, and accepts only the same `channel_id`; manual
sessions are never renewed. If the saved session is rejected and renewal fails, `ytm search` falls
back to a signed-out search with a warning.

Non-interactive: `ytm setup --browser firefox --slot 0 --yes`; with no TTY and no flags → exit 2
with "stdin is not a terminal; pass --browser/--slot or --manual < headers.txt".

## Stream resolution (M3)

```go
type Resolver interface {
    Resolve(ctx context.Context, videoID string, q Quality) (Stream, error)
}
// Stream{VideoID, URL, Headers, Expires, AvailableAt, Codec, Bitrate, Duration}
```

- Command (one process per track):
  `yt-dlp --ignore-config --no-playlist --no-warnings --simulate -f <fmt>
  --extractor-args youtube:player_client=default,android -O '%(.{url,http_headers,acodec,…})j'
  [--cookies <private copy>] [--compat-options no-certifi] [--remote-components C]…
  [--no-js-runtimes --js-runtimes R…] https://music.youtube.com/watch?v=<id>`.
  `-O` with a field subset prints ~1.6 KB instead of `-J`'s ~650 KB. `expire` is read from the
  URL query (fallback: now + 5 h, ytm-player's TTL).
- Quality → format, copied from ytm-player 2.2.0 `services/stream.py` `QUALITY_FORMATS`
  (supersedes the draft strings): high `bestaudio/best[vcodec!=none][acodec!=none]`, medium
  `bestaudio[abr<=128]/bestaudio/…`, low `bestaudio[abr<=64]/bestaudio/…`. The combined
  fallback covers clients with no audio-only stream; mpv runs `--no-video`.
- `[yt_dlp]` options: `cookies_file` (wins) or, with `use_session_cookies`, `stream_cookies.txt`
  from `ytm setup`; `ca_bundle` → `SSL_CERT_FILE` + `--compat-options no-certifi`;
  `remote_components`; `js_runtimes` (replaces yt-dlp's default list, like the Python API the
  original uses). `cookie_browser` is setup-only; playback never reads a browser.
- The cookie jar is **copied to a private temp file per run**: yt-dlp saves `--cookies` back on
  exit, which would race between two concurrent runs and could clobber a jar `ytm setup` just wrote.
- Errors (from yt-dlp's last `ERROR:` line, phrases per yt-dlp 2026.10.26
  `extractor/youtube/_video.py`): `ErrUnavailable` (unavailable/private/region/Premium/no
  format), `ErrSignInRequired` ("sign in" reasons: age gate, bot check, members-only),
  `ErrRateLimited`, `ErrYtDlpNotFound`, `ErrInvalidID` (IDs must match `^[A-Za-z0-9_-]{1,64}$`,
  checked before running). Cookie-file parse errors are redacted (they quote cookie values).
- `stream.Cache` wraps the resolver: in-memory, key (videoID, quality), fresh until
  `expire − 5 min`, max 128 entries (soonest-expiring evicted). Not persisted (URLs are IP-bound).
  Concurrent requests share one run; the run is cancelled only when **every** waiter has gone,
  so a cancelled prefetch never aborts a play of the same track. At most 2 yt-dlp processes.
  Transient failures retry after 1 s and 2 s (3 attempts, as the original); classified errors do
  not. `Clear()` (after `ytm setup`) drops results of runs already in flight.
- **`available_at`**: googlevideo answers 403 until this time (observed live ≈3 s after resolving;
  mpv then fails with "loading failed"). `Cache.Playable` = resolve + wait; every URL handed to
  mpv goes through it (in a `tea.Cmd`).
- `Cache.Refresh(id)` = invalidate + resolve: the re-resolve-once step after `TrackEnded{EndError}`
  (spec 02). The once-then-skip policy itself is applied by the queue owner (M5).
- `stream.Prefetcher`: one goroutine; `Want(q, next, next+1)` replaces the list (2 slots, priority
  order), cancels work for tracks no longer wanted, skips cached ones, tries each ID once per list,
  and reports each result via a non-blocking callback (TUI: `tea.Program.Send`).
- **Hover prefetch** (`[playback] prefetch_on_hover`, default on): when the cursor rests on a
  track row for 0.4 s (Search, Library track columns, playlist/album/artist pages, Queue), the UI
  calls `Want(q, hovered, next)`, hovered first. Moving on before it finishes cancels it (the
  Prefetcher drops IDs no longer wanted); the playing track is never requested. Enter then joins
  the run in flight or hits the cache. Measured live (2026-11-17, yt-dlp 2026.10.26, signed out):
  a cold Enter costs ≈5 s of resolve plus ≈3 s of `available_at` wait; `default` is the only
  player client that returns audio without a PO token (`tv`, `tv_simply`, `web_safari`, `mweb`,
  `ios`, `web_music`, `android_vr` all fail), so the latency cannot be cut per request.
- Local audio cache hit (M7) short-circuits resolution with a `file://` path.
- Warm process: **rejected with numbers.** A resolve takes 3.7–4.9 s wall (live, 2026-11-17), of
  which yt-dlp startup + import is ~0.2 s; the rest is network. `--batch-file -` also reads stdin
  to EOF before extracting (`yt_dlp.get_urls` → `read_batch_urls`), so it cannot serve requests
  one at a time. Prefetch hides the latency instead.

## Browse, library and edits (M5)

- `Album`, `Playlist` + `PlaylistMore` (2025 `continuationItemRenderer` tokens, body
  `{"continuation": token}` with no browseId), `Artist` (top songs, Albums / Singles & EPs / Videos
  shelves matched by their English titles, as ytmusicapi does), `ArtistAlbums` (grid +
  `gridContinuation`), `LibraryPlaylists/Albums/Artists` (old-style `continuations` paging,
  `&ctoken=…&continuation=…&type=next`), and writes `CreatePlaylist`, `DeletePlaylist`,
  `AddPlaylistItems` (`DEDUPE_OPTION_SKIP` only when duplicates are allowed), `RemovePlaylistItems`
  (needs `SetVideoID`), `Rate`. Paths, column rules and bodies ported from ytmusicapi at the pinned
  commit. Writes are not retried or shared; a non-`SUCCEEDED` `status` → `ErrEditFailed`.
- Track rows: the column holding title/artist/album is found from its navigation target (album
  pages use fixed columns); `SetVideoID` falls back to `playlistItemData.playlistSetVideoId`.
- **Fixtures** (2026-11-17, signed in, read-only calls): `record_test.go` (`-tags record`,
  `YTM_RECORD_DIR`) writes raw responses outside the repo; `scripts/scrubfixtures` drops tracking
  keys, cuts lists to 6 entries (menus kept whole), replaces account name/handle/channel id, and
  fully anonymizes library and owned-playlist captures (free text → `Text N`, ids → same-prefix
  fakes, thumbnails → example.invalid). It refuses to write a file in which an account value or an
  original id survives.
- **Not verified live:** the write calls (no edits were made to the owner's account); bodies are
  checked against ytmusicapi in `TestWriteRequestBodies`. Server response to a duplicate add is
  assumed to be a non-success `status`.

## Edge cases

- Free tier: some tracks are video-only (OMV) → still audio-only stream via yt-dlp.
- Age-restricted / Premium-only → needs `use_session_cookies`; error toast explains.
- Region-blocked → `stream.ErrUnavailable`, skip to next with toast.
- Search results with no duration / no album / multiple artists / "Various Artists".
- Playlists > 5,000 tracks → continuation pages streamed into the table progressively.
- Clock skew affects SAPISIDHASH only loosely (timestamp in seconds); not validated server-side tightly.

## Test plan

- Fixtures: recorded InnerTube JSON responses in `internal/ytmusic/testdata/responses/` (scrubbed:
  `responseContext`, tracking params removed),
  one per renderer shape incl. odd ones. Golden tests: fixture → normalized structs (JSON goldens).
- `httptest.Server` tests for headers (SAPISIDHASH correctness against a known vector), retries,
  401 renewal path, rate limiter.
- Live smoke tests behind `//go:build live` + env `YTM_LIVE=1` (not in CI): search, charts, home.
- Resolver: fake yt-dlp is the test binary re-executed (`FAKE_YTDLP=mode`, portable to Windows,
  replacing the draft's shell script in `testdata/bin/`); `Runner` injection for args assembly per
  config option; scrubbed real `-O` output in `internal/stream/testdata/`; expiry parsing,
  cancellation kills the process, prefetch reordering, dedupe/limit/retry with a fake clock.
- Live (`-tags live`, `YTM_LIVE=1`): resolve all qualities + ranged GET; real mpv plays a resolved
  track and advances gaplessly into a prefetched, appended one.

## Open questions

1. ~~Primary cookie extractor~~ — yt-dlp shell-out without a URL (M2, see above).
2. ~~`hl`~~ — fixed to `en`, no new key (owner accepted defaults 2026-11-16). Parsers rely on English
   labels only for view counts; types come from page types / browse ids, so another `hl` would
   degrade views only.
