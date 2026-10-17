#!/bin/sh
# Install or update inua from its latest release on GitHub:
#
#   curl -fsSL https://inua.lyric.im/install.sh | sh
#
# It puts the programs in ~/.local/bin and the light apps' launchers in
# ~/.local/share, checks the packages inua needs, writes your configuration
# with `inua init` (never replacing your files), and, with sudo, adds the
# Inua login session. Where inua replaces something you have, it asks
# first, and the answer defaults to yes for gpg's pinentry (inua-pinentry)
# and the keyring apps keep secrets in (inua-keyring, in pass), and for
# Omarchy's shell (Quickshell, with inua-shell); with no login manager it
# offers greetd + tuigreet (yes), while GDM, SDDM, LightDM … are kept
# unless you answer yes. Without a password store it
# offers to make one with a GPG key of yours (yes), or with a new key
# (no), and says what to run by hand. Run it again to update.
#
# Environment:
#   INUA_VERSION=v0.1.2   a release instead of the latest
#   PREFIX=~/.local       where the programs go (PREFIX/bin)
#   INUA_NO_SESSION=1     leave the login session alone (no sudo)
#   INUA_ARCHIVE=file     a release archive already downloaded
#   INUA_DM=gdm3          the login manager to assume, instead of systemd's
set -eu

repo=lyricat/inua
prefix=${PREFIX:-$HOME/.local}
data=${XDG_DATA_HOME:-$HOME/.local/share}

# The output: a heading for each step, ✓ for what is done, ! for what
# needs your attention; in colour on a terminal.
if [ -t 1 ]; then
    bold='\033[1m' dim='\033[2m' green='\033[32m' yellow='\033[33m' red='\033[31m' blue='\033[34m' off='\033[0m'
else
    bold= dim= green= yellow= red= blue= off=
fi
steps=6
[ -n "${INUA_NO_SESSION:-}" ] && steps=5
step=0
heading() { step=$((step + 1)); printf "\n${bold}${blue}[%d/%d]${off} ${bold}%s${off}\n" "$step" "$steps" "$*"; }
say() { printf "      %s\n" "$*"; }
done_() { printf "  ${green}✓${off}   %s\n" "$*"; }
note() { printf "  ${yellow}!${off}   %s\n" "$*"; }
die() { printf "  ${red}✗   %s${off}\n" "$*" >&2; exit 1; }

# ask QUESTION y|n: true for yes. Reads the terminal, as the script itself
# comes down a pipe; without one, the default stands.
ask() {
    if [ "$2" = y ]; then hint="[Y/n]"; else hint="[y/N]"; fi
    if ! (: </dev/tty) 2>/dev/null; then
        [ "$2" = y ]
        return
    fi
    printf "  ${bold}?${off}   %s ${dim}%s${off} " "$1" "$hint" >/dev/tty
    read -r answer </dev/tty || answer=
    case $answer in
        [Yy]*) return 0 ;;
        [Nn]*) return 1 ;;
        *) [ "$2" = y ] ;;
    esac
}

have() { command -v "$1" >/dev/null 2>&1; }

# The package manager here, and what it calls the packages inua needs:
# pkg NAME gives the name for this system's manager.
manager=
for m in apt-get pacman dnf zypper; do
    if have "$m"; then manager=$m; break; fi
