package ui

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gsk"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/oned"
	"github.com/makiuchi-d/gozxing/qrcode"
	qrdecoder "github.com/makiuchi-d/gozxing/qrcode/decoder"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
	"github.com/lyricat/inua/internal/upgrade"
)

// Settings → About's system manifest: the card shell.Card says, drawn as a
// slip the ship prints of itself (the slip, a status stamp, a line to
// share), in the theme's colours. One drawing makes the page's card and
// the image: the page draws it at cardView's scale, Copy image and Save
// render it at twice its size.

// The card's size, as shared: 16:9, whole in a post.
const cardW, cardH = 1200, 675

// cardView is the scale the page shows it at.
const cardView = 0.72

// cardCodes are the card's barcode (of its serial) and QR code (of the
// release's page), made once when it is read.
type cardCodes struct{ bar, qr *gozxing.BitMatrix }

// fetchSystem reads the card off the main thread each time the About page
// is shown (the uptime and the readings move), and shows it.
func (s *shellUI) fetchSystem() {
	a := &s.settings.about
	if a.fetching {
		return
	}
	a.fetching = true
	client := s.hyprClient()
	inua := strings.TrimPrefix(s.opts.Version, "v")
	link := upgrade.Repo
	if upgrade.IsRelease(s.opts.Version) {
		link = upgrade.Repo + "/releases/tag/v" + inua
	}
	var screens []shell.CardScreen
	for _, m := range s.snap.Outputs {
		sc := shell.CardScreen{Width: m.Width, Height: m.Height, Scale: m.Scale}
		if m.RefreshRate > 0 {
			sc.Hz = math.Round(m.RefreshRate)
		}
		screens = append(screens, sc)
	}
	bat := s.sys.Bat
	css := s.cfg.ThemeCSS(s.theme(), s.readUI)
	theme := shell.ThemeTitle(css, s.theme())
	colors := config.ThemeColors(css)
	go func() {
		info := system.ReadSysInfo("/")
		wm := "Hyprland"
		if client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			if b, err := client.Request(ctx, "j/version"); err == nil {
				var v struct{ Tag, Version string }
				if json.Unmarshal(b, &v) == nil {
					wm += " " + strings.TrimPrefix(cmp.Or(v.Tag, v.Version), "v")
				}
			} else {
				log.Printf("about: hyprland version: %v", err)
			}
			cancel()
		}
		card := shell.NewCard(info, wm, inua, screens, bat, theme, link, time.Now())
		codes := makeCardCodes(card)
		gtkx.Post(func() {
			a.fetching = false
			a.card, a.cardCodes, a.cardColors = &card, codes, colors
			s.showCard()
		})
	}()
}

// makeCardCodes encodes the serial as Code 128 and the link as a QR code;
// a code that cannot be made is left out.
func makeCardCodes(c shell.Card) cardCodes {
	var out cardCodes
	margin := map[gozxing.EncodeHintType]interface{}{gozxing.EncodeHintType_MARGIN: 0}
	if m, err := oned.NewCode128Writer().Encode(c.Serial, gozxing.BarcodeFormat_CODE_128, 0, 1, margin); err == nil {
		out.bar = m
	} else {
		log.Printf("about: barcode: %v", err)
	}
	qrHints := map[gozxing.EncodeHintType]interface{}{
		gozxing.EncodeHintType_MARGIN:           0,
		gozxing.EncodeHintType_ERROR_CORRECTION: qrdecoder.ErrorCorrectionLevel_M,
	}
	if c.Link != "" {
		if m, err := qrcode.NewQRCodeWriter().Encode(c.Link, gozxing.BarcodeFormat_QR_CODE, 0, 0, qrHints); err == nil {
			out.qr = m
		} else {
			log.Printf("about: qr code: %v", err)
		}
	}
	return out
}

