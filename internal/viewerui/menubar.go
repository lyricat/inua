package viewerui

import (
	"codeberg.org/puregotk/puregotk/v4/gio"

	"github.com/lyricat/inua/internal/appui"
)

// menuBar gives the viewer's commands, also on its toolbar and keys, a
// place in the menu the bar shows while its window has the focus. The
// process has one window, so the menu goes away with it.
func (a *App) menuBar() *appui.MenuBar {
	activate := func(action gio.SimpleAction, _ uintptr) {
		switch action.GetName() {
		case "details":
			on := !a.detailsReveal.GetRevealChild()
			a.detailsReveal.SetRevealChild(on)
			a.detailsToggle.SetActive(on)
		case "source":
			on := !a.sourceToggle.GetActive()
			a.sourceToggle.SetActive(on)
			a.text.showSource(on)
		case "close-window":
			a.win.Close()
		default:
			a.action(action.GetName())
		}
	}
	action := func(name, label string) appui.MenuAction {
		return appui.MenuAction{Name: name, Label: label, Activate: activate}
	}
	groups := []appui.MenuGroup{
		{Title: "File", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("open", "Open…"),
		}}, {Actions: []appui.MenuAction{
			action("close-window", "Close Window"),
		}}}},
		{Title: "Edit", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("copy", "Copy Image"),
			action("wallpaper", "Set as Wallpaper"),
			action("folder", "Show in Folder"),
		}}}},
		{Title: "View", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("zoom_in", "Zoom In"),
			action("zoom_out", "Zoom Out"),
			action("zoom_level", "Reset Zoom"),
		}}, {Actions: []appui.MenuAction{
			action("prev", "Previous File"),
			action("next", "Next File"),
			action("rotate", "Rotate"),
		}}, {Actions: []appui.MenuAction{
			action("details", "Details"),
			action("source", "Source"),
			action("fullscreen", "Fullscreen"),
		}}}},
	}
	return appui.NewMenuBar(a.c, groups)
}
