package termui

import (
	"log"
	"math"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/agent"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/term/layout"
)

// Column 3 is the shown session's panes and nothing else: tiles laid out
// by the session's split tree, 8 px apart and from the edges, each a
// 32 px head (state, kind, title, folder or checkout) over its
// terminal. The focused tile's border is the accent; the gaps drag to
// resize.

// gap is the space between tiles and around them.
const gap = 8

// tile is one pane of the shown session.
type tile struct {
	id    int
	box   *gtk.Box
	mark  *gtk.Box
	icon  *gtk.Image
	title *gtk.Label
	dir   *gtk.Label
	state *gtk.Label
	head  *gtk.Box // the handle that drags the pane (panedrag.go)
	pane  *termPane
	// In a checkout, it shows in the folder's place: its name, the
	// branch's mark and the branch, as much of it as the head has room
	// for (fitGit).
	git       *gtk.Box
	gitName   *gtk.Label
	gitIcon   *gtk.Image
	gitBranch *gtk.Label
	gitNeed   [2]int // the tile's width for the whole checkout, for the branch alone
	gitShown  gitFit
	width     int // where it was put last
	onEnter   func(gtk.EventControllerMotion, float64, float64)
}

// gitFit is how much of the checkout a head shows.
type gitFit int

const (
	gitNone   gitFit = iota
	gitBranch        // the mark and the branch
	gitAll           // the name too
)

// What a head needs around its title and checkout (ui/style.css's
// .term-head padding and the tile's border; the head's spacing), the
// title's room kept before the checkout shows, and the longest name and
// branch shown whole.
const (
	headChrome     = 2*12 + 2
	headSpacing    = 9
	gitSpacing     = 5
	titleRoom      = 160
	gitNameChars   = 20
	gitBranchChars = 28
)

// tiling is column 3's layout: the tree laid out (a copy while a gap is
// dragged), and the drag.
type tiling struct {
	layout   *gtk.CustomLayout
	tree     *layout.Node
	zoom     int
	dragging *layout.Divider
	dragFrom float64
	// rects is where each tile was put last.
	rects map[int]layout.Rect
	// session is the session laid out; anim a zoom on its way, fit the
	// tiles on their way to a new layout.
	session int
	anim    zoomAnim
	fit     fitAnim
	ticking bool

	onMeasure                            gtk.CustomMeasureFunc
	onAllocate                           gtk.CustomAllocateFunc
	onMode                               gtk.CustomRequestModeFunc
	onDragBegin, onDragUpdate, onDragEnd func(gtk.GestureDrag, float64, float64)
	onMotion                             func(gtk.EventControllerMotion, float64, float64)
	onAnim                               glib.SourceFunc
}

// zoomAnim is a tile zooming: in, from its place to all of column 3
// while the others fade; out, back to its place while they come back.
type zoomAnim struct {
	id    int // the tile, 0 for none
	in    bool
	from  layout.Rect // in: its place before
	start time.Time
}

// fitAnim is the tiles going to the places a new layout gives them (a
// swap, a split, a pane gone, a preset): each from where it was, a new
// one fading in where it lands.
type fitAnim struct {
	from  map[int]layout.Rect // nil for none
	start time.Time
}

// zoomTime is how long a zoom takes, and a fit.
const zoomTime = 220 * time.Millisecond

