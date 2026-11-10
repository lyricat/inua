package ui

import (
	"strconv"
	"strings"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gobject/types"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
)

// barItemFace is how the settings' layout editor shows one of
// config.BarItems, or the space: its name (i18n.M, said in the user's
// language when shown) and its icon.
type barItemFace struct {
	label, icon string
}

// barItemFaces are the bar's items as the editor shows them, by their
// names in bar.layout; the bar's own widgets are bar.item's.
var barItemFaces = map[string]barItemFace{
	"workspaces":     {i18n.M("Workspaces and Engawa"), "view-grid-symbolic"},
	"app_menu":       {i18n.M("App menu"), "open-menu-symbolic"},
	"app_title":      {i18n.M("App name"), "application-x-executable-symbolic"},
	"clock":          {i18n.M("Clock"), "alarm-symbolic"},
	"tray":           {i18n.M("Tray icons"), "view-more-symbolic"},
	"recording":      {i18n.M("Recording"), "media-record-symbolic"},
	"passwords":      {i18n.M("Passwords"), "dialog-password-symbolic"},
	"clipboard":      {i18n.M("Clipboard history"), "edit-paste-symbolic"},
	"notifications":  {i18n.M("Notifications"), "preferences-system-notifications-symbolic"},
	"dictation":      {i18n.M("Dictation"), "audio-input-microphone-symbolic"},
	"control_center": {i18n.M("Control Center"), "network-wireless-signal-excellent-symbolic"},
	config.BarSpace:  {i18n.M("Space"), ""},
}

// barLayoutSettings is the Bar & Dock page's [LAYOUT]: a strip that is a
// small bar, its items in their order, and the items still to add under
// it. Items are dragged into the strip, along it, and back to the
// palette to take them off; or a click adds one, and the tools under the
// strip move or remove the one selected. Every edit applies at once and
// is saved as every setting is (changeSettings).
type barLayoutSettings struct {
	strip, mark           gtk.Box
	palette               gtk.FlowBox
	selected              gtk.Label
	left, right, remove   gtk.Button
	reset                 gtk.Button
	chips                 []*gtk.ToggleButton // the strip's, in its order; owned by it
	shown                 []string            // the layout they show
	filled                bool                // the strip shows shown
	sel                   int                 // the selected item's index, -1 for none
	dragFrom              int                 // the strip's item being dragged, -1 for none
	onChip, onAdd, onTool func(gtk.Button)
	onDragBegin           func(gtk.DragSource, uintptr)
	onDragEnd             func(gtk.DragSource, uintptr, bool)
	onStripMotion         func(gtk.DropTarget, float64, float64) gdk.DragAction
	onStripLeave          func(gtk.DropTarget)
	onStripDrop           func(gtk.DropTarget, uintptr, float64, float64) bool
	onPaletteDrop         func(gtk.DropTarget, uintptr, float64, float64) bool
	onPaletteMotion       func(gtk.DropTarget, float64, float64) gdk.DragAction
	onPaletteLeave        func(gtk.DropTarget)
}

// getBarLayoutSettings takes the editor's widgets from settings.ui.
func (s *shellUI) getBarLayoutSettings(ui *gtkx.Builder) {
	bl := &s.settings.barLayout
	ui.Get("bar_layout", &bl.strip)
	ui.Get("bar_layout_mark", &bl.mark)
	ui.Get("bar_layout_palette", &bl.palette)
	ui.Get("bar_layout_selected", &bl.selected)
	ui.Get("bar_layout_left", &bl.left)
	ui.Get("bar_layout_right", &bl.right)
	ui.Get("bar_layout_remove", &bl.remove)
	ui.Get("bar_layout_reset", &bl.reset)
	bl.chips, bl.shown, bl.filled, bl.sel, bl.dragFrom = nil, nil, false, -1, -1
	s.bindBarLayoutHandlers()
	for _, b := range []*gtk.Button{&bl.left, &bl.right, &bl.remove, &bl.reset} {
		b.ConnectClicked(&bl.onTool)
	}
	strip := gtk.NewDropTarget(types.GType(gobject.TypeStringVal), gdk.ActionMoveValue)
	strip.ConnectMotion(&bl.onStripMotion)
	strip.ConnectLeave(&bl.onStripLeave)
	strip.ConnectDrop(&bl.onStripDrop)
	bl.strip.AddController(&strip.EventController)
	palette := gtk.NewDropTarget(types.GType(gobject.TypeStringVal), gdk.ActionMoveValue)
	palette.ConnectMotion(&bl.onPaletteMotion)
	palette.ConnectLeave(&bl.onPaletteLeave)
	palette.ConnectDrop(&bl.onPaletteDrop)
	bl.palette.AddController(&palette.EventController)
}

