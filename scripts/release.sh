#!/usr/bin/env bash
set -euo pipefail

version=${1:-snapshot}
out=${2:-dist}
if (( $# > 2 )) || [[ ! "$version" =~ ^(snapshot|v[0-9]+\.[0-9]+\.[0-9]+)$ ]]; then
    printf 'Usage: %s [snapshot|vX.Y.Z] [output-directory]\n' "$0" >&2
    exit 2
fi

mkdir -p -- "$out"
out=$(cd -- "$out" && pwd)
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
stage=$(mktemp -d "$out/.whwmpc.XXXXXXXX")
trap 'rm -r -- "$stage"' EXIT

umask 022
export GOTOOLCHAIN=local CGO_ENABLED=0 GOAMD64=v1 GOARM64=v8.0 LC_ALL=C
shopt -s nullglob
archives=()
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
    export GOOS=${target%/*} GOARCH=${target#*/}
    name="whwmpc_${version}_${GOOS}_${GOARCH}"
    executable=whwmpc
    if [[ "$GOOS" == windows ]]; then
        executable+=.exe
    fi
    package="$stage/$name"
    mkdir -p "$package/LICENSES"
    go build -mod=readonly -trimpath -buildvcs=false -ldflags="-s -w -X main.version=$version" \
        -o "$package/$executable" ./cmd/whwmpc
    cp README.md "$package/"
    if [[ -f LICENSE ]]; then
        cp LICENSE "$package/"
    fi
    cp "$(go env GOROOT)/LICENSE" "$package/LICENSES/Go"

    go list -mod=readonly -deps -f '{{with .Module}}{{if not .Main}}{{printf "%s\t%s" .Path .Dir}}{{end}}{{end}}' ./cmd/whwmpc \
        | sed '/^$/d' | LC_ALL=C sort -u > "$stage/modules"
    while IFS=$'\t' read -r module directory; do
        notices=("$directory"/LICENSE* "$directory"/COPYING* "$directory"/NOTICE* "$directory"/PATENTS*)
        if (( ${#notices[@]} == 0 )); then
            printf 'No license notice found for %s\n' "$module" >&2
            exit 1
        fi
        mkdir -p "$package/LICENSES/$module"
        cp -- "${notices[@]}" "$package/LICENSES/$module/"
    done < "$stage/modules"

    if [[ "$GOOS" == windows ]]; then
        archive="$name.zip"
        python3 - "$package" "$stage/$archive" <<'PYTHON'
from pathlib import Path
import sys
import zipfile

root = Path(sys.argv[1])
with zipfile.ZipFile(sys.argv[2], "w", compression=zipfile.ZIP_DEFLATED) as archive:
    for path in sorted(root.rglob("*")):
        if path.is_file():
            entry = zipfile.ZipInfo(path.relative_to(root).as_posix())
            entry.compress_type = zipfile.ZIP_DEFLATED
            entry.external_attr = path.stat().st_mode << 16
            archive.writestr(entry, path.read_bytes())
PYTHON
    else
        archive="$name.tar.gz"
        tar -C "$package" --sort=name --owner=0 --group=0 --numeric-owner --mtime=@0 -czf "$stage/$archive" .
    fi
    archives+=("$archive")
done

(
    cd -- "$stage"
    sha256sum "${archives[@]}" > SHA256SUMS
    sha256sum -c SHA256SUMS
    mv -- "${archives[@]}" SHA256SUMS "$out/"
)
