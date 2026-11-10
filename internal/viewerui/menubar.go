package viewerui

import (
	"codeberg.org/puregotk/puregotk/v4/gio"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/i18n"
)

// menuBar gives the viewer's commands, also on its toolbar and keys, a
// place in the menu the bar shows while its window has the focus. The
// process has one window, so the menu goes away with it.
func (a *App) menuBar() *appui.MenuBar {
	activate := func(action gio.SimpleAction, _ uintptr) {
		switch name := action.GetName(); {
		case a.edit.on && name != "details" && name != "fullscreen" && name != "folder" && name != "close-window":
			a.editMenu(name)
		case name == "details":
			on := !a.detailsReveal.GetRevealChild()
			a.detailsReveal.SetRevealChild(on)
			a.detailsToggle.SetActive(on)
		case name == "source":
			on := !a.sourceToggle.GetActive()
			a.sourceToggle.SetActive(on)
			a.text.showSource(on)
		case name == "close-window":
			a.win.Close()
		default:
			a.action(name)
		}
	}
	action := func(name, label string) appui.MenuAction {
		return appui.MenuAction{Name: name, Label: label, Activate: activate}
	}
	groups := []appui.MenuGroup{
		{Title: i18n.M("File"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("open", i18n.M("Open…")),
		}}, {Actions: []appui.MenuAction{
			action("close-window", i18n.M("Close Window")),
		}}}},
		{Title: i18n.M("Edit"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("copy", i18n.M("Copy Image")),
			action("wallpaper", i18n.M("Set as Wallpaper")),
			action("folder", i18n.M("Show in Folder")),
		}}}},
		{Title: i18n.M("View"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("zoom_in", i18n.M("Zoom In")),
			action("zoom_out", i18n.M("Zoom Out")),
			action("zoom_level", i18n.M("Reset Zoom")),
		}}, {Actions: []appui.MenuAction{
			action("prev", i18n.M("Previous File")),
			action("next", i18n.M("Next File")),
			action("rotate", i18n.M("Rotate")),
		}}, {Actions: []appui.MenuAction{
			action("details", i18n.M("Details")),
			action("source", i18n.M("Source")),
			action("fullscreen", i18n.M("Fullscreen")),
		}}}},
	}
	return appui.NewMenuBar(a.c, groups)
}
