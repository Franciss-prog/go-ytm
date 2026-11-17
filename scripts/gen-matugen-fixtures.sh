#!/usr/bin/env bash
# Regenerates internal/theme/testdata/matugen/*.toml by rendering
# contrib/matugen/ytm-go.toml with the real matugen (4.x). A role matugen
# does not provide makes the render fail, which is the check (spec 08).
# Uses a throwaway config so the user's own matugen templates never run.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
out="$root/internal/theme/testdata/matugen"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$out"
for mode in dark light; do
	cat >"$tmp/config.toml" <<CFG
[config]
[templates.ytm-go]
input_path = '$root/contrib/matugen/ytm-go.toml'
output_path = '$out/$mode.toml'
CFG
	matugen -c "$tmp/config.toml" -m "$mode" color hex "#ff4e45" >/dev/null
	echo "wrote $out/$mode.toml ($(matugen --version))"
done
