package termui

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gobject/types"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/term/model"
)

// A session lifts as one block; only its header starts the drag. The
// margins open a gap with the same motion as workspace rows (wsdrag.go).
type sessionDrag struct {
	id, workspace, from int
	drag                uintptr
	source              *gtk.DragSource
	rows                []int
	gaps                []wsGap
	slot                float64
	hotX, hotY          int32
	ticking             bool
	// pending keeps the drop visible until the host has sent its state,
	// including on rejection. The authoritative model is never edited here.
	pending   *sessionMove
	onPrepare func(gtk.DragSource, float64, float64) gdk.ContentProvider
	onBegin   func(gtk.DragSource, uintptr)
	onEnd     func(gtk.DragSource, uintptr, bool)
	onAccept  func(gtk.DropTarget, uintptr) bool
	onMotion  func(gtk.DropTarget, float64, float64) gdk.DragAction
	onLeave   func(gtk.DropTarget)
	onDrop    func(gtk.DropTarget, uintptr, float64, float64) bool
	onTick    glib.SourceFunc
}

type sessionMove struct{ workspace, session, before int }

// preview copies only the ordering, leaving the host's state available
// for reconciliation and the panes, focus and processes untouched.
func (d *sessionDrag) preview(w *model.Workspace) *model.Workspace {
	if w == nil || d.pending == nil || d.pending.workspace != w.ID {
		return w
	}
	copy := *w
	copy.Sessions = slices.Clone(w.Sessions)
	m := model.Model{Workspaces: []*model.Workspace{&copy}}
	m.MoveSession(w.ID, d.pending.session, d.pending.before)
	return &copy
}

func (c *columns) startSessionDrag(a *App) {
	d := &c.sessionDrag
	eligible := func() bool {
		return a.client != nil && d.pending == nil && len(d.rows) > 1 && strings.TrimSpace(c.find.GetText()) == ""
	}
	d.onPrepare = func(ds gtk.DragSource, x, y float64) gdk.ContentProvider {
		if !eligible() {
			return gdk.ContentProvider{}
		}
		w := ds.GetWidget()
		if w == nil {
			return gdk.ContentProvider{}
		}
		defer w.Unref()
		spot, ok := c.clickAt[w.GoPointer()]
		if !ok || spot.pane != 0 {
			return gdk.ContentProvider{}
		}
		d.hotX, d.hotY = int32(x), int32(y)
		var v gobject.Value
		v.Init(gobject.TypeStringVal)
		v.SetString("session:" + strconv.Itoa(spot.session))
		content := gdk.NewContentProviderForValue(&v)
		v.Unset()
		return *content // prepare transfers the reference to GTK
	}
	d.onBegin = func(ds gtk.DragSource, drag uintptr) {
		w := ds.GetWidget()
		if w == nil {
			return
		}
		defer w.Unref()
		id := c.clickAt[w.GoPointer()].session
		from := slices.Index(d.rows, id)
		ws := a.shownWorkspace()
		if !eligible() || ws == nil || from < 0 {
			ds.DragCancel()
			return
		}
		d.id, d.workspace, d.from, d.drag, d.source = id, ws.ID, from, drag, &ds
		c.lastHead = 0 // a drag is not the first click of a rename
		block := c.heads[id].section
		d.slot = float64(block.GetHeight()) + 14 // .term-session's bottom margin
		live := gtk.NewWidgetPaintable(&block.Widget)
		still := live.GetCurrentImage()
		live.Unref()
		ds.SetIcon(still, d.hotX, d.hotY)
		gobject.ObjectNewFromInternalPtr(still.GoPointer()).Unref()
		gtkx.Post(func() {
			if d.drag != drag || d.id == 0 {
				return
			}
			c.setSessionGap(d.from+1, true)
			block.SetVisible(false)
		})
	}
	d.onEnd = func(_ gtk.DragSource, drag uintptr, _ bool) {
		if d.drag == drag {
			c.endSessionDrag()
		}
	}
	// The native drag identity rejects other windows and pane/workspace
	// drags even if their string payload happens to name a session here.
	d.onAccept = func(_ gtk.DropTarget, ptr uintptr) bool {
		if d.id == 0 || !eligible() {
			return false
		}
		drop := gdk.DropNewFromInternalPtr(ptr)
		drag := drop.GetDrag()
		if drag == nil {
			return false
		}
		defer drag.Unref()
		return drag.GoPointer() == d.drag
	}
	d.onMotion = func(_ gtk.DropTarget, _, y float64) gdk.DragAction {
		if d.id == 0 || !eligible() {
			return 0
		}
		c.setSessionGap(c.sessionBefore(y), false)
		return gdk.ActionMoveValue
	}
	d.onLeave = func(gtk.DropTarget) {
		if d.id != 0 {
			c.setSessionGap(-1, false)
		}
	}
	d.onDrop = func(dt gtk.DropTarget, _ uintptr, _, y float64) bool {
		v := dt.GetValue()
		ws := a.shownWorkspace()
		if d.id == 0 || !eligible() || ws == nil || ws.ID != d.workspace || v == nil || v.GetString() != "session:"+strconv.Itoa(d.id) {
			return false
		}
		drop := dt.GetCurrentDrop()
		if drop == nil {
			return false
		}
		defer drop.Unref()
		if !d.onAccept(dt, drop.GoPointer()) {
			return false
		}
		before := 0
		if i := c.sessionBefore(y); i < len(d.rows) {
			before = d.rows[i]
		}
		move := &sessionMove{d.workspace, d.id, before}
		d.pending = move
		c.endSessionDrag()
		// Put the existing blocks in place before GTK can draw the source
		// again. Rebuilding the headers here would destroy the drag source.
		var previous *gtk.Widget
		for _, s := range d.preview(ws).Sessions {
			block := &c.heads[s.ID].section.Widget
			c.sessions.ReorderChildAfter(block, previous)
			previous = block
		}
		a.ask("move session", func(cl *host.Client) error {
			err := cl.MoveSession(move.workspace, move.session, move.before)
			// The host publishes its state before replying, even on failure;
			// its apply callback is already queued ahead of this one.
			gtkx.Post(func() {
				if d.pending == move {
					d.pending = nil
					c.update(a)
				}
			})
			return err
		})
		gtkx.Post(func() { c.fillSessions(a) })
		return true
	}
	d.onTick = func(uintptr) bool {
		moving := false
		step := func(cur, to float64) float64 {
			if math.Abs(to-cur) < 0.6 {
				return to
			}
			moving = true
			return cur + (to-cur)*0.3
		}
		for i := range d.gaps {
			g := &d.gaps[i]
			g.top, g.bottom = step(g.top, g.toTop), step(g.bottom, g.toBottom)
			c.applySessionGap(i)
		}
		d.ticking = moving
		return moving
	}
	drop := gtk.NewDropTarget(types.GType(gobject.TypeStringVal), gdk.ActionMoveValue)
	drop.SetPropagationPhase(gtk.PhaseCaptureValue)
	drop.ConnectAccept(&d.onAccept)
	drop.ConnectEnter(&d.onMotion)
	drop.ConnectMotion(&d.onMotion)
	drop.ConnectLeave(&d.onLeave)
	drop.ConnectDrop(&d.onDrop)
	c.sessions.AddController(&drop.EventController)
}

