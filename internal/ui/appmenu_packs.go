package ui

import (
	"context"
	"log"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/uikit"
)

// menuPack is the declarative menu for a window, from the packs in
// docs/appmenus.md.
func (s *shellUI) menuPack(w compositor.Window) (appmenu.Pack, bool) {
	if s.appmenu.packs == nil {
		return appmenu.Pack{}, false
	}
	desktop := ""
	if e := s.dockState.apps.ForClass(w.AppID); e != nil {
		desktop = e.ID
	}
	return s.appmenu.packs.Match(w.AppID, desktop)
}

// entriesFromPack builds a pack's menu for the window pid owns.
func (s *shellUI) entriesFromPack(pid int, pack appmenu.Pack) []appmenu.Entry {
	w, ok := s.appWindowOf(pid)
	if !ok {
		return nil
	}
	return pack.Entries(w.ID, pid, s.genericAppSections(pid), s.windowMenuItems(w, pid))
}

// windowMenuItems is the include "window" snippet.
func (s *shellUI) windowMenuItems(w compositor.Window, pid int) []appmenu.Entry {
	action := func(label, kind string) appmenu.Entry {
		return appmenu.Entry{Label: label, Enabled: true, Action: appmenu.Action{Kind: kind, Bus: w.ID, Index: int32(pid)}}
	}
	return []appmenu.Entry{
		action(i18n.T("Fullscreen"), appmenu.ActionFullscreen),
		action(i18n.T("Float"), appmenu.ActionFloat),
		action(i18n.T("Center Window"), appmenu.ActionCenter),
	}
}

// appWindowOf is the window of process pid the bar names.
func (s *shellUI) appWindowOf(pid int) (compositor.Window, bool) {
	for _, w := range s.snap.Windows {
		if w.PID == pid {
			return w, true
		}
	}
	return compositor.Window{}, false
}

// appNameOf is what the bar calls an application: its desktop entry's
// name, else its class.
func (s *shellUI) appNameOf(w compositor.Window) string {
	if e := s.dockState.apps.ForClass(w.AppID); e != nil {
		return e.Name
	}
	return w.AppID
}

// genericAppSections is the menu an application without one of its own is
// given: the commands that work for every window. Packs use it through
// `include = ["generic"]`.
func (s *shellUI) genericAppSections(pid int) []appmenu.Entry {
	w, ok := s.appWindowOf(pid)
	if !ok {
		return nil
	}
	name := s.appNameOf(w)
	action := func(label, kind, window string) appmenu.Entry {
		return appmenu.Entry{Label: label, Enabled: true, Action: appmenu.Action{Kind: kind, Bus: window, Index: int32(pid)}}
	}
	section := func(label string, items ...appmenu.Entry) appmenu.Entry {
		return appmenu.Entry{Label: label, Enabled: true, Children: items}
	}
	return []appmenu.Entry{
		section(i18n.T("File"),
			action(i18n.T("Close Window"), appmenu.ActionCloseWindow, w.ID),
			action(i18n.Tf("Quit %s", name), appmenu.ActionQuitApp, w.ID)),
		section(i18n.T("Edit"),
			action(i18n.T("Copy Window Title"), appmenu.ActionCopyTitle, w.ID),
			action(i18n.T("Copy Window Class"), appmenu.ActionCopyClass, w.ID)),
		section(i18n.T("View"),
			action(i18n.T("Fullscreen"), appmenu.ActionFullscreen, w.ID),
			action(i18n.T("Float"), appmenu.ActionFloat, w.ID),
			action(i18n.T("Center Window"), appmenu.ActionCenter, w.ID)),
		section(i18n.T("Help"),
			action(i18n.Tf("About %s", name), appmenu.ActionAbout, w.ID)),
	}
}

