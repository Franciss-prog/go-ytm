# ytm-go

A YouTube Music player for the terminal, written in Go.

Vim keys, mouse support, cover art, a spectrum visualizer, and colors that follow your
wallpaper (pywal / matugen). It ships as one binary: no Python, no venv.

It is a Go rewrite of [ytm-player](https://github.com/peternaame-boop/ytm-player) (Python/Textual,
MIT). The goal is the same features without the lag.

**Status: early, but it plays music.** You can search, browse your library, build a queue and
listen. There are no releases yet: build it from source. See [What works](#what-works) and
[Not done yet](#not-done-yet).

## Quick start

You need:

- Go 1.26 or newer (to build)
- [mpv](https://mpv.io/) (plays the audio)
- [yt-dlp](https://github.com/yt-dlp/yt-dlp) plus a JavaScript runtime it can use, such as
  [deno](https://deno.com/) (yt-dlp needs one to unlock YouTube's audio links)
- Optional: `pw-cat` from PipeWire, for the spectrum visualizer (Linux only)
- Optional: kitty or Ghostty, for sharp cover art (other terminals get half-block art)

```sh
git clone https://github.com/Franciss-prog/go-ytm.git
cd go-ytm
make build          # builds ./bin/ytm
./bin/ytm doctor    # checks mpv, yt-dlp, config and your terminal
./bin/ytm           # starts the player
```

Searching works without an account. To get your library, playlists and likes, sign in once:

```sh
./bin/ytm setup     # reads your YouTube Music session from a browser you are logged in to
```

`setup` uses `yt-dlp --cookies-from-browser`, so it supports the browsers yt-dlp supports
(Firefox, Chrome, Chromium, Brave, Edge, Vivaldi, Opera, Zen, Helium).

## What works

Pages (switch with the tab bar or the keys below):

- **Search**: songs, videos, albums, artists, playlists, with suggestions.
- **Library**: Miller columns, like ranger or yazi: Playlists / Albums / Artists, then the
  items, then their tracks.
- **Queue**: reorder, remove, shuffle, repeat, filter and sort.
- **Track** (`g n`): a centered "now playing" card with cover art, title, artist, album, time,
  volume, a spectrum visualizer and a progress bar you can click to seek.
- **Browse** (`g b`): For You, Charts (with a country picker), New releases and Playlists.
- **Liked Songs** (`g y`) and **Recently Played** (`g r`, local and YouTube Music history).
- Album, playlist and artist pages, a track actions popup (`a`), add to playlist (`A`),
  like/unlike (`l`), and a playlist sidebar (`Ctrl+E`).

Playback:

- Gapless playback. The next track is resolved and loaded into mpv before the current one ends.
- **Hover prefetch.** When the cursor rests on a track for 0.4 s, ytm starts resolving it, so
  Enter plays almost at once. Without it, a new track takes about 10 s to start, because
  YouTube needs roughly 5 s to hand out the audio link and then blocks it for about 3 s more.
- Radio from any track (actions popup), Liked Songs radio, and a discovery mix (`D`).
- Autoplay: when the queue runs out, related tracks are added (`autoplay = true`).
- mpv crash recovery: mpv restarts and resumes where it was.

Looks:

- Cover art as real images (kitty graphics protocol) or half blocks, `Ctrl+A` to toggle.
- Themes from pywal, matugen, your terminal's own 16 colors, built-ins (`ytm-dark`,
  `ytm-light`, `nord`, `gruvbox-dark`) or your own `theme.toml`.
- Live reload: change your wallpaper and the player recolors itself, without stopping playback.
- Transparent mode (`Ctrl+T`): the player paints no backgrounds, so your terminal's opacity and
  blur show through.
- Contrast guard: colors that would be hard to read are adjusted automatically.
- Settings popup (`,`) to change theme, transparency and more while it runs.

## Keys

Vim-style, with counts (`5j`) and chords (`g s`). Press `?` in the player for the full list.
Every key can be rebound in `keymap.toml`.

| Key | Action |
| --- | --- |
| `j` / `k`, arrows | move down / up |
| `enter` | play / open |
| `g g` / `G` | top / bottom |
| `/` | filter the list |
| `backspace` | back |
| `tab` | next panel |
| `g s` / `g l` / `z` / `g n` | Search / Library / Queue / Track page |
| `h` / `right` | Library: previous / next column |
| `space` | play / pause |
| `n` / `p` | next / previous track |
| `<` / `>` | seek back / forward |
| `-` / `+` / `_` | volume down / up / mute |
| `Ctrl+S` / `Ctrl+R` | shuffle / repeat |
| `Z` / `X` | add to queue / play next |
| `v` / `V` | mark / mark range |
| `a` | track actions |
| `,` | settings |
| `q` | quit |

The mouse works too: click rows, right-click for actions, scroll, and click or scroll the
progress bar to seek.

## Command line

```sh
ytm                          # start the player
ytm search "daft punk"       # search from the shell
ytm search "daft punk" --json | jq '.[0]'
ytm setup                    # sign in from your browser
ytm doctor                   # diagnose mpv, yt-dlp, config, terminal
ytm theme show               # print the active theme and any contrast fixes
ytm config check             # validate config.toml
ytm config path              # where the config lives
ytm config defaults          # print every setting with its default
```

Output is styled on a terminal and plain when piped. `--json` and `NO_COLOR` are respected.

## Configuration

Files live in `~/.config/ytm-go/` (`$XDG_CONFIG_HOME` is respected; `%APPDATA%\ytm-go\` on
Windows). None of them are required.

| File | What it holds |
| --- | --- |
| `config.toml` | playback, UI, theme, yt-dlp options |
| `keymap.toml` | your key overrides |
| `theme.toml` | your own colors (with `source = "file"`) |

Run `ytm config defaults` to see every setting. A few useful ones:

```toml
[playback]
default_volume = 80
prefetch_on_hover = true   # resolve the track under the cursor ahead of time

[ui]
album_art_mode = "auto"    # auto | kitty | blocks | off
visualizer = true          # spectrum on the Track page (Linux, needs pw-cat)
show_sidebar = false

[theme]
source = "pywal"           # static | pywal | matugen | ansi | file
transparent = true
watch = true               # recolor live when the palette file changes
enforce_contrast = true
fallback = "ytm-dark"      # used when the palette file is missing or broken
```

### pywal

Set `source = "pywal"` and run `wal -i <wallpaper>` as usual. ytm reads
`~/.cache/wal/colors.json`. To pick which color goes where, use `[theme.pywal_map]`, for example
`accent = "color5"`.

### matugen

1. `cp contrib/matugen/ytm-go.toml ~/.config/ytm-go/matugen-template.toml`
2. Add this to `~/.config/matugen/config.toml`:

   ```toml
   [templates.ytm-go]
   input_path = '~/.config/ytm-go/matugen-template.toml'
   output_path = '~/.config/ytm-go/matugen-colors.toml'
   ```

3. Set `source = "matugen"` and run matugen as usual. No post hook is needed: ytm watches the
   output file.

### Transparent terminal

A terminal program cannot make itself see-through: your terminal emulator controls that. Set the
opacity there (kitty: `background_opacity 0.85`, alacritty: `[window] opacity = 0.85`,
WezTerm: `window_background_opacity = 0.85`), then turn on `transparent = true` or press
`Ctrl+T`.

### When colors look wrong

- `ytm doctor` shows which palette file is used and whether live reload is on.
- `ytm theme show` lists every color the contrast guard changed.
- `kill -USR1 $(pgrep -x ytm)` forces a theme reload.

## Not done yet

- Resume the last session, offline downloads and the audio cache
- Synced lyrics
- Controlling a running player from the shell (`ytm play`, `ytm next`, ...)
- MPRIS / media keys, notifications, Discord, Last.fm, Spotify import
- Releases and packages (AUR, Homebrew, ...)

The plan, milestone by milestone, is in [docs/PROGRESS.md](docs/PROGRESS.md), and the design
specs are in [docs/specs/](docs/specs/).

## How it works

```
 Bubble Tea UI  <-- messages -->  player core  <-- JSON IPC -->  mpv
       |                              |
       |                       stream resolver  -->  yt-dlp
       |                              |
  theme watcher               YouTube Music API (InnerTube)
       |
  pywal / matugen files
```

- **No cgo.** mpv runs as a separate process, controlled over its JSON IPC socket.
- **The UI never waits.** Network, disk and mpv calls run in the background and report back as
  messages, so a slow request never freezes a keypress.
- **Fakes for everything.** mpv, YouTube Music, yt-dlp and the clock sit behind interfaces, so the
  tests run without network or audio.

Code layout:

```
cmd/ytm/            entry point
internal/ui/        Bubble Tea pages, popups, frame
internal/player/    mpv process and JSON IPC
internal/ytmusic/   YouTube Music (InnerTube) client
internal/stream/    yt-dlp resolver, URL cache, prefetch
internal/theme/     palettes, contrast guard, live reload
internal/art/       cover art fetch, kitty graphics
internal/viz/       spectrum analyzer
internal/config/    config.toml loading and validation
internal/keys/      keymap, chords, counts
internal/cli/       the ytm command
docs/specs/         design specs
```

## Development

```sh
make build   # ./bin/ytm
make test    # go test -race ./...
make lint
make run
```

Read `CLAUDE.md` for the project rules: one PR per milestone, Conventional Commits, tests and
lint must pass.

## Disclaimer

ytm-go is an unofficial client. It is not affiliated with or endorsed by YouTube or Google. It
relies on undocumented YouTube Music endpoints and on yt-dlp, so it can break when YouTube changes
things. Use your own account, at your own risk, and respect YouTube's terms of service.

## Credits and license

- Modeled on [ytm-player](https://github.com/peternaame-boop/ytm-player) by peternaame-boop and
  contributors (MIT).
- The look takes ideas from kew and ytkew screenshots. No code was copied from them.
- Built with [Charm](https://charm.sh) (Bubble Tea, Lip Gloss, Bubbles, Huh), mpv and yt-dlp.

Released under the [MIT License](LICENSE).
