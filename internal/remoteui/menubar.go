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
			// As the page's buttons allow: a machine selected, paired and
			// reachable, or one already streaming, which comes forward.
			if w.selected == "sharing" {
				return
			}
			m, _ := w.machine()
			if _, ok := w.streams[m.Addr]; ok || w.machineConnect.GetVisible() && w.machineConnect.GetSensitive() {
				w.connect()
			}
		case "disconnect":
			if w.selected != "sharing" && w.machineQuit.GetVisible() {
				w.quit()
			}
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
		{Title: i18n.M("File"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("close-window", i18n.M("Close Window")),
		}}}},
		{Title: i18n.M("Session"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("connect", i18n.M("Connect")),
			action("disconnect", i18n.M("End Remote Session")),
		}}, {Actions: []appui.MenuAction{
			action("show-sharing", i18n.M("This Computer")),
		}}}},
		{Title: i18n.M("View"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("refresh", i18n.M("Refresh")),
			action("find", i18n.M("Find Machines")),
		}}}},
	}
	return appui.NewMenuBar(w.c, groups)
}