func (a *App) buildTiling() {
	t := &a.tiling
	t.onMode = func(uintptr) gtk.SizeRequestMode { return gtk.SizeRequestConstantSizeValue }
	t.onMeasure = func(_ uintptr, _ gtk.Orientation, _ int32, min, nat, minBase, natBase *int32) {
		*min, *nat, *minBase, *natBase = 1, 1, -1, -1
	}
	t.onAllocate = func(_ uintptr, w, h, _ int32) { a.allocateTiles(int(w), int(h)) }
	t.onAnim = func(uintptr) bool {
		if t.anim.id != 0 && time.Since(t.anim.start) >= zoomTime {
			a.endZoom()
		}
		if t.fit.from != nil && time.Since(t.fit.start) >= zoomTime {
			a.endFit()
		}
		a.tilesW.QueueAllocate()
		t.ticking = t.anim.id != 0 || t.fit.from != nil
		return t.ticking
	}
	t.layout = gtk.NewCustomLayout(&t.onMode, &t.onMeasure, &t.onAllocate)
	a.tilesW.SetLayoutManager(&t.layout.LayoutManager)

	// Dragging a gap moves its split; the tiles' own gestures take the
	// drags that start on them.
	drag := gtk.NewGestureDrag()
	t.onDragBegin = func(g gtk.GestureDrag, x, y float64) {
		d := a.dividerAt(x, y)
		if d == nil {
			g.SetState(gtk.EventSequenceDeniedValue)
			return
		}
		t.dragging = d
		t.dragFrom = x
		if d.Node.Split == layout.Down {
			t.dragFrom = y
		}
	}
	t.onDragUpdate = func(_ gtk.GestureDrag, dx, dy float64) {
		d := t.dragging
		if d == nil {
			return
		}
		pos := t.dragFrom + dx
		if d.Node.Split == layout.Down {
			pos = t.dragFrom + dy
		}
		d.SetRatio(pos, gap)
		a.tilesW.QueueAllocate()
	}
	t.onDragEnd = func(gtk.GestureDrag, float64, float64) {
		d := t.dragging
		t.dragging = nil
		s := a.shown()
		if d == nil || s == nil {
			return
		}
		session, path, ratio := s.ID, d.Path, d.Node.Ratio
		a.ask("resize", func(c *host.Client) error {
			return c.Layout(session, host.OpRatio, host.Message{Path: path, Ratio: ratio})
		})
	}
	drag.ConnectDragBegin(&t.onDragBegin)
	drag.ConnectDragUpdate(&t.onDragUpdate)
	drag.ConnectDragEnd(&t.onDragEnd)
	a.tilesW.AddController(&drag.EventController)

	motion := gtk.NewEventControllerMotion()
	t.onMotion = func(_ gtk.EventControllerMotion, x, y float64) {
		cursor := ""
		if d := a.dividerAt(x, y); d != nil {
			cursor = "col-resize"
			if d.Node.Split == layout.Down {
				cursor = "row-resize"
			}
		}
		a.tilesW.SetCursorFromName(cursor)
	}
	motion.ConnectMotion(&t.onMotion)
	a.tilesW.AddController(&motion.EventController)
}

// area is where the tiles go in column 3.
func (a *App) area() layout.Rect {
	w, h := float64(a.tilesW.GetWidth()), float64(a.tilesW.GetHeight())
	return layout.Rect{X: gap, Y: gap, W: max(w-2*gap, 1), H: max(h-2*gap, 1)}
}

// dividerAt is the gap under x, y, a few pixels either side; nil for
// none.
func (a *App) dividerAt(x, y float64) *layout.Divider {
	t := &a.tiling
	if t.tree == nil || t.zoom != 0 {
		return nil
	}
	for _, d := range layout.Dividers(t.tree, a.area(), gap) {
		l := d.Line
		if x >= l.X-2 && x <= l.X+l.W+2 && y >= l.Y-2 && y <= l.Y+l.H+2 {
			return &d
		}
	}
	return nil
}

// allocateTiles places each tile where the tree puts it: the zoomed one
// over all of them.
func (a *App) allocateTiles(w, h int) {
	if a.wantFolds() != a.folds() {
		gtkx.Post(a.applyFolds) // not during the layout
	}
	t := &a.tiling
	r := layout.Rect{X: gap, Y: gap, W: max(float64(w)-2*gap, 1), H: max(float64(h)-2*gap, 1)}
	rects := layout.Rects(t.tree, r, gap)
	zoom := t.zoom
	if an := t.anim; an.id != 0 {
		// On its way: the tile between its place and all of column 3,
		// the others in their places, fading.
		p := easeOut(float64(time.Since(an.start)) / float64(zoomTime))
		from, to, shown := an.from, r, 1-p
		if !an.in {
			from, to, shown = r, rects[an.id], p
		}
		rects[an.id] = lerp(from, to, p)
		for id, tl := range a.tiles {
			if id != an.id {
				tl.box.SetOpacity(shown)
			}
		}
		zoom = 0
	} else if zoom != 0 {
		rects = map[int]layout.Rect{zoom: r}
	} else if t.fit.from != nil {
		p := easeOut(float64(time.Since(t.fit.start)) / float64(zoomTime))
		for id, to := range rects {
			if from, ok := t.fit.from[id]; ok {
				rects[id] = lerp(from, to, p)
			} else if tl := a.tiles[id]; tl != nil {
				tl.box.SetOpacity(p)
			}
		}
	}
	t.rects = rects
	for id, tl := range a.tiles {
		rr, ok := rects[id]
		if zoom != 0 {
			ok = id == zoom
			rr = r
		}
		tl.box.SetChildVisible(ok)
		if !ok {
			continue
		}
		rect := gdk.Rectangle{X: int32(rr.X), Y: int32(rr.Y), Width: int32(math.Max(rr.W, 1)), Height: int32(math.Max(rr.H, 1))}
		tl.box.SizeAllocate((*gtk.Allocation)(unsafe.Pointer(&rect)), -1)
		tl.width = int(rect.Width)
		if tl.gitFor(tl.width) != tl.gitShown {
			gtkx.Post(func() { // not during the layout
				if a.tiles[id] == tl {
					tl.fitGit()
				}
			})
		}
	}
}

