package diskui

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/disk"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// listMax is how many of a folder's items its list shows; the rest are
// counted on a last line.
const listMax = 40

// mapSize is the map's side, in pixels at scale 1.
const mapSize = 300

// kindMax is how many folders a kind's page lists.
const kindMax = 200

// window is Disk Usage's window: the index of places and kinds, the page
// chosen, and the panel of the item chosen.
type window struct {
	c   *appui.Ctx
	win *gtk.ApplicationWindow
	// menu is published for the bar while this window has the focus.
	menu *appui.MenuBar

	sidePlaces, sideKinds                gtk.Box
	sideChoose                           gtk.Button
	pages                                gtk.Stack
	placeLabel, placeMeta, placeEmpty    gtk.Label
	placeError, progressText, progressAt gtk.Label
	placeScan, placeStop                 gtk.Button
	placeProgress, placeMap, crumbs      gtk.Box
	mapList, legend, mapMiddle           gtk.Box
	mapOverlay                           gtk.Overlay
	mapCanvas                            *gtk.Widget
	// drawn is the arcs the map draws now, paint their colours; ticking
	// is set while the frame clock turns the scanning sweep.
	drawn                                     []disk.Arc
	paint                                     disk.Paint
	ticking                                   bool
	onTick                                    gtk.TickCallback
	onTickDone                                glib.DestroyNotify
	mapName, mapSize                          gtk.Label
	kindLabel, kindMeta, kindDesc, kindNone   gtk.Label
	kindRows, kindEmpty                       gtk.Box
	kindEmptyIcon, detailsEmpty               gtk.Image
	details, selSteps, selCleanBox, selFacts  gtk.Box
	selTag, selName, selPath, selMeta         gtk.Label
	selVerdict, selClean                      gtk.Label
	selDeleteNote, selDeleteErr, selItemError gtk.Label
	selRunNote, selRunOut, selRunDone         gtk.Label
	selRunError, selDone                      gtk.Label
	selMap, selOpen                           gtk.Button
	selTrash, selDelete, selCopy, selRun      gtk.Button

	home   string
	places []disk.Place
	scans  map[string]*scanState // by the place's path
	page   string                // "place" or "kind"
	place  string                // the place in view
	kind   disk.Kind             // the kind in view

	sel      *disk.Node
	routes   map[*disk.Node]*disk.Route
	answers  map[*disk.Node]*aiAnswer // what the AI said, by item
	askingAI *disk.Node               // the item the AI is asked about
	onAsk    func(gtk.Button)
	running  *disk.Node // the item whose clean command runs
	runs     map[*disk.Node]runResult
	question *dialog // the question asked, while it is open
	done     struct {
		n    *disk.Node
		text string
	} // what was just done, said on the item it was done to or in
	onCopied glib.SourceFunc
	ai       aiState

	arcs  []disk.Arc
	hover *disk.Node

	sideRows  map[uintptr]string    // a place's button → its path
	kindBtns  map[uintptr]disk.Kind // a kind's button → the kind
	nodeRows  map[uintptr]*disk.Node
	sideBtns  map[string]*gtk.Button // places' and kinds' buttons, by "place:PATH" or "kind:ID"
	dialog    *gtk.FileDialog
	closed    bool
	onClick   func(gtk.Button)
	onChosen  gio.AsyncReadyCallback
	onPress   func(gtk.GestureClick, int32, float64, float64)
	onMotion  func(gtk.EventControllerMotion, float64, float64)
	onLeave   func(gtk.EventControllerMotion)
	onKey     func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onClose   func(gtk.Window) bool
	onRowMove func(gtk.EventControllerMotion, float64, float64)
	rowHover  map[uintptr]*disk.Node
}

// scanState is a place's scan: running, or done with its tree.
type scanState struct {
	root     *disk.Node
	focus    *disk.Node
	progress *disk.Progress
	cancel   context.CancelFunc
	err      string
	started  time.Time
	done     time.Time
	phase    float64 // the sweep's angle while it runs
}

func (s *scanState) running() bool { return s != nil && s.cancel != nil }

