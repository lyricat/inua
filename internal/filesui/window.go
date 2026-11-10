package filesui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/i18n"
)

// window is one of the app's windows.
type window struct {
	app *App
	win gtk.ApplicationWindow

	sidebarReveal, detailsReveal, jobsReveal gtk.Revealer
	sidebar, sidebarCol, details             gtk.Box
	menu                                     *appui.MenuBar
	hiddenToggle, dualToggle, detailsToggle  gtk.ToggleButton
	paned                                    gtk.Paned
	jobLabel, jobRate                        gtk.Label
	jobBar                                   gtk.ProgressBar

	cardLayer, cardBox, cardList, cardButtons gtk.Box
	cardForm, cardAside                       gtk.Box
	cardTitle, cardText, cardError            gtk.Label
	cardEntry                                 gtk.Entry
	cardSecret                                gtk.PasswordEntry
	cardScroll                                gtk.ScrolledWindow
	cardCheck                                 gtk.CheckButton

	p      [2]*pane
	active int
	dual   bool
	owned  []uintptr // registered with the hub, forgotten when the window goes

	side     []sideItem // the sidebar's entries, for Alt+1 … Alt+9
	sideMenu *popMenu
	sidePtr  []uintptr
	card     *card
	jobs     jobs
	detGen   atomic.Uint64 // the details shown; a goroutine's results for another are dropped
	flashes  uint64
	closing  bool // asked to close while operations ran

	onRealize func(gtk.Widget)

	foot footer  // the picker's footer, hidden in Files' windows
	pick *picker // the request, when the window picks for an app
}

func (a *App) newWindow() *window {
	w := &window{app: a}
	ui := a.c.Load("files.ui")
	ui.Get("files", &w.win)
	ui.Get("sidebar_reveal", &w.sidebarReveal)
	ui.Get("details_reveal", &w.detailsReveal)
	ui.Get("jobs_reveal", &w.jobsReveal)
	ui.Get("sidebar", &w.sidebar)
	ui.Get("sidebar_col", &w.sidebarCol)
	ui.Get("details", &w.details)
	ui.Get("hidden_toggle", &w.hiddenToggle)
	ui.Get("dual_toggle", &w.dualToggle)
	ui.Get("details_toggle", &w.detailsToggle)
	ui.Get("panes", &w.paned)
	ui.Get("job_label", &w.jobLabel)
	ui.Get("job_rate", &w.jobRate)
	ui.Get("job_bar", &w.jobBar)
	ui.Get("card_layer", &w.cardLayer)
	ui.Get("card", &w.cardBox)
	ui.Get("card_list", &w.cardList)
	ui.Get("card_buttons", &w.cardButtons)
	ui.Get("card_title", &w.cardTitle)
	ui.Get("card_text", &w.cardText)
	ui.Get("card_form", &w.cardForm)
	ui.Get("card_aside", &w.cardAside)
	ui.Get("card_error", &w.cardError)
	ui.Get("card_entry", &w.cardEntry)
	ui.Get("card_secret", &w.cardSecret)
	ui.Get("card_scroll", &w.cardScroll)
	ui.Get("card_check", &w.cardCheck)
	var keysButton, jobCancel, sidebarHide gtk.Button
	ui.Get("keys_button", &keysButton)
	ui.Get("sidebar_hide", &sidebarHide)
	ui.Get("job_cancel", &jobCancel)
	f := &w.foot
	ui.Get("picker_foot", &f.foot)
	ui.Get("picker_name_row", &f.nameRow)
	ui.Get("picker_choices", &f.choices)
	ui.Get("picker_title", &f.title)
	ui.Get("picker_error", &f.err)
	ui.Get("picker_name", &f.name)
	ui.Get("picker_filter", &f.filter)
	ui.Get("picker_cancel", &f.cancel)
	ui.Get("picker_accept", &f.accept)
	ui.Close()

	// appui initializes only the first window. Files also creates windows
	// after all previous ones close, while the process is still resident.
	w.win.SetApplication(a.c.App)
	w.win.SetTitle(i18n.T("Files"))
	for _, c := range []string{"app", "settings", "files"} {
		w.win.AddCssClass(c)
	}
	a.wins = append(a.wins, w)
	if s := a.settings; s.Width > 0 && s.Height > 0 {
		w.win.SetDefaultSize(int32(s.Width), int32(s.Height))
	}

	w.click(&keysButton, func() { w.showKeys() })
	w.click(&sidebarHide, func() { w.setSidebar(false) })
	w.click(&jobCancel, func() { w.cancelJob() })
	w.toggle(&w.hiddenToggle, func(on bool) {
		if p := w.cur(); p.fv.Order.ShowsHidden != on {
			p.toggleHidden()
		}
	})
	w.toggle(&w.dualToggle, func(on bool) { w.setDual(on) })
	w.toggle(&w.detailsToggle, func(on bool) { w.setDetails(on) })
	w.enter(&w.cardEntry, func() { w.cardDefault() })
	w.edit(&w.cardEntry, func() { w.cardEdited() })
	w.enter(&w.cardSecret, func() { w.cardDefault() })

	w.p[0] = newPane(w, 0)
	w.p[1] = newPane(w, 1)
	w.paned.SetStartChild(&w.p[0].box.Widget)
	w.p[0].box.Unref()
	w.paned.SetEndChild(&w.p[1].box.Widget)
	w.p[1].box.Unref()
	w.p[1].box.SetVisible(false)
	w.setActive(w.p[0])

	w.own(w.app.h.key(&w.win.Widget, true, w.key))
	w.app.h.closing[w.win.GoPointer()] = w.closeRequested
	w.own(w.win.GoPointer())
	w.win.ConnectCloseRequest(&w.app.h.onClose)

	// The details follow the window's width (fitDetails).
	w.onRealize = func(gtk.Widget) {
		if sf := w.win.GetSurface(); sf != nil {
			w.notify(sf, "width", func() { w.fitDetails() })
			sf.Unref() // GetSurface takes a reference
		}
		w.fitDetails()
	}
	w.win.ConnectRealize(&w.onRealize)

	s := a.settings
	w.sidebarReveal.SetRevealChild(s.Sidebar)
	w.p[0].sidebarShow.SetVisible(!s.Sidebar)
	w.setDetails(s.Details)
	w.dualToggle.SetActive(s.Dual)
	w.sideMenu = newPopMenu(a.h, &w.sidebarCol.Widget)
	w.buildSidebar()
	w.menu = w.menuBar()
	w.menu.AppendLocal(&w.sidebarCol)
	return w
}

