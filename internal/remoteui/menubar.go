package remoteui

import (
	"context"

	"codeberg.org/puregotk/puregotk/v4/gio"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/i18n"
)

func (w *window) menuBar() *appui.MenuBar {
	activate := func(action gio.SimpleAction, _ uintptr) {
		switch action.GetName() {
		case "close-window":
			w.win.Close()
		case "connect":
			w.connect()
		case "disconnect":
			w.quit()
		case "show-sharing":
			w.choose("sharing")
		case "find":
			w.find()
		case "refresh":
			if w.selected == "sharing" {
				go w.refreshSharing(context.Background())
			} else if w.selected != "" {
				go w.ask(context.Background(), w.selected)
			}
		}
	}
	action := func(name, label string) appui.MenuAction {
		return appui.MenuAction{Name: name, Label: label, Activate: activate}
	}
	groups := []appui.MenuGroup{
		{Title: i18n.T("File"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("close-window", i18n.T("Close Window")),
		}}}},
		{Title: i18n.T("Session"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("connect", i18n.T("Connect")),
			action("disconnect", i18n.T("End Remote Session")),
		}}, {Actions: []appui.MenuAction{
			action("show-sharing", i18n.T("This Computer")),
		}}}},
		{Title: i18n.T("View"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("refresh", i18n.T("Refresh")),
			action("find", i18n.T("Find Machines")),
		}}}},
	}
	return appui.NewMenuBar(w.c, groups)
}
