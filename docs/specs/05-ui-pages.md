# 05 — UI pages, components, layout

Status: **M4 implemented** (shell, bar, help, toasts), **M5 implemented** (pages, popups, playlist sidebar, mouse), **M6 implemented** (browse, liked, recent, radio), **M7 implemented** (search history, Download, shuffle lock, session resume) · Milestones: M4 (shell, bar, help), M5, M6, M8

## Goals

- Pages and behavior matching the original: Library, Search, Browse, Context (album/artist/
  playlist), Queue, Liked Songs, Recently Played, Help; playlist + lyrics sidebars; playback bar;
  popups (actions, playlist picker, create playlist, confirm, input, country picker, Spotify import,
  **Settings** new, **command palette** new, **theme picker** new).
- 60 fps scrolling over 1,000+ rows; every view uses theme tokens only and passes the transparent
  render test (see 08).

## Non-goals

- Pixel-perfect copies of Textual widgets. Image protocols (kitty/sixel) for album art (possible later).

## Layout

> **Superseded by spec 09 (M5b, ytkew look)** for the frame (tab bar, titled panels, mini player,
> hint footer), TrackTable row style, the Library page (Miller columns) and the new track page.
> The sections below describe the M4/M5 layout and stay valid where 09 says nothing.

```
┌ header: ← Back  Library  ·  g- (pending chord)                     Forward → ┐
├──────────────┬───────────────────────────────────────────┬──────────────────┤
│ Playlists    │ main page                                  │ Lyrics (g L)     │
│ (C-e)        │                                            │                  │
│ width =      │                                            │ width = 36       │
│ sidebar_width│                                            │                  │
├──────────────┴───────────────────────────────────────────┴──────────────────┤
│ selection info: full title of focused row                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│ playback bar (3 rows; album art 6×3 cells when enabled)                     │
└─────────────────────────────────────────────────────────────────────────────┘
```

Breakpoints: < 100 cols hides lyrics sidebar (toggle overlays instead); < 80 hides playlist sidebar
and chart "Featured globally" row; < 60 compact bar (no art, 1 row). Height < 12 → bar only + list.

### Rendering strategy (perf)

