package connectui

import (
	"codeberg.org/puregotk/puregotk/v4/gio"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/i18n"
)

func (w *window) menuBar() *appui.MenuBar {
	activate := func(action gio.SimpleAction, _ uintptr) {
		switch action.GetName() {
		case "close-window":
			w.win.Close()
		case "refresh":
			w.fail("")
			w.call(nil, "Refresh")
		case "add-device":
			w.choose("add")
		}
	}
	action := func(name, label string) appui.MenuAction {
		return appui.MenuAction{Name: name, Label: label, Activate: activate}
	}
	groups := []appui.MenuGroup{
		{Title: i18n.M("File"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("close-window", i18n.M("Close Window")),
		}}}},
		{Title: i18n.M("View"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("refresh", i18n.M("Refresh")),
			action("add-device", i18n.M("Add Device by Address…")),
		}}}},
	}
	return appui.NewMenuBar(w.c, groups)
}
