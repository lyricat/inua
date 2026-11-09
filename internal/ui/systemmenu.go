package ui

import (
	"log"
	"strings"

	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/uikit"
)

// systemSettingsPages names the settings pages the system menu opens, in the
// settings index's order, with the titles the pages carry.
var systemSettingsPages = []struct{ name, title string }{
	{"appearance", i18n.M("Appearance")},
	{"bar", i18n.M("Bar & Dock")},
	{"launcher", i18n.M("Launcher")},
	{"apps", i18n.M("Default Apps")},
	{"ai", i18n.M("AI")},
	{"notifications", i18n.M("Notifications")},
	{"clipboard", i18n.M("Clipboard")},
	{"keyring", i18n.M("Keyring")},
	{"wifi", i18n.M("Wi-Fi")},
	{"bluetooth", i18n.M("Bluetooth")},
	{"displays", i18n.M("Displays")},
	{"keyboard", i18n.M("Keyboard")},
	{"mouse", i18n.M("Mouse & Touch")},
	{"sound", i18n.M("Sound")},
	{"voice", i18n.M("Dictation")},
	{"securitykeys", i18n.M("Security Keys")},
	{"shortcuts", i18n.M("Shortcuts")},
	{"compositor", i18n.M("Compositor")},
	{"power", i18n.M("Power")},
}

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
	var pages []appmenu.Entry
	for _, page := range systemSettingsPages {
		pages = append(pages, item(page.title, "settings:"+page.name))
	}
	settings := appmenu.Entry{Label: i18n.T("Settings"), Enabled: true, Submenu: true, Children: pages}
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
			settings,
			item(i18n.T("Launcher"), "launcher"),
			item(i18n.T("Screenshot"), "screenshot"),
			item(i18n.T("Control Center"), "control"),
		),
		section(i18n.T("Session"),
			item(i18n.T("Lock Screen"), "lock"),
			item(i18n.T("Log Out"), "logout"),
		),
		section(i18n.T("Power"), power...),
		// After the power actions, as a desktop puts Help last.
		section(i18n.T("Help"),
			item(i18n.T("About inua"), "settings:about"),
			item(i18n.T("Keyboard Shortcuts"), "settings:shortcuts"),
		),
	}
}

// systemMenuItems is the same menu as lines, for the application title.
func (s *shellUI) systemMenuItems() []uikit.Item {
	return s.systemItems(s.systemMenuEntries())
}

func (s *shellUI) systemItems(entries []appmenu.Entry) []uikit.Item {
	var out []uikit.Item
	for _, e := range entries {
		if e.Separator {
			if len(out) > 0 && !out[len(out)-1].Separator {
				out = append(out, uikit.Item{Separator: true})
			}
			continue
		}
		if len(e.Children) > 0 {
			out = append(out, uikit.Item{Label: e.Label, Items: s.systemItems(e.Children)})
			continue
		}
		command := e.Action.Bus
		out = append(out, uikit.Item{Label: e.Label, Run: func() { s.runSystemAction(command) }})
	}
	if len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	return out
}

// runSystemAction runs one of the system menu's commands.
func (s *shellUI) runSystemAction(command string) {
	if page, ok := strings.CutPrefix(command, "settings:"); ok {
		s.openSettings()
		s.showSettingsPage(page)
		return
	}
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
