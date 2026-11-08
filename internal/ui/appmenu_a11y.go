package ui

import (
	"log"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/uikit"
)

// loadA11yMenu reads a bar's menu through the application's accessibility
// tree, for toolkits that publish no dbusmenu.
func (s *shellUI) loadA11yMenu(b *bar, pid int) {
	if s.appmenu.a11y == nil {
		s.appmenu.a11y = appmenu.NewA11y()
	}
	a11y := s.appmenu.a11y
	go func() {
		entries, err := a11y.Menu(pid)
		gtkx.Post(func() {
			if b.appMenuPID != pid || b.appMenuReg != (appmenu.Registration{}) {
				return
			}
			if err != nil {
				log.Printf("appmenu accessibility %d: %v", pid, err)
				return
			}
			if !appmenu.FillableMenu(entries) {
				entries = nil
			}
			s.renderAppMenu(b, entries)
		})
	}()
}

// a11yMenuItems turns accessibility entries into a menu's lines.
func (s *shellUI) a11yMenuItems(pid int, entries []appmenu.Entry) []uikit.Item {
	var out []uikit.Item
	for _, e := range entries {
		if e.Separator {
			if len(out) > 0 && !out[len(out)-1].Separator {
				out = append(out, uikit.Item{Separator: true})
			}
			continue
		}
		it := uikit.Item{Label: e.Label}
		switch {
		case e.Toggle == "checkmark" && e.Checked:
			it.Mark = "check"
		case e.Toggle == "radio" && e.Checked:
			it.Mark = "radio"
		case e.Toggle != "":
			it.Mark = "off"
		}
		switch {
		case e.Submenu || len(e.Children) > 0:
			if e.Enabled {
				it.Items = s.a11yMenuItems(pid, e.Children)
			}
		case e.Enabled:
			entry := e
			it.Run = func() { s.activateA11yEntry(pid, entry) }
		}
		out = append(out, it)
	}
	if len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	return out
}

// showA11yMenu opens a section of the accessibility menu under w. Sections
// a browser builds only while open are filled first.
func (s *shellUI) showA11yMenu(b *bar, i int, w *gtk.Widget) {
	if i < 0 || i >= len(b.appMenuEntries) {
		return
	}
	entry := b.appMenuEntries[i]
	pid := b.appMenuPID
	s.appmenu.menuRequest++
	request := s.appmenu.menuRequest
	w.Ref()
	go func() {
		var entries []appmenu.Entry
		var err error
		if len(entry.Children) > 0 {
			entries = entry.Children
		} else if s.appmenu.a11y != nil {
			entries, err = s.appmenu.a11y.FillSection(entry)
		}
		gtkx.Post(func() {
			defer w.Unref()
			if err != nil {
				log.Printf("appmenu open %q: %v", entry.Label, err)
				return
			}
			items := s.a11yMenuItems(pid, entries)
			if s.appmenu.menuRequest != request || len(items) == 0 || !w.GetMapped() || !s.a11yMenuCurrent(pid) {
				return
			}
			s.closeAppMenu()
			pop := uikit.NewMenu(items, uikit.Below)
			pop.SetParent(w)
			pop.ConnectClosed(&s.onAppMenuClosed)
			pop.Unref()
			pop.Popup()
			s.appmenu.menu = pop
		})
	}()
}

// a11yMenuCurrent is whether pid's accessibility menu is still the one the
// focused monitor's bar shows.
func (s *shellUI) a11yMenuCurrent(pid int) bool {
	if pid <= 0 || focusedPID(s.snap) != pid {
		return false
	}
	for _, b := range s.bars {
		if b.appMenuPID == pid && b.appMenuReg == (appmenu.Registration{}) {
			return true
		}
	}
	return false
}

// activateA11yEntry presses an accessibility entry while its process still
// has the focus, then reads the menu again: an action may change it.
func (s *shellUI) activateA11yEntry(pid int, e appmenu.Entry) {
	if !s.a11yMenuCurrent(pid) || s.appmenu.a11y == nil {
		return
	}
	a11y := s.appmenu.a11y
	go func() {
		if err := a11y.ActivateIn(e); err != nil {
			log.Printf("appmenu activate: %v", err)
		}
		gtkx.Post(func() {
			if !s.a11yMenuCurrent(pid) {
				return
			}
			for _, b := range s.bars {
				if b.appMenuPID == pid && b.appMenuReg == (appmenu.Registration{}) {
					s.loadA11yMenu(b, pid)
				}
			}
		})
	}()
}
