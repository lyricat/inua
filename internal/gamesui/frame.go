package gamesui

import (
	"image/color"
	"time"

	"codeberg.org/puregotk/puregotk/v4/cairo"
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/games"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// play is one game in its window.
type play interface {
	// setup builds the game's board into the frame; called once.
	setup(f *frame)
	// reset starts a new game.
	reset()
	// redraw draws again in the theme's new colours.
	redraw()
}

// palette is the theme's colours, for drawing.
type palette struct {
	app, surface, surface2, line, t0, t1, t2, accent, ok, warn, danger color.NRGBA
}

// frame is a game's window (game.ui): its name, a status line and
// numbers on top, the board, and the keys at the bottom.
type frame struct {
	c                            *appui.Ctx
	win                          *gtk.ApplicationWindow
	menu                         *appui.MenuBar
	heading, status, rules, hint gtk.Label
	stats, body                  gtk.Box
	restart                      gtk.Button
	col                          palette
	focus                        int

	// keyDown and keyUp get the game's keys, letters in lower case.
	keyDown func(key uint32) bool
	keyUp   func(key uint32)

	onRestart func(gtk.Button)
	onKey     func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onKeyUp   func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType)
	onClose   func(gtk.Window) bool
}

// Keys, as GDK names them.
const (
	keyHome  = 0xff50
	keyLeft  = 0xff51
	keyUp    = 0xff52
	keyRight = 0xff53
	keyDown  = 0xff54
	keyShift = 0xffe1
	keyRShft = 0xffe2
	keyEnter = 0xff0d
)

func lower(k uint32) uint32 {
	if k >= 'A' && k <= 'Z' {
		return k + 'a' - 'A'
	}
	return k
}

// view is a game as an appui light app.
type view struct {
	info entry
	g    play
	f    frame
}

func (v *view) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	f := &v.f
	f.c = c
	f.win = &gtk.ApplicationWindow{}
	u := c.Load("game.ui")
	u.Get("game", f.win)
	u.Get("heading", &f.heading)
	u.Get("status", &f.status)
	u.Get("rules", &f.rules)
	u.Get("hint", &f.hint)
	u.Get("stats", &f.stats)
	u.Get("body", &f.body)
	u.Get("restart", &f.restart)
	u.Close()
	f.heading.SetText(i18n.T(v.info.Title))
	f.rules.SetText(i18n.T(v.info.Rules))
	f.hint.SetText(i18n.T(v.info.Keys))
	f.loadColors()
	f.onRestart = func(gtk.Button) { v.g.reset() }
	f.restart.ConnectClicked(&f.onRestart)
	f.menu = v.menuBar()
	f.onClose = func(gtk.Window) bool {
		if f.menu != nil {
			f.menu.Close()
		}
		return false
	}
	f.win.ConnectCloseRequest(&f.onClose)
	f.onKey = func(_ gtk.EventControllerKey, key, _ uint32, mod gdk.ModifierType) bool {
		if mod&(gdk.ControlMaskValue|gdk.AltMaskValue) != 0 {
			return false
		}
		if lower(key) == 'n' {
			v.g.reset()
			return true
		}
		return f.keyDown != nil && f.keyDown(lower(key))
	}
	f.onKeyUp = func(_ gtk.EventControllerKey, key, _ uint32, _ gdk.ModifierType) {
		if f.keyUp != nil {
			f.keyUp(lower(key))
		}
	}
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	keys.ConnectKeyPressed(&f.onKey)
	keys.ConnectKeyReleased(&f.onKeyUp)
	f.win.AddController(&keys.EventController)
	v.g.setup(f)
	v.g.reset()
	return f.win
}

// Theme is the game's own theme, if it keeps one (themed).
func (v *view) Theme() string {
	if t, ok := v.g.(themed); ok {
		return t.theme()
	}
	return ""
}

// themed is a game that keeps a theme of its own, whatever the shell's.
type themed interface{ theme() string }

func (v *view) ThemeChanged() {
	v.f.loadColors()
	v.g.redraw()
}

func (f *frame) loadColors() {
	c := func(name string) color.NRGBA { return hexColor(f.c.Colors[name]) }
	f.col = palette{c("app"), c("surface"), c("surface2"), c("line"), c("t0"), c("t1"), c("t2"),
		c("accent"), c("ok"), c("warn"), c("danger")}
}

// stat adds a number to the header, under a small name; it returns the
// label to set.
func (f *frame) stat(name string) *gtk.Label {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 1)
	k := gtk.NewLabel(name)
	k.AddCssClass("kicker")
	k.SetXalign(1)
	gtkx.Append(box, &k.Widget)
	v := gtk.NewLabel("")
	v.AddCssClass("game-stat")
	v.SetXalign(1)
	gtkx.Append(box, &v.Widget)
	gtkx.Append(&f.stats, &box.Widget)
	return v
}

// say sets the status line, in a tone: "" plain, "good" or "bad".
func (f *frame) say(text, tone string) {
	f.status.SetText(text)
	f.status.RemoveCssClass("good")
	f.status.RemoveCssClass("bad")
	if tone != "" {
		f.status.AddCssClass(tone)
	}
}

var noDestroy glib.DestroyNotify = func(uintptr) {}

// canvas is a board drawn with cairo, in units of its own (w × h) that
// are scaled to fit the widget and centred in it; or, filling it, in the
// widget's own units, w × h its size.
type canvas struct {
	area          *gtk.DrawingArea
	w, h          float64
	scale, ox, oy float64
	top           bool // drawn at the top of the widget, not in its middle
	fill          bool // drawn in the widget's units, at its size
	paint         func(p pen)
	onDraw        gtk.DrawingAreaDrawFunc

	onPress  func(gtk.GestureClick, int32, float64, float64)
	onMotion func(gtk.EventControllerMotion, float64, float64)
	onLeave  func(gtk.EventControllerMotion)

	step   func(dt float64) bool
	onTick gtk.TickCallback
	tick   uint32
	last   time.Time
}

