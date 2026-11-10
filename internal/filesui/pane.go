package filesui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// mode is what a pane shows.
type mode int

const (
	inFolder mode = iota
	inSearch      // what a search below dir found
	inTrash       // the trash
)

// pane shows one folder. Its entries live in Go (all, in the folder's
// order); the views' model is a GtkStringList of the names of those
// shown, and a row binds to the entry at its position in shown. Only the
// rows on screen have widgets.
type pane struct {
	w *window
	n int // 0 or 1

	box, tabsBox, mark                              gtk.Box
	back, fwd, up, emptyTrash, zoomLevel            gtk.Button
	sidebarShow                                     gtk.Button
	pathStack                                       gtk.Stack
	crumbs                                          gtk.Box
	crumbScroll                                     gtk.ScrolledWindow
	location                                        gtk.Entry
	search                                          gtk.SearchEntry
	columns                                         gtk.Box
	columnsHead                                     gtk.ScrolledWindow
	nameHead                                        gtk.Label
	nameCol, more                                   *gtk.Button
	colHeads                                        [len(columns)]*gtk.Button
	colLabels                                       [len(columns)]*gtk.Label
	grips                                           [len(columns) + 1]*gtk.Box // the name's, then the columns'
	views                                           gtk.Stack
	listScroll, gridScroll, columnsScroll           gtk.ScrolledWindow
	columnBox, trailBox                             gtk.Box
	colMain                                         gtk.Overlay
	emptyLabel, filterLabel, statusLabel, freeLabel gtk.Label
	colEmpty                                        gtk.Label

	model      *gtk.StringList
	sel        *gtk.MultiSelection
	list       *gtk.ListView
	grid       *gtk.GridView
	col        *gtk.ListView    // the folder's own column of the column view
	trail      []*trailColumn   // the columns of the folders above it
	preview    *trailColumn     // the column of the folder selected in it
	termButton *gtk.Button      // the picker has none
	rows       map[uintptr]*row // by list item
	rowOf      map[uintptr]*row // by the row's own widget, for picking
	names      []string         // what the model holds
	menu       *popMenu
	crumbPtr   []uintptr
	tabs       []*tab
	tab        int // the one shown
	tabPtrs    []uintptr

	mode   mode
	dir    string
	all    []files.Entry
	shown  []int           // indexes into all, in the model's order
	dirs   []string        // search: each entry's folder
	scores []int           // search: how well each matched
	trash  []files.Trashed // trash: each entry's item
	filter string
	fv     files.FolderView
	hist   files.History
	gen    uint64
	fills  uint64             // refills, so a fill coming in parts stops at the next
	stop   context.CancelFunc // the folder's watch, or the search
	err    error              // why the folder could not be read
	free   string
	query  string // the search's

	boundInfo   map[string]entryInfo // the details last put in the model, by name
	flashing    bool                 // the status line says something for a moment
	selectFirst bool                 // once the folder is read, its first item, if nothing else
	reveal      bool                 // the next fill scrolls to what it selects (see loaded)
}

// row is the widgets of a list row, a grid cell or a column's row.
type row struct {
	box     gtk.Box
	icon    gtk.Image
	name    gtk.Label
	cols    [len(columns)]*gtk.Label // the list's columns, in columns' order
	end     *gtk.Box                 // a list row's, under the columns button
	arrow   gtk.Image                // a column's: shown for a folder
	grid    bool
	compact bool // a column's row: the icon, the name and the arrow
	pos     uint32
	path    string // bound to, for thumbnails arriving later
}

// sizeCol is the size's column in columns.
const sizeCol = 0

