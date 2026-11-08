// Package viewerui is inua-viewer's window (docs/apps.md#viewer): a
// picture zoomed, turned and stepped through its folder; a PDF's pages one
// under another; Markdown laid out to read, or plain text. The logic is
// internal/apps/viewer; this is the view.
package viewerui

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gdkpixbuf"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/viewer"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// Register gives the Viewer to appui: each start a window of its own.
func Register() {
	appui.RegisterMany("viewer", "Viewer", i18n.M("Viewer"), func() appui.View { return &App{} })
}

// App is the Viewer: one window per start, showing a file and stepping
// through the files of its kind in its folder.
type App struct {
	c   *appui.Ctx
	win *gtk.ApplicationWindow

	toolbar                                    gtk.Box
	name, meta, status, trashLabel, emptyLabel gtk.Label
	zoomLevel, prev, next, fullscreenB         gtk.Button
	rotateB, copyB, wallpaperB, editB          gtk.Button
	stage                                      gtk.Stack
	scroller                                   gtk.ScrolledWindow
	canvas                                     gtk.Fixed
	picture                                    gtk.Picture
	empty, details                             gtk.Box
	detailsReveal                              gtk.Revealer
	detailsToggle, sourceToggle                gtk.ToggleButton
	hadj, vadj                                 *gtk.Adjustment
	dialog                                     *gtk.FileDialog

	files []string
	index int
	kind  viewer.Kind // how the shown file is shown
	size  int64       // the shown document's; an image keeps its own
	mod   time.Time

	pdf  pdfView
	text textView
	edit editing // drawing on the image shown (edit.go)

	// The images loaded: the one shown, and those beside it (load.go).
	cache      map[string]*shot
	work       chan struct{} // a place for each load running
	cur        *shot
	prefetched bool // the images beside the shown one are loading

	// What is drawn.
	gen        uintptr // bumps on every image shown: an old animation's frames stop
	iter       *gdkpixbuf.PixbufAnimationIter
	texW, texH int // the texture drawn, which may be a smaller preview
	w, h       int // the image's pixels as shown (oriented, turned)
	turns      int // quarter turns clockwise asked for

	fit          bool    // follow the window, never above actual size
	zoom         float64 // screen pixels per image pixel
	viewW, viewH int     // the stage's size when last fitted
	px, py       float64 // the pointer over the stage
	dragX, dragY float64 // the scroll position when a drag began
	pinchZoom    float64

	trashArmed bool
	trashGen   uintptr
	statusGen  uintptr

	onButton                  func(gtk.Button)
	onDetails, onSource       func(gtk.ToggleButton)
	onKey                     func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onScroll                  func(gtk.EventControllerScroll, float64, float64) bool
	onMotion                  func(gtk.EventControllerMotion, float64, float64)
	onDragBegin, onDragUpdate func(gtk.GestureDrag, float64, float64)
	onPinchBegin              func(gtk.Gesture, uintptr)
	onPinch                   func(gtk.GestureZoom, float64)
	onDouble                  func(gtk.GestureClick, int32, float64, float64)
	onViewChanged             func(gtk.Adjustment)
	onFullscreened            func(uintptr, uintptr, uintptr)
	menu                      *appui.MenuBar
	onFrame, onDisarm, onCalm glib.SourceFunc
	onRefit                   glib.SourceFunc
	refitting                 bool
	onChosen                  gio.AsyncReadyCallback
}

