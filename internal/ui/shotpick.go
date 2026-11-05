package ui

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/capture/hyprland"
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/uikit"
)

// Window and Screen pick from a grid instead of the still: every window
// on every workspace, grouped by workspace under a title (the ones shown
// now marked), or every screen, each a card with a live picture, laid out
// in justified rows that keep each card's shape. The cards rise into
// place one after another as the grid opens and lift under the pointer;
// arrows and Enter pick by keys. A window is taken whole through
// Hyprland's toplevel export, shown or not. With Record on, a window or a
// screen is clicked on the still as before: what is recorded must be on
// the screen.

// pickCard is one card of the grid.
type pickCard struct {
	btn     *gtk.Button
	pic     *gtk.Picture
	address string       // a window's
	screen  *shotSurface // a screen's
	w, h    int          // its picture's pixels, for the live pictures
	tex     *gdk.Texture
}

type shotPicker struct {
	on      *shotSurface // the surface it shows on, nil when shut
	kind    string       // "window" or "screen"
	cards   []*pickCard
	byBtn   map[uintptr]*pickCard
	stream  *hyprland.Stream
	gen     uint64
	onClick func(gtk.Button)

	mu      sync.Mutex
	pending map[string]hyprland.Frame // the latest frame of each window, to show
	drain   bool
}

// pickerWanted says the grid shows in this mode: Window or Screen, not
// recording.
func (s *shellUI) pickerWanted() bool {
	sh := &s.shot
	return (sh.mode == "window" || sh.mode == "screen") && !sh.record && len(sh.surfaces) > 0
}

// syncPicker shows the grid for the mode, or puts it away.
func (s *shellUI) syncPicker() {
	sh := &s.shot
	pk := &sh.picker
	if !s.pickerWanted() {
		s.closePicker()
		return
	}
	if pk.on != nil && pk.kind == sh.mode {
		return
	}
	s.closePicker()
	var on *shotSurface
	for _, sf := range sh.surfaces {
		if sf.focusedMon {
			on = sf
		}
	}
	if on == nil {
		on = sh.surfaces[0]
	}
	pk.on, pk.kind = on, sh.mode
	pk.byBtn = map[uintptr]*pickCard{}
	if pk.onClick == nil {
		pk.onClick = func(b gtk.Button) {
			if c := pk.byBtn[b.GoPointer()]; c != nil {
				gtkx.Post(func() { s.pickCard(c) }) // the card lives in the grid
			}
		}
	}
	s.fillPicker()
	on.picker.SetVisible(true)
	if len(pk.cards) > 0 {
		pk.cards[0].btn.GrabFocus()
	}
	if pk.kind == "window" {
		s.startPickerStream()
	}
}

