package lightui

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"image/png"
	"math"
	"slices"
	"strconv"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/chart"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/uikit"
	"github.com/lyricat/inua/internal/workshop"
)

// The building blocks taken from Settings and the light apps: tabs, a
// segmented choice, a slider, progress, a spinner, a chart, tiles and an
// image (docs/workshop.md).

// wsMore are the widgets of those blocks.
type wsMore struct {
	strip   *gtk.Box // tabs' titles, a segmented choice's buttons
	toggles []*gtk.ToggleButton
	tabBtns []*gtk.Button
	choice  string // the tab shown, the option chosen
	scale   *gtk.Scale
	bar     *gtk.ProgressBar
	pic     *gtk.Picture
	flow    *gtk.FlowBox
	fns     []any // the callbacks the widgets call, kept alive
}

// makeMore makes the widgets of the blocks here; false for a type it
// does not know.
func (a *workshopApp) makeMore(w *wsNode, n workshop.Node) bool {
	m := &w.more
	switch n.Type {
	case "tabs":
		outer := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		outer.AddCssClass("ws-tabs")
		m.strip = gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		m.strip.AddCssClass("ws-tab-strip")
		m.strip.SetHalign(gtk.AlignCenterValue)
		gtkx.Append(outer, &m.strip.Widget)
		w.kids = gtk.NewBox(gtk.OrientationVerticalValue, 0)
		w.kids.AddCssClass("ws-tab-pages")
		w.kids.SetVexpand(true)
		gtkx.Append(outer, &w.kids.Widget)
		w.sig, w.w = outer.GoPointer(), &outer.Widget
	case "tab":
		w.kids = gtk.NewBox(gtk.OrientationVerticalValue, 12)
		w.kids.AddCssClass("ws-tab")
		w.kids.SetVexpand(true)
		w.w = &w.kids.Widget
	case "segmented":
		m.strip = gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		m.strip.AddCssClass("segmented")
		m.strip.AddCssClass("linked")
		m.strip.SetHalign(gtk.AlignStartValue)
		m.strip.SetValign(gtk.AlignCenterValue)
		var first *gtk.ToggleButton
		for _, opt := range n.Options {
			b := gtk.NewToggleButtonWithLabel(opt)
			if first == nil {
				first = b
			} else {
				b.SetGroup(first)
			}
			opt := opt
			fn := func(b gtk.ToggleButton) {
				if b.GetActive() && m.choice != opt {
					m.choice = opt
					a.send("change", w.n.ID, opt)
				}
			}
			m.fns = append(m.fns, &fn)
			b.ConnectToggled(&fn)
			m.toggles = append(m.toggles, b)
			gtkx.Append(m.strip, &b.Widget)
		}
		if len(n.Options) > 0 {
			m.choice = n.Options[0]
			a.filling = true
			first.SetActive(true)
			a.filling = false
		}
		w.n.Options = n.Options
		w.sig, w.w = m.strip.GoPointer(), &m.strip.Widget
	case "slider":
		lo, hi, step := sliderRange(n)
		m.scale = gtk.NewScaleWithRange(gtk.OrientationHorizontalValue, lo, hi, step)
		m.scale.SetDrawValue(true)
		m.scale.SetDigits(digits(step))
		m.scale.SetHexpand(true)
		m.scale.AddCssClass("ws-slider")
		fn := func(gtk.Range) {
			if n := a.bySig[m.scale.GoPointer()]; n != nil {
				a.send("change", n.n.ID, m.scale.GetValue())
			}
		}
		m.fns = append(m.fns, &fn)
		m.scale.ConnectValueChanged(&fn)
		w.sig, w.w = m.scale.GoPointer(), &m.scale.Widget
	case "progress":
		box := gtk.NewBox(gtk.OrientationVerticalValue, 6)
		m.bar = gtk.NewProgressBar()
		m.bar.AddCssClass("ws-progress")
		gtkx.Append(box, &m.bar.Widget)
		w.sub = label("sub")
		gtkx.Append(box, &w.sub.Widget)
		w.w = &box.Widget
	case "spinner":
		box := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		sp := gtk.NewSpinner()
		sp.Start()
		gtkx.Append(box, &sp.Widget)
		w.sub = label("sub")
		gtkx.Append(box, &w.sub.Widget)
		w.w = &box.Widget
	case "chart":
		m.pic = gtk.NewPicture()
		m.pic.AddCssClass("ws-chart")
		m.pic.SetCanShrink(true)
		m.pic.SetContentFit(gtk.ContentFitFillValue)
		m.pic.SetHexpand(true)
		w.w = &m.pic.Widget
	case "tiles":
		m.flow = gtk.NewFlowBox()
		m.flow.AddCssClass("ws-tiles")
		m.flow.SetSelectionMode(gtk.SelectionNoneValue)
		m.flow.SetHomogeneous(true)
		m.flow.SetMaxChildrenPerLine(6)
		m.flow.SetColumnSpacing(8)
		m.flow.SetRowSpacing(8)
		w.w = &m.flow.Widget
	case "image":
		if n.Src != "" {
			m.pic = gtk.NewPicture()
			m.pic.SetContentFit(gtk.ContentFitContainValue)
			m.pic.SetCanShrink(true)
			w.w = &m.pic.Widget
		} else {
			w.icon = gtk.NewImageFromIconName("")
			w.icon.AddCssClass("ws-image-icon")
			w.w = &w.icon.Widget
			if n.Style == "app" {
				w.w = uikit.AppIcon(w.icon, nil) // an app's icon, as the dock shows it
			}
		}
	default:
		return false
	}
	return true
}

