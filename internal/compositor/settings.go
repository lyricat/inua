package compositor

import (
	"context"
	"strings"
)

// Settings are inua's settings a compositor applies: the same values on
// every backend, each written the backend's own way (Hyprland's inua.conf,
// niri's inua.kdl, Wayfire's options over IPC). A nil pointer leaves the
// compositor's own setting alone.
type Settings struct {
	KbOptions     *string  // XKB options, comma-separated
	TouchpadSpeed *float64 // -1 to 1
	TouchpadAccel string   // "adaptive", "flat", or "" for the compositor's
	Binds         []KeyBind
}

// KeyBind is a shortcut inua sets: keys as Keys writes them
// ("SUPER+SHIFT+S") and the shell command they run. Replaces are the keys
// the command had in the compositor's own config, to be taken off where
// the compositor can.
type KeyBind struct {
	Keys     string
	Command  string
	Replaces []string
}

// Bind is a shortcut the compositor has from its own config: a command it
// runs (Command), or something of its own (Action, such as "close-window").
type Bind struct {
	Keys    string
	Command string
	Action  string
}

// Configurer applies inua's settings and lists the compositor's own
// shortcuts. CapKeyboard, CapTouchpad and CapBinds say which parts it
// takes; Hint says what to do about one that is Unavailable.
type Configurer interface {
	ApplySettings(ctx context.Context, s Settings) error
	Binds(ctx context.Context) ([]Bind, error)
	// Source names where the settings go, to show: "niri's inua.kdl".
	Source() string
	// Hint is what the user can do so that c works, "" when nothing.
	Hint(c Capability) string
}

// Exiter ends the session (logs out).
type Exiter interface {
	Exit(ctx context.Context) error
}

// Combo is a shortcut's modifiers and its key, an XKB key name.
type Combo struct {
	Super, Ctrl, Alt, Shift bool
	Key                     string
}

// ParseKeys reads "SUPER+SHIFT+S"; false for an empty or malformed one.
func ParseKeys(keys string) (Combo, bool) {
	parts := strings.Split(keys, "+")
	if keys == "" || parts[len(parts)-1] == "" {
		return Combo{}, false
	}
	c := Combo{Key: parts[len(parts)-1]}
	for _, m := range parts[:len(parts)-1] {
		switch strings.ToUpper(m) {
		case "SUPER":
			c.Super = true
		case "CTRL", "CONTROL":
			c.Ctrl = true
		case "ALT":
			c.Alt = true
		case "SHIFT":
			c.Shift = true
		default:
			return Combo{}, false
		}
	}
	return c, true
}

// String writes a combo back as ParseKeys reads it, a single letter in
// upper case.
func (c Combo) String() string {
	var parts []string
	for _, m := range []struct {
		on   bool
		name string
	}{{c.Super, "SUPER"}, {c.Ctrl, "CTRL"}, {c.Alt, "ALT"}, {c.Shift, "SHIFT"}} {
		if m.on {
			parts = append(parts, m.name)
		}
	}
	key := c.Key
	if len(key) == 1 {
		key = strings.ToUpper(key)
	}
	return strings.Join(append(parts, key), "+")
}
