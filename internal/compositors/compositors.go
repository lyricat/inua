// Package compositors links every compositor backend into a program, so
// compositor.Detect can find the one the session runs: import it (for its
// side effect) from any program that talks to the compositor.
package compositors

import (
	_ "github.com/lyricat/inua/internal/hypr" // Hyprland
	_ "github.com/lyricat/inua/internal/niri" // niri
)