// showCard puts the card on the page, drawn smaller, with a line under it
// for what Copy and Save did.
func (s *shellUI) showCard() {
	a := &s.settings.about
	if s.settings.win == nil {
		return
	}
	for _, b := range []*gtk.Button{&a.fetchCopy, &a.cardImage, &a.cardSav} {
		b.SetSensitive(a.card != nil)
	}
	if a.cardView == nil {
		gtkx.Clear(&a.fetch)
		a.cardView = gtkx.NewCanvas(&gtkx.Canvas{Snapshot: func(w *gtk.Widget, sn *gtk.Snapshot) {
			// Drawn straight into the page, as its own text is: an image of
			// the card, even one to one, comes out blurred at a fractional
			// scale. Its text is set at the size it shows at (cardPen.text).
			if a.card != nil {
				k := float32(w.GetWidth()) / cardW
				sn.Scale(k, k)
				drawCard(w, sn, a.card, a.cardCodes, a.cardColors, k)
			}
		}})
		a.cardView.SetSizeRequest(int32(cardW*cardView), int32(cardH*cardView))
		a.cardView.SetHalign(gtk.AlignStartValue)
		a.cardView.AddCssClass("card-view")
		a.fetch.Append(a.cardView) // the box takes its floating reference

		a.cardNote = gtk.NewLabel("")
		a.cardNote.AddCssClass("sub")
		a.cardNote.SetXalign(0)
		a.cardNote.SetSelectable(true)
		a.cardNote.SetWrap(true)
		gtkx.Append(&a.fetch, &a.cardNote.Widget)
	}
	if a.card == nil {
		a.cardNote.SetText(i18n.T("Reading…"))
	} else if a.cardNote.GetText() == i18n.T("Reading…") {
		a.cardNote.SetText("")
	}
	a.cardView.QueueDraw() // a new reading
}

// cardTexture draws the card at scale into an image; nil when the
// window cannot draw now.
func (s *shellUI) cardTexture(scale float64) *gdk.Texture {
	a := &s.settings.about
	if s.settings.win == nil || a.card == nil || a.cardView == nil {
		return nil
	}
	r := s.settings.win.GetRenderer()
	if r == nil {
		return nil
	}
	sn := gtk.NewSnapshot()
	sn.Scale(float32(scale), float32(scale))
	drawCard(a.cardView, sn, a.card, a.cardCodes, a.cardColors, float32(scale))
	node := sn.FreeToNode()
	if node == nil {
		return nil
	}
	defer node.Unref()
	return r.RenderTexture(node, cardRect(0, 0, float32(math.Round(cardW*scale)), float32(math.Round(cardH*scale))))
}

// cardSay says under the card what Copy or Save did.
func (s *shellUI) cardSay(text string, failed bool) {
	a := &s.settings.about
	if s.settings.win == nil || a.cardNote == nil {
		return
	}
	a.cardNote.SetText(text)
	toggle(&a.cardNote.Widget, "error", failed)
}

// copyCardText copies the card as Markdown.
func (s *shellUI) copyCardText() {
	a := &s.settings.about
	if a.card == nil {
		return
	}
	text := a.card.Text()
	go func() {
		err := copyText(text)
		gtkx.Post(func() {
			if err != nil {
				s.cardSay(i18n.Tf("Not copied: %s", err.Error()), true)
				return
			}
			s.cardSay(i18n.T("Copied as text."), false)
		})
	}()
}

// renderCard draws the card at twice its size into a PNG at path.
func (s *shellUI) renderCard(path string) error {
	tex := s.cardTexture(2)
	if tex == nil {
		return fmt.Errorf("%s", i18n.T("the window cannot draw it now"))
	}
	defer tex.Unref()
	if !tex.SaveToPng(path) {
		return fmt.Errorf("%s", i18n.Tf("%s cannot be written", path))
	}
	return nil
}

// copyCardImage puts the card on the clipboard as a PNG.
func (s *shellUI) copyCardImage() {
	dir := cmp.Or(os.Getenv("XDG_RUNTIME_DIR"), os.TempDir())
	path := filepath.Join(dir, "inua-manifest.png")
	if err := s.renderCard(path); err != nil {
		s.cardSay(i18n.Tf("Not copied: %s", err.Error()), true)
		return
	}
	go func() {
		err := copyImage(path)
		gtkx.Post(func() {
			if err != nil {
				s.cardSay(i18n.Tf("Not copied: %s", err.Error()), true)
				return
			}
			s.cardSay(i18n.T("Copied as an image: paste it in a chat or a post."), false)
		})
	}()
}