// barLayout is the layout the editor edits: the settings', or the default
// one while they have none.
func (s *shellUI) barLayout() []string { return s.cfg.Bar.BarLayout() }

// setBarLayout applies an edit, sel the item to select after it.
func (s *shellUI) setBarLayout(layout []string, sel int) {
	c := s.cfg
	c.Bar.Layout = layout
	s.settings.barLayout.sel = sel
	s.changeSettings(c)
}

func (s *shellUI) bindBarLayoutHandlers() {
	bl := &s.settings.barLayout
	if bl.onChip != nil {
		return // the handlers outlive the window
	}
	// A click on an item in the strip selects it, or a second one lets
	// it go.
	bl.onChip = func(b gtk.Button) {
		i, ok := chipIndex(b.GetName())
		if !ok {
			return
		}
		if bl.sel == i {
			bl.sel = -1
		} else {
			bl.sel = i
		}
		s.showBarLayoutTools()
	}
	// A click on one in the palette adds it after the one selected, or at
	// the end, and selects it.
	bl.onAdd = func(b gtk.Button) {
		id := strings.TrimPrefix(b.GetName(), "add:")
		layout := s.barLayout()
		at := len(layout)
		if bl.sel >= 0 && bl.sel < len(layout) {
			at = bl.sel + 1
		}
		if out, ok := shell.BarLayoutInsert(layout, id, at); ok {
			s.setBarLayout(out, at)
		}
	}
	bl.onTool = func(b gtk.Button) {
		layout := s.barLayout()
		switch b.GetName() {
		case "bar_layout_reset":
			c := s.cfg
			c.Bar.Layout = nil // unset: the default, the clock centred
			bl.sel = -1
			s.changeSettings(c)
		case "bar_layout_left":
			if out, ok := shell.BarLayoutMove(layout, bl.sel, bl.sel-1); ok {
				s.setBarLayout(out, bl.sel-1)
			}
		case "bar_layout_right":
			if out, ok := shell.BarLayoutMove(layout, bl.sel, bl.sel+2); ok {
				s.setBarLayout(out, bl.sel+1)
			}
		case "bar_layout_remove":
			if out, ok := shell.BarLayoutRemove(layout, bl.sel); ok {
				s.setBarLayout(out, -1)
			}
		}
	}
	// The dragged item: a still of it follows the pointer; one from the
	// strip leaves its place there open while it goes.
	bl.onDragBegin = func(ds gtk.DragSource, _ uintptr) {
		w := ds.GetWidget()
		if w == nil {
			return
		}
		live := gtk.NewWidgetPaintable(w)
		still := live.GetCurrentImage()
		live.Unref()
		ds.SetIcon(still, w.GetWidth()/2, w.GetHeight()/2)
		gobject.ObjectNewFromInternalPtr(still.GoPointer()).Unref() // ours (transfer full)
		bl.dragFrom = -1
		if i, ok := chipIndex(w.GetName()); ok && i < len(bl.chips) {
			bl.dragFrom = i
			bl.chips[i].AddCssClass("lifted")
		}
	}
	// Dropped or not, the item is in its place again, or in its new one.
	bl.onDragEnd = func(gtk.DragSource, uintptr, bool) {
		for _, c := range bl.chips {
			c.RemoveCssClass("lifted")
		}
		bl.dragFrom = -1
		bl.mark.SetVisible(false)
		bl.palette.RemoveCssClass("drop")
	}
	bl.onStripMotion = func(_ gtk.DropTarget, x, _ float64) gdk.DragAction {
		s.markBarLayoutGap(s.barLayoutGap(x))
		return gdk.ActionMoveValue
	}
	bl.onStripLeave = func(gtk.DropTarget) { bl.mark.SetVisible(false) }
	bl.onStripDrop = func(dt gtk.DropTarget, _ uintptr, x, _ float64) bool {
		bl.mark.SetVisible(false)
		v := dt.GetValue()
		if v == nil {
			return false
		}
		drag := v.GetString()
		gap := s.barLayoutGap(x)
		layout := s.barLayout()
		var out []string
		ok, sel := false, gap
		if id, isAdd := strings.CutPrefix(drag, "add:"); isAdd {
			out, ok = shell.BarLayoutInsert(layout, id, gap)
		} else if i, id, isBar := barDrag(drag); isBar && i < len(layout) && layout[i] == id {
			out, ok = shell.BarLayoutMove(layout, i, gap)
			if gap > i {
				sel = gap - 1
			}
		}
		if ok {
			// After the drop has been answered: the strip, and the dragged
			// item in it, are made anew.
			gtkx.Post(func() { s.setBarLayout(out, sel) })
		}
		return ok
	}
	// Into the palette, an item of the bar's comes off it.
	bl.onPaletteMotion = func(gtk.DropTarget, float64, float64) gdk.DragAction {
		if bl.dragFrom < 0 {
			return 0 // the palette's own
		}
		bl.palette.AddCssClass("drop")
		return gdk.ActionMoveValue
	}
	bl.onPaletteLeave = func(gtk.DropTarget) { bl.palette.RemoveCssClass("drop") }
	bl.onPaletteDrop = func(dt gtk.DropTarget, _ uintptr, _, _ float64) bool {
		bl.palette.RemoveCssClass("drop")
		v := dt.GetValue()
		if v == nil {
			return false
		}
		layout := s.barLayout()
		i, id, isBar := barDrag(v.GetString())
		if !isBar || i >= len(layout) || layout[i] != id {
			return false
		}
		out, ok := shell.BarLayoutRemove(layout, i)
		if ok {
			gtkx.Post(func() { s.setBarLayout(out, -1) })
		}
		return ok
	}
}