func (a *App) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.c = c
	a.win = &gtk.ApplicationWindow{}
	ui := c.Load("viewer.ui")
	ui.Get("viewer", a.win)
	ui.Get("toolbar", &a.toolbar)
	ui.Get("name", &a.name)
	ui.Get("meta", &a.meta)
	ui.Get("status", &a.status)
	ui.Get("zoom_level", &a.zoomLevel)
	ui.Get("stage", &a.stage)
	ui.Get("scroller", &a.scroller)
	ui.Get("picture", &a.picture)
	ui.Get("canvas", &a.canvas)
	ui.Get("prev", &a.prev)
	ui.Get("next", &a.next)
	ui.Get("rotate", &a.rotateB)
	ui.Get("copy", &a.copyB)
	ui.Get("wallpaper", &a.wallpaperB)
	ui.Get("edit", &a.editB)
	ui.Get("edit_stage", &a.edit.stage)
	ui.Get("edit_picture", &a.edit.picture)
	ui.Get("empty", &a.empty)
	ui.Get("empty_label", &a.emptyLabel)
	ui.Get("details_reveal", &a.detailsReveal)
	ui.Get("details", &a.details)
	ui.Get("details_toggle", &a.detailsToggle)
	ui.Get("source", &a.sourceToggle)
	ui.Get("trash_label", &a.trashLabel)
	ui.Get("fullscreen", &a.fullscreenB)
	buttons := map[string]*gtk.Button{}
	for _, n := range []string{"open", "zoom_out", "zoom_in", "folder", "trash", "open_empty"} {
		b := &gtk.Button{}
		ui.Get(n, b)
		buttons[n] = b
	}
	a.pdf.init(a, ui)
	a.text.init(a, ui)
	ui.Close()
	buttons["zoom_level"], buttons["prev"], buttons["next"], buttons["fullscreen"] = &a.zoomLevel, &a.prev, &a.next, &a.fullscreenB
	buttons["rotate"], buttons["copy"], buttons["wallpaper"], buttons["edit"] = &a.rotateB, &a.copyB, &a.wallpaperB, &a.editB
	a.edit.onButton = func(b gtk.Button) { a.editAction(b.GetName()) }
	a.edit.onSaved = func(_, result, _ uintptr) { a.savedEdit(result) }

	a.onButton = func(b gtk.Button) { a.action(b.GetName()) }
	for n, b := range buttons {
		b.SetName(n)
		b.ConnectClicked(&a.onButton)
	}
	a.onDetails = func(t gtk.ToggleButton) { a.detailsReveal.SetRevealChild(t.GetActive()) }
	a.detailsToggle.ConnectToggled(&a.onDetails)
	a.onSource = func(t gtk.ToggleButton) { a.text.showSource(t.GetActive()) }
	a.sourceToggle.ConnectToggled(&a.onSource)

	a.hadj, a.vadj = a.scroller.GetHadjustment(), a.scroller.GetVadjustment()
	// The stage's size changes during the layout, which takes no new
	// sizes: refit once it is done.
	a.onViewChanged = func(gtk.Adjustment) {
		if !a.refitting {
			a.refitting = true
			glib.IdleAdd(&a.onRefit, 0)
		}
	}
	a.onRefit = func(uintptr) bool {
		a.refitting = false
		a.refit()
		return false
	}
	a.hadj.ConnectChanged(&a.onViewChanged)
	a.vadj.ConnectChanged(&a.onViewChanged)
	a.bindInput()
	a.menu = a.menuBar()
	// Full screen, however it came: the file alone.
	a.onFullscreened = func(uintptr, uintptr, uintptr) {
		full := a.win.IsFullscreen()
		a.toolbar.SetVisible(!full)
		a.detailsReveal.SetVisible(!full)
		a.fullscreenB.SetIconName(map[bool]string{true: "view-restore-symbolic", false: "view-fullscreen-symbolic"}[full])
	}
	gobject.SignalConnect(a.win.GoPointer(), "notify::fullscreened", glib.NewCallback(&a.onFullscreened))

	a.onFrame = func(gen uintptr) bool {
		if gen == a.gen && a.iter != nil {
			a.iter.Advance(nil)
			a.showFrame()
		}
		return false
	}
	a.onDisarm = func(gen uintptr) bool {
		if gen == a.trashGen {
			a.disarmTrash()
		}
		return false
	}
	a.onCalm = func(gen uintptr) bool {
		if gen == a.statusGen {
			a.status.SetText("")
		}
		return false
	}
	a.onChosen = func(_, result, _ uintptr) {
		d := a.dialog
		a.dialog = nil
		if d == nil {
			return
		}
		defer d.Unref()
		file, err := d.OpenFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || file == nil {
			return // cancelled
		}
		path := file.GetPath()
		unrefObject(file)
		a.Open([]string{path})
	}
	a.fit, a.zoom = true, 1
	return a.win
}