// showAppAbout says what the application behind pid is: its name and icon,
// the window the bar names, and its process.
func (s *shellUI) showAppAbout(pid int) {
	w, ok := s.appWindowOf(pid)
	if !ok {
		return
	}
	name, icon := s.appNameOf(w), w.AppID
	if e := s.dockState.apps.ForClass(w.AppID); e != nil {
		icon = e.Icon
	}
	win := gtk.NewWindow()
	win.AddCssClass("about")
	win.SetDefaultSize(360, -1)
	win.SetResizable(false)
	win.SetTitle(i18n.Tf("About %s", name))
	body := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	body.AddCssClass("about-body")
	mark := gtk.NewImage()
	mark.AddCssClass("about-icon")
	setIcon(mark, icon)
	mark.SetPixelSize(64)
	title := gtk.NewLabel(name)
	title.AddCssClass("about-name")
	window := gtk.NewLabel(w.Title)
	window.AddCssClass("about-window")
	window.SetEllipsize(3)
	process := gtk.NewLabel(i18n.Tf("Process %d", pid))
	process.AddCssClass("about-process")
	okButton := gtk.NewButtonWithLabel(i18n.T("Close"))
	onClose := func(gtk.Button) { win.Destroy() }
	okButton.ConnectClicked(&onClose)
	for _, child := range []*gtk.Widget{&mark.Widget, &title.Widget, &window.Widget, &process.Widget, &okButton.Widget} {
		body.Append(child)
	}
	win.SetChild(&body.Widget)
	body.Unref()
	win.Present()
}

// appMenuKeyLabel formats a pack shortcut the same way uikit shows other
// keycaps. It is display-only; activation still sends the original key names.
func appMenuKeyLabel(action appmenu.Action) string {
	if action.Kind == appmenu.ActionKeySequence {
		steps := make([]string, 0, len(action.Sequence))
		for _, step := range action.Sequence {
			key := strings.ToLower(step.Key)
			if step.Mods != "" {
				mods := strings.ToLower(strings.ReplaceAll(step.Mods, " ", "-"))
				key = mods + "-" + key
			}
			steps = append(steps, key)
		}
		return strings.Join(steps, " ")
	}
	if action.Kind != appmenu.ActionKeys || action.Key == "" {
		return ""
	}
	mods := strings.Fields(action.Mods)
	for i, mod := range mods {
		switch strings.ToUpper(mod) {
		case "CTRL":
			mods[i] = "Ctrl"
		case "ALT":
			mods[i] = "Alt"
		case "SHIFT":
			mods[i] = "Shift"
		case "SUPER":
			mods[i] = "Super"
		}
	}
	key := action.Key
	switch strings.ToLower(key) {
	case "comma":
		key = ","
	case "equal":
		key = "="
	case "minus":
		key = "-"
	case "underscore":
		key = "_"
	case "slash":
		key = "/"
	case "grave":
		key = "`"
	case "space":
		key = "Space"
	default:
		if len(key) == 1 {
			key = strings.ToUpper(key)
		} else {
			key = strings.ToUpper(key[:1]) + key[1:]
		}
	}
	return strings.Join(append(mods, key), " ")
}

// packedMenuItems turns pack entries into a menu's lines.
func (s *shellUI) packedMenuItems(pid int, entries []appmenu.Entry) []uikit.Item {
	var out []uikit.Item
	for _, e := range entries {
		if e.Separator {
			if len(out) > 0 && !out[len(out)-1].Separator {
				out = append(out, uikit.Item{Separator: true})
			}
			continue
		}
		it := uikit.Item{Label: e.Label, Keys: appMenuKeyLabel(e.Action)}
		switch {
		case e.Submenu || len(e.Children) > 0:
			if e.Enabled {
				it.Items = s.packedMenuItems(pid, e.Children)
			}
		case e.Enabled:
			entry := e
			it.Run = func() { s.activatePackedEntry(pid, entry) }
		}
		out = append(out, it)
	}
	if len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	return out
}

// showPackedMenu opens the pack menu of the bar's entry i under w.
func (s *shellUI) showPackedMenu(b *bar, i int, w *gtk.Widget) {
	if i < 0 || i >= len(b.appMenuEntries) {
		return
	}
	entry := b.appMenuEntries[i]
	items := s.packedMenuItems(b.appMenuPID, entry.Children)
	if len(items) == 0 {
		return
	}
	s.closeAppMenu()
	pop := uikit.NewMenu(items, uikit.Below)
	pop.SetParent(w)
	pop.ConnectClosed(&s.onAppMenuClosed)
	pop.Unref()
	pop.Popup()
	s.appmenu.menu = pop
}

// openAppTitleMenu hangs the menu every window has (About, Close, Quit,
// Fullscreen, Float) under the application's icon and name.
func (s *shellUI) openAppTitleMenu(b *bar) {
	if b.appPopover.GoPointer() != 0 {
		if b.appPopover.GetVisible() {
			b.appPopover.Popdown()
			return
		}
		b.appPopover.Unparent()
		b.appPopover = gtk.Popover{}
	}
	w, ok := shell.BarWindow(s.snap, b.connector())
	if !ok {
		// No window on this bar: the system's own menu, as an empty
		// workspace has.
		s.showTitleMenu(b, s.systemMenuItems())
		return
	}
	sections := s.genericAppSections(w.PID)
	if len(sections) == 0 {
		return
	}
	s.showTitleMenu(b, s.titleMenuItems(w.PID, sections[0].Children))
}