func sliderRange(n workshop.Node) (lo, hi, step float64) {
	lo, hi, step = 0, 100, n.Step
	if n.Min != nil {
		lo = *n.Min
	}
	if n.Max != nil {
		hi = *n.Max
	}
	if hi <= lo {
		hi = lo + 1
	}
	if step <= 0 {
		step = 1
	}
	return lo, hi, step
}

// digits is how many decimals a slider shows for its step.
func digits(step float64) int32 {
	d := int32(0)
	for d < 4 && math.Abs(step-math.Round(step)) > 1e-9 {
		step *= 10
		d++
	}
	return d
}

// sameMore says whether the blocks here can show n with their widgets.
func sameMore(old, n workshop.Node) bool {
	switch n.Type {
	case "segmented":
		return slices.Equal(old.Options, n.Options)
	case "slider":
		ol, oh, os := sliderRange(old)
		nl, nh, ns := sliderRange(n)
		return ol == nl && oh == nh && os == ns
	case "image":
		return (old.Src == "") == (n.Src == "") && (old.Style == "app") == (n.Style == "app")
	}
	return true
}

// updateMore shows n in the widgets of the blocks here.
func (a *workshopApp) updateMore(w *wsNode, n workshop.Node, prev workshop.Node, valueChanged bool) {
	m := &w.more
	switch n.Type {
	case "tabs":
		var ids, titles []string
		for _, t := range n.Children {
			ids, titles = append(ids, t.ID), append(titles, t.Title)
		}
		var oldIDs, oldTitles []string
		for _, t := range prev.Children {
			oldIDs, oldTitles = append(oldIDs, t.ID), append(oldTitles, t.Title)
		}
		if len(m.tabBtns) == 0 || !slices.Equal(ids, oldIDs) || !slices.Equal(titles, oldTitles) {
			for _, b := range m.tabBtns {
				m.strip.Remove(&b.Widget)
			}
			m.tabBtns, m.fns = nil, nil
			for i, t := range n.Children {
				b := gtk.NewButtonWithLabel(t.Title)
				b.AddCssClass("ws-tab-button")
				id := t.ID
				if id == "" {
					id = strconv.Itoa(i)
				}
				fn := func(gtk.Button) { a.showTab(w, id, true) }
				m.fns = append(m.fns, &fn)
				b.ConnectClicked(&fn)
				m.tabBtns = append(m.tabBtns, b)
				gtkx.Append(m.strip, &b.Widget)
			}
		}
		if valueChanged || m.choice == "" || !slices.Contains(tabIDs(n), m.choice) {
			want := n.String()
			if !slices.Contains(tabIDs(n), want) && len(n.Children) > 0 {
				want = tabIDs(n)[0]
			}
			m.choice = want
		}
	case "segmented":
		if valueChanged {
			a.setValue(w, n.String())
		}
	case "slider":
		if valueChanged || prev.Type == "" {
			a.setValue(w, n.Number())
		}
	case "progress":
		m.bar.SetFraction(min(max(n.Number(), 0), 1))
		w.sub.SetText(n.Text)
		w.sub.SetVisible(n.Text != "")
	case "spinner":
		w.sub.SetText(n.Text)
		w.sub.SetVisible(n.Text != "")
	case "chart":
		h := n.Height
		if h <= 0 {
			h = 80
		}
		m.pic.SetSizeRequest(-1, int32(h))
		a.drawChart(m.pic, n, h)
	case "tiles":
		m.flow.RemoveAll()
		m.fns = nil
		for _, t := range n.Children {
			b := gtk.NewButton()
			b.AddCssClass("ws-tile")
			if t.Selected {
				b.AddCssClass("on")
			}
			b.SetName(t.ID)
			col := gtk.NewBox(gtk.OrientationVerticalValue, 4)
			if t.Icon != "" {
				img := gtk.NewImageFromIconName("")
				setBlockIcon(img, t.Icon)
				img.SetHalign(gtk.AlignStartValue)
				gtkx.Append(col, &img.Widget)
			}
			title := label("title")
			title.SetText(t.Title)
			title.SetEllipsize(3)
			gtkx.Append(col, &title.Widget)
			if t.Sub != "" {
				sub := label("sub")
				sub.SetText(t.Sub)
				sub.SetEllipsize(3)
				gtkx.Append(col, &sub.Widget)
			}
			b.SetChild(&col.Widget)
			b.ConnectClicked(&a.onClick)
			m.flow.Append(&b.Widget)
		}
	case "image":
		if m.pic != nil {
			m.pic.SetFilename(n.Src)
			size := func(v int) int32 {
				if v > 0 {
					return int32(v)
				}
				return -1
			}
			m.pic.SetSizeRequest(size(n.Width), size(n.Height))
		} else {
			setBlockIcon(w.icon, n.Icon)
			size := cmp.Or(max(n.Height, 0), max(n.Width, 0), 32)
			w.icon.SetPixelSize(int32(size))
		}
	}
}