func (a *App) ThemeChanged() { a.text.recolor() }

// Open shows the files of the arguments (see viewer.List).
func (a *App) Open(args []string) {
	a.files, a.index = viewer.List(args)
	a.show(a.index)
}

// bindInput: on the picture, wheel and pinch zoom, drags pan, a double
// click switches between fitting and actual size; and the keys, which the
// window takes before the text view does.
func (a *App) bindInput() {
	motion := gtk.NewEventControllerMotion()
	a.onMotion = func(_ gtk.EventControllerMotion, x, y float64) { a.px, a.py = x, y }
	motion.ConnectMotion(&a.onMotion)
	a.scroller.AddController(&motion.EventController)

	// A wheel zooms; a touchpad scrolls, or zooms with Ctrl.
	scroll := gtk.NewEventControllerScroll(gtk.EventControllerScrollBothAxesValue)
	scroll.SetPropagationPhase(gtk.PhaseCaptureValue)
	a.onScroll = func(s gtk.EventControllerScroll, dx, dy float64) bool {
		if a.w == 0 {
			return false
		}
		ctrl := s.GetCurrentEventState()&gdk.ControlMaskValue != 0
		if s.GetUnit() == gdk.ScrollUnitSurfaceValue && !ctrl {
			return false // the scrolled window pans
		}
		if dy == 0 {
			return true
		}
		a.zoomTo(a.zoom*wheelFactor(s, dy), a.px, a.py)
		return true
	}
	scroll.ConnectScroll(&a.onScroll)
	a.scroller.AddController(&scroll.EventController)

	drag := gtk.NewGestureDrag()
	a.onDragBegin = func(gtk.GestureDrag, float64, float64) {
		a.dragX, a.dragY = a.hadj.GetValue(), a.vadj.GetValue()
	}
	a.onDragUpdate = func(_ gtk.GestureDrag, dx, dy float64) {
		a.hadj.SetValue(a.dragX - dx)
		a.vadj.SetValue(a.dragY - dy)
	}
	drag.ConnectDragBegin(&a.onDragBegin)
	drag.ConnectDragUpdate(&a.onDragUpdate)
	a.scroller.AddController(&drag.EventController)

	pinch := gtk.NewGestureZoom()
	a.onPinchBegin = func(gtk.Gesture, uintptr) { a.pinchZoom = a.zoom }
	a.onPinch = func(g gtk.GestureZoom, scale float64) {
		var x, y float64
		if !g.GetBoundingBoxCenter(&x, &y) {
			x, y = a.px, a.py
		}
		a.zoomTo(a.pinchZoom*scale, x, y)
	}
	pinch.ConnectBegin(&a.onPinchBegin)
	pinch.ConnectScaleChanged(&a.onPinch)
	a.scroller.AddController(&pinch.EventController)

	click := gtk.NewGestureClick()
	a.onDouble = func(_ gtk.GestureClick, n int32, x, y float64) {
		if n == 2 {
			a.toggleActual(x, y)
		}
	}
	click.ConnectPressed(&a.onDouble)
	a.scroller.AddController(&click.EventController)

	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	a.onKey = func(_ gtk.EventControllerKey, keyval, _ uint32, state gdk.ModifierType) bool {
		return a.key(keyval, state)
	}
	keys.ConnectKeyPressed(&a.onKey)
	a.win.AddController(&keys.EventController)
}

// wheelFactor is how much a turn of the wheel, or a touchpad's scroll,
// zooms by.
func wheelFactor(s gtk.EventControllerScroll, dy float64) float64 {
	if s.GetUnit() == gdk.ScrollUnitSurfaceValue {
		return math.Exp(-dy * 0.01)
	}
	return math.Pow(1.2, -dy)
}

// Keys the viewer takes.
const (
	keyBackSpace = 0xff08
	keyHome      = 0xff50
	keyLeft      = 0xff51
	keyUp        = 0xff52
	keyRight     = 0xff53
	keyDown      = 0xff54
	keyPageUp    = 0xff55
	keyPageDown  = 0xff56
	keyEnd       = 0xff57
	keyKPAdd     = 0xffab
	keyKPSub     = 0xffad
	keyKP0       = 0xffb0
	keyKP1       = 0xffb1
	keyF11       = 0xffc8
	keyDelete    = 0xffff
	keyEsc       = 0xff1b
)

