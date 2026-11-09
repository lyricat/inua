package ui

import (
	"log"

	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/uikit"
)

// systemMenuEntries is the bar's menu on a workspace with no windows: the
// shell's own commands, in the groups a desktop has. The application title
// menu shows the same items, so the two cannot drift.
func (s *shellUI) systemMenuEntries() []appmenu.Entry {
	item := func(label, command string) appmenu.Entry {
		return appmenu.Entry{Label: label, Enabled: true, Action: appmenu.Action{Kind: appmenu.ActionSystem, Bus: command}}
	}
	section := func(label string, items ...appmenu.Entry) appmenu.Entry {
		return appmenu.Entry{Label: label, Enabled: true, Submenu: true, Children: items}
	}
	power := []appmenu.Entry{
		item(i18n.T("Suspend"), "suspend"),
	}
	if s.control.canHibernate {
		power = append(power, item(i18n.T("Hibernate"), "hibernate"))
	}
	power = append(power,
		appmenu.Entry{Separator: true},
		item(i18n.T("Restart"), "reboot"),
		item(i18n.T("Shut Down"), "poweroff"),
	)
	return []appmenu.Entry{
		section(i18n.T("System"),
			item(i18n.T("Settings"), "settings"),
			item(i18n.T("Launcher"), "launcher"),
			item(i18n.T("Screenshot"), "screenshot"),
			item(i18n.T("Control Center"), "control"),
		),
		section(i18n.T("Session"),
			item(i18n.T("Lock Screen"), "lock"),
			item(i18n.T("Log Out"), "logout"),
		),
		section(i18n.T("Power"), power...),
	}
}

// systemMenuItems is the same menu as lines, for the application title.
func (s *shellUI) systemMenuItems() []uikit.Item {
	var out []uikit.Item
	for i, section := range s.systemMenuEntries() {
		if i > 0 {
			out = append(out, uikit.Item{Separator: true})
		}
		for _, e := range section.Children {
			if e.Separator {
				if len(out) > 0 && !out[len(out)-1].Separator {
					out = append(out, uikit.Item{Separator: true})
				}
				continue
			}
			command := e.Action.Bus
			out = append(out, uikit.Item{Label: e.Label, Run: func() { s.runSystemAction(command) }})
		}
	}
	if len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	return out
}

// runSystemAction runs one of the system menu's commands.
func (s *shellUI) runSystemAction(command string) {
	switch command {
	case "settings":
		s.openSettings()
	case "launcher":
		s.showLauncher(shell.Apps)
	case "screenshot":
		s.screenshotRequest("area")
	case "control":
		s.toggleControl()
	case "lock":
		if err := s.lockScreen(); err != nil {
			log.Printf("system menu: lock: %v", err)
		}
	case "logout", "suspend", "hibernate", "reboot", "poweroff":
		s.doPower(command)
	}
}
