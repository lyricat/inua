package ui

import (
	"context"
	"fmt"
	"log"
	"maps"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gobject/types"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/workshop"
)

// The app dock: one per monitor along a configurable screen edge, the
// pinned apps and then the others with windows. Clicking opens an app or
// brings it forward (again: its next window); right-clicking offers a new
// window, pinning and quitting. It lives with its monitor's bar.

// dockHideMS is how long the pointer may be away before an autohiding dock
// goes; dockLaunchMS how long a started app's icon bounces at most.
const (
	dockHideMS   = 500
	dockEdge     = 3 // px of a hidden dock along the screen edge
	dockLaunchMS = 6000
)

type dock struct {
	win      gtk.ApplicationWindow
	root     gtk.Box
	reveal   gtk.Revealer
	island   gtk.Box
	items    gtk.Box
	edge     gtk.Box
	refs     gtkx.Scope
	shown    string                 // the apps and icons shown (dockSignature)
	buttons  map[string]*dockButton // by app, borrowed from items
	size     int
	position string
	gaps     bool // style.edge_gaps
	vertical bool // apps stack along a vertical screen edge
	hide     bool // autohide
	hover    bool
	hideGen  uintptr
	drawer   *gtk.Button // the drawer's button, borrowed from items; nil without one
	workshop *gtk.Button // the workshop drawer's, there while workshop apps are installed

	// Dragging an app: its button is hidden while it is away, and the
	// others move aside to leave a gap where it would land (see setGap).
	dragKey string
}

// dockButton is one app's button and the parts that change in place, so
// their CSS transitions play.
type dockButton struct {
	btn *gtk.Button
	img *gtk.Image
	dot *gtk.Box

	// The gap beside the button, as margins: where they are and where
	// they are going (animated by onDockGapTick).
	start, end, toStart, toEnd float64
}

// dockState is shared by the docks: the apps they show and the open menu.
type dockState struct {
	apps       shell.AppIndex
	items      []shell.DockItem
	launching  map[string]bool // apps started from the dock, bouncing until a window shows
	menuKey    string          // the app whose menu is open
	menu       *gtk.Popover    // borrowed from its button
	gapTicking bool            // onDockGapTick is running
	drawer     *drawerPanel    // the open drawer, nil when closed
	programs   map[int]string  // /proc/PID/comm of windows' processes, by PID

	// The workshop apps installed, read again when their folder changes.
	workshopStamp string
	workshopApps  []workshop.Manifest
}

// syncDock gives a bar's monitor a dock, or takes it away, following
// dock.enable; a change of size, position or autohide builds it anew.
func (s *shellUI) syncDock(b *bar) {
	c := s.cfg.Dock
	want := c.Enable && s.layer
	if b.dock != nil && (!want || b.dock.size != c.Size || b.dock.hide != c.Autohide || b.dock.position != c.Position || b.dock.gaps != s.cfg.Style.EdgeGaps) {
		dropDock(b)
	}
	if want && b.dock == nil {
		b.dock = s.newDock(b)
	}
	if b.dock != nil {
		s.renderDock(b)
	}
}

func dropDock(b *bar) {
	if b.dock == nil {
		return
	}
	b.dock.win.Destroy()
	b.dock.refs.Release()
	b.dock = nil
}