- Each component caches its rendered string keyed by `(version, width, height, themeVersion)`;
  `View()` concatenates cached blocks. Only dirty components re-render (progress tick touches only
  the bar's time segment).
- Track table renders only visible rows (virtualized); row strings cached per (row, width, selected,
  marked, playing, themeVersion). 1,000-row scroll = O(visible) work.
- Progress tick: 4 Hz while playing, 0 Hz when paused/idle (idle CPU < 1 %).

## Shared component: TrackTable

Columns: mark gutter │ # │ Title │ Artist │ Album │ Duration (widths from `[ui] col_*`, 0 = flex).
Features: cursor, marks (`v`), range marks (`V`), clear (`escape`), filter (`/`, fuzzy-subsequence,
case-insensitive, matches title/artist/album), sort (`s t/a/A/d/D`, `s r`), playing indicator (▶),
autoplay marker (↻), unavailable rows dimmed, progressive loading footer ("Loading… 400/1,200"),
right-click → actions popup, click mark column → toggle mark, Ctrl-click / Shift-click marks.
Selection styles honor `transparent_selection` (see 08).

```
  #  Title                         Artist              Album              Time
  1  Get Lucky                     Daft Punk           Random Access…     6:09
▌ 2  Instant Crush                 Daft Punk, Julian…  Random Access…     5:37   ← cursor (bar mode)
● 3  Lose Yourself to Dance        Daft Punk           Random Access…     5:53   ← marked
▶ 4  Giorgio by Moroder            Daft Punk           Random Access…     9:04   ← playing
  /dance█                                         filter: 2 of 13 · sort: title ↑ · 1 marked
```

## Pages

### Library (`g l`)
Tabs: Playlists · Albums · Artists. Selecting a playlist opens it in-page (header with title,
count, total duration, `Shuffle lock: ON/off` toggle, `[▶ Play] [⤮ Shuffle] [◉ Radio]`).
```
 Playlists  Albums  Artists
 ┌──────────────────────────────────────────────┐
 │ Liked Music                     1,204 songs  │
 │ ▌Late night drives                 87 songs  │
 │ Focus                              43 songs  │
 └──────────────────────────────────────────────┘
```

### Search (`g s`)
Input with predictive suggestions (debounced 150 ms, stale responses dropped); mode toggle
music/all; results grouped: Top result card, Songs (TrackTable), Albums, Artists, Playlists,
Videos (all mode). `tab` cycles result panels. Search history (SQLite) shown when input is empty.
```
 Search ▸ daft punk█                                   mode: music
 ┌ suggestions ─────────┐
 │ daft punk get lucky  │
 │ daft punk around the │
 └──────────────────────┘
 Top result: Daft Punk · Artist                     Songs
 Albums: Discovery · Homework · RAM            ▌1 One More Time   5:20
 Playlists: This Is Daft Punk                   2 Around the W…   7:09
```

### Context page (album / artist / playlist)
Album: header (title, artist, year, track count) + TrackTable. Artist: header + Top songs +
Albums/Singles shelves + "Discography" (artist albums continuation). Playlist: same as Library
playlist view, with continuation loading.

### Browse (`g b`)
Tabs: For You (home shelves, `[ui] home_shelves`) · Charts (region picker `c`, shelf pills:
Featured globally row + country charts) · New Releases · Playlists (up to 25).
```
 For You   Charts   New releases   Playlists                 Region: Global (c)
 Featured globally:  [Coachella 2026]  [Top 100 Music Videos]
 Charts:  [Top 100 Songs]  Trending 20   Weekly Top Songs on Shorts
 ──────────────────────────────────────────────────────────────
 TrackTable…
```

### Queue (`z`)
TrackTable in playback order (shuffle-aware), "Generated from: <source>" header when radio/discovery
(`show_queue_source`), `J/K` reorder with counts, `delete`/`d d` remove, ↻ autoplay rows; removing an
autoplay pick blocks it from being re-added until queue clear/exit.

### Liked Songs (`g y`) & Recently Played (`g r`)
Liked: TrackTable + `[▶ Start Radio]` (5 random seeds). Recently Played: tabs All · Local · YT Music
with the one-line explanation under the tab row, exact semantics per original docs (100+100 rows,
merge by videoID keeping local position, partial-failure footer).

### Help (`?`)
Generated from the live keymap (so rebinds show), grouped by category, searchable with `/`.
Prose sections rendered with Glamour; links to Settings (`,`).

### Sidebars
- Playlists (`C-e`, `C-w h`): library playlists, `j/k`, `enter` opens, right-click actions,
  overflow truncate/wrap.
- Lyrics (`g L`): synced lines; `lyrics_played/current/upcoming` tokens; current line centered;
  auto-scroll pauses for 5 s after manual scroll; `C-f/C-b` scroll 10 lines; "No lyrics" state;
  source label (YouTube / LRCLIB); transliteration (`T`) and bidi per `[ui] bidi_mode`.

### Playback bar
```
 ▗▄▖  Instant Crush                                  ♥   ⤮  ⟳1   ◁◁  ▶  ▷▷
 ▐█▌  Daft Punk, Julian Casablancas · Random Access Memories
 ▝▀▘  2:13 ━━━━━━━━━━━━━━━━━━━━╸─────────────────── 5:37            vol 80%
```
Click progress → seek; scroll on progress → scrub (commits after 0.6 s); scroll on volume → ±5;
click ♥ / ⤮ / ⟳ toggles. Buffering shows spinner in place of ▶.

### Popups
Centered, max 60 × 20, border in `border` token, no fill unless `popup_fill` (08). `escape` closes.
- Actions: Play, Play next, Add to queue, Add to playlist, Start radio, Go to album, Go to artist,
  Like/Unlike, Download for offline, Remove from playlist (when applicable), Copy link.
- Playlist picker (+ create new, duplicate detection like original), Confirm, Input, Country picker
  (68 regions + Global, fuzzy filter), Spotify import (M11).
- Settings (`,`) — see 08 §6. Command palette (`:`) — fuzzy list of all actions + "Theme: …",
  "Settings", "Reload theme".

### Toasts
Bottom-right above bar, max 3 stacked, 4 s, severity tokens (error/warn/success), non-blocking.

## Transliteration & BiDi (M8)

- Transliteration: pure-Go `golang.org/x/text/unicode/norm` + a compact table-based transliterator
  (Unidecode data, ~200 KB embedded) — dependency choice justified in M8 PR; candidates evaluated then.
- BiDi: `golang.org/x/text/unicode/bidi` for `reorder` mode; `auto` reorders only lines containing
  RTL runs when the terminal is not known to do BiDi itself; `passthrough` leaves text alone.

## Edge cases

- Wide chars (CJK, emoji) → width via `charmbracelet/x/ansi` string width; truncation never splits graphemes.
- Empty states: not authenticated, offline, empty playlist, no results, API error (retry button `r`).
- Navigation stack max 20 (as original), state (cursor, scroll, filter, sort) restored on back/forward.
- Theme reload while a popup is open / during filter typing → state untouched (08).

## Test plan

- teatest-based golden snapshots per page at 80×24 and 140×40, opaque and transparent, NO_COLOR.
- Key-dispatch tests per page (actions → expected model changes), mouse event tests.
- Benchmarks: `BenchmarkTrackTableView1000`, `BenchmarkScroll1000`, `BenchmarkPlaybackBarTick`.
- Transparent-render test registry: each page registers a constructor; test renders every page and
  asserts no `48;` SGR (see 08).

## Implementation notes (M4)

- Packages: `internal/ui` (root model, layout, header, bar, toasts, Help, placeholder pages),
  `internal/keys` (grammar, trie, dispatcher), `internal/app` (composition root, shutdown, crash
  guard, `--trace-frames` wrapper). `internal/ui/components` and `pages` subpackages are deferred
  until M5 has more than one real page.
- Layout rows: header (1) · main · selection info (1) · rule (1) · bar (3). Breakpoints as above;
  the lyrics sidebar below 100 columns overlays the right side of the main area.
- **Root background:** opaque mode sets `tea.View.BackgroundColor/ForegroundColor` (terminal default
  colors via OSC 10/11, restored on exit) instead of painting cells: nested spans end in SGR reset,
  which cuts a parent fill short. Only single-line strips (header, info bar), solid selection and
  popups/toasts paint backgrounds. Transparent and NO_COLOR leave the terminal colors alone.
- Help is plain Lip Gloss text generated from the live keymap, filterable with `/`. **Glamour is not
  used** (no dependency added): the prose is three lines, and Glamour's built-in styles paint code
  backgrounds that the transparent test would have to exempt. Revisit if Help grows real prose.
- Actions whose features arrive later (queue, likes, radio, palette, theme picker, album
  art, lyrics) show an info toast "Not available yet: … arrives in a later version".
- `toggle_transparency` flips and persists `theme.transparent` via `config.Patch` (M4b). Settings
  (`,`) is an overlay over the main area that captures keys (`j/k` move, `h/l`/space change, enter
  saves changed keys, esc/`q`/`,` revert); it is in the transparent-render registry.
- Timers (toast expiry, chord timeout) use a context-aware `after` Cmd instead of `tea.Tick`, whose
  goroutine outlives the program by up to the tick length (found by goleak).
- Mouse in M4: header ← Back / Forward →, click on the progress bar seeks, wheel over the volume
  changes it by 5. Volume step 5 per `+`/`-` (× count).
- Tests: a scenario registry (`internal/ui/ui_test.go`) drives the transparent-render test (2 sizes ×
  3 selection modes), NO_COLOR test, width/height fit test (6 sizes) and goldens (plain text at 80×24
  and 140×40, styled opaque/transparent at 80×24, `testdata/TestGoldens/`). Goldens snapshot
  `View().Content` directly; one `teatest` test runs the real program loop. New pages must be added
  to the registry.

## Implementation notes (M5 popups)

- One popup at a time in the root model; a flow replaces it (picker → new playlist → done, picker →
  "Already in playlist" confirm) instead of stacking. Popups own raw keys; mouse input is ignored
  behind them. Async results are addressed to the popup instance and dropped once it closed.
- Actions popup items run the page's own key actions (`select`, `play_next`, `add_to_queue`,
  `delete_item`, `add_to_playlist`), so popup and keys behave the same. Track items: Play, Play next,
  Add to queue (Queue page: Remove from queue), Go to artist, Go to album, Add to playlist, Remove
  from playlist (owned playlists only), Like/Unlike, Copy link (OSC 52, `tea.SetClipboard`). Single-
  track items hide when rows are marked. **Start radio arrived in M6; Download for offline in M7**
  (works on marked rows too; spec 01, "Persistence").
  Library playlists: Open, Start radio, Toggle shuffle lock (M7), Delete playlist (owned only),
  Copy link.
