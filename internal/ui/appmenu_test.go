package ui

import (
	"testing"

	"github.com/lyricat/inua/internal/config"
)

// menuWanted is the per-application switch over the bar's own: with the bar's
// menu on, an application in menu_off still has none; with it off, one in
// menu_on has one anyway.
func TestMenuWantedPerApplication(t *testing.T) {
	cases := []struct {
		name  string
		bar   bool
		off   []string
		on    []string
		class string
		want  bool
	}{
		{"the bar's switch alone", true, nil, nil, "firefox", true},
		{"the bar's switch off", false, nil, nil, "firefox", false},
		{"an application left out", true, []string{"firefox"}, nil, "firefox", false},
		{"another application", true, []string{"firefox"}, nil, "dolphin", true},
		{"an application turned on", false, nil, []string{"firefox"}, "firefox", true},
		{"left out on both sides", true, []string{"firefox"}, []string{"firefox"}, "firefox", false},
		{"a window with no class", true, nil, nil, "", false},
		// A desktop ID matches when the dock knows the application: the test
		// shell has no applications scanned, so only the class matches here.
		{"a desktop ID, with no application scanned", true, []string{"org.mozilla.firefox"}, nil, "firefox", true},
		{"listed in another case", true, []string{"Firefox"}, nil, "firefox", false},
	}
	for _, tc := range cases {
		s := &shellUI{cfg: config.Config{Bar: config.Bar{
			Menu: tc.bar, MenuOff: tc.off, MenuOn: tc.on,
		}}}
		if got := s.menuWanted(tc.class); got != tc.want {
			t.Errorf("%s: menuWanted(%q) = %v, want %v", tc.name, tc.class, got, tc.want)
		}
	}
}