func (s *shellUI) newDock(b *bar) *dock {
	position := s.cfg.Dock.Position
	d := &dock{size: s.cfg.Dock.Size, position: position, vertical: position == "left" || position == "right", hide: s.cfg.Dock.Autohide, gaps: s.cfg.Style.EdgeGaps}
	ui := s.load("dock.ui", &d.refs)
	ui.Get("dock", &d.win)
	ui.Get("root", &d.root)
	ui.Get("reveal", &d.reveal)
	ui.Get("island", &d.island)
	ui.Get("items", &d.items)
	ui.Get("edge", &d.edge)
	ui.Close()
	if d.vertical {
		d.root.SetOrientation(gtk.OrientationHorizontalValue)
		d.items.SetOrientation(gtk.OrientationVerticalValue)
		d.island.SetValign(gtk.AlignCenterValue)
		d.island.SetMarginBottom(0)
		d.reveal.SetVexpand(true)
		if position == "left" {
			if d.gaps {
				d.island.SetMarginStart(6)
			}
			d.reveal.SetTransitionType(gtk.RevealerTransitionTypeSlideRightValue)
		} else {
			if d.gaps {
				d.island.SetMarginEnd(6)
			}
			d.island.AddCssClass("dock-right")
			d.reveal.SetTransitionType(gtk.RevealerTransitionTypeSlideLeftValue)
		}
		d.edge.Widget.SetSizeRequest(dockEdge, -1)
		d.island.AddCssClass("dock-side")
	} else {
		d.root.SetOrientation(gtk.OrientationVerticalValue)
		d.items.SetOrientation(gtk.OrientationHorizontalValue)
		d.reveal.SetHexpand(true)
		if position == "top" {
			d.island.SetMarginBottom(0)
			if d.gaps {
				d.island.SetMarginTop(6)
			}
			d.reveal.SetTransitionType(gtk.RevealerTransitionTypeSlideDownValue)
		} else {
			if !d.gaps {
				d.island.SetMarginBottom(0)
			}
			d.reveal.SetTransitionType(gtk.RevealerTransitionTypeSlideUpValue)
		}
		d.edge.Widget.SetSizeRequest(-1, dockEdge)
	}
	switch position {
	case "top", "left":
		d.root.ReorderChildAfter(&d.edge.Widget, &gtk.Widget{})
	// The window takes the whole dock as it starts to show (fitDock): the
	// dock keeps to its screen edge, so it slides out of it.
	case "right":
		d.root.SetHalign(gtk.AlignEndValue)
	default:
		d.root.SetValign(gtk.AlignEndValue)
	}
	drop := gtk.NewDropTarget(types.GType(gobject.TypeStringVal), gdk.ActionMoveValue)
	drop.ConnectMotion(&s.onDockDropMotion)
	drop.ConnectLeave(&s.onDockDropLeave)
	drop.ConnectDrop(&s.onDockDrop)
	d.items.AddController(&drop.EventController)
	wheel := gtk.NewEventControllerScroll(gtk.EventControllerScrollVerticalValue | gtk.EventControllerScrollDiscreteValue)
	wheel.ConnectScroll(&s.onDockScroll)
	d.items.AddController(&wheel.EventController)
	d.win.SetApplication(s.app)
	w := d.win.GoPointer()
	layershell.InitForWindow(w)
	layershell.SetNamespace(w, "inua-dock")
	layershell.SetLayer(w, layershell.LayerTop)
	layershell.SetMonitor(w, b.mon.GoPointer())
	// Only the dock's own edge: the surface is as long as the dock and
	// centred along that edge, so the screen beside it stays clickable.
	edge := layershell.EdgeBottom
	switch position {
	case "top":
		edge = layershell.EdgeTop
	case "left":
		edge = layershell.EdgeLeft
	case "right":
		edge = layershell.EdgeRight
	}
	layershell.SetAnchor(w, edge, true)
	if d.hide {
		layershell.SetExclusiveZone(w, 0)
		d.reveal.SetRevealChild(false)
		d.edge.SetVisible(true)
		motion := gtk.NewEventControllerMotion()
		motion.ConnectEnter(&s.onDockEnter)
		motion.ConnectLeave(&s.onDockLeave)
		d.win.AddController(&motion.EventController)
		gtkx.ConnectNotify(&d.reveal, "child-revealed", &s.onDockRevealed)
	} else {
		layershell.AutoExclusiveZone(w)
	}
	d.win.Present()
	return d
}

// dockApps rebuilds the app index when the desktop entries arrive.
func (s *shellUI) dockApps(entries []*desktop.Entry) {
	s.dockState.apps = shell.NewAppIndex(entries)
	s.renderDocks()
}

func (s *shellUI) renderDocks() {
	s.forgetPrograms()
	for _, b := range s.bars {
		if b.dock != nil {
			s.renderDock(b)
		}
	}
}

