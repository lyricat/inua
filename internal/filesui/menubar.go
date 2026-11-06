package filesui

import (
	"codeberg.org/puregotk/puregotk/v4/gio"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/appui"
)

// menuBar gives the file manager's commands, also available from its keys
// and context menus, a place in GTK's application menu model.
func (w *window) menuBar() *appui.MenuBar {
	activate := func(action gio.SimpleAction, _ uintptr) {
		p := w.cur()
		switch action.GetName() {
		case "undo":
			w.app.undo(w)
		case "new-tab":
			p.newTab(p.locationKey(), true)
		case "back":
			p.goBack()
		case "forward":
			p.goForward()
		case "up":
			p.goUp()
		case "search":
			p.startSearch()
		case "view-list":
			p.setView(files.ListView)
		case "view-grid":
			p.setView(files.GridView)
		case "view-columns":
			p.setView(files.ColumnView)
		case "toggle-details":
			w.setDetails(!w.detailsReveal.GetRevealChild())
		case "select-all":
			p.sel.SelectAll()
		case "invert-selection":
			w.invertSelection()
		case "keys":
			w.showKeys()
		case "close-window":
			w.win.Close()
		default:
			w.do(action.GetName())
		}
	}
	action := func(name, label string) appui.MenuAction {
		return appui.MenuAction{Name: name, Label: label, Activate: activate}
	}
	groups := []appui.MenuGroup{
		{Title: "File", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("new-window", "New Window"),
			action("new-tab", "New Tab"),
			action("new-folder", "New folder…"),
			action("new-file", "New file…"),
		}}, {Actions: []appui.MenuAction{
			action("open", "Open"),
			action("open-with", "Open with…"),
		}}, {Actions: []appui.MenuAction{
			action("close-window", "Close Window"),
		}}}},
		{Title: "Edit", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("undo", "Undo"),
		}}, {Actions: []appui.MenuAction{
			action("cut", "Cut"),
			action("copy", "Copy"),
			action("paste", "Paste"),
			action("delete", "Delete for good…"),
		}}, {Actions: []appui.MenuAction{
			action("select-all", "Select All"),
			action("invert-selection", "Invert Selection"),
		}}}},
		{Title: "View", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("back", "Back"),
			action("forward", "Forward"),
			action("up", "Up"),
			action("reload", "Reload"),
			action("search", "Search"),
		}}, {Actions: []appui.MenuAction{
			action("view-list", "List"),
			action("view-grid", "Grid"),
			action("view-columns", "Columns"),
		}}, {Actions: []appui.MenuAction{
			action("hidden", "Hidden files"),
			action("toggle-details", "Details"),
		}}}},
		{Title: "Help", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("keys", "Keyboard Shortcuts"),
		}}}},
	}
	return appui.NewMenuBar(w.app.c, groups)
}