// chipIndex is the index in the strip a chip's name ("bar:3") gives.
func chipIndex(name string) (int, bool) {
	n, ok := strings.CutPrefix(name, "bar:")
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(n)
	return i, err == nil && i >= 0
}

// barDrag reads what an item dragged from the strip carries: its index
// and its name ("bar:3:clock"), so that a drop is only for the layout it
// was dragged from.
func barDrag(drag string) (int, string, bool) {
	rest, ok := strings.CutPrefix(drag, "bar:")
	if !ok {
		return 0, "", false
	}
	n, id, ok := strings.Cut(rest, ":")
	i, err := strconv.Atoi(n)
	return i, id, ok && err == nil && i >= 0
}

// barLayoutGap is the gap of the layout a drop at x along the strip goes
// in: before the item whose first half x is over, or after the last.
func (s *shellUI) barLayoutGap(x float64) int {
	bl := &s.settings.barLayout
	for i, c := range bl.chips {
		var r graphene.Rect
		if !c.ComputeBounds(&bl.strip.Widget, &r) {
			continue
		}
		f := *(*[4]float32)(unsafe.Pointer(&r)) // x, y, width, height
		if float32(x) < f[0]+f[2]/2 {
			return i
		}
	}
	return len(bl.chips)
}

// markBarLayoutGap shows where a drop goes: a line in the accent in the
// gap, over the strip, so that nothing moves under the pointer.
func (s *shellUI) markBarLayoutGap(gap int) {
	bl := &s.settings.barLayout
	x := float32(4) // the strip's padding: before everything
	edge := func(i int, end bool) (float32, bool) {
		var r graphene.Rect
		if !bl.chips[i].ComputeBounds(&bl.strip.Widget, &r) {
			return 0, false
		}
		f := *(*[4]float32)(unsafe.Pointer(&r))
		if end {
			return f[0] + f[2] + 1, true
		}
		return f[0] - 3, true
	}
	switch {
	case gap < len(bl.chips):
		if v, ok := edge(gap, false); ok {
			x = v
		}
	case len(bl.chips) > 0:
		if v, ok := edge(len(bl.chips)-1, true); ok {
			x = v
		}
	}
	bl.mark.SetMarginStart(int32(max(0, x)))
	bl.mark.SetVisible(true)
}

