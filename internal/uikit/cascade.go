package uikit

import (
	"sync"

	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/lyricat/inua/internal/gtkx"
)

type cascadeItemInfo struct {
	pop  *gtk.Popover
	root *gtk.Popover
	item Item
	btn  *gtk.Button
}

var cascades struct {
	sync.Mutex
	items    map[uintptr]*cascadeItemInfo
	children map[uintptr]*gtk.Popover // parent popover ptr -> active child popover
}

var onCascadeClick func(gtk.Button)
var onCascadeEnter func(gtk.EventControllerMotion, float64, float64)
var onCascadeClosed func(gtk.Popover)
var onCascadeDestroy func(gtk.Widget)

func init() {
	cascades.items = map[uintptr]*cascadeItemInfo{}
	cascades.children = map[uintptr]*gtk.Popover{}

	onCascadeClick = func(b gtk.Button) {
		cascades.Lock()
		info := cascades.items[b.GoPointer()]
		cascades.Unlock()
		if info == nil {
			return
		}

		// Leaf item
		if len(info.item.Items) == 0 {
			if info.item.Run != nil {
				run := info.item.Run
				info.root.Popdown()
				gtkx.Post(run)
			}
			return
		}

		// Submenu item
		cascades.Lock()
		activeChild := cascades.children[info.pop.GoPointer()]
		cascades.Unlock()

		if activeChild != nil && activeChild.GetParent() != nil && activeChild.GetParent().GoPointer() == b.GoPointer() {
			closeCascadeChild(info.pop)
			b.RemoveCssClass("active")
			return
		}

		openCascadeSubmenu(b, info)
	}

	onCascadeEnter = func(c gtk.EventControllerMotion, _, _ float64) {
		w := c.GetWidget()
		if w == nil {
			return
		}
		cascades.Lock()
		info := cascades.items[w.GoPointer()]
		cascades.Unlock()
		if info == nil {
			return
		}

		cascades.Lock()
		activeChild := cascades.children[info.pop.GoPointer()]
		cascades.Unlock()

		// Only track on hover if a child submenu is already open in this popover
		if activeChild != nil && activeChild.GetParent() != nil && activeChild.GetParent().GoPointer() != w.GoPointer() {
			if len(info.item.Items) > 0 {
				b := gtk.ButtonNewFromInternalPtr(w.GoPointer())
				openCascadeSubmenu(*b, info)
			}
		}
	}

	onCascadeClosed = func(pop gtk.Popover) {
		closeCascadeChild(&pop)
	}

	onCascadeDestroy = func(w gtk.Widget) {
		popPtr := w.GoPointer()
		cascades.Lock()
		child := cascades.children[popPtr]
		delete(cascades.children, popPtr)
		for btnPtr, info := range cascades.items {
			if info.pop.GoPointer() == popPtr {
				delete(cascades.items, btnPtr)
			}
		}
		cascades.Unlock()

		if child != nil {
			closeCascadeChild(child)
		}
	}
}

func openCascadeSubmenu(b gtk.Button, info *cascadeItemInfo) {
	closeCascadeChild(info.pop)

	b.AddCssClass("active")

	child := buildCascadeMenu(info.item.Items, info.root, false)
	child.SetPosition(gtk.PosRightValue)
	child.SetParent(&b.Widget)

	cascades.Lock()
	cascades.children[info.pop.GoPointer()] = child
	cascades.Unlock()

	child.Popup()
}

func closeCascadeChild(pop *gtk.Popover) {
	cascades.Lock()
	child := cascades.children[pop.GoPointer()]
	delete(cascades.children, pop.GoPointer())
	cascades.Unlock()

	if child != nil {
		closeCascadeChild(child)

		if parentBtn := child.GetParent(); parentBtn != nil {
			btn := gtk.ButtonNewFromInternalPtr(parentBtn.GoPointer())
			btn.RemoveCssClass("active")
		}

		child.Popdown()
		gtkx.DetachPopoverLater(child)
	}
}

// NewSideMenu displays adjacent parent and child panels.
func NewSideMenu(list []Item, at MenuAt) *gtk.Popover {
	pop := buildCascadeMenu(list, nil, true)
	switch at {
	case Below:
		pop.SetPosition(gtk.PosBottomValue)
	case Above:
		pop.AddCssClass("above")
		pop.SetPosition(gtk.PosTopValue)
	case AtPointer:
		pop.AddCssClass("at-pointer")
	}
	return pop
}

// NewCascadeMenu creates cascading popovers.
func NewCascadeMenu(list []Item) *gtk.Popover {
	return NewSideMenu(list, Below)
}

func buildCascadeMenu(list []Item, root *gtk.Popover, isRoot bool) *gtk.Popover {
	pop := gtk.NewPopover()
	if isRoot {
		root = pop
		pop.SetHalign(gtk.AlignStartValue) // Keep root menu's left edge aligned with bar button
	}
	pop.AddCssClass("glass-menu")
	pop.AddCssClass("gtk-frame")
	pop.SetHasArrow(false)
	pop.SetAutohide(true)

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
		l := gtk.NewLabel(it.Label)
		l.SetXalign(0)
		l.SetHexpand(true)
		gtkx.Append(row, &l.Widget)
		if len(it.Items) > 0 {
			img := gtk.NewImageFromIconName("go-next-symbolic")
			gtkx.Append(row, &img.Widget)
		} else if it.Keys != "" {
			k := gtk.NewLabel(it.Keys)
			k.AddCssClass("menu-key")
			gtkx.Append(row, &k.Widget)
		}
		b.SetChild(&row.Widget)
		row.Unref()
		b.SetSensitive(it.Run != nil || len(it.Items) > 0)
		b.ConnectClicked(&onCascadeClick)

		motion := gtk.NewEventControllerMotion()
		motion.ConnectEnter(&onCascadeEnter)
		b.AddController(&motion.EventController)

		info := &cascadeItemInfo{
			pop:  pop,
			root: root,
			item: it,
			btn:  b,
		}

		cascades.Lock()
		cascades.items[b.GoPointer()] = info
		cascades.Unlock()

		gtkx.Append(box, &b.Widget)
	}

	pop.SetChild(&box.Widget)
	box.Unref()

	pop.ConnectClosed(&onCascadeClosed)
	pop.ConnectDestroy(&onCascadeDestroy)

	return pop
}