func newPane(w *window, n int) *pane {
	p := &pane{w: w, n: n, rows: map[uintptr]*row{}, rowOf: map[uintptr]*row{}}
	ui := w.app.c.Load("filespane.ui")
	ui.Take("pane", &p.box)
	ui.Get("tabs", &p.tabsBox)
	ui.Get("pane_mark", &p.mark)
	ui.Get("zoom_level", &p.zoomLevel)
	ui.Get("back", &p.back)
	ui.Get("forward", &p.fwd)
	ui.Get("up", &p.up)
	ui.Get("empty_trash", &p.emptyTrash)
	ui.Get("path_stack", &p.pathStack)
	ui.Get("crumbs", &p.crumbs)
	ui.Get("crumb_scroll", &p.crumbScroll)
	ui.Get("location", &p.location)
	ui.Get("search", &p.search)
	ui.Get("columns", &p.columns)
	ui.Get("columns_head", &p.columnsHead)
	ui.Get("sidebar_show", &p.sidebarShow)
	ui.Get("views", &p.views)
	ui.Get("list_scroll", &p.listScroll)
	ui.Get("grid_scroll", &p.gridScroll)
	ui.Get("columns_scroll", &p.columnsScroll)
	ui.Get("column_box", &p.columnBox)
	ui.Get("trail_box", &p.trailBox)
	ui.Get("empty_label", &p.emptyLabel)
	ui.Get("filter_label", &p.filterLabel)
	ui.Get("status_label", &p.statusLabel)
	ui.Get("free_label", &p.freeLabel)
	buttons := map[string]*gtk.Button{}
	for _, id := range []string{"edit_path", "copy_path", "terminal", "zoom_in", "zoom_out"} {
		b := &gtk.Button{}
		ui.Get(id, b)
		buttons[id] = b
	}
	ui.Close()

	w.click(&p.sidebarShow, func() { w.setSidebar(true) })
	w.click(&p.back, func() { p.goBack() })
	w.click(&p.fwd, func() { p.goForward() })
	w.click(&p.up, func() { p.goUp() })
	w.click(&p.emptyTrash, func() { w.confirmEmptyTrash() })
	w.click(buttons["edit_path"], func() { w.setActive(p); p.editLocation() })
	w.click(buttons["copy_path"], func() { w.setActive(p); w.copyFolderPath(p) })
	w.click(buttons["terminal"], func() { w.setActive(p); w.terminal(p.dir) })
	p.termButton = buttons["terminal"]
	w.click(buttons["zoom_in"], func() { w.zoom(1) })
	w.click(buttons["zoom_out"], func() { w.zoom(-1) })
	w.click(&p.zoomLevel, func() { w.zoom(0) })
	// The list, the grid or the columns, chosen by the view's text tabs.
	w.notify(&p.views, "visible-child-name", func() {
		if v := files.View(p.views.GetVisibleChildName()); v != p.shownView() && (v == files.ListView || v == files.GridView || v == files.ColumnView) {
			p.setView(v)
		}
	})
	// A click beside the path's buttons edits it; one on a button goes
	// there (the button's own click), so it is left alone here.
	w.press(&p.crumbScroll.Widget, false, func(button uint32, _ int32, x, y float64) {
		if button != 1 {
			return
		}
		for at := p.crumbScroll.Pick(x, y, gtk.PickDefaultValue); at != nil; at = at.GetParent() {
			if at.HasCssClass("files-crumb") {
				return
			}
			if at.GoPointer() == p.crumbScroll.GoPointer() {
				break
			}
		}
		w.setActive(p)
		p.editLocation()
	})
	w.enter(&p.location, func() { p.typedLocation() })
	w.edit(&p.search, func() { p.searchChanged() })

	// The selection takes the list's reference, and each view takes one of
	// the selection's: the pane keeps one of its own, as its views give
	// theirs up when the other view shows.
	p.model = gtk.NewStringList(nil)
	p.sel = gtk.NewMultiSelection(p.model)
	p.sel.Ref()
	w.app.h.onSelectionChanged(p.sel, func() { w.selectionChanged(p) })
	w.own(p.sel.GoPointer())

	lf := w.factory(p.setupRow, p.bindRow, p.teardownRow)
	p.list = gtk.NewListView(p.sel, &lf.ListItemFactory)
	p.list.AddCssClass("files-list")
	p.list.SetEnableRubberband(true)
	w.activate(p.list.GoPointer(), func(pos uint32) { p.activate(pos) })
	p.list.ConnectActivate(&w.app.h.onListAct)
	p.listScroll.SetChild(&p.list.Widget)
	// The heads follow the list sideways.
	hadj := p.listScroll.GetHadjustment()
	w.notify(hadj, "value", func() { p.columnsHead.GetHadjustment().SetValue(hadj.GetValue()) })

	gf := w.factory(p.setupCell, p.bindRow, p.teardownRow)
	p.grid = gtk.NewGridView(noModel, &gf.ListItemFactory)
	p.grid.AddCssClass("files-grid")
	p.grid.SetEnableRubberband(true)
	p.grid.SetMaxColumns(64)
	w.activate(p.grid.GoPointer(), func(pos uint32) { p.activate(pos) })
	p.grid.ConnectActivate(&w.app.h.onGridAct)
	p.gridScroll.SetChild(&p.grid.Widget)

	p.buildColumnView()

	for _, v := range []*gtk.Widget{&p.list.Widget, &p.grid.Widget, &p.col.Widget} {
		w.press(v, false, func(button uint32, _ int32, x, y float64) {
			switch button {
			case 3:
				p.contextMenu(v, x, y)
			case 2: // a folder in a new tab, behind this one
				if r, ok := p.pick(v, x, y); ok && int(r.pos) < len(p.shown) && p.all[p.shown[r.pos]].Dir && p.mode != inTrash {
					p.newTab(p.path(p.shown[r.pos]), false)
				}
			}
		})
		w.scroll(v, func(dy float64) { w.zoom(map[bool]int{true: -1, false: 1}[dy > 0]) })
		w.dragSource(v, func(x, y float64) uintptr { return p.dragContent(v, x, y) })
		w.dropTarget(v, func(paths []string, x, y float64, state gdk.ModifierType) {
			p.dropped(v, paths, x, y, state)
		})
	}
	// A click anywhere in the pane makes it the one keys go to.
	w.press(&p.box.Widget, true, func(uint32, int32, float64, float64) { w.setActive(p) })

	p.menu = newPopMenu(w.app.h, &p.box.Widget)
	p.buildColumns()
	p.tabs = []*tab{{}}
	p.syncZoom()
	return p
}

func (p *pane) destroy() {
	if p.stop != nil {
		p.stop()
	}
	p.gen++
	p.menu.destroy()
	p.w.app.h.forget(p.tabPtrs)
	p.sel.Unref()
}

// --- rows ---

func (p *pane) setupRow(item uintptr) {
	li := gtk.ListItemNewFromInternalPtr(item)
	r := &row{}
	r.box = *gtk.NewBox(gtk.OrientationHorizontalValue, gap)
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
	for k, c := range columns {
		l := fixedLabel(c.width, c.right)
		l.AddCssClass("readout")
		l.AddCssClass("files-col-" + string(c.by))
		r.cols[k] = l
		gtkx.Append(&r.box, &l.Widget)
	}
	r.end = gtk.NewBox(gtk.OrientationHorizontalValue, 0) // under the columns button
	r.end.SetSizeRequest(trailing, -1)
	gtkx.Append(&r.box, &r.end.Widget)
	p.sizeRow(r) // the widths chosen, a row made during a drag too
	li.SetChild(&r.box.Widget)
	r.box.Unref()
	p.rows[item] = r
	p.rowOf[r.box.GoPointer()] = r
}

func (p *pane) setupCell(item uintptr) {
	li := gtk.ListItemNewFromInternalPtr(item)
	r := &row{grid: true}
	r.box = *gtk.NewBox(gtk.OrientationVerticalValue, 6)
	r.box.AddCssClass("files-cell")
	r.icon = *gtk.NewImage()
	r.icon.AddCssClass("files-icon")
	gtkx.Append(&r.box, &r.icon.Widget)
	r.name = *gtk.NewLabel("")
	r.name.AddCssClass("files-name")
	r.name.SetWrap(true)
	r.name.SetWrapMode(2) // word or char
	r.name.SetLines(2)
	r.name.SetEllipsize(2)
	r.name.SetJustify(gtk.JustifyCenterValue)
	r.name.SetMaxWidthChars(16)
	gtkx.Append(&r.box, &r.name.Widget)
	li.SetChild(&r.box.Widget)
	r.box.Unref()
	p.rows[item] = r
	p.rowOf[r.box.GoPointer()] = r
}

func (p *pane) teardownRow(item uintptr) {
	if r := p.rows[item]; r != nil {
		delete(p.rowOf, r.box.GoPointer())
	}
	delete(p.rows, item)
}

