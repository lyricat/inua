package lightui

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/uikit"
	"github.com/lyricat/inua/internal/workshop"
)

// A workshop app (docs/feat/workshop.md): a program that describes its
// window in building blocks over JSON-RPC; this draws them in inua's look
// and tells the program what the user does. Widgets that are still in the
// tree after a render (same place, type and id) are kept, so inputs keep
// their focus and text, and lists their scroll.

// RunWorkshop runs `inua-apps workshop [--dev] ID|DIR`.
func RunWorkshop(args []string, uiDir string) int {
	dev := false
	args = slices.DeleteFunc(slices.Clone(args), func(a string) bool {
		if a == "--dev" {
			dev = true
			return true
		}
		return false
	})
	if len(args) == 0 && !dev { // the Workshop app: the apps installed
		return appui.RunView("workshop", appui.IDBase+".Workshop", i18n.M("Workshop"), &workshopApp{manager: true}, false, nil, uiDir)
	}
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: inua-apps workshop [[--dev] ID|DIR]")
		return 2
	}
	m, err := workshop.Find(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return appui.RunView("workshop", m.AppID(), m.Name, &workshopApp{m: m, dev: dev}, false, nil, uiDir)
}

// wsNode is a building block as drawn: its node, its outermost widget,
// where its children go, and the widgets its updates touch.
type wsNode struct {
	n        workshop.Node
	w        *gtk.Widget
	kids     *gtk.Box // children go here; nil for a leaf
	children []*wsNode

	title, sub, meta *gtk.Label
	icon             *gtk.Image
	button           *gtk.Button
	entry            *gtk.Entry
	pass             *gtk.PasswordEntry
	search           *gtk.SearchEntry
	text             *gtk.TextView
	buf              *gtk.TextBuffer
	sw               *gtk.Switch
	drop             *gtk.DropDown
	grid             *gtk.Grid
	bar              *wsNode  // a page's bar
	barBox           *gtk.Box // where it goes
	sig              uintptr  // the object whose signals name this input
	more             wsMore   // the blocks of workshop_blocks.go
}

type workshopApp struct {
	c   *appui.Ctx
	m   workshop.Manifest
	dev bool
	// manager is the Workshop app itself, drawn with the same blocks:
	// its events go to manage, not to a program.
	manager bool
	mg      workshopManager
	win     *gtk.ApplicationWindow

	holder   *gtk.Box // the page, or the starting or error page
	shown    *gtk.Widget
	root     *wsNode
	inputs   map[string]*wsNode  // by id
	bySig    map[uintptr]*wsNode // by the object that sends their signals
	filling  bool                // set while values are set from the app: no events
	sess     *workshop.Session
	events   chan func() // what goes to the app, in order
	gen      int         // counts sessions, so a gone one's news are dropped
	stopping bool
	clip     gtkx.SecretClip
	dialogs  map[*wsDialog]bool // open, with their callbacks

	onClick     func(gtk.Button)
	onRowClick  func(gtk.GestureClick, int32, float64, float64)
	onChanged   func(uintptr, uintptr)
	onActivate  func(gtk.Entry)
	onPassAct   func(gtk.PasswordEntry)
	onSearch    func(gtk.SearchEntry)
	onSearchAct func(gtk.SearchEntry)
	onSwitch    func(gtk.Switch, bool) bool
	onDrop      func(uintptr, uintptr, uintptr)
	onRestart   func(gtk.Button)
	onCloseReq  func(gtk.Window) bool
	cancelWatch context.CancelFunc

	fileDialog                               *wsFiles // open, for the app
	onFileOpened, onFileSaved, onFilesOpened gio.AsyncReadyCallback
}

func (a *workshopApp) ThemeChanged() {}

