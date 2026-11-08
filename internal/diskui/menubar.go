package diskui

import (
	"codeberg.org/puregotk/puregotk/v4/gio"

	"github.com/lyricat/inua/internal/appui"
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
		{Title: "File", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("choose-folder", "Choose Folder…"),
		}}, {Actions: []appui.MenuAction{
			action("close-window", "Close Window"),
		}}}},
		{Title: "Edit", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("copy-command", "Copy Cleanup Command"),
		}}}},
		{Title: "View", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("scan", "Scan"),
			action("stop", "Stop Scanning"),
		}}}},
	}
	return appui.NewMenuBar(w.c, groups)
}