// --- the hub, with the window's registrations kept to forget them ---

func (w *window) own(ptr uintptr) { w.owned = append(w.owned, ptr) }

func (w *window) click(b *gtk.Button, f func()) {
	w.app.h.click(b, f)
	w.own(b.GoPointer())
}

func (w *window) toggle(b *gtk.ToggleButton, f func(bool)) {
	w.app.h.toggle(b, f)
	w.own(b.GoPointer())
}

func (w *window) enter(e gobject.Ptr, f func()) {
	w.app.h.enter(e, f)
	w.own(e.GoPointer())
}

func (w *window) edit(e gobject.Ptr, f func()) {
	w.app.h.edit(e, f)
	w.own(e.GoPointer())
}

func (w *window) notify(obj gobject.Ptr, property string, f func()) {
	w.app.h.notify(obj, property, f)
	w.own(obj.GoPointer())
}

func (w *window) scroll(v *gtk.Widget, f func(dy float64)) {
	w.own(w.app.h.ctrlScroll(v, f))
}

func (w *window) factory(setup, bind, teardown func(uintptr)) *gtk.SignalListItemFactory {
	f := w.app.h.factory(setup, bind, teardown)
	w.own(f.GoPointer())
	return f
}

func (w *window) activate(view uintptr, f func(uint32)) {
	w.app.h.activated[view] = f
	w.own(view)
}

func (w *window) press(v *gtk.Widget, capture bool, f func(uint32, int32, float64, float64)) {
	w.own(w.app.h.press(v, capture, f))
}

func (w *window) dragSource(v *gtk.Widget, f func(x, y float64) uintptr) {
	w.own(w.app.h.dragSource(v, f))
}

func (w *window) dropTarget(v *gtk.Widget, f func(paths []string, x, y float64, state gdk.ModifierType)) {
	w.own(w.app.h.dropTarget(v, func(val *gobject.Value, x, y float64, state gdk.ModifierType) bool {
		paths := droppedPaths(val)
		if len(paths) == 0 {
			return false
		}
		f(paths, x, y, state)
		return true
	}))
}