func (a *workshopApp) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.c = c
	a.win = gtk.NewApplicationWindow(c.App)
	a.win.SetDefaultSize(int32(a.m.Width), int32(a.m.Height))
	a.holder = gtk.NewBox(gtk.OrientationVerticalValue, 0)
	a.holder.SetVexpand(true)
	a.holder.SetHexpand(true)
	a.win.SetChild(&a.holder.Widget)
	a.inputs, a.bySig, a.dialogs = map[string]*wsNode{}, map[uintptr]*wsNode{}, map[*wsDialog]bool{}
	a.events = make(chan func(), 256)
	go func() {
		for f := range a.events {
			f()
		}
	}()

	a.onClick = func(b gtk.Button) { a.send("click", b.GetName(), nil) }
	a.onRowClick = func(g gtk.GestureClick, _ int32, _, _ float64) { a.send("click", g.GetWidget().GetName(), nil) }
	a.onChanged = func(ptr, _ uintptr) {
		if n := a.bySig[ptr]; n != nil {
			a.send("change", n.n.ID, a.value(n))
		}
	}
	a.onActivate = func(e gtk.Entry) { a.submit(e.GoPointer()) }
	a.onPassAct = func(e gtk.PasswordEntry) { a.submit(e.GoPointer()) }
	a.onSearch = func(e gtk.SearchEntry) {
		if n := a.bySig[e.GoPointer()]; n != nil {
			a.send("change", n.n.ID, a.value(n))
		}
	}
	a.onSearchAct = func(e gtk.SearchEntry) { a.submit(e.GoPointer()) }
	a.onSwitch = func(sw gtk.Switch, on bool) bool {
		if n := a.bySig[sw.GoPointer()]; n != nil {
			a.send("change", n.n.ID, on)
		}
		return false
	}
	a.onDrop = func(ptr, _, _ uintptr) {
		if n := a.bySig[ptr]; n != nil {
			a.send("change", n.n.ID, a.value(n))
		}
	}
	a.onRestart = func(gtk.Button) { a.start() }
	a.bindFileDialogs()
	a.onCloseReq = func(gtk.Window) bool {
		a.stopping = true
		if a.cancelWatch != nil {
			a.cancelWatch()
		}
		if s := a.sess; s != nil {
			s.Stop() // the app has its few seconds to save
		}
		return false
	}
	a.win.ConnectCloseRequest(&a.onCloseReq)

	if a.manager {
		a.manage()
		return a.win
	}
	if a.dev {
		ctx, cancel := context.WithCancel(context.Background())
		a.cancelWatch = cancel
		go workshop.Watch(ctx, a.m.Dir, 700*time.Millisecond, func() {
			gtkx.Post(func() {
				if m, err := workshop.Load(a.m.Dir); err == nil {
					a.m = m
				}
				log.Printf("workshop: a file changed; starting the app again")
				a.start()
			})
		})
	}
	a.start()
	menuWindow(c, a.win, []appui.MenuGroup{{Title: "View", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{{Name: "workshop-restart", Label: "Restart", Activate: func(gio.SimpleAction, uintptr) { a.start() }}}}}}})
	return a.win
}

// start runs the app's program, again if it ran, its window starting
// over.
func (a *workshopApp) start() {
	if old := a.sess; old != nil {
		go old.Stop()
	}
	a.gen++
	gen := a.gen
	a.root = nil
	a.inputs, a.bySig = map[string]*wsNode{}, map[uintptr]*wsNode{}
	a.show(a.notice("content-loading-symbolic", i18n.Tf("Starting %s…", a.m.Name), "", "", false))
	s := workshop.NewSession(a.m, wsHost{a, gen})
	if a.dev {
		s.Trace = os.Stderr
	}
	a.sess = s
	cfg, _, _ := config.Load(config.DefaultPath())
	go func() {
		err := s.Start(i18n.Lang(), cfg.Style.ForApps())
		if err != nil {
			_, logText := s.Why()
			gtkx.Post(func() {
				if gen == a.gen {
					a.failed(i18n.Tf("%s could not start", a.m.Name), err.Error(), logText)
				}
			})
			return
		}
		<-s.Exited()
		why, logText := s.Why()
		gtkx.Post(func() {
			if gen != a.gen || a.stopping {
				return
			}
			msg := i18n.T("It ended without an error.")
			if why != nil {
				msg = why.Error()
			}
			a.failed(i18n.Tf("%s stopped", a.m.Name), msg, logText)
		})
	}()
}

// failed shows why the app is not running, what it wrote to stderr, and
// a button to start it again.
func (a *workshopApp) failed(title, msg, logText string) {
	a.root = nil
	a.show(a.notice("dialog-warning-symbolic", title, msg, strings.TrimSpace(logText), true))
}