// startZoom animates a zoom from pane from to pane to (0: none): in,
// from the tile's place; out, back to it. The tile's grid keeps its size
// on the way, and goes over the others. Without the desktop's
// animations, or from one zoomed pane to another, it is at once.
func (a *App) startZoom(from, to int) {
	t := &a.tiling
	a.endZoom()
	a.endFit()
	if !gtk.SettingsGetDefault().GetPropertyGtkEnableAnimations() {
		return
	}
	switch {
	case from == 0 && to != 0:
		r, ok := t.rects[to]
		if !ok || a.tiles[to] == nil {
			return
		}
		t.anim = zoomAnim{id: to, in: true, from: r}
	case from != 0 && to == 0 && a.tiles[from] != nil:
		t.anim = zoomAnim{id: from}
	default:
		return
	}
	t.anim.start = time.Now()
	tl := a.tiles[t.anim.id]
	tl.pane.sizeHeld = true
	if last := a.tilesW.GetLastChild(); last != nil && last.GoPointer() != tl.box.GoPointer() {
		a.tilesW.ReorderChildAfter(&tl.box.Widget, last)
	}
	if !t.ticking {
		t.ticking = true
		glibTimeout(16, &t.onAnim)
	}
}

// endZoom ends a zoom's animation where it goes: the grid takes the
// tile's size, the others show or hide.
func (a *App) endZoom() {
	t := &a.tiling
	if t.anim.id == 0 {
		return
	}
	if tl := a.tiles[t.anim.id]; tl != nil {
		tl.pane.sizeHeld = false
		// The last frame put the tile where it ends already, or a pixel
		// off: allocated the same size again, GTK would leave its
		// widgets be, and the grid would keep the size it was held at.
		tl.pane.widget.QueueAllocate()
	}
	for _, tl := range a.tiles {
		tl.box.SetOpacity(1)
	}
	t.anim = zoomAnim{}
	a.tilesW.QueueAllocate()
}

// startFit animates the tiles to the places tree gives them, from where
// they are, when any moves; their grids keep their sizes on the way.
func (a *App) startFit(tree *layout.Node) {
	t := &a.tiling
	to := layout.Rects(tree, a.area(), gap)
	moved := false
	for id, r := range to {
		if f, ok := t.rects[id]; ok && math.Max(math.Max(math.Abs(f.X-r.X), math.Abs(f.Y-r.Y)), math.Max(math.Abs(f.W-r.W), math.Abs(f.H-r.H))) > 0.5 {
			moved = true
		}
	}
	if !moved || !gtk.SettingsGetDefault().GetPropertyGtkEnableAnimations() {
		return
	}
	from := map[int]layout.Rect{}
	for id, r := range t.rects {
		from[id] = r
	}
	a.endFit()
	t.fit = fitAnim{from: from, start: time.Now()}
	for id := range from {
		if tl := a.tiles[id]; tl != nil {
			tl.pane.sizeHeld = true
		}
	}
	if !t.ticking {
		t.ticking = true
		glibTimeout(16, &t.onAnim)
	}
}

// endFit ends the tiles' way to a new layout where it goes: each grid
// takes its tile's size (allocated anew: see endZoom).
func (a *App) endFit() {
	t := &a.tiling
	if t.fit.from == nil {
		return
	}
	for id := range t.fit.from {
		if tl := a.tiles[id]; tl != nil {
			tl.pane.sizeHeld = false
			tl.pane.widget.QueueAllocate()
		}
	}
	for _, tl := range a.tiles {
		tl.box.SetOpacity(1)
	}
	t.fit = fitAnim{}
	a.tilesW.QueueAllocate()
}

// easeOut slows to a stop: x from 0 to 1 (clamped), cubic.
func easeOut(x float64) float64 {
	x = min(max(x, 0), 1)
	return 1 - math.Pow(1-x, 3)
}

