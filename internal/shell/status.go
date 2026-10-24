package shell

import (
	"fmt"
	"math"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/system"
)

// System is the latest state of each system service.
type System struct {
	Net   system.NetworkState
	BT    system.BluetoothState
	Bat   system.BatteryState
	Prof  system.PowerProfileState
	Media system.MediaState
	Audio system.AudioState
	DND   bool
	TS    system.TailscaleState // read while the control center is open
	Voice system.VoiceState
}

// VoiceIcon is the bar's microphone: idle on, red while recording, the
// hourglass while transcribing, and off when the daemon is not there.
func VoiceIcon(v system.VoiceState) string {
	switch {
	case !v.Available:
		return "microphone-slash-symbolic"
	case v.Status == "recording":
		return "microphone-symbolic"
	case v.Status == "transcribing":
		return "hourglass-symbolic"
	}
	return "microphone-symbolic"
}

// VoiceTooltip is what the microphone's hover says.
func VoiceTooltip(v system.VoiceState) string {
	switch {
	case !v.Available:
		return i18n.T("Voice input is not available")
	case v.Status == "recording":
		return i18n.T("Listening…")
	case v.Status == "transcribing":
		return i18n.T("Transcribing…")
	}
	return i18n.T("Voice input")
}

// NetIcon is the bar's network icon.
func NetIcon(n system.NetworkState) string {
	switch {
	case !n.Available:
		return "network-offline-symbolic"
	case n.Kind == "wifi":
		return WifiIcon(n.Strength)
	case n.Kind == "vpn":
		return "network-vpn-symbolic"
	case n.Kind != "":
		return "network-wired-symbolic"
	case n.WifiDevice && !n.WifiEnabled:
		return "network-wireless-disabled-symbolic"
	case n.WifiDevice:
		return "network-wireless-offline-symbolic"
	}
	return "network-offline-symbolic"
}

// NetTooltip names the connection.
func NetTooltip(n system.NetworkState) string {
	if n.Name != "" {
		return n.Name
	}
	return i18n.T("Offline")
}

// WifiIcon maps signal strength (0–100) to an icon.
func WifiIcon(strength int) string {
	switch {
	case strength > 75:
		return "network-wireless-signal-excellent-symbolic"
	case strength > 50:
		return "network-wireless-signal-good-symbolic"
	case strength > 25:
		return "network-wireless-signal-ok-symbolic"
	case strength > 5:
		return "network-wireless-signal-weak-symbolic"
	}
	return "network-wireless-signal-none-symbolic"
}

// WifiTile is the Wi-Fi tile of the control center: subtitle and icon.
func WifiTile(n system.NetworkState) (sub, icon string) {
	switch {
	case !n.Available:
		return i18n.T("Unavailable"), "network-wireless-offline-symbolic"
	case !n.WifiDevice:
		return i18n.T("No Wi-Fi device"), "network-wireless-offline-symbolic"
	case !n.WifiEnabled:
		return i18n.T("Off"), "network-wireless-disabled-symbolic"
	case n.Kind == "wifi":
		return n.Name, WifiIcon(n.Strength)
	case n.Kind != "":
		return n.Name, "network-wireless-signal-none-symbolic" // wired or VPN; Wi-Fi is on but unused
	}
	return i18n.T("Not connected"), "network-wireless-offline-symbolic"
}

// BluetoothTile is the subtitle of the Bluetooth tile.
func BluetoothTile(b system.BluetoothState) string {
	switch {
	case !b.Available:
		return i18n.T("Unavailable")
	case !b.Powered:
		return i18n.T("Off")
	case len(b.Connected) == 1:
		return b.Connected[0]
	case len(b.Connected) > 1:
		return i18n.Tf("%d devices", len(b.Connected))
	}
	return i18n.T("On")
}

