package filesui

import (
	"path/filepath"
	"slices"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/apps/files/chooser"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/portal"
)

// The app chooser asks an app's user which app opens a file or a link
// (the portal's AppChooser): Open With's list, in a window of its own
// over the app that asked.

type appChooser struct {
	a       *App
	win     *gtk.Window
	list    *gtk.Box
	entry   *gtk.Entry
	answer  chan<- string // "" when cancelled
	choices []string      // desktop IDs, without .desktop
	shown   []string      // the ones listed, in order
	last    string
	ptrs    []uintptr // registered with the hub: the window's, then the list's
	fixed   int       // how many are the window's
	done    bool
}

// ChooseApp shows the app chooser and waits for the user
// (chooser.AppPicker); it runs on the request's D-Bus goroutine.
func (a *App) ChooseApp(r chooser.AppRequest, updates <-chan []string, done <-chan struct{}) (string, bool) {
	ch := make(chan string, 1)
	opened := make(chan *appChooser, 1)
	a.whenReady(func() { opened <- a.openAppChooser(r, ch) })
	c := <-opened
	for {
		select {
		case choice := <-ch:
			return choice, choice != ""
		case list := <-updates:
			gtkx.Post(func() { c.setChoices(list) })
		case <-done:
			gtkx.Post(func() { c.finish("") })
			return "", false
		}
	}
}

func (a *App) openAppChooser(r chooser.AppRequest, ch chan<- string) *appChooser {
	c := &appChooser{a: a, answer: ch, choices: r.Choices, last: r.Last}
	win := gtk.NewWindow()
	win.SetApplication(a.c.App)
	for _, cl := range []string{"app", "settings", "files", "files-appchooser"} {
		win.AddCssClass(cl)
	}
	title := i18n.T("Choose an app")
	if name := r.Filename; name != "" {
		title = i18n.Tf("Open “%s” with", filepath.Base(name))
	} else if r.URI != "" {
		title = i18n.T("Open the link with")
	}
	win.SetTitle(title)
	win.SetDefaultSize(420, 480)
	c.win = win

	box := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	box.AddCssClass("files-card")
	head := gtk.NewLabel(title)
	head.AddCssClass("files-card-title")
	head.SetXalign(0)
	head.SetWrap(true)
	gtkx.Append(box, &head.Widget)
	if who := appName(r.AppID); who != "" {
		sub := gtk.NewLabel(i18n.Tf("%s asks which app to use.", who))
		sub.AddCssClass("sub")
		sub.SetXalign(0)
		gtkx.Append(box, &sub.Widget)
	}
	c.entry = gtk.NewEntry()
	c.entry.SetPlaceholderText(i18n.T("Find an app"))
	box.Append(&c.entry.Widget)
	a.h.edit(c.entry, func() { c.fill() })
	a.h.enter(c.entry, func() {
		if len(c.shown) > 0 {
			c.finish(c.shown[0])
		}
	})
	c.ptrs = append(c.ptrs, c.entry.GoPointer())

	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scroll.SetVexpand(true)
	c.list = gtk.NewBox(gtk.OrientationVerticalValue, 0)
	c.list.AddCssClass("files-card-list")
	scroll.SetChild(&c.list.Widget)
	gtkx.Append(box, &scroll.Widget)

	buttons := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	buttons.SetHalign(gtk.AlignEndValue)
	cancel := gtk.NewButtonWithLabel(i18n.T("Cancel"))
	cancel.AddCssClass("outlined")
	a.h.click(cancel, func() { c.finish("") })
	c.ptrs = append(c.ptrs, cancel.GoPointer())
	gtkx.Append(buttons, &cancel.Widget)
	gtkx.Append(box, &buttons.Widget)

	c.ptrs = append(c.ptrs, a.h.key(&win.Widget, false, func(keyval uint32, _ gdk.ModifierType) bool {
		if keyval == uint32(gdk.KEY_Escape) {
			c.finish("")
			return true
		}
		return false
	}))
	a.h.closing[win.GoPointer()] = func() bool {
		c.finish("")
		return false
	}
	c.ptrs = append(c.ptrs, win.GoPointer())
	win.ConnectCloseRequest(&a.h.onClose)

	win.SetChild(&box.Widget)
	box.Unref()
	c.fixed = len(c.ptrs)
	c.fill()
	a.holdPicker()
	win.Present()
	gtkx.SetAppID(win, portal.FilesName)
	gtkx.SetForeignParent(win, r.Parent)
	c.entry.GrabFocus()
	return c
}

// setChoices is the portal's new list while the chooser is open.
func (c *appChooser) setChoices(list []string) {
	if c.done {
		return
	}
	c.choices = list
	c.fill()
}

// fill lists the choices: the one used last first, then by name; typing
// finds among them.
func (c *appChooser) fill() {
	h := c.a.h
	h.forget(c.ptrs[c.fixed:])
	c.ptrs = c.ptrs[:c.fixed]
	gtkx.Clear(c.list)
	var apps []app
	useAppThemeForInua := c.a.c.Config().Style.AppIconsInua
	for _, id := range c.choices {
		info := appInfo(id + ".desktop")
		if info == nil {
			continue
		}
		apps = append(apps, app{id: id, name: info.GetDisplayName(), icon: info.GetIcon()})
	}
	slices.SortStableFunc(apps, func(x, y app) int {
		if (x.id == c.last) != (y.id == c.last) {
			if x.id == c.last {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(x.name), strings.ToLower(y.name))
	})
	if q := strings.TrimSpace(c.entry.GetText()); q != "" {
		apps = files.Rank(apps, q, func(a app) string { return a.name + " " + a.id })
	}
	c.shown = c.shown[:0]
	for _, ap := range apps {
		b := gtk.NewButton()
		b.AddCssClass("files-card-item")
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		var paintable *gtk.IconPaintable
		inuaApp := strings.HasPrefix(ap.id, "io.github.lyricat.Inua.")
		if useAppThemeForInua && inuaApp {
			names := append(slices.Clone(desktop.InuaIconAliases(ap.id)), "applications-other", "application-x-executable")
			for _, name := range names {
				if c.a.icons.theme.HasIcon(name) {
					paintable = c.a.icons.theme.LookupIcon(name, nil, 24, c.win.GetScaleFactor(), gtk.TextDirLtrValue, regular)
					if paintable != nil {
						break
					}
				}
			}
		}
		if paintable == nil && ap.icon != nil && ap.icon.Ptr != 0 && !(useAppThemeForInua && inuaApp) {
			paintable = c.a.icons.theme.LookupByGicon(ap.icon, 24, c.win.GetScaleFactor(), gtk.TextDirLtrValue, regular)
		}
		if paintable != nil {
			img := gtk.NewImage()
			img.SetPixelSize(24)
			img.SetFromPaintable(paintable)
			paintable.Unref()
			gtkx.Append(row, &img.Widget)
		}
		t := text(ap.name, ap.id)
		t.SetHexpand(true)
		gtkx.Append(row, &t.Widget)
		b.SetChild(&row.Widget)
		row.Unref()
		id := ap.id
		h.click(b, func() { c.finish(id) })
		c.ptrs = append(c.ptrs, b.GoPointer())
		gtkx.Append(c.list, &b.Widget)
		c.shown = append(c.shown, id)
	}
}

// finish answers once and closes the window.
func (c *appChooser) finish(choice string) {
	if c.done {
		return
	}
	c.done = true
	c.answer <- choice
	c.a.h.forget(c.ptrs)
	c.win.Destroy()
	c.a.releasePicker()
}
