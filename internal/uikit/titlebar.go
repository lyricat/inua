package uikit

import "codeberg.org/puregotk/puregotk/v4/gtk"

// TitlebarWindow is a window that can take a titlebar.
type TitlebarWindow interface {
	GetTitlebar() *gtk.Widget
	SetTitlebar(*gtk.Widget)
}

// AddTitlebar gives win the titlebar its Wayfire session needs. GTK4 keeps
// decorations on its own side, and Wayfire 0.9's decoration plugin leaves
// Wayland clients alone, so a window without a titlebar has no title, no
// minimize, maximize or close button, and no bar to drag it by. The headerbar
// shows the window's title and GTK's window buttons; a window that already
// has a titlebar keeps it.
func AddTitlebar(win TitlebarWindow) {
	if win.GetTitlebar() != nil {
		return
	}
	bar := gtk.NewHeaderBar()
	bar.AddCssClass("window-titlebar")
	bar.SetShowTitleButtons(true)
	bar.SetDecorationLayout(":minimize,maximize,close")
	win.SetTitlebar(&bar.Widget)
}
