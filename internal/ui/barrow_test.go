//go:build linux

package ui

import (
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/gtkx"
)

// The bar's island and the settings' small bar, given the rows' layout
// on a real GTK (a Broadway display), put the group between two spaces
// in the middle of the whole row, its padding included, however wide the
// group, with an item beside it hidden, the room either side uneven; and
// the island still fills with the default's centred box. Checked by the
// allocations, not by eye.
func TestBarRowsOnGTK(t *testing.T) {
	if _, err := exec.LookPath("gtk4-broadwayd"); err != nil {
		t.Skip("gtk4-broadwayd not installed")
	}
	// GTK keeps the display it first opened for the whole process: this
	// runs in a process of its own, as TestAppAboutWindows does not.
	if os.Getenv("INUA_UI_TEST") != t.Name() {
		cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.v")
		cmd.Env = append(os.Environ(), "INUA_UI_TEST="+t.Name())
		out, err := cmd.CombinedOutput()
		switch {
		case err != nil:
			t.Fatalf("%v\n%s", err, out)
		case strings.Contains(string(out), "--- SKIP"):
			t.Skipf("%s", out)
		}
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	startBroadway(t)

	app := gtk.NewApplication("io.github.lyricat.Inua.BarRowTest", gio.GApplicationNonUniqueValue)
	defer app.Unref()
	activate := func(gio.Application) {
		app.Hold()
		gtkx.Post(func() {
			defer app.Quit()
			defer app.Release()
			checkBarRows(t)
		})
	}
	app.ConnectActivate(&activate)
	if code := app.Run(1, []string{"bar-row-test"}); code != 0 {
		t.Fatalf("application exit code = %d", code)
	}
}

func checkBarRows(t *testing.T) {
	css := gtk.NewCssProvider()
	css.LoadFromString(".row { padding: 0 8px 0 12px; border: 1px solid black; }")
	gtk.StyleContextAddProviderForDisplay(gdk.DisplayGetDefault(), css, uint32(gtk.STYLE_PROVIDER_PRIORITY_APPLICATION))

	s := &shellUI{}
	b := &bar{}
	box := func(w int32) gtk.Box {
		x := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		x.SetSizeRequest(w, 20)
		return *x
	}
	b.island = box(-1)
	b.island.AddCssClass("row")
	b.workspaces, b.appMenu, b.tray = box(100), box(80), box(300)
	b.appButton = *gtk.NewButton() // the application's icon and name
	b.appButton.SetSizeRequest(60, 20)
	b.centered = *gtk.NewCenterBox()
	b.centered.SetHexpand(true)
	b.island.Append(&b.centered.Widget)
	s.layIsland(b)

	win := gtk.NewWindow()
	win.SetDecorated(false)
	win.SetDefaultSize(1200, 40)
	win.SetChild(&b.island.Widget)
	win.Present()
	defer win.Destroy()

	// lay gives row width as its window would (Broadway draws no
	// frames without a browser, so nothing would lay it out again).
	lay := func(row *gtk.Widget, width int32) {
		ctx := glib.MainContextDefault()
		for ctx.Iteration(false) {
		}
		var m, n int32
		row.Measure(gtk.OrientationHorizontalValue, -1, &m, &n, nil, nil)
		row.Measure(gtk.OrientationVerticalValue, -1, &m, &n, nil, nil)
		row.Allocate(width, max(n, 30), -1, nil)
	}
	bounds := func(w, of *gtk.Widget) (x, width float64) {
		var r graphene.Rect
		if !w.ComputeBounds(of, &r) {
			t.Fatal("no bounds")
		}
		f := *(*[4]float32)(unsafe.Pointer(&r))
		return float64(f[0]), float64(f[2])
	}
	// middle is the middle of the widgets from..to against that of the
	// row's border box.
	middle := func(row, from, to *gtk.Widget) (got, want float64) {
		x, w := bounds(row, row)
		a, _ := bounds(from, row)
		c, cw := bounds(to, row)
		return (a + c + cw) / 2, x + w/2
	}

	// The default: the centred box fills the island.
	lay(&b.island.Widget, 1200)
	if x, w := bounds(&b.centered.Widget, &b.island.Widget); x != 0 || int32(w) != b.island.GetWidth() {
		t.Errorf("the default's box at %v, %v wide, in an island %d wide", x, w, b.island.GetWidth())
	}

	// A layout of the user's, as arrange has it: the group of the app's
	// menu and title between the spaces, the tray beside them.
	b.centered.SetVisible(false)
	b.layout = []string{"workspaces", config.BarSpace, "app_menu", "app_title", config.BarSpace, "tray"}
	for _, id := range b.layout {
		if id == config.BarSpace {
			sp := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
			sp.SetHexpand(true)
			b.island.Append(&sp.Widget)
			b.spaces = append(b.spaces, sp)
			continue
		}
		b.island.Append(b.item(id))
	}
	check := func(what string) {
		t.Helper()
		for _, width := range []int32{1024, 1200, 1920} {
			lay(&b.island.Widget, width)
			if got, want := middle(&b.island.Widget, &b.appMenu.Widget, &b.appButton.Widget); math.Abs(got-want) > 0.5 {
				t.Errorf("%s, %d wide: the group's middle at %v, the bar's at %v", what, width, got, want)
			}
		}
	}
	check("the bar")
	b.appMenu.SetSizeRequest(230, 20)
	check("the group wider")
	b.tray.SetVisible(false)
	check("the tray hidden")
	b.tray.SetVisible(true)
	b.workspaces.SetSizeRequest(250, 20)
	check("the start wider")
	// Too wide to be centred: the group stops beside it.
	b.workspaces.SetSizeRequest(600, 20)
	lay(&b.island.Widget, 1200)
	if x, _ := bounds(&b.appMenu.Widget, &b.island.Widget); x != 600+2*islandSpacing {
		t.Errorf("the start too wide: the group at %v, not beside it", x)
	}

	// The settings' small bar, the same layout in chips.
	bl := &s.settings.barLayout
	bl.strip = box(-1)
	bl.strip.AddCssClass("row")
	s.layStrip()
	bl.shown = b.layout
	for _, id := range bl.shown {
		c := gtk.NewToggleButton()
		if id == config.BarSpace {
			c.SetLabel("Space")
			c.SetHexpand(true)
		} else {
			c.SetSizeRequest(40, 20)
		}
		bl.strip.Append(&c.Widget)
		bl.chips = append(bl.chips, c)
	}
	strip := gtk.NewWindow()
	strip.SetDecorated(false)
	strip.SetDefaultSize(700, 40)
	strip.SetChild(&bl.strip.Widget)
	strip.Present()
	defer strip.Destroy()
	lay(&bl.strip.Widget, 700)
	if got, want := middle(&bl.strip.Widget, &bl.chips[2].Widget, &bl.chips[3].Widget); math.Abs(got-want) > 0.5 {
		t.Errorf("the preview: the group's middle at %v, the strip's at %v", got, want)
	}
	if _, w1 := bounds(&bl.chips[1].Widget, &bl.strip.Widget); w1 >= 700/2 {
		t.Errorf("the preview's first space %v wide", w1)
	}
}
