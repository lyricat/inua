package ui

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg" // pictures saved elsewhere may be JPEGs
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"golang.org/x/image/draw"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/shell"
)

// The screenshot tool's drawer: once a picture or a recording is taken,
// the still goes and this shows instead, where the toolbar was: the recent
// screenshots and recordings over a small toolbar. The windows are live
// again, so an item dragged out drops into any of them; a drop puts the
// drawer away. Area, Window and Screen take another, Record on to record.

const (
	drawerItems = 12  // how many it lists
	thumbW      = 132 // an item's picture, in logical pixels
	thumbH      = 84
)

type shotDrawer struct {
	win    gtk.ApplicationWindow
	items  gtk.Box
	empty  gtk.Label
	bar    gtk.Box
	hint   gtk.Label
	record gtk.ToggleButton
	refs   gtkx.Scope
	shown  []shell.ShotItem
	pics   []*gtk.Picture // each item's picture, owned by its button
	gen    int            // counts fillings, so late thumbnails can tell they are stale

	cancelled bool // the drag under way was cancelled
}

func (s *shellUI) bindShotDrawerHandlers() {
	sh := &s.shot
	// Callbacks that put the drawer away do it after the signal: the
	// widget that sent it lives in the drawer.
	later := func(f func()) { gtkx.Post(f) }
	s.onShotDrawer = func(gtk.Button) {
		later(func() {
			s.closeCapture("")
			s.openShotDrawer()
		})
	}
	s.onShotTake = func(b gtk.Button) {
		mode := b.GetName()
		if d := sh.drawer; d != nil {
			sh.record = d.record.GetActive()
		}
		later(func() { s.openShot(mode) })
	}
	s.onShotDrawerRecord = func(b gtk.ToggleButton) {
		if d := sh.drawer; d != nil {
			s.showDrawerRecord(d)
		}
	}
	s.onShotDrawerClose = func(gtk.Button) { later(func() { s.closeShot("") }) }
	s.onShotDrawerKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if keyval != keyEscape {
			return false
		}
		later(func() { s.closeShot("") })
		return true
	}
	s.onShotReopen = func(uintptr) bool {
		if mode := sh.reopen; mode != "" {
			sh.reopen = ""
			s.openShot(mode)
		}
		return false
	}
	s.onShotItem = func(b gtk.Button) {
		if it, ok := s.shotItemOf(&b.Widget); ok {
			later(func() { s.closeShot("") })
			go func() {
				if err := exec.Command("xdg-open", it.Path).Run(); err != nil {
					log.Printf("screenshot: open %s: %v", it.Path, err)
				}
			}()
		}
	}
	s.onShotItemDragBegin = func(ds gtk.DragSource, _ uintptr) {
		d := sh.drawer
		w := ds.GetWidget()
		if d == nil || w == nil {
			return
		}
		d.cancelled = false
		if i, ok := shotItemIndex(w); ok && i < len(d.pics) {
			p := gtk.NewWidgetPaintable(&d.pics[i].Widget)
			ds.SetIcon(p, thumbW/2, thumbH/2)
			p.Unref()
		}
	}
	s.onShotItemDragCancel = func(gtk.DragSource, uintptr, gdk.DragCancelReason) bool {
		if d := sh.drawer; d != nil {
			d.cancelled = true
		}
		return false // GTK's own animation back
	}
	s.onShotItemDragEnd = func(gtk.DragSource, uintptr, bool) {
		if d := sh.drawer; d != nil && !d.cancelled {
			later(func() { s.closeShot("") }) // dropped: done
		}
	}
}

// openShotDrawer shows the drawer on the focused screen, or fills it again
// if it is open.
func (s *shellUI) openShotDrawer() {
	sh := &s.shot
	if sh.drawer == nil {
		s.buildShotDrawer()
		if sh.drawer == nil {
			return
		}
	}
	s.fillShotDrawer()
}

func (s *shellUI) closeShotDrawer() {
	sh := &s.shot
	d := sh.drawer
	if d == nil {
		return
	}
	sh.drawer = nil
	d.gen++
	d.win.Destroy()
	d.refs.Release()
}