// saveCard asks where to keep the card's PNG; onCardSaved writes it.
func (s *shellUI) saveCard() {
	st := &s.settings
	a := &st.about
	if st.dialog != nil || a.card == nil {
		return
	}
	d := gtk.NewFileDialog()
	d.SetTitle(i18n.T("Save the card"))
	d.SetInitialName("inua-" + strings.ToLower(a.card.Serial) + ".png")
	if home, err := os.UserHomeDir(); err == nil {
		f := gio.FileNewForPath(filepath.Join(home, "Pictures"))
		d.SetInitialFolder(f)
		unref(f)
	}
	st.dialog = d
	d.Save(&st.win.Window, nil, &s.onCardSaved, 0)
}

func (s *shellUI) bindCardHandlers() {
	s.onCardSaved = func(_, result, _ uintptr) {
		st := &s.settings
		d := st.dialog
		st.dialog = nil
		if d == nil {
			return
		}
		defer d.Unref()
		file, err := d.SaveFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || file == nil || st.win == nil {
			return // cancelled, or the window closed meanwhile
		}
		path := file.GetPath()
		unref(file)
		if err := s.renderCard(path); err != nil {
			s.cardSay(i18n.Tf("Not saved: %s", err.Error()), true)
			return
		}
		s.cardSay(i18n.Tf("Saved as %s", shortenHome(path)), false)
	}
}

// --- drawing ---

// The card's families, as style.css names them.
const (
	cardMono = "Geist Mono, IBM Plex Mono, JetBrains Mono, monospace"
	cardSans = "IBM Plex Sans, Zen Kaku Gothic New, Noto Sans CJK JP, sans-serif"
	cardText = "Zen Kaku Gothic New, Noto Sans CJK JP, Noto Sans CJK SC, sans-serif"
)

// cardFont is a run of the card's text: its family, size in pixels,
// weight, and tracking in ems.
type cardFont struct {
	family string
	px     float64
	weight pango.Weight
	track  float64
}

// cardPen draws on a snapshot at the card's own size, 1200×675.
type cardPen struct {
	w      *gtk.Widget
	s      *gtk.Snapshot
	colors map[string]string
	scale  float32 // what the snapshot is scaled by, for text
}

func (p cardPen) color(token string, alpha float32) gdk.RGBA {
	c := hexRGBA(p.colors[token])
	c.Alpha *= alpha
	return c
}

func cardRect(x, y, w, h float32) *graphene.Rect {
	var r graphene.Rect
	r.Init(x, y, w, h)
	return &r
}

func (p cardPen) fill(token string, alpha, x, y, w, h float32) {
	c := p.color(token, alpha)
	p.s.AppendColor(&c, cardRect(x, y, w, h))
}

// cardMark is inua's mark as a path, parsed the first time it is drawn.
var cardMark *gsk.Path

// mark draws inua's mark in a square of size at (x, y).
func (p cardPen) mark(token string, x, y, size float32) {
	if cardMark == nil {
		cardMark = gsk.PathParse(shell.CardMark)
	}
	c := p.color(token, 1)
	p.s.Save()
	p.s.Translate(&graphene.Point{X: x, Y: y})
	p.s.Scale(size/100, size/100)
	p.s.AppendFill(cardMark, gsk.FillRuleEvenOddValue, &c)
	p.s.Restore()
}

// line is a hairline across, at y.
func (p cardPen) line(token string, x, y, w float32) { p.fill(token, 1, x, y, w, 1) }

// dashes is a dashed hairline across, at y.
func (p cardPen) dashes(token string, x, y, w float32) {
	for at := x; at < x+w; at += 7 {
		p.fill(token, 1, at, y, min(4, x+w-at), 1)
	}
}

// Alignments of a run of text: from x, ending at x, centred on x.
const (
	cardLeft = iota
	cardRight
	cardCenter
)