// notice is a page with an icon, a title, a line, the app's log and the
// restart button.
func (a *workshopApp) notice(icon, title, msg, logText string, restart bool) *gtk.Widget {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	box.AddCssClass("ws-notice")
	box.SetVexpand(true)
	box.SetValign(gtk.AlignCenterValue)
	img := gtk.NewImageFromIconName(icon)
	img.SetPixelSize(36)
	img.AddCssClass("ws-empty-icon")
	gtkx.Append(box, &img.Widget)
	t := gtk.NewLabel(title)
	t.AddCssClass("ws-empty-title")
	t.SetWrap(true)
	gtkx.Append(box, &t.Widget)
	if msg != "" {
		l := gtk.NewLabel(msg)
		l.AddCssClass("sub")
		l.SetWrap(true)
		l.SetSelectable(true)
		l.SetJustify(gtk.JustifyCenterValue)
		gtkx.Append(box, &l.Widget)
	}
	if logText != "" {
		sw := gtk.NewScrolledWindow()
		sw.SetPolicy(gtk.PolicyAutomaticValue, gtk.PolicyAutomaticValue)
		sw.SetMinContentHeight(60)
		sw.SetMaxContentHeight(220)
		sw.SetPropagateNaturalHeight(true)
		sw.AddCssClass("ws-log")
		l := gtk.NewLabel(logText)
		l.AddCssClass("ws-mono")
		l.SetXalign(0)
		l.SetYalign(0)
		l.SetSelectable(true)
		sw.SetChild(&l.Widget)
		gtkx.Append(box, &sw.Widget)
	}
	if restart {
		b := gtk.NewButtonWithLabel(i18n.T("Start again"))
		b.AddCssClass("outlined")
		b.SetHalign(gtk.AlignCenterValue)
		b.ConnectClicked(&a.onRestart)
		gtkx.Append(box, &b.Widget)
		if a.dev {
			n := gtk.NewLabel(i18n.T("It starts again by itself when a file of it changes."))
			n.AddCssClass("sub")
			gtkx.Append(box, &n.Widget)
		}
	}
	return &box.Widget
}

// show puts w in the window, instead of what was there.
func (a *workshopApp) show(w *gtk.Widget) {
	if a.shown != nil && a.shown.GoPointer() == w.GoPointer() {
		return
	}
	if a.shown != nil {
		a.holder.Remove(a.shown)
	}
	gtkx.Append(a.holder, w)
	a.shown = w
}

// send tells the app what the user did, with every input's value, in
// order, off the main loop.
func (a *workshopApp) send(kind, id string, value any) {
	if a.manager && !a.filling && id != "" {
		a.manageEvent(kind, id)
		return
	}
	if a.filling || a.sess == nil || id == "" {
		return
	}
	s, state := a.sess, a.state()
	a.events <- func() { s.Send(kind, id, value, state) }
}

func (a *workshopApp) submit(ptr uintptr) {
	if n := a.bySig[ptr]; n != nil {
		a.send("submit", n.n.ID, a.value(n))
	}
}

// value is an input's value as the app sees it.
func (a *workshopApp) value(n *wsNode) any {
	if v, ok := a.valueMore(n); ok {
		return v
	}
	switch {
	case n.entry != nil:
		return n.entry.GetText()
	case n.pass != nil:
		return n.pass.GetText()
	case n.search != nil:
		return n.search.GetText()
	case n.buf != nil:
		var start, end gtk.TextIter
		n.buf.GetBounds(&start, &end)
		return n.buf.GetText(&start, &end, true)
	case n.sw != nil:
		return n.sw.GetActive()
	case n.drop != nil:
		if i := int(n.drop.GetSelected()); i >= 0 && i < len(n.n.Options) {
			return n.n.Options[i]
		}
		return ""
	}
	return nil
}

func (a *workshopApp) state() map[string]any {
	out := map[string]any{}
	for id, n := range a.inputs {
		out[id] = a.value(n)
	}
	return out
}