// showBarLayout fills the strip and the palette from the settings, when
// they changed since.
func (s *shellUI) showBarLayout() {
	bl := &s.settings.barLayout
	layout := s.barLayout()
	if bl.filled && sameLayout(layout, bl.shown) {
		s.showBarLayoutTools()
		return
	}
	bl.filled, bl.shown = true, layout
	if bl.sel >= len(layout) {
		bl.sel = -1
	}
	gtkx.Clear(&bl.strip)
	bl.chips = nil
	if len(layout) == 0 {
		l := gtk.NewLabel(i18n.T("Nothing on the bar: drag items here"))
		l.AddCssClass("sub")
		l.SetHexpand(true)
		gtkx.Append(&bl.strip, &l.Widget)
	}
	for i, id := range layout {
		face := barItemFaces[id]
		c := gtk.NewToggleButton()
		c.AddCssClass("tool")
		c.SetName("bar:" + strconv.Itoa(i))
		c.SetTooltipText(i18n.T(face.label))
		if id == config.BarSpace {
			c.AddCssClass("space")
			c.SetHexpand(true) // as the bar's spaces: a share of the room left
			l := gtk.NewLabel(i18n.T(face.label))
			c.SetChild(&l.Widget)
			l.Unref()
		} else {
			c.SetIconName(face.icon)
		}
		c.ConnectClicked(&bl.onChip)
		s.dragBarItem(&c.Widget, "bar:"+strconv.Itoa(i)+":"+id)
		bl.strip.Append(&c.Widget)
		c.Unref() // the strip holds it
		bl.chips = append(bl.chips, c)
	}
	bl.palette.RemoveAll()
	for _, id := range shell.BarPalette(layout) {
		face := barItemFaces[id]
		b := gtk.NewButton()
		b.AddCssClass("outlined")
		b.AddCssClass("small")
		b.SetName("add:" + id)
		b.SetTooltipText(i18n.T("Add to the bar"))
		box := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
		if face.icon != "" {
			img := gtk.NewImageFromIconName(face.icon)
			gtkx.Append(box, &img.Widget)
		}
		l := gtk.NewLabel(i18n.T(face.label))
		gtkx.Append(box, &l.Widget)
		b.SetChild(&box.Widget)
		box.Unref()
		b.ConnectClicked(&bl.onAdd)
		s.dragBarItem(&b.Widget, "add:"+id)
		bl.palette.Append(&b.Widget)
		b.Unref() // the palette holds it
	}
	s.showBarLayoutTools()
}

// dragBarItem lets w be dragged, carrying what.
func (s *shellUI) dragBarItem(w *gtk.Widget, what string) {
	bl := &s.settings.barLayout
	var v gobject.Value
	v.Init(gobject.TypeStringVal)
	v.SetString(what)
	content := gdk.NewContentProviderForValue(&v)
	v.Unset()
	drag := gtk.NewDragSource()
	drag.SetActions(gdk.ActionMoveValue)
	drag.SetContent(content)
	content.Unref()
	drag.ConnectDragBegin(&bl.onDragBegin)
	drag.ConnectDragEnd(&bl.onDragEnd)
	w.AddController(&drag.EventController)
}

// showBarLayoutTools marks the selected item, and says what the tools
// under the strip can do with it.
func (s *shellUI) showBarLayoutTools() {
	bl := &s.settings.barLayout
	for i, c := range bl.chips {
		c.SetActive(i == bl.sel)
	}
	n := len(bl.shown)
	on := bl.sel >= 0 && bl.sel < n
	bl.left.SetSensitive(on && bl.sel > 0)
	bl.right.SetSensitive(on && bl.sel < n-1)
	bl.remove.SetSensitive(on)
	bl.reset.SetSensitive(s.cfg.Bar.Layout != nil)
	if on {
		bl.selected.SetText(i18n.T(barItemFaces[bl.shown[bl.sel]].label))
	} else {
		bl.selected.SetText(i18n.T("Select an item to move it or take it off the bar"))
	}
}
