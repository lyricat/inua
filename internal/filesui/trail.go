package filesui

import (
	"path/filepath"
	"slices"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/gtkx"
)

// The column view (as Finder's): a column for each folder of the path,
// from the first below the root (files.Trail). The last is the folder
// itself, the pane's own view with its selection, keys and menus; the
// ones before it show the folders above, the next folder of the path
// marked; after it, the folder selected in it. A click in a column before
// or after goes to that column's folder with the item clicked selected.

// trailColumn is a column other than the folder's own: a folder above it,
// or the one selected in it. Its entries are read each time it shows; it
// does not follow their changes.
type trailColumn struct {
	scroll gtk.ScrolledWindow
	list   gtk.ListView
	model  *gtk.StringList
	sel    *gtk.SingleSelection
	rows   map[uintptr]*row // by list item
	rowOf  map[uintptr]*row // by the row's own widget, for picking
	dir    string
	shown  []files.Entry
	names  []string
	mark   string // the name marked
	gen    uint64
}

// columnWidth is a column's width at 100%; the zoom scales it.
const columnWidth = 240

func (p *pane) columnWidth() int32 {
	return int32(columnWidth * zoomPercent[zoomStep(p.w.app.settings.Zoom)] / 100)
}

// buildColumnView makes the folder's own column; the others are made as a
// path first needs them.
func (p *pane) buildColumnView() {
	w := p.w
	f := w.factory(func(item uintptr) {
		r := p.columnRow(item)
		r.item = item
		p.rows[item] = r
		p.rowOf[r.box.GoPointer()] = r
	}, p.bindRow, p.teardownRow)
	p.col = gtk.NewListView(noModel, &f.ListItemFactory)
	p.col.AddCssClass("files-list")
	p.col.SetEnableRubberband(true)
	w.activate(p.col.GoPointer(), func(pos uint32) { p.activate(pos) })
	p.col.ConnectActivate(&w.app.h.onListAct)

	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scroll.SetChild(&p.col.Widget)
	p.colMain = *gtk.NewOverlay()
	p.colMain.AddCssClass("files-column")
	p.colMain.AddCssClass("current")
	p.colMain.SetChild(&scroll.Widget)
	scroll.Unref()
	// Why the folder shows nothing, over its column only.
	p.colEmpty = *gtk.NewLabel("")
	p.colEmpty.AddCssClass("files-column-empty")
	p.colEmpty.SetWrap(true)
	p.colEmpty.SetJustify(gtk.JustifyCenterValue)
	p.colEmpty.SetValign(gtk.AlignCenterValue)
	p.colEmpty.SetCanTarget(false)
	p.colMain.AddOverlay(&p.colEmpty.Widget)
	p.colEmpty.Unref()
	gtkx.Append(&p.columnBox, &p.colMain.Widget)

	p.preview = p.newTrailColumn()
	gtkx.Append(&p.columnBox, &p.preview.scroll.Widget)
}

// columnRow makes a row of a column: the icon, the name and, for a
// folder, an arrow.
func (p *pane) columnRow(item uintptr) *row {
	li := gtk.ListItemNewFromInternalPtr(item)
	r := &row{compact: true}
	r.group = gtk.NewLabel("")
	r.group.AddCssClass("files-group-heading")
	r.group.SetXalign(0)
	r.group.SetHexpand(true)
	r.group.SetMaxWidthChars(18)
	r.group.SetEllipsize(3)
	r.group.SetCanTarget(false)
	r.group.SetVisible(false)
	r.box = *gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	r.box.AddCssClass("files-row")
	r.icon = *gtk.NewImage()
	r.icon.AddCssClass("files-icon")
	gtkx.Append(&r.box, &r.icon.Widget)
	r.name = *gtk.NewLabel("")
	r.name.AddCssClass("files-name")
	r.name.SetXalign(0)
	r.name.SetHexpand(true)
	r.name.SetEllipsize(2) // middle: the extension stays in view
	gtkx.Append(&r.box, &r.name.Widget)
	r.arrow = *gtk.NewImageFromIconName("go-next-symbolic")
	r.arrow.AddCssClass("files-column-arrow")
	r.arrow.SetPixelSize(12)
	gtkx.Append(&r.box, &r.arrow.Widget)
	outer := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	gtkx.Append(outer, &r.group.Widget)
	gtkx.Append(outer, &r.box.Widget)
	li.SetChild(&outer.Widget)
	outer.Unref()
	return r
}