func openWindow(c *appui.Ctx) *window {
	w := &window{
		c: c, win: &gtk.ApplicationWindow{}, scans: map[string]*scanState{}, page: "place",
		routes: map[*disk.Node]*disk.Route{}, answers: map[*disk.Node]*aiAnswer{}, runs: map[*disk.Node]runResult{},
		sideRows: map[uintptr]string{}, kindBtns: map[uintptr]disk.Kind{}, nodeRows: map[uintptr]*disk.Node{},
		sideBtns: map[string]*gtk.Button{}, rowHover: map[uintptr]*disk.Node{},
	}
	w.home, _ = os.UserHomeDir()
	b := c.Load("disk.ui")
	b.Get("disk", w.win)
	for name, ptr := range map[string]gobject.Ptr{
		"side_places": &w.sidePlaces, "side_kinds": &w.sideKinds, "side_choose": &w.sideChoose, "pages": &w.pages,
		"place_label": &w.placeLabel, "place_meta": &w.placeMeta, "place_empty": &w.placeEmpty,
		"place_error": &w.placeError, "progress_text": &w.progressText, "progress_at": &w.progressAt,
		"place_scan": &w.placeScan, "place_stop": &w.placeStop, "place_progress": &w.placeProgress,
		"place_map": &w.placeMap, "crumbs": &w.crumbs, "map_list": &w.mapList, "legend": &w.legend,
		"map_middle": &w.mapMiddle, "map_overlay": &w.mapOverlay, "map_name": &w.mapName, "map_size": &w.mapSize,
		"kind_label": &w.kindLabel, "kind_meta": &w.kindMeta, "kind_desc": &w.kindDesc, "kind_empty": &w.kindEmpty,
		"kind_empty_icon": &w.kindEmptyIcon, "kind_empty_text": &w.kindNone, "kind_rows": &w.kindRows, "details_empty": &w.detailsEmpty, "details": &w.details, "sel_steps": &w.selSteps,
		"sel_clean_box": &w.selCleanBox, "sel_facts": &w.selFacts,
		"sel_tag": &w.selTag, "sel_name": &w.selName, "sel_path": &w.selPath, "sel_meta": &w.selMeta,
		"sel_verdict": &w.selVerdict, "sel_clean": &w.selClean,
		"sel_delete_note": &w.selDeleteNote, "sel_delete_error": &w.selDeleteErr,
		"sel_copy": &w.selCopy, "sel_run": &w.selRun, "sel_run_note": &w.selRunNote, "sel_run_out": &w.selRunOut,
		"sel_run_done": &w.selRunDone, "sel_run_error": &w.selRunError, "sel_done": &w.selDone,
		"sel_item_error": &w.selItemError, "sel_map": &w.selMap, "sel_open": &w.selOpen,
		"sel_trash": &w.selTrash, "sel_delete": &w.selDelete,
	} {
		b.Get(name, ptr)
	}
	b.Close()
	w.mapCanvas = w.buildMap()
	w.mapOverlay.SetChild(w.mapCanvas)
	w.onTickDone = func(uintptr) {}

	w.onClick = func(b gtk.Button) { w.clicked(b) }
	w.onAsk = func(gtk.Button) { w.askAI() }
	for _, btn := range []*gtk.Button{&w.sideChoose, &w.placeScan, &w.placeStop, &w.selMap, &w.selOpen,
		&w.selTrash, &w.selDelete, &w.selCopy, &w.selRun} {
		btn.ConnectClicked(&w.onClick)
	}
	w.onChosen = func(_, result, _ uintptr) { w.folderChosen(result) }

	click := gtk.NewGestureClick()
	click.SetButton(1)
	w.onPress = func(_ gtk.GestureClick, n int32, x, y float64) { w.mapClicked(x, y) }
	click.ConnectReleased(&w.onPress)
	w.mapCanvas.AddController(&click.EventController)
	motion := gtk.NewEventControllerMotion()
	w.onMotion = func(_ gtk.EventControllerMotion, x, y float64) { w.mapHovered(x, y) }
	w.onLeave = func(gtk.EventControllerMotion) { w.setHover(nil) }
	motion.ConnectMotion(&w.onMotion)
	motion.ConnectLeave(&w.onLeave)
	w.mapCanvas.AddController(&motion.EventController)
	w.mapCanvas.SetCursorFromName("pointer")
	w.onRowMove = func(m gtk.EventControllerMotion, _, _ float64) {
		if n := w.rowHover[m.GoPointer()]; n != nil && n != w.hover {
			w.setHover(n)
		}
	}

	keys := gtk.NewEventControllerKey()
	w.onKey = func(_ gtk.EventControllerKey, keyval, _ uint32, state gdk.ModifierType) bool {
		up := keyval == uint32(gdk.KEY_BackSpace) || keyval == uint32(gdk.KEY_Up) && state&gdk.AltMaskValue != 0
		if up && w.page == "place" {
			if s := w.scans[w.place]; s != nil && s.focus != nil && s.focus.Parent != nil {
				w.focus(s.focus.Parent)
				return true
			}
		}
		return false
	}
	keys.ConnectKeyPressed(&w.onKey)
	w.win.AddController(&keys.EventController)

	w.onClose = func(gtk.Window) bool {
		w.closed = true
		for _, s := range w.scans {
			if s.cancel != nil {
				s.cancel()
			}
		}
		w.ai.close()
		if w.menu != nil {
			w.menu.Close()
		}
		return false
	}
	w.win.ConnectCloseRequest(&w.onClose)
	w.menu = w.menuBar()

	w.places = disk.Places(w.home)
	w.fillLegend()
	w.fillSide()
	w.showPlace(w.home)
	return w
}

