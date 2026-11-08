package gamesui

import (
	"codeberg.org/puregotk/puregotk/v4/gio"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/i18n"
)

// menuBar publishes the game's commands in GTK's application menu model.
func (v *view) menuBar() *appui.MenuBar {
	activate := func(action gio.SimpleAction, _ uintptr) {
		switch action.GetName() {
		case "new-game":
			v.g.reset()
		case "fullscreen":
			if v.f.win.IsFullscreen() {
				v.f.win.Unfullscreen()
			} else {
				v.f.win.Fullscreen()
			}
		case "close-window":
			v.f.win.Close()
		}
	}
	action := func(name, label string) appui.MenuAction {
		return appui.MenuAction{Name: name, Label: label, Activate: activate}
	}
	groups := []appui.MenuGroup{
		{Title: i18n.T("Game"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("new-game", i18n.T("New game")),
		}}}},
		{Title: i18n.T("View"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("fullscreen", i18n.T("Fullscreen")),
		}}}},
		{Title: i18n.T("File"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("close-window", i18n.T("Close Window")),
		}}}},
	}
	return appui.NewMenuBar(v.f.c, groups)
}