// showDrawerRecord marks the drawer's toolbar red while Record is on.
func (s *shellUI) showDrawerRecord(d *shotDrawer) {
	on := d.record.GetActive()
	toggle(&d.bar.Widget, "recording", on)
	if on {
		d.hint.SetText(i18n.T("Record another"))
	} else {
		d.hint.SetText(i18n.T("Take another"))
	}
}

func (s *shellUI) buildShotDrawer() {
	sh := &s.shot
	mon := s.monitorByName(s.snap.FocusedOutput)
	if mon == nil {
		for _, b := range s.bars { // any screen rather than none
			mon = b.mon
			break
		}
	}
	if mon == nil {
		return
	}
	d := &shotDrawer{}
	var area, window, screen, closeBtn gtk.Button
	ui := s.load("shotdrawer.ui", &d.refs)
	ui.Get("shot_drawer", &d.win)
	ui.Get("drawer_items", &d.items)
	ui.Get("drawer_empty", &d.empty)
	ui.Get("drawer_bar", &d.bar)
	ui.Get("drawer_bar_hint", &d.hint)
	ui.Get("take_record", &d.record)
	ui.Get("take_area", &area)
	ui.Get("take_window", &window)
	ui.Get("take_screen", &screen)
	ui.Get("drawer_close", &closeBtn)
	ui.Close()
	for _, b := range []*gtk.Button{&area, &window, &screen} {
		b.ConnectClicked(&s.onShotTake)
	}
	closeBtn.ConnectClicked(&s.onShotDrawerClose)
	d.record.SetActive(sh.record)
	s.showDrawerRecord(d)
	d.record.ConnectToggled(&s.onShotDrawerRecord)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(&s.onShotDrawerKey)
	d.win.AddController(&keys.EventController)

	d.win.SetApplication(s.app)
	w := d.win.GoPointer()
	if s.layer {
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-shot-drawer")
		layershell.SetLayer(w, layershell.LayerOverlay)
		layershell.SetKeyboardMode(w, layershell.KeyboardOnDemand) // Escape
		layershell.SetAnchor(w, layershell.EdgeBottom, true)
		// The toolbar where the tool's was, 48 px up (screenshot.blp),
		// less the margin around the panel that leaves room for shadows.
		layershell.SetMargin(w, layershell.EdgeBottom, 48-16)
		layershell.SetMonitor(w, mon.GoPointer())
	} else {
		d.win.SetTitle(i18n.T("inua recent screenshots"))
	}
	sh.drawer = d
	d.win.Present()
}

// fillShotDrawer lists the recent screenshots and recordings, their
// pictures made (or read from the cache) off the main loop.
func (s *shellUI) fillShotDrawer() {
	d := s.shot.drawer
	d.gen++
	gen := d.gen
	tmp, _ := shotTempDir()
	d.shown = shell.RecentShots([]string{s.cfg.ShotFolder(), tmp, s.cfg.RecordingFolder()}, drawerItems)
	d.pics = nil
	gtkx.Clear(&d.items)
	d.empty.SetVisible(len(d.shown) == 0)
	for i, it := range d.shown {
		d.pics = append(d.pics, s.shotItemButton(d, i, it))
	}
	items := d.shown
	go func() {
		for i, it := range items {
			thumb, err := shotThumb(it)
			if err != nil {
				log.Printf("screenshot: thumbnail %s: %v", filepath.Base(it.Path), err)
				continue
			}
			gtkx.Post(func() {
				if d := s.shot.drawer; d != nil && d.gen == gen && i < len(d.pics) {
					d.pics[i].SetFilename(thumb)
				}
			})
		}
	}()
}

