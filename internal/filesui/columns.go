package filesui

import (
	"math"
	"os/user"
	"path/filepath"
	"strconv"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// column is one of the list's columns after the name. Each has a fixed
// width, the head's and the rows' alike, so they line up whatever the
// text: its own, or the one its head's edge was dragged to (colWidth).
type column struct {
	by    files.SortBy
	title string // in small capitals
	width int32  // its own
	right bool   // numbers end at the right
}

var columns = [...]column{
	{files.BySize, i18n.M("SIZE"), 86, true},
	{files.ByModified, i18n.M("MODIFIED"), 128, false},
	{files.ByCreated, i18n.M("CREATED"), 128, false},
	{files.ByOpened, i18n.M("OPENED"), 128, false},
	{files.ByKind, i18n.M("KIND"), 150, false},
	{files.ByPerms, i18n.M("PERMISSIONS"), 92, false},
	{files.ByOwner, i18n.M("OWNER"), 84, false},
	{files.ByGroup, i18n.M("GROUP"), 84, false},
}

// nameMin is the least the name keeps in the list; a narrower pane
// scrolls the list sideways instead.
const nameMin = files.MinNameWidth

// gap is the room between a row's icon, name and columns. Between the
// heads, a grip fills it.
const gap = 10

// trailing is the width at the end of the head that holds the columns
// button; rows leave it empty, so both line up.
const trailing = 24

// columnTitle is a column's head in what the pane shows: the trash's
// dates are when items were trashed and its kinds where they came from;
// a search's kinds are the folders found in.
func (p *pane) columnTitle(c column) string {
	switch {
	case p.mode == inTrash && c.by == files.ByModified:
		return i18n.T("TRASHED")
	case p.mode == inTrash && c.by == files.ByKind:
		return i18n.T("FROM")
	case p.mode == inSearch && c.by == files.ByKind:
		return i18n.T("FOLDER")
	}
	return i18n.T(c.title)
}

// columnText is what the row of the entry at i shows in column c.
func (p *pane) columnText(c column, i int, path string) string {
	e := &p.all[i]
	switch c.by {
	case files.BySize:
		if e.Dir {
			return p.w.app.counts.text(path, p)
		}
		return sizeText(e.Size)
	case files.ByModified:
		return dateText(e.ModTime)
	case files.ByCreated:
		return dateText(e.Created)
	case files.ByOpened:
		return dateText(e.Opened)
	case files.ByKind:
		switch p.mode {
		case inTrash:
			return shortHome(filepath.Dir(p.trash[i].Orig))
		case inSearch:
			return shortHome(p.dirs[i])
		}
		return p.w.app.kinds.of(e).desc
	case files.ByPerms:
		return e.Mode.String()
	case files.ByOwner:
		return p.w.app.userName(e.UID)
	case files.ByGroup:
		return p.w.app.groupName(e.GID)
	}
	return ""
}

// fixedLabel is a label of exactly width: its text is cut, never let
// widen it.
func fixedLabel(width int32, right bool) *gtk.Label {
	l := gtk.NewLabel("")
	l.SetSizeRequest(width, -1)
	l.SetMaxWidthChars(1)
	l.SetEllipsize(3)
	l.SetXalign(map[bool]float32{true: 1, false: 0}[right])
	return l
}

// buildColumns makes the column heads: the name's, one for each column,
// each with a grip on its right edge, and the button that chooses them.
func (p *pane) buildColumns() {
	w := p.w
	name := gtk.NewButton()
	name.AddCssClass("files-col")
	p.nameCol = name
	name.SetHexpand(true)
	p.nameHead = *gtk.NewLabel("")
	p.nameHead.AddCssClass("kicker")
	p.nameHead.SetXalign(0)
	name.SetChild(&p.nameHead.Widget)
	w.click(name, func() { p.sortBy(files.ByName) })
	gtkx.Append(&p.columns, &name.Widget)
	p.grips[0] = p.grip(files.ByName)
	for k, c := range columns {
		b := gtk.NewButton()
		b.AddCssClass("files-col")
		l := fixedLabel(w.app.colWidth(c), c.right)
		l.AddCssClass("kicker")
		b.SetChild(&l.Widget)
		p.colHeads[k], p.colLabels[k] = b, l
		by := c.by
		w.click(b, func() { p.sortBy(by) })
		gtkx.Append(&p.columns, &b.Widget)
		p.grips[k+1] = p.grip(by)
	}
	more := gtk.NewButtonFromIconName("view-more-symbolic")
	p.more = more
	more.AddCssClass("plain")
	more.AddCssClass("files-col-more")
	more.AddCssClass("tight")
	more.SetValign(gtk.AlignCenterValue)
	more.SetHalign(gtk.AlignEndValue) // at the end when the name is not stretched
	more.SetSizeRequest(trailing, -1)
	more.SetTooltipText(i18n.T("Columns"))
	w.click(more, func() { p.menu.showUnder(p.columnItems, &more.Widget) })
	gtkx.Append(&p.columns, &more.Widget)
	w.press(&p.columns.Widget, false, func(button uint32, _ int32, x, y float64) {
		if button == 3 {
			var px, py float64
			p.columns.TranslateCoordinates(&p.box.Widget, x, y, &px, &py)
			p.menu.show(p.columnItems, int32(px), int32(py))
		}
	})
}

// grip makes the grip at the right edge of column by's head, in the gap
// before the next head. Dragging it with the primary button makes the
// column wider or narrower, the rows with it, and keeps that width; a
// double click gives the column its own width again (the name: it takes
// what the others leave). The grip is not the head's button, so neither
// sorts.
func (p *pane) grip(by files.SortBy) *gtk.Box {
	g := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	g.AddCssClass("files-col-grip")
	g.SetSizeRequest(gap, -1)
	g.SetCursorFromName("col-resize")
	rule := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	rule.AddCssClass("files-col-rule")
	rule.SetHalign(gtk.AlignCenterValue)
	rule.SetHexpand(true)
	gtkx.Append(g, &rule.Widget)
	gtkx.Append(&p.columns, &g.Widget)

	// Where the pointer is, in the heads' scroller: the grip moves with
	// the edge it drags, the scroller does not.
	at := func(x, y float64) float64 {
		var hx, hy float64
		g.TranslateCoordinates(&p.columnsHead.Widget, x, y, &hx, &hy)
		return hx
	}
	var x0 float64 // the press
	var w0 int     // the width then
	moved := false // past the threshold: a drag, not a click
	p.w.drag(&g.Widget, &drag{
		begin: func(_ *gtk.GestureDrag, x, y float64) {
			x0, w0, moved = at(x, y), p.shownWidth(by), false
		},
		move: func(d *gtk.GestureDrag, dx, dy float64) {
			var sx, sy float64
			d.GetStartPoint(&sx, &sy)
			off := at(sx+dx, sy+dy) - x0
			if !moved {
				if math.Abs(off) < dragThreshold {
					return
				}
				moved = true
				d.SetState(gtk.EventSequenceClaimedValue)
				g.AddCssClass("dragging")
				if _, fixed := p.nameWidth(); !fixed && by != files.ByName {
					// After a stretched name the columns sit at the
					// pane's end, and an edge dragged would stay put
					// while its column grew to the left. The name keeps
					// the width it has, so the edge follows the pointer.
					p.w.app.settings.SetColumnWidth(files.ByName, p.shownWidth(files.ByName))
				}
			}
			p.w.app.setWidth(by, w0+int(math.Round(off)))
		},
		end: func(*gtk.GestureDrag, float64, float64) {
			if moved {
				g.RemoveCssClass("dragging")
				p.w.app.settings.Save() // once the drag is over, not at each step
			}
			moved = false
		},
	})
	p.w.press(&g.Widget, false, func(button uint32, n int32, _, _ float64) {
		if button == 1 && n == 2 {
			p.w.app.resetWidth(by)
		}
	})
	return g
}

// dragThreshold is how far, in pixels, a grip goes with the pointer
// before it resizes: a click, or a double click, that slips does not.
const dragThreshold = 3

// shownWidth is column by's width on screen: the name's without the
// icon before it.
func (p *pane) shownWidth(by files.SortBy) int {
	if by == files.ByName {
		if w, fixed := p.nameWidth(); fixed {
			return int(w)
		}
		return int(p.nameCol.GetWidth() - listIcon(p.w.app.settings.Zoom) - gap)
	}
	for _, c := range columns {
		if c.by == by {
			return int(p.w.app.colWidth(c))
		}
	}
	return 0
}

// nameWidth is the least the name has, and whether it has that alone:
// once its edge is dragged it stays at that width, the list's end taking
// what is left; till then it takes what the columns leave.
func (p *pane) nameWidth() (int32, bool) {
	if w := p.w.app.settings.ColumnWidth(files.ByName); w > 0 {
		return int32(w), true
	}
	return nameMin, false
}

// syncWidths sizes the heads and the list's rows on screen to the
// columns' widths.
func (p *pane) syncWidths() {
	name, fixed := p.nameWidth()
	p.nameCol.SetSizeRequest(listIcon(p.w.app.settings.Zoom)+gap+name, -1)
	p.nameCol.SetHexpand(!fixed)
	p.more.SetHexpand(fixed)
	for k, c := range columns {
		p.colLabels[k].SetSizeRequest(p.w.app.colWidth(c), -1)
	}
	for _, r := range p.rows {
		p.sizeRow(r)
	}
}

// sizeRow sizes a list row's name and columns as the heads are.
func (p *pane) sizeRow(r *row) {
	if r.end == nil { // a grid's cell, or a row of the column view
		return
	}
	name, fixed := p.nameWidth()
	r.name.SetSizeRequest(name, -1)
	r.name.SetHexpand(!fixed)
	// A fixed name's text must not widen it (fixedLabel).
	r.name.SetMaxWidthChars(map[bool]int32{true: 1, false: -1}[fixed])
	r.end.SetHexpand(fixed)
	for k, c := range columns {
		r.cols[k].SetSizeRequest(p.w.app.colWidth(c), -1)
	}
}

// colWidth is column c's width: the one its edge was dragged to, else
// its own.
func (a *App) colWidth(c column) int32 {
	if w := a.settings.ColumnWidth(c.by); w > 0 {
		return int32(w)
	}
	return c.width
}

// setWidth makes column by w wide, as far as it goes, in every pane.
func (a *App) setWidth(by files.SortBy, w int) {
	a.settings.SetColumnWidth(by, w)
	a.widthsChanged()
}

// resetWidth gives column by its own width again, in every pane.
func (a *App) resetWidth(by files.SortBy) {
	a.settings.ResetColumnWidth(by)
	a.widthsChanged()
	a.settings.Save()
}

func (a *App) widthsChanged() {
	for _, w := range a.wins {
		for _, p := range w.p {
			p.syncWidths()
		}
	}
}

// columnItems is the columns menu: each column, turned on and off.
func (p *pane) columnItems() []menuItem {
	var items []menuItem
	for _, c := range columns {
		by := c.by
		items = append(items, menuItem{label: p.columnTitle(c), check: true, on: p.w.app.settings.ShowsColumn(by), run: func() {
			p.w.app.settings.ToggleColumn(by)
			p.w.app.columnsChanged()
		}})
	}
	return items
}

// syncColumns shows the heads of the columns chosen, the order marked on
// the one sorted by, and the name's head over the names.
func (p *pane) syncColumns() {
	s := p.w.app.settings
	by := p.fv.Order.By
	if by == "" {
		by = files.ByName
	}
	arrow := func(on bool) string {
		switch {
		case !on || p.mode != inFolder:
			return ""
		case p.fv.Order.Reverse:
			return " ↓"
		}
		return " ↑"
	}
	p.nameHead.SetText(i18n.T("NAME") + arrow(by == files.ByName))
	p.nameHead.SetMarginStart(listIcon(s.Zoom) + gap) // past the icons
	for k, c := range columns {
		p.colHeads[k].SetVisible(s.ShowsColumn(c.by))
		p.grips[k+1].SetVisible(s.ShowsColumn(c.by))
		p.colLabels[k].SetText(p.columnTitle(c) + arrow(by == c.by))
	}
	p.syncWidths() // the name's head follows the icons' size
}

// userName and groupName are an owner's names, looked up once.
func (a *App) userName(uid uint32) string {
	if n, ok := a.users[uid]; ok {
		return n
	}
	n := strconv.Itoa(int(uid))
	if u, err := user.LookupId(n); err == nil {
		n = u.Username
	}
	a.users[uid] = n
	return n
}

func (a *App) groupName(gid uint32) string {
	if n, ok := a.groups[gid]; ok {
		return n
	}
	n := strconv.Itoa(int(gid))
	if g, err := user.LookupGroupId(n); err == nil {
		n = g.Name
	}
	a.groups[gid] = n
	return n
}

// columnsChanged shows the columns chosen in every pane.
func (a *App) columnsChanged() {
	for _, w := range a.wins {
		for _, p := range w.p {
			p.syncColumns()
			p.rebind()
		}
	}
}