// closeRequested lets the window go, unless operations still run: then
// it hides until they end (the app stays, so they are not cut short).
func (w *window) closeRequested() bool {
	if w.jobs.running {
		w.closing = true
		w.win.SetVisible(false)
		return true
	}
	if w.pick != nil {
		w.pickClosed()
	} else {
		w.saveSize()
	}
	w.detGen.Add(1) // the details' reads still out find the window gone
	for _, p := range w.p {
		p.destroy()
	}
	w.app.h.forget(w.owned)
	w.app.h.forget(w.sidePtr)
	w.sideMenu.destroy()
	if w.menu != nil {
		w.menu.Close()
	}
	for _, p := range w.p {
		w.app.h.forget(p.crumbPtr)
	}
	w.app.closed(w)
	return false
}

func (w *window) saveSize() {
	var width, height int32
	w.win.GetDefaultSize(&width, &height)
	if gw, gh := w.win.GetWidth(), w.win.GetHeight(); gw > 0 && gh > 0 {
		width, height = gw, gh
	}
	if width > 0 && height > 0 && !w.win.IsMaximized() {
		w.app.settings.Width, w.app.settings.Height = int(width), int(height)
		w.app.settings.Touch()
	}
}

// --- panes ---

func (w *window) cur() *pane { return w.p[w.active] }

func (w *window) other() *pane {
	if !w.dual {
		return nil
	}
	return w.p[1-w.active]
}

func (w *window) panes() []*pane {
	if w.dual {
		return w.p[:]
	}
	return w.p[:1]
}

func (w *window) setActive(p *pane) {
	w.active = p.n
	// With two panes, a square at the start of each pane's path: filled
	// for the one the keys go to; the other's selection goes quiet.
	for _, q := range w.p {
		q.mark.SetVisible(w.dual)
		setClass(&q.mark.Widget, "on", q == p)
		setClass(&q.box.Widget, "inactive", q != p && w.dual)
	}
	w.hiddenToggle.SetActive(p.fv.Order.ShowsHidden)
	w.syncTitle()
	w.selectionChanged(p)
	w.markSidebar()
}

func (w *window) syncTitle() {
	if w.pick != nil { // what the app asks for
		return
	}
	p := w.cur()
	t := i18n.T("Files")
	switch {
	case p.mode == inTrash:
		t = i18n.T("Trash")
	case p.dir != "":
		t = filepath.Base(p.dir)
		if p.dir == files.Home() {
			t = i18n.T("Home")
		}
	}
	w.win.SetTitle(t)
}

// open shows a folder, or a remote disk, in the pane with the keys.
func (w *window) open(target string) {
	p := w.cur()
	if strings.HasPrefix(target, trashKey) {
		p.showTrash(p.dir != "")
		return
	}
	if !files.IsLocalURI(target) {
		p.goTo(files.Home(), false)
		w.connectRemote(p, target)
		return
	}
	p.goTo(target, p.dir != "")
	w.syncTitle()
	w.markSidebar()
}

// reveal opens the folder of paths with them selected.
func (w *window) reveal(paths []string) {
	if len(paths) == 0 {
		return
	}
	var names []string
	for _, p := range paths {
		names = append(names, filepath.Base(p))
	}
	w.cur().goTo(filepath.Dir(paths[0]), w.cur().dir != "", names...)
	w.syncTitle()
}

func (w *window) setDual(on bool) {
	if w.dual == on {
		return
	}
	w.dual = on
	w.dualToggle.SetActive(on)
	w.p[1].box.SetVisible(on)
	if on && w.p[1].dir == "" {
		w.p[1].goTo(w.p[0].dir, false)
	}
	if !on && w.active == 1 {
		w.setActive(w.p[0])
	}
	if on {
		if width := w.paned.GetWidth(); width > 0 {
			w.paned.SetPosition(width / 2)
		}
	}
	w.setActive(w.cur())
	for _, p := range w.p {
		p.syncColumns()
		p.rebind()
	}
	w.app.settings.Dual = on
	w.app.settings.Touch()
	w.fitDetails()
	w.cur().focus()
}