func tabIDs(n workshop.Node) []string {
	var out []string
	for i, t := range n.Children {
		if t.ID == "" {
			out = append(out, strconv.Itoa(i))
		} else {
			out = append(out, t.ID)
		}
	}
	return out
}

// afterKids shows the tab chosen, once the tabs' pages are there.
func (a *workshopApp) afterKids(w *wsNode) {
	if w.n.Type == "tabs" {
		a.showTab(w, w.more.choice, false)
	}
}

// showTab shows one of the tabs' pages, and tells the app when the user
// chose it.
func (a *workshopApp) showTab(w *wsNode, id string, chosen bool) {
	ids := tabIDs(w.n)
	for i, page := range w.children {
		on := i < len(ids) && ids[i] == id
		page.w.SetVisible(on)
		if i < len(w.more.tabBtns) {
			toggleClass(&w.more.tabBtns[i].Widget, "checked", on)
		}
	}
	changed := w.more.choice != id
	w.more.choice = id
	if chosen && changed {
		a.send("change", w.n.ID, id)
	}
}

// valueMore is the value of an input here, as the app sees it; ok false
// for other blocks.
func (a *workshopApp) valueMore(n *wsNode) (any, bool) {
	switch n.n.Type {
	case "tabs", "segmented":
		return n.more.choice, true
	case "slider":
		return n.more.scale.GetValue(), true
	}
	return nil, false
}

// setValueMore sets an input here as the app says; false for other
// blocks. Called with a.filling set.
func (a *workshopApp) setValueMore(n *wsNode, v any) bool {
	switch n.n.Type {
	case "tabs":
		s, _ := v.(string)
		if slices.Contains(tabIDs(n.n), s) {
			a.showTab(n, s, false)
		}
	case "segmented":
		s, _ := v.(string)
		if i := slices.Index(n.n.Options, s); i >= 0 {
			n.more.choice = s
			n.more.toggles[i].SetActive(true)
		}
	case "slider":
		f, ok := v.(float64)
		if ok {
			n.more.scale.SetValue(f)
		}
	default:
		return false
	}
	return true
}