- Playlist pages (M7) show a `⤮ Shuffle lock: ON/off` line under the panel title (accent when on);
  a click toggles it. Keyboard: the playlist's actions popup (`a` on a library playlist row).
- Playlist picker lists owned library playlists only (edits fail on others), filters by typed
  substring like the original, "+ New playlist" first. A refused add (`ErrEditFailed`) asks the
  original's "Already in …, add anyway?" and retries with duplicates allowed; a selection holding a
  song twice asks before sending. Recently-used ordering (original: a JSON file) is not ported yet.
- New playlist: name + privacy (private default, then public, unlisted); no description field.
  Created with the selected video ids (deduplicated) in one `CreatePlaylist` call.
- `delete_item` on an owned playlist page and on a library playlist asks to confirm. Edits never
  change the screen before YouTube Music confirms; the library list and an open playlist reload after.

## Implementation notes (M5 playlist sidebar)

- Layout as the original's `PlaylistSidebar`: pinned rows ♥ Liked Songs · ⏱ Recently Played ·
  ♫ Discovery Mix (highlighted when that page is shown; mouse targets — keyboard reaches them with
  `g y` / `g r` / `D`), a rule, the "Playlists" title, the library playlists **without Liked Music**
  (`LM`, pinned above), and a footer "N playlists" or "shown/total" while filtered. Pinned rows
  and the rule are dropped below 8 rows. Counts show the number only ("87"), the full count is on
  the selection info line.