func (w *window) setDetails(on bool) {
	w.detailsToggle.SetActive(on)
	if w.pick != nil { // the picker's own
		go w.app.pickMem.SetDetails(on)
	} else if w.app.settings.Details != on {
		w.app.settings.Details = on
		w.app.settings.Touch()
	}
	if !w.fitDetails() && w.detailsReveal.GetRevealChild() {
		w.showDetails()
	}
}

// The widths fitDetails weighs: the sidebar's and the details' (files.blp),
// and the least each pane keeps beside the details.
const (
	sidebarWidth = 216
	detailsWidth = 300
	paneRoom     = 440
)

// fitDetails shows the details when they are on and the window has room
// for them beside the panes; a narrower window leaves them hidden, the
// toggle still on, until it widens again. It says whether they came or
// went.
func (w *window) fitDetails() bool {
	on := w.detailsToggle.GetActive()
	// The surface's width: the window's own is allocated only after.
	var width int32
	if sf := w.win.GetSurface(); sf != nil {
		width = sf.GetWidth()
		sf.Unref()
	}
	if on && width > 0 {
		need := detailsWidth + paneRoom
		if w.dual {
			need += paneRoom
		}
		if w.sidebarReveal.GetRevealChild() {
			need += sidebarWidth
		}
		on = int(width) >= need
	}
	if on == w.detailsReveal.GetRevealChild() {
		return false
	}
	w.detailsReveal.SetRevealChild(on)
	if on {
		w.showDetails()
	}
	return true
}

// setSidebar shows or hides the sidebar; its toggle moves to the left
// pane's head while it is hidden.
func (w *window) setSidebar(on bool) {
	w.sidebarReveal.SetRevealChild(on)
	w.p[0].sidebarShow.SetVisible(!on)
	w.app.settings.Sidebar = on
	w.app.settings.Touch()
	w.fitDetails()
}

// selectionChanged follows a pane's selection: the status line, and the
// details when they show that pane.
func (w *window) selectionChanged(p *pane) {
	p.updateStatus()
	p.syncPreview()
	w.pickSelectionChanged(p)
	if p == w.cur() && w.detailsReveal.GetRevealChild() {
		gen := w.detGen.Add(1)
		// After the selection settles: a rubber band changes it many times.
		w.app.h.later(60, func() {
			if gen == w.detGen.Load() {
				w.showDetails()
			}
		})
	}
}

// flash says something in the status line of the pane with the keys,
// for a few seconds.
func (w *window) flash(msg string) {
	w.flashes++
	n := w.flashes
	p := w.cur()
	p.flashing = true
	p.statusLabel.SetText(msg)
	p.statusLabel.AddCssClass("flash")
	w.app.h.later(4000, func() {
		if n == w.flashes {
			p.flashing = false
			p.statusLabel.RemoveCssClass("flash")
			p.updateStatus()
		}
	})
}

// --- keys ---

