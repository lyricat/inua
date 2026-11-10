package filesui

import (
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/apps/files/disks"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// sideItem is an entry of the sidebar.
type sideItem struct {
	path   string // what it opens, to mark it while a pane is there
	button *gtk.Button
	open   func()
}

// buildSidebar fills the sidebar: places and the trash, bookmarks, drives
// and other mounts, remote disks.
func (w *window) buildSidebar() {
	w.app.h.forget(w.sidePtr)
	w.sidePtr = w.sidePtr[:0]
	w.side = w.side[:0]
	gtkx.Clear(&w.sidebar)

	w.sideHead(i18n.T("PLACES"))
	var marks []files.Place
	for _, pl := range files.Places() {
		if pl.Kind == "bookmark" {
			marks = append(marks, pl)
			continue
		}
		path := pl.Path
		name := pl.Name
		if pl.Kind == "home" {
			name = i18n.T("Home")
		}
		w.sideEntry(name, "", pl.Icon, path, func() { w.cur().goTo(path, true) }, nil)
	}
	if w.pick == nil {
		w.sideEntry(i18n.T("Trash"), "", trashIcon(), trashKey, func() { w.cur().showTrash(true) }, nil,
			menuItem{label: i18n.T("Empty Trash"), run: func() { w.confirmEmptyTrash() }})
	}

	// Without bookmarks the section, head and all, is left out.
	if len(marks) > 0 {
		w.sideHead(i18n.T("BOOKMARKS"))
	}
	for _, pl := range marks {
		path := pl.Path
		w.sideEntry(pl.Name, "", pl.Icon, path, func() { w.cur().goTo(path, true) }, nil,
			menuItem{label: i18n.T("Remove from the bookmarks"), run: func() {
				if _, err := files.ToggleBookmark(path); err != nil {
					w.flash(err.Error())
				}
				w.app.sidebarsChanged()
			}})
	}

	w.sideHead(i18n.T("DEVICES"))
	w.sideEntry(i18n.T("Computer"), "", "computer", "/", func() { w.cur().goTo("/", true) }, nil)
	shown := w.app.remotePoints() // listed as remote disks
	for _, v := range w.app.volumes {
		v := v
		sub := disks.Size(v.Size)
		path := ""
		if v.Mounted() {
			path = v.MountPoints[0]
			shown[path] = true
			if sp, err := files.SpaceOf(path); err == nil && sp.Total > 0 {
				sub = i18n.Tf("%s free of %s", disks.Size(sp.Free), disks.Size(sp.Total))
			}
		} else if v.Encrypted {
			sub = i18n.Tf("%s, locked", disks.Size(v.Size))
		}
		icon := "drive-harddisk"
		switch {
		case v.Drive.Bus == "usb" || v.Drive.Bus == "sdio" || v.Drive.Removable:
			icon = "drive-removable-media"
		}
		var eject func()
		var extra []menuItem
		if v.Mounted() || v.Drive.Removable && (v.Drive.Ejectable || v.Drive.CanPowerOff) {
			eject = func() { w.eject(v) }
			extra = append(extra, menuItem{label: i18n.T("Eject"), run: eject})
		}
		w.sideEntry(v.Name(), sub, icon, path, func() { w.openVolume(v) }, eject, extra...)
	}
	for _, m := range files.UserMounts(w.app.mounts, false) {
		if shown[m.Point] {
			continue
		}
		path := m.Point
		icon := "drive-harddisk"
		if strings.HasPrefix(m.FSType, "fuse.") || m.FSType == "nfs" || m.FSType == "nfs4" || m.FSType == "cifs" {
			icon = "folder-remote"
		}
		w.sideEntry(filepath.Base(path), m.FSType, icon, path, func() { w.cur().goTo(path, true) }, nil)
	}

	w.remoteSide()
	w.markSidebar()
}

func (w *window) sideHead(text string) {
	l := gtk.NewLabel(text)
	l.AddCssClass("kicker")
	l.AddCssClass("files-side-head")
	l.SetXalign(0)
	gtkx.Append(&w.sidebar, &l.Widget)
}

// sideEntry adds an entry: a button that opens it and, for a drive or a
// remote disk, a button that ejects or disconnects it. A right click
// shows its menu: opening it in a new tab or window, and what more there
// is to do with it (extra).
func (w *window) sideEntry(name, sub, icon, path string, open, eject func(), extra ...menuItem) {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	row.AddCssClass("files-side-row")
	b := gtk.NewButton()
	b.AddCssClass("index-item")
	b.AddCssClass("files-side-item")
	b.SetHexpand(true)
	box := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	img := gtk.NewImage()
	w.app.icons.set(img, icon, 18)
	img.AddCssClass("files-side-icon")
	gtkx.Append(box, &img.Widget)
	t := text(name, sub)
	t.SetHexpand(true)
	// Long names are cut, not let widen the sidebar.
	for c := t.GetFirstChild(); c != nil; {
		(&gtk.Label{Widget: *c}).SetMaxWidthChars(18)
		next := c.GetNextSibling()
		c.Unref()
		c = next
	}
	gtkx.Append(box, &t.Widget)
	b.SetChild(&box.Widget)
	box.Unref()
	if sub != "" {
		b.SetTooltipText(name + "\n" + sub)
	}
	w.app.h.click(b, open)
	w.sidePtr = append(w.sidePtr, b.GoPointer())
	var items []menuItem
	if path != "" {
		items = []menuItem{
			{label: i18n.T("Open in a new tab"), run: func() { w.cur().newTab(path, true) }},
			{label: i18n.T("Open in a new window"), run: func() { w.app.ShowFoldersNow([]string{path}) }},
		}
		if path == trashKey {
			items = items[:1]
		}
	}
	if len(extra) > 0 && len(items) > 0 {
		items = append(items, menuItem{})
	}
	items = append(items, extra...)
	if len(items) > 0 {
		g := w.app.h.press(&b.Widget, false, func(button uint32, _ int32, x, y float64) {
			if button != 3 {
				return
			}
			var sx, sy float64
			b.TranslateCoordinates(&w.sidebarCol.Widget, x, y, &sx, &sy)
			w.sideMenu.show(func() []menuItem { return items }, int32(sx), int32(sy))
		})
		w.sidePtr = append(w.sidePtr, g)
	}
	w.side = append(w.side, sideItem{path: path, button: b, open: open})
	gtkx.Append(row, &b.Widget)
	if eject != nil {
		e := gtk.NewButtonFromIconName("media-eject-symbolic")
		e.AddCssClass("plain")
		e.AddCssClass("files-eject")
		e.SetValign(gtk.AlignCenterValue)
		e.SetTooltipText(i18n.T("Eject"))
		w.app.h.click(e, eject)
		w.sidePtr = append(w.sidePtr, e.GoPointer())
		gtkx.Append(row, &e.Widget)
	}
	gtkx.Append(&w.sidebar, &row.Widget)
}

// trashIcon is the trash's icon, full or empty.
func trashIcon() string {
	if es, err := os.ReadDir(filepath.Join(files.HomeTrash(), "files")); err == nil && len(es) > 0 {
		return "user-trash-full"
	}
	return "user-trash"
}

// markSidebar marks the entry of where the pane with the keys is.
func (w *window) markSidebar() {
	p := w.cur()
	here := p.locationKey()
	for _, it := range w.side {
		setClass(&it.button.Widget, "selected", it.path != "" && it.path == here)
	}
}

// sideJump opens the sidebar's entry n (Alt+1 is the first).
func (w *window) sideJump(n int) {
	if n < len(w.side) {
		w.side[n].open()
	}
}