// dockSignature is which apps the dock shows and how; while it stays the
// same, the buttons are updated in place.
func dockSignature(items []shell.DockItem, size int, drawer bool) string {
	var sb strings.Builder
	fmt.Fprint(&sb, size, drawer)
	for _, it := range items {
		fmt.Fprintf(&sb, "|%s,%s,%v", it.Key, it.Icon, it.Pinned)
	}
	return sb.String()
}

// inputMethodWindow reports whether a window belongs to the input method
// (Fcitx's candidate popup, whatever class it gets), by the program that
// owns it. Names are read from /proc/PID/comm once per PID and forgotten
// when the PID has no window left.
func (s *shellUI) inputMethodWindow(t hypr.Toplevel) bool {
	if t.PID <= 0 { // unknown, as for some XWayland windows
		return false
	}
	if s.dockState.programs == nil {
		s.dockState.programs = map[int]string{}
	}
	name, ok := s.dockState.programs[t.PID]
	if !ok {
		b, _ := os.ReadFile("/proc/" + strconv.Itoa(t.PID) + "/comm")
		name = strings.TrimSpace(string(b))
		s.dockState.programs[t.PID] = name
	}
	return shell.IsInputMethodProgram(name)
}

// forgetPrograms drops the names of processes without windows.
func (s *shellUI) forgetPrograms() {
	live := map[int]bool{}
	for _, t := range s.snap.Toplevels {
		live[t.PID] = true
	}
	maps.DeleteFunc(s.dockState.programs, func(pid int, _ string) bool { return !live[pid] })
}

func (s *shellUI) renderDock(b *bar) {
	d := b.dock
	items := shell.Dock(s.cfg.Dock.Pinned, s.dockState.apps, s.snap, s.inputMethodWindow)
	s.dockState.items = items
	for _, it := range items {
		if len(it.Windows) > 0 {
			delete(s.dockState.launching, it.Key) // it is up
		}
	}
	drawer := s.cfg.Dock.Drawer
	withWorkshop := drawer && len(s.installedWorkshop()) > 0
	if sig := dockSignature(items, d.size, drawer) + fmt.Sprint(withWorkshop); sig != d.shown {
		d.shown = sig
		if s.dockState.menu != nil {
			s.dockState.menu.Popdown() // its button is about to go
		}
		gtkx.Clear(&d.items)
		d.buttons = map[string]*dockButton{}
		sepDone := false
		for i, it := range items {
			if !it.Pinned && !sepDone && i > 0 { // between the pinned and the rest
				sepDone = true
				orientation := gtk.OrientationVerticalValue
				if d.vertical {
					orientation = gtk.OrientationHorizontalValue
				}
				sep := gtk.NewSeparator(orientation)
				sep.AddCssClass("dock-sep")
				gtkx.Append(&d.items, &sep.Widget)
			}
			db := s.newDockButton(it, d.size)
			db.btn.SetVisible(it.Key != d.dragKey) // still being dragged
			d.buttons[it.Key] = db
			gtkx.Append(&d.items, &db.btn.Widget)
		}
		d.drawer, d.workshop = nil, nil
		if drawer {
			orientation := gtk.OrientationVerticalValue
			if d.vertical {
				orientation = gtk.OrientationHorizontalValue
			}
			sep := gtk.NewSeparator(orientation)
			sep.AddCssClass("dock-sep")
			gtkx.Append(&d.items, &sep.Widget)
			d.drawer = s.drawerButton(d.size, drawerApps)
			gtkx.Append(&d.items, &d.drawer.Widget)
			if withWorkshop {
				d.workshop = s.drawerButton(d.size, drawerWorkshop)
				gtkx.Append(&d.items, &d.workshop.Widget)
			}
		}
	}
	for _, it := range items {
		if db := d.buttons[it.Key]; db != nil {
			s.updateDockButton(db, it)
		}
	}
	fitDock(d)
}