func (w *window) key(keyval uint32, state gdk.ModifierType) bool {
	ctrl := state&gdk.ControlMaskValue != 0
	shift := state&gdk.ShiftMaskValue != 0
	alt := state&gdk.AltMaskValue != 0
	k := gdk.KeyvalToLower(keyval)
	p := w.cur()

	if w.card != nil {
		return w.cardKey(keyval, state)
	}
	if w.pick != nil {
		if result, handled := w.pickKey(keyval, state); handled {
			return result
		}
	}
	switch p.pathStack.GetVisibleChildName() {
	case "entry":
		switch keyval {
		case uint32(gdk.KEY_Escape):
			p.pathStack.SetVisibleChildName("crumbs")
			p.focus()
			return true
		case uint32(gdk.KEY_Tab):
			p.completeLocation()
			return true
		}
		return false
	case "search":
		if keyval == uint32(gdk.KEY_Escape) {
			p.goTo(p.dir, false)
			p.focus()
			return true
		}
		if w.focusIn(&p.search.Widget) {
			if keyval == uint32(gdk.KEY_Down) {
				p.focus()
				return true
			}
			return false
		}
	}

	switch {
	case ctrl && alt:
		switch k {
		case 'n':
			w.do("new-file")
		case 't':
			w.do("terminal")
		default:
			return false
		}
		return true
	case ctrl && shift:
		switch k {
		case 'n':
			w.do("new-folder")
		case 'c':
			w.do("copy-path")
		case 'a':
			p.sel.UnselectAll()
		case 'i':
			w.invertSelection()
		case uint32(gdk.KEY_ISO_Left_Tab), uint32(gdk.KEY_Tab):
			p.stepTab(-1)
		case '?', '/':
			w.showKeys()
		case 'z':
			w.app.undo(w)
		default:
			return false
		}
		return true
	case ctrl:
		switch k {
		case 'l':
			p.editLocation()
		case 'f':
			p.startSearch()
		case 'p':
			w.openJump()
		case 'h':
			w.do("hidden")
		case '1':
			p.setView(files.ListView)
		case '2':
			p.setView(files.GridView)
		case '3':
			p.setView(files.ColumnView)
		case 'c':
			w.do("copy")
		case 'x':
			w.do("cut")
		case 'v':
			w.do("paste")
		case 'z':
			w.app.undo(w)
		case 'd':
			w.do("bookmark")
		case 'n':
			w.do("new-window")
		case 'r':
			w.do("reload")
		case 'i':
			w.setDetails(!w.detailsToggle.GetActive())
		case 't':
			p.newTab(p.locationKey(), true)
		case 'w':
			p.closeTab(p.tab)
		case 'q':
			w.win.Close()
		case uint32(gdk.KEY_Tab), uint32(gdk.KEY_Page_Down):
			p.stepTab(1)
		case uint32(gdk.KEY_Page_Up):
			p.stepTab(-1)
		case '+', '=', uint32(gdk.KEY_KP_Add):
			w.zoom(1)
		case '-', uint32(gdk.KEY_KP_Subtract):
			w.zoom(-1)
		case '0':
			w.zoom(0)
		case '?', '/':
			w.showKeys()
		default:
			return false
		}
		return true
	case alt:
		switch keyval {
		case uint32(gdk.KEY_Left):
			p.goBack()
		case uint32(gdk.KEY_Right):
			p.goForward()
		case uint32(gdk.KEY_Up):
			p.goUp()
		case uint32(gdk.KEY_Home):
			p.goTo(files.Home(), true)
		case uint32(gdk.KEY_Return), uint32(gdk.KEY_KP_Enter):
			w.setDetails(true)
		default:
			if k >= '1' && k <= '9' {
				w.sideJump(int(k - '1'))
				return true
			}
			return false
		}
		return true
	}

	switch keyval {
	case uint32(gdk.KEY_F2):
		w.do("rename")
	case uint32(gdk.KEY_F3):
		w.setDual(!w.dual)
	case uint32(gdk.KEY_F5):
		if w.dual {
			w.do("copy-other")
		} else {
			w.do("reload")
		}
	case uint32(gdk.KEY_F6):
		if !w.dual {
			return false
		}
		w.do("move-other")
	case uint32(gdk.KEY_F9):
		w.setSidebar(!w.sidebarReveal.GetRevealChild())
	case uint32(gdk.KEY_F10), uint32(gdk.KEY_Menu):
		if keyval == uint32(gdk.KEY_F10) && !shift {
			return false
		}
		p.menuAtSelection()
	case uint32(gdk.KEY_Delete), uint32(gdk.KEY_KP_Delete):
		if p.mode == inTrash || shift {
			w.do("delete")
		} else {
			w.do("trash")
		}
	case uint32(gdk.KEY_Tab), uint32(gdk.KEY_ISO_Left_Tab):
		if !w.dual {
			return false
		}
		w.setActive(w.p[1-w.active])
		w.cur().focus()
	case uint32(gdk.KEY_Left), uint32(gdk.KEY_Right):
		// In the columns: the column before, or into the folder selected.
		if !p.columnsShown() || !w.focusIn(&p.col.Widget) {
			return false
		}
		if keyval == uint32(gdk.KEY_Left) {
			p.goUp()
		} else {
			p.enterSelected()
		}
	case uint32(gdk.KEY_BackSpace):
		if p.filter != "" {
			r := []rune(p.filter)
			p.setFilter(string(r[:len(r)-1]))
		} else {
			p.goUp()
		}
	case uint32(gdk.KEY_Escape):
		switch {
		case p.filter != "":
			p.setFilter("")
		case p.mode == inSearch:
			p.goTo(p.dir, false)
		default:
			p.sel.UnselectAll()
		}
	case uint32(gdk.KEY_Return), uint32(gdk.KEY_KP_Enter):
		if shift {
			w.do("open-with")
			return true
		}
		if len(p.selected()) > 1 {
			w.do("open")
			return true
		}
		return false // the view activates the row with the cursor
	case uint32(gdk.KEY_space):
		if p.filter != "" {
			p.setFilter(p.filter + " ")
		} else {
			w.setDetails(!w.detailsToggle.GetActive())
		}
	default:
		r := rune(gdk.KeyvalToUnicode(keyval))
		if r < ' ' || r == 0x7f || shift && r == ' ' {
			return false
		}
		if p.mode == inSearch {
			p.search.GrabFocus()
			return false
		}
		p.setFilter(p.filter + string(r))
	}
	return true
}

