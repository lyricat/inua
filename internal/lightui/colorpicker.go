package lightui

import (
	"codeberg.org/puregotk/puregotk/v4/gio"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/colors"
	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// colorApp is the Color Picker. The shell picks (inua call screenshot
// color) and adds to the history; the app shows the history and reads it
// again when it changes.
type colorApp struct {
	c        *appui.Ctx
	data     colors.Data
	mod      time.Time
	current  string // the colour shown, "#rrggbb"
	swatch   gtk.Picture
	forms    gtk.Box
	history  gtk.FlowBox
	empty    gtk.Label
	onButton func(gtk.Button)
	onTick   glib.SourceFunc
}

func (a *colorApp) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.c = c
	win := &gtk.ApplicationWindow{}
	var pick, clear gtk.Button
	ui := c.Load("colorpicker.ui")
	ui.Get("colorpicker", win)
	ui.Get("swatch", &a.swatch)
	ui.Get("forms", &a.forms)
	ui.Get("history", &a.history)
	ui.Get("history_empty", &a.empty)
	ui.Get("pick", &pick)
	ui.Get("clear", &clear)
	ui.Close()

	pick.SetName("pick")
	clear.SetName("clear")
	a.onButton = func(b gtk.Button) { a.button(b.GetName()) }
	a.onTick = func(uintptr) bool {
		if st, err := os.Stat(store.Path(colors.StoreName)); err == nil && !st.ModTime().Equal(a.mod) {
			a.load()
			if len(a.data.History) > 0 {
				a.current = a.data.History[0] // a new pick
			}
			a.show()
		}
		return true
	}
	pick.ConnectClicked(&a.onButton)
	clear.ConnectClicked(&a.onButton)
	a.load()
	if len(a.data.History) > 0 {
		a.current = a.data.History[0]
	}
	a.show()
	glib.TimeoutAdd(700, &a.onTick, 0)
	menuWindow(c, win, []appui.MenuGroup{{Title: "Edit", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{{Name: "color-pick", Label: "Pick a color", Activate: func(gio.SimpleAction, uintptr) { a.button("pick") }}, {Name: "color-clear", Label: "Clear history", Activate: func(gio.SimpleAction, uintptr) { a.button("clear") }}}}}}})
	return win
}

func (a *colorApp) ThemeChanged() {}

func (a *colorApp) load() {
	a.data = colors.Data{}
	if err := store.Load(colors.StoreName, &a.data); err != nil {
		log.Printf("colors: %v", err)
	}
	if st, err := os.Stat(store.Path(colors.StoreName)); err == nil {
		a.mod = st.ModTime()
	}
}

func (a *colorApp) button(name string) {
	switch {
	case name == "pick":
		// The shell covers the screens with their still; the window
		// stays, so the pick shows here when it is done.
		go func() {
			if out, err := exec.Command("inua", "call", "screenshot", "color").CombinedOutput(); err != nil {
				log.Printf("colors: %v %s", err, strings.TrimSpace(string(out)))
			}
		}()
	case name == "clear":
		a.data.History = nil
		a.current = ""
		if err := store.Save(colors.StoreName, a.data); err != nil {
			log.Printf("colors: %v", err)
		}
		a.load()
		a.show()
	case strings.HasPrefix(name, "copy:"):
		text := strings.TrimPrefix(name, "copy:")
		go exec.Command("wl-copy", "--", text).Run()
	case strings.HasPrefix(name, "show:"):
		a.current = strings.TrimPrefix(name, "show:")
		a.show()
	}
}

func (a *colorApp) show() {
	c, ok := colors.Parse(a.current)
	gtkx.Clear(&a.forms)
	if ok {
		tex := colorTexture(c)
		a.swatch.SetPaintable(tex)
		tex.Unref()
		for _, f := range [][2]string{{i18n.T("HEX"), c.Hex()}, {i18n.T("RGB"), c.RGB()}, {i18n.T("HSL"), c.HSL()}} {
			row := gtk.NewBox(gtk.OrientationHorizontalValue, 14)
			row.AddCssClass("row")
			k := gtk.NewLabel(f[0])
			k.AddCssClass("kicker")
			k.SetSizeRequest(44, -1)
			k.SetXalign(0)
			gtkx.Append(row, &k.Widget)
			v := gtk.NewLabel(f[1])
			v.AddCssClass("form-value")
			v.SetHexpand(true)
			v.SetXalign(0)
			v.SetSelectable(true)
			gtkx.Append(row, &v.Widget)
			gtkx.Append(row, &plainIcon("copy:"+f[1], "edit-copy-symbolic", i18n.Tf("Copy %s", f[1]), &a.onButton).Widget)
			gtkx.Append(&a.forms, &row.Widget)
		}
	} else {
		noPaintable(&a.swatch)
		l := gtk.NewLabel(i18n.T("No colour yet. Pick one from anywhere on the screen."))
		l.AddCssClass("empty")
		l.SetWrap(true)
		l.SetXalign(0)
		gtkx.Append(&a.forms, &l.Widget)
	}

	for ch := a.history.GetFirstChild(); ch != nil; ch = a.history.GetFirstChild() {
		a.history.Remove(ch)
	}
	for _, h := range a.data.History {
		hc, ok := colors.Parse(h)
		if !ok {
			continue
		}
		b := gtk.NewButton()
		b.AddCssClass("chip-swatch")
		b.SetHalign(gtk.AlignCenterValue)
		b.SetValign(gtk.AlignCenterValue)
		if strings.EqualFold(h, a.current) {
			b.AddCssClass("current")
		}
		b.SetName("show:" + h)
		b.SetTooltipText(h)
		p := gtk.NewPicture()
		tex := colorTexture(hc)
		p.SetPaintable(tex)
		tex.Unref()
		p.SetContentFit(gtk.ContentFitFillValue)
		p.SetSizeRequest(28, 28)
		b.SetChild(&p.Widget)
		p.Unref()
		b.ConnectClicked(&a.onButton)
		a.history.Append(&b.Widget)
		b.Unref()
	}
	a.empty.SetVisible(len(a.data.History) == 0)
}

// colorTexture is one pixel of a colour, for a picture to stretch.
func colorTexture(c colors.Color) *gdk.MemoryTexture {
	px := []byte{c.R, c.G, c.B, 255}
	bytes := glib.NewBytes(px, 4)
	defer bytes.Unref()
	return gdk.NewMemoryTexture(1, 1, gdk.MemoryR8g8b8a8Value, bytes, 4)
}