func (p *pane) newTrailColumn() *trailColumn {
	w := p.w
	c := &trailColumn{rows: map[uintptr]*row{}, rowOf: map[uintptr]*row{}}
	c.model = gtk.NewStringList(nil)
	c.sel = gtk.NewSingleSelection(c.model) // takes the list's reference
	c.sel.SetAutoselect(false)
	c.sel.SetCanUnselect(true)
	f := w.factory(func(item uintptr) {
		r := p.columnRow(item)
		c.rows[item] = r
		c.rowOf[r.box.GoPointer()] = r
	}, func(item uintptr) { p.bindTrailRow(c, item) }, func(item uintptr) {
		if r := c.rows[item]; r != nil {
			delete(c.rowOf, r.box.GoPointer())
		}
		delete(c.rows, item)
	})
	c.list = *gtk.NewListView(c.sel, &f.ListItemFactory)
	c.list.AddCssClass("files-list")
	c.list.SetFocusable(false)
	// The column's mark follows the path only: a click goes there, and the
	// path marks it again.
	w.press(&c.list.Widget, true, func(button uint32, n int32, x, y float64) {
		r, ok := pickRow(c.rowOf, &c.list.Widget, x, y)
		if !ok || int(r.pos) >= len(c.shown) {
			return
		}
		e := c.shown[r.pos]
		switch {
		case button == 1 && n == 1:
			w.setActive(p)
			dir := c.dir
			gtkx.Post(func() { p.goTo(dir, true, e.Name) })
		case button == 2 && e.Dir:
			p.newTab(filepath.Join(c.dir, e.Name), false)
		}
	})
	w.scroll(&c.list.Widget, func(dy float64) { w.zoom(map[bool]int{true: -1, false: 1}[dy > 0]) })

	c.scroll = *gtk.NewScrolledWindow()
	c.scroll.AddCssClass("files-column")
	c.scroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	c.scroll.SetSizeRequest(p.columnWidth(), -1)
	c.scroll.SetChild(&c.list.Widget)
	c.list.Unref()
	c.scroll.SetVisible(false)
	return c
}

func (p *pane) bindTrailRow(c *trailColumn, item uintptr) {
	r := c.rows[item]
	if r == nil {
		return
	}
	r.pos = gtk.ListItemNewFromInternalPtr(item).GetPosition()
	if int(r.pos) >= len(c.shown) {
		return
	}
	e := &c.shown[r.pos]
	r.path = filepath.Join(c.dir, e.Name)
	r.name.SetText(e.Name)
	r.group.SetVisible(false)
	p.kindIcon(p.w.app.kinds.of(e), e, r, listIcon(p.w.app.settings.Zoom))
	r.arrow.SetVisible(e.Dir)
	setClass(&r.box.Widget, "hidden", e.Hidden)
	setClass(&r.box.Widget, "link", e.Link != "")
	setClass(&r.box.Widget, "cut", p.w.app.isCut(r.path))
	r.box.SetTooltipText(tooltipFor(e, r.path))
}

// columnsShown says whether the pane shows a folder in columns; a search
// and the trail show only their own column.
func (p *pane) columnsShown() bool {
	return p.fv.View == files.ColumnView && p.mode == inFolder
}

// syncTrail shows the columns of the folders above this one, each read
// again, with the next folder of the path marked; and the folder selected
// here after it.
func (p *pane) syncTrail() {
	var above []string
	if p.columnsShown() {
		above = files.Trail(p.dir)
		above = above[:len(above)-1]
	}
	for len(p.trail) < len(above) {
		c := p.newTrailColumn()
		gtkx.Append(&p.trailBox, &c.scroll.Widget)
		p.trail = append(p.trail, c)
	}
	for k, c := range p.trail {
		if k >= len(above) {
			c.hide()
			continue
		}
		next := p.dir
		if k+1 < len(above) {
			next = above[k+1]
		}
		p.fillColumn(c, above[k], filepath.Base(next))
	}
	p.syncPreview()
	p.columnsToEnd()
}

