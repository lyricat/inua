package lightui

import (
	"fmt"
	"strconv"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
)

// menuWindow puts a local application menu above an existing window child.
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

// The apps are drawn in the settings window's language (window class
// "settings"): rows of a title over a dim line, sections, hairline-framed
// controls. These build the pieces Go adds.

// text is a title over a dim line, as a settings row has.
func text(title, sub string) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	box.SetValign(gtk.AlignCenterValue)
	t := gtk.NewLabel(title)
	t.AddCssClass("title")
	t.SetXalign(0)
	t.SetEllipsize(3)
	gtkx.Append(box, &t.Widget)
	if sub != "" {
		s := gtk.NewLabel(sub)
		s.AddCssClass("sub")
		s.SetXalign(0)
		s.SetEllipsize(3)
		gtkx.Append(box, &s.Widget)
	}
	return box
}

// indexItem is an entry in an app's index (the left column): a button
// named name holding child.
func indexItem(name string, child *gtk.Widget, selected bool, onClick *func(gtk.Button)) *gtk.Button {
	b := gtk.NewButton()
	b.AddCssClass("index-item")
	if selected {
		b.AddCssClass("selected")
	}
	b.SetName(name)
	b.SetChild(child)
	b.ConnectClicked(onClick)
	return b
}

// rowButton is a whole row that does something when clicked.
func rowButton(name string, child *gtk.Widget, onClick *func(gtk.Button)) *gtk.Button {
	b := gtk.NewButton()
	b.AddCssClass("row-button")
	b.SetName(name)
	b.SetChild(child)
	b.ConnectClicked(onClick)
	return b
}

// plainIcon is a frameless icon button.
func plainIcon(name, icon, tip string, onClick *func(gtk.Button)) *gtk.Button {
	b := gtk.NewButtonFromIconName(icon)
	b.AddCssClass("plain")
	b.SetName(name)
	b.SetTooltipText(tip)
	b.SetValign(gtk.AlignCenterValue)
	b.ConnectClicked(onClick)
	return b
}

// cell is a small framed value with its name above, for a grid of facts.
func cell(name, value string) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 4)
	box.AddCssClass("cell")
	k := gtk.NewLabel(name)
	k.AddCssClass("label")
	k.SetXalign(0)
	gtkx.Append(box, &k.Widget)
	v := gtk.NewLabel(value)
	v.AddCssClass("value")
	v.SetXalign(0)
	gtkx.Append(box, &v.Widget)
	return box
}

// clearGrid removes a grid's children.
func clearGrid(g *gtk.Grid) {
	for c := g.GetFirstChild(); c != nil; c = g.GetFirstChild() {
		g.Remove(c)
	}
}

// picker sets a time: two-digit units with a step up above and down below
// each, separated by colons.
type picker struct {
	max    []int  // each unit's largest value
	wrap   []bool // past the end, start over
	value  []int
	digits []*gtk.Label
	onStep func(gtk.Button)
	// changed runs after a step.
	changed func()
}

// newPicker builds a picker into box with units of the given maxima and,
// if given, a caption under each.
func newPicker(box *gtk.Box, max []int, wrap []bool, captions ...string) *picker {
	p := &picker{max: max, wrap: wrap, value: make([]int, len(max))}
	p.onStep = func(b gtk.Button) {
		unit, step, _ := strings.Cut(b.GetName(), ":")
		i, _ := strconv.Atoi(unit)
		d, _ := strconv.Atoi(step)
		p.step(i, d)
	}
	box.AddCssClass("picker")
	box.SetSpacing(4)
	for i := range max {
		if i > 0 {
			colon := gtk.NewLabel(":")
			colon.AddCssClass("picker-colon")
			gtkx.Append(box, &colon.Widget)
		}
		unit := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		unit.AddCssClass("picker-unit")
		up := gtk.NewButtonFromIconName("pan-up-symbolic")
		up.AddCssClass("plain")
		up.AddCssClass("tight")
		up.SetName(fmt.Sprintf("%d:1", i))
		up.ConnectClicked(&p.onStep)
		gtkx.Append(unit, &up.Widget)
		l := gtk.NewLabel("00")
		l.AddCssClass("picker-digits")
		p.digits = append(p.digits, l)
		gtkx.Append(unit, &l.Widget)
		down := gtk.NewButtonFromIconName("pan-down-symbolic")
		down.AddCssClass("plain")
		down.AddCssClass("tight")
		down.SetName(fmt.Sprintf("%d:-1", i))
		down.ConnectClicked(&p.onStep)
		gtkx.Append(unit, &down.Widget)
		if i < len(captions) {
			c := gtk.NewLabel(captions[i])
			c.AddCssClass("picker-caption")
			gtkx.Append(unit, &c.Widget)
		}
		gtkx.Append(box, &unit.Widget)
	}
	return p
}

func (p *picker) step(i, d int) {
	if i < 0 || i >= len(p.value) {
		return
	}
	v := p.value[i] + d
	switch {
	case v > p.max[i] && p.wrap[i]:
		v = 0
	case v < 0 && p.wrap[i]:
		v = p.max[i]
	}
	p.set(i, max(0, min(v, p.max[i])))
	if p.changed != nil {
		p.changed()
	}
}

func (p *picker) set(i, v int) {
	p.value[i] = v
	p.digits[i].SetText(fmt.Sprintf("%02d", v))
}

// noPaintable empties a picture: the bindings take a paintable value, and
// a zero texture passes NULL.
func noPaintable(p *gtk.Picture) { p.SetPaintable(&gdk.Texture{}) }

func unrefObject(o gobject.Ptr) { (&gobject.Object{Ptr: o.GoPointer()}).Unref() }