// focusIn says whether the keyboard is in widget or below it.
func (w *window) focusIn(widget *gtk.Widget) bool {
	f := w.win.GetFocus()
	if f == nil {
		return false
	}
	defer f.Unref()
	return f.GoPointer() == widget.GoPointer() || f.IsAncestor(widget)
}

func (w *window) invertSelection() {
	p := w.cur()
	n := uint32(len(p.shown))
	bs, mask := gtk.NewBitsetEmpty(), gtk.NewBitsetRange(0, n)
	for i := range n {
		if !p.sel.IsSelected(i) {
			bs.Add(i)
		}
	}
	p.sel.SetSelection(bs, mask)
	bs.Unref()
	mask.Unref()
}

func (w *window) zoom(d int) {
	z := &w.app.settings.Zoom
	switch {
	case d == 0:
		*z = 0
	default:
		*z = max(-2, min(3, *z+d))
	}
	w.app.settings.Touch()
	for _, ww := range w.app.wins {
		for _, p := range ww.p {
			p.syncZoom()
		}
	}
	w.app.rebindAll()
}

// --- the actions, by name: keys, the menu and the sidebar's buttons ---

// item is a menu entry that runs the action id.
func (w *window) item(id, label, key string) menuItem {
	return menuItem{label: label, key: key, run: func() { w.do(id) }}
}

// menuItems are the menu's for what is selected in p.
func (w *window) menuItems(p *pane) []menuItem {
	if w.pick != nil {
		return w.pickMenuItems(p)
	}
	sel := p.selected()
	sep := menuItem{}
	if p.mode == inTrash {
		if len(sel) == 0 {
			return []menuItem{w.item("empty-trash", i18n.T("Empty Trash"), "")}
		}
		return []menuItem{
			w.item("restore", i18n.T("Restore"), ""),
			w.item("delete", i18n.T("Delete for good…"), "Del"),
			sep,
			w.item("empty-trash", i18n.T("Empty Trash"), ""),
		}
	}
	if len(sel) == 0 {
		items := []menuItem{
			w.item("new-folder", i18n.T("New folder…"), "Ctrl+Shift+N"),
			w.item("new-file", i18n.T("New file…"), "Ctrl+Alt+N"),
			w.item("paste", i18n.T("Paste"), "Ctrl+V"),
			sep,
			w.item("terminal", i18n.T("Terminal here"), "Ctrl+Alt+T"),
			w.item("tidy", i18n.T("Tidy with AI…"), ""),
			w.item("copy-path", i18n.T("Copy the folder's path"), "Ctrl+Shift+C"),
			w.item("bookmark", w.bookmarkLabel(p.dir), "Ctrl+D"),
			sep,
			w.item("hidden", i18n.T("Hidden files"), "Ctrl+H"),
			w.item("reload", i18n.T("Reload"), "Ctrl+R"),
		}
		return items
	}
	one := len(sel) == 1
	dir := one && p.all[sel[0]].Dir
	items := []menuItem{w.item("open", i18n.T("Open"), "Enter")}
	if one && !dir {
		items = append(items, w.item("open-with", i18n.T("Open with…"), "Shift+Enter"))
	}
	if dir {
		items = append(items, w.item("open-tab", i18n.T("Open in a new tab"), i18n.T("Middle click")))
		items = append(items, w.item("open-window", i18n.T("Open in a new window"), ""))
		if w.dual {
			items = append(items, w.item("open-other", i18n.T("Open in the other pane"), ""))
		}
	}
	if p.mode == inSearch {
		items = append(items, w.item("show-folder", i18n.T("Show in its folder"), ""))
	}
	items = append(items, sep,
		w.item("cut", i18n.T("Cut"), "Ctrl+X"),
		w.item("copy", i18n.T("Copy"), "Ctrl+C"),
		w.item("copy-path", i18n.T("Copy the path"), "Ctrl+Shift+C"),
	)
	if dir {
		items = append(items, w.item("paste-into", i18n.T("Paste into the folder"), ""),
			w.item("tidy-sel", i18n.T("Tidy with AI…"), ""))
	}
	items = append(items, sep)
	if allArchives(p.selectedPaths()) {
		items = append(items, w.item("extract", i18n.T("Extract here"), ""))
	}
	items = append(items, w.item("compress", i18n.T("Compress…"), ""))
	if w.dual {
		items = append(items, sep,
			w.item("copy-other", i18n.T("Copy to the other pane"), "F5"),
			w.item("move-other", i18n.T("Move to the other pane"), "F6"),
		)
	}
	items = append(items, sep)
	if one {
		items = append(items, w.item("rename", i18n.T("Rename…"), "F2"))
	} else {
		items = append(items, w.item("rename", i18n.Nf("Rename %d item…", "Rename %d items…", len(sel), len(sel)), "F2"))
	}
	items = append(items,
		w.item("trash", i18n.T("Move to Trash"), "Del"),
		w.item("delete", i18n.T("Delete for good…"), "Shift+Del"),
	)
	if dir {
		items = append(items, sep, w.item("bookmark-sel", w.bookmarkLabel(p.path(sel[0])), ""))
	}
	items = append(items, sep, w.item("properties", i18n.T("Details"), "Alt+Enter"))
	return items
}