// fillPicker makes the grid: a section for each workspace, or one of the
// screens.
func (s *shellUI) fillPicker() {
	sh := &s.shot
	pk := &sh.picker
	sf := pk.on
	gtkx.Clear(&sf.pickerBody)
	width := math.Min(sf.w-2*96, 1600)
	// Few windows show larger.
	count := len(sh.surfaces)
	if pk.kind == "window" {
		count = 0
		if gs, err := shell.PickGroups(sh.clients, sh.shownWS); err == nil {
			for _, g := range gs {
				count += len(g.Windows)
			}
		}
	}
	target := math.Max(140, math.Min(sf.h/4.6, 260))
	if count <= 4 {
		target = math.Max(160, math.Min(sf.h/3.2, 340))
	}
	n := 0 // the cards made, for their entrance one after another
	section := func(title, meta string, shown bool, aspects []float64, card func(i int, t shell.Tile) *pickCard) {
		head := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		head.AddCssClass("shot-pick-head")
		head.AddCssClass(fmt.Sprintf("shot-pick-d%d", min(n, 23)))
		mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		mark.AddCssClass("mark")
		if shown {
			mark.AddCssClass("on")
		}
		mark.SetValign(gtk.AlignCenterValue)
		head.Append(&mark.Widget)
		l := gtk.NewLabel("")
		l.SetMarkup(uikit.Bracketed(title))
		l.AddCssClass("shot-pick-title")
		head.Append(&l.Widget)
		m := gtk.NewLabel(meta)
		m.AddCssClass("shot-pick-meta")
		head.Append(&m.Widget)
		sf.pickerBody.Append(&head.Widget)

		tiles, height := shell.Justify(aspects, width, target, target*1.5, 14)
		grid := gtk.NewFixed()
		grid.SetSizeRequest(int32(width), int32(math.Ceil(height)))
		grid.AddCssClass("shot-pick-grid")
		for i, t := range tiles {
			c := card(i, t)
			c.btn.SetSizeRequest(int32(t.W), int32(t.H))
			c.btn.AddCssClass(fmt.Sprintf("shot-pick-d%d", min(n, 23)))
			c.btn.ConnectClicked(&pk.onClick)
			grid.Put(&c.btn.Widget, t.X, t.Y)
			pk.byBtn[c.btn.GoPointer()] = c
			pk.cards = append(pk.cards, c)
			n++
		}
		sf.pickerBody.Append(&grid.Widget)
	}
	newCard := func(title, sub string, t shell.Tile) *pickCard {
		c := &pickCard{btn: gtk.NewButton()}
		c.btn.AddCssClass("shot-pick-card")
		c.btn.SetOverflow(gtk.OverflowHiddenValue) // the picture inside its corners
		over := gtk.NewOverlay()
		c.pic = gtk.NewPicture()
		c.pic.SetContentFit(gtk.ContentFitCoverValue)
		c.pic.SetCanShrink(true)
		c.pic.AddCssClass("shot-pick-pic")
		over.SetChild(&c.pic.Widget)
		cap := gtk.NewBox(gtk.OrientationVerticalValue, 1)
		cap.AddCssClass("shot-pick-caption")
		cap.SetValign(gtk.AlignEndValue)
		tl := gtk.NewLabel(title)
		tl.AddCssClass("shot-pick-name")
		tl.SetXalign(0)
		tl.SetEllipsize(3)
		cap.Append(&tl.Widget)
		if sub != "" {
			sl := gtk.NewLabel(sub)
			sl.AddCssClass("shot-pick-sub")
			sl.SetXalign(0)
			sl.SetEllipsize(3)
			cap.Append(&sl.Widget)
		}
		over.AddOverlay(&cap.Widget)
		c.btn.SetChild(&over.Widget)
		c.btn.SetTooltipText(title)
		c.w, c.h = int(t.W*sf.scale), int(t.H*sf.scale)
		return c
	}
	if pk.kind == "screen" {
		var aspects []float64
		for _, o := range sh.surfaces {
			aspects = append(aspects, o.w/o.h)
		}
		section(i18n.T("SCREENS"), i18n.Nf("%d screen", "%d screens", len(sh.surfaces), len(sh.surfaces)), true, aspects, func(i int, t shell.Tile) *pickCard {
			o := sh.surfaces[i]
			c := newCard(o.name, fmt.Sprintf("%d × %d", o.bounds.Dx(), o.bounds.Dy()), t)
			c.screen = o
			c.pic.SetFilename(o.file)
			return c
		})
		return
	}
	groups, err := shell.PickGroups(sh.clients, sh.shownWS)
	if err != nil {
		log.Printf("screenshot: %v", err)
	}
	if len(groups) == 0 {
		l := gtk.NewLabel(i18n.T("No windows are open."))
		l.AddCssClass("shot-pick-empty")
		sf.pickerBody.Append(&l.Widget)
		return
	}
	for _, g := range groups {
		var aspects []float64
		for _, w := range g.Windows {
			aspects = append(aspects, w.W/w.H)
		}
		title := i18n.Tf("WORKSPACE %s", g.Name)
		if g.ID < 0 {
			title = g.Name // special:name
		}
		section(title, i18n.Nf("%d window", "%d windows", len(g.Windows), len(g.Windows)), g.Shown, aspects, func(i int, t shell.Tile) *pickCard {
			w := g.Windows[i]
			name := w.Title
			if name == "" {
				name = w.Class
			}
			c := newCard(name, w.Class, t)
			c.address = w.Address
			return c
		})
	}
}

// startPickerStream keeps the windows' pictures live, each at its card's
// size.
func (s *shellUI) startPickerStream() {
	pk := &s.shot.picker
	sizes := map[string][2]int{}
	var targets []string
	for _, c := range pk.cards {
		if c.address != "" {
			targets = append(targets, c.address)
			sizes[c.address] = [2]int{c.w, c.h}
		}
	}
	if len(targets) == 0 || s.hypr == nil || !s.caps.Has(compositor.CapPreviews) {
		return
	}
	pk.gen++
	gen := pk.gen
	pk.stream = hyprland.Start(context.Background(), targets, func(f hyprland.Frame) {
		sz := sizes[f.Address]
		f = hyprland.Shrink(f, sz[0], sz[1])
		pk.mu.Lock()
		if pk.pending == nil {
			pk.pending = map[string]hyprland.Frame{}
		}
		pk.pending[f.Address] = f
		queue := !pk.drain
		pk.drain = true
		pk.mu.Unlock()
		if queue {
			gtkx.Post(func() { s.drainPicker(gen) })
		}
	}, func(err error) { log.Printf("screenshot: window pictures: %v", err) })
}

// drainPicker shows the frames come since the last time.
func (s *shellUI) drainPicker(gen uint64) {
	pk := &s.shot.picker
	pk.mu.Lock()
	frames := pk.pending
	pk.pending, pk.drain = nil, false
	pk.mu.Unlock()
	if gen != pk.gen || pk.on == nil {
		return
	}
	for _, c := range pk.cards {
		f, ok := frames[c.address]
		if !ok {
			continue
		}
		tex := frameTexture(f)
		if tex == nil {
			continue
		}
		c.pic.SetPaintable(tex)
		if c.tex != nil {
			c.tex.Unref()
		}
		c.tex = tex
	}
}