func (p *pane) bindRow(item uintptr) {
	r := p.rows[item]
	if r == nil {
		return
	}
	li := gtk.ListItemNewFromInternalPtr(item)
	r.pos = li.GetPosition()
	if int(r.pos) >= len(p.shown) {
		return
	}
	i := p.shown[r.pos]
	e := &p.all[i]
	k := p.w.app.kinds.of(e)
	r.path = p.path(i)
	r.name.SetText(e.Name)
	zoom := p.w.app.settings.Zoom
	if r.compact {
		p.kindIcon(k, e, r, listIcon(zoom))
		r.arrow.SetVisible(e.Dir && p.mode != inTrash)
	} else if r.grid {
		if tex := p.w.app.thumbs.get(r.path, e, p); tex != nil {
			r.icon.SetPixelSize(gridIcon(zoom))
			r.icon.SetFromPaintable(tex)
		} else {
			p.kindIcon(k, e, r, gridIcon(zoom))
		}
	} else {
		p.kindIcon(k, e, r, listIcon(zoom))
		for c, col := range columns {
			on := p.w.app.settings.ShowsColumn(col.by)
			r.cols[c].SetVisible(on)
			if on {
				r.cols[c].SetText(p.columnText(col, i, r.path))
			}
		}
	}
	setClass(&r.box.Widget, "hidden", e.Hidden)
	setClass(&r.box.Widget, "link", e.Link != "")
	setClass(&r.box.Widget, "cut", p.w.app.isCut(r.path))
	r.box.SetTooltipText(tooltipFor(e, r.path))
}

// kindIcon shows an entry's icon in its row: a folder's by where it is
// (home, the user folders), else its kind's.
func (p *pane) kindIcon(k kind, e *files.Entry, r *row, size int32) {
	if e.Dir && !e.Broken {
		p.w.app.icons.set(&r.icon, p.w.app.folderIcon(r.path), size)
		return
	}
	p.w.app.icons.setKind(&r.icon, k, size)
}

func tooltipFor(e *files.Entry, path string) string {
	if e.Link != "" {
		return i18n.Tf("%s → %s", e.Name, e.Link)
	}
	if len(e.Name) > 40 {
		return e.Name
	}
	return ""
}

// rebind binds the rows on screen again, for what they show changed
// (the zoom, a thumbnail, a folder's count): a splice of every item,
// keeping the selection.
func (p *pane) rebind() {
	sel := p.selectedNames()
	p.names = nil
	p.refill(sel, false)
	for _, c := range p.trail {
		c.rebind()
	}
	p.preview.rebind()
}

// updateRows changes the rows on screen that show path, without
// touching the model: a thumbnail or a folder's count arriving must not
// move the keyboard's place in the list.
func (p *pane) updateRows(path string, f func(r *row)) {
	for _, r := range p.rows {
		if r.path == path {
			f(r)
		}
	}
}

// --- where it is ---

// path is where the entry at i is.
func (p *pane) path(i int) string {
	switch p.mode {
	case inSearch:
		return filepath.Join(p.dirs[i], p.all[i].Name)
	case inTrash:
		return p.trash[i].Path()
	}
	return filepath.Join(p.dir, p.all[i].Name)
}

// goTo shows dir; the folder left is put in the history when record.
// select names entries to select once it is read.
func (p *pane) goTo(dir string, record bool, sel ...string) {
	if strings.HasPrefix(dir, "~") {
		dir = files.Home() + dir[1:]
	}
	dir = filepath.Clean(dir)
	if record && p.dir != "" && (p.dir != dir || p.mode != inFolder) {
		p.hist.Visit(p.locationKey())
	}
	p.leaveSearch()
	// The columns go on wherever they go, in their order: the folders'
	// own views would change them at each step.
	if p.fv.View != files.ColumnView {
		p.fv = p.w.app.settings.FolderView(dir)
	}
	p.mode, p.dir, p.filter = inFolder, dir, ""
	p.selectFirst = false
	p.w.app.settings.Visited(dir)
	p.load(sel)
}

// locationKey is where the pane is, for the history: a folder, or
// "trash:" for the trash.
func (p *pane) locationKey() string {
	if p.mode == inTrash {
		return trashKey
	}
	return p.dir
}

const trashKey = "trash:"

func (p *pane) goKey(key string, record bool, sel ...string) {
	if key == trashKey {
		p.showTrash(record)
		return
	}
	p.goTo(key, record, sel...)
}

func (p *pane) goBack() {
	if k, ok := p.hist.Back(p.locationKey()); ok {
		p.goKey(k, false)
	}
}

func (p *pane) goForward() {
	if k, ok := p.hist.Forward(p.locationKey()); ok {
		p.goKey(k, false)
	}
}

func (p *pane) goUp() {
	if p.mode != inFolder {
		p.goTo(p.dir, true)
		return
	}
	parent := filepath.Dir(p.dir)
	if parent != p.dir {
		p.goTo(parent, true, filepath.Base(p.dir))
	}
}