// setValue sets an input as the app says, sending no event.
func (a *workshopApp) setValue(n *wsNode, v any) {
	a.filling = true
	defer func() { a.filling = false }()
	if a.setValueMore(n, v) {
		return
	}
	s, _ := v.(string)
	switch {
	case n.entry != nil:
		if n.entry.GetText() != s {
			n.entry.SetText(s)
		}
	case n.pass != nil:
		if n.pass.GetText() != s {
			n.pass.SetText(s)
		}
	case n.search != nil:
		if n.search.GetText() != s {
			n.search.SetText(s)
		}
	case n.buf != nil:
		if a.value(n) != s {
			n.buf.SetText(s, -1)
		}
	case n.sw != nil:
		on, _ := v.(bool)
		n.sw.SetActive(on)
	case n.drop != nil:
		if i := slices.Index(n.n.Options, s); i >= 0 {
			n.drop.SetSelected(uint32(i))
		}
	}
}

// render draws the app's tree, keeping what is still there.
func (a *workshopApp) render(root workshop.Node) {
	a.root = a.sync(a.root, root)
	a.inputs, a.bySig = map[string]*wsNode{}, map[uintptr]*wsNode{}
	a.index(a.root)
	a.show(a.root.w)
}

// index notes the tree's inputs, by id and by the object that sends
// their signals.
func (a *workshopApp) index(n *wsNode) {
	if n.sig != 0 {
		a.bySig[n.sig] = n
		if n.n.ID != "" {
			a.inputs[n.n.ID] = n
		}
	}
	for _, c := range n.children {
		a.index(c)
	}
	if n.bar != nil {
		a.index(n.bar)
	}
}

// sync makes n's widgets, or updates old's when it is the same block.
func (a *workshopApp) sync(old *wsNode, n workshop.Node) *wsNode {
	if old != nil && old.n.Type == n.Type && old.n.ID == n.ID && a.same(old.n, n) {
		a.update(old, n)
		return old
	}
	w := a.make(n)
	a.update(w, n)
	return w
}

// same says whether a block's widgets can show n: what they are made with
// did not change.
func (a *workshopApp) same(old, n workshop.Node) bool {
	switch n.Type {
	case "page":
		return (old.Scroll == nil) == (n.Scroll == nil) && (old.Scroll == nil || *old.Scroll == *n.Scroll)
	case "entry":
		return old.Password == n.Password
	case "dropdown":
		return slices.Equal(old.Options, n.Options)
	case "box":
		return old.Horizontal == n.Horizontal && old.Spacing == n.Spacing
	case "row":
		return (old.ID == "") == (n.ID == "")
	}
	return sameMore(old, n)
}

// syncKids brings a container's children to kids, keeping those at the
// same place that are the same block.
func (a *workshopApp) syncKids(p *wsNode, kids []workshop.Node) {
	var out []*wsNode
	var prev *gtk.Widget
	for i, k := range kids {
		var old *wsNode
		if i < len(p.children) {
			old = p.children[i]
		}
		w := a.sync(old, k)
		if w != old {
			if old != nil {
				p.kids.Remove(old.w)
			}
			if prev == nil {
				p.kids.InsertChildAfter(w.w, &gtk.Widget{})
			} else {
				p.kids.InsertChildAfter(w.w, prev)
			}
		}
		if p.n.Type == "columns" {
			w.w.SetHexpand(k.Width == 0)
			toggleClass(w.w, "ws-col-next", i > 0)
		}
		prev = w.w
		out = append(out, w)
	}
	for _, old := range p.children[min(len(kids), len(p.children)):] {
		p.kids.Remove(old.w)
	}
	p.children = out
}

func toggleClass(w *gtk.Widget, class string, on bool) {
	if on {
		w.AddCssClass(class)
	} else {
		w.RemoveCssClass(class)
	}
}

func label(class string) *gtk.Label {
	l := gtk.NewLabel("")
	if class != "" {
		l.AddCssClass(class)
	}
	l.SetXalign(0)
	return l
}