// say shows a message in the line l, beside what it is about; "" hides it.
func say(l *gtk.Label, msg string) {
	l.SetText(msg)
	l.SetVisible(msg != "")
}

func setClass(wd *gtk.Widget, class string, on bool) {
	if on {
		wd.AddCssClass(class)
	} else {
		wd.RemoveCssClass(class)
	}
}

// background runs f off the main thread, then done on it, unless the
// window closed meanwhile.
func (w *window) background(f func(), done func()) {
	go func() {
		f()
		gtkx.Post(func() {
			if !w.closed {
				done()
			}
		})
	}()
}

func (w *window) clicked(b gtk.Button) {
	p := b.GoPointer()
	if path, ok := w.sideRows[p]; ok {
		w.showPlace(path)
		return
	}
	if k, ok := w.kindBtns[p]; ok {
		w.showKind(k)
		return
	}
	if n, ok := w.nodeRows[p]; ok {
		if w.page == "kind" {
			w.selectNode(n)
		} else {
			w.pick(n)
		}
		return
	}
	switch p {
	case w.sideChoose.GoPointer():
		w.chooseFolder()
	case w.placeScan.GoPointer():
		w.scan(w.place)
	case w.placeStop.GoPointer():
		if s := w.scans[w.place]; s.running() {
			s.cancel()
		}
	case w.selMap.GoPointer():
		w.showOnMap(w.sel)
	case w.selOpen.GoPointer():
		w.openInFiles()
	case w.selTrash.GoPointer():
		w.remove(w.sel, false)
	case w.selDelete.GoPointer():
		w.askDelete()
	case w.selCopy.GoPointer():
		w.copyCommand()
	case w.selRun.GoPointer():
		w.askRun()
	}
}

// --- the index ---

// indexItem makes an index button: an icon, a title, a line under it.
func indexItem(icon, title, sub string) *gtk.Button {
	b := gtk.NewButton()
	b.AddCssClass("index-item")
	box := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	img := gtk.NewImageFromIconName(icon)
	img.AddCssClass("index-icon")
	gtkx.Append(box, &img.Widget)
	text := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	text.SetHexpand(true)
	t := gtk.NewLabel(title)
	t.AddCssClass("title")
	t.SetXalign(0)
	t.SetEllipsize(3) // end
	gtkx.Append(text, &t.Widget)
	text.SetValign(gtk.AlignCenterValue)
	if sub == "" {
		gtkx.Append(box, &text.Widget)
		b.SetChild(&box.Widget)
		return b
	}
	s := gtk.NewLabel(sub)
	s.AddCssClass("sub")
	s.SetXalign(0)
	s.SetEllipsize(3)
	gtkx.Append(text, &s.Widget)
	gtkx.Append(box, &text.Widget)
	b.SetChild(&box.Widget)
	return b
}

func (w *window) placeName(p disk.Place) string {
	switch {
	case p.Path == w.home:
		return i18n.T("Home")
	case p.Path == "/":
		return i18n.T("System")
	case p.Name != "":
		return p.Name
	}
	return filepath.Base(p.Path)
}

func placeIcon(p disk.Place, home string) string {
	switch {
	case p.Path == home:
		return "user-home-symbolic"
	case p.Total > 0 && p.Name != "" || p.Path == "/":
		return "disk-drive-symbolic"
	}
	return "folder-symbolic"
}

