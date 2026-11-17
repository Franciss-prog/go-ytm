# 04 — Config, keymap, theme files

Status: **M4 keymap engine, M4b config.Patch implemented** · Milestones: M0 (loader), M4 (keymap engine, static themes), M4b (theme sources)

## Goals

- Same sections and defaults as the original `config.toml` wherever they make sense, under
  `~/.config/ytm-go/`. Unknown keys warn (don't fail). Bad values clamp + warn with **file:line:col**.
- Keymap: same action names and default bindings as the original, plus chords and count prefixes.
- Theme: one `[theme]` section (new) feeding the semantic palette (see 08).
- Writes (Settings popup, `ytm config set`) preserve comments, ordering and unrelated keys.

## Non-goals

- YAML/JSON config. Env-var overrides for every key (only `YTM_GO_CONFIG_DIR` and standard ones).

## Loading

1. Defaults (Go struct literal, single source of truth; `ytm config defaults` prints it as TOML).
2. Decode `config.toml` with `go-toml/v2` into a **raw** struct (pointers = "set by user").
3. Validate + clamp → `Config` + `[]Diagnostic{File, Line, Col, Key, Severity, Msg}`.
   Line/col: `DecodeError.Position()` for syntax/type errors; for semantic errors we locate the key's
   node via `unstable.Parser` byte ranges → line/col.
4. Diagnostics: printed by `ytm doctor` and `ytm config check`; in the TUI, one non-blocking toast
   "config: 2 warnings — run `ytm config check`". Syntax error in `config.toml` → defaults + error toast;
   the app still starts.

### Writes (comment-preserving)

`config.Patch(path, []Change{{Key: "theme.transparent", Value: true}})`:
parse with `unstable.Parser` (`KeepComments = true`), find the value node's byte `Range` and splice
the new literal; if the key is missing, append `key = value` at the end of its table (create the
table at EOF if missing). Atomic write (temp + rename, keep mode). Round-trip tests: comments,
blank lines, inline tables, dotted keys, CRLF files, missing table, missing file.

Implementation (M4b): strings are written as basic `"…"` strings (TOML escapes only); a key inside an
inline table is refused ("edit it by hand") rather than rewritten; the patched bytes are re-parsed
before the rename, so a patch never leaves an invalid file.

## `config.toml` schema

Original sections kept verbatim unless noted. "≈" = semantics differ from original.

```toml
[general]
startup_page = "library"        # library|search|browse|queue|liked_songs|recently_played|help
brand_account_id = ""           # 21-digit brand account id
check_for_updates = true        # ≈ checks GitHub releases (M12), once per 24 h
# playback_bar_position: original had no effect → dropped (accepted, ignored, warned)

[playback]
audio_quality = "high"          # high|medium|low
default_volume = 80             # 0..100 (clamped)
autoplay = true
gapless = true
seek_step = 5                   # 1..300 s
api_timeout = 15                # 1..120 s
resume_on_launch = true
history_min_listen_seconds = 5  # 0..3600
sync_history_to_ytmusic = true
mpv_config = false              # NEW: load the user's mpv.conf (default: mpv runs with --no-config)
mpv_args = []                   # NEW: extra mpv arguments
# prefer_audio: no effect in original → dropped (accepted, ignored)

[cache]
max_size_mb = 1024              # 0 disables audio cache; min 64 otherwise
location = ""                   # blank = platform cache dir
# enabled / prefetch_next: no effect in original → accepted, ignored

[yt_dlp]
cookies_file = ""
use_session_cookies = false
cookie_browser = ""             # helium|chrome|chromium|brave|firefox|zen|edge|vivaldi|opera
ca_bundle = ""
remote_components = ""
js_runtimes = ""
binary = ""                     # NEW: path to yt-dlp; blank = PATH lookup

[search]
default_mode = "music"          # music|all
max_history = 500               # ≈ enforced (original ignored it)
predictive = true

[ui]
album_art_mode = "blocks"       # NEW: blocks|off   (old `album_art = true/false` accepted as alias)
icons = "unicode"               # NEW: unicode|nerd|ascii
progress_style = "block"        # block|line
sidebar_width = 30              # 16..80
lyrics_width = 36               # 24..80
col_index = 4                   # 0 = auto
col_title = 0
col_artist = 0
col_album = 0
col_duration = 8
bidi_mode = "auto"              # auto|reorder|passthrough
region = "ZZ"                   # ISO 3166-1 alpha-2 or ZZ; "ES-ES" → "ES"
home_shelves = 3                # 1..25
show_selection_info = true
sidebar_overflow = "truncate"   # truncate|wrap
show_queue_source = true
mouse = true                    # NEW: allow disabling mouse capture (terminal text selection)
# theme = "..." : original key → accepted as alias of [theme].name with a deprecation warning

[theme]                         # NEW section, see 08
source = "static"               # static|pywal|matugen|ansi|file
name = "ytm-dark"
transparent = false
transparent_selection = "bar"   # bar|reverse|underline|solid
popup_fill = false
enforce_contrast = true
watch = true
pywal_path = ""                 # blank = ~/.cache/wal/colors.json
matugen_path = ""               # blank = <config>/matugen-colors.toml
file = ""                       # used when source = "file"; blank = <config>/theme.toml
fallback = "ytm-dark"
color_depth = "auto"            # NEW: auto|truecolor|256|16 (override detection)

[keys]
chord_timeout_ms = 1000         # 100..5000; pending chord timeout

[notifications]
enabled = true
timeout_seconds = 5
format = "{title} — {artist}"   # placeholders: {title} {artist} {album}

[mpris]
enabled = true

[discord]
enabled = false
client_id = ""

[lyrics]
transliteration = false

[lastfm]
enabled = false
api_key = ""
api_secret = ""
session_key = ""
username = ""
password_hash = ""

[logging]
level = "WARNING"               # DEBUG|INFO|WARNING|ERROR|CRITICAL (mapped to slog levels)
max_bytes = 5242880
backup_count = 3
keep_crashes = 10
```

Defaults table = above values. Optional-feature "requires pip extra" notes disappear: everything is
compiled in.

## `keymap.toml`

```toml
[keys]
like_toggle = ["C-l"]           # list of bindings; a string is accepted too
toggle_transparency = []        # empty list = unbound
```

### Key grammar

- Single key: `a`, `A`, `space`, `enter`, `escape`, `tab`, `backspace`, `delete`, `up`, `down`,
  `left`, `right`, `home`, `end`, `page_up`, `page_down`, `f1`..`f12`, punctuation (`+ - _ . , / ? < > ^`).
- Modifiers: `C-` Ctrl, `S-` Shift, `M-` Alt, combinable (`C-S-tab`).
- Sequence (chord): space-separated: `"g l"`, `"C-w h"`, `"d d"`, `"s A"`.
- Count prefix: digits typed before an action (`15J`, `5j`); `0` alone is not a count start.
  Count applies to count-aware actions (`move_*`, `page_*`, `reorder_*`, `seek_*`, `volume_*`);
  others ignore it. Max count 9999.
- Pending state times out after 1000 ms (configurable `[keys] chord_timeout_ms`? → open question);
  a non-matching key cancels the chord and is then processed normally. Pending chord shown in the
  status area (`g-`, `15`).
- Conflicts: a binding that is a strict prefix of another (e.g. `g` vs `g l`) → the longer wins,
  load emits a warning. Duplicate key across actions → later action in file wins, warning.

Implementation (M4): canonical tokens put modifiers in the order `C-`, `M-`, `S-`; shifted
printables are written as the character (`S-a` → `A`, `S-/` is just `?`), and `C-A` becomes
`C-S-a`. A user entry for an action **replaces** its defaults (an empty list unbinds); a user
binding that takes a default key of another action is an info diagnostic, a duplicate between two
user entries is a warning (later in file wins), a strict prefix is dropped with a warning. Counts
apply only to counted actions (`move_*`, `page_*`, `go_top/bottom`, `reorder_*`, `seek_*`,
`volume_*`); `5 g g` = go to row 5 (open question 3 resolved: yes). The dispatcher has no clock: the
TUI arms the timeout and passes back a sequence number, so stale timers are ignored.

Trie over normalized key tokens in `internal/keys`; the TUI root dispatches
`Action{Name, Count}` to the focused page, then to global handlers. Text inputs (search box, filter)
capture keys first; `escape` leaves them.

### Default bindings (identical to original)

| Action | Keys | Action | Keys |
|---|---|---|---|
| play_pause | `space` | like_toggle | `l` |
| next_track | `n` | play_next | `X` `C-x` |
| previous_track | `p` | add_to_queue | `Z` `C-z` |
| play_random | `.` | add_to_playlist | `A` |
| cycle_repeat | `C-r` | delete_item | `delete` `d d` |
| toggle_shuffle | `C-s` | track_actions | `a` |
| volume_up / volume_down | `+` / `-` | mark_toggle / mark_range / mark_clear | `v` / `V` / `escape` |
| mute | `_` | discovery_mix | `D` |
| seek_forward / seek_backward | `>` / `<` | filter | `/` |
| seek_start | `^` | pick_country | `c` |
| move_down | `j` `down` `C-n` | reorder_down / reorder_up | `J` / `K` |
| move_up | `k` `up` `C-p` | sort_title/artist/album/duration/date | `s t` `s a` `s A` `s d` `s D` |
| page_down / page_up | `C-f` `page_down` / `C-b` `page_up` | reverse_sort | `s r` |
| go_top / go_bottom | `g g` `home` / `G` `end` | toggle_search_mode | (unbound) |
| select | `enter` | library / search / browse | `g l` / `g s` / `g b` |
| focus_next / focus_prev | `tab` / `S-tab` | liked_songs / recently_played | `g y` / `g r` |
| focus_pane_left/right/cycle | `C-w h` / `C-w l` / `C-w w` | lyrics | `g L` |
| go_back / go_forward | `backspace` / `S-backspace` | current_context / jump_to_current | `g space` / `g c` |
| quit | `q` `C-q` | queue | `z` |
| toggle_sidebar | `C-e` | help | `?` `C-h` |
| toggle_transliteration | `T` | toggle_album_art | `C-a` |

### New actions (need your approval — public behavior)

| Action | Proposed default | Why this key |
|---|---|---|
| `settings` | `,` | Prompt-specified. `,` is unbound in the original. |
| `toggle_transparency` | `C-t` | Unbound in original; `t`/`T` taken (`T` = transliteration). Ctrl+T is not intercepted by common terminals in non-tab contexts. Rebindable. |
| `command_palette` | `:` | Replaces Textual's `Ctrl+P` palette (which the original used for theme switching). `C-p` stays `move_up` as in the original. |
| `theme_picker` | `g t` | Quick static-theme switcher (original: `C-p` → Theme). |
| `retry` | `r` | M5: the "retry button `r`" of spec 05's API-error empty state, as a bindable action. `r` is unbound in the original (only `C-r`, `s r`, `g r`). |

## `theme.toml` (source = "file", and per-token overrides)

```toml
# Full palette (source = "file") — every token optional except bg/fg/accent; missing ones derived.
[colors]
bg = "#0f0f0f"
fg = "#f1f1f1"
accent = "#ff4e45"
# surface, surface_alt, border, accent_dim, muted, selected_bg, selected_fg, progress_filled,
# progress_empty, lyrics_played, lyrics_current, lyrics_upcoming, error, warn, success

# Overrides applied on top of ANY source (pywal/matugen/static…):
[overrides]
lyrics_current = "#ff4e45"
```

Values: `#rgb`, `#rrggbb`, `ansi:0..15`, `default` (terminal default fg/bg). Original
`[colors]` keys (`playback_bar_bg`, `selected_item`, `active_tab`, `inactive_tab`, …) are accepted as
aliases (`selected_item` → `selected_bg`, `playback_bar_bg` → `surface`, `active_tab` → `fg`,
`inactive_tab` → `muted`), with a deprecation note in `config check`.

## Edge cases

- File missing → defaults, no warning. File not UTF-8 / BOM → BOM stripped, invalid UTF-8 → error w/ line.
- Wrong type (`default_volume = "loud"`) → error at line:col, default used.
- Out-of-range (`default_volume = 150`) → clamped to 100, warning.
- Unknown section/key → warning (typo hint via Levenshtein ≤ 2: "did you mean `seek_step`?").
- Windows paths in TOML (`'C:\path'` literal strings) supported; `~` expanded on all OSes.
- Keymap binding to unknown action → warning; invalid key token → warning, binding skipped.

## Test plan

- Table tests: every key, valid/invalid/clamp; golden diagnostics output with line:col.
- Patch round-trip golden files (input.toml + changes → expected.toml) incl. comments/CRLF.
- Keys: parser table tests; trie dispatch with chords, counts (`15J`, `g g`, `5 g g`?), timeouts
  (fake clock), prefix conflict warnings; fuzz test for the key parser.
- `ytm config defaults | ytm config check -` round-trip has zero diagnostics.

## Open questions

1. Approve new actions/keys: `settings=,`, `toggle_transparency=C-t`, `command_palette=:`, `theme_picker=g t`.
2. Approve new config keys: `[theme]` section, `[ui] icons`, `[ui] album_art_mode`, `[ui] mouse`,
   `[yt_dlp] binary`, `[theme] color_depth`, `[keys] chord_timeout_ms` (default 1000).
3. Count before chord (`5 g g` → go to row 5, vim-style)? Default: yes for `go_top`/`go_bottom`.