func newCanvas(w, h float64, paint func(p pen)) *canvas {
	c := &canvas{area: gtk.NewDrawingArea(), w: w, h: h, paint: paint}
	c.area.SetHexpand(true)
	c.area.SetVexpand(true)
	c.area.SetFocusable(true)
	c.onDraw = func(_ uintptr, ctx *cairo.Context, width, height int32, _ uintptr) {
		if c.fill {
			c.w, c.h = float64(width), float64(height)
		}
		c.scale = min(float64(width)/c.w, float64(height)/c.h)
		c.ox = (float64(width) - c.w*c.scale) / 2
		c.oy = (float64(height) - c.h*c.scale) / 2
		if c.top {
			c.oy = 0
		}
		p := newPen(ctx)
		p.Translate(c.ox, c.oy)
		p.Scale(c.scale, c.scale)
		p.Round()
		c.paint(p)
	}
	c.area.SetDrawFunc(&c.onDraw, 0, &noDestroy)
	c.onTick = func(uintptr, uintptr, uintptr) bool {
		now := time.Now()
		dt := now.Sub(c.last).Seconds()
		c.last = now
		more := c.step != nil && c.step(dt)
		c.area.QueueDraw()
		if !more {
			c.tick = 0
		}
		return more
	}
	return c
}

// at is a point of the widget in the board's units.
func (c *canvas) at(x, y float64) games.Vec {
	if c.scale == 0 {
		return games.Vec{X: -1, Y: -1}
	}
	return games.Vec{X: (x - c.ox) / c.scale, Y: (y - c.oy) / c.scale}
}

func (c *canvas) redraw() { c.area.QueueDraw() }

// animate calls step every frame, with the seconds since the last, until
// it returns false.
func (c *canvas) animate(step func(dt float64) bool) {
	c.step = step
	if c.tick == 0 {
		c.last = time.Now()
		c.tick = c.area.AddTickCallback(&c.onTick, 0, &noDestroy)
	}
}

// button is an outlined button that runs do when clicked.
func button(label string, do *func(gtk.Button)) *gtk.Button {
	b := gtk.NewButtonWithLabel(label)
	b.AddCssClass("outlined")
	b.ConnectClicked(do)
	return b
}

// choices is a row of buttons for one of a few settings (a level, a
// size); the one in use is lit.
type choices struct {
	box     *gtk.Box
	buttons []*gtk.Button
	pick    func(int)
	onClick func(gtk.Button)
}

// choices adds a row of choices above the board.
func (f *frame) choices(names []string, pick func(int)) *choices {
	c := &choices{box: gtk.NewBox(gtk.OrientationHorizontalValue, 6), pick: pick}
	c.box.AddCssClass("game-choices")
	c.box.SetHalign(gtk.AlignCenterValue)
	c.onClick = func(b gtk.Button) {
		i := int(b.GetName()[0] - 'a')
		c.set(i)
		c.pick(i)
	}
	for i, n := range names {
		b := button(n, &c.onClick)
		b.SetName(string(rune('a' + i)))
		b.AddCssClass("game-choice")
		gtkx.Append(c.box, &b.Widget)
		c.buttons = append(c.buttons, b)
	}
	f.body.Prepend(&c.box.Widget)
	c.box.Unref()
	return c
}

// set lights choice i.
func (c *choices) set(i int) {
	for n, b := range c.buttons {
		if n == i {
			b.AddCssClass("on")
		} else {
			b.RemoveCssClass("on")
		}
	}
}

// clicks gets the canvas's clicks, with the mouse button (1 primary,
// 2 middle, 3 secondary), in the board's units.
func (c *canvas) clicks(do func(button int, at games.Vec)) {
	c.onPress = func(g gtk.GestureClick, _ int32, x, y float64) {
		c.area.GrabFocus()
		do(int(g.GetCurrentButton()), c.at(x, y))
	}
	click := gtk.NewGestureClick()
	click.SetButton(0)
	click.ConnectPressed(&c.onPress)
	c.area.AddController(&click.EventController)
}

// hovers gets where the pointer is over the canvas; it leaves at -1, -1.
func (c *canvas) hovers(do func(at games.Vec)) {
	c.onMotion = func(_ gtk.EventControllerMotion, x, y float64) { do(c.at(x, y)) }
	c.onLeave = func(gtk.EventControllerMotion) { do(games.Vec{X: -1, Y: -1}) }
	m := gtk.NewEventControllerMotion()
	m.ConnectMotion(&c.onMotion)
	m.ConnectLeave(&c.onLeave)
	c.area.AddController(&m.EventController)
}

// add puts a canvas on the frame's body.
func (f *frame) add(c *canvas) { gtkx.Append(&f.body, &c.area.Widget) }

// veil dims a w × h board and sets a title and a line over it, for the
// end of a game.
func (f *frame) veil(pn pen, w, h float64, title, sub string, tone color.NRGBA) {
	pn.Alpha(f.col.app, 0.72)
	pn.Rect(0, 0, w, h)
	pn.Fill()
	pn.Text(title, "IBM Plex Sans Bold 24", w/2, h/2-14, 0.5, 0.5, tone)
	pn.Text(sub, "IBM Plex Sans 12", w/2, h/2+18, 0.5, 0.5, f.col.t1)
}
