package lightui

import (
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
)

// menuWindow puts a local application menu above an existing window child,
// and publishes it for the bar.
func menuWindow(c *appui.Ctx, win *gtk.ApplicationWindow, groups []appui.MenuGroup) *appui.MenuBar {
	bar := appui.NewMenuBar(c, groups)
	content := win.GetChild() // our reference; gtkx.Append drops it
	root := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	bar.Widget.SetVisible(false)
	bar.AppendLocal(root)
	// The window must let go of its child before the child can go in the box:
	// a widget has one parent, and GTK refuses to append one that still has it.
	win.SetChild(&root.Widget)
	if content != nil {
		gtkx.Append(root, content)
	}
	root.Unref()
	return bar
}