// frameTexture is a captured frame as a texture; nil for a format it
// does not know.
func frameTexture(f hyprland.Frame) *gdk.Texture {
	format, ok := frameFormat(f.Format)
	if !ok {
		return nil
	}
	b := glib.NewBytes(f.Pixels, uint(len(f.Pixels)))
	defer b.Unref()
	t := gdk.NewMemoryTexture(int32(f.Width), int32(f.Height), format, b, uint(f.Stride))
	if t == nil {
		return nil
	}
	return &t.Texture
}

// frameFormat is GDK's name for a frame's pixel format.
func frameFormat(f uint32) (gdk.MemoryFormat, bool) {
	switch f {
	case 0, 0x34325241:
		return gdk.MemoryB8g8r8a8PremultipliedValue, true
	case 1, 0x34325258:
		return gdk.MemoryB8g8r8x8Value, true
	case 0x34324241:
		return gdk.MemoryR8g8b8a8PremultipliedValue, true
	case 0x34324258:
		return gdk.MemoryR8g8b8x8Value, true
	}
	return 0, false
}

// closePicker puts the grid away and stops its pictures.
func (s *shellUI) closePicker() {
	pk := &s.shot.picker
	if pk.on == nil {
		return
	}
	pk.gen++
	pk.stream.Stop()
	pk.stream = nil
	for _, c := range pk.cards {
		if c.tex != nil {
			c.tex.Unref()
		}
	}
	pk.on.picker.SetVisible(false)
	gtkx.Clear(&pk.on.pickerBody)
	pk.on, pk.cards, pk.byBtn = nil, nil, nil
}

// pickCard takes what a card stands for: its screen as the still has it,
// or its window whole, captured now.
func (s *shellUI) pickCard(c *pickCard) {
	sh := &s.shot
	if !sh.open {
		return
	}
	if c.screen != nil {
		s.takeShot(c.screen, shell.Rect{W: c.screen.w, H: c.screen.h})
		return
	}
	if p := sh.portal; p != nil && p.share != nil { // to share, not to save
		sh.portal = nil
		p.share <- "window:" + c.address
		s.closeCapture("")
		return
	}
	s.closePicker()
	address := c.address
	go func() {
		img, err := captureWindow(address)
		gtkx.Post(func() {
			if err != nil {
				log.Printf("screenshot: window: %v", err)
				s.closeCapture("")
				return
			}
			s.deliverWindow(img)
		})
	}()
}

// deliverWindow saves and copies a window taken whole, or gives it to the
// app that asked for a picture.
func (s *shellUI) deliverWindow(img *image.RGBA) {
	if s.shot.portal != nil {
		dir, err := shotTempDir()
		if err == nil {
			file := filepath.Join(dir, "window-"+strconv.FormatInt(time.Now().UnixNano(), 36)+".png")
			if err = writePNG(file, img); err == nil && s.portalTake(file, img.Bounds()) {
				s.closeCapture("")
				return
			}
		}
		if err != nil {
			log.Printf("screenshot: %v", err)
		}
	}
	s.closeCapture("")
	go func() {
		err := s.deliverShot(func(path string) error {
			f, err := os.Create(path)
			if err != nil {
				return err
			}
			enc := png.Encoder{CompressionLevel: png.BestSpeed}
			if err := enc.Encode(f, img); err != nil {
				f.Close()
				return err
			}
			return f.Close()
		})
		if err != nil {
			log.Printf("screenshot: %v", err)
			return
		}
		gtkx.Post(s.shotTaken)
	}()
}

// captureWindow takes one frame of a window, at its own size, through
// Hyprland's toplevel export: shown or not.
func captureWindow(address string) (*image.RGBA, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got := make(chan hyprland.Frame, 1)
	failed := make(chan error, 1)
	stream := hyprland.Start(ctx, []string{address}, func(f hyprland.Frame) {
		select {
		case got <- f:
		default:
		}
	}, func(err error) { failed <- err })
	defer stream.Stop()
	select {
	case f := <-got:
		return frameImage(f)
	case err := <-failed:
		return nil, err
	case <-ctx.Done():
		return nil, fmt.Errorf("no picture of the window came")
	}
}

// frameImage is a frame as RGBA pixels, upright.
func frameImage(f hyprland.Frame) (*image.RGBA, error) {
	img := image.NewRGBA(image.Rect(0, 0, f.Width, f.Height))
	bgr, alpha := false, true
	switch f.Format {
	case 0, 0x34325241:
		bgr = true
	case 1, 0x34325258:
		bgr, alpha = true, false
	case 0x34324241:
	case 0x34324258:
		alpha = false
	default:
		return nil, fmt.Errorf("pixel format %#x", f.Format)
	}
	for y := range f.Height {
		sy := y
		if f.YInvert {
			sy = f.Height - 1 - y
		}
		src := f.Pixels[sy*f.Stride : sy*f.Stride+f.Width*4]
		dst := img.Pix[y*img.Stride : y*img.Stride+f.Width*4]
		for x := 0; x < f.Width*4; x += 4 {
			r, g, b, a := src[x], src[x+1], src[x+2], src[x+3]
			if bgr {
				r, b = b, r
			}
			if !alpha {
				a = 255
			}
			dst[x], dst[x+1], dst[x+2], dst[x+3] = r, g, b, a
		}
	}
	return img, nil
}