// fillSide lists the places and the kinds.
func (w *window) fillSide() {
	gtkx.Clear(&w.sidePlaces)
	gtkx.Clear(&w.sideKinds)
	w.sideRows = map[uintptr]string{}
	w.kindBtns = map[uintptr]disk.Kind{}
	w.sideBtns = map[string]*gtk.Button{}
	for _, p := range w.places {
		sub := ""
		switch s := w.scans[p.Path]; {
		case s.running():
			sub = i18n.T("Scanning…")
		case s != nil && s.root != nil && p.Total == 0:
			sub = disk.Size(s.root.Size)
		case p.Total > 0:
			sub = i18n.Tf("%s free of %s", disk.Size(p.Free), disk.Size(p.Total))
		default:
			sub = disk.Tilde(p.Path, w.home)
		}
		b := indexItem(placeIcon(p, w.home), w.placeName(p), sub)
		b.ConnectClicked(&w.onClick)
		w.sideRows[b.GoPointer()] = p.Path
		w.sideBtns["place:"+p.Path] = b
		gtkx.Append(&w.sidePlaces, &b.Widget)
	}
	for _, k := range disk.Kinds {
		sub := ""
		if w.scanned() {
			all, safe := disk.Total(w.found(k))
			sub = i18n.Tf("%s · %s safe", disk.Size(all), disk.Size(safe))
		}
		b := indexItem(kindIcons[k], k.Name(), sub)
		b.ConnectClicked(&w.onClick)
		w.kindBtns[b.GoPointer()] = k
		w.sideBtns["kind:"+k.ID()] = b
		gtkx.Append(&w.sideKinds, &b.Widget)
	}
	w.markSide()
}

func (w *window) markSide() {
	for key, b := range w.sideBtns {
		on := w.page == "place" && key == "place:"+w.place || w.page == "kind" && key == "kind:"+w.kind.ID()
		setClass(&b.Widget, "selected", on)
	}
}

// scanned reports whether a scan has finished.
func (w *window) scanned() bool {
	for _, s := range w.scans {
		if s.root != nil {
			return true
		}
	}
	return false
}

// found are the folders of kind k every scan found, each once.
func (w *window) found(k disk.Kind) []*disk.Node {
	var all []*disk.Node
	for _, p := range w.places {
		if s := w.scans[p.Path]; s != nil && s.root != nil {
			all = append(all, disk.Found(s.root, k)...)
		}
	}
	// A place inside another, both scanned: what both found once.
	paths := make(map[*disk.Node]string, len(all))
	for _, n := range all {
		paths[n] = n.Path()
	}
	slices.SortStableFunc(all, func(a, b *disk.Node) int { return strings.Compare(paths[a], paths[b]) })
	var out []*disk.Node
	last := ""
	for _, n := range all {
		p := paths[n]
		if last != "" && (p == last || strings.HasPrefix(p, last+"/")) {
			continue
		}
		out = append(out, n)
		last = p
	}
	slices.SortStableFunc(out, func(a, b *disk.Node) int {
		switch {
		case a.Size > b.Size:
			return -1
		case a.Size < b.Size:
			return 1
		}
		return 0
	})
	return out
}

// chooseFolder asks for a folder to scan.
func (w *window) chooseFolder() {
	if w.dialog != nil {
		return
	}
	d := gtk.NewFileDialog()
	d.SetTitle(i18n.T("Choose a folder to scan"))
	f := gio.FileNewForPath(w.home)
	d.SetInitialFolder(f)
	(&gobject.Object{Ptr: f.GoPointer()}).Unref()
	w.dialog = d
	d.SelectFolder(&w.win.Window, nil, &w.onChosen, 0)
}

func (w *window) folderChosen(result uintptr) {
	d := w.dialog
	w.dialog = nil
	if d == nil {
		return
	}
	defer d.Unref()
	file, err := d.SelectFolderFinish(&gio.AsyncResultBase{Ptr: result})
	if err != nil || file == nil {
		return // cancelled
	}
	path := file.GetPath()
	(&gobject.Object{Ptr: file.GoPointer()}).Unref()
	if path != "" && !w.closed {
		w.scanFolder(path)
	}
}

// scanFolder adds path to the places, if it is not one, and scans it.
func (w *window) scanFolder(path string) {
	path, err := filepath.Abs(path)
	if err != nil {
		return
	}
	path = filepath.Clean(path)
	if !slices.ContainsFunc(w.places, func(p disk.Place) bool { return p.Path == path }) {
		w.places = append(w.places, disk.Place{Path: path})
		w.fillSide()
	}
	w.showPlace(path)
	if s := w.scans[path]; !s.running() {
		w.scan(path)
	}
}

// --- a place ---

