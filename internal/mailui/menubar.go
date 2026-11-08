package mailui

import (
	"codeberg.org/puregotk/puregotk/v4/gio"

	"github.com/lyricat/inua/internal/apps/mail"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/i18n"
)

// menuBar gives Mail's commands a place in GTK's application menu model.
func (w *window) menuBar() *appui.MenuBar {
	activate := func(action gio.SimpleAction, _ uintptr) {
		switch action.GetName() {
		case "new-message":
			w.newComposer(mail.Draft{}, nil)
		case "refresh":
			w.refresh()
		case "toggle-sidebar":
			w.toggleSidebar()
		case "toggle-assistant":
			w.toggleAssist()
		case "search":
			w.search.GrabFocus()
		case "mark-read":
			w.markAllRead()
		case "settings":
			w.showSettings("")
		case "keys":
			w.showKeys()
		case "close-window":
			w.win.Close()
		}
	}
	action := func(name, label string) appui.MenuAction {
		return appui.MenuAction{Name: name, Label: label, Activate: activate}
	}
	groups := []appui.MenuGroup{
		{Title: i18n.T("File"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("new-message", i18n.T("New Message")),
		}}, {Actions: []appui.MenuAction{
			action("close-window", i18n.T("Close Window")),
		}}}},
		{Title: i18n.T("View"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("refresh", i18n.T("Refresh")),
			action("search", i18n.T("Search")),
		}}, {Actions: []appui.MenuAction{
			action("toggle-sidebar", i18n.T("Sidebar")),
			action("toggle-assistant", i18n.T("Assistant")),
		}}, {Actions: []appui.MenuAction{
			action("mark-read", i18n.T("Mark All Read")),
		}}}},
		{Title: i18n.T("Help"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("keys", i18n.T("Keyboard Shortcuts")),
			action("settings", i18n.T("Settings")),
		}}}},
	}
	return appui.NewMenuBar(w.c, groups)
}