// fitDock sizes an autohiding dock's surface. Hidden, the dock is only its
// edge, as long as the dock: a surface of no length is never shown, and
// the pointer could not reach it. A mapped layer surface follows its
// window's default size rather than its content (see bar.go), so the
// default size is the whole dock while it shows and the edge once hidden.
func fitDock(d *dock) {
	if !d.hide {
		return
	}
	along, across := gtk.OrientationHorizontalValue, gtk.OrientationVerticalValue
	if d.vertical {
		along, across = across, along
	}
	var least, length int32
	d.island.Measure(along, -1, &least, &length, nil, nil)
	depth := int32(dockEdge)
	if d.reveal.GetRevealChild() || d.reveal.GetChildRevealed() {
		var island int32
		d.island.Measure(across, -1, &least, &island, nil, nil)
		depth += island
	}
	if d.vertical {
		d.edge.SetSizeRequest(dockEdge, length)
		d.win.SetDefaultSize(depth, length)
	} else {
		d.edge.SetSizeRequest(length, dockEdge)
		d.win.SetDefaultSize(length, depth)
	}
}

// newDockButton is one app: its icon over a square marking it running.
func (s *shellUI) newDockButton(it shell.DockItem, size int) *dockButton {
	btn := gtk.NewButton()
	btn.AddCssClass("dock-item")
	btn.SetName("dock:" + it.Key)
	btn.SetTooltipText(it.Name)
	btn.SetFocusable(false)
	box := gtk.NewBox(gtk.OrientationVerticalValue, 3)
	img := gtk.NewImage()
	setIcon(img, it.Icon)
	img.SetPixelSize(int32(size))
	gtkx.Append(box, &img.Widget)
	dot := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	dot.AddCssClass("dock-dot")
	dot.SetHalign(gtk.AlignCenterValue)
	gtkx.Append(box, &dot.Widget)
	btn.SetChild(&box.Widget)
	box.Unref()
	btn.ConnectClicked(&s.onDockClicked)
	if it.Entry != nil {
		// Drag to reorder (or pin): the drag carries the app's key.
		var v gobject.Value
		v.Init(gobject.TypeStringVal)
		v.SetString(it.Key)
		content := gdk.NewContentProviderForValue(&v)
		v.Unset()
		drag := gtk.NewDragSource()
		drag.SetActions(gdk.ActionMoveValue)
		drag.SetContent(content)
		content.Unref()
		drag.ConnectDragBegin(&s.onDockDragBegin)
		drag.ConnectDragEnd(&s.onDockDragEnd)
		drag.ConnectDragCancel(&s.onDockDragCancel)
		btn.AddController(&drag.EventController)
	}
	menu := gtk.NewGestureClick()
	menu.SetButton(3)
	menu.ConnectPressed(&s.onDockMenu)
	btn.AddController(&menu.EventController)
	return &dockButton{btn: btn, img: img, dot: dot}
}

// updateDockButton shows whether the app runs, is in front, or is starting;
// the classes drive CSS transitions and animations.
func (s *shellUI) updateDockButton(db *dockButton, it shell.DockItem) {
	db.btn.SetTooltipText(it.Name)
	toggle(&db.dot.Widget, "running", len(it.Windows) > 0 && !it.Active)
	toggle(&db.dot.Widget, "active", it.Active)
	toggle(&db.img.Widget, "launching", s.dockState.launching[it.Key])
}

// scrollDock goes n notches' way (down the next, up the previous) through
// the running apps in the dock's order, bringing the app's latest window
// forward as a click on its icon does.
func (s *shellUI) scrollDock(n int) {
	var keys []string
	current := ""
	for _, it := range s.dockState.items {
		if len(it.Windows) == 0 {
			continue
		}
		keys = append(keys, it.Key)
		if it.Active {
			current = it.Key
		}
	}
	key, ok := s.dockWheel.Next(keys, current, n, time.Now())
	if !ok {
		return
	}
	if it, ok := s.dockItem(key); ok {
		s.focusWindowHere(it.Windows[0].Address)
	}
}

func (s *shellUI) dockItem(key string) (shell.DockItem, bool) {
	for _, it := range s.dockState.items {
		if it.Key == key {
			return it, true
		}
	}
	return shell.DockItem{}, false
}

func (s *shellUI) launchApp(e *desktop.Entry) {
	if strings.HasPrefix(e.ID, "inua:") {
		s.drawerShellApp(e.ID)
		return
	}
	if err := desktop.LaunchWith(e, s.cfg.TerminalCommand()); err != nil {
		log.Printf("dock: %s: %v", e.ID, err)
	}
}

