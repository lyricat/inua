// Package compsettings turns inua's settings into the neutral form a
// compositor backend applies (compositor.Settings): the same keyboard,
// touchpad and shortcut settings whichever compositor runs.
package compsettings

import (
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/hyprconf"
)

// From is the settings of c for a backend.
func From(c config.Config) compositor.Settings {
	s := compositor.Settings{
		KbOptions:     c.Input.KbOptions,
		TouchpadSpeed: c.Input.TouchpadSpeed,
		TouchpadAccel: c.Input.TouchpadAccel,
	}
	for _, sc := range c.Shortcuts {
		// A shortcut with no keys or no command (dictation while it is off)
		// still takes its old keys away where the compositor can.
		s.Binds = append(s.Binds, compositor.KeyBind{Keys: sc.Keys, Command: hyprconf.ShortcutCommand(c, sc), Replaces: sc.Replaces})
	}
	return s
}
