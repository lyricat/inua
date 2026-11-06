package termui

import (
	"log"

	"codeberg.org/puregotk/puregotk/v4/gio"

	"github.com/lyricat/inua/internal/dbusmenu"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/settings"
)

// menuGroup is one of the window's menus and its sections of action names.
type menuGroup struct {
	title   string
	actions [][]string // sections
}

// menuGroups is the window's actions, as the command palette has them, in
// the menus a Mac app has.
func menuGroups() ([]menuGroup, map[string]string) {
	label := map[string]string{}
	for _, act := range settings.Actions {
		label[act.Name] = i18n.T(act.Label)
	}
	label["quit"] = i18n.T("Quit")
	return []menuGroup{
		{i18n.T("Terminal"), [][]string{{"settings"}, {"restart_host"}, {"quit"}}},
		{i18n.T("Shell"), [][]string{{"new_session", "new_workspace"}, {"split_right", "split_down", "close_pane"}, {"rename"}}},
		{i18n.T("Edit"), [][]string{{"copy", "paste"}, {"find", "copy_mode"}}},
		{i18n.T("View"), [][]string{{"zoom", "equalize", "next_layout", "swap"}, {"fold_workspaces", "fold_sessions"}, {"font_larger", "font_smaller", "font_reset"}}},
		{i18n.T("Window"), [][]string{{"previous_session", "next_session"}, {"next_waiting", "next_idle", "agents"}}},
		{i18n.T("Help"), [][]string{{"palette", "keys"}}},
	}, label
}

// menuBar gives a Mac's menu bar the window's actions. The keys stay the
// window's own (chord): the menus show none, so a chord never runs twice.
func (a *App) menuBar() {
	a.onMenuItem = func(act gio.SimpleAction, _ uintptr) {
		switch name := act.GetName(); name {
		case "quit":
			a.c.App.Quit()
		default:
			a.do(name, a.focusedPane())
		}
	}
	add := func(name string) {
		act := gio.NewSimpleAction(name, nil)
		act.ConnectActivate(&a.onMenuItem)
		a.c.App.AddAction(act)
		act.Unref()
	}
	groups, label := menuGroups()
	for _, act := range settings.Actions {
		add(act.Name)
	}
	add("quit")
	bar := gio.NewMenu()
	for _, m := range groups {
		menu := gio.NewMenu()
		for _, section := range m.actions {
			sec := gio.NewMenu()
			for _, name := range section {
				sec.Append(label[name], "app."+name)
			}
			menu.AppendSection("", &sec.MenuModel)
			sec.Unref()
		}
		bar.AppendSubmenu(m.title, &menu.MenuModel)
		menu.Unref()
	}
	a.c.App.SetMenubar(&bar.MenuModel)
	bar.Unref()
	a.win.SetShowMenubar(true)
}

// publishMenu serves the same menus on the session bus, for the bar to show
// while this window has the focus.
func (a *App) publishMenu() {
	groups, label := menuGroups()
	var nodes []dbusmenu.Node
	for _, g := range groups {
		n := dbusmenu.Node{Label: g.title, Enabled: true}
		for i, section := range g.actions {
			if i > 0 {
				n.Children = append(n.Children, dbusmenu.Node{Separator: true})
			}
			for _, name := range section {
				n.Children = append(n.Children, dbusmenu.Node{Label: label[name], Action: name, Enabled: true})
			}
		}
		nodes = append(nodes, n)
	}
	menu, err := dbusmenu.Publish(nodes, func(action string) {
		gtkx.Post(func() {
			if action == "quit" {
				a.c.App.Quit()
				return
			}
			a.do(action, a.focusedPane())
		})
	})
	if err != nil {
		log.Printf("term: menu: %v", err)
		return
	}
	a.menu = menu
}

// focusedPane is the pane that has the keys, or nil.
func (a *App) focusedPane() *termPane {
	for _, t := range a.tiles {
		if p := t.pane; p != nil && p.widget.HasFocus() {
			return p
		}
	}
	return nil
}