// drawChart draws the points as the light apps' charts are drawn.
func (a *workshopApp) drawChart(pic *gtk.Picture, n workshop.Node, h int) {
	if len(n.Points) < 2 {
		pic.SetPaintable(&gdk.PaintableBase{}) // none
		return
	}
	scale := max(1, int(pic.GetScaleFactor()))
	w := int(pic.GetWidth())
	if w < 50 {
		w = 480
	}
	o := chart.Options{}
	if n.Min != nil && n.Max != nil && *n.Max > *n.Min {
		o.Lo, o.Hi = *n.Min, *n.Max
	}
	img := chart.Line(n.Points, w*scale, h*scale, hexColor(a.c.Colors["accent"]), o)
	var buf bytes.Buffer
	if png.Encode(&buf, img) != nil {
		return
	}
	data := buf.Bytes()
	tex, err := gdk.NewTextureFromBytes(glib.NewBytes(data, uint(len(data))))
	if err != nil {
		return
	}
	pic.SetPaintable(tex)
	tex.Unref()
}

// --- file dialogs -------------------------------------------------------------------

// wsFiles is a file dialog the app asked for, and where its answer goes.
type wsFiles struct {
	d      *gtk.FileDialog
	answer chan []string
}

func (h wsHost) OpenFile(ctx context.Context, title string, multiple bool) ([]string, error) {
	return h.files(ctx, func(a *workshopApp, f *wsFiles) {
		f.d.SetTitle(cmp.Or(title, i18n.T("Open")))
		if multiple {
			f.d.OpenMultiple(&a.win.Window, nil, &a.onFilesOpened, 0)
		} else {
			f.d.Open(&a.win.Window, nil, &a.onFileOpened, 0)
		}
	})
}

func (h wsHost) SaveFile(ctx context.Context, title, name string) (string, error) {
	paths, err := h.files(ctx, func(a *workshopApp, f *wsFiles) {
		f.d.SetTitle(cmp.Or(title, i18n.T("Save")))
		if name != "" {
			f.d.SetInitialName(name)
		}
		f.d.Save(&a.win.Window, nil, &a.onFileSaved, 0)
	})
	if len(paths) == 0 {
		return "", err
	}
	return paths[0], err
}

// files shows a file dialog over the app's window and waits for it.
func (h wsHost) files(ctx context.Context, show func(*workshopApp, *wsFiles)) ([]string, error) {
	answer := make(chan []string, 1)
	busy := make(chan error, 1)
	gtkx.Post(func() {
		a := h.a
		if !h.live() {
			busy <- errors.New("the window closed")
			return
		}
		if a.fileDialog != nil {
			busy <- errors.New("a file dialog is open already")
			return
		}
		f := &wsFiles{d: gtk.NewFileDialog(), answer: answer}
		a.fileDialog = f
		show(a, f)
		busy <- nil
	})
	if err := <-busy; err != nil {
		return nil, err
	}
	select {
	case paths := <-answer:
		return paths, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// bindFileDialogs makes the callbacks of the app's file dialogs.
func (a *workshopApp) bindFileDialogs() {
	done := func(paths []string) {
		f := a.fileDialog
		a.fileDialog = nil
		if f == nil {
			return
		}
		f.d.Unref()
		f.answer <- paths
	}
	a.onFileOpened = func(_, result, _ uintptr) {
		if a.fileDialog == nil {
			return
		}
		file, err := a.fileDialog.d.OpenFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || file == nil {
			done(nil)
			return
		}
		p := file.GetPath()
		unrefObject(file)
		done(onePath(p))
	}
	a.onFileSaved = func(_, result, _ uintptr) {
		if a.fileDialog == nil {
			return
		}
		file, err := a.fileDialog.d.SaveFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || file == nil {
			done(nil)
			return
		}
		p := file.GetPath()
		unrefObject(file)
		done(onePath(p))
	}
	a.onFilesOpened = func(_, result, _ uintptr) {
		if a.fileDialog == nil {
			return
		}
		files, err := a.fileDialog.d.OpenMultipleFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || files == nil {
			done(nil)
			return
		}
		var paths []string
		for i := range files.GetNItems() {
			f := &gio.FileBase{Ptr: files.GetItem(i)}
			if p := f.GetPath(); p != "" {
				paths = append(paths, p)
			}
			unrefObject(f)
		}
		unrefObject(files)
		done(paths)
	}
}

func onePath(p string) []string {
	if p == "" {
		return nil
	}
	return []string{p}
}
