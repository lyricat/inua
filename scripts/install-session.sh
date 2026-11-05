#!/bin/sh
# Install the shared session entry. An optional staging root avoids system writes.
# Build/install binaries and initialize user config first (make install-session).
set -eu

if [ "$#" -gt 2 ]; then
    echo "usage: $0 [greetd|gdm|sddm|other] [staging-root]" >&2
    exit 2
fi
dm=${1:-greetd}
case "$dm" in
    greetd|gdm|sddm|other) ;; # other: any login manager reading wayland-sessions
    *) echo "inua: unsupported display manager: $dm (use greetd, gdm, sddm or other)" >&2; exit 2 ;;
esac
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
dest=${2:-/}

install -Dm755 "$root/scripts/inua-session" "$dest/usr/local/bin/inua-session"
install -Dm644 "$root/config/session/inua.desktop" "$dest/usr/share/wayland-sessions/inua.desktop"
# The niri and Wayfire sessions show only where those are installed (TryExec).
install -Dm644 "$root/config/session/inua-niri.desktop" "$dest/usr/share/wayland-sessions/inua-niri.desktop"
install -Dm644 "$root/config/session/inua-wayfire.desktop" "$dest/usr/share/wayland-sessions/inua-wayfire.desktop"
# account NAME: whether the system being installed has that user.
account() {
    if [ "$dest" = / ]; then
        getent passwd "$1" >/dev/null 2>&1
    else
        grep -q "^$1:" "$dest/etc/passwd" 2>/dev/null
    fi
}

if [ "$dm" = greetd ]; then
    config="$dest/etc/greetd/config.toml"
    # The greeter's account and VT are the greetd package's: _greetd on
    # Debian and Ubuntu, greeter on Arch, greetd on Fedora. Those in the
    # config the package installed come first.
    user= vt=
    if [ -f "$config" ]; then
        user=$(sed -n 's/^[[:space:]]*user[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$config" | head -n 1)
        vt=$(sed -n 's/^[[:space:]]*vt[[:space:]]*=[[:space:]]*\([0-9][0-9]*\).*/\1/p' "$config" | head -n 1)
    fi
    if [ -z "$user" ] || ! account "$user"; then
        user=
        for name in _greetd greeter greetd; do
            if account "$name"; then user=$name; break; fi
        done
    fi
    if [ -z "$user" ]; then
        user=_greetd
        echo "inua: no greeter account (_greetd, greeter or greetd) yet; assuming _greetd. Install greetd, then run this again." >&2
    fi
    tuigreet=$(command -v tuigreet 2>/dev/null || echo /usr/bin/tuigreet)
    staged=$(mktemp)
    trap 'rm -f "$staged"' EXIT
    sed -e "s|^user = .*|user = \"$user\"|" -e "s|^vt = .*|vt = ${vt:-7}|" \
        -e "s|/usr/bin/tuigreet|$tuigreet|" "$root/config/session/greetd.toml" >"$staged"
    if ! cmp -s "$staged" "$config"; then
        install -Dm644 --backup=numbered "$staged" "$config"
        echo "tuigreet configured in $config for the greeter $user on VT ${vt:-7}; any previous config is saved as $config.~N~."
    fi
fi
echo "Inua session installed. Select Inua in your login screen, or run inua-session from a TTY."