func (a *App) key(keyval uint32, state gdk.ModifierType) bool {
	if a.edit.on {
		return a.editKey(keyval, state)
	}
	ctrl := state&gdk.ControlMaskValue != 0
	shift := state&gdk.ShiftMaskValue != 0
	switch {
	case ctrl && (keyval == 'o' || keyval == 'O'):
		a.action("open")
	case ctrl && (keyval == 'c' || keyval == 'C'):
		a.action("copy")
	case ctrl && keyval == keyPageUp:
		a.action("prev")
	case ctrl && keyval == keyPageDown:
		a.action("next")
	case ctrl && (keyval == '+' || keyval == '=' || keyval == keyKPAdd):
		a.zoomStep(true)
	case ctrl && (keyval == '-' || keyval == keyKPSub):
		a.zoomStep(false)
	case ctrl && (keyval == '0' || keyval == keyKP0):
		a.zoomReset(true)
	case ctrl:
		return false
	case a.kind == viewer.PDF && a.pdf.key(keyval, shift):
	case (a.kind == viewer.Markdown || a.kind == viewer.Text) && a.text.key(keyval, shift):
	case keyval == keyLeft || keyval == keyPageUp || keyval == keyBackSpace:
		a.action("prev")
	case keyval == keyRight || keyval == keyPageDown || keyval == ' ':
		a.action("next")
	case keyval == keyHome:
		a.show(0)
	case keyval == keyEnd:
		a.show(len(a.files) - 1)
	case keyval == '+' || keyval == '=' || keyval == keyKPAdd:
		a.zoomStep(true)
	case keyval == '-' || keyval == keyKPSub:
		a.zoomStep(false)
	case keyval == '0' || keyval == keyKP0:
		a.zoomReset(true)
	case keyval == '1' || keyval == keyKP1:
		a.zoomReset(false)
	case keyval == 'f' || keyval == 'F' || keyval == keyF11:
		a.action("fullscreen")
	case keyval == keyEsc && a.win.IsFullscreen():
		a.action("fullscreen")
	case keyval == keyEsc: // out of full screen first, then away
		a.win.Close()
	case keyval == 'r' && !shift:
		a.turn(1)
	case keyval == 'R' || (keyval == 'r' && shift) || keyval == 'l' || keyval == 'L':
		a.turn(3)
	case (keyval == 's' || keyval == 'S') && a.kind == viewer.Markdown:
		a.sourceToggle.SetActive(!a.sourceToggle.GetActive())
	case keyval == 'i' || keyval == 'I':
		a.detailsToggle.SetActive(!a.detailsToggle.GetActive())
	case (keyval == 'e' || keyval == 'E') && a.kind == viewer.Image:
		a.startEdit()
	case keyval == keyDelete:
		a.action("trash")
	default:
		return false
	}
	return true
}

// zoomStep zooms the file shown in or out a step.
func (a *App) zoomStep(up bool) {
	switch a.kind {
	case viewer.PDF:
		a.pdf.step(up)
	case viewer.Markdown, viewer.Text:
		a.text.step(up)
	default:
		a.zoomTo(viewer.StepZoom(a.zoom, up), a.hadj.GetPageSize()/2, a.vadj.GetPageSize()/2)
	}
}

// zoomReset fits the file shown to the window, or shows it at its actual
// size.
func (a *App) zoomReset(fit bool) {
	switch a.kind {
	case viewer.PDF:
		a.pdf.reset(fit)
	case viewer.Markdown, viewer.Text:
		a.text.setZoom(1)
	default:
		if fit {
			a.fitWindow()
		} else {
			a.zoomTo(1, a.hadj.GetPageSize()/2, a.vadj.GetPageSize()/2)
		}
	}
}

// setZoomLabel shows the zoom, and what a click on it does.
func (a *App) setZoomLabel(label, tip string) {
	a.zoomLevel.SetLabel(label)
	a.zoomLevel.SetTooltipText(tip)
}

