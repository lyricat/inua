package uikit

import (
	"sync"

	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/lyricat/inua/internal/gtkx"
)

// cascadeLine binds one row to its root menu and the submenu at its level.
type cascadeLine struct {
	root, parent *gtk.Popover
	item         Item
}

var cascades struct {
	sync.Mutex
	lines    map[uintptr]cascadeLine
	rows     map[uintptr][]uintptr
	children map[uintptr]*gtk.Popover
}

var onCascadeDestroy = func(w gtk.Widget) {
	cascades.Lock()
	for _, ptr := range cascades.rows[w.GoPointer()] {
		delete(cascades.lines, ptr)
	}
	delete(cascades.rows, w.GoPointer())
	delete(cascades.children, w.GoPointer())
	cascades.Unlock()
}

// A popover owns its submenu; remove it before the parent is unparented.
var onCascadeClosed = func(pop gtk.Popover) {
	cascades.Lock()
	child := cascades.children[pop.GoPointer()]
	delete(cascades.children, pop.GoPointer())
	cascades.Unlock()
	if child != nil {
		child.Popdown()
		child.Unparent()
	}
}

func openCascade(b *gtk.Button, line cascadeLine) {
	cascades.Lock()
	old := cascades.children[line.parent.GoPointer()]
	if old != nil && old.GetParent() != nil && old.GetParent().GoPointer() == b.GoPointer() {
		cascades.Unlock()
		return
	}
	delete(cascades.children, line.parent.GoPointer())
	cascades.Unlock()
	if old != nil {
		old.Popdown()
		old.Unparent()
	}
	if len(line.item.Items) == 0 {
		return
	}
	child := cascadeMenu(line.item.Items, line.root, false)
	child.SetPosition(gtk.PosRightValue)
	child.SetParent(&b.Widget)
	child.Unref()
	cascades.Lock()
	cascades.children[line.parent.GoPointer()] = child
	cascades.Unlock()
	child.Popup()
}

var onCascadeHover func(gtk.EventControllerMotion, float64, float64)
var onCascadeClick func(gtk.Button)

func init() {
	onCascadeHover = func(c gtk.EventControllerMotion, _, _ float64) {
		w := c.GetWidget()
		if w == nil {
			return
		}
		cascades.Lock()
		line, ok := cascades.lines[w.GoPointer()]
		cascades.Unlock()
		if ok {
			openCascade(gtk.ButtonNewFromInternalPtr(w.GoPointer()), line)
		}
	}

	onCascadeClick = func(b gtk.Button) {
		cascades.Lock()
		line, ok := cascades.lines[b.GoPointer()]
		cascades.Unlock()
		if !ok {
			return
		}
		if len(line.item.Items) > 0 {
			openCascade(&b, line)
			return
		}
		if line.item.Run != nil {
			line.root.Popdown()
			gtkx.Post(line.item.Run)
		}
	}

}

// NewCascadeMenu keeps parent menus open while submenus appear beside rows.
func NewCascadeMenu(list []Item) *gtk.Popover { return cascadeMenu(list, nil, true) }

func cascadeMenu(list []Item, root *gtk.Popover, _ bool) *gtk.Popover {
	pop := gtk.NewPopover()
	if root == nil {
		root = pop
	}
	pop.AddCssClass("glass-menu")
	pop.AddCssClass("gtk-frame")
	pop.SetHasArrow(false)
	// The bar must continue receiving pointer motion while a menu is open.
	pop.SetAutohide(false)
	box := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	box.AddCssClass("menu-items")
	marks := false
	for _, it := range list {
		marks = marks || it.Mark != ""
	}
	for _, it := range list {
		if it.Separator {
			sep := gtk.NewSeparator(gtk.OrientationHorizontalValue)
			gtkx.Append(box, &sep.Widget)
			continue
		}
		b := gtk.NewButton()
		b.AddCssClass("menu-item")
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		if marks {
			mark := gtk.NewLabel(map[string]string{"check": "✓", "radio": "●"}[it.Mark])
			mark.AddCssClass("menu-mark")
			mark.SetSizeRequest(14, -1)
			gtkx.Append(row, &mark.Widget)
		}
		label := gtk.NewLabel(it.Label)
		label.SetXalign(0)
		label.SetHexpand(true)
		gtkx.Append(row, &label.Widget)
		if len(it.Items) > 0 {
			img := gtk.NewImageFromIconName("go-next-symbolic")
			gtkx.Append(row, &img.Widget)
		} else if it.Keys != "" {
			key := gtk.NewLabel(it.Keys)
			key.AddCssClass("menu-key")
			gtkx.Append(row, &key.Widget)
		}
		b.SetChild(&row.Widget)
		row.Unref()
		b.SetSensitive(it.Run != nil || len(it.Items) > 0)
		b.ConnectClicked(&onCascadeClick)
		motion := gtk.NewEventControllerMotion()
		motion.ConnectEnter(&onCascadeHover)
		b.AddController(&motion.EventController)
		cascades.Lock()
		if cascades.lines == nil {
			cascades.lines = map[uintptr]cascadeLine{}
			cascades.rows = map[uintptr][]uintptr{}
			cascades.children = map[uintptr]*gtk.Popover{}
		}
		cascades.lines[b.GoPointer()] = cascadeLine{root: root, parent: pop, item: it}
		cascades.rows[pop.GoPointer()] = append(cascades.rows[pop.GoPointer()], b.GoPointer())
		cascades.Unlock()
		gtkx.Append(box, &b.Widget)
	}
	pop.SetChild(&box.Widget)
	box.Unref()
	pop.ConnectClosed(&onCascadeClosed)
	pop.ConnectDestroy(&onCascadeDestroy)
	return pop
}