// text draws a run with its top at y, aligned on x; width > 0 wraps it.
// It returns its size. The run is set at the size it is drawn at, the
// snapshot's scale undone: small text set at the card's size and scaled
// down loses the tops of its glyphs.
func (p cardPen) text(x, y float32, str string, f cardFont, token string, align int, width float32) (float32, float32) {
	k := p.scale
	px := f.px * float64(k)
	l := p.w.CreatePangoLayout("")
	desc := pango.FontDescriptionFromString(f.family)
	desc.SetAbsoluteSize(px * float64(pango.SCALE))
	if f.weight != 0 {
		desc.SetWeight(f.weight)
	}
	l.SetFontDescription(desc)
	desc.Free()
	if f.track != 0 {
		// letter_spacing is in 1024ths of a point; a point is 4/3 pixels.
		spacing := int(f.track * px * 0.75 * 1024)
		l.SetMarkup(fmt.Sprintf(`<span letter_spacing="%d">%s</span>`, spacing, html.EscapeString(str)), -1)
	} else {
		l.SetText(str, -1)
	}
	if width > 0 {
		l.SetWidth(int32(width * k * float32(pango.SCALE)))
		l.SetWrap(pango.WrapWordValue)
	}
	var pw, ph int32
	l.GetPixelSize(&pw, &ph)
	lw, lh := float32(pw)/k, float32(ph)/k
	switch align {
	case cardRight:
		x -= lw
	case cardCenter:
		x -= lw / 2
	}
	c := p.color(token, 1)
	p.s.Save()
	p.s.Translate(&graphene.Point{X: x, Y: y})
	p.s.Scale(1/k, 1/k)
	p.s.AppendLayout(l, &c)
	p.s.Restore()
	l.Unref()
	return lw, lh
}

// hexRGBA is a theme colour, "#rrggbb"; grey when it is missing.
func hexRGBA(hex string) gdk.RGBA {
	v, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	if err != nil || len(hex) != 7 {
		return gdk.RGBA{Red: 0.5, Green: 0.5, Blue: 0.5, Alpha: 1}
	}
	f := func(shift uint) float32 { return float32(v>>shift&0xff) / 255 }
	return gdk.RGBA{Red: f(16), Green: f(8), Blue: f(0), Alpha: 1}
}