func (a *App) action(name string) {
	if name != "trash" {
		a.disarmTrash()
	}
	switch name {
	case "open", "open_empty":
		a.choose()
	case "prev":
		if len(a.files) > 1 {
			a.show((a.index - 1 + len(a.files)) % len(a.files))
		}
	case "next":
		if len(a.files) > 1 {
			a.show((a.index + 1) % len(a.files))
		}
	case "zoom_in":
		a.zoomStep(true)
	case "zoom_out":
		a.zoomStep(false)
	case "zoom_level":
		switch a.kind {
		case viewer.PDF:
			a.pdf.reset(!a.pdf.fit)
		case viewer.Markdown, viewer.Text:
			a.text.setZoom(1)
		default:
			a.toggleActual(a.hadj.GetPageSize()/2, a.vadj.GetPageSize()/2)
		}
	case "rotate":
		a.turn(1)
	case "edit":
		a.startEdit()
	case "fullscreen":
		if a.win.IsFullscreen() {
			a.win.Unfullscreen()
		} else {
			a.win.Fullscreen()
		}
	}
	path := a.current()
	if path == "" {
		return
	}
	switch name {
	case "copy":
		a.copy(path)
	case "wallpaper":
		if a.kind == viewer.Image {
			a.setWallpaper(path)
		}
	case "folder":
		go showInFolder(path)
	case "trash":
		if !a.trashArmed {
			a.trashArmed = true
			a.trashLabel.SetVisible(true)
			a.trashGen++
			glib.TimeoutAdd(3000, &a.onDisarm, a.trashGen)
			return
		}
		a.disarmTrash()
		if _, err := gio.FileNewForPath(path).Trash(nil); err != nil {
			log.Printf("viewer: trash: %v", err)
			a.flash(i18n.T("Could not move to Trash"))
			return
		}
		if s := a.cache[path]; s != nil {
			a.drop(s)
		}
		a.files = slices.Delete(a.files, a.index, a.index+1)
		a.flash(i18n.T("Moved to Trash"))
		if len(a.files) == 0 {
			a.leave(viewer.None)
			a.showEmpty(i18n.T("Nothing left here."))
			return
		}
		a.show(min(a.index, len(a.files)-1))
	}
}

// copy puts the image on the clipboard, a PDF's page as text, or the
// text selected (else all of it).
func (a *App) copy(path string) {
	var mime string
	var read func() (io.Reader, func(), error)
	switch a.kind {
	case viewer.PDF:
		doc, page := a.pdf.doc, a.pdf.cur
		if doc == nil {
			return
		}
		mime = "text/plain;charset=utf-8"
		read = func() (io.Reader, func(), error) {
			s := doc.PageText(page)
			if strings.TrimSpace(s) == "" {
				return nil, nil, errors.New("no text on the page")
			}
			return strings.NewReader(s), func() {}, nil
		}
	case viewer.Markdown, viewer.Text:
		s := a.text.copyText()
		mime = "text/plain;charset=utf-8"
		read = func() (io.Reader, func(), error) { return strings.NewReader(s), func() {}, nil }
	default:
		mime = viewer.Mime(path)
		read = func() (io.Reader, func(), error) {
			f, err := os.Open(path)
			if err != nil {
				return nil, nil, err
			}
			return f, func() { f.Close() }, nil
		}
	}
	go func() {
		r, done, err := read()
		if err == nil {
			cmd := exec.Command("wl-copy", "--type", mime) // its output to /dev/null: no pipe to wait on
			cmd.Stdin = r
			err = cmd.Run()
			done()
		}
		gtkx.Post(func() {
			if err != nil {
				log.Printf("viewer: copy: %v", err)
				a.flash(i18n.T("Could not copy"))
			} else {
				a.flash(i18n.T("Copied"))
			}
		})
	}()
}

func (a *App) current() string {
	if a.index < 0 || a.index >= len(a.files) {
		return ""
	}
	return a.files[a.index]
}

func (a *App) disarmTrash() {
	a.trashArmed = false
	a.trashLabel.SetVisible(false)
}