// lerp is the rect p of the way from a to b.
func lerp(a, b layout.Rect, p float64) layout.Rect {
	mix := func(x, y float64) float64 { return x + (y-x)*p }
	return layout.Rect{X: mix(a.X, b.X), Y: mix(a.Y, b.Y), W: mix(a.W, b.W), H: mix(a.H, b.H)}
}

// syncTiles makes the shown session's tiles: new panes get one, attached
// to the host; panes gone or no longer shown lose theirs.
func (a *App) syncTiles() {
	s := a.shown()
	want := map[int]bool{}
	if s != nil {
		for _, id := range s.Tree.Panes() {
			want[id] = true
		}
		switch {
		case s.ID != a.tiling.session:
			a.endZoom()
			a.endFit()
		case s.Zoom != a.tiling.zoom:
			a.startZoom(a.tiling.zoom, s.Zoom)
		case s.Zoom == 0 && a.tiling.dragging == nil:
			a.startFit(s.Tree)
		}
		a.tiling.session = s.ID
		a.tiling.tree, a.tiling.zoom = s.Tree.Clone(), s.Zoom
	} else {
		a.endZoom()
		a.endFit()
		a.tiling.session, a.tiling.tree, a.tiling.zoom = 0, nil, 0
	}
	for id := range a.tiles {
		if !want[id] {
			a.dropTile(id)
		}
	}
	for id := range want {
		if a.tiles[id] == nil {
			if err := a.addTile(id); err != nil {
				log.Printf("term: pane %d: %v", id, err)
			}
		}
	}
	for id, t := range a.tiles {
		t.update(a, a.info[id])
		if id == s.Focus {
			t.box.AddCssClass("focused")
		} else {
			t.box.RemoveCssClass("focused")
		}
	}
	a.tilesW.QueueAllocate()
	if s != nil {
		if t := a.tiles[s.Focus]; t != nil && a.sheet == nil && !t.pane.widget.HasFocus() && !t.pane.busy() {
			t.pane.widget.GrabFocus()
		}
	}
}

