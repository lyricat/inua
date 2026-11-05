package desktop

import "strings"

// Wayfire reports whether the current session identifies itself as Wayfire.
func Wayfire() bool {
	for _, name := range CurrentDesktops() {
		if strings.EqualFold(name, "wayfire") {
			return true
		}
	}
	return false
}