// titleMenuItems turns entries into the title menu's lines.
func (s *shellUI) titleMenuItems(pid int, entries []appmenu.Entry) []uikit.Item {
	var out []uikit.Item
	for _, e := range entries {
		if e.Separator {
			if len(out) > 0 && !out[len(out)-1].Separator {
				out = append(out, uikit.Item{Separator: true})
			}
			continue
		}
		if len(e.Children) > 0 {
			continue
		}
		entry := e
		out = append(out, uikit.Item{Label: e.Label, Run: func() { s.doPackedEntry(pid, entry) }})
	}
	if len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	return out
}

// showTitleMenu hangs items under the application's icon and name.
func (s *shellUI) showTitleMenu(b *bar, items []uikit.Item) {
	if len(items) == 0 {
		return
	}
	pop := uikit.NewMenu(items, uikit.Below)
	pop.SetParent(&b.appButton.Widget)
	pop.Unref() // the button owns it
	pop.Popup()
	b.appPopover = *pop
}

// activatePackedEntry runs a pack item while its process still has the focus.
func (s *shellUI) activatePackedEntry(pid int, e appmenu.Entry) {
	if pid <= 0 || focusedPID(s.snap) != pid {
		return
	}
	s.doPackedEntry(pid, e)
}

// doPackedEntry runs a pack item, whatever has the focus: the title menu
// belongs to a bar, not to the focused window.
func (s *shellUI) doPackedEntry(pid int, e appmenu.Entry) {
	go func() {
		var err error
		switch e.Action.Kind {
		case appmenu.ActionAbout:
			gtkx.Post(func() { s.showAppAbout(pid) })
		case appmenu.ActionCloseWindow:
			s.closeWindow(e.Action.Bus)
		case appmenu.ActionQuitApp:
			for _, w := range s.snap.Windows {
				if w.PID == pid {
					s.closeWindow(w.ID)
				}
			}
		case appmenu.ActionFullscreen:
			if a, ok := s.comp.(compositor.Arranger); ok {
				s.compositorDo("fullscreen window", func(ctx context.Context) error { return a.ToggleFullscreen(ctx, e.Action.Bus) })
			}
		case appmenu.ActionFloat:
			if f, ok := s.comp.(compositor.Floater); ok {
				s.compositorDo("toggle floating", func(ctx context.Context) error { return f.ToggleFloating(ctx, e.Action.Bus) })
			}
		case appmenu.ActionCenter:
			if a, ok := s.comp.(compositor.Arranger); ok {
				s.compositorDo("center window", func(ctx context.Context) error { return a.CenterWindow(ctx, e.Action.Bus) })
			}
		case appmenu.ActionCopyTitle:
			if w, ok := s.appWindowOf(pid); ok {
				gtkx.Post(func() {
					if c := gdk.DisplayGetDefault().GetClipboard(); c != nil {
						c.SetText(w.Title)
					}
				})
			}
		case appmenu.ActionCopyClass:
			if w, ok := s.appWindowOf(pid); ok {
				gtkx.Post(func() {
					if c := gdk.DisplayGetDefault().GetClipboard(); c != nil {
						c.SetText(w.AppID)
					}
				})
			}
		case appmenu.ActionSystem:
			s.runSystemAction(e.Action.Bus)
		case appmenu.ActionKeys:
			k, ok := s.comp.(compositor.Keystroker)
			if !ok || !s.caps.Has(compositor.CapKeystrokes) {
				log.Printf("appmenu: cannot send %s %s to the window", e.Action.Mods, e.Action.Key)
				break
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err = k.SendKey(ctx, e.Action.Bus, e.Action.Mods, e.Action.Key)
			cancel()
		case appmenu.ActionKeySequence:
			k, ok := s.comp.(compositor.Keystroker)
			if !ok || !s.caps.Has(compositor.CapKeystrokes) {
				log.Printf("appmenu: cannot send key sequence to the window")
				break
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			for i, step := range e.Action.Sequence {
				if i > 0 {
					time.Sleep(60 * time.Millisecond)
				}
				if err = k.SendKey(ctx, e.Action.Bus, step.Mods, step.Key); err != nil {
					break
				}
			}
			cancel()
		}
		if err != nil {
			log.Printf("appmenu activate: %v", err)
		}
	}()
}