- Focus: `C-w h` focuses the sidebar (showing it if hidden, toast when the window is < 80 columns),
  `C-w l` returns to the main area, `C-w w` cycles. While focused it takes `j/k`, paging,
  `g g`/`G`, `/` filter (fuzzy, owns raw keys while typing), `escape` clears the filter, `enter`
  opens the playlist (focus stays in the sidebar; the playlist already shown is not reopened),
  `a` actions popup, `delete`/`d d` delete (owned only, with confirm), `r` retry after an error.
  Everything else falls through to the page and then the global keys, as in the original.
  `C-e` hiding the sidebar returns focus to the main area.
- Loads once at start when shown (or when first shown), keeps the cursor on the same playlist
  across reloads, reloads after playlist creates/adds/removes, and drops a deleted playlist at
  once. Stale load answers are dropped (request sequence).
- `sidebar_overflow = "wrap"` wraps long names onto more rows (List `SetWrap`), the count on the
  last row when it fits; `truncate` cuts with "…" like before.

## Implementation notes (M5 mouse)

- Clicks route by region: header (← Back / Forward →), sidebar, main area, playback bar. Clicking
  a pane gives it keyboard focus. Popups own input: clicks and wheel behind them are ignored.
- Track tables, as the original's `track_table.py`: a click moves the cursor (and sets the range
  anchor); a click on the cursor row runs `select` (Textual raises RowSelected there); Ctrl-click or
  a click in the mark gutter toggles the mark; Shift-click marks from the anchor; right click moves
  the cursor and opens the actions popup; a header click sorts by that column (again: reverses;
  `#` = source order). Lists (library tabs, search panels, artist releases) work the same without
  marks; tab rows switch tabs; the search input row starts editing.
