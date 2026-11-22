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

export GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux
shopt -s nullglob
archives=()
for arch in amd64 arm64; do
    export GOARCH=$arch
    name="whwmpc_${version}_linux_${arch}"
    package="$stage/$name"
    mkdir -p "$package/LICENSES"
    go build -mod=readonly -trimpath -buildvcs=false -ldflags="-s -w -X main.version=$version" \
        -o "$package/whwmpc" ./cmd/whwmpc
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

    archive="$name.tar.gz"
    tar -C "$package" --sort=name --owner=0 --group=0 --numeric-owner --mtime=@0 -czf "$stage/$archive" .
    archives+=("$archive")
done

(
    cd -- "$stage"
    sha256sum "${archives[@]}" > SHA256SUMS
    sha256sum -c SHA256SUMS
    mv -- "${archives[@]}" SHA256SUMS "$out/"
)
