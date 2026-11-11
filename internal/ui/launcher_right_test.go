//go:build linux

package ui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/shell"
)

// A right-click on a launcher result moves the highlight to it before its
// actions open, and gives back the reference GestureClick.GetWidget took:
// the result is freed once the launcher lets it go. Checked on a real GTK
// (a Broadway display).
func TestLauncherRightClick(t *testing.T) {
	if _, err := exec.LookPath("gtk4-broadwayd"); err != nil {
		t.Skip("gtk4-broadwayd not installed")
	}
	// GTK keeps the display it first opened for the whole process: this
	// runs in a process of its own, as TestBarRowsOnGTK does.
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

	app := gtk.NewApplication("io.github.lyricat.Inua.LauncherRightTest", gio.GApplicationNonUniqueValue)
	defer app.Unref()
	activate := func(gio.Application) {
		app.Hold()
		gtkx.Post(func() {
			defer app.Quit()
			defer app.Release()
			checkLauncherRightClick(t, app)
		})
	}
	app.ConnectActivate(&activate)
	if code := app.Run(1, []string{"launcher-right-test"}); code != 0 {
		t.Fatalf("application exit code = %d", code)
	}
}

func checkLauncherRightClick(t *testing.T, app *gtk.Application) {
	s := &shellUI{}
	s.bindLauncherHandlers()
	l := &s.launcher

	// An open launcher, its three results already built, the first
	// selected; results without actions, so no menu is parented to one.
	win := gtk.NewApplicationWindow(app)
	defer win.Destroy()
	l.win = win
	l.mode = *gtk.NewLabel("")
	l.query = *gtk.NewEntry()
	l.message = *gtk.NewLabel("")
	l.messageTag = *gtk.NewLabel("")
	l.messageBox = *gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	l.scroll = *gtk.NewScrolledWindow()
	l.fade = *gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	l.foot = *gtk.NewOverlay()
	l.foot.SetVisible(false)
	for _, w := range []*gtk.Widget{&l.mode.Widget, &l.query.Widget, &l.message.Widget, &l.messageTag.Widget, &l.messageBox.Widget, &l.scroll.Widget, &l.fade.Widget, &l.foot.Widget} {
		w.RefSink()
		defer w.Unref()
	}
	l.st = shell.Launcher{Open: true, Mode: shell.Windows, Results: make([]shell.Result, 3)}
	l.shown = l.st.Version
	l.items = make([]gtk.Widget, len(l.st.Results))
	results := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	results.RefSink()
	defer results.Unref()
	for i := range l.items {
		box := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		s.markLauncherResult(&box.Widget, i)
		gtkx.Append(results, &box.Widget)
	}
	s.renderLauncher()
	if !l.items[0].HasCssClass("selected") {
		t.Fatal("the first result is not marked selected")
	}

	// GObject's reference count, read where GObject keeps it (after the
	// type instance's class pointer): no API returns it.
	refs := func(w *gtk.Widget) uint32 {
		obj := *(*unsafe.Pointer)(unsafe.Pointer(&w.Ptr))
		return *(*uint32)(unsafe.Add(obj, unsafe.Sizeof(uintptr(0))))
	}
	right := gtk.NewGestureClick()
	right.SetButton(3)
	l.items[2].AddController(&right.EventController)
	before := refs(&l.items[2])
	for range 3 {
		s.onLauncherRight(*right, 1, 0, 0)
	}
	if after := refs(&l.items[2]); after != before {
		t.Errorf("result 2 has %d references after three right-clicks, %d before: each leaked one", after, before)
	}

	if l.st.Selected != 2 {
		t.Errorf("selected %d after a right-click on result 2", l.st.Selected)
	}
	for i := range l.items {
		if got, want := l.items[i].HasCssClass("selected"), i == 2; got != want {
			t.Errorf("result %d marked selected %v, want %v: the highlight did not follow the right-click", i, got, want)
		}
	}

	l.win = nil
}
