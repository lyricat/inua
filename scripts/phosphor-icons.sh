#!/bin/sh
# Copy the Phosphor icons (https://phosphoricons.com, MIT) that inua-shell
# draws into ui/icons, named as the freedesktop icons the shell asks for.
# The shell puts them in an icon theme of its own (style.icons = "phosphor").
# Edit the list and run again to change or add one; the files are committed.
#
#   scripts/phosphor-icons.sh path/to/node_modules/@phosphor-icons/core [WEIGHT]
#
# The regular weight (the default) is the built-in set. Another weight
# (bold, fill, light, thin, duotone) makes a sample icon set of the user's
# own, config/inua/icons/phosphor-WEIGHT, which `inua init` installs.
# npm i @phosphor-icons/core
set -eu
src=${1:?usage: $0 PHOSPHOR_CORE_DIR (node_modules/@phosphor-icons/core) [WEIGHT]}
weight=${2:-regular}
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
if [ "$weight" = regular ]; then
	dest=$root/ui/icons suffix=
else
	dest=$root/config/inua/icons/phosphor-$weight suffix=-$weight
fi
[ -d "$src/assets/$weight" ] || { echo "$0: no weight $weight in $src/assets" >&2; exit 1; }
mkdir -p "$dest"
rm -f "$dest"/*.svg
cp "$src/LICENSE" "$dest/LICENSE"

# freedesktop name (without -symbolic)   Phosphor name
while read -r name icon; do
	case "$name" in '' | '#'*) continue ;; esac
	cp "$src/assets/$weight/$icon$suffix.svg" "$dest/$name-symbolic.svg"
done <<'LIST'
# virtual machines (inua-vm): the mark of each system it can run, and the
# window's own placeholder. Phosphor has no FreeBSD logo: a sailboat stands in
vm-machine                            desktop-tower
vm-os-android                         android-logo
vm-os-custom                          cpu
vm-os-freebsd                         sailboat
vm-os-linux                           linux-logo
vm-os-windows                         windows-logo
# bar and control center: network
network-offline                       globe-simple-x
network-vpn                           shield-check
network-wired                         network
network-wireless-disabled             wifi-slash
network-wireless-offline              wifi-x
network-wireless-signal-excellent     wifi-high
network-wireless-signal-good          wifi-medium
network-wireless-signal-ok            wifi-medium
network-wireless-signal-weak          wifi-low
network-wireless-signal-none          wifi-none
bluetooth-active                      bluetooth
bluetooth-disabled                    bluetooth-slash
# sound and brightness
audio-volume-muted                    speaker-x
audio-volume-low                      speaker-low
audio-volume-medium                   speaker-low
audio-volume-high                     speaker-high
display-brightness                    sun
# battery, in steps of ten (shell.BatteryIcon); Phosphor has one charging icon
battery-level-0                       battery-empty
battery-level-10                      battery-low
battery-level-20                      battery-low
battery-level-30                      battery-medium
battery-level-40                      battery-medium
battery-level-50                      battery-medium
battery-level-60                      battery-high
battery-level-70                      battery-high
battery-level-80                      battery-high
battery-level-90                      battery-full
battery-level-100                     battery-full
battery-level-100-charged             battery-full
battery-level-0-charging              battery-charging
battery-level-10-charging             battery-charging
battery-level-20-charging             battery-charging
battery-level-30-charging             battery-charging
battery-level-40-charging             battery-charging
battery-level-50-charging             battery-charging
battery-level-60-charging             battery-charging
battery-level-70-charging             battery-charging
battery-level-80-charging             battery-charging
battery-level-90-charging             battery-charging
battery-level-100-charging            battery-charging
# media
audio-x-generic                       music-note
media-playback-start                  play
media-playback-pause                  pause
media-skip-backward                   skip-back
media-skip-forward                    skip-forward
# control center buttons and tiles
system-lock-screen                    lock
dialog-password                       key
security-high                         shield-check
action-unavailable                    prohibit
edit-paste                            clipboard-text
applets-screenshooter                 camera
emblem-system                         gear-six
power-profile-power-saver             leaf
weather-clear-night                   moon
# notifications
preferences-system-notifications      bell
window-close                          x
edit-undo                             arrow-counter-clockwise
# launcher and settings
application-x-executable              app-window
edit-clear                            x
pan-down                              caret-down
pan-up                                caret-up
# screenshot tool
edit-select-all                       selection
focus-windows                         app-window
video-display                         monitor
# light apps
alarm                                 alarm
weather-clear                         sun
weather-few-clouds                    cloud-sun
weather-few-clouds-night              cloud-moon
weather-overcast                      cloud
weather-fog                           cloud-fog
weather-showers-scattered             cloud-rain
weather-showers                       cloud-rain
weather-snow                          cloud-snow
weather-storm                         cloud-lightning
weather-severe-alert                  warning
go-previous                           caret-left
go-next                               caret-right
list-add                              plus
accessories-calculator                calculator
view-list                             list
# search entries
system-search                         magnifying-glass
edit-find                             magnifying-glass
# the dock's drawer and the control center's power row
view-app-grid                         squares-four
application-x-addon                   lego
system-suspend                        moon
system-reboot                         arrow-clockwise
system-shutdown                       power
system-log-out                        sign-out
# color picker and recording
edit-copy                             copy
color-select                          eyedropper
media-record                          record
# image viewer
zoom-in                               magnifying-glass-plus
zoom-out                              magnifying-glass-minus
object-rotate-right                   arrow-clockwise
user-trash                            trash
folder-open                           folder-open
folder                                folder
view-fullscreen                       corners-out
view-restore                          corners-in
dialog-information                    info
preferences-desktop-wallpaper         image-square
image-x-generic                       image
# calculator
calc-backspace                        backspace
calc-plus-minus                       plus-minus
document-open-recent                  clock-counter-clockwise
# password dialog: hide and show the private details
view-conceal                          eye-slash
view-reveal                           eye
# the settings index
preferences-desktop-appearance        palette
user-desktop                          layout
system-run                            rocket-launch
applications-science                  sparkle
preferences-desktop-keyboard-shortcuts command
input-keyboard                        keyboard
input-mouse                           mouse-simple
help-about                            info
# Pass: edit, and rename or move
document-edit                         pencil-simple
go-jump                               arrow-bend-up-right
# Pass: a one-time code from a QR code, read off the screen
pass-qr-code                          qr-code
pass-scan                             scan
# Music and Video
media-playback-stop                   stop
media-eject                           eject
media-playlist-shuffle                shuffle
media-playlist-repeat                 repeat
media-playlist-repeat-song            repeat-once
media-view-subtitles                  subtitles
media-audio-track                     waveform
list-remove                           minus
# GPG: a signature that does not hold, saving, the key chosen
dialog-warning                        warning
document-save                         floppy-disk
object-select                         check
# Pass: an entry's icon, by its name (internal/apps/pass/icon.go)
pass-airplane                         airplane
pass-amazon-logo                      amazon-logo
pass-android-logo                     android-logo
pass-apple-logo                       apple-logo
pass-bank                             bank
pass-certificate                      certificate
pass-chat-circle                      chat-circle
pass-cloud                            cloud
pass-credit-card                      credit-card
pass-currency-btc                     currency-btc
pass-database                         database
pass-device-mobile                    device-mobile
pass-discord-logo                     discord-logo
pass-dropbox-logo                     dropbox-logo
pass-envelope                         envelope
pass-facebook-logo                    facebook-logo
pass-figma-logo                       figma-logo
pass-film-strip                       film-strip
pass-game-controller                  game-controller
pass-git-branch                       git-branch
pass-github-logo                      github-logo
pass-gitlab-logo                      gitlab-logo
pass-globe                            globe
pass-google-logo                      google-logo
pass-graduation-cap                   graduation-cap
pass-hard-drives                      hard-drives
pass-heartbeat                        heartbeat
pass-house                            house
pass-instagram-logo                   instagram-logo
pass-key                              key
pass-linkedin-logo                    linkedin-logo
pass-mastodon-logo                    mastodon-logo
pass-microsoft-outlook-logo           microsoft-outlook-logo
pass-microsoft-teams-logo             microsoft-teams-logo
pass-music-notes                      music-notes
pass-network                          network
pass-notion-logo                      notion-logo
pass-package                          package
pass-paypal-logo                      paypal-logo
pass-pinterest-logo                   pinterest-logo
pass-reddit-logo                      reddit-logo
pass-shield-check                     shield-check
inua-benchmark                        gauge
pass-shopping-cart                    shopping-cart
pass-slack-logo                       slack-logo
pass-spotify-logo                     spotify-logo
pass-steam-logo                       steam-logo
pass-stripe-logo                      stripe-logo
pass-telegram-logo                    telegram-logo
pass-terminal-window                  terminal-window
pass-tiktok-logo                      tiktok-logo
pass-twitch-logo                      twitch-logo
pass-user                             user
pass-wallet                           wallet
pass-whatsapp-logo                    whatsapp-logo
pass-wifi-high                        wifi-high
pass-windows-logo                     windows-logo
pass-x-logo                           x-logo
pass-youtube-logo                     youtube-logo
# Files: the file manager's buttons (files, places and drives use the desktop's theme)
text-x-generic                        file-text
user-home                             house
media-eject                           eject
go-up                                 arrow-up
view-grid                             squares-four
view-dual                             columns
utilities-terminal                    terminal-window
# Viewer: Markdown's source
viewer-source                         code
# Small Fortune's panel
fortune-house                         house
fortune-hotel                         building
fortune-sell                          hand-coins
fortune-lot                           plant
fortune-pier                          boat
fortune-power                         lightning
fortune-water                         drop
fortune-dice                          dice-five
# Files
view-more                             dots-three
LIST
echo "$(ls "$dest"/*.svg | wc -l) icons in $dest"