// drawCard draws the manifest at 1200×675: the slip on the left, the
// stamp over it, the line to share and the theme's colours on the right.
func drawCard(w *gtk.Widget, s *gtk.Snapshot, c *shell.Card, codes cardCodes, colors map[string]string, scale float32) {
	p := cardPen{w: w, s: s, colors: colors, scale: scale}
	// The ground: the bar's deeper dark in a dark theme, the window's own
	// ground in a light one, where the bar stays dark and the text would
	// not show on it.
	ground := "app"
	if colors["sb"] != "" && luma(colors["app"]) < 0.5 {
		ground = "sb"
	}
	p.fill(ground, 1, 0, 0, cardW, cardH)
	for y := float32(11); y < cardH; y += 22 { // the field's dots
		for x := float32(11); x < cardW; x += 22 {
			p.fill("field", 0.45, x, y, 1.2, 1.2)
		}
	}

	mono := func(px float64) cardFont { return cardFont{family: cardMono, px: px} }
	kicker := cardFont{family: cardMono, px: 11, weight: pango.WeightSemiboldValue, track: 0.12}

	// The slip.
	const sx, sy, sw, sh = 96, 30, 456, 615
	p.fill("panel", 1, sx, sy, sw, sh)
	p.fill("line", 1, sx, sy, sw, 1)
	p.fill("line", 1, sx, sy+sh-1, sw, 1)
	p.fill("line", 1, sx, sy, 1, sh)
	p.fill("line", 1, sx+sw-1, sy, 1, sh)
	for _, m := range [][2]float32{{sx, sy}, {sx + sw - 14, sy}, {sx, sy + sh - 14}, {sx + sw - 14, sy + sh - 14}} {
		// The registration marks: an L at each corner, pointing out.
		left, top := m[0] == sx, m[1] == sy
		hx, vy := m[0], m[1]
		if !top {
			vy = sy + sh - 2
		}
		p.fill("accent", 1, hx, vy, 14, 2)
		vx := float32(sx)
		if !left {
			vx = sx + sw - 2
		}
		p.fill("accent", 1, vx, m[1], 2, 14)
	}

	x0, x1 := float32(sx+30), float32(sx+sw-30)
	y := float32(sy + 26)
	p.mark("accent", x0, y-4, 24)
	p.text(x0+34, y, i18n.T("INUA // SYSTEM MANIFEST"), cardFont{family: cardMono, px: 12.5, weight: pango.WeightSemiboldValue, track: 0.28}, "t0", cardLeft, 0)
	y += 24
	small := cardFont{family: cardMono, px: 11, track: 0.1}
	p.text(x0, y, "NO. "+c.Serial, small, "t2", cardLeft, 0)
	p.text(x1, y, c.Time, small, "t2", cardRight, 0)
	y += 30
	p.line("line", x0, y, x1-x0)
	y += 8

	row := 22.0
	section := func(name string) {
		p.text(x0, y, name, kicker, "accent", cardLeft, 0)
		y += 20
	}
	pair := func(left, right, leftToken, rightToken string) {
		_, h := p.text(x0, y, left, mono(12.5), leftToken, cardLeft, x1-x0-110)
		p.text(x1, y, right, mono(12.5), rightToken, cardRight, 0)
		y += max(float32(row), h+5) // a long name wraps, and takes its lines
	}
	gauge := func(label string, u system.Usage) {
		p.text(x0, y, label, mono(12.5), "t2", cardLeft, 0)
		value := shell.CardGiB(u)
		vw, _ := p.text(x1, y, value, mono(12.5), "t0", cardRight, 0)
		bx, bw := x0+50, x1-vw-12-(x0+50)
		p.fill("surface2", 1, bx, y+9, bw, 3)
		if u.Total > 0 {
			p.fill("accent", 1, bx, y+9, bw*float32(u.Used)/float32(u.Total), 3)
		}
		y += float32(row)
	}

	section(i18n.T("[UNIT]"))
	pair(c.Model, c.Arch, "t0", "t2")
	y += 8
	section(i18n.T("[SILICON]"))
	if c.CPU != "" {
		threads := ""
		if c.Threads > 0 {
			threads = i18n.Tf("%d thr", c.Threads)
		}
		p.text(x0, y, "CPU", mono(12.5), "t2", cardLeft, 0)
		p.text(x0+50, y, c.CPU, mono(12.5), "t0", cardLeft, x1-x0-130)
		p.text(x1, y, threads, mono(12.5), "t0", cardRight, 0)
		y += float32(row)
	}
	if c.GPU != "" {
		p.text(x0, y, "GPU", mono(12.5), "t2", cardLeft, 0)
		p.text(x0+50, y, c.GPU, mono(12.5), "t0", cardLeft, x1-x0-50)
		y += float32(row)
	}
	if c.Memory.Total > 0 {
		gauge("MEM", c.Memory)
	}
	if c.Disk.Total > 0 {
		gauge(i18n.T("DISK"), c.Disk)
	}
	y += 8
	section(i18n.T("[STACK]"))
	for _, l := range c.Stack {
		name := l.Name
		if l.Note != "" {
			name += " · " + l.Note
		}
		pair(name, l.Version, "t0", "t0")
	}
	if len(c.Screens) > 0 {
		y += 8
		section(i18n.T("[OPTICS]"))
		for _, sc := range c.Screens[:min(2, len(c.Screens))] {
			rest := strings.TrimPrefix(sc.String(), fmt.Sprintf("%d×%d", sc.Width, sc.Height))
			pair(fmt.Sprintf("%d×%d", sc.Width, sc.Height), strings.TrimPrefix(rest, " · "), "t0", "t0")
		}
	}
	y += 4
	p.line("line", x0, y, x1-x0)
	y += 8
	pair(i18n.T("UPTIME"), c.Uptime, "t2", "t0")
	y += 2
	p.line("line", x0, y, x1-x0)

	// The codes, at the slip's foot in the slip's own colours (light on dark
	// under a dark theme, which phone cameras read too), with the quiet
	// margins scanners need (four modules round the QR code, ten at the
	// barcode's ends).
	paper, ink := "panel", "t0"
	foot := float32(sy + sh - 22)
	const qmod, quiet = 3, 12 // 4 modules of 3
	labelH := float32(99)
	if q := codes.qr; q != nil {
		labelH = float32(qmod*q.GetWidth() + 2*quiet)
	}
	lx, ly := x0, foot-labelH
	p.fill(paper, 1, lx, ly, x1-lx, labelH)
	qx := x1
	if q := codes.qr; q != nil {
		size := float32(qmod * q.GetWidth())
		qx = x1 - quiet - size
		for j := 0; j < q.GetHeight(); j++ {
			for i := 0; i < q.GetWidth(); i++ {
				if q.Get(i, j) {
					p.fill(ink, 1, qx+float32(i*qmod), ly+quiet+float32(j*qmod), qmod, qmod)
				}
			}
		}
	}
	if b := codes.bar; b != nil && b.GetWidth() > 0 {
		// Modules in half pixels, so the image at twice the size has
		// whole ones; the margin at each end is ten of them.
		room := qx - lx - quiet
		mod := float32(math.Floor(float64(room/float32(b.GetWidth()+20))*2) / 2)
		mod = max(1, min(2, mod))
		bx := lx + 10*mod
		for i := 0; i < b.GetWidth(); i++ {
			if b.Get(i, 0) {
				p.fill(ink, 1, bx+float32(i)*mod, ly+quiet, mod, labelH-2*quiet-18)
			}
		}
		p.text(bx, ly+labelH-quiet-13, c.Serial, cardFont{family: cardMono, px: 10.5, weight: pango.WeightSemiboldValue, track: 0.3}, ink, cardLeft, 0)
	}

	// The stamp, on the slip's top edge, half on the paper, hiding none of
	// its lines.
	stampToken := "accent"
	if !c.Status.Nominal {
		stampToken = "warn"
	}
	s.Save()
	s.Translate(&graphene.Point{X: sx + sw - 178, Y: sy - 6})
	s.Rotate(-6)
	label := "[ " + c.Status.Stamp + " ]"
	stamp := cardFont{family: cardMono, px: 13, weight: pango.WeightSemiboldValue, track: 0.22}
	lw, lh := measure(p, label, stamp)
	p.fill("panel", 0.75, 0, 0, lw+24, lh+12)
	p.fill(stampToken, 1, 0, 0, lw+24, 2)
	p.fill(stampToken, 1, 0, lh+10, lw+24, 2)
	p.fill(stampToken, 1, 0, 0, 2, lh+12)
	p.fill(stampToken, 1, lw+22, 0, 2, lh+12)
	p.text(12, 6, label, stamp, stampToken, cardLeft, 0)
	s.Restore()

	// Beside it: the transmission, the headline, the stack in a line, the
	// theme's colours.
	rx, rw := float32(640), float32(cardW-64-640)
	headline := cardFont{family: cardSans, px: 54, weight: pango.WeightLightValue}
	_, hh := measure(p, c.Status.Headline, headline, rw)
	block := 18 + 24 + hh + 24 + 24 + 24 + 24
	ry := (cardH - block) / 2
	p.text(rx+18, ry, i18n.T("[TRANSMISSION]"), cardFont{family: cardMono, px: 11.5, track: 0.18}, "t2", cardLeft, 0)
	p.fill("t2", 1, rx, ry+4, 7, 1)
	p.fill("t2", 1, rx, ry+10, 7, 1)
	p.fill("t2", 1, rx, ry+4, 1, 7)
	p.fill("t2", 1, rx+6, ry+4, 1, 7)
	ok := i18n.T("OK")
	if !c.Status.Nominal {
		ok = i18n.T("CHECK")
	}
	okw, _ := p.text(rx+rw, ry, ok, cardFont{family: cardMono, px: 11.5, track: 0.18}, map[bool]string{true: "t2", false: "warn"}[c.Status.Nominal], cardRight, 0)
	p.dashes("line", rx+150, ry+8, rw-150-okw-12)
	ry += 18 + 24
	p.text(rx, ry, c.Status.Headline, headline, "t0", cardLeft, rw)
	ry += hh + 24
	tag := i18n.T("Hyprland, with inua on top.")
	if c.Theme != "" {
		tag += " " + c.Theme + "."
	}
	p.text(rx, ry, tag, cardFont{family: cardText, px: 16}, "t1", cardLeft, rw)
	ry += 24 + 24
	for i, token := range shell.CardColors {
		p.fill(token, 1, rx+float32(i)*24, ry, 24, 24)
	}
}

// measure is a run's size, drawn nowhere.
func measure(p cardPen, str string, f cardFont, width ...float32) (float32, float32) {
	off := cardPen{w: p.w, s: gtk.NewSnapshot(), colors: p.colors, scale: p.scale}
	wid := float32(0)
	if len(width) > 0 {
		wid = width[0]
	}
	w, h := off.text(0, 0, str, f, "t0", cardLeft, wid)
	if n := off.s.FreeToNode(); n != nil {
		n.Unref()
	}
	return w, h
}

// luma is a colour's lightness, 0 to 1, as the eye weighs its parts.
func luma(hex string) float64 {
	c := hexRGBA(hex)
	return 0.2126*float64(c.Red) + 0.7152*float64(c.Green) + 0.0722*float64(c.Blue)
}