// make makes a block's widgets; update fills them.
func (a *workshopApp) make(n workshop.Node) *wsNode {
	w := &wsNode{n: workshop.Node{Type: n.Type, ID: n.ID}}
	vbox := func(class string, spacing int32) *gtk.Box {
		b := gtk.NewBox(gtk.OrientationVerticalValue, spacing)
		if class != "" {
			b.AddCssClass(class)
		}
		return b
	}
	switch n.Type {
	case "page":
		outer := vbox("ws-root", 0)
		outer.SetVexpand(true)
		w.kids = vbox("ws-page", 12)
		if n.Scroll == nil || *n.Scroll {
			sw := gtk.NewScrolledWindow()
			sw.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
			sw.SetVexpand(true)
			sw.SetChild(&w.kids.Widget)
			gtkx.Append(outer, &sw.Widget)
		} else {
			w.kids.SetVexpand(true)
			w.kids.AddCssClass("ws-fixed")
			gtkx.Append(outer, &w.kids.Widget)
		}
		w.barBox = vbox("", 0)
		gtkx.Append(outer, &w.barBox.Widget)
		w.w = &outer.Widget
	case "columns":
		w.kids = gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		w.kids.AddCssClass("ws-columns")
		w.kids.SetVexpand(true)
		w.w = &w.kids.Widget
	case "scroll":
		sw := gtk.NewScrolledWindow()
		sw.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
		sw.SetVexpand(true)
		w.kids = vbox("ws-scroll", 6)
		sw.SetChild(&w.kids.Widget)
		w.w = &sw.Widget
	case "box":
		o := gtk.OrientationVerticalValue
		if n.Horizontal {
			o = gtk.OrientationHorizontalValue
		}
		sp := n.Spacing
		if sp == 0 {
			sp = 8
		}
		w.kids = gtk.NewBox(o, int32(sp))
		w.w = &w.kids.Widget
	case "section":
		box := vbox("section", 0)
		head := uikit.NewHead("", "")
		w.title, w.meta = head.Label, head.Meta
		w.title.SetVisible(true)
		gtkx.Append(box, &head.Box.Widget)
		w.kids = vbox("section-body", 6)
		gtkx.Append(box, &w.kids.Widget)
		w.w = &box.Widget
	case "row":
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		row.AddCssClass("ws-row")
		w.icon = gtk.NewImageFromIconName("")
		w.icon.SetValign(gtk.AlignCenterValue)
		gtkx.Append(row, &w.icon.Widget)
		col := vbox("", 2)
		col.SetHexpand(true)
		col.SetValign(gtk.AlignCenterValue)
		w.title = label("title")
		w.title.SetEllipsize(3)
		gtkx.Append(col, &w.title.Widget)
		w.sub = label("sub")
		w.sub.SetWrap(true)
		gtkx.Append(col, &w.sub.Widget)
		gtkx.Append(row, &col.Widget)
		w.kids = gtk.NewBox(gtk.OrientationHorizontalValue, 6)
		w.kids.SetValign(gtk.AlignCenterValue)
		gtkx.Append(row, &w.kids.Widget)
		if n.ID != "" {
			row.AddCssClass("clickable")
			row.SetName(n.ID)
			g := gtk.NewGestureClick()
			g.ConnectReleased(&a.onRowClick)
			row.AddController(&g.EventController)
		}
		w.w = &row.Widget
	case "text":
		w.title = label("")
		w.title.SetWrap(true)
		w.title.SetWrapMode(2)
		w.w = &w.title.Widget
	case "facts":
		w.grid = gtk.NewGrid()
		w.grid.SetColumnSpacing(16)
		w.grid.SetRowSpacing(6)
		w.grid.AddCssClass("ws-facts")
		w.w = &w.grid.Widget
	case "button":
		w.button = gtk.NewButton()
		w.button.SetName(n.ID)
		w.button.SetValign(gtk.AlignCenterValue)
		w.button.ConnectClicked(&a.onClick)
		w.w = &w.button.Widget
	case "entry":
		if n.Password {
			w.pass = gtk.NewPasswordEntry()
			w.pass.SetShowPeekIcon(true)
			gtkx.ConnectChanged(w.pass, &a.onChanged)
			w.pass.ConnectActivate(&a.onPassAct)
			w.sig, w.w = w.pass.GoPointer(), &w.pass.Widget
		} else {
			w.entry = gtk.NewEntry()
			gtkx.ConnectChanged(w.entry, &a.onChanged)
			w.entry.ConnectActivate(&a.onActivate)
			w.sig, w.w = w.entry.GoPointer(), &w.entry.Widget
		}
	case "search":
		w.search = gtk.NewSearchEntry()
		w.search.ConnectSearchChanged(&a.onSearch)
		w.search.ConnectActivate(&a.onSearchAct)
		w.sig, w.w = w.search.GoPointer(), &w.search.Widget
	case "textarea":
		sw := gtk.NewScrolledWindow()
		sw.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
		sw.SetMinContentHeight(120)
		sw.AddCssClass("ws-textarea")
		w.text = gtk.NewTextView()
		w.text.SetWrapMode(gtk.WrapWordCharValue)
		w.text.SetTopMargin(8)
		w.text.SetBottomMargin(8)
		w.text.SetLeftMargin(10)
		w.text.SetRightMargin(10)
		sw.SetChild(&w.text.Widget)
		w.buf = w.text.GetBuffer()
		gtkx.ConnectChanged(w.buf, &a.onChanged)
		w.sig, w.w = w.buf.GoPointer(), &sw.Widget
	case "switch":
		w.sw = gtk.NewSwitch()
		w.sw.SetValign(gtk.AlignCenterValue)
		w.sw.ConnectStateSet(&a.onSwitch)
		w.sig, w.w = w.sw.GoPointer(), &w.sw.Widget
	case "dropdown":
		w.drop = gtk.NewDropDownFromStrings(n.Options)
		w.drop.SetValign(gtk.AlignCenterValue)
		gtkx.ConnectNotify(w.drop, "selected", &a.onDrop)
		w.sig, w.w = w.drop.GoPointer(), &w.drop.Widget
		w.n.Options = n.Options
	case "bar":
		bar := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		bar.AddCssClass("ws-bar")
		w.kids = gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		w.kids.SetHexpand(true)
		w.kids.SetHalign(gtk.AlignEndValue)
		gtkx.Append(bar, &w.kids.Widget)
		w.w = &bar.Widget
	case "empty":
		box := vbox("ws-empty", 8)
		box.SetVexpand(true)
		box.SetValign(gtk.AlignCenterValue)
		w.icon = gtk.NewImageFromIconName("")
		w.icon.SetPixelSize(36)
		w.icon.AddCssClass("ws-empty-icon")
		gtkx.Append(box, &w.icon.Widget)
		w.title = gtk.NewLabel("")
		w.title.AddCssClass("ws-empty-title")
		w.title.SetWrap(true)
		gtkx.Append(box, &w.title.Widget)
		w.sub = gtk.NewLabel("")
		w.sub.AddCssClass("sub")
		w.sub.SetWrap(true)
		w.sub.SetJustify(gtk.JustifyCenterValue)
		gtkx.Append(box, &w.sub.Widget)
		w.w = &box.Widget
	default:
		if a.makeMore(w, n) {
			break
		}
		w.title = label("warn")
		w.title.SetText(i18n.Tf("Unknown building block %q", n.Type))
		w.w = &w.title.Widget
	}
	return w
}