done
pkg() {
    case $manager:$1 in
        apt-get:gtk4) echo libgtk-4-1 ;;
        apt-get:gtk4-layer-shell) echo libgtk4-layer-shell0 ;;
        zypper:gtk4) echo libgtk-4-1 ;;
        zypper:gtk4-layer-shell) echo libgtk4-layer-shell0 ;;
        zypper:pass) echo password-store ;;
        zypper:gnupg) echo gpg2 ;;
        dnf:gnupg) echo gnupg2 ;;
        pacman:tuigreet) echo greetd-tuigreet ;;
        apt-get:gst-*) echo "gstreamer1.0-plugins-${1#gst-}" | sed 's/plugins-libav/libav/' ;;
        pacman:gst-*) echo "gst-plugins-${1#gst-}" | sed 's/plugins-libav/libav/' ;;
        dnf:gst-bad | dnf:gst-ugly) echo "gstreamer1-plugins-${1#gst-}-free" ;;
        dnf:gst-libav) echo gstreamer1-plugin-libav ;;
        dnf:gst-*) echo "gstreamer1-plugins-${1#gst-}" ;;
        zypper:gst-*) echo "gstreamer-plugins-${1#gst-}" ;;
        *) echo "$1" ;;
    esac
}
# pkg_install NAMES…: installs packages with this system's manager (sudo).
pkg_install() {
    case $manager in
        apt-get) sudo apt-get install -y "$@" ;;
        pacman) sudo pacman -S --needed --noconfirm "$@" ;;
        dnf) sudo dnf install -y "$@" ;;
        zypper) sudo zypper --non-interactive install "$@" ;;
        *) return 1 ;;
    esac
}

# The version of inua installed now, if any: "0.0.6".
version_of() { "$prefix/bin/inua" version 2>/dev/null | sed -n 's/^inua //p'; }

# --- the release ---------------------------------------------------------

heading "Downloading inua"
before=$(version_of || true)
[ "$(uname -s)" = Linux ] || die "inua runs on Linux"
case $(uname -m) in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) die "no release for $(uname -m); build from source: https://github.com/$repo" ;;
esac
for tool in tar sha256sum; do have "$tool" || die "$tool is needed"; done

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM
name=inua_linux_$arch

if [ -n "${INUA_ARCHIVE:-}" ]; then
    cp "$INUA_ARCHIVE" "$tmp/$name.tar.gz"
    done_ "using $INUA_ARCHIVE"
else
    have curl || die "curl is needed"
    if [ -n "${INUA_VERSION:-}" ]; then
        base=https://github.com/$repo/releases/download/$INUA_VERSION
    else
        base=https://github.com/$repo/releases/latest/download
    fi
    say "${INUA_VERSION:-the latest release}, for $arch"
    curl -fsSL -o "$tmp/$name.tar.gz" "$base/$name.tar.gz" || die "could not download $base/$name.tar.gz"
    curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || die "could not download the checksums"
    (cd "$tmp" && grep " $name.tar.gz\$" checksums.txt | sha256sum -c --quiet -) || die "the download does not match its checksum"
    done_ "downloaded, and its checksum matches"
fi
tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
src=$tmp/$name

# --- the programs and launchers --------------------------------------------

heading "Installing the programs"
for bin in inua-shell inua inua-apps inua-files inua-viewer inua-vm inua-games inua-pinentry inua-assistant inua-keyring; do
    [ -f "$src/$bin" ] || continue # inua-files, inua-viewer, inua-vm, inua-games, inua-assistant and inua-keyring came after v0.0.2
    install -Dm755 "$src/$bin" "$prefix/bin/$bin"
done
install -Dm644 -t "$data/applications" "$src"/config/apps/*.desktop
install -Dm644 -t "$data/icons/hicolor/scalable/apps" "$src"/config/apps/*.svg
have update-desktop-database && update-desktop-database -q "$data/applications" 2>/dev/null || true
after=$(version_of)
if [ -z "$before" ]; then
    done_ "installed inua $after in $prefix/bin"
elif [ "$before" = "$after" ]; then
    done_ "inua $after is already the latest; reinstalled it in $prefix/bin"
else
    done_ "updated inua $before → $after in $prefix/bin"
fi
done_ "the light apps and games are in your app launcher"
case :$PATH: in
    *":$prefix/bin:"*) ;;
    *) note "$prefix/bin is not on your PATH (the Inua session adds it)" ;;
esac

# --- what inua needs --------------------------------------------------------

heading "Checking what inua needs"
missing=
if have Hyprland; then
    # inua needs 0.53 or later: "Hyprland 0.53.3 built from …"
    hyprland=$(Hyprland --version 2>/dev/null | sed -n '1s/^Hyprland \([0-9][0-9]*\)\.\([0-9][0-9]*\).*/\1.\2/p')
    case $hyprland in
        0.[0-9] | 0.[0-4][0-9] | 0.5[0-2]) note "inua needs Hyprland 0.53 or later; this one is $hyprland. Update it before logging in to Inua" ;;
    esac