func (w *window) showPlace(path string) {
	w.page, w.place = "place", path
	w.pages.SetVisibleChildName("place")
	w.markSide()
	var p disk.Place
	for _, q := range w.places {
		if q.Path == path {
			p = q
		}
	}
	w.placeLabel.SetText("[" + strings.ToUpper(w.placeName(p)) + "]")
	w.showScan()
	if s := w.scans[path]; s != nil && s.root != nil {
		w.focus(s.focus)
	} else {
		w.selectNode(nil)
	}
}

// showScan shows the place's scan as it is: none yet, running, failed or
// done.
func (w *window) showScan() {
	s := w.scans[w.place]
	running := s.running()
	done := s != nil && s.root != nil
	w.placeScan.SetVisible(!running)
	w.placeStop.SetVisible(running)
	if done {
		w.placeScan.SetLabel(i18n.T("Scan again"))
		w.placeScan.RemoveCssClass("suggested-action")
		w.placeScan.AddCssClass("outlined")
	} else {
		w.placeScan.SetLabel(i18n.T("Scan"))
		w.placeScan.RemoveCssClass("outlined")
		w.placeScan.AddCssClass("suggested-action")
	}
	w.placeEmpty.SetVisible(!running && !done)
	where := disk.Tilde(w.place, w.home)
	if w.place == w.home {
		where = i18n.T("your home")
	}
	w.placeEmpty.SetText(i18n.Tf("Scan to see what fills %s.", where))
	w.placeProgress.SetVisible(running)
	w.crumbs.SetVisible(done && !running)
	w.mapList.SetVisible(done && !running)
	w.legend.SetVisible(done && !running)
	if !done || running {
		w.arcs = nil
		w.drawMap()
	}
	errText := ""
	if s != nil {
		errText = s.err
	}
	say(&w.placeError, errText)
	meta := ""
	for _, p := range w.places {
		if p.Path == w.place && p.Total > 0 {
			meta = i18n.Tf("%s used of %s", disk.Size(p.Total-p.Free), disk.Size(p.Total))
		}
	}
	switch {
	case running:
		meta = i18n.T("Scanning…")
		w.showProgress(s)
		w.sweep(s, w.place)
	case done:
		meta = i18n.Tf("%s in %s files · %s", disk.Size(s.root.Size), count(s.root.Files), i18n.Time(s.done, "15:04"))
	}
	w.placeMeta.SetText(meta)
}