// showDockMenu opens an app's menu on its button.
func (s *shellUI) showDockMenu(btn *gtk.Widget, it shell.DockItem) {
	group := gio.NewSimpleActionGroup()
	menu := gio.NewMenu()
	add := func(label, action string) {
		a := gio.NewSimpleAction(action, nil)
		a.ConnectActivate(&s.onDockAction)
		group.AddAction(a)
		a.Unref()
		menu.Append(label, "dock."+action)
	}
	if it.Entry != nil {
		if !strings.HasPrefix(it.Entry.ID, "inua:") { // the shell's tools have one window
			add(i18n.T("New Window"), "new")
		}
		if it.Pinned {
			add(i18n.T("Remove from Dock"), "unpin")
		} else {
			add(i18n.T("Keep in Dock"), "pin")
		}
	}
	if len(it.Windows) > 0 {
		label := i18n.T("Quit")
		if len(it.Windows) > 1 {
			label = i18n.Tf("Quit (%d windows)", len(it.Windows))
		}
		add(label, "quit")
	}
	if menu.GetNItems() == 0 {
		menu.Unref()
		group.Unref()
		return
	}
	btn.InsertActionGroup("dock", group)
	group.Unref()
	pop := gtk.NewPopoverMenuFromModel(&menu.MenuModel)
	menu.Unref()
	pop.SetHasArrow(false)
	pop.SetPosition(gtk.PosTopValue)
	pop.SetParent(btn)
	pop.ConnectClosed(&s.onDockMenuClosed)
	pop.Unref() // the button holds it
	s.dockState.menuKey = it.Key
	s.dockState.menu = &pop.Popover
	pop.Popup()
}

