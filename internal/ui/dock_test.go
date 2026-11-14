package ui

import (
	"testing"

	"github.com/lyricat/inua/internal/config"
)

// The dock is built for the overview even when the desktop does not show
// it, and only a desktop dock that stays up reserves its edge.
func TestDockWantedAndExclusive(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		enable, showDock, autohide   bool
		layer                        bool
		wantBuilt, wantExclusiveZone bool
	}{
		{name: "desktop dock", enable: true, showDock: true, layer: true, wantBuilt: true, wantExclusiveZone: true},
		{name: "desktop dock, not in the overview", enable: true, layer: true, wantBuilt: true, wantExclusiveZone: true},
		{name: "desktop dock autohiding", enable: true, showDock: true, autohide: true, layer: true, wantBuilt: true},
		{name: "overview only", showDock: true, layer: true, wantBuilt: true},
		{name: "overview only, autohide set", showDock: true, autohide: true, layer: true, wantBuilt: true},
		{name: "neither", layer: true},
		{name: "no layer shell", enable: true, showDock: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := config.Default()
			c.Dock.Enable, c.Dock.Autohide, c.Overview.ShowDock = tc.enable, tc.autohide, tc.showDock
			if got := dockWanted(c, tc.layer); got != tc.wantBuilt {
				t.Errorf("dockWanted = %v, want %v", got, tc.wantBuilt)
			}
			if !tc.wantBuilt {
				return
			}
			if got := dockExclusive(c.Dock); got != tc.wantExclusiveZone {
				t.Errorf("dockExclusive = %v, want %v", got, tc.wantExclusiveZone)
			}
		})
	}
}

// The overview leaves room for the dock it raises, whether or not the
// desktop shows the dock.
func TestOverviewDockInsets(t *testing.T) {
	c := config.Default()
	c.Dock.Size = 40
	c.Dock.Position = "left"
	for _, enable := range []bool{true, false} {
		c.Dock.Enable = enable
		c.Overview.ShowDock = true
		s := &shellUI{cfg: c, layer: true}
		if top, bottom, start, end := s.overviewDockInsets(); top != 0 || bottom != 0 || start != 56 || end != 0 {
			t.Errorf("dock.enable=%v: insets %d %d %d %d, want 0 0 56 0", enable, top, bottom, start, end)
		}
		s.cfg.Overview.ShowDock = false
		if top, bottom, start, end := s.overviewDockInsets(); top|bottom|start|end != 0 {
			t.Errorf("dock.enable=%v, show_dock off: insets %d %d %d %d, want none", enable, top, bottom, start, end)
		}
	}
	s := &shellUI{cfg: c}
	s.cfg.Overview.ShowDock = true
	if top, bottom, start, end := s.overviewDockInsets(); top|bottom|start|end != 0 {
		t.Errorf("without layer shell: insets %d %d %d %d, want none", top, bottom, start, end)
	}
}