// addTile makes a pane's tile and attaches it: its replay comes first.
func (a *App) addTile(id int) error {
	info := a.info[id]
	p, err := newTermPane(paneBackend{a, id}, max(info.Cols, 2), max(info.Rows, 1), a.scheme(), a.c.Colors["warn"], a.look())
	if err != nil {
		return err
	}
	p.id = id
	p.chord = a.chord
	p.focused = func() { a.focusPane(id) }
	t := &tile{id: id, pane: p}
	t.box = gtk.NewBox(gtk.OrientationVerticalValue, 0)
	t.box.AddCssClass("term-tile")
	t.box.SetOverflow(gtk.OverflowHiddenValue) // a held grid, in a smaller tile
	t.box.SetCursorFromName("default")
	head := gtk.NewBox(gtk.OrientationHorizontalValue, 9)
	head.AddCssClass("term-head")
	t.mark = gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	t.mark.AddCssClass("mark")
	t.mark.SetValign(gtk.AlignCenterValue)
	head.Append(&t.mark.Widget)
	t.icon = gtk.NewImageFromIconName(a.iconOf(info))
	t.icon.SetPixelSize(14)
	t.icon.AddCssClass("term-kind")
	head.Append(&t.icon.Widget)
	t.title = gtk.NewLabel("")
	t.title.AddCssClass("term-head-title")
	t.title.SetEllipsize(3) // end
	t.title.SetXalign(0)
	t.title.SetHexpand(true)
	head.Append(&t.title.Widget)
	t.dir = gtk.NewLabel("")
	t.dir.AddCssClass("term-head-dir")
	t.dir.SetEllipsize(1) // start
	head.Append(&t.dir.Widget)
	t.git = gtk.NewBox(gtk.OrientationHorizontalValue, gitSpacing)
	t.git.AddCssClass("term-head-git")
	t.git.SetVisible(false)
	t.gitName = gtk.NewLabel("")
	t.gitName.AddCssClass("term-head-dir")
	t.gitName.SetEllipsize(3) // end
	t.gitName.SetMaxWidthChars(gitNameChars)
	t.git.Append(&t.gitName.Widget)
	t.gitIcon = gtk.NewImageFromIconName("term-git-branch-symbolic")
	t.gitIcon.SetPixelSize(12)
	t.git.Append(&t.gitIcon.Widget)
	t.gitBranch = gtk.NewLabel("")
	t.gitBranch.AddCssClass("term-head-branch")
	t.gitBranch.SetEllipsize(3) // end
	t.gitBranch.SetMaxWidthChars(gitBranchChars)
	t.git.Append(&t.gitBranch.Widget)
	head.Append(&t.git.Widget)
	t.state = gtk.NewLabel("")
	t.state.AddCssClass("term-state")
	head.Append(&t.state.Widget)
	t.box.Append(&head.Widget)
	t.box.Append(&p.root.Widget)
	t.head = head
	a.dragHead(head, id)
	motion := gtk.NewEventControllerMotion()
	t.onEnter = func(c gtk.EventControllerMotion, _, _ float64) {
		if !a.set.Terminal.FocusFollowsMouse || !a.win.IsActive() || a.client == nil ||
			a.tiles[id] != t || p.closed || p.exited || !t.box.GetMapped() || !t.box.GetChildVisible() {
			return
		}
		select {
		case <-a.client.Done():
			return
		default:
		}
		s := a.shown()
		if s == nil || !s.Tree.Has(id) || (s.Zoom != 0 && s.Zoom != id) {
			return
		}
		if _, ok := a.info[id]; !ok {
			return
		}
		// A sheet, search or menu keeps the keys; dragging never changes
		// the receiving pane, even when the pointer crosses a tile.
		if a.sheet != nil || a.closePending || a.cols.headMenu.pop != nil ||
			a.paneDrag.id != 0 || a.cols.drag.id != 0 || a.tiling.dragging != nil {
			return
		}
		if focus := a.win.GetFocus(); focus != nil {
			defer focus.Unref()
			if focus.GoPointer() == a.cols.find.GoPointer() || focus.IsAncestor(&a.cols.find.Widget) {
				return
			}
			if pop := focus.GetAncestor(gtk.PopoverGLibType()); pop != nil {
				pop.Unref()
				return
			}
		}
		const buttons = gdk.Button1MaskValue | gdk.Button2MaskValue | gdk.Button3MaskValue | gdk.Button4MaskValue | gdk.Button5MaskValue
		if c.GetCurrentEventState()&buttons != 0 {
			return
		}
		for _, other := range a.tiles {
			if q := other.pane; q.busy() || q.selecting || q.barDrag || q.buttons != 0 || q.copy != nil {
				return
			}
		}
		// Only entry requests focus, so keyboard focus survives motion
		// within this tile. The focus controller synchronizes the host.
		if !p.widget.HasFocus() {
			p.widget.GrabFocus()
		}
	}
	motion.ConnectEnter(&t.onEnter)
	t.box.AddController(&motion.EventController)
	t.box.SetParent(&a.tilesW.Widget)
	a.tiles[id] = t
	a.mu.Lock()
	a.live[id] = p
	a.mu.Unlock()
	t.update(a, info)
	a.ask("attach", func(c *host.Client) error { return c.Attach(id) })
	return nil
}

// dropTile detaches a pane and forgets its tile; the pane runs on.
func (a *App) dropTile(id int) {
	t := a.tiles[id]
	if t == nil {
		return
	}
	delete(a.tiles, id)
	a.mu.Lock()
	delete(a.live, id)
	a.mu.Unlock()
	if _, alive := a.info[id]; alive { // a closed pane is gone already
		a.ask("detach", func(c *host.Client) error { return c.Detach(id) })
	}
	t.pane.close()
	t.box.Unparent()
	a.freeLater(append(t.pane.callbacks(), &t.onEnter))
}

// The bindings turn each Go callback GTK calls into one of a few thousand
// the process has, and free none themselves: a pane's dozens, made anew
// for each tile, ran them out after some forty switches of session. A
// closed pane's are freed a second on, when an event or a clipboard read
// under way as it closed is over; so are the settings window's.

// gone are the callbacks of a pane or window closed, waiting to be freed.
type gone struct {
	cbs []any
	at  time.Time
}

// freeLater frees the callbacks of a pane or window closed in a second.
func (a *App) freeLater(cbs []any) {
	a.gone = append(a.gone, gone{cbs, time.Now()})
	if len(a.gone) == 1 {
		glibTimeout(1000, &a.onFree)
	}
}

// freeGone frees the callbacks of what closed a second ago or more; true
// while others wait.
func (a *App) freeGone() bool {
	keep := a.gone[:0]
	for _, g := range a.gone {
		if time.Since(g.at) < time.Second {
			keep = append(keep, g)
			continue
		}
		for _, cb := range g.cbs {
			glib.UnrefCallback(cb) // one never connected: nothing to free
		}
	}
	clear(a.gone[len(keep):])
	a.gone = keep
	return len(a.gone) > 0
}

