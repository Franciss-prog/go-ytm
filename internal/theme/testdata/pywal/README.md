# pywal fixtures

- `full-16.json`, `dark-vivid.json`, `low-saturation.json`, `very-light.json`: pywal 3.x built-in
  schemes (`pywal/colorschemes/{dark,light}/base16-{default,dracula,grayscale,github}.json`, MIT),
  copied verbatim.
- `matugen-pywalfox-sparse.json`: the owner's real `~/.cache/wal/colors.json` (2026-11-16), written
  by matugen's `pywalfox-colors.json` template: no `special`, most `colorN` empty. Wallpaper path
  scrubbed.

`*.golden.toml` are the mapped themes; regenerate with `go test ./internal/theme -update`.