func (c *columns) dragSessionHead(h *sessionHead) {
	ds := gtk.NewDragSource()
	ds.SetActions(gdk.ActionMoveValue)
	ds.ConnectPrepare(&c.sessionDrag.onPrepare)
	ds.ConnectDragBegin(&c.sessionDrag.onBegin)
	ds.ConnectDragEnd(&c.sessionDrag.onEnd)
	h.button.AddController(&ds.EventController)
}

func (c *columns) endSessionDrag() {
	d := &c.sessionDrag
	d.id, d.drag, d.source = 0, 0, nil
	for _, id := range d.rows {
		c.heads[id].section.SetVisible(true)
	}
	c.setSessionGap(-1, true)
}

// A changed workspace, session order or pane layout invalidates the
// geometry captured at lift. Cancel before its widgets are replaced.
func (c *columns) cancelSessionDrag() {
	source := c.sessionDrag.source
	c.endSessionDrag()
	if source != nil {
		source.DragCancel()
	}
}

func (c *columns) sessionBefore(y float64) int {
	d := &c.sessionDrag
	for i, id := range d.rows {
		if i == d.from {
			continue
		}
		block := c.heads[id].section
		var r graphene.Rect
		if !block.GetVisible() || !block.ComputeBounds(&c.sessions.Widget, &r) {
			continue
		}
		f := *(*[4]float32)(unsafe.Pointer(&r))
		if y < float64(f[1]+f[3]/2) {
			return i
		}
	}
	return len(d.rows)
}

func (c *columns) setSessionGap(before int, now bool) {
	d := &c.sessionDrag
	last := -1
	for i := range d.rows {
		if i != d.from || d.id == 0 {
			last = i
		}
	}
	for i := range d.gaps {
		g := &d.gaps[i]
		g.toTop, g.toBottom = 0, 0
		if before >= 0 && i == before && i != d.from {
			g.toTop = d.slot
		}
		if before == len(d.rows) && i == last {
			g.toBottom = d.slot
		}
		if now {
			g.top, g.bottom = g.toTop, g.toBottom
			c.applySessionGap(i)
		}
	}
	if !now && !d.ticking {
		d.ticking = true
		glibTimeout(16, &d.onTick)
	}
}

func (c *columns) applySessionGap(i int) {
	d := &c.sessionDrag
	block := c.heads[d.rows[i]].section
	g := d.gaps[i]
	block.SetMarginTop(int32(math.Round(g.top)))
	block.SetMarginBottom(int32(math.Round(g.bottom)))
}
