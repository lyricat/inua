// Package samples embeds the bootstrap configuration in this directory, so
// `inua init` can write it without the repository. Paths in FS are relative
// to $XDG_CONFIG_HOME: hypr/hyprland.conf, hypr/inua.conf (sourced by
// hyprland.conf, rewritten by inua), inua/config.toml, inua/style.css, and
// a sample theme and icon set of the user's own: inua/themes/nord.css,
// inua/icons/phosphor-bold/ (scripts/phosphor-icons.sh writes it).
package samples

import (
	"embed"
	"io/fs"
)

//go:embed hypr/hyprland.conf hypr/inua.conf inua/config.toml inua/style.css inua/themes/nord.css inua/icons/phosphor-bold
var FS embed.FS

// Compositors are the sample configs of the other compositors inua runs
// on, written when the user first chooses one (internal/session), not by
// inua init: niri/config.kdl and wayfire/wayfire.ini.
//
//go:embed niri/config.kdl wayfire/wayfire.ini
var Compositors embed.FS

// IconSet is the sample icon set's folder in FS.
const IconSet = "inua/icons/phosphor-bold"

// Files lists FS in the order they are installed: inua.conf before the
// hyprland.conf that sources it, since Hyprland rejects a missing source.
var Files = append([]string{"hypr/inua.conf", "hypr/hyprland.conf", "inua/config.toml", "inua/style.css", "inua/themes/nord.css"}, iconSet()...)

func iconSet() []string {
	entries, err := fs.ReadDir(FS, IconSet)
	if err != nil {
		panic(err) // embedded: a broken build
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = IconSet + "/" + e.Name()
	}
	return names
}