- Sidebar: a playlist click opens it at once (the original's `instant_select`), right click opens
  its actions; pinned rows navigate (Discovery Mix still toasts "not available yet").
- Bar: click on progress seeks; ♥ / ⤮ / ⟳ / ▶ glyphs run `like_toggle` / `toggle_shuffle` /
  `cycle_repeat` / `play_pause`; wheel on the volume ±5; wheel on progress scrubs 3 s per tick
  (up = forward), previewed on the bar and seeked 0.6 s after the last tick (original's
  `progress_bar.py` constants). A new track cancels a pending scrub.
- Wheel over the sidebar or main area moves that pane's cursor one row per event (terminals send
  several events per notch).
- The bar draws no ◁◁ / ▷▷ glyphs yet (the mock above has them); `n` / `p` remain the route.

## Implementation notes (M6)

- **Browse** (`g b`, tab "browse"): tabs For You · Charts · New releases · Playlists (`tab`
  cycles, mouse on the tab row). Each tab loads on first view; For You and Playlists share one
  `Home(home_shelves)` call. For You lists each shelf under a `── Title` heading row (8 items
  per shelf, as the original). **Charts differs from the mock above:** a list of the region's
  chart playlists, then "Top artists", instead of pill rows over a TrackTable; enter opens the
  chart as a normal playlist page (TrackTable, play, marks). "Featured globally" event shelves
  are just more rows. `c` opens the country picker (Global + 67 regions from ytm-player
  `regions.py`, typed filter), saves `[ui] region` with `config.Patch` and reloads the charts;
  answers for an older region are dropped. Rows: songs play (autoplay continues), albums,
  artists and playlists open their page, `RD…` mixes play their watch queue.
- **Liked Songs** (`g y`): the `LM` playlist page under its own id and title with a
  "▶ Start Radio" line (click: five random rows as seeds, label "Radio: Liked Songs").
  Keyboard radio is the actions popup's **Start radio** (cursor or marked rows); no new key.
- **Recently Played** (`g r`): All · Local · YT Music with the original's one-line
  explanations, "▶ Start Radio" as on Liked. Account history reloads each time the page is
  shown. All = local rows first in their order, then account-only rows, one row per video id
  (100 + 100). When the account history fails, All keeps the local rows and adds a footer
  "Account history unavailable: …". **Local** is the play log (recorded at
  `history_min_listen_seconds`, signed in or not), kept in SQLite since M7.
- Library root column gains Recently Played (account history as a track column), spec 09.
- **Radio**: actions popup **Start radio** on tracks (one seed, or the marked rows as a
  shuffled multi-seed mix) and on library playlists (`RDAMPL`). It replaces the queue with
  seeds + radio, clears the queue context (no shuffle lock key) and shows "Generated from:
  <label>" on the Queue page (`show_queue_source`). No suggestions → warning, queue untouched.
- **Autoplay** (`[playback] autoplay`, repeat off): when a track starts with ≤ 3 tracks left,
  a radio seeded by the last 5 played tracks is appended (↻, skipping queued and dismissed
  ids), once per queue at a time; at the end of the queue (natural end or `n`) it fetches and
  continues instead of stopping.
- **Discovery mix** (`D`, sidebar row): rotates charts → trending → home → liked → a library
  artist's top songs → history, starting after the last source that worked; three random
  seeds → radio labelled "Discovery (<source>)", then shows the Queue.
- **History sync**: once a track has played more than `history_min_listen_seconds` past where
  it was loaded (mpv position events, so terminal focus does not matter), it goes to the local
  log and, with `sync_history_to_ytmusic` and a session, to `AddHistoryItem` once. Failures are
  logged, not retried (position events arrive several times a second).

## Open questions

1. Lyrics sidebar default width 36 — add `[ui] lyrics_width`? (default yes, 24..80)
