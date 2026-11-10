package diskui

import (
	"codeberg.org/puregotk/puregotk/v4/gio"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/i18n"
)

// menuBar gives Disk Usage's commands, also on its buttons and keys, a place
// in the menu the bar shows while this window has the focus.
func (w *window) menuBar() *appui.MenuBar {
	activate := func(action gio.SimpleAction, _ uintptr) {
		switch action.GetName() {
		case "choose-folder":
			w.chooseFolder()
		case "scan":
			w.scan(w.place)
		case "stop":
			if s := w.scans[w.place]; s.running() {
				s.cancel()
			}
		case "copy-command":
			w.copyCommand()
		case "close-window":
			w.win.Close()
		}
	}
	action := func(name, label string) appui.MenuAction {
		return appui.MenuAction{Name: name, Label: label, Activate: activate}
	}
	groups := []appui.MenuGroup{
		{Title: i18n.M("File"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("choose-folder", i18n.M("Choose Folder…")),
		}}, {Actions: []appui.MenuAction{
			action("close-window", i18n.M("Close Window")),
		}}}},
		{Title: i18n.M("Edit"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("copy-command", i18n.M("Copy Cleanup Command")),
		}}}},
		{Title: i18n.M("View"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("scan", i18n.M("Scan")),
			action("stop", i18n.M("Stop Scanning")),
		}}}},
	}
	return appui.NewMenuBar(w.c, groups)
}