// update shows n in w's widgets.
func (a *workshopApp) update(w *wsNode, n workshop.Node) {
	prev := w.n
	w.n = n
	if n.Width > 0 {
		w.w.SetSizeRequest(int32(n.Width), -1)
	} else if prev.Width > 0 {
		w.w.SetSizeRequest(-1, -1)
	}
	if n.Type == "scroll" {
		// A column's look: the light apps' sidebar (the index column, on
		// the panel colour) or a panel at the right (as Viewer's details).
		toggleClass(w.w, "ws-sidebar", n.Style == "sidebar")
		toggleClass(w.w, "ws-panel", n.Style == "panel")
	}
	// An input's value is the app's when it is made and when the app
	// changes it; in between the user's typing stands.
	valueChanged := w.sig != 0 && len(n.Value) > 0 && string(n.Value) != string(prev.Value)
	switch n.Type {
	case "page":
		var kids []workshop.Node
		var bar *workshop.Node
		for i, k := range n.Children {
			if k.Type == "bar" && bar == nil {
				bar = &n.Children[i]
				continue
			}
			kids = append(kids, k)
		}
		a.syncKids(w, kids)
		switch {
		case bar == nil && w.bar != nil:
			w.barBox.Remove(w.bar.w)
			w.bar = nil
		case bar != nil:
			b := a.sync(w.bar, *bar)
			if b != w.bar {
				if w.bar != nil {
					w.barBox.Remove(w.bar.w)
				}
				gtkx.Append(w.barBox, b.w)
				w.bar = b
			}
		}
		return
	case "section":
		w.title.SetMarkup(uikit.Bracketed(strings.ToUpper(n.Title)))
		w.meta.SetText(n.Meta)
	case "row":
		w.title.SetText(n.Title)
		w.sub.SetText(n.Sub)
		w.sub.SetVisible(n.Sub != "")
		setBlockIcon(w.icon, n.Icon)
		toggleClass(w.w, "selected", n.Selected)
		w.w.SetName(n.ID)
	case "text":
		w.title.SetText(n.Text)
		for _, c := range []string{"ws-heading", "title", "sub", "ws-mono", "ws-hero", "readout", "kicker"} {
			w.w.RemoveCssClass(c)
		}
		switch n.Style {
		case "heading":
			w.w.AddCssClass("ws-heading")
		case "title", "sub", "readout", "kicker":
			w.w.AddCssClass(n.Style)
		case "mono":
			w.w.AddCssClass("ws-mono")
		case "hero":
			w.w.AddCssClass("ws-hero")
		}
		// Mono text is what one copies (a command, a path); a selectable
		// label would take the focus a removed button leaves, all selected.
		w.title.SetSelectable(n.Style == "mono")
	case "facts":
		clearGrid(w.grid)
		for i, item := range n.Items {
			if len(item) == 0 {
				continue
			}
			k := label("kicker")
			k.SetText(strings.ToUpper(item[0]))
			k.SetYalign(0)
			w.grid.Attach(&k.Widget, 0, int32(i), 1, 1)
			v := label("ws-fact")
			if len(item) > 1 {
				v.SetText(item[1])
			}
			v.SetWrap(true)
			v.SetWrapMode(2)
			v.SetSelectable(true)
			v.SetHexpand(true)
			w.grid.Attach(&v.Widget, 1, int32(i), 1, 1)
		}
	case "button":
		b := w.button
		switch {
		case n.Icon != "" && n.Label != "":
			box := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
			img := gtk.NewImageFromIconName(n.Icon)
			gtkx.Append(box, &img.Widget)
			gtkx.Append(box, &gtk.NewLabel(n.Label).Widget)
			b.SetChild(&box.Widget)
		case n.Icon != "":
			b.SetIconName(n.Icon)
		default:
			b.SetLabel(n.Label)
		}
		for _, c := range []string{"suggested-action", "destructive-action", "plain", "outlined"} {
			b.RemoveCssClass(c)
		}
		if c := map[string]string{"primary": "suggested-action", "danger": "destructive-action", "plain": "plain"}[n.Style]; c != "" {
			b.AddCssClass(c)
		} else {
			b.AddCssClass("outlined")
		}
		b.SetTooltipText(n.Sub)
		b.SetName(n.ID)
	case "entry":
		if w.entry != nil {
			w.entry.SetPlaceholderText(n.Placeholder)
		} else {
			var v gobject.Value
			v.Init(gobject.TypeStringVal)
			v.SetString(n.Placeholder)
			w.pass.SetProperty("placeholder-text", &v)
			v.Unset()
		}
		if valueChanged {
			a.setValue(w, n.String())
		}
	case "search":
		w.search.SetPlaceholderText(n.Placeholder)
		if valueChanged {
			a.setValue(w, n.String())
		}
	case "textarea":
		if valueChanged {
			a.setValue(w, n.String())
		}
	case "switch":
		if valueChanged {
			a.setValue(w, n.Bool())
		}
	case "dropdown":
		if valueChanged {
			a.setValue(w, n.String())
		}
	case "empty":
		setBlockIcon(w.icon, n.Icon)
		w.title.SetText(n.Title)
		w.sub.SetText(n.Text)
		w.sub.SetVisible(n.Text != "")
	default:
		a.updateMore(w, n, prev, valueChanged)
	}
	if w.kids != nil {
		a.syncKids(w, n.Children)
		a.afterKids(w)
	}
}