func (w *window) bookmarkLabel(dir string) string {
	if files.Bookmarked(dir) {
		return i18n.T("Remove the bookmark")
	}
	return i18n.T("Bookmark")
}

// do runs an action by name on the pane with the keys.
func (w *window) do(id string) {
	p := w.cur()
	sel := p.selectedPaths()
	var fs []string // the files among them
	switch id {
	case "open":
		opened := false
		for _, i := range p.selected() {
			if p.all[i].Dir && !opened && p.mode != inTrash {
				p.goTo(p.path(i), true)
				opened = true
			} else if !p.all[i].Dir {
				fs = append(fs, p.path(i))
			}
		}
		if len(fs) > 0 {
			w.launch(fs, false)
		}
	case "open-with":
		for _, i := range p.selected() {
			if !p.all[i].Dir {
				fs = append(fs, p.path(i))
			}
		}
		if len(fs) > 0 {
			w.launch(fs, true)
		}
	case "open-tab":
		for _, path := range sel {
			if st, err := os.Stat(path); err == nil && st.IsDir() {
				p.newTab(path, false)
			}
		}
	case "open-window":
		w.app.ShowFoldersNow(sel[:1])
	case "open-other":
		if o := w.other(); o != nil && len(sel) > 0 {
			o.goTo(sel[0], true)
		}
	case "show-folder":
		if len(sel) > 0 {
			p.goTo(filepath.Dir(sel[0]), true, filepath.Base(sel[0]))
		}
	case "copy", "cut":
		if len(sel) == 0 || p.mode == inTrash {
			return
		}
		w.app.setClipboard(w, sel, id == "cut")
		if id == "cut" {
			w.flash(i18n.Nf("Cut %d item: paste to move it.", "Cut %d items: paste to move them.", len(sel), len(sel)))
		} else {
			w.flash(i18n.Nf("Copied %d item.", "Copied %d items.", len(sel), len(sel)))
		}
	case "copy-path":
		paths := sel
		if len(paths) == 0 {
			paths = []string{p.dir}
		}
		w.app.copyText(w, strings.Join(paths, "\n"))
		w.flash(i18n.Nf("Copied %d path.", "Copied %d paths.", len(paths), len(paths)))
	case "paste":
		if p.mode == inFolder {
			w.app.paste(w, p.dir)
		}
	case "paste-into":
		if len(sel) == 1 {
			w.app.paste(w, sel[0])
		}
	case "copy-other", "move-other":
		o := w.other()
		if o == nil || len(sel) == 0 || o.mode != inFolder || p.mode == inTrash {
			return
		}
		kind := files.Copy
		if id == "move-other" {
			kind = files.Move
		}
		w.app.run(w, files.Job{Kind: kind, Sources: sel, Dest: o.dir})
	case "compress":
		if len(sel) > 0 && p.mode != inTrash {
			w.askCompress(p, sel)
		}
	case "extract":
		if p.mode != inTrash && allArchives(sel) {
			w.extract(sel)
		}
	case "rename":
		switch {
		case p.mode == inTrash:
		case len(sel) == 1:
			w.askRename(p, sel[0])
		case len(sel) > 1:
			w.askRenameMany(p, sel)
		}
	case "trash":
		if len(sel) > 0 && p.mode != inTrash {
			w.app.run(w, files.Job{Kind: files.MoveToTrash, Sources: sel})
		}
	case "delete":
		if len(sel) > 0 {
			w.confirmDelete(p)
		}
	case "restore":
		var items []files.Trashed
		for _, i := range p.selected() {
			items = append(items, p.trash[i])
		}
		if len(items) > 0 {
			w.app.run(w, files.Job{Kind: files.Untrash, Trashed: items})
		}
	case "empty-trash":
		w.confirmEmptyTrash()
	case "new-folder", "new-file":
		if p.mode == inFolder {
			w.askNewName(p, id == "new-folder")
		}
	case "terminal":
		w.terminal(p.dir)
	case "tidy":
		if p.mode == inFolder {
			w.tidy(p.dir)
		}
	case "tidy-sel":
		if len(sel) == 1 {
			if fi, err := os.Stat(sel[0]); err == nil && fi.IsDir() {
				w.tidy(sel[0])
			}
		}
	case "bookmark", "bookmark-sel":
		dir := p.dir
		if id == "bookmark-sel" && len(sel) == 1 {
			dir = sel[0]
		}
		if p.mode != inFolder && id == "bookmark" {
			return
		}
		on, err := files.ToggleBookmark(dir)
		switch {
		case err != nil:
			w.flash(err.Error())
		case on:
			w.flash(i18n.Tf("Bookmarked %s.", filepath.Base(dir)))
		default:
			w.flash(i18n.Tf("Removed the bookmark of %s.", filepath.Base(dir)))
		}
		w.app.sidebarsChanged()
	case "hidden":
		p.toggleHidden()
		w.hiddenToggle.SetActive(p.fv.Order.ShowsHidden)
	case "reload":
		p.reload()
	case "new-window":
		dir := p.dir
		if dir == "" || p.mode == inTrash {
			dir = files.Home()
		}
		w.app.ShowFoldersNow([]string{dir})
	case "properties":
		w.setDetails(true)
	}
}