// shotItemButton adds an item to the drawer: its picture over when it was
// taken, a click to open it, a drag to give its file.
func (s *shellUI) shotItemButton(d *shotDrawer, i int, it shell.ShotItem) *gtk.Picture {
	btn := gtk.NewButton()
	btn.AddCssClass("shot-item")
	btn.SetName("item:" + strconv.Itoa(i))
	btn.SetTooltipText(filepath.Base(it.Path))
	box := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	over := gtk.NewOverlay()
	pic := gtk.NewPicture()
	pic.AddCssClass("shot-thumb")
	pic.SetContentFit(gtk.ContentFitCoverValue)
	pic.SetCanShrink(true)
	pic.SetSizeRequest(thumbW, thumbH)
	over.SetChild(&pic.Widget)
	pic.Unref()
	if it.Video {
		badge := gtk.NewImageFromIconName("media-playback-start-symbolic")
		badge.AddCssClass("shot-video-badge")
		badge.SetHalign(gtk.AlignCenterValue)
		badge.SetValign(gtk.AlignCenterValue)
		badge.SetCanTarget(false)
		over.AddOverlay(&badge.Widget)
		badge.Unref()
	}
	gtkx.Append(box, &over.Widget)
	caption := gtk.NewLabel(shotWhen(it.Time, time.Now()))
	caption.AddCssClass("shot-item-caption")
	caption.SetXalign(0)
	gtkx.Append(box, &caption.Widget)
	btn.SetChild(&box.Widget)
	box.Unref()
	btn.ConnectClicked(&s.onShotItem)

	// The file, as file managers, browsers and most apps take a drop.
	uri := (&url.URL{Scheme: "file", Path: it.Path}).String() + "\r\n"
	bytes := glib.NewBytes([]byte(uri), uint(len(uri)))
	content := gdk.NewContentProviderForBytes("text/uri-list", bytes)
	bytes.Unref()
	drag := gtk.NewDragSource()
	drag.SetActions(gdk.ActionCopyValue)
	drag.SetContent(content)
	content.Unref()
	drag.ConnectDragBegin(&s.onShotItemDragBegin)
	drag.ConnectDragCancel(&s.onShotItemDragCancel)
	drag.ConnectDragEnd(&s.onShotItemDragEnd)
	btn.AddController(&drag.EventController)
	gtkx.Append(&d.items, &btn.Widget)
	return pic
}

func shotItemIndex(w *gtk.Widget) (int, bool) {
	n, ok := strings.CutPrefix(w.GetName(), "item:")
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(n)
	return i, err == nil
}

func (s *shellUI) shotItemOf(w *gtk.Widget) (shell.ShotItem, bool) {
	d := s.shot.drawer
	if i, ok := shotItemIndex(w); ok && d != nil && i < len(d.shown) {
		return d.shown[i], true
	}
	return shell.ShotItem{}, false
}

// shotWhen says when an item was taken: the time today, else the day.
func shotWhen(t, now time.Time) string {
	if y, m, d := t.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		return t.Format("15:04")
	}
	if t.Year() == now.Year() {
		return i18n.Time(t, "Jan 2 15:04")
	}
	return i18n.Time(t, "Jan 2 2006")
}

// shotThumb is a small picture of a screenshot or a recording's first
// moment, made once and kept in the cache, named for the file's path,
// time and size.
func shotThumb(it shell.ShotItem) (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".cache")
	}
	dir := filepath.Join(base, "inua", "shot-thumbs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	sum := sha1.Sum([]byte(fmt.Sprintf("%s\x00%d\x00%d", it.Path, it.Time.UnixNano(), it.Size)))
	out := filepath.Join(dir, hex.EncodeToString(sum[:])+".png")
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}
	tmp := out + ".tmp.png"
	defer os.Remove(tmp)
	// Twice the drawer's size, sharp on a scaled screen.
	if it.Video {
		if err := videoFrame(it.Path, tmp, 2*thumbW); err != nil {
			return "", err
		}
	} else if err := scaleImage(it.Path, tmp, 2*thumbW, 2*thumbH); err != nil {
		return "", err
	}
	return out, os.Rename(tmp, out)
}

func scaleImage(src, dst string, maxW, maxH int) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return err
	}
	b := img.Bounds()
	w, h := shell.ThumbSize(b.Dx(), b.Dy(), maxW, maxH)
	small := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(small, small.Bounds(), img, b, draw.Src, nil)
	return writePNG(dst, small)
}

// videoFrame has ffmpeg save a frame from just after a video's start, or
// its very first for one shorter than that.
func videoFrame(src, dst string, width int) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return err
	}
	var last error
	for _, at := range []string{"0.5", "0"} {
		out, err := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error", "-y", "-ss", at, "-i", src,
			"-frames:v", "1", "-vf", fmt.Sprintf("scale=%d:-2", width), dst).CombinedOutput()
		if st, serr := os.Stat(dst); err == nil && serr == nil && st.Size() > 0 {
			return nil
		}
		last = fmt.Errorf("ffmpeg: %v %s", err, strings.TrimSpace(string(out)))
	}
	return last
}
