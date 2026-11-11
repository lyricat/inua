//go:build linux

package ui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/gtkx"
)

// With bar.menu off the bar shows no menu, neither the system menu over an
// empty desktop nor a window's pack or generic menu, however the menus are
// asked for again: the end of a search, the registrar, a pack reload or a
// published menu's update. Turned on, the same bar shows them; turned off
// again, they go. Checked on a real GTK (a Broadway display).
func TestBarMenuOffStaysOff(t *testing.T) {
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
	isolatePacks(t)

	app := gtk.NewApplication("io.github.lyricat.Inua.BarMenuOffTest", gio.GApplicationNonUniqueValue)
	defer app.Unref()
	activate := func(gio.Application) {
		app.Hold()
		gtkx.Post(func() {
			defer app.Quit()
			defer app.Release()
			checkBarMenuOff(t)
		})
	}
	app.ConnectActivate(&activate)
	if code := app.Run(1, []string{"bar-menu-off-test"}); code != 0 {
		t.Fatalf("application exit code = %d", code)
	}
}

func checkBarMenuOff(t *testing.T) {
	monitors := gdk.DisplayGetDefault().GetMonitors()
	if monitors.GetNItems() == 0 {
		t.Fatal("the Broadway display has no monitor")
	}
	b := &bar{mon: gdk.MonitorNewFromInternalPtr(monitors.GetItem(0))}
	defer b.mon.Unref()
	b.appMenu = *gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	b.appMenu.Ref()
	defer b.appMenu.Unref()
	output := b.connector()

	s := &shellUI{bars: map[uintptr]*bar{1: b}}
	s.appmenu.packs = appmenu.NewPacks("")
	s.appmenu.packs.Refresh()

	// An empty desktop gets the system menu; a focused window without a
	// process (no published or accessibility menu to wait for) its
	// generic menu.
	empty := compositor.Snapshot{
		Outputs:       []compositor.Output{{Name: output, ActiveWorkspace: "1"}},
		FocusedOutput: output,
	}
	window := empty
	window.Windows = []compositor.Window{{ID: "0xb", AppID: "inua-test", Title: "Test", Workspace: "1", Output: output, Focused: true}}
	window.FocusedWindow = "0xb"

	settle := func() {
		ctx := glib.MainContextDefault()
		for ctx.Iteration(false) {
		}
	}
	shown := func() int {
		n := 0
		for c := b.appMenu.GetFirstChild(); c != nil; c = c.GetNextSibling() {
			n++
		}
		return n
	}
	// Every way the shell asks for the bars' menus again.
	sources := []struct {
		name string
		ask  func()
	}{
		{"sync", s.syncAppMenus},
		{"registrar change", func() {
			for _, b := range s.bars {
				b.appMenuSearch.stale = true
			}
			s.syncAppMenus()
		}},
		{"search ended", func() {
			request, ok := b.appMenuSearch.beginSearch("0xb-again")
			if ok && b.appMenuSearch.endSearch(request, appmenu.Registration{}) {
				s.syncAppMenus()
			}
		}},
		{"pack reload", func() {
			s.checkAppMenuPacks()
			deadline := time.Now().Add(200 * time.Millisecond)
			for time.Now().Before(deadline) {
				settle()
				time.Sleep(5 * time.Millisecond)
			}
		}},
		{"published menu update", func() {
			s.reloadAppMenu(appmenu.Registration{Service: ":1.9", Path: "/MenuBar/1"})
		}},
	}
	for _, snap := range []struct {
		name string
		snap compositor.Snapshot
	}{{"empty desktop", empty}, {"focused window", window}} {
		s.snap = snap.snap

		s.cfg.Bar.Menu = false
		s.syncAppMenu(b) // as setHypr does
		for _, src := range sources {
			src.ask()
			settle()
			if n := shown(); n != 0 || b.appMenu.GetVisible() || b.appMenuSystem || b.appMenuWindow != "" {
				t.Errorf("%s, bar.menu off, after %s: %d buttons, visible %v, system %v, window %q",
					snap.name, src.name, n, b.appMenu.GetVisible(), b.appMenuSystem, b.appMenuWindow)
			}
		}

		s.cfg.Bar.Menu = true
		s.syncAppMenu(b)
		settle()
		if n := shown(); n == 0 || !b.appMenu.GetVisible() {
			t.Errorf("%s, bar.menu on: %d buttons, visible %v; want the menu", snap.name, n, b.appMenu.GetVisible())
		}
		for _, src := range sources {
			src.ask()
			settle()
			if n := shown(); n == 0 {
				t.Errorf("%s, bar.menu on, after %s: the menu went away", snap.name, src.name)
			}
		}

		s.cfg.Bar.Menu = false
		s.syncAppMenu(b)
		settle()
		if n := shown(); n != 0 || b.appMenu.GetVisible() {
			t.Errorf("%s, bar.menu turned off: %d buttons, visible %v", snap.name, n, b.appMenu.GetVisible())
		}
	}
}
