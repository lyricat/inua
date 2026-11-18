package viewerui

import (
	"fmt"
	"log"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gdkpixbuf"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gobject/types"
	"codeberg.org/puregotk/puregotk/v4/gsk"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"github.com/lyricat/inua/internal/apps/viewer"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// The text view: Markdown laid out in a column (viewer.ParseMarkdown's
// blocks, set with text tags; tables and pictures as widgets in the text),
// or plain text in a fixed-width font. The tags' sizes, margins and
// spacing follow the zoom, and their colours the theme.

const (
	column       = 760 // Markdown's column, in logical pixels at 100%
	textMinZoom  = 0.5
	textMaxZoom  = 3
	mdImageWidth = 720 // the widest a picture in Markdown is shown
	monoFamily   = "Geist Mono,IBM Plex Mono,JetBrains Mono,monospace"
)

type textView struct {
	a          *App
	scroller   gtk.ScrolledWindow
	view       gtk.TextView
	buf        *gtk.TextBuffer
	hadj, vadj *gtk.Adjustment

	path   string
	kind   viewer.Kind
	src    string
	blocks []viewer.Block
	gen    uintptr // bumps with each file: late reads and pictures are thrown away
	source bool    // Markdown shown as written
	zoom   float64 // 1 at the style's size
	stats  [3]int  // lines, words, characters
	links  []textLink
	marks  []string // the headings' marks
	anchor string   // a heading to scroll to once laid out
	margin int      // the column's side margins as set

	tagTable *gtk.TextTagTable
	tags     map[string]*gtk.TextTag
	specs    map[string]tagSpec

	onWidth  func(gtk.Adjustment)
	onClick  func(gtk.GestureClick, int32, float64, float64)
	onMotion func(gtk.EventControllerMotion, float64, float64)
	onScroll func(gtk.EventControllerScroll, float64, float64) bool
	onAnchor glib.SourceFunc
}

// textLink is where a link is in the text, by character offsets.
type textLink struct {
	start, end int
	dest       string
}

// tagSpec is what of a tag follows the zoom: its scale, and its margin,
// first-line indent and spacing at 100%.
type tagSpec struct {
	scale                 float64
	margin, indent        int
	above, below, rmargin int
}

func (t *textView) init(a *App, ui *gtkx.Builder) {
	t.a, t.zoom = a, 1
	ui.Get("text_scroller", &t.scroller)
	ui.Get("text", &t.view)
	t.buf = t.view.GetBuffer()
	t.tagTable = t.buf.GetTagTable()
	t.hadj, t.vadj = t.scroller.GetHadjustment(), t.scroller.GetVadjustment()
	t.makeTags()

	t.onWidth = func(gtk.Adjustment) { t.fitColumn() }
	t.hadj.ConnectChanged(&t.onWidth)
	t.onAnchor = func(uintptr) bool {
		t.scrollTo(t.anchor)
		t.anchor = ""
		return false
	}

	click := gtk.NewGestureClick()
	click.SetButton(1)
	t.onClick = func(_ gtk.GestureClick, n int32, x, y float64) {
		if n != 1 || t.buf.GetHasSelection() {
			return
		}
		if dest := t.linkAt(x, y); dest != "" {
			t.follow(dest)
		}
	}
	click.ConnectReleased(&t.onClick)
	t.view.AddController(&click.EventController)

	motion := gtk.NewEventControllerMotion()
	t.onMotion = func(_ gtk.EventControllerMotion, x, y float64) {
		if t.linkAt(x, y) != "" {
			t.view.SetCursorFromName("pointer")
		} else {
			t.view.SetCursorFromName("text")
		}
	}
	motion.ConnectMotion(&t.onMotion)
	t.view.AddController(&motion.EventController)

	scroll := gtk.NewEventControllerScroll(gtk.EventControllerScrollVerticalValue)
	scroll.SetPropagationPhase(gtk.PhaseCaptureValue)
	t.onScroll = func(s gtk.EventControllerScroll, _, dy float64) bool {
		if s.GetCurrentEventState()&gdk.ControlMaskValue == 0 {
			return false
		}
		if dy != 0 {
			t.setZoom(t.zoom * wheelFactor(s, dy))
		}
		return true
	}
	scroll.ConnectScroll(&t.onScroll)
	t.scroller.AddController(&scroll.EventController)
}

// makeTags makes the tags Markdown is set with, the lowest first.
func (t *textView) makeTags() {
	t.tags, t.specs = map[string]*gtk.TextTag{}, map[string]tagSpec{}
	add := func(name string, spec tagSpec, set func(*gtk.TextTag)) {
		tag := gtk.NewTextTag(name)
		if set != nil {
			set(tag)
		}
		t.tagTable.Add(tag)
		t.tags[name], t.specs[name] = tag, spec
	}
	add("body", tagSpec{scale: 1}, nil)
	add("para", tagSpec{below: 12}, nil)
	add("tight", tagSpec{below: 4}, nil)
	add("item", tagSpec{indent: -20}, nil)
	add("marker", tagSpec{}, nil)
	for i, h := range []struct {
		scale        float64
		above, below int
	}{{1.9, 24, 10}, {1.5, 22, 8}, {1.25, 18, 6}, {1.1, 14, 6}, {1, 12, 4}, {0.95, 12, 4}} {
		add(fmt.Sprintf("h%d", i+1), tagSpec{scale: h.scale, above: h.above, below: h.below}, func(tag *gtk.TextTag) {
			tag.SetPropertyWeight(600)
		})
	}
	add("strong", tagSpec{}, func(tag *gtk.TextTag) { tag.SetPropertyWeight(700) })
	add("em", tagSpec{}, func(tag *gtk.TextTag) { setEnum(tag, "style", pango.StyleGLibType(), int32(pango.StyleItalicValue)) })
	add("strike", tagSpec{}, func(tag *gtk.TextTag) { tag.SetPropertyStrikethrough(true) })
	add("code", tagSpec{scale: 0.9}, func(tag *gtk.TextTag) { tag.SetPropertyFamily(monoFamily) })
	add("codeblock", tagSpec{scale: 0.9, margin: 16, rmargin: 16}, func(tag *gtk.TextTag) {
		tag.SetPropertyFamily(monoFamily)
		tag.SetPropertyAccumulativeMargin(true)
		tag.SetPropertyWrapModeSet(false) // long lines wrap as the rest
	})
	add("mono", tagSpec{}, func(tag *gtk.TextTag) { tag.SetPropertyFamily(monoFamily) })
	add("rule", tagSpec{scale: 0.25}, nil)
	add("gap", tagSpec{scale: 0.5}, nil)
	add("link", tagSpec{}, func(tag *gtk.TextTag) {
		setEnum(tag, "underline", pango.UnderlineGLibType(), int32(pango.UnderlineSingleValue))
	})
	t.recolor()
	t.applyZoom()
}

// level is the tag for a block in n lists or quotes, made when first
// needed: its margins add to the others'.
func (t *textView) level(kind string, n int) *gtk.TextTag {
	name := fmt.Sprintf("%s-%d", kind, n)
	if tag := t.tags[name]; tag != nil {
		return tag
	}
	tag := gtk.NewTextTag(name)
	tag.SetPropertyAccumulativeMargin(true)
	t.tagTable.Add(tag)
	step := 28
	if kind == "quote" {
		step = 20
	}
	t.tags[name], t.specs[name] = tag, tagSpec{margin: step}
	t.recolor()
	t.apply(name)
	return tag
}

func setEnum(tag *gtk.TextTag, prop string, typ types.GType, v int32) {
	var val gobject.Value
	val.Init(typ)
	val.SetEnum(v)
	tag.SetProperty(prop, &val)
	val.Unset()
}

// recolor gives the tags the theme's colours.
func (t *textView) recolor() {
	colors := t.a.c.Colors
	set := func(name, prop, token string) {
		tag, c := t.tags[name], colors[token]
		if tag == nil || c == "" {
			return
		}
		switch prop {
		case "fg":
			tag.SetPropertyForeground(c)
		case "bg":
			tag.SetPropertyBackground(c)
		case "para":
			tag.SetPropertyParagraphBackground(c)
		}
	}
	set("link", "fg", "accent")
	set("marker", "fg", "t2")
	set("code", "bg", "surface2")
	set("codeblock", "para", "surface2")
	set("rule", "para", "line")
	for name := range t.tags {
		if strings.HasPrefix(name, "quote-") {
			set(name, "fg", "t1")
		}
	}
}

// apply sets a tag's sizes for the zoom.
func (t *textView) apply(name string) {
	tag, s, z := t.tags[name], t.specs[name], t.zoom
	px := func(n int) int32 { return int32(math.Round(float64(n) * z)) }
	if s.scale > 0 {
		tag.SetPropertyScale(s.scale * z)
	}
	if s.margin != 0 {
		tag.SetPropertyLeftMargin(px(s.margin))
	}
	if s.rmargin != 0 {
		tag.SetPropertyRightMargin(px(s.rmargin))
	}
	if s.indent != 0 {
		tag.SetPropertyIndent(px(s.indent))
	}
	if s.above != 0 {
		tag.SetPropertyPixelsAboveLines(px(s.above))
	}
	if s.below != 0 {
		tag.SetPropertyPixelsBelowLines(px(s.below))
	}
}

func (t *textView) applyZoom() {
	for name := range t.specs {
		t.apply(name)
	}
}

// open reads a file off the main thread, then lays it out.
func (t *textView) open(path string, kind viewer.Kind) {
	t.clear()
	t.path, t.kind = path, kind
	gen := t.gen
	go func() {
		src, err := viewer.ReadText(path)
		var blocks []viewer.Block
		if err == nil && kind == viewer.Markdown {
			blocks = viewer.ParseMarkdown(src)
		}
		l, w, c := viewer.TextStats(src)
		gtkx.Post(func() {
			if gen != t.gen {
				return
			}
			if err != nil {
				t.a.failed(path, err)
				return
			}
			t.src, t.blocks, t.stats = src, blocks, [3]int{l, w, c}
			t.render()
			t.a.showDetails()
		})
	}()
}

// clear empties the view.
func (t *textView) clear() {
	t.gen++
	t.deleteMarks()
	t.buf.SetText("", 0)
	t.src, t.blocks, t.links, t.anchor, t.stats = "", nil, nil, "", [3]int{}
	t.a.sourceToggle.SetActive(false)
}

func (t *textView) deleteMarks() {
	for _, m := range t.marks {
		t.buf.DeleteMarkByName(m)
	}
	t.marks = nil
}

// showSource switches Markdown between laid out and as written.
func (t *textView) showSource(on bool) {
	if on == t.source {
		return
	}
	t.source = on
	if t.src != "" {
		t.render()
	}
}

// render lays the file out afresh.
func (t *textView) render() {
	t.gen++ // pictures of a layout before are not wanted
	t.deleteMarks()
	t.buf.SetText("", 0)
	t.links = nil
	if t.kind == viewer.Markdown && !t.source {
		dir := filepath.Dir(t.path)
		for _, b := range t.blocks {
			t.block(b, dir)
		}
	} else {
		t.put(t.src, t.tags["body"], t.tags["mono"])
	}
	var start gtk.TextIter
	t.buf.GetStartIter(&start)
	t.buf.PlaceCursor(&start)
	t.vadj.SetValue(0)
	t.margin = -1
	t.fitColumn()
	if t.anchor != "" {
		glib.IdleAdd(&t.onAnchor, 0)
	}
}

// end is the text's end, as an offset.
func (t *textView) end() int {
	var it gtk.TextIter
	t.buf.GetEndIter(&it)
	return int(it.GetOffset())
}

// put adds text at the end with tags.
func (t *textView) put(s string, tags ...*gtk.TextTag) {
	if s == "" {
		return
	}
	from := t.end()
	var it gtk.TextIter
	t.buf.GetEndIter(&it)
	t.buf.Insert(&it, s, -1)
	t.tag(from, tags)
}

// tag applies tags from an offset to the end.
func (t *textView) tag(from int, tags []*gtk.TextTag) {
	var start, end gtk.TextIter
	t.buf.GetIterAtOffset(&start, int32(from))
	t.buf.GetEndIter(&end)
	for _, tag := range tags {
		t.buf.ApplyTag(tag, &start, &end)
	}
}

// with is tags and more, in a list of its own.
func with(tags []*gtk.TextTag, more ...*gtk.TextTag) []*gtk.TextTag {
	return append(slices.Clip(tags), more...)
}

// block lays out a block of Markdown.
func (t *textView) block(b viewer.Block, dir string) {
	tags := []*gtk.TextTag{t.tags["body"]}
	if b.Quote > 0 {
		tags = append(tags, t.level("quote", b.Quote))
	}
	if b.Depth > 0 {
		tags = append(tags, t.level("list", b.Depth))
	}
	switch b.Kind {
	case viewer.Heading:
		tags = append(tags, t.tags[fmt.Sprintf("h%d", b.Level)])
		var it gtk.TextIter
		t.buf.GetEndIter(&it)
		if name := "#" + b.Anchor; !slices.Contains(t.marks, name) {
			t.buf.CreateMark(name, &it, true).Unref()
			t.marks = append(t.marks, name)
		}
		t.spans(b.Spans, tags, dir)
		t.put("\n", tags...)
	case viewer.Para:
		if b.Tight {
			tags = append(tags, t.tags["tight"])
		} else {
			tags = append(tags, t.tags["para"])
		}
		if b.Marker != "" {
			tags = append(tags, t.tags["item"])
			t.put(b.Marker+" ", with(tags, t.tags["marker"])...)
		}
		t.spans(b.Spans, tags, dir)
		t.put("\n", tags...)
	case viewer.CodeBlock:
		t.put(b.Code+"\n", with(tags, t.tags["codeblock"])...)
		t.put("\n", with(tags, t.tags["gap"])...)
	case viewer.Rule:
		t.put("​\n", with(tags, t.tags["rule"])...)
		t.put("\n", with(tags, t.tags["gap"])...)
	case viewer.Table:
		from := t.end()
		t.table(b.Rows, dir)
		t.tag(from, tags)
		t.put("\n", with(tags, t.tags["para"])...)
	}
}

// spans adds a block's runs of text, its pictures, and where its links are.
func (t *textView) spans(spans []viewer.Span, tags []*gtk.TextTag, dir string) {
	for _, s := range spans {
		from := t.end()
		if s.Image != "" {
			t.image(s, tags, dir)
		} else {
			t.put(s.Text, with(tags, t.styleTags(s.Style)...)...)
		}
		if s.Link != "" && t.end() > from {
			t.links = append(t.links, textLink{from, t.end(), s.Link})
		}
	}
}

func (t *textView) styleTags(s viewer.Style) []*gtk.TextTag {
	var tags []*gtk.TextTag
	for _, st := range []struct {
		style viewer.Style
		name  string
	}{{viewer.Strong, "strong"}, {viewer.Emph, "em"}, {viewer.Code, "code"}, {viewer.Strike, "strike"}, {viewer.Linked, "link"}} {
		if s&st.style != 0 {
			tags = append(tags, t.tags[st.name])
		}
	}
	return tags
}

// local is a link's file, if it is one: its path and the heading after
// "#".
func local(dest, dir string) (path, frag string, ok bool) {
	u, err := url.Parse(dest)
	if err != nil || (u.Scheme != "" && u.Scheme != "file") || u.Path == "" {
		return "", "", false
	}
	path = u.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return path, u.Fragment, true
}

// image adds a picture where it stands in the text, drawn once it has
// loaded; a picture on the web shows as its link.
func (t *textView) image(s viewer.Span, tags []*gtk.TextTag, dir string) {
	path, _, ok := local(s.Image, dir)
	if !ok {
		label := s.Text
		if label == "" {
			label = s.Image
		}
		from := t.end()
		t.put(label, with(tags, t.tags["link"])...)
		if s.Link == "" {
			t.links = append(t.links, textLink{from, t.end(), s.Image})
		}
		return
	}
	from := t.end()
	var it gtk.TextIter
	t.buf.GetEndIter(&it)
	// The text view owns the holder; a picture put in it later is wanted
	// only while the layout is (gen), and so is the holder.
	holder := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	anchor := t.buf.CreateChildAnchor(&it)
	t.view.AddChildAtAnchor(&holder.Widget, anchor)
	anchor.Unref()
	holder.Unref()
	t.tag(from, tags)
	gen, scale, alt := t.gen, t.a.scale(), s.Text
	go func() {
		var tex *gdk.Texture
		pb, err := gdkpixbuf.NewPixbufFromFile(path)
		if err == nil {
			if w, h, most := int(pb.GetWidth()), int(pb.GetHeight()), int(mdImageWidth*scale); w > most {
				if small := pb.ScaleSimple(int32(most), int32(max(1, h*most/w)), gdkpixbuf.GdkInterpBilinearValue); small != nil {
					pb.Unref()
					pb = small
				}
			}
			tex = gdk.NewTextureForPixbuf(pb)
			pb.Unref()
		}
		gtkx.Post(func() {
			if gen != t.gen {
				if tex != nil {
					tex.Unref()
				}
				return
			}
			if tex == nil {
				l := gtk.NewLabel(cmpOr(alt, filepath.Base(path)))
				l.AddCssClass("viewer-md-missing")
				gtkx.Append(holder, &l.Widget)
				return
			}
			// The picture at its pixels, shown at the screen's: scaled as a
			// whole in a Fixed, which takes the size it is shown at.
			fr := gtk.NewFixed()
			pic := gtk.NewPicture()
			pic.SetCanShrink(false)
			pic.SetPaintable(tex)
			tex.Unref()
			fr.Put(&pic.Widget, 0, 0)
			tr := gsk.NewTransform().Scale(float32(1/scale), float32(1/scale))
			fr.SetChildTransform(&pic.Widget, tr)
			tr.Unref()
			pic.Unref()
			if alt != "" {
				fr.SetTooltipText(alt)
			}
			gtkx.Append(holder, &fr.Widget)
		})
	}()
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// table adds a table as a grid of labels where it stands in the text.
func (t *textView) table(rows [][][]viewer.Span, dir string) {
	grid := gtk.NewGrid()
	grid.AddCssClass("viewer-md-table")
	for r, row := range rows {
		for c, cell := range row {
			l := gtk.NewLabel("")
			l.SetMarkup(markup(cell, dir))
			l.SetXalign(0)
			l.SetWrap(true)
			l.SetMaxWidthChars(48)
			l.AddCssClass("viewer-md-cell")
			if r == 0 {
				l.AddCssClass("viewer-md-head")
			}
			grid.Attach(&l.Widget, int32(c), int32(r), 1, 1)
			l.Unref()
		}
	}
	var it gtk.TextIter
	t.buf.GetEndIter(&it)
	anchor := t.buf.CreateChildAnchor(&it)
	t.view.AddChildAtAnchor(&grid.Widget, anchor)
	anchor.Unref()
	grid.Unref()
}

// markup is spans as Pango markup, for a table's cell; a link to a file
// is made absolute.
func markup(spans []viewer.Span, dir string) string {
	var b strings.Builder
	for _, s := range spans {
		text := escape(s.Text)
		if s.Image != "" && text == "" {
			text = escape(s.Image)
		}
		wrap := func(open, close string) { text = open + text + close }
		if s.Style&viewer.Code != 0 {
			wrap("<tt>", "</tt>")
		}
		if s.Style&viewer.Strong != 0 {
			wrap("<b>", "</b>")
		}
		if s.Style&viewer.Emph != 0 {
			wrap("<i>", "</i>")
		}
		if s.Style&viewer.Strike != 0 {
			wrap("<s>", "</s>")
		}
		if s.Link != "" && !strings.HasPrefix(s.Link, "#") {
			href := s.Link
			if path, frag, ok := local(href, dir); ok {
				href = (&url.URL{Scheme: "file", Path: path, Fragment: frag}).String()
			}
			wrap(`<a href="`+escape(href)+`">`, "</a>")
		}
		b.WriteString(text)
	}
	return b.String()
}

func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(s)
}

// fitColumn keeps Markdown to a column in the middle of a wide window;
// plain text takes the width.
func (t *textView) fitColumn() {
	w := t.hadj.GetPageSize()
	if w <= 0 {
		return
	}
	m := 24.0
	if t.kind == viewer.Markdown {
		m = max(m, (w-column*t.zoom)/2)
	}
	if int(m) == t.margin {
		return
	}
	t.margin = int(m)
	t.view.SetLeftMargin(int32(m))
	t.view.SetRightMargin(int32(m))
}

// linkAt is the link under a point of the view, "" if none.
func (t *textView) linkAt(x, y float64) string {
	if len(t.links) == 0 {
		return ""
	}
	var bx, by int32
	t.view.WindowToBufferCoords(gtk.TextWindowWidgetValue, int32(x), int32(y), &bx, &by)
	var it gtk.TextIter
	if !t.view.GetIterAtLocation(&it, bx, by) {
		return ""
	}
	off := int(it.GetOffset())
	i := sort.Search(len(t.links), func(i int) bool { return t.links[i].end > off })
	if i < len(t.links) && t.links[i].start <= off {
		return t.links[i].dest
	}
	return ""
}

// follow goes where a link leads: a heading here, a file the viewer
// shows (in this window), or anything else in the app for it.
func (t *textView) follow(dest string) {
	if strings.HasPrefix(dest, "#") {
		t.scrollTo(dest[1:])
		return
	}
	uri := dest
	if path, frag, ok := local(dest, filepath.Dir(t.path)); ok {
		if _, err := os.Stat(path); err != nil {
			t.a.flash(i18n.T("Not found"))
			return
		}
		if viewer.Sniff(path) != viewer.None {
			t.a.Open([]string{path})
			t.anchor = frag
			return
		}
		uri = (&url.URL{Scheme: "file", Path: path}).String()
	}
	if _, err := gio.AppInfoLaunchDefaultForUri(uri, nil); err != nil {
		log.Printf("viewer: open %s: %v", uri, err)
		t.a.flash(i18n.T("Could not open the link"))
	}
}

// scrollTo scrolls to a heading by its id.
func (t *textView) scrollTo(anchor string) {
	if anchor == "" {
		return
	}
	if m := t.buf.GetMark("#" + anchor); m != nil {
		t.view.ScrollToMark(m, 0, true, 0, 0)
		m.Unref()
	}
}

// copyText is the text selected, else all of it as written.
func (t *textView) copyText() string {
	var start, end gtk.TextIter
	if t.buf.GetSelectionBounds(&start, &end) {
		return t.buf.GetText(&start, &end, false)
	}
	return t.src
}

func (t *textView) step(up bool) { t.setZoom(viewer.StepZoom(t.zoom, up)) }

func (t *textView) setZoom(z float64) {
	t.zoom = max(textMinZoom, min(z, textMaxZoom))
	t.applyZoom()
	t.fitColumn()
}

// key takes the keys that scroll: the arrows up and down, Page Up and
// Down and Space by a screen, Home and End to the ends.
func (t *textView) key(keyval uint32, shift bool) bool {
	line, screen := 48.0, t.vadj.GetPageSize()*0.9
	v := t.vadj.GetValue()
	switch {
	case keyval == keyUp:
		v -= line
	case keyval == keyDown:
		v += line
	case keyval == keyPageUp || (keyval == ' ' && shift):
		v -= screen
	case keyval == keyPageDown || keyval == ' ':
		v += screen
	case keyval == keyHome:
		v = 0
	case keyval == keyEnd:
		v = t.vadj.GetUpper()
	default:
		return false
	}
	t.vadj.SetValue(max(0, min(v, t.vadj.GetUpper()-t.vadj.GetPageSize())))
	return true
}