// flash says something in the toolbar for a moment.
func (a *App) flash(text string) {
	a.status.SetText(text)
	a.statusGen++
	glib.TimeoutAdd(1800, &a.onCalm, a.statusGen)
}

// choose asks for a file to open.
func (a *App) choose() {
	if a.dialog != nil {
		return
	}
	d := gtk.NewFileDialog()
	d.SetTitle(i18n.T("Open a File"))
	filter := gtk.NewFileFilter()
	filter.SetName(i18n.T("Images, PDFs, Markdown and text"))
	for _, s := range viewer.Suffixes() {
		filter.AddSuffix(s)
	}
	d.SetDefaultFilter(filter)
	filter.Unref()
	if p := a.current(); p != "" {
		f := gio.FileNewForPath(filepath.Dir(p))
		d.SetInitialFolder(f)
		unrefObject(f)
	} else if home, err := os.UserHomeDir(); err == nil {
		f := gio.FileNewForPath(filepath.Join(home, "Pictures"))
		d.SetInitialFolder(f)
		unrefObject(f)
	}
	a.dialog = d
	d.Open(&a.win.Window, nil, &a.onChosen, 0)
}

// show shows the i-th file, in the view for its kind.
func (a *App) show(i int) {
	if i < 0 || i >= len(a.files) {
		if len(a.files) == 0 {
			a.showEmpty(i18n.T("Open an image or a document to view it."))
		}
		return
	}
	path := a.files[i]
	kind := viewer.Sniff(path)
	if kind == viewer.None {
		kind = viewer.Image // for GDK's loaders to try
	}
	a.leave(kind)
	a.index, a.kind = i, kind
	a.name.SetText(filepath.Base(path))
	a.win.SetTitle(filepath.Base(path))
	a.empty.SetVisible(false)
	a.setTools()
	a.size, a.mod = 0, time.Time{}
	if kind != viewer.Image {
		if st, err := os.Stat(path); err == nil {
			a.size, a.mod = st.Size(), st.ModTime()
		}
	}
	switch kind {
	case viewer.PDF:
		a.stage.SetVisibleChildName("pdf")
		a.pdf.open(path)
	case viewer.Markdown, viewer.Text:
		a.stage.SetVisibleChildName("text")
		a.text.open(path, kind)
	default:
		a.stage.SetVisibleChildName("image")
		a.showImage(path)
	}
	a.showMeta()
	a.showDetails()
}

// leave lets go of what the shown file's view holds, before a file of
// kind next is shown.
func (a *App) leave(next viewer.Kind) {
	a.endEdit()
	switch a.kind {
	case viewer.Image:
		if next != viewer.Image {
			a.clearImage()
			a.keep()
		}
	case viewer.PDF:
		a.pdf.close()
	case viewer.Markdown, viewer.Text:
		a.text.clear()
	}
	a.kind = viewer.None
}

// setTools shows the toolbar's buttons the kind of file shown has use for.
func (a *App) setTools() {
	image := a.kind == viewer.Image
	a.rotateB.SetVisible(image)
	a.editB.SetVisible(image)
	a.wallpaperB.SetVisible(image)
	a.sourceToggle.SetVisible(a.kind == viewer.Markdown)
	many := len(a.files) > 1
	a.prev.SetVisible(many)
	a.next.SetVisible(many)
	switch a.kind {
	case viewer.PDF:
		a.copyB.SetTooltipText(i18n.T("Copy the page's text (Ctrl+C)"))
		a.prev.SetTooltipText(i18n.T("Previous file (Ctrl+Page Up)"))
		a.next.SetTooltipText(i18n.T("Next file (Ctrl+Page Down)"))
	case viewer.Markdown, viewer.Text:
		a.copyB.SetTooltipText(i18n.T("Copy the text (Ctrl+C)"))
		a.prev.SetTooltipText(i18n.T("Previous (←)"))
		a.next.SetTooltipText(i18n.T("Next (→)"))
	default:
		a.copyB.SetTooltipText(i18n.T("Copy the image (Ctrl+C)"))
		a.prev.SetTooltipText(i18n.T("Previous (←)"))
		a.next.SetTooltipText(i18n.T("Next (→)"))
	}
}