// copyFolderPath copies the path of the folder p shows.
func (w *window) copyFolderPath(p *pane) {
	if p.mode == inTrash {
		return
	}
	w.app.copyText(w, p.dir)
	w.flash(i18n.Tf("Copied %s.", shortHome(p.dir)))
}

// terminal opens a terminal in dir: the one chosen in inua's settings
// (Default Apps), started there; else xdg-terminal-exec, told the folder
// with --dir (a terminal that serves its windows from one process, as
// GNOME's, starts them where it runs, not where it is asked from), else
// $TERMINAL, else kitty with --directory.
func (w *window) terminal(dir string) {
	if dir == "" {
		dir = files.Home()
	}
	var try [][]string
	if c, _, _ := config.Load(config.DefaultPath()); len(c.TerminalCommand()) > 0 {
		try = append(try, c.TerminalCommand())
	}
	try = append(try, []string{"xdg-terminal-exec", "--dir=" + dir})
	if t := strings.Fields(os.Getenv("TERMINAL")); len(t) > 0 {
		try = append(try, t)
	}
	try = append(try, []string{"kitty", "--directory", dir})
	for _, argv := range try {
		if _, err := exec.LookPath(argv[0]); err != nil {
			continue
		}
		if err := desktop.Spawn(argv, dir); err == nil {
			return
		}
	}
	w.flash(i18n.T("No terminal was found: install xdg-terminal-exec or set $TERMINAL."))
}