// setBlockIcon shows an icon by its name in inua's theme, or a file (an
// app's own icon, for the Workshop app); none hides it.
func setBlockIcon(img *gtk.Image, icon string) {
	switch {
	case icon == "":
	case strings.HasPrefix(icon, "/"):
		img.SetFromFile(icon)
		img.SetPixelSize(32) // an app's own icon, not a symbol
	default:
		img.SetFromIconName(icon)
	}
	img.SetVisible(icon != "")
}

// wsHost is the window as the session sees it; gen tells a session that
// was started again apart.
type wsHost struct {
	a   *workshopApp
	gen int
}

func (h wsHost) live() bool { return h.gen == h.a.gen }

func (h wsHost) Render(root workshop.Node) {
	gtkx.Post(func() {
		if h.live() {
			h.a.render(root)
		}
	})
}

func (h wsHost) SetValues(values map[string]any) {
	gtkx.Post(func() {
		if !h.live() {
			return
		}
		for id, v := range values {
			if n := h.a.inputs[id]; n != nil {
				h.a.setValue(n, v)
			}
		}
	})
}

func (h wsHost) State() map[string]any {
	st, err := gtkx.Call(2*time.Second, func() map[string]any { return h.a.state() })
	if err != nil {
		return map[string]any{}
	}
	return st
}

