package viewerui

import (
	"image"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/annotui"
	"github.com/lyricat/inua/internal/apps/viewer"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// Editing an image: arrows, boxes, a pen and a highlighter, text, numbered
// steps and pixelated parts drawn over it (internal/annotui), on a page of
// its own where the image fits the window as it stands (turned, mirrored).
// Save asks where each time, a name beside the file offered; Copy puts the
// image as edited on the clipboard; Done, or Escape, goes back, warning
// once first when the edits are not saved.

type editing struct {
	on      bool
	stage   gtk.Overlay
	picture gtk.Picture
	ed      *annotui.Editor
	tex     *gdk.Texture // the image as it stands, edited over
	path    string
	saved   bool // nothing drawn since the last save or copy
	leaving bool // Escape once with edits not saved: again leaves
	gen     uintptr
	dialog  *gtk.FileDialog

	onButton func(gtk.Button)
	onSaved  gio.AsyncReadyCallback
}

// startEdit opens the image shown for editing, once it has loaded whole.
func (a *App) startEdit() {
	e := &a.edit
	s := a.cur
	if e.on || a.kind != viewer.Image || s == nil {
		return
	}
	if s.anim != nil {
		a.flash(i18n.T("Moving images cannot be edited"))
		return
	}
	if !s.full || s.tex == nil {
		a.flash(i18n.T("Still loading…"))
		return
	}
	// Its pixels, then made upright off the main thread.
	w, h := int(s.tex.GetWidth()), int(s.tex.GetHeight())
	bgra := make([]byte, w*h*4)
	s.tex.Download(bgra, uint(w*4))
	turns, mirror := a.orientation()
	e.gen++
	gen, path := e.gen, s.path
	a.flash(i18n.T("Opening for editing…"))
	go func() {
		img := uprightFromBGRA(bgra, w, h, turns, mirror)
		gtkx.Post(func() {
			if gen != e.gen || a.cur == nil || a.cur.path != path {
				return
			}
			a.showEdit(img, path)
		})
	}()
}

// uprightFromBGRA is premultiplied BGRA pixels (GdkTexture's download) as
// an RGBA image standing as Viewer shows it.
func uprightFromBGRA(px []byte, w, h, turns int, mirror bool) *image.RGBA {
	img := &image.RGBA{Pix: px, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
	for i := 0; i+3 < len(px); i += 4 {
		px[i], px[i+2] = px[i+2], px[i]
	}
	return viewer.Upright(img, turns, mirror)
}

func (a *App) showEdit(img *image.RGBA, path string) {
	e := &a.edit
	e.on, e.path, e.saved, e.leaving = true, path, true, false
	e.tex = rgbaTexture(img)
	e.picture.SetPaintable(e.tex)
	e.ed = annotui.New(img, img.Bounds())
	e.ed.Changed = func() { e.saved, e.leaving = e.ed.Doc.Empty(), false }
	layer := e.ed.Layer()
	e.stage.AddOverlay(layer)
	e.ed.Fit()

	bar := e.ed.Toolbar()
	bar.SetHalign(gtk.AlignCenterValue)
	bar.SetValign(gtk.AlignEndValue)
	bar.SetMarginBottom(20)
	sep := gtk.NewSeparator(gtk.OrientationVerticalValue)
	bar.Append(&sep.Widget)
	for _, b := range []struct{ name, icon, tip string }{
		{"edit_save", "document-save-symbolic", i18n.T("Save… (Ctrl+S)")},
		{"edit_copy", "edit-copy-symbolic", i18n.T("Copy (Ctrl+C)")},
	} {
		btn := gtk.NewButtonFromIconName(b.icon)
		btn.SetName(b.name)
		btn.SetTooltipText(b.tip)
		btn.AddCssClass("annot-tool")
		btn.ConnectClicked(&e.onButton)
		bar.Append(&btn.Widget)
	}
	done := gtk.NewButtonWithLabel(i18n.T("Done"))
	done.SetName("edit_done")
	done.SetTooltipText(i18n.T("Back to viewing (Esc)"))
	done.AddCssClass("annot-done")
	done.ConnectClicked(&e.onButton)
	bar.Append(&done.Widget)
	e.stage.AddOverlay(&bar.Widget)

	a.stage.SetVisibleChildName("edit")
	a.prev.SetVisible(false)
	a.next.SetVisible(false)
	a.status.SetText("")
}

// endEdit goes back to viewing the image, the edits let go of.
func (a *App) endEdit() {
	e := &a.edit
	if !e.on {
		e.gen++ // an opening on its way is not wanted
		return
	}
	e.on = false
	e.stage.RemoveOverlay(e.ed.Layer())
	e.stage.RemoveOverlay(&e.ed.Toolbar().Widget)
	e.ed.Free()
	e.ed = nil
	noPaintable(&e.picture)
	e.tex.Unref()
	e.tex = nil
	a.stage.SetVisibleChildName("image")
	a.setTools()
}

// leaveEdit is Done or Escape: with edits not saved, a first time warns.
func (a *App) leaveEdit() {
	e := &a.edit
	if !e.saved && !e.leaving {
		e.leaving = true
		a.flash(i18n.T("Not saved: Esc again to leave"))
		return
	}
	a.endEdit()
}

// editMenu is a menu command chosen while editing, as editKey takes the
// keys: copy takes the edited image, a command that shows another file
// first leaves editing as Done does (with edits not saved, a first time
// warns), and what changes the view under the editor waits until it ends.
func (a *App) editMenu(name string) {
	e := &a.edit
	switch name {
	case "copy":
		a.copyEdit()
	case "open", "prev", "next":
		if !e.saved && !e.leaving {
			e.leaving = true
			a.flash(i18n.T("Not saved: choose it again to leave"))
			return
		}
		a.endEdit()
		a.action(name)
	}
}

// editKey takes the keys while editing; the words of a text are typed
// into their entry.
func (a *App) editKey(keyval uint32, state gdk.ModifierType) bool {
	e := &a.edit
	if e.ed.Typing() {
		return false
	}
	ctrl := state&gdk.ControlMaskValue != 0
	switch {
	case ctrl && (keyval == 'z' || keyval == 'Z'):
		if state&gdk.ShiftMaskValue != 0 {
			e.ed.Redo()
		} else {
			e.ed.Undo()
		}
	case ctrl && (keyval == 'y' || keyval == 'Y'):
		e.ed.Redo()
	case ctrl && (keyval == 's' || keyval == 'S'):
		a.saveEdit()
	case ctrl && (keyval == 'c' || keyval == 'C'):
		a.copyEdit()
	case keyval == keyEsc && e.ed.Tool() != "":
		e.ed.SetTool("") // the tool first, then away
	case keyval == keyEsc:
		a.leaveEdit()
	default:
		return false
	}
	return true
}

func (a *App) editAction(name string) {
	switch name {
	case "edit_save":
		a.saveEdit()
	case "edit_copy":
		a.copyEdit()
	case "edit_done":
		a.leaveEdit()
	}
}

// renderEdit writes the image with its edits, at its size, as a PNG.
func (a *App) renderEdit(path string) bool {
	e := &a.edit
	renderer := a.win.GetRenderer()
	if renderer == nil {
		return false
	}
	b := image.Rect(0, 0, int(e.tex.GetWidth()), int(e.tex.GetHeight()))
	tex, err := e.ed.Render(renderer, e.tex, b)
	if err != nil {
		log.Printf("viewer: edit: %v", err)
		return false
	}
	defer tex.Unref()
	return tex.SaveToPng(path)
}

// saveEdit asks where to save the image as edited, a PNG beside it named
// after it offered.
func (a *App) saveEdit() {
	e := &a.edit
	if e.dialog != nil {
		return
	}
	d := gtk.NewFileDialog()
	d.SetTitle(i18n.T("Save the Edited Image"))
	name := strings.TrimSuffix(filepath.Base(e.path), filepath.Ext(e.path))
	d.SetInitialName(name + "-edited.png")
	f := gio.FileNewForPath(filepath.Dir(e.path))
	d.SetInitialFolder(f)
	unrefObject(f)
	e.dialog = d
	d.Save(&a.win.Window, nil, &e.onSaved, 0)
}

func (a *App) savedEdit(result uintptr) {
	e := &a.edit
	d := e.dialog
	e.dialog = nil
	if d == nil {
		return
	}
	defer d.Unref()
	file, err := d.SaveFinish(&gio.AsyncResultBase{Ptr: result})
	if err != nil || file == nil || !e.on {
		return // cancelled
	}
	path := file.GetPath()
	unrefObject(file)
	if !strings.EqualFold(filepath.Ext(path), ".png") {
		path += ".png" // what is written is a PNG
	}
	if !a.renderEdit(path) {
		a.flash(i18n.T("Could not save"))
		return
	}
	e.saved, e.leaving = true, false
	a.flash(i18n.Tf("Saved as %s", filepath.Base(path)))
	a.addFile(path)
}

// copyEdit puts the image as edited on the clipboard.
func (a *App) copyEdit() {
	tmp, err := os.CreateTemp("", "inua-viewer-edit-*.png")
	if err != nil {
		a.flash(i18n.T("Could not copy"))
		return
	}
	tmp.Close()
	if !a.renderEdit(tmp.Name()) {
		os.Remove(tmp.Name())
		a.flash(i18n.T("Could not copy"))
		return
	}
	go func() {
		defer os.Remove(tmp.Name())
		f, err := os.Open(tmp.Name())
		if err == nil {
			cmd := exec.Command("wl-copy", "--type", "image/png")
			cmd.Stdin = f
			err = cmd.Run()
			f.Close()
		}
		gtkx.Post(func() {
			if err != nil {
				log.Printf("viewer: copy: %v", err)
				a.flash(i18n.T("Could not copy"))
				return
			}
			a.flash(i18n.T("Copied"))
		})
	}()
}

// addFile puts a file just saved beside the one shown in the list, when
// it is in the same folder, so stepping through finds it.
func (a *App) addFile(path string) {
	cur := a.current()
	if cur == "" || filepath.Dir(cur) != filepath.Dir(path) {
		return
	}
	for _, p := range a.files {
		if p == path {
			return
		}
	}
	a.files = append(a.files[:a.index+1], append([]string{path}, a.files[a.index+1:]...)...)
}

// rgbaTexture makes a texture of premultiplied RGBA pixels.
func rgbaTexture(px *image.RGBA) *gdk.Texture {
	b := glib.NewBytes(px.Pix, uint(len(px.Pix)))
	tex := gdk.NewMemoryTexture(int32(px.Rect.Dx()), int32(px.Rect.Dy()), gdk.MemoryR8g8b8a8PremultipliedValue, b, uint(px.Stride))
	b.Unref()
	return &tex.Texture
}