// load reads the folder on a goroutine and follows its changes.
func (p *pane) load(sel []string) {
	p.gen++
	gen := p.gen
	if p.stop != nil {
		p.stop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.stop = cancel
	dir, order := p.dir, p.fv.Order
	p.showCrumbs()
	p.syncView()
	p.syncTrail()
	p.updateNav()
	go func() {
		es, err := files.ReadDir(dir)
		files.Sort(es, order)
		free := ""
		if sp, err := files.SpaceOf(dir); err == nil && sp.Total > 0 {
			free = i18n.Tf("%s free", sizeText(int64(sp.Free)))
		}
		gtkx.Post(func() {
			if gen != p.gen {
				return
			}
			p.free = free
			p.loaded(es, err, sel, true)
		})
	}()
	reload := func() {
		gtkx.Post(func() {
			if gen == p.gen {
				p.reload()
			}
		})
	}
	gone := func() {
		gtkx.Post(func() {
			if gen != p.gen {
				return
			}
			// The folder went: the nearest parent that is still there.
			d := dir
			for d != "/" {
				if st, err := os.Stat(d); err == nil && st.IsDir() {
					break
				}
				d = filepath.Dir(d)
			}
			p.goTo(d, false)
		})
	}
	if err := files.Watch(ctx, dir, reload, gone); err != nil {
		p.free = ""
	}
}

// reload reads the folder again, keeping the selection and the place.
func (p *pane) reload() {
	switch p.mode {
	case inTrash:
		p.showTrash(false)
	case inFolder:
		gen := p.gen
		dir, order := p.dir, p.fv.Order
		sel := p.selectedNames()
		go func() {
			es, err := files.ReadDir(dir)
			files.Sort(es, order)
			gtkx.Post(func() {
				if gen == p.gen {
					p.loaded(es, err, sel, false)
				}
			})
		}()
	}
}

// loaded shows the folder as read, selecting sel; reveal scrolls to it,
// for a selection that is new (the folder came from, files pasted), not
// for a reload: one runs each time the folder changes, many times while a
// file is copied in, and must leave the view where the user put it.
func (p *pane) loaded(es []files.Entry, err error, sel []string, reveal bool) {
	p.err = err
	p.all, p.dirs, p.trash = es, nil, nil
	p.w.app.counts.forget(p.dir)
	p.reveal = reveal
	p.refill(sel, true)
	if p.selectFirst {
		p.selectFirst = false
		if len(sel) == 0 && len(p.shown) > 0 {
			p.sel.SelectItem(0, true)
		}
	}
}

// refill computes what is shown (hidden files, the filter) and puts it in
// the model, changing only the part that changed so the view keeps its
// place; then selects sel by name, or the filter's best match. It scrolls
// to sel when the whole model is new, or when loaded asks it to.
func (p *pane) refill(sel []string, keepPlace bool) {
	reveal := !keepPlace || p.reveal
	p.reveal = false
	hidden := p.fv.Order.ShowsHidden || p.mode != inFolder
	var best int
	if p.filter != "" {
		idx, b := files.Filter(p.all, p.filter)
		p.shown, best = p.shown[:0], -1
		for k, i := range idx {
			if (hidden || !p.all[i].Hidden) && p.w.pickShows(&p.all[i]) {
				if k == b {
					best = len(p.shown)
				}
				p.shown = append(p.shown, i)
			}
		}
	} else {
		p.shown = p.shown[:0]
		for i := range p.all {
			if (hidden || !p.all[i].Hidden) && p.w.pickShows(&p.all[i]) {
				p.shown = append(p.shown, i)
			}
		}
	}
	names := make([]string, len(p.shown))
	for k, i := range p.shown {
		names[k] = p.all[i].Name
	}
	p.fills++
	view := p.view()
	n := p.model.GetNItems()
	parts := n == 0 && len(names) > 2*fillPart && view.GetMapped()
	switch {
	case parts:
		p.model.Splice(0, 0, names[:fillPart])
	case !keepPlace || n != uint32(len(p.names)):
		p.model.Splice(0, n, names)
	default:
		at, removed, added := diffNames(p.names, names, p.changedSince(names))
		p.model.Splice(uint32(at), uint32(removed), added)
	}
	p.names = names

	selectThem := func() {
		switch {
		case p.filter != "" && best >= 0:
			p.sel.SelectItem(uint32(best), true)
			p.scrollTo(uint32(best))
		case len(sel) > 0:
			p.selectNames(sel, reveal)
		}
	}
	if !parts {
		selectThem()
	}
	p.syncView()
	p.updateStatus()
	p.w.selectionChanged(p)
	if p == p.w.cur() && p.w.card == nil && p.pathStack.GetVisibleChildName() == "crumbs" && !p.w.namingFile() {
		p.focus()
	}
	if !parts {
		return
	}
	// An empty view makes a row for each item it may show, up to 200, at
	// over a millisecond each, all before the frame that shows them: the
	// first screenful is drawn, then the rest come a part a frame, and the
	// selection once all are in.
	fill, at, drawn := p.fills, fillPart, false
	p.w.app.h.eachFrame(view, func() bool {
		if fill != p.fills {
			return false // filled again since
		}
		if !drawn { // this frame draws the first part
			drawn = true
			return true
		}
		end := min(at+fillPart, len(names))
		p.model.Splice(uint32(at), 0, names[at:end])
		if at = end; at < len(names) {
			return true
		}
		selectThem()
		p.w.selectionChanged(p)
		return false
	})
}

// fillPart is how many items an empty view gets a frame: about a
// screenful of rows.
const fillPart = 48

// changedSince marks which of the new names are entries whose details
// changed, so their rows are bound again even when their names did not
// move: a file written to keeps its name but not its size.
func (p *pane) changedSince(names []string) func(i int) bool {
	old := p.boundInfo
	p.boundInfo = make(map[string]entryInfo, len(p.shown))
	for _, i := range p.shown {
		e := &p.all[i]
		p.boundInfo[e.Name] = entryInfo{e.Size, e.ModTime, e.Mode}
	}
	return func(k int) bool {
		was, ok := old[names[k]]
		return ok && was != p.boundInfo[names[k]]
	}
}

type entryInfo struct {
	size int64
	mod  time.Time
	mode os.FileMode
}

// diffNames finds the one stretch that differs between the model's
// names and the new ones: the common start and end stay, so the view
// keeps its scroll and the rows outside the stretch are not bound again.
func diffNames(old, cur []string, changed func(int) bool) (at, removed int, added []string) {
	start := 0
	for start < len(old) && start < len(cur) && old[start] == cur[start] && !changed(start) {
		start++
	}
	eo, ec := len(old), len(cur)
	for eo > start && ec > start && old[eo-1] == cur[ec-1] && !changed(ec-1) {
		eo--
		ec--
	}
	return start, eo - start, cur[start:ec]
}

// --- views ---

func (p *pane) setView(v files.View) {
	if p.fv.View == v {
		p.syncView() // the columns chosen in a search: its list stays
		return
	}
	p.fv.View = v
	p.saveView()
	p.syncView()
	p.syncTrail()
	p.focus()
}

// view is the widget of the view shown: the list, the grid, or the
// folder's own column.
func (p *pane) view() *gtk.Widget {
	switch p.shownView() {
	case files.GridView:
		return &p.grid.Widget
	case files.ColumnView:
		return &p.col.Widget
	}
	return &p.list.Widget
}

// shownView is the view shown: the pane's, but a search's results in the
// list, as they come from many folders.
func (p *pane) shownView() files.View {
	switch v := p.fv.View; {
	case v == files.GridView, v == files.ColumnView && p.mode == inFolder:
		return v
	}
	return files.ListView
}

func (p *pane) saveView() {
	if p.mode == inFolder {
		p.w.app.settings.SetFolderView(p.dir, p.fv)
	}
}

// syncView shows the list, the grid, the columns, or why there is
// nothing, and gives the model to the view shown only.
func (p *pane) syncView() {
	v := p.shownView()
	p.columnsHead.SetVisible(v == files.ListView)
	if v != files.ListView {
		p.list.SetModel(noModel)
	}
	if v != files.GridView {
		p.grid.SetModel(noModel)
	}
	if v != files.ColumnView {
		p.col.SetModel(noModel)
	}
	switch v {
	case files.GridView:
		p.grid.SetModel(p.sel)
	case files.ColumnView:
		p.col.SetModel(p.sel)
	default:
		p.list.SetModel(p.sel)
	}
	p.emptyTrash.SetVisible(p.mode == inTrash && len(p.all) > 0)
	empty := p.emptyText()
	// In the columns, why the folder shows nothing is said in its column.
	p.emptyLabel.SetVisible(len(p.shown) == 0 && v != files.ColumnView)
	p.emptyLabel.SetText(empty)
	p.colEmpty.SetVisible(len(p.shown) == 0 && v == files.ColumnView)
	p.colEmpty.SetText(empty)
	if !p.columnsShown() {
		for _, c := range p.trail {
			c.hide()
		}
		p.preview.hide()
	}
	p.views.SetVisibleChildName(string(v))
	p.syncColumns()
}

func (p *pane) emptyText() string {
	switch {
	case p.err != nil && os.IsPermission(p.err):
		return i18n.T("You may not read this folder.")
	case p.err != nil && os.IsNotExist(p.err):
		return i18n.T("This folder does not exist.")
	case p.err != nil:
		return i18n.Tf("This folder cannot be read: %v", p.err)
	case p.filter != "":
		return i18n.Tf("Nothing here matches “%s”.", p.filter)
	case p.mode == inSearch && p.query == "":
		return i18n.T("Type to search below this folder.")
	case p.mode == inSearch:
		return i18n.Tf("Nothing below this folder matches “%s”.", p.query)
	case p.mode == inTrash:
		return i18n.T("The trash is empty.")
	case len(p.all) > 0:
		return i18n.Nf("Only hidden files: %d of them (Ctrl+H shows them).", "Only hidden files: %d of them (Ctrl+H shows them).", len(p.all), len(p.all))
	}
	return i18n.T("This folder is empty.")
}

func (p *pane) sortBy(by files.SortBy) {
	if p.mode != inFolder {
		return
	}
	if p.fv.Order.By == by {
		p.fv.Order.Reverse = !p.fv.Order.Reverse
	} else {
		p.fv.Order.By, p.fv.Order.Reverse = by, by == files.ByModified || by == files.BySize
	}
	p.saveView()
	sel := p.selectedNames()
	files.Sort(p.all, p.fv.Order)
	p.names = nil
	p.refill(sel, false)
	if p.columnsShown() {
		p.syncTrail() // the columns before and after in the same order
	}
}

func (p *pane) toggleHidden() {
	p.fv.Order.ShowsHidden = !p.fv.Order.ShowsHidden
	p.saveView()
	p.refill(p.selectedNames(), true)
	if p.columnsShown() {
		p.syncTrail()
	}
}

func (p *pane) scrollTo(pos uint32) {
	switch p.shownView() {
	case files.GridView:
		p.grid.ScrollTo(pos, gtk.ListScrollFocusValue, nil)
	case files.ColumnView:
		p.col.ScrollTo(pos, gtk.ListScrollFocusValue, nil)
	default:
		p.list.ScrollTo(pos, gtk.ListScrollFocusValue, nil)
	}
}

// focus gives the keyboard to the pane's view.
func (p *pane) focus() {
	if len(p.shown) == 0 {
		p.box.GrabFocus()
		return
	}
	p.view().GrabFocus()
}

// --- selection ---

// selected are the indexes into all of what is selected, in order.
func (p *pane) selected() []int {
	bs := p.sel.GetSelection()
	defer bs.Unref()
	n := bs.GetSize()
	out := make([]int, 0, n)
	for k := range uint32(n) {
		if pos := bs.GetNth(k); int(pos) < len(p.shown) {
			out = append(out, p.shown[pos])
		}
	}
	return out
}

func (p *pane) selectedPaths() []string {
	var out []string
	for _, i := range p.selected() {
		out = append(out, p.path(i))
	}
	return out
}

func (p *pane) selectedNames() []string {
	var out []string
	for _, i := range p.selected() {
		out = append(out, p.all[i].Name)
	}
	return out
}

// selectNames selects the entries named; reveal brings the first in view.
func (p *pane) selectNames(names []string, reveal bool) {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	bs, mask := gtk.NewBitsetEmpty(), gtk.NewBitsetRange(0, uint32(len(p.shown)))
	first := -1
	for k, i := range p.shown {
		if want[p.all[i].Name] {
			bs.Add(uint32(k))
			if first < 0 {
				first = k
			}
		}
	}
	p.sel.SetSelection(bs, mask)
	bs.Unref()
	mask.Unref()
	if reveal && first >= 0 {
		// After a frame: rows the model just got are laid out then, and the
		// view scrolls only as far as it must, not at all when the item is
		// in sight. Asked at once, it cannot tell, and puts the item at the
		// top of the view (a paste moved the list under the user).
		pos, fill, laid := uint32(first), p.fills, false
		p.w.app.h.eachFrame(p.view(), func() bool {
			if fill != p.fills {
				return false // filled again since
			}
			if !laid {
				laid = true
				return true
			}
			p.scrollTo(pos)
			return false
		})
	}
}

// --- the filter typed into the folder ---

func (p *pane) setFilter(f string) {
	p.filter = f
	p.filterLabel.SetVisible(f != "")
	p.filterLabel.SetText("/" + f)
	p.refill(nil, true)
}

// --- opening ---

func (p *pane) activate(pos uint32) {
	if int(pos) >= len(p.shown) {
		return
	}
	i := p.shown[pos]
	e := &p.all[i]
	switch {
	case p.mode == inTrash:
		return
	case p.w.pick != nil:
		p.w.pickActivate(p, i)
	case e.Dir:
		p.goTo(p.path(i), true)
	default:
		p.w.launch([]string{p.path(i)}, false)
	}
}

// typedLocation goes where the path entry says: a folder, a file's
// folder with the file selected, or a remote disk to connect.
func (p *pane) typedLocation() {
	t := strings.TrimSpace(p.location.GetText())
	p.pathStack.SetVisibleChildName("crumbs")
	p.focus()
	if t == "" {
		return
	}
	if strings.HasPrefix(t, trashKey) {
		p.showTrash(true)
		return
	}
	if !files.IsLocalURI(t) {
		p.w.connectRemote(p, t)
		return
	}
	if local, ok := files.PathOfURI(t); ok {
		t = local
	} else if strings.HasPrefix(t, "~") {
		t = files.Home() + t[1:]
	}
	if !filepath.IsAbs(t) {
		t = filepath.Join(p.dir, t)
	}
	st, err := os.Stat(t)
	switch {
	case err == nil && st.IsDir():
		p.goTo(t, true)
	case err == nil:
		p.goTo(filepath.Dir(t), true, filepath.Base(t))
	default:
		p.w.flash(i18n.Tf("%s does not exist.", shortHome(t)))
	}
}

func (p *pane) editLocation() {
	p.location.SetText(shortHome(p.dir) + "/")
	if p.dir == "/" {
		p.location.SetText("/")
	}
	p.pathStack.SetVisibleChildName("entry")
	p.location.GrabFocus()
	p.location.SetPosition(-1)
}

// completeLocation completes the path typed to the folder names it
// starts, as far as they agree (Tab).
func (p *pane) completeLocation() {
	t := p.location.GetText()
	full := t
	if strings.HasPrefix(full, "~") {
		full = files.Home() + full[1:]
	}
	dir, prefix := filepath.Dir(full), filepath.Base(full)
	if strings.HasSuffix(full, "/") {
		dir, prefix = full, ""
	}
	es, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var match []string
	for _, e := range es {
		if (e.IsDir() || e.Type()&os.ModeSymlink != 0) && strings.HasPrefix(strings.ToLower(e.Name()), strings.ToLower(prefix)) &&
			(prefix != "" && strings.HasPrefix(prefix, ".") || !strings.HasPrefix(e.Name(), ".")) {
			match = append(match, e.Name())
		}
	}
	if len(match) == 0 {
		return
	}
	common := match[0]
	for _, m := range match[1:] {
		for !strings.HasPrefix(strings.ToLower(m), strings.ToLower(common)) {
			common = common[:len(common)-1]
		}
	}
	done := filepath.Join(dir, common)
	if len(match) == 1 {
		done += "/"
	}
	p.location.SetText(shortHome(done))
	p.location.SetPosition(-1)
}

// --- the path bar ---

func (p *pane) showCrumbs() {
	if p == p.w.cur() {
		// Where the pane with the keys is, wherever it went from.
		p.w.syncTitle()
		p.w.markSidebar()
	}
	p.buildTabs()
	p.w.app.h.forget(p.crumbPtr)
	p.crumbPtr = p.crumbPtr[:0]
	gtkx.Clear(&p.crumbs)
	if p.mode == inTrash {
		p.crumb(i18n.T("Trash"), "user-trash-symbolic", trashKey, true)
		return
	}
	segs := crumbTrail(p.dir, files.Home())
	for i, s := range segs {
		if i > 0 && segs[i-1].path != "/" { // the root's crumb is a slash already
			sep := gtk.NewLabel("/")
			sep.AddCssClass("files-crumb-sep")
			gtkx.Append(&p.crumbs, &sep.Widget)
		}
		p.crumb(s.label, s.icon, s.path, s.current)
	}
	if p.mode == inSearch {
		sep := gtk.NewLabel("·")
		sep.AddCssClass("files-crumb-sep")
		gtkx.Append(&p.crumbs, &sep.Widget)
		l := gtk.NewLabel(i18n.T("SEARCH"))
		l.AddCssClass("tag")
		gtkx.Append(&p.crumbs, &l.Widget)
	}
	// The end of the path in view.
	p.w.app.h.later(30, func() {
		if adj := p.crumbScroll.GetHadjustment(); adj != nil {
			adj.SetValue(adj.GetUpper())
		}
	})
}

func (p *pane) crumb(label, icon, target string, current bool) {
	b := gtk.NewButton()
	b.AddCssClass("files-crumb")
	if current {
		b.AddCssClass("current")
	}
	if icon != "" {
		b.SetIconName(icon)
		b.SetTooltipText(shortHome(target))
	} else {
		b.SetLabel(label)
	}
	p.w.app.h.click(b, func() { p.goKey(target, true) })
	// The other buttons leave the pane where it is: the middle one opens
	// the folder in a new tab behind this one, the right one its menu.
	g := p.w.app.h.press(&b.Widget, false, func(button uint32, _ int32, x, y float64) {
		switch button {
		case 2:
			p.newTab(target, false)
		case 3:
			p.crumbMenu(&b.Widget, target, x, y)
		}
	})
	p.crumbPtr = append(p.crumbPtr, b.GoPointer(), g)
	gtkx.Append(&p.crumbs, &b.Widget)
}

func (p *pane) updateNav() {
	p.back.SetSensitive(p.hist.CanBack())
	p.fwd.SetSensitive(p.hist.CanForward())
	p.up.SetSensitive(p.mode != inFolder || p.dir != "/")
}

// --- status ---

func (p *pane) updateStatus() {
	n := len(p.shown)
	var parts []string
	switch p.mode {
	case inSearch:
		parts = append(parts, i18n.Nf("%d found", "%d found", n, n))
	default:
		parts = append(parts, i18n.Nf("%d item", "%d items", n, n))
	}
	if hidden := len(p.all) - n; hidden > 0 && p.filter == "" {
		if pk := p.w.pick; pk != nil {
			// The picker's filter leaves files out, not hidden.
			hidden = 0
			for i := range p.all {
				if p.all[i].Hidden && p.w.pickShows(&p.all[i]) {
					hidden++
				}
			}
			if left := len(p.all) - n - hidden; left > 0 {
				parts = append(parts, i18n.Nf("%d not of this kind", "%d not of this kind", left, left))
			}
		}
		if hidden > 0 {
			parts = append(parts, i18n.Nf("%d hidden", "%d hidden", hidden, hidden))
		}
	}
	sel := p.selected()
	if len(sel) > 0 {
		var size int64
		dirs := 0
		for _, i := range sel {
			if p.all[i].Dir {
				dirs++
			} else {
				size += p.all[i].Size
			}
		}
		s := i18n.Nf("%d selected", "%d selected", len(sel), len(sel))
		if dirs < len(sel) {
			s += " (" + sizeText(size) + ")"
		}
		parts = append(parts, s)
	}
	if !p.flashing {
		p.statusLabel.SetText(strings.Join(parts, " · "))
	}
	p.freeLabel.SetText(p.free)
}

// --- search ---

func (p *pane) startSearch() {
	if p.mode == inTrash {
		return
	}
	if p.mode != inSearch {
		p.hist.Visit(p.dir)
	}
	p.mode, p.filter, p.query = inSearch, "", ""
	p.filterLabel.SetVisible(false)
	p.all, p.dirs, p.trash = nil, nil, nil
	p.refill(nil, false)
	p.showCrumbs()
	p.search.SetText("")
	p.pathStack.SetVisibleChildName("search")
	p.search.GrabFocus()
}

func (p *pane) leaveSearch() {
	if p.mode == inSearch {
		p.pathStack.SetVisibleChildName("crumbs")
	}
}

// searchChanged starts the search again a moment after the typing stops.
func (p *pane) searchChanged() {
	if p.mode != inSearch {
		return
	}
	p.gen++
	gen := p.gen
	q := strings.TrimSpace(p.search.GetText())
	p.w.app.h.later(220, func() {
		if gen == p.gen {
			p.runSearch(q)
		}
	})
}

func (p *pane) runSearch(q string) {
	if p.stop != nil {
		p.stop()
	}
	p.query = q
	p.all, p.dirs, p.scores = nil, nil, nil
	p.refill(nil, false)
	if q == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.stop = cancel
	gen := p.gen
	root, hidden := p.dir, p.fv.Order.ShowsHidden
	var mu sync.Mutex
	var batch []files.Hit
	flushing := false
	flush := func() {
		mu.Lock()
		hs := batch
		batch, flushing = nil, false
		mu.Unlock()
		if gen != p.gen || len(hs) == 0 {
			return
		}
		at := len(p.names)
		for _, h := range hs {
			p.all = append(p.all, h.Entry)
			p.dirs = append(p.dirs, h.Dir)
			p.scores = append(p.scores, h.Score)
			p.shown = append(p.shown, len(p.all)-1)
			p.names = append(p.names, h.Entry.Name)
		}
		p.model.Splice(uint32(at), 0, p.names[at:])
		p.syncView()
		p.updateStatus()
	}
	go func() {
		files.Search(ctx, root, q, files.SearchOptions{Hidden: hidden}, func(hs []files.Hit) {
			mu.Lock()
			batch = append(batch, hs...)
			post := !flushing
			flushing = true
			mu.Unlock()
			if post {
				gtkx.Post(flush)
			}
		})
		gtkx.Post(func() {
			flush()
			if gen != p.gen {
				return
			}
			// Found in the walk's order; done, the best matches first.
			sel := p.selectedPaths()
			idx := make([]int, len(p.all))
			for i := range idx {
				idx[i] = i
			}
			slices.SortStableFunc(idx, func(a, b int) int { return p.scores[b] - p.scores[a] })
			all, dirs, scores := make([]files.Entry, len(idx)), make([]string, len(idx)), make([]int, len(idx))
			for k, i := range idx {
				all[k], dirs[k], scores[k] = p.all[i], p.dirs[i], p.scores[i]
			}
			p.all, p.dirs, p.scores = all, dirs, scores
			p.names = nil
			p.refill(nil, false)
			if len(sel) == 0 && len(p.shown) > 0 {
				p.sel.SelectItem(0, true)
			}
			p.statusLabel.SetText(p.statusLabel.GetText() + " · " + i18n.T("done"))
		})
	}()
}

// --- trash ---

func (p *pane) showTrash(record bool) {
	if record && p.mode != inTrash && p.dir != "" {
		p.hist.Visit(p.locationKey())
	}
	if p.mode != inTrash {
		p.filter = ""
		p.filterLabel.SetVisible(false)
	}
	if p.stop != nil {
		p.stop()
	}
	p.mode = inTrash
	p.fv = files.FolderView{View: files.ListView, Order: files.Order{By: files.ByModified, Reverse: true}}
	p.gen++
	gen := p.gen
	p.showCrumbs()
	p.updateNav()
	sel := p.selectedNames()
	ctx, cancel := context.WithCancel(context.Background())
	p.stop = cancel
	go func() {
		trashes := files.Trashes()
		items := files.ListTrash(trashes)
		es := files.TrashEntries(items)
		// Newest first, the items beside their entries.
		idx := make([]int, len(es))
		for i := range idx {
			idx[i] = i
		}
		slices.SortStableFunc(idx, func(a, b int) int { return es[b].ModTime.Compare(es[a].ModTime) })
		ses, sitems := make([]files.Entry, len(es)), make([]files.Trashed, len(es))
		for k, i := range idx {
			ses[k], sitems[k] = es[i], items[i]
		}
		gtkx.Post(func() {
			if gen != p.gen {
				return
			}
			p.err, p.free = nil, ""
			p.all, p.trash, p.dirs = ses, sitems, nil
			p.refill(sel, true)
		})
		reload := func() {
			gtkx.Post(func() {
				if gen == p.gen {
					p.showTrash(false)
				}
			})
		}
		for _, t := range trashes {
			files.Watch(ctx, filepath.Join(t, "info"), reload, nil)
		}
	}()
}

// --- the menu of the items under the pointer ---

// pick finds the row under x, y of view v, if any.
func (p *pane) pick(v *gtk.Widget, x, y float64) (*row, bool) {
	return pickRow(p.rowOf, v, x, y)
}

// pickRow finds the row under x, y of view v among rowOf's.
func pickRow(rowOf map[uintptr]*row, v *gtk.Widget, x, y float64) (*row, bool) {
	w := v.Pick(x, y, gtk.PickDefaultValue)
	for w != nil {
		if r, ok := rowOf[w.GoPointer()]; ok {
			w.Unref()
			return r, true
		}
		parent := w.GetParent()
		w.Unref()
		if parent == nil || parent.GoPointer() == v.GoPointer() {
			if parent != nil {
				parent.Unref()
			}
			return nil, false
		}
		w = parent
	}
	return nil, false
}

func (p *pane) contextMenu(v *gtk.Widget, x, y float64) {
	p.w.setActive(p)
	if r, ok := p.pick(v, x, y); ok {
		if !p.sel.IsSelected(r.pos) {
			p.sel.SelectItem(r.pos, true)
		}
	} else {
		p.sel.UnselectAll()
	}
	var px, py float64
	v.TranslateCoordinates(&p.box.Widget, x, y, &px, &py)
	p.showMenu(int32(px), int32(py))
}

// menuAtSelection opens the menu under the first selected row on screen
// (the menu key), else near the top of the pane.
func (p *pane) menuAtSelection() {
	x, y := float64(40), float64(80)
	if sel := p.selected(); len(sel) > 0 {
		for _, r := range p.rows {
			if int(r.pos) < len(p.shown) && p.shown[r.pos] == sel[0] && r.box.GetMapped() {
				r.box.TranslateCoordinates(&p.box.Widget, 48, float64(r.box.GetHeight()), &x, &y)
				break
			}
		}
	}
	p.showMenu(int32(x), int32(y))
}

// showMenu opens the menu of the selection (or of the folder) at x, y in
// the pane.
func (p *pane) showMenu(x, y int32) {
	p.menu.show(func() []menuItem { return p.w.menuItems(p) }, x, y)
}

// --- drag and drop ---

// dragContent offers the selection (with the row under the pointer) as
// text/uri-list, which GTK apps take as files and browsers as uploads.
func (p *pane) dragContent(v *gtk.Widget, x, y float64) uintptr {
	r, ok := p.pick(v, x, y)
	if !ok {
		return 0 // on empty space: the rubber band, not a drag
	}
	if !p.sel.IsSelected(r.pos) {
		p.sel.SelectItem(r.pos, true)
	}
	paths := p.selectedPaths()
	if len(paths) == 0 {
		return 0
	}
	list := files.URIList(paths)
	bytes := glib.NewBytes([]byte(list), uint(len(list)))
	cp := gdk.NewContentProviderForBytes("text/uri-list", bytes)
	bytes.Unref()
	return cp.GoPointer()
}

// dropped copies or moves dropped files into the folder under the
// pointer, else this pane's folder.
func (p *pane) dropped(v *gtk.Widget, paths []string, x, y float64, state gdk.ModifierType) {
	if p.mode == inTrash || len(paths) == 0 || p.w.pick != nil { // the picker chooses, it does not move
		return
	}
	dest := p.dir
	if r, ok := p.pick(v, x, y); ok && int(r.pos) < len(p.shown) {
		if i := p.shown[r.pos]; p.all[i].Dir {
			dest = p.path(i)
		}
	}
	kind := files.Copy
	switch {
	case state&gdk.ControlMaskValue != 0:
	case state&gdk.ShiftMaskValue != 0:
		kind = files.Move
	case files.SameDevice(paths[0], dest):
		kind = files.Move
	}
	p.w.app.run(p.w, files.Job{Kind: kind, Sources: paths, Dest: dest})
}

// droppedPaths are the local files of a dropped GdkFileList.
func droppedPaths(v *gobject.Value) []string {
	boxed := v.GetBoxed()
	if boxed == 0 {
		return nil
	}
	var paths []string
	list := (*gdk.FileList)(unsafe.Add(unsafe.Pointer(nil), boxed)) // as tailscale.go does
	for l := list.GetFiles(); l != nil; l = l.Next {
		if path := (&gio.FileBase{Ptr: l.Data}).GetPath(); path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

// --- zoom ---

// The zoom's steps, -2 to 3: the percent shown, and the icons' sizes.
var (
	zoomPercent = [...]int{80, 90, 100, 115, 130, 150}
	listIcons   = [...]int32{14, 15, 16, 19, 22, 26}
	gridIcons   = [...]int32{56, 68, 80, 104, 128, 160}
)

func zoomStep(z int) int        { return max(0, min(len(zoomPercent)-1, z+2)) }
func listIcon(z int) int32      { return listIcons[zoomStep(z)] }
func gridIcon(z int) int32      { return gridIcons[zoomStep(z)] }
func zoomClass(step int) string { return fmt.Sprintf("files-zoom-%d", step) }

// syncZoom sizes the rows' text as the zoom says (a class on the pane,
// style.css) and shows it.
func (p *pane) syncZoom() {
	z := zoomStep(p.w.app.settings.Zoom)
	for k := range zoomPercent {
		setClass(&p.box.Widget, zoomClass(k), k == z)
	}
	p.zoomLevel.SetLabel(fmt.Sprintf("%d%%", zoomPercent[z]))
	p.syncColumns()
	p.syncColumnWidths()
}

// --- small helpers ---

// noModel takes a view's model away: the bindings take an interface, and
// a zero one passes NULL.
var noModel = &gtk.SelectionModelBase{}

func setClass(w *gtk.Widget, class string, on bool) {
	if on {
		w.AddCssClass(class)
	} else {
		w.RemoveCssClass(class)
	}
}

// sizeText is a size as the list shows it: 812 B, 14.2 kB, 3.4 MB.
func sizeText(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, i := float64(n)/unit, 0
	for v >= unit && i < 4 {
		v /= unit
		i++
	}
	f := "%.1f %s"
	if v >= 100 {
		f = "%.0f %s"
	}
	return fmt.Sprintf(f, v, []string{"kB", "MB", "GB", "TB", "PB"}[i])
}

// dateText is a time as the list shows it: the time today, the day and
// month this year, else the date.
func dateText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	switch {
	case t.Year() == now.Year() && t.YearDay() == now.YearDay():
		return i18n.Time(t, "15:04")
	case t.Year() == now.Year():
		return i18n.Time(t, "Jan 2 15:04")
	}
	return i18n.Time(t, "Jan 2 2006")
}

// shortHome writes the home folder as "~".
func shortHome(path string) string {
	home := files.Home()
	if path == home || strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}