// failed says a file could not be shown.
func (a *App) failed(path string, err error) {
	switch {
	case errors.Is(err, viewer.ErrEncrypted):
		a.showEmpty(i18n.Tf("%s is locked with a password.", filepath.Base(path)))
	case errors.Is(err, viewer.ErrNotText):
		a.showEmpty(i18n.Tf("%s is not UTF-8 text.", filepath.Base(path)))
	case viewer.PDFAvailable() != nil && viewer.KindOf(path) == viewer.PDF:
		a.showEmpty(i18n.T("Install libpoppler-glib8t64 to view PDFs."))
	default:
		a.showEmpty(i18n.Tf("Could not open %s.\n%v", filepath.Base(path), err))
	}
}

func (a *App) showEmpty(text string) {
	a.emptyLabel.SetText(text)
	a.empty.SetVisible(true)
	if len(a.files) == 0 {
		a.name.SetText(i18n.T("Viewer"))
		a.win.SetTitle(i18n.T("Viewer"))
		a.meta.SetText("")
		a.prev.SetVisible(false)
		a.next.SetVisible(false)
	}
}

func (a *App) position() string {
	if len(a.files) <= 1 {
		return ""
	}
	return fmt.Sprintf("%d / %d", a.index+1, len(a.files))
}

// showMeta: where it is in the folder; an image's size, a PDF's page or a
// text's lines; its file size and type.
func (a *App) showMeta() {
	var parts []string
	if p := a.position(); p != "" {
		parts = append(parts, p)
	}
	size := a.size
	switch a.kind {
	case viewer.PDF:
		if n := a.pdf.pages(); n > 0 {
			parts = append(parts, i18n.Tf("Page %d of %d", a.pdf.cur+1, n))
		}
	case viewer.Markdown, viewer.Text:
		if lines := a.text.stats[0]; lines > 0 {
			parts = append(parts, i18n.Nf("%d line", "%d lines", lines, lines))
		}
	default:
		if a.w > 0 {
			parts = append(parts, fmt.Sprintf("%d × %d", a.w, a.h))
		}
		size = 0
		if a.cur != nil {
			size = a.cur.size
		}
	}
	if size > 0 {
		parts = append(parts, viewer.Size(size))
	}
	if ext := strings.TrimPrefix(filepath.Ext(a.current()), "."); ext != "" {
		parts = append(parts, strings.ToUpper(ext))
	}
	a.meta.SetText(strings.Join(parts, "  ·  "))
}