func count(n int64) string {
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func (w *window) showProgress(s *scanState) {
	w.progressText.SetText(i18n.Tf("%s files · %s · %.0f s", count(s.progress.Files.Load()), disk.Size(s.progress.Bytes.Load()),
		time.Since(s.started).Seconds()))
	w.progressAt.SetText(disk.Tilde(s.progress.At(), w.home))
}

// scan measures the place at path.
func (w *window) scan(path string) {
	if s := w.scans[path]; s.running() {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &scanState{progress: &disk.Progress{}, cancel: cancel, started: time.Now()}
	if old := w.scans[path]; old != nil {
		w.forget(old.root)
	}
	w.scans[path] = s
	if w.place == path {
		w.selectNode(nil)
		w.showScan()
	}
	w.fillSide()
	w.sweep(s, path)
	var root *disk.Node
	var err error
	w.background(func() {
		root, err = disk.Scan(ctx, path, disk.Options{Home: w.home}, s.progress)
	}, func() {
		cancel()
		s.cancel = nil
		s.done = time.Now()
		switch {
		case ctx.Err() != nil && err != nil:
			s.err = "" // stopped
		case err != nil:
			s.err = i18n.Tf("Cannot scan %s: %v", disk.Tilde(path, w.home), err)
		default:
			s.root, s.focus = root, root
		}
		if w.scans[path] != s {
			return
		}
		w.fillSide()
		if w.page == "place" && w.place == path {
			w.showScan()
			if s.root != nil {
				w.focus(s.root)
			}
		} else if w.page == "kind" {
			w.showKind(w.kind)
		}
	})
}

// forget drops what is kept about the nodes of a tree scanned again.
func (w *window) forget(root *disk.Node) {
	if root == nil {
		return
	}
	for n := range w.routes {
		if n.Root() == root {
			delete(w.routes, n)
		}
	}
	for n := range w.answers {
		if n.Root() == root {
			delete(w.answers, n)
		}
	}
	if w.sel != nil && w.sel.Root() == root {
		w.selectNode(nil)
	}
}

// --- the map ---

// focus shows the folder n on the map and in the list, and chooses it.
func (w *window) focus(n *disk.Node) {
	s := w.scans[w.place]
	if s == nil || s.root == nil || n == nil {
		return
	}
	s.focus = n
	w.hover = nil
	w.arcs = disk.Layout(n)
	w.drawMap()
	w.showMiddle(n)
	w.fillCrumbs(n)
	w.fillList(n)
	w.selectNode(n)
}

// pick is a click on an item of the folder in view: a folder is opened,
// anything else chosen.
func (w *window) pick(n *disk.Node) {
	if n.Dir && len(n.Kids) > 0 {
		w.focus(n)
		return
	}
	w.selectNode(n)
}

func (w *window) mapClicked(x, y float64) {
	s := w.scans[w.place]
	if s == nil || s.focus == nil {
		return
	}
	g := disk.Measure(float64(w.mapCanvas.GetWidth()), float64(w.mapCanvas.GetHeight()))
	i, middle := disk.Hit(w.arcs, g, x, y)
	switch {
	case middle && s.focus.Parent != nil:
		w.focus(s.focus.Parent)
	case middle:
		w.selectNode(s.focus)
	case i >= 0:
		w.pick(w.arcs[i].Node)
	}
}

func (w *window) mapHovered(x, y float64) {
	g := disk.Measure(float64(w.mapCanvas.GetWidth()), float64(w.mapCanvas.GetHeight()))
	i, _ := disk.Hit(w.arcs, g, x, y)
	if i < 0 {
		w.setHover(nil)
		return
	}
	w.setHover(w.arcs[i].Node)
}

// setHover marks n on the map, and says in the middle what it is.
func (w *window) setHover(n *disk.Node) {
	if n == w.hover {
		return
	}
	w.hover = n
	w.drawMap()
	if n != nil {
		w.showMiddle(n)
	} else if s := w.scans[w.place]; s != nil && s.focus != nil {
		w.showMiddle(s.focus)
	}
}

func (w *window) showMiddle(n *disk.Node) {
	w.mapName.SetText(nodeName(n))
	w.mapSize.SetText(disk.Size(n.Size))
}

func nodeName(n *disk.Node) string {
	if n.Rest {
		return i18n.Nf("%d smaller file", "%d smaller files", int(n.Files), n.Files)
	}
	return n.Name
}

func hexColor(s string) color.NRGBA {
	v, err := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32)
	if err != nil || len(s) != 7 {
		return color.NRGBA{128, 128, 128, 255}
	}
	return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
}

// drawMap draws the map in the theme's colours: the folder in view; while
// scanning, the folders measured so far and the sweep; before a scan, the
// disk's used room.
func (w *window) drawMap() {
	s := w.scans[w.place]
	col := func(name string) color.NRGBA { return hexColor(w.c.Colors[name]) }
	alpha := func(c color.NRGBA, a float64) color.NRGBA {
		c.A = uint8(float64(c.A) * a)
		return c
	}
	plain := alpha(col("t2"), 0.6)
	p := disk.Paint{Middle: col("surface"), Plain: plain, Hot: col("t0"), Guide: alpha(col("line"), 0.9)}
	p.Kind[disk.KindDev] = col("accent")
	p.Kind[disk.KindAgent] = col("warn")
	p.Kind[disk.KindCache] = col("ok")
	arcs := w.arcs
	switch {
	case s.running():
		arcs = disk.Scanning(s.progress.Tops(), s.progress.Bytes.Load(), s.phase, p.Kind, plain, col("accent"))
		w.mapName.SetText(disk.Size(s.progress.Bytes.Load()))
		w.mapSize.SetText(i18n.T("counted"))
	case s != nil && s.focus != nil:
		p.Guide = color.NRGBA{}
		p.Fade = func(ring int) float64 { return 1 - 0.12*float64(ring-1) }
		p.HotArc = map[*disk.Node]bool{}
		if w.hover != nil {
			for _, a := range w.arcs {
				for q := a.Node; q != nil; q = q.Parent {
					if q == w.hover {
						p.HotArc[a.Node] = true
						break
					}
				}
			}
		}
	default:
		var place disk.Place
		for _, q := range w.places {
			if q.Path == w.place {
				place = q
			}
		}
		arcs = disk.Used(place.Total-place.Free, place.Total, alpha(col("accent"), 0.55), alpha(col("line"), 0.6))
		if place.Total > 0 {
			w.mapName.SetText(disk.Size(place.Free))
			w.mapSize.SetText(i18n.T("free"))
		} else {
			w.mapName.SetText(i18n.T("Not scanned"))
			w.mapSize.SetText("")
		}
	}
	w.drawn, w.paint = arcs, p
	w.mapCanvas.QueueDraw()
}

// fillLegend says what the map's colours are.
func (w *window) fillLegend() {
	gtkx.Clear(&w.legend)
	for _, it := range []struct{ class, text string }{
		{"dev", disk.KindDev.Name()}, {"agent", disk.KindAgent.Name()}, {"cache", disk.KindCache.Name()},
		{"plain", i18n.T("Other")},
	} {
		box := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
		sw := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		sw.AddCssClass("disk-swatch")
		sw.AddCssClass(it.class)
		sw.SetValign(gtk.AlignCenterValue)
		gtkx.Append(box, &sw.Widget)
		l := gtk.NewLabel(it.text)
		l.AddCssClass("sub")
		gtkx.Append(box, &l.Widget)
		gtkx.Append(&w.legend, &box.Widget)
	}
}

// fillCrumbs shows the folders from the place to n.
func (w *window) fillCrumbs(n *disk.Node) {
	gtkx.Clear(&w.crumbs)
	for k := range w.nodeRows {
		delete(w.nodeRows, k)
	}
	var trail []*disk.Node
	for a := n; a != nil; a = a.Parent {
		trail = append(trail, a)
	}
	slices.Reverse(trail)
	for i, a := range trail {
		if i > 0 {
			sep := gtk.NewLabel("›")
			sep.AddCssClass("disk-crumb-sep")
			gtkx.Append(&w.crumbs, &sep.Widget)
		}
		name := a.Name
		if i == 0 {
			name = disk.Tilde(a.Path(), w.home)
		}
		b := gtk.NewButtonWithLabel(name)
		b.AddCssClass("disk-crumb")
		if a == n {
			b.AddCssClass("current")
		}
		b.ConnectClicked(&w.onClick)
		w.nodeRows[b.GoPointer()] = a
		gtkx.Append(&w.crumbs, &b.Widget)
	}
}

// kindClass is the class of the colour n has on the map.
func kindClass(n *disk.Node) string {
	if r := disk.RuleOf(n); r != nil {
		return r.Kind.ID()
	}
	return "plain"
}

// fillList lists what is in n, largest first.
func (w *window) fillList(n *disk.Node) {
	gtkx.Clear(&w.mapList)
	for k := range w.rowHover {
		delete(w.rowHover, k)
	}
	for i, k := range n.Kids {
		if i == listMax {
			rest := gtk.NewLabel(i18n.Nf("%d more item", "%d more items", len(n.Kids)-i, len(n.Kids)-i))
			rest.AddCssClass("sub")
			rest.AddCssClass("disk-more")
			rest.SetXalign(0)
			gtkx.Append(&w.mapList, &rest.Widget)
			break
		}
		b := w.nodeRow(k, n.Size)
		gtkx.Append(&w.mapList, &b.Widget)
	}
	if len(n.Kids) == 0 {
		l := gtk.NewLabel(i18n.T("This folder is empty."))
		l.AddCssClass("empty")
		l.SetXalign(0)
		gtkx.Append(&w.mapList, &l.Widget)
	}
}

// nodeRow is a row of the list: its colour, its name and its size, its
// share of whole in the size's tooltip.
func (w *window) nodeRow(n *disk.Node, whole int64) *gtk.Button {
	b := gtk.NewButton()
	b.AddCssClass("row-button")
	b.AddCssClass("disk-row")
	box := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	sw := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	sw.AddCssClass("disk-swatch")
	sw.AddCssClass(kindClass(n))
	if n.Rest {
		sw.AddCssClass("rest")
	}
	sw.SetValign(gtk.AlignCenterValue)
	gtkx.Append(box, &sw.Widget)
	name := nodeName(n)
	if n.Dir {
		name += "/"
	}
	l := gtk.NewLabel(name)
	l.AddCssClass("label")
	l.SetXalign(0)
	l.SetHexpand(true)
	l.SetEllipsize(2) // middle
	if r := n.Rule; r != nil {
		l.SetTooltipText(i18n.T(r.What))
	}
	gtkx.Append(box, &l.Widget)
	size := gtk.NewLabel(disk.Size(n.Size))
	size.AddCssClass("readout")
	size.SetXalign(1)
	size.SetSizeRequest(64, -1)
	if whole > 0 {
		size.SetTooltipText(fmt.Sprintf("%.1f%%", 100*float64(n.Size)/float64(whole)))
	}
	gtkx.Append(box, &size.Widget)
	b.SetChild(&box.Widget)
	b.ConnectClicked(&w.onClick)
	w.nodeRows[b.GoPointer()] = n
	m := gtk.NewEventControllerMotion()
	m.ConnectEnter(&w.onRowMove)
	b.AddController(&m.EventController)
	w.rowHover[m.GoPointer()] = n
	return b
}

// --- a kind ---

var kindText = map[disk.Kind]string{
	disk.KindDev:   i18n.M("Dependencies, builds and toolchains."),
	disk.KindAgent: i18n.M("Agents' sessions, worktrees and AI models."),
	disk.KindCache: i18n.M("Caches, downloaded packages and the trash."),
}

var kindIcons = map[disk.Kind]string{
	disk.KindDev: "disk-dev-symbolic", disk.KindAgent: "disk-agent-symbolic", disk.KindCache: "disk-cache-symbolic",
}

func (w *window) showKind(k disk.Kind) {
	w.page, w.kind = "kind", k
	w.pages.SetVisibleChildName("kind")
	w.markSide()
	w.kindLabel.SetText("[" + strings.ToUpper(k.Name()) + "]")
	w.kindDesc.SetText(i18n.T(kindText[k]))
	gtkx.Clear(&w.kindRows)
	for p := range w.nodeRows {
		delete(w.nodeRows, p)
	}
	list := w.found(k)
	all, safe := disk.Total(list)
	w.kindMeta.SetText("")
	w.kindEmptyIcon.SetFromIconName(kindIcons[k])
	w.kindEmpty.SetVisible(len(list) == 0)
	switch {
	case !w.scanned():
		w.kindNone.SetText(i18n.T("Scan a place first."))
	case len(list) == 0:
		w.kindNone.SetText(i18n.T("Nothing found."))
	default:
		w.kindMeta.SetText(i18n.Nf("%s in %d folder · %s safe to delete", "%s in %d folders · %s safe to delete", len(list),
			disk.Size(all), len(list), disk.Size(safe)))
	}
	for i, n := range list {
		if i == kindMax {
			break
		}
		gtkx.Append(&w.kindRows, &w.kindRow(n).Widget)
	}
	if w.sel != nil && !slices.Contains(list, w.sel) {
		w.selectNode(nil)
	}
}

// kindRow is a row of a kind's page: what the folder is, where, whether
// it can go, and its size.
func (w *window) kindRow(n *disk.Node) *gtk.Button {
	b := gtk.NewButton()
	b.AddCssClass("row-button")
	b.AddCssClass("disk-row")
	box := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	text.SetHexpand(true)
	t := gtk.NewLabel(i18n.T(n.Rule.What))
	t.AddCssClass("label")
	t.SetXalign(0)
	gtkx.Append(text, &t.Widget)
	p := gtk.NewLabel(disk.Tilde(n.Path(), w.home))
	p.AddCssClass("sub")
	p.AddCssClass("disk-path")
	p.SetXalign(0)
	p.SetEllipsize(1) // start
	gtkx.Append(text, &p.Widget)
	gtkx.Append(box, &text.Widget)
	if !n.Rule.Group {
		tag := gtk.NewLabel(n.Rule.Verdict.Tag())
		tag.AddCssClass("disk-verdict-tag")
		tag.AddCssClass(verdictClass(n.Rule.Verdict))
		tag.SetValign(gtk.AlignCenterValue)
		gtkx.Append(box, &tag.Widget)
	}
	size := gtk.NewLabel(disk.Size(n.Size))
	size.AddCssClass("readout")
	size.SetXalign(1)
	size.SetSizeRequest(72, -1)
	gtkx.Append(box, &size.Widget)
	b.SetChild(&box.Widget)
	b.ConnectClicked(&w.onClick)
	w.nodeRows[b.GoPointer()] = n
	return b
}

func verdictClass(v disk.Verdict) string {
	switch v {
	case disk.Safe:
		return "safe"
	case disk.Review:
		return "review"
	case disk.Keep:
		return "keep"
	}
	return "unknown"
}

// showOnMap opens n's place on the map, its folder in view.
func (w *window) showOnMap(n *disk.Node) {
	if n == nil {
		return
	}
	root := n.Root()
	for path, s := range w.scans {
		if s.root != root {
			continue
		}
		w.showPlace(path)
		f := n
		if !n.Dir || len(n.Kids) == 0 {
			f = n.Parent
		}
		w.focus(f)
		w.selectNode(n)
		return
	}
}
