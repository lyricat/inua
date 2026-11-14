package ui

import (
	"log"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/uikit"
)

// The bar's menu over an empty desktop (shell.EmptyDesktop): the shell's
// own commands, in the groups a desktop has, where an application's menu
// would be.

// systemLine is a line of the system menu: a command, a submenu, or a
// hairline between groups. Labels are i18n.M, said in the user's language
// when the menu opens.
type systemLine struct {
	label     string
	command   string // for runSystemCommand
	lines     []systemLine
	separator bool
}

// settingsPageTitles are the settings pages' titles, as settings.ui gives
// them, for the system menu, which lists the pages before the window that
// has them is made.
var settingsPageTitles = map[string]string{
	"wifi":          i18n.M("Wi-Fi"),
	"bluetooth":     i18n.M("Bluetooth"),
	"displays":      i18n.M("Displays"),
	"keyboard":      i18n.M("Keyboard"),
	"mouse":         i18n.M("Mouse & Touch"),
	"sound":         i18n.M("Sound"),
	"voice":         i18n.M("Dictation"),
	"securitykeys":  i18n.M("Security Keys"),
	"appearance":    i18n.M("Appearance"),
	"bar":           i18n.M("Bar & Dock"),
	"launcher":      i18n.M("Launcher"),
	"notifications": i18n.M("Notifications"),
	"shortcuts":     i18n.M("Shortcuts"),
	"apps":          i18n.M("Default Apps"),
	"ai":            i18n.M("AI"),
	"clipboard":     i18n.M("Clipboard"),
	"keyring":       i18n.M("Keyring"),
	"compositor":    i18n.M("Compositor"),
	"power":         i18n.M("Power"),
	"about":         i18n.M("About"),
}

// systemMenu is the system menu's first level, each a section of lines:
// System (every settings page, in the index's groups, then the launcher,
// a screenshot and the control center), Session, Power (Hibernate only
// where the machine can) and Help.
func systemMenu(canHibernate bool) []systemLine {
	var pages []systemLine
	for _, g := range settingsGroups {
		if len(pages) > 0 {
			pages = append(pages, systemLine{separator: true})
		}
		for _, name := range g.pages {
			pages = append(pages, systemLine{label: settingsPageTitles[name], command: "settings:" + name})
		}
	}
	power := []systemLine{{label: i18n.M("Suspend"), command: "suspend"}}
	if canHibernate {
		power = append(power, systemLine{label: i18n.M("Hibernate"), command: "hibernate"})
	}
	power = append(power,
		systemLine{separator: true},
		systemLine{label: i18n.M("Restart"), command: "reboot"},
		systemLine{label: i18n.M("Shut Down"), command: "poweroff"},
	)
	return []systemLine{
		{label: i18n.M("System"), lines: []systemLine{
			{label: i18n.M("Settings"), lines: pages},
			{separator: true},
			{label: i18n.M("Launcher"), command: "launcher"},
			{label: i18n.M("Screenshot"), command: "screenshot"},
			{label: i18n.M("Control Center"), command: "control"},
		}},
		{label: i18n.M("Session"), lines: []systemLine{
			{label: i18n.M("Lock Screen"), command: "lock"},
			{label: i18n.M("Log Out"), command: "logout"},
		}},
		{label: i18n.M("Power"), lines: power},
		{label: i18n.M("Help"), lines: []systemLine{
			{label: i18n.M("About inua"), command: "settings:about"},
			{label: i18n.M("Keyboard Shortcuts"), command: "settings:shortcuts"},
		}},
	}
}

// systemMenuEntries is the system menu's first level as the bar's buttons
// read it.
func systemMenuEntries(canHibernate bool) []appmenu.Entry {
	var out []appmenu.Entry
	for _, l := range systemMenu(canHibernate) {
		out = append(out, appmenu.Entry{Label: i18n.T(l.label), Enabled: true, Submenu: true})
	}
	return out
}

// showSystemMenu pops up the section of the system menu under the bar's
// button i, its lines made as the menu opens: Hibernate as logind last
// said, which askHibernate asked again when the menu came up.
func (s *shellUI) showSystemMenu(i int, w *gtk.Widget) {
	sections := systemMenu(s.control.canHibernate)
	if i < 0 || i >= len(sections) {
		return
	}
	items := s.systemItems(sections[i].lines)
	if len(items) == 0 {
		return
	}
	s.closeAppMenu()
	pop := uikit.NewMenu(items, uikit.Below)
	pop.SetParent(w)
	pop.ConnectClosed(&s.onAppMenuClosed)
	pop.Unref() // the parent holds it now
	pop.Popup()
	s.appmenu.menu = pop
}

// systemItems turns system menu lines into a menu's, with no hairline at
// either end or twice.
func (s *shellUI) systemItems(lines []systemLine) []uikit.Item {
	var out []uikit.Item
	for _, l := range lines {
		if l.separator {
			if len(out) > 0 && !out[len(out)-1].Separator {
				out = append(out, uikit.Item{Separator: true})
			}
			continue
		}
		it := uikit.Item{Label: i18n.T(l.label)}
		if len(l.lines) > 0 {
			it.Items = s.systemItems(l.lines)
		} else {
			command := l.command
			it.Run = func() { s.runSystemCommand(command) }
		}
		out = append(out, it)
	}
	if len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	return out
}

// runSystemCommand does what a line of the system menu says, as the
// launcher and the control center do it. What ends the session, or
// hibernates, asks first as the control center does: it opens with the
// action waiting for its second click.
func (s *shellUI) runSystemCommand(command string) {
	if page, ok := strings.CutPrefix(command, "settings:"); ok {
		s.openSettings()
		if !s.showSettingsPage(page) {
			log.Printf("system menu: no settings page %q", page)
		}
		return
	}
	switch command {
	case "launcher":
		s.showLauncher(shell.Apps)
	case "screenshot":
		s.screenshotRequest("open")
	case "control":
		s.openControl()
	case "lock":
		if err := s.lockScreen(); err != nil {
			log.Printf("system menu: lock: %v", err)
		}
	case "suspend":
		s.doPower(command)
	case "hibernate", "reboot", "poweroff", "logout":
		s.armControlPower(command)
	default:
		log.Printf("system menu: unknown command %q", command)
	}
}

// armControlPower opens the control center with its power actions shown
// and one of them waiting for its second click there.
func (s *shellUI) armControlPower(name string) {
	c := &s.control
	if c.closing {
		return
	}
	s.openControl()
	if !c.open || c.powerLabels[name] == nil {
		return
	}
	c.power.SetActive(true)
	if c.armed != name {
		s.powerAction(name)
	}
}