// update writes a pane's head.
func (t *tile) update(a *App, info host.PaneInfo) {
	title := paneTitle(info)
	t.title.SetText(title)
	t.dir.SetText(shortDir(info.Dir))
	t.icon.SetFromIconName(a.iconOf(info))
	a.brand(t.icon, info)
	paint(t.mark, t.state, lookOf(info))
	if r, ok := a.tiling.rects[t.id]; ok && t.width == 0 {
		t.width = int(r.W)
	}
	t.showGit(info.Git, title)
}

// showGit writes the checkout in the head, in the folder's place, and
// what it needs to show whole or in part.
func (t *tile) showGit(g *host.Git, title string) {
	t.dir.SetVisible(g == nil)
	if g == nil {
		t.gitNeed = [2]int{}
		t.fitGit()
		return
	}
	name := baseName(g.Root)
	t.gitName.SetText(name)
	t.gitBranch.SetText(g.Branch)
	icon, tip := "term-git-branch-symbolic", i18n.Tf("%s\nBranch %s", shortDir(g.Root), g.Branch)
	if g.Worktree {
		icon, tip = "term-git-worktree-symbolic", i18n.Tf("%s, a worktree of %s\nBranch %s", shortDir(g.Root), shortDir(g.Repo), g.Branch)
	}
	t.gitIcon.SetFromIconName(icon)
	t.git.SetTooltipText(tip)
	// Measured from the text: a hidden widget measures nothing.
	fixed := headChrome + 4*headSpacing + natural(&t.mark.Widget) + natural(&t.icon.Widget) + natural(&t.state.Widget) +
		min(textWidth(&t.title.Widget, title), titleRoom)
	branch := 12 + gitSpacing + textWidth(&t.gitBranch.Widget, clip(g.Branch, gitBranchChars))
	t.gitNeed = [2]int{fixed + textWidth(&t.gitName.Widget, clip(name, gitNameChars)) + gitSpacing + branch, fixed + branch}
	t.fitGit()
}

// gitFor is how much of the checkout a tile width wide shows.
func (t *tile) gitFor(width int) gitFit {
	switch {
	case t.gitNeed[0] == 0:
		return gitNone
	case width >= t.gitNeed[0]:
		return gitAll
	case width >= t.gitNeed[1]:
		return gitBranch
	}
	return gitNone
}

// fitGit shows as much of the checkout as the tile has room for.
func (t *tile) fitGit() {
	f := t.gitFor(t.width)
	t.gitShown = f
	t.git.SetVisible(f != gitNone)
	t.gitName.SetVisible(f == gitAll)
}

// natural is a widget's natural width.
func natural(w *gtk.Widget) int {
	var minimum, nat, minBase, natBase int32
	w.Measure(gtk.OrientationHorizontalValue, -1, &minimum, &nat, &minBase, &natBase)
	return int(nat)
}

// textWidth is the width of text in a widget's font.
func textWidth(w *gtk.Widget, text string) int {
	l := w.CreatePangoLayout(text)
	var x, y int32
	l.GetPixelSize(&x, &y)
	l.Unref()
	return int(x)
}

// clip is text as a label n characters at most shows it.
func clip(text string, n int) string {
	if r := []rune(text); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return text
}

// focusPane tells the host which pane has the focus, when the user moved
// it here.
func (a *App) focusPane(id int) {
	s := a.shown()
	if s == nil || s.Focus == id {
		return
	}
	for tid, t := range a.tiles {
		if tid == id {
			t.box.AddCssClass("focused")
		} else {
			t.box.RemoveCssClass("focused")
		}
	}
	s.Focus = id
	session := s.ID
	a.ask("focus", func(c *host.Client) error {
		return c.Layout(session, host.OpFocus, host.Message{Pane: id})
	})
}

// paneTitle is what a pane's head and row call it: an agent's title
// without its spinner, a shell's command running, else the title the
// program set, else the program.
func paneTitle(info host.PaneInfo) string {
	if info.Agent != "" {
		if t, _ := agent.Title(info.Title); t != "" {
			return t
		}
		return info.AgentName
	}
	if info.Command != "" {
		return info.Command
	}
	if info.Title != "" {
		return info.Title
	}
	if len(info.Argv) > 0 {
		return baseName(info.Argv[0])
	}
	return i18n.T("Terminal")
}