elif [ "$manager" = dnf ] && ! dnf -q repoquery hyprland 2>/dev/null | grep -q .; then
    # Fedora's own repositories have no Hyprland after Fedora 42.
    note "Hyprland is not in your repositories; enable a Hyprland COPR (https://wiki.hypr.land/Getting-Started/Installation/), then run this again"
else
    missing="$missing $(pkg hyprland)"
fi
# ldconfig is in /sbin, which a user's PATH may not have.
ldconfig=$(command -v ldconfig || echo /sbin/ldconfig)
lib() { "$ldconfig" -p 2>/dev/null | grep -q "$1"; }
lib libgtk-4.so.1 || missing="$missing $(pkg gtk4)"
lib libgtk4-layer-shell.so || missing="$missing $(pkg gtk4-layer-shell)"
# inua-keyring keeps apps' secrets in pass (docs/feat/keyring.md)
have pass || missing="$missing $(pkg pass)"
have gpg || missing="$missing $(pkg gnupg)"
# the clipboard history, Color Picker and Viewer copy with wl-copy; the
# history reads with wl-paste
have wl-paste || missing="$missing $(pkg wl-clipboard)"
# Music and Video play with GStreamer and its plugins; each package is
# known by one plugin of it.
gst_plugin() {
    for d in /usr/lib/gstreamer-1.0 /usr/lib64/gstreamer-1.0 /usr/lib/*-linux-gnu/gstreamer-1.0; do
        [ -e "$d/$1" ] && return 0
    done
    return 1
}
gst_plugin libgstplayback.so || missing="$missing $(pkg gst-base)"
gst_plugin libgstequalizer.so || missing="$missing $(pkg gst-good)"
gst_plugin libgstmpegtsdemux.so || missing="$missing $(pkg gst-bad)"
gst_plugin libgstasf.so || missing="$missing $(pkg gst-ugly)"
gst_plugin libgstlibav.so || missing="$missing $(pkg gst-libav)"
if [ -z "$missing" ]; then
    done_ "Hyprland, GTK 4, gtk4-layer-shell, pass, gpg, wl-clipboard and GStreamer are all here"
else
    note "inua also needs:$missing"
    # shellcheck disable=SC2086
    if [ -n "$manager" ] && ask "Install them with ${manager%-get} (sudo)?" y && pkg_install $missing; then
        done_ "installed$missing"
    else
        note "install them${manager:+ with ${manager%-get}} before logging in to Inua"
    fi
fi

# --- your configuration -----------------------------------------------------

heading "Writing your configuration"
# inua init asks before replacing gpg's pinentry with inua-pinentry, and
# Omarchy's shell (Quickshell) with inua-shell.
if PATH="$prefix/bin:$PATH" "$prefix/bin/inua" init; then
    done_ "configuration written (your own files are kept)"
else
    note "inua init reported problems (above); fix them, then run: inua init"
fi
# inua init leaves a Hyprland config of yours alone (an older Omarchy's
# hyprland.conf, say); the Inua session does not start until it starts
# inua-shell.
starts=yes
PATH="$prefix/bin:$PATH" "$prefix/bin/inua" config hyprland >/dev/null 2>&1 || starts=no
if [ "$starts" = no ]; then
    note "your Hyprland config does not start inua-shell, so the Inua session will not start yet; inua init said how to add it (above)"
    [ -d "$HOME/.local/share/omarchy" ] &&
        note "Omarchy's config also starts waybar and mako, which would run beside inua's bar and notifications"
fi

# --- the keyring ------------------------------------------------------------

heading "Setting up the keyring"

# prompt QUESTION DEFAULT: a line typed at the terminal, else DEFAULT.
prompt() {
    printf "  ${bold}?${off}   %s ${dim}[%s]${off} " "$1" "$2" >/dev/tty
    read -r answer </dev/tty || answer=
    printf "%s\n" "${answer:-$2}"
}

# secret_keys: the GPG keys of yours that can encrypt, one a line:
# fingerprint, a tab, the first user ID.
secret_keys() {
    gpg --batch --list-secret-keys --with-colons 2>/dev/null | awk -F: '
        $1 == "sec" { ok = $2 != "e" && $2 != "r" && $12 ~ /E/; fpr = ""; uid = ""; next }
        $1 == "fpr" && fpr == "" { fpr = $10 }
        $1 == "uid" && uid == "" && ok && fpr != "" { uid = $10; gsub(/\\x3a/, ":", uid); print fpr "\t" uid }'
}

# setup_store: makes the password store inua-keyring keeps apps' secrets
# in, if there is none: with a GPG key of yours, or a new one you choose
# the passphrase of. Nothing without a terminal to ask at. True when the
# store is there.
setup_store() {
    store=${PASSWORD_STORE_DIR:-$HOME/.password-store}
    [ -f "$store/.gpg-id" ] && return 0
    (: </dev/tty) 2>/dev/null || return 1
    told=yes # what to run by hand is said below
    say "pass has no password store yet; inua-keyring keeps apps' secrets in one, locked with a GPG key"
    keys=$(secret_keys)
    n=$(printf "%s" "$keys" | grep -c . || true)
    fpr=
    if [ "$n" -eq 1 ]; then
        fpr=$(printf "%s" "$keys" | cut -f1)
        say "with your key $(printf "%s" "$keys" | cut -f2), this runs:"
        say "    pass init $fpr"
        say "(by hand later: that, then inua keyring replace)"
        ask "Create the password store with this key?" y || return 1
    elif [ "$n" -gt 1 ]; then
        say "your GPG keys:"
        printf "%s\n" "$keys" | awk -F'\t' '{ printf "        %d  %s  %s\n", NR, $2, substr($1, length($1) - 15) }'
        say "the one chosen runs: pass init KEY (by hand later: that, then inua keyring replace)"
        choice=$(prompt "Which key locks the password store? 0 for none" 1)
        case $choice in '' | *[!0-9]*) choice=0 ;; esac
        [ "$choice" -ge 1 ] && [ "$choice" -le "$n" ] || return 1
        fpr=$(printf "%s\n" "$keys" | sed -n "${choice}p" | cut -f1)
    else
        say "you have no GPG key. To make one and the password store by hand:"
        say "    gpg --quick-generate-key \"Your Name <you@example.org>\" default default never"
        say "    pass init KEY-ID          (the fingerprint gpg printed)"
        say "    inua keyring replace"
        ask "Or create them now? You choose the key's passphrase" n || return 1
        name=$(prompt "Your name" "$(git config --get user.name 2>/dev/null || getent passwd "$(id -un)" | cut -d: -f5 | cut -d, -f1)")
        email=$(prompt "Your email" "$(git config --get user.email 2>/dev/null || true)")
        uid=$name
        [ -n "$email" ] && uid="$name <$email>"
        [ -n "$uid" ] || return 1
        # gpg asks for the passphrase through the pinentry: inua's dialog, or
        # without a shell running the usual one, on this terminal if need be.
        GPG_TTY=$(tty </dev/tty) gpg --quick-generate-key "$uid" default default never </dev/tty || return 1
        fpr=$(secret_keys | awk -F'\t' -v uid="$uid" '$2 == uid { f = $1 } END { print f }')
        [ -n "$fpr" ] || return 1
        done_ "made the GPG key $uid"
        note "back it up: gpg --export-secret-keys --armor $fpr > inua-key.asc. Without the key, or its passphrase, the secrets in pass cannot be read"
    fi
    pass init "$fpr" >/dev/null || return 1
    done_ "made the password store in $store"
}

# inua keyring replace asks before replacing the running keyring
# (gnome-keyring …), copying its items into pass first.
if [ ! -x "$prefix/bin/inua-keyring" ] || ! have pass; then
    note "inua-keyring needs pass; once it is installed, run: inua keyring replace"
elif told=no; ! have gpg || ! setup_store; then
    if [ "$told" = yes ]; then
        note "inua-keyring waits for a password store; the keyring is left as it is"
    else
        note "pass has no password store yet; run: pass init YOUR-GPG-KEY-ID, then: inua keyring replace"
    fi
elif [ -z "${DBUS_SESSION_BUS_ADDRESS:-}" ] && [ ! -S "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/bus" ]; then
    note "no desktop session here; in one, run: inua keyring replace"
elif PATH="$prefix/bin:$PATH" "$prefix/bin/inua" keyring replace </dev/null; then
    done_ "inua-keyring keeps apps' secrets in pass"
else
    note "the keyring was not replaced (above); try again with: inua keyring replace"
fi

# --- the login session ------------------------------------------------------

if [ -n "${INUA_NO_SESSION:-}" ]; then
    printf "\n${bold}${green}Done.${off} Log in with inua-session from a TTY, or install the session later.\n"
    exit 0
fi

heading "Adding the Inua login session"

# autologin DM: whether that login manager logs someone in by itself, as
# Omarchy's SDDM does; greetd + tuigreet would ask for the password.
autologin() {
    case $1 in
        sddm) grep -rqsE '^[[:space:]]*User=[^[:space:]]' /etc/sddm.conf /etc/sddm.conf.d ;;
        gdm | gdm3) grep -qsE '^[[:space:]]*AutomaticLoginEnable[[:space:]]*=[[:space:]]*[Tt]rue' /etc/gdm3/custom.conf /etc/gdm/custom.conf ;;
        lightdm) grep -rqsE '^[[:space:]]*autologin-user=[^[:space:]]' /etc/lightdm ;;
        *) false ;;
    esac
}

# The login manager systemd starts: gdm3, sddm, lightdm, greetd …, or none.
current=${INUA_DM-$(basename "$(readlink -f /etc/systemd/system/display-manager.service 2>/dev/null || true)" .service)}
[ "$current" = display-manager ] && current=
switch=no
case $current in
    greetd) mode=greetd ;;
    "")
        mode=other
        [ -n "$manager" ] && ask "No login manager runs. Install greetd + tuigreet, the login screen inua recommends?" y && switch=yes
        ;;
    *)
        mode=other
        say "you log in with $current; Inua will be in its list of sessions"
        if autologin "$current"; then
            note "$current logs you in by itself; greetd + tuigreet would ask for your password. To be logged in to Inua instead, set its session to inua.desktop (Session=inua.desktop for SDDM)"
        fi
        [ -n "$manager" ] && ask "Replace $current with greetd + tuigreet, the login screen inua recommends? $current stays installed" n && switch=yes
        ;;
esac
if [ "$switch" = yes ]; then
    say "installing greetd and $(pkg tuigreet) (sudo)"
    pkg_install greetd "$(pkg tuigreet)" || die "install greetd and $(pkg tuigreet) with your package manager, then run this again"
    mode=greetd
fi

say "adding the session (sudo)"
sudo sh "$src/scripts/install-session.sh" "$mode"
done_ "Inua is a login session"

if [ "$switch" = yes ]; then
    [ -n "$current" ] && sudo systemctl disable "$current.service"
    sudo systemctl enable --force greetd.service
    done_ "greetd + tuigreet greet you after a restart; until then this session goes on"
    printf "\n${bold}${green}Done.${off} Restart, then log in to Inua.\n"
else
    printf "\n${bold}${green}Done.${off} Log out and choose Inua.\n"
fi
