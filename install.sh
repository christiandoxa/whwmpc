#!/bin/sh
set -eu

fail() {
    printf 'whwmpc: %s\n' "$*" >&2
    exit 1
}

download() {
    curl -fsSL --proto '=https' --proto-redir '=https' "$@"
}

main() {
    [ "$#" -le 1 ] || fail "penggunaan: sh install.sh [vX.Y.Z]"
    repo=https://github.com/christiandoxa/whwmpc
    [ "$(uname -s)" = Linux ] || fail "installer ini khusus Linux"
    case "$(uname -m)" in
        x86_64) arch=amd64 ;;
        aarch64) arch=arm64 ;;
        *) fail "arsitektur tidak didukung: $(uname -m)" ;;
    esac
    for tool in curl sha256sum awk tar; do
        command -v "$tool" >/dev/null || fail "perintah diperlukan: $tool"
    done

    if [ "$#" -eq 1 ]; then
        tag=$1
    else
        latest=$(download -o /dev/null -w '%{url_effective}' "$repo/releases/latest")
        tag=${latest#"$repo/releases/tag/"}
    fi
    printf '%s\n' "$tag" | awk '/^v[0-9]+\.[0-9]+\.[0-9]+$/ { ok=1 } END { exit !(NR == 1 && ok) }' ||
        fail "tag rilis harus berbentuk vX.Y.Z"
    archive="whwmpc_${tag}_linux_${arch}.tar.gz"
    base="$repo/releases/download/$tag"

    bin_dir="$HOME/.local/bin"
    target="$bin_dir/whwmpc"
    mkdir -p -- "$bin_dir"
    if [ -L "$target" ] || { [ -e "$target" ] && [ ! -f "$target" ]; }; then
        fail "target harus berupa file reguler: $target"
    fi
    stage=$(mktemp -d "$bin_dir/.whwmpc.XXXXXXXX")
    trap 'rm -rf -- "$stage"' EXIT
    trap 'exit 1' HUP INT TERM

    download -o "$stage/$archive" "$base/$archive"
    download -o "$stage/SHA256SUMS" "$base/SHA256SUMS"
    (
        cd "$stage"
        awk -v archive="$archive" '
            NF == 2 && $2 == archive && length($1) == 64 && $1 !~ /[^a-fA-F0-9]/ {
                print $1 "  " archive; count++
            }
            END { if (count != 1) exit 1 }
        ' SHA256SUMS > checksum
        sha256sum -c checksum
        tar -xOzf "$archive" -- ./whwmpc > whwmpc
        test -s whwmpc
    )
    chmod 755 "$stage/whwmpc"
    mv -fT -- "$stage/whwmpc" "$target"
    printf 'whwmpc %s terpasang di %s\n' "$tag" "$target"
    case ":${PATH:-}:" in
        *":$bin_dir:"*) ;;
        *) printf 'Tambahkan ~/.local/bin ke PATH atau jalankan langsung dari %s\n' "$target" ;;
    esac
}

main "$@"
