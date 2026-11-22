#!/usr/bin/env bash
set -euo pipefail

if [[ ${GITHUB_ACTIONS:-} != true || ${RUNNER_ENVIRONMENT:-} != github-hosted || $(uname -s) != Linux ]]; then
    printf '%s\n' 'Daemon smoke requires a GitHub-hosted Linux runner.' >&2
    exit 1
fi

source_binary=$(realpath -- "${1:?pass the built whwmpc executable}")
fixture=$(mktemp -d "${RUNNER_TEMP:?}/whwmpc-daemon.XXXXXXXX")
nonce=${fixture##*.}
account="whwmpc-${nonce,,}"
fixture_home="$fixture/home"
binary="$fixture_home/whwmpc"
chmod 0711 "$fixture"
sudo useradd --create-home --home-dir "$fixture_home" "$account"

cleanup() {
    sudo loginctl disable-linger "$account" || true
    sudo loginctl terminate-user "$account" || true
    sudo userdel --remove "$account"
    rmdir "$fixture"
}
trap cleanup EXIT

sudo install -o "$account" -m 0755 "$source_binary" "$binary"
uid_dir="/run/user/$(id -u "$account")"
sudo loginctl enable-linger "$account"
for _ in {1..40}; do
    [[ -S "$uid_dir/bus" ]] && break
    sleep 0.25
done
test -S "$uid_dir/bus"

as_user() {
    sudo -u "$account" env -i HOME="$fixture_home" PATH=/usr/bin:/bin LC_ALL=C \
        XDG_RUNTIME_DIR="$uid_dir" DBUS_SESSION_BUS_ADDRESS="unix:path=$uid_dir/bus" "$@"
}

as_user "$binary" daemon install --interval 5m
as_user "$binary" daemon status | grep -Fx 'ActiveState=inactive'
as_user "$binary" daemon enable
as_user "$binary" daemon start
as_user "$binary" daemon status | grep -Fx 'ActiveState=active'
as_user /usr/bin/systemctl --user start whwmpc-monitor.service
as_user "$binary" history --json | jq -e '[.entries[] | select(.category == "daemon" and .kind == "scan")] | length == 1'
as_user "$binary" daemon stop
as_user "$binary" daemon status | grep -Fx 'ActiveState=inactive'
as_user "$binary" daemon disable
as_user "$binary" daemon uninstall
test ! -e "$fixture_home/.config/systemd/user/whwmpc-monitor.service"
test ! -e "$fixture_home/.config/systemd/user/whwmpc-monitor.timer"