// syncPreview shows the folder selected, when it is one alone, in the
// column after this one.
func (p *pane) syncPreview() {
	sel := p.selected()
	if !p.columnsShown() || len(sel) != 1 || !p.all[sel[0]].Dir || p.all[sel[0]].Broken {
		p.preview.hide()
		return
	}
	dir := p.path(sel[0])
	if p.preview.dir == dir && p.preview.scroll.GetVisible() {
		return
	}
	p.fillColumn(p.preview, dir, "")
	p.columnsToEnd()
}

// fillColumn shows dir in c, read on a goroutine, with mark marked. What
// c showed stays until then when it was dir already, so going along the
// path does not blank the columns that stay.
func (p *pane) fillColumn(c *trailColumn, dir, mark string) {
	c.gen++
	gen := c.gen
	if c.dir != dir {
		c.dir, c.shown, c.names = dir, nil, nil
		c.model.Splice(0, c.model.GetNItems(), nil)
	}
	c.scroll.SetVisible(true)
	c.setMark(mark)
	order := p.fv.Order
	go func() {
		es, _ := files.ReadDir(dir)
		files.Sort(es, order)
		gtkx.Post(func() {
			if gen != c.gen {
				return
			}
			c.show(es, order.ShowsHidden)
		})
	}()
}

// show puts es in the column: hidden files only when they show, but the
// one marked always, as the path goes through it.
func (c *trailColumn) show(es []files.Entry, hidden bool) {
	c.shown = c.shown[:0]
	names := make([]string, 0, len(es))
	for _, e := range es {
		if hidden || !e.Hidden || e.Name == c.mark {
			c.shown = append(c.shown, e)
			names = append(names, e.Name)
		}
	}
	if !slices.Equal(names, c.names) {
		c.model.Splice(0, c.model.GetNItems(), names)
		c.names = names
	}
	c.setMark(c.mark)
}

// rebind binds the column's rows again (the zoom, the cut files).
func (c *trailColumn) rebind() {
	if c.scroll.GetVisible() && len(c.names) > 0 {
		c.model.Splice(0, c.model.GetNItems(), c.names)
		c.setMark(c.mark)
	}
}

// setMark marks the entry named, and brings it in view.
func (c *trailColumn) setMark(name string) {
	c.mark = name
	at := slices.Index(c.names, name)
	if name == "" || at < 0 {
		c.sel.SetSelected(gtk.INVALID_LIST_POSITION)
		return
	}
	c.sel.SetSelected(uint32(at))
	c.list.ScrollTo(uint32(at), gtk.ListScrollNoneValue, nil)
}

func (c *trailColumn) hide() {
	if c.dir == "" && !c.scroll.GetVisible() {
		return
	}
	c.gen++
	c.dir, c.shown, c.names, c.mark = "", nil, nil, ""
	c.model.Splice(0, c.model.GetNItems(), nil)
	c.scroll.SetVisible(false)
}

// columnsToEnd scrolls the columns to the last, after it is laid out.
func (p *pane) columnsToEnd() {
	p.w.app.h.later(30, func() {
		if adj := p.columnsScroll.GetHadjustment(); adj != nil {
			adj.SetValue(adj.GetUpper())
		}
	})
}

// enterSelected goes into the folder selected, with its first item
// selected (→ in the columns).
func (p *pane) enterSelected() {
	sel := p.selected()
	if len(sel) != 1 || !p.all[sel[0]].Dir || p.mode != inFolder {
		return
	}
	p.goTo(p.path(sel[0]), true)
	p.selectFirst = true
}

// syncColumnWidths sizes the columns for the zoom.
func (p *pane) syncColumnWidths() {
	width := p.columnWidth()
	p.colMain.SetSizeRequest(width, -1)
	for _, c := range append(slices.Clone(p.trail), p.preview) {
		c.scroll.SetSizeRequest(width, -1)
	}
	if p.columnsShown() {
		p.columnsToEnd()
	}
}