func (s *shellUI) bindDockHandlers() {
	s.onDockClicked = func(b gtk.Button) {
		it, ok := s.dockItem(strings.TrimPrefix(b.GetName(), "dock:"))
		if !ok {
			return
		}
		launch, addr := shell.DockTarget(it)
		switch {
		case launch:
			s.launchApp(it.Entry)
			s.markLaunching(it.Key)
		case addr != "":
			s.focusWindowHere(addr)
		}
	}
	s.onDockScroll = func(_ gtk.EventControllerScroll, _, dy float64) bool {
		s.scrollDock(int(dy))
		return true
	}
	s.onDockMenu = func(g gtk.GestureClick, _ int32, _, _ float64) {
		w := g.GetWidget()
		if w == nil {
			return
		}
		if it, ok := s.dockItem(strings.TrimPrefix(w.GetName(), "dock:")); ok {
			s.showDockMenu(w, it)
		}
	}
	s.onDockAction = func(a gio.SimpleAction, _ uintptr) {
		it, ok := s.dockItem(s.dockState.menuKey)
		if !ok {
			return
		}
		switch a.GetName() {
		case "new":
			s.launchApp(it.Entry)
			if len(it.Windows) == 0 {
				s.markLaunching(it.Key)
			}
		case "pin", "unpin":
			c := s.cfg
			c.Dock.Pinned = shell.Pin(c.Dock.Pinned, it.Key, a.GetName() == "pin")
			s.changeSettings(c)
		case "quit":
			for _, w := range it.Windows {
				s.dispatch("closewindow address:" + w.Address)
			}
		}
	}
	s.onDockDragBegin = func(ds gtk.DragSource, _ uintptr) {
		w := ds.GetWidget()
		if w == nil {
			return
		}
		for _, b := range s.bars {
			if b.dock == nil {
				continue
			}
			for key, db := range b.dock.buttons {
				if db.btn.GoPointer() != w.GoPointer() {
					continue
				}
				// The icon follows the pointer: a still of it, as the
				// button goes from the dock.
				live := gtk.NewWidgetPaintable(&db.img.Widget)
				still := live.GetCurrentImage()
				live.Unref()
				size := int32(b.dock.size / 2)
				ds.SetIcon(still, size, size)
				gobject.ObjectNewFromInternalPtr(still.GoPointer()).Unref() // ours (transfer full)
				d := b.dock
				d.dragKey = key
				gtkx.Post(func() {
					// Its place stays open as the gap, so nothing moves.
					s.setGap(d, s.nextPinned(key), true)
					if db := d.buttons[key]; db != nil {
						db.btn.SetVisible(false)
					}
				})
				return
			}
		}
	}
	s.onDockDragEnd = func(gtk.DragSource, uintptr, bool) {
		// Dropped or not: the app is back in its place, or in its new one
		// (the dock is rebuilt in that order already).
		for _, b := range s.bars {
			if d := b.dock; d != nil && d.dragKey != "" {
				if db := d.buttons[d.dragKey]; db != nil {
					db.btn.SetVisible(true)
				}
				d.dragKey = ""
				s.setGap(d, "-", false)
			}
		}
	}
	s.onDockDragCancel = func(_ gtk.DragSource, _ uintptr, reason gdk.DragCancelReason) bool {
		// A pinned app dropped off the dock is taken off it.
		if reason != gdk.DragCancelNoTargetValue {
			return false
		}
		for _, b := range s.bars {
			if d := b.dock; d != nil && d.dragKey != "" && s.pinned(d.dragKey) {
				s.unpin(d.dragKey)
				return true // gone: no flying back
			}
		}
		return false
	}
	s.onDockDropMotion = func(dt gtk.DropTarget, x, y float64) gdk.DragAction {
		if d := s.dockOfItems(dt.GetWidget()); d != nil {
			axis := dockAxis(d, x, y)
			if s.unpinZone(d, axis) {
				s.setGap(d, "-", false)
			} else {
				s.setGap(d, s.dropBefore(d, axis), false)
			}
		}
		return gdk.ActionMoveValue
	}
	s.onDockDropLeave = func(dt gtk.DropTarget) {
		if d := s.dockOfItems(dt.GetWidget()); d != nil {
			s.setGap(d, "-", false)
		}
	}
	s.onDockGapTick = func(uintptr) bool {
		moving := false
		for _, b := range s.bars {
			if b.dock != nil && stepGap(b.dock) {
				moving = true
			}
		}
		s.dockState.gapTicking = moving
		return moving
	}
	s.onDockDrop = func(dt gtk.DropTarget, _ uintptr, x, y float64) bool {
		d := s.dockOfItems(dt.GetWidget())
		v := dt.GetValue()
		if d == nil || v == nil {
			return false
		}
		key := v.GetString()
		if _, ok := s.dockItem(key); !ok {
			return false
		}
		axis := dockAxis(d, x, y)
		if s.unpinZone(d, axis) {
			s.unpin(key)
			return true
		}
		before := s.dropBefore(d, axis)
		c := s.cfg
		c.Dock.Pinned = shell.MovePinned(c.Dock.Pinned, key, before)
		s.changeSettings(c)
		return true
	}
	s.onDockMenuClosed = func(p gtk.Popover) {
		s.dockState.menu = nil
		gtkx.Post(func() {
			p.Unparent()
			s.hideDocksSoon()
		})
	}
	s.onDockEnter = func(c gtk.EventControllerMotion, _, _ float64) {
		if d := s.dockOf(c.GetWidget()); d != nil {
			d.hover = true
			d.reveal.SetRevealChild(true)
			fitDock(d)
		}
	}
	s.onDockLeave = func(c gtk.EventControllerMotion) {
		if d := s.dockOf(c.GetWidget()); d != nil {
			d.hover = false
			s.hideDocksSoon()
		}
	}
	s.onDockLaunched = func(uintptr) bool {
		// Started apps that never showed a window stop bouncing.
		clear(s.dockState.launching)
		s.renderDocks()
		return false
	}
	s.onDockRevealed = func(obj, _, _ uintptr) {
		for _, b := range s.bars {
			if d := b.dock; d != nil && d.reveal.GoPointer() == obj {
				fitDock(d) // closed: back to the edge
			}
		}
	}
	s.onDockHide = func(gen uintptr) bool {
		for _, b := range s.bars {
			if d := b.dock; d != nil && d.hide && !d.hover && gen == d.hideGen && s.dockState.menu == nil && s.dockState.drawer == nil {
				d.reveal.SetRevealChild(false)
			}
		}
		return false
	}
}