// showDetails fills the details panel: the file, then the camera, the
// document or the text.
func (a *App) showDetails() {
	gtkx.Clear(&a.details)
	path := a.current()
	if path == "" {
		return
	}
	section := func(title string) {
		l := gtk.NewLabel(title)
		l.AddCssClass("kicker")
		l.AddCssClass("viewer-detail-head")
		l.SetXalign(0)
		gtkx.Append(&a.details, &l.Widget)
	}
	row := func(key, value string) {
		if value == "" {
			return
		}
		box := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		box.AddCssClass("viewer-detail")
		k := gtk.NewLabel(key)
		k.AddCssClass("viewer-detail-key")
		k.SetXalign(0)
		gtkx.Append(box, &k.Widget)
		v := gtk.NewLabel(value)
		v.AddCssClass("viewer-detail-value")
		v.SetXalign(0)
		v.SetWrap(true)
		v.SetWrapMode(2) // word or character
		v.SetSelectable(true)
		gtkx.Append(box, &v.Widget)
		gtkx.Append(&a.details, &box.Widget)
	}
	date := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return i18n.Time(t, "Jan 2, 2006  15:04")
	}
	section(i18n.T("FILE"))
	row(i18n.T("Name"), filepath.Base(path))
	row(i18n.T("Folder"), shortHome(filepath.Dir(path)))
	switch a.kind {
	case viewer.PDF:
		row(i18n.T("Size"), viewer.Size(a.size))
		row(i18n.T("Type"), viewer.Mime(path))
		row(i18n.T("Modified"), date(a.mod))
		if a.pdf.doc == nil {
			return
		}
		info := a.pdf.info
		section(i18n.T("DOCUMENT"))
		row(i18n.T("Title"), info.Title)
		row(i18n.T("Author"), info.Author)
		row(i18n.T("Subject"), info.Subject)
		row(i18n.T("Pages"), fmt.Sprint(a.pdf.pages()))
		row(i18n.T("Page size"), viewer.PageSizeText(a.pdf.doc.PageSize(a.pdf.cur)))
		row(i18n.T("Created"), date(info.Created))
		row(i18n.T("Changed"), date(info.Modified))
		row(i18n.T("Made with"), strings.Join(slices.Compact([]string{info.Creator, info.Producer}), "  ·  "))
		return
	case viewer.Markdown, viewer.Text:
		row(i18n.T("Size"), viewer.Size(a.size))
		row(i18n.T("Type"), viewer.Mime(path))
		row(i18n.T("Modified"), date(a.mod))
		if st := a.text.stats; st[2] > 0 {
			section(i18n.T("TEXT"))
			row(i18n.T("Lines"), fmt.Sprint(st[0]))
			row(i18n.T("Words"), fmt.Sprint(st[1]))
			row(i18n.T("Characters"), fmt.Sprint(st[2]))
		}
		return
	}
	s := a.cur
	if s == nil {
		return
	}
	row(i18n.T("Size"), viewer.Size(s.size))
	ow, oh := s.fullW, s.fullH // as it stands, before any turns asked for
	if t, _ := viewer.Orientation(s.orient); t%2 == 1 {
		ow, oh = oh, ow
	}
	dims := fmt.Sprintf("%d × %d", ow, oh)
	if mp := viewer.Megapixels(ow, oh); mp != "" {
		dims += "  ·  " + mp
	}
	row(i18n.T("Dimensions"), dims)
	row(i18n.T("Type"), viewer.Mime(path))
	row(i18n.T("Modified"), date(s.mod))
	if e := s.exif; !e.Empty() {
		section(i18n.T("CAMERA"))
		row(i18n.T("Camera"), e.Camera())
		row(i18n.T("Lens"), e.Lens)
		row(i18n.T("Exposure"), e.Settings())
		row(i18n.T("Taken"), date(e.Taken))
	}
}

// setWallpaper makes the image inua's wallpaper and asks the shell to read
// its settings again.
func (a *App) setWallpaper(path string) {
	cfgPath := config.DefaultPath()
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		cfg = config.Default()
	}
	cfg.Background.Enable = true
	cfg.Background.Image = shortHome(path)
	if err := config.Save(cfgPath, cfg); err != nil {
		log.Printf("viewer: wallpaper: %v", err)
		a.flash(i18n.T("Could not set the wallpaper"))
		return
	}
	go exec.Command("inua", "call", "shell", "reload").Run()
	a.flash(i18n.T("Set as wallpaper"))
}

// scale is the screen's pixels per logical pixel.
func (a *App) scale() float64 {
	if n := a.win.GetNative(); n != nil {
		if s := n.GetSurface(); s != nil {
			if f := s.GetScale(); f > 0 {
				return f
			}
		}
	}
	return 1
}

// showInFolder opens the file manager at the file, or its folder.
func showInFolder(path string) {
	uri := "file://" + path
	err := exec.Command("gdbus", "call", "--session", "--dest", "org.freedesktop.FileManager1",
		"--object-path", "/org/freedesktop/FileManager1",
		"--method", "org.freedesktop.FileManager1.ShowItems", "['"+strings.ReplaceAll(uri, "'", "\\'")+"']", "").Run()
	if err == nil {
		return
	}
	if err := exec.Command("xdg-open", filepath.Dir(path)).Run(); err != nil {
		log.Printf("viewer: show in folder: %v", err)
	}
}

// shortHome writes the home directory as "~".
func shortHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && (p == home || strings.HasPrefix(p, home+"/")) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func unrefObject(o gobject.Ptr) { (&gobject.Object{Ptr: o.GoPointer()}).Unref() }

// noPaintable empties a picture.
func noPaintable(p *gtk.Picture) { p.SetPaintable(&gdk.Texture{}) }
