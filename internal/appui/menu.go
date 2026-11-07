package appui

import (
	"fmt"
	"log"
	"os"
	"sync/atomic"

	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbusmenu"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// menuSeq numbers the menus one process publishes.
var menuSeq atomic.Uint64

// MenuAction is one command in a menu, or — when it has Children — a submenu.
type MenuAction struct {
	Name     string
	Label    string // an i18n message key
	Activate func(gio.SimpleAction, uintptr)
	Children []MenuAction // a submenu hanging from this item
}

// MenuSection groups related commands with a separator.
type MenuSection struct {
	Actions []MenuAction
}

// MenuGroup is a top-level menu, such as View or File.
type MenuGroup struct {
	Title    string // i18n message key
	Sections []MenuSection
}

// MenuBar owns the menu model, its local GTK menu bar, and the D-Bus menu the
// shell reads. Keep it reachable for as long as the window is open.
type MenuBar struct {
	Model   *gio.Menu
	Widget  *gtk.PopoverMenuBar
	group   *gio.SimpleActionGroup
	actions map[string]bool
	server  *dbusmenu.Server
	busName string
}

// NewMenuBar creates the menu's GActions and its GMenu model, and publishes the
// same tree as com.canonical.dbusmenu so the shell can show it in the top bar.
// Labels and group titles are translated with i18n.T.
func NewMenuBar(c *Ctx, groups []MenuGroup) *MenuBar {
	bar := gio.NewMenu()
	actions := gio.NewSimpleActionGroup()
	known := make(map[string]bool)
	for _, group := range groups {
		submenu := gio.NewMenu()
		for _, section := range group.Sections {
			items := gio.NewMenu()
			for _, item := range section.Actions {
				appendAction(items, actions, known, item)
			}
			submenu.AppendSection("", &items.MenuModel)
			items.Unref()
		}
		bar.AppendSubmenu(i18n.T(group.Title), &submenu.MenuModel)
		submenu.Unref()
	}
	widget := gtk.NewPopoverMenuBarFromModel(&bar.MenuModel)
	widget.SetVisible(false) // menus are presented by the shell, not duplicated in the app window
	widget.InsertActionGroup("app", actions)
	m := &MenuBar{Model: bar, Widget: widget, group: actions, actions: known}
	m.publish(groups)
	return m
}

// appendAction adds one item to a menu: a command with its GAction, or a
// submenu when the item has children.
func appendAction(menu *gio.Menu, actions *gio.SimpleActionGroup, known map[string]bool, item MenuAction) {
	if len(item.Children) > 0 {
		sub := gio.NewMenu()
		for _, child := range item.Children {
			appendAction(sub, actions, known, child)
		}
		menu.AppendSubmenu(i18n.T(item.Label), &sub.MenuModel)
		sub.Unref()
		return
	}
	action := gio.NewSimpleAction(item.Name, nil)
	action.ConnectActivate(&item.Activate)
	actions.AddAction(action)
	action.Unref()
	menu.Append(i18n.T(item.Label), "app."+item.Name)
	known[item.Name] = true
}

// publish exports the menu on the session bus and claims it under this
// window's own name. Without a shell that reads the protocol the menu simply
// stays local to the window.
func (m *MenuBar) publish(groups []MenuGroup) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("appui: dbusmenu: %v", err)
		return
	}
	name := dbusmenu.Name(os.Getpid(), menuSeq.Add(1))
	server := dbusmenu.New(conn, func(action string) {
		if !m.actions[action] {
			return
		}
		gtkx.Post(func() {
			if a := m.group.LookupAction(action); a != nil {
				a.Activate(nil)
				func() { o := gobject.Object{Ptr: a.GoPointer()}; o.Unref() }()
			}
		})
	})
	if err = server.OwnName(name); err != nil {
		log.Printf("appui: dbusmenu name: %v", err)
		conn.Close()
		return
	}
	if err = server.Publish(menuNodes(groups)); err != nil {
		log.Printf("appui: dbusmenu publish: %v", err)
		server.Close(name)
		return
	}
	m.server, m.busName = server, name
}

// menuNodes turns the groups into the published tree: one submenu per group,
// a separator between sections, and a submenu wherever an item has children.
func menuNodes(groups []MenuGroup) []dbusmenu.Node {
	out := make([]dbusmenu.Node, 0, len(groups))
	for _, group := range groups {
		top := dbusmenu.Node{Label: i18n.T(group.Title), Enabled: true}
		for i, section := range group.Sections {
			if i > 0 {
				top.Children = append(top.Children, dbusmenu.Node{Separator: true})
			}
			for _, item := range section.Actions {
				top.Children = append(top.Children, menuNode(item))
			}
		}
		out = append(out, top)
	}
	return out
}

func menuNode(item MenuAction) dbusmenu.Node {
	n := dbusmenu.Node{Label: i18n.T(item.Label), Action: item.Name, Enabled: true}
	for _, child := range item.Children {
		n.Children = append(n.Children, menuNode(child))
	}
	return n
}

// Close unregisters the window and drops the D-Bus menu.
func (m *MenuBar) Close() {
	if m.server != nil {
		m.server.Close(m.busName)
	}
}

// AppendLocal inserts the menu bar at the start of a vertical window layout.
func (m *MenuBar) AppendLocal(box *gtk.Box) { box.Prepend(&m.Widget.Widget) }

var _ = fmt.Sprintf