// showDock brings an autohidden dock up; it hides again once the pointer
// has been and gone.
func (s *shellUI) showDock(d *dock) {
	d.reveal.SetRevealChild(true)
	fitDock(d)
}

func (s *shellUI) dockOf(w *gtk.Widget) *dock {
	for _, b := range s.bars {
		if b.dock != nil && w != nil && b.dock.win.GoPointer() == w.GoPointer() {
			return b.dock
		}
	}
	return nil
}

// hideDocksSoon hides autohiding docks the pointer has left, after a
// moment.
func (s *shellUI) hideDocksSoon() {
	for _, b := range s.bars {
		if d := b.dock; d != nil && d.hide && !d.hover {
			d.hideGen++
			glib.TimeoutAdd(dockHideMS, &s.onDockHide, d.hideGen)
		}
	}
}

// markLaunching bounces an app's icon until its first window shows, or for
// a few seconds.
func (s *shellUI) markLaunching(key string) {
	if s.dockState.launching == nil {
		s.dockState.launching = map[string]bool{}
	}
	s.dockState.launching[key] = true
	s.renderDocks()
	glib.TimeoutAdd(dockLaunchMS, &s.onDockLaunched, 0)
}

// focusWindowHere focuses a window without moving the pointer to it, as
// focusing does unless cursor:no_warps is set: the pointer stays on the
// dock, where the user clicked.
func (s *shellUI) focusWindowHere(addr string) {
	if s.client == nil {
		return
	}
	client, lua := s.client, s.hyprLua
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := focusQuietly(ctx, client, lua, addr); err != nil {
			log.Printf("dock: focus %s: %v", addr, err)
		}
	}()
}

// focusQuietly focuses the window addr with cursor:no_warps turned on for
// the focus only, and back as it was: in one Lua script where Hyprland
// runs Lua (0.55 and later), else in one batch of classic commands. It
// blocks; run it off the main thread.
func focusQuietly(ctx context.Context, client *hypr.Client, lua bool, addr string) error {
	if lua {
		script := fmt.Sprintf(`local noWarps = hl.get_config("cursor.no_warps"); hl.config({cursor={no_warps=true}}); local ok, err = pcall(function() hl.dispatch(hl.dsp.focus({window=%q})) end); hl.config({cursor={no_warps=noWarps}}); if not ok then error(err) end`, "address:"+addr)
		reply, err := client.Request(ctx, "eval "+script)
		if err == nil && strings.TrimSpace(string(reply)) != "ok" {
			err = fmt.Errorf("eval: %s", strings.TrimSpace(string(reply)))
		}
		return err
	}
	focus := "focuswindow address:" + addr
	reply, err := client.Request(ctx, "j/getoption cursor:no_warps")
	if err == nil && strings.Contains(string(reply), `"int": 0`) {
		// One batch, so the setting is back before anything else runs.
		_, err = client.Request(ctx, "[[BATCH]]keyword cursor:no_warps 1;dispatch "+focus+";keyword cursor:no_warps 0")
		return err
	}
	return client.Dispatch(ctx, focus)
}

// dockOfItems is the dock whose item box w is.
func (s *shellUI) dockOfItems(w *gtk.Widget) *dock {
	for _, b := range s.bars {
		if b.dock != nil && w != nil && b.dock.items.GoPointer() == w.GoPointer() {
			return b.dock
		}
	}
	return nil
}

// dockAxis picks the along-the-dock coordinate for drag operations.
func dockAxis(d *dock, x, y float64) float64 {
	if d.vertical {
		return y
	}
	return x
}

// dropBefore chooses the pinned app a drop along the dock goes before, ""
// for the end of the pinned apps.
func (s *shellUI) dropBefore(d *dock, axis float64) string {
	for _, it := range s.dockState.items {
		db := d.buttons[it.Key]
		if !it.Pinned || db == nil {
			continue
		}
		var r graphene.Rect
		if !db.btn.ComputeBounds(&d.items.Widget, &r) {
			continue
		}
		f := *(*[4]float32)(unsafe.Pointer(&r)) // x, y, width, height
		start, extent := f[0], f[2]
		if d.vertical {
			start, extent = f[1], f[3]
		}
		if float32(axis) < start+extent/2 {
			return it.Key
		}
	}
	return ""
}