func (h wsHost) Copy(text string, secret bool) error {
	gtkx.Post(func() {
		if secret {
			h.a.clip.Copy(text)
			return
		}
		clip := h.a.win.GetClipboard()
		clip.SetText(text)
		clip.Unref()
	})
	return nil
}

func (h wsHost) SetTitle(title string) {
	gtkx.Post(func() { h.a.win.SetTitle(title) })
}

func (h wsHost) Confirm(ctx context.Context, title, text, ok string, danger bool) (bool, error) {
	answer := make(chan bool, 1)
	gtkx.Post(func() { h.a.confirm(title, text, ok, danger, answer) })
	select {
	case v := <-answer:
		return v, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// wsDialog is a question the app asked, with its callbacks.
type wsDialog struct {
	win      *gtk.Window
	answer   chan bool
	onButton func(gtk.Button)
	onClose  func(gtk.Window) bool
}

// confirm asks the user, over the app's window, and answers once.
func (a *workshopApp) confirm(title, text, ok string, danger bool, answer chan bool) {
	d := &wsDialog{win: gtk.NewWindow(), answer: answer}
	a.dialogs[d] = true
	done := func(v bool) {
		if !a.dialogs[d] {
			return
		}
		delete(a.dialogs, d)
		d.answer <- v
		d.win.Destroy()
	}
	d.onButton = func(b gtk.Button) { done(b.GetName() == "ok") }
	d.onClose = func(gtk.Window) bool { done(false); return false }
	d.win.SetTransientFor(&a.win.Window)
	d.win.SetModal(true)
	d.win.SetResizable(false)
	d.win.SetTitle(title)
	d.win.AddCssClass("app")
	d.win.AddCssClass("settings")
	d.win.ConnectCloseRequest(&d.onClose)
	box := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	box.AddCssClass("ws-dialog")
	box.SetSizeRequest(360, -1)
	t := label("ws-dialog-title")
	t.SetText(title)
	t.SetWrap(true)
	gtkx.Append(box, &t.Widget)
	if text != "" {
		l := label("sub")
		l.SetText(text)
		l.SetWrap(true)
		l.SetMaxWidthChars(44)
		gtkx.Append(box, &l.Widget)
	}
	buttons := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	buttons.SetHalign(gtk.AlignEndValue)
	buttons.SetMarginTop(8)
	cancel := gtk.NewButtonWithLabel(i18n.T("Cancel"))
	cancel.AddCssClass("outlined")
	cancel.SetName("cancel")
	cancel.ConnectClicked(&d.onButton)
	if ok == "" {
		ok = i18n.T("OK")
	}
	yes := gtk.NewButtonWithLabel(ok)
	yes.SetName("ok")
	yes.AddCssClass(map[bool]string{true: "destructive-action", false: "suggested-action"}[danger])
	yes.ConnectClicked(&d.onButton)
	gtkx.Append(buttons, &cancel.Widget)
	gtkx.Append(buttons, &yes.Widget)
	gtkx.Append(box, &buttons.Widget)
	d.win.SetChild(&box.Widget)
	d.win.Present()
	cancel.GrabFocus()
}
