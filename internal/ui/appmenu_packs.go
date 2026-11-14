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

// packMenuEntries is the menu of window w when it published none: its
// pack's, or the generic one when there is no pack or the pack adds nothing
// (a pack that only carries flags). Every entry is bound to w's id.
func (s *shellUI) packMenuEntries(w compositor.Window, pack appmenu.Pack, matched bool) []appmenu.Entry {
	generic := genericAppSections(w, s.appNameOf(w))
	if !matched {
		return generic
	}
	if entries := pack.Entries(w.ID, w.PID, generic, windowMenuItems(w)); len(entries) > 0 {
		return entries
	}
	return generic
}

// windowMenuItems is the include "window" snippet.
func windowMenuItems(w compositor.Window) []appmenu.Entry {
	action := func(label, kind string) appmenu.Entry {
		return appmenu.Entry{Label: label, Enabled: true, Action: appmenu.Action{Kind: kind, Bus: w.ID, Index: int32(w.PID)}}
	}
	return []appmenu.Entry{
		action(i18n.T("Fullscreen"), appmenu.ActionFullscreen),
		action(i18n.T("Float"), appmenu.ActionFloat),
		action(i18n.T("Center Window"), appmenu.ActionCenter),
	}
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
// given: the commands that work for every window, bound to window w, which
// the bar calls name. Packs use it through `include = ["generic"]`.
func genericAppSections(w compositor.Window, name string) []appmenu.Entry {
	action := func(label, kind string) appmenu.Entry {
		return appmenu.Entry{Label: label, Enabled: true, Action: appmenu.Action{Kind: kind, Bus: w.ID, Index: int32(w.PID)}}
	}
	section := func(label string, items ...appmenu.Entry) appmenu.Entry {
		return appmenu.Entry{Label: label, Enabled: true, Children: items}
	}
	return []appmenu.Entry{
		section(i18n.T("File"),
			action(i18n.T("Close Window"), appmenu.ActionCloseWindow),
			action(i18n.Tf("Quit %s", name), appmenu.ActionQuitApp)),
		section(i18n.T("Edit"),
			action(i18n.T("Copy Window Title"), appmenu.ActionCopyTitle),
			action(i18n.T("Copy Window Class"), appmenu.ActionCopyClass)),
		section(i18n.T("View"),
			action(i18n.T("Fullscreen"), appmenu.ActionFullscreen),
			action(i18n.T("Float"), appmenu.ActionFloat),
			action(i18n.T("Center Window"), appmenu.ActionCenter)),
		section(i18n.T("Help"),
			action(i18n.Tf("About %s", name), appmenu.ActionAbout)),
	}
}

// showAppAbout says what the application behind window w is: its name and
// icon, the window's title, and its process. Every About window is its own;
// GTK owns it from here, and Close or the compositor's close destroys it.
func (s *shellUI) showAppAbout(w compositor.Window) {
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
	process := gtk.NewLabel(i18n.Tf("Process %d", w.PID))
	process.AddCssClass("about-process")
	okButton := gtk.NewButtonWithLabel(i18n.T("Close"))
	okButton.ConnectClicked(&s.onAppAboutClose) // shared: one trampoline for every opening
	for _, child := range []*gtk.Widget{&mark.Widget, &title.Widget, &window.Widget, &process.Widget, &okButton.Widget} {
		gtkx.Append(body, child)
	}
	win.SetChild(&body.Widget)
	body.Unref()
	win.Present()
	win.Unref() // GTK keeps its toplevel until it is destroyed
}

// closeAppAbout is the About window's Close: it destroys the window the
// button is in, whichever of the open ones that is.
func closeAppAbout(btn gtk.Button) {
	root := btn.GetRoot()
	if root == nil {
		return
	}
	win := gtk.Window{}
	win.Ptr = root.GoPointer()
	win.Destroy()
	win.Unref() // GetRoot's reference
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
	case "period":
		key = "."
	case "bracketleft":
		key = "["
	case "bracketright":
		key = "]"
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
func (s *shellUI) packedMenuItems(entries []appmenu.Entry) []uikit.Item {
	return s.windowEntryItems(entries, s.activatePackedEntry)
}

// titleMenuItems are the lines of the menu the application's name opens.
// That menu is the window's own, whatever menu the bar shows beside it (a
// published or an accessibility one), so its items act while the window
// they were built for has the focus, not only while the pack's menu shows.
func (s *shellUI) titleMenuItems(entries []appmenu.Entry) []uikit.Item {
	return s.windowEntryItems(entries, s.activateWindowEntry)
}

// windowEntryItems turns entries into a menu's lines, each item run by run.
func (s *shellUI) windowEntryItems(entries []appmenu.Entry, run func(appmenu.Entry)) []uikit.Item {
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
				it.Items = s.windowEntryItems(e.Children, run)
			}
		case e.Enabled:
			entry := e
			it.Run = func() { run(entry) }
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
	items := s.packedMenuItems(entry.Children)
	if len(items) == 0 {
		return
	}
	s.closeAppMenu()
	pop := uikit.NewCascadeMenu(items)
	pop.SetParent(w)
	pop.ConnectClosed(&s.onAppMenuClosed)
	pop.Unref()
	pop.Popup()
	s.appmenu.menu = pop
}

// packMenuShown is whether a bar shows the pack's menu for window id.
func (s *shellUI) packMenuShown(id string) bool {
	if id == "" {
		return false
	}
	for _, b := range s.bars {
		if b.appMenuWindow == id && !b.appMenuA11y && b.appMenuReg == (appmenu.Registration{}) {
			return true
		}
	}
	return false
}

// packTarget is what an activated pack entry acts on, read from the
// snapshot on the main thread so nothing that runs later reads it.
type packTarget struct {
	window  compositor.Window
	windows []string // the ids of every window of its process, for quit-app
}

// packTargetOf is the window a pack entry was bound to, when it is still
// the focused one: the menu was built for that window, and its commands go
// to no other, not even another window of the same process.
func packTargetOf(snap compositor.Snapshot, e appmenu.Entry) (packTarget, bool) {
	id := e.Action.Bus
	if id == "" || snap.FocusedWindow != id {
		return packTarget{}, false
	}
	w, ok := snap.Window(id)
	if !ok {
		return packTarget{}, false
	}
	t := packTarget{window: w}
	if e.Action.Kind == appmenu.ActionQuitApp {
		for _, other := range snap.Windows {
			if other.ID == w.ID || (w.PID > 0 && other.PID == w.PID) {
				t.windows = append(t.windows, other.ID)
			}
		}
	}
	return t, true
}

// activatePackedEntry runs a pack item while the window it was built for
// still has the focus and a bar still shows the pack's menu for it: one
// withdrawn while a published menu is looked for again runs nothing. What
// it needs of the snapshot is read here, on the main thread; only the key
// strokes are sent from a goroutine.
func (s *shellUI) activatePackedEntry(e appmenu.Entry) {
	if !s.packMenuShown(e.Action.Bus) {
		return
	}
	s.activateWindowEntry(e)
}

// activateWindowEntry runs an entry built for a window (a pack's, or the
// generic menu's) while that window still has the focus: one built for a
// window that lost it, or is gone, runs nothing.
func (s *shellUI) activateWindowEntry(e appmenu.Entry) {
	t, ok := packTargetOf(s.snap, e)
	if !ok {
		return
	}
	id := t.window.ID
	switch e.Action.Kind {
	case appmenu.ActionAbout:
		s.showAppAbout(t.window)
	case appmenu.ActionCloseWindow:
		s.closeWindow(id)
	case appmenu.ActionQuitApp:
		for _, w := range t.windows {
			s.closeWindow(w)
		}
	case appmenu.ActionFullscreen:
		if a, ok := s.comp.(compositor.Arranger); ok {
			s.compositorDo("fullscreen window", func(ctx context.Context) error { return a.ToggleFullscreen(ctx, id) })
		}
	case appmenu.ActionFloat:
		if f, ok := s.comp.(compositor.Floater); ok {
			s.compositorDo("toggle floating", func(ctx context.Context) error { return f.ToggleFloating(ctx, id) })
		}
	case appmenu.ActionCenter:
		if a, ok := s.comp.(compositor.Arranger); ok {
			s.compositorDo("center window", func(ctx context.Context) error { return a.CenterWindow(ctx, id) })
		}
	case appmenu.ActionCopyTitle:
		if c := gdk.DisplayGetDefault().GetClipboard(); c != nil {
			c.SetText(t.window.Title)
		}
	case appmenu.ActionCopyClass:
		if c := gdk.DisplayGetDefault().GetClipboard(); c != nil {
			c.SetText(t.window.AppID)
		}
	case appmenu.ActionKeys, appmenu.ActionKeySequence:
		k, ok := s.comp.(compositor.Keystroker)
		if !ok || !s.caps.Has(compositor.CapKeystrokes) {
			log.Printf("appmenu: cannot send %s to the window", appMenuKeyLabel(e.Action))
			return
		}
		action := e.Action
		go func() {
			if err := sendPackKeys(k, id, action, 60*time.Millisecond); err != nil {
				log.Printf("appmenu activate: %v", err)
			}
		}()
	}
}

// sendPackKeys sends a pack item's shortcut, or each stroke of its
// sequence gap apart, to the window id.
func sendPackKeys(k compositor.Keystroker, id string, a appmenu.Action, gap time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if a.Kind == appmenu.ActionKeys {
		return k.SendKey(ctx, id, a.Mods, a.Key)
	}
	for i, step := range a.Sequence {
		if i > 0 {
			time.Sleep(gap)
		}
		if err := k.SendKey(ctx, id, step.Mods, step.Key); err != nil {
			return err
		}
	}
	return nil
}