// SaverTile is the subtitle of the power saver tile.
func SaverTile(p system.PowerProfileState) string {
	switch {
	case !p.Available:
		return i18n.T("Unavailable")
	case p.Active == "power-saver":
		return i18n.T("On")
	case p.Active == "performance":
		return i18n.T("Performance")
	}
	return i18n.T("Off")
}

// TailscaleTile is the Tailscale tile: its subtitle, whether it shows
// on, and whether a click connects or disconnects (else it opens the app,
// which shows what is in the way: logging in, the daemon, an approval).
func TailscaleTile(t system.TailscaleState) (sub string, on, toggles bool) {
	switch {
	case !t.Running:
		return i18n.T("Not running"), false, false
	case t.Backend == "Running" && t.ExitNode != "":
		return i18n.Tf("via %s", t.ExitNode), true, true
	case t.Backend == "Running":
		return i18n.Tf("%d online", t.Online), true, true
	case t.Backend == "Stopped":
		return i18n.T("Off"), false, true
	case t.Backend == "NeedsLogin":
		return i18n.T("Logged out"), false, false
	case t.Backend == "NeedsMachineAuth":
		return i18n.T("Needs approval"), false, false
	case t.Backend == "Starting":
		return i18n.T("Connecting…"), true, true
	}
	return t.Backend, false, false
}

// SaverTarget is the profile a click on the power saver tile switches to.
func SaverTarget(p system.PowerProfileState) string {
	if p.Active == "power-saver" {
		return "balanced"
	}
	return "power-saver"
}

// VolumeIcon maps the output volume to an icon.
func VolumeIcon(a system.AudioState) string {
	switch {
	case !a.Available || a.Muted || a.Volume <= 0:
		return "audio-volume-muted-symbolic"
	case a.Volume < 0.34:
		return "audio-volume-low-symbolic"
	case a.Volume < 0.67:
		return "audio-volume-medium-symbolic"
	}
	return "audio-volume-high-symbolic"
}

// Percent is a 0–1 level as a readout: "64%".
func Percent(f float64) string {
	return fmt.Sprintf("%.0f%%", max(0, min(f, 1))*100)
}

// VolumeText is the volume readout; muted shows as such.
func VolumeText(a system.AudioState) string {
	switch {
	case !a.Available:
		return "—"
	case a.Muted:
		return "muted"
	}
	return Percent(a.Volume)
}

// BatteryIcon picks the icon for a battery level in steps of ten.
func BatteryIcon(b system.BatteryState) string {
	if b.Full {
		return "battery-level-100-charged-symbolic"
	}
	level := int(math.Round(max(0, min(b.Percent, 100))/10)) * 10
	if b.Charging {
		return fmt.Sprintf("battery-level-%d-charging-symbolic", level)
	}
	return fmt.Sprintf("battery-level-%d-symbolic", level)
}

// BatteryText is the long battery line of the control center.
func BatteryText(b system.BatteryState) string {
	switch {
	case b.Full:
		return i18n.T("Fully charged")
	case b.Charging:
		return i18n.Tf("%.0f%% · Charging", b.Percent)
	}
	return fmt.Sprintf("%.0f%%", b.Percent)
}

// MediaText is the now-playing title and artist; the player's name fills in
// for missing metadata.
func MediaText(m system.MediaState) (title, artist string) {
	title = m.Title
	if title == "" {
		title = m.Identity
	}
	artist = m.Artist
	if artist == "" && title != m.Identity {
		artist = m.Identity
	}
	return title, artist
}

// PlayIcon is the icon of the play/pause button.
func PlayIcon(playing bool) string {
	if playing {
		return "media-playback-pause-symbolic"
	}
	return "media-playback-start-symbolic"
}

// VolumeChanged reports whether an audio update is a change the user made
// (the volume or the mute), which the volume indicator shows; audio
// coming or going is not.
func VolumeChanged(old, new system.AudioState) bool {
	return old.Available && new.Available && (old.Volume != new.Volume || old.Muted != new.Muted)
}