// unpinZone reports whether a drop past the pinned apps takes the dragged app off
// its pinned apps.
func (s *shellUI) unpinZone(d *dock, axis float64) bool {
	if d.dragKey == "" || !s.pinned(d.dragKey) {
		return false
	}
	end := 0.0
	for _, it := range s.dockState.items {
		db := d.buttons[it.Key]
		if !it.Pinned || it.Key == d.dragKey || db == nil {
			continue
		}
		var r graphene.Rect
		if db.btn.ComputeBounds(&d.items.Widget, &r) {
			f := *(*[4]float32)(unsafe.Pointer(&r))
			if d.vertical {
				end = float64(f[1]+f[3]) + db.end
			} else {
				end = float64(f[0]+f[2]) + db.end
			}
		}
	}
	if end == 0 {
		end = float64(d.size) + 12
	}
	return axis > end+4
}

func (s *shellUI) pinned(key string) bool { return slices.Contains(s.cfg.Dock.Pinned, key) }

// unpin takes an app off the dock's pinned ones.
func (s *shellUI) unpin(key string) {
	c := s.cfg
	c.Dock.Pinned = shell.Pin(c.Dock.Pinned, key, false)
	s.changeSettings(c)
}

// setGap opens a gap in the dock where a drop would land, before an app,
// or after the last pinned one (""), or closes it ("-"). The apps slide
// aside, unless now is set.
func (s *shellUI) setGap(d *dock, before string, now bool) {
	last := ""
	for _, it := range s.dockState.items {
		if it.Pinned && it.Key != d.dragKey {
			last = it.Key
		}
	}
	slot := 0.0 // an app's extent with its padding, and the item box's spacing
	for key, db := range d.buttons {
		var r graphene.Rect
		if key != d.dragKey && db.btn.ComputeBounds(&d.items.Widget, &r) {
			f := *(*[4]float32)(unsafe.Pointer(&r))
			if d.vertical {
				slot = float64(f[3]) + 4
			} else {
				slot = float64(f[2]) + 4
			}
			break
		}
	}
	for key, db := range d.buttons {
		db.toStart, db.toEnd = 0, 0
		if before != "-" && before != "" && key == before {
			db.toStart = slot
		}
		if before == "" && key == last {
			db.toEnd = slot
		}
		if now {
			db.start, db.end = db.toStart, db.toEnd
			setDockGap(d, db, db.start, db.end)
		}
	}
	if !s.dockState.gapTicking {
		s.dockState.gapTicking = true
		glib.TimeoutAdd(16, &s.onDockGapTick, 0)
	}
}

func setDockGap(d *dock, db *dockButton, start, end float64) {
	if d.vertical {
		db.btn.SetMarginTop(int32(math.Round(start)))
		db.btn.SetMarginBottom(int32(math.Round(end)))
	} else {
		db.btn.SetMarginStart(int32(math.Round(start)))
		db.btn.SetMarginEnd(int32(math.Round(end)))
	}
}

// stepGap moves the dock's margins a step toward their targets, easing
// out; it reports whether any still moves.
func stepGap(d *dock) bool {
	moving := false
	step := func(cur, to float64) float64 {
		if math.Abs(to-cur) < 0.6 {
			return to
		}
		moving = true
		return cur + (to-cur)*0.3
	}
	for _, db := range d.buttons {
		start, end := step(db.start, db.toStart), step(db.end, db.toEnd)
		if start != db.start || end != db.end {
			db.start, db.end = start, end
			setDockGap(d, db, start, end)
		}
	}
	return moving
}

// nextPinned is the pinned app after key, "" when it is the last or not
// pinned: where its gap is when it is lifted out.
func (s *shellUI) nextPinned(key string) string {
	found := false
	for _, it := range s.dockState.items {
		if !it.Pinned {
			continue
		}
		if found {
			return it.Key
		}
		found = it.Key == key
	}
	return ""
}
