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
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/compositor/compositortest"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/uikit"
)

func TestAppMenuCascade(t *testing.T) {
	if _, err := exec.LookPath("gtk4-broadwayd"); err != nil {
		t.Skip("gtk4-broadwayd not installed")
	}
	if os.Getenv("INUA_UI_TEST") != t.Name() {
		cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.v")
		cmd.Env = append(os.Environ(), "INUA_UI_TEST="+t.Name())
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if strings.Contains(string(out), "--- SKIP") {
			t.Skipf("%s", out)
		}
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	startBroadway(t)
	app := gtk.NewApplication("io.github.lyricat.Inua.CascadeTest", gio.GApplicationNonUniqueValue)
	defer app.Unref()
	activate := func(gio.Application) {
		app.Hold()
		gtkx.Post(func() {
			defer app.Quit()
			defer app.Release()
			win := gtk.NewApplicationWindow(app)
			anchor := gtk.NewButton()
			anchor.SetLabel("Menu")
			win.SetChild(&anchor.Widget)
			win.Present()
			pop := uikit.NewCascadeMenu([]uikit.Item{{Label: "Submenu", Items: []uikit.Item{{Label: "Action", Run: func() {}}}}})
			pop.SetParent(&anchor.Widget)
			pop.Popup()
			ctx := glib.MainContextDefault()
			for ctx.Iteration(false) {
			}
			box := pop.GetChild()
			if box == nil {
				t.Error("missing menu")
				return
			}
			row := box.GetFirstChild()
			if row == nil {
				t.Error("missing row")
				return
			}
			gobject.SignalEmitByName(&gobject.Object{Ptr: row.Ptr}, "clicked")
			for ctx.Iteration(false) {
			}
			var child *gtk.Popover
			for w := row.GetFirstChild(); w != nil; w = w.GetNextSibling() {
				if w.HasCssClass("glass-menu") {
					child = gtk.PopoverNewFromInternalPtr(w.GoPointer())
					break
				}
			}
			if child == nil || !child.GetVisible() || child.GetPosition() != gtk.PosRightValue || !pop.GetVisible() {
				t.Errorf("parent and side submenu not both visible: child=%v", child != nil)
			}
			pop.Popdown()
			pop.Unparent()
			pop.Unref()
			win.Destroy()
			win.Unref()
			checkTopMenuHover(t, app)
		})
	}
	app.ConnectActivate(&activate)
	if code := app.Run(1, []string{"cascade-test"}); code != 0 {
		t.Fatalf("application exit %d", code)
	}
}

// The first click opens a menu; moving over its neighbour replaces it.
func checkTopMenuHover(t *testing.T, app *gtk.Application) {
	monitors := gdk.DisplayGetDefault().GetMonitors()
	if monitors.GetNItems() == 0 {
		t.Fatal("no monitor")
	}
	b := &bar{mon: gdk.MonitorNewFromInternalPtr(monitors.GetItem(0))}
	defer b.mon.Unref()
	b.appMenu = *gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	output := b.connector()
	b.appMenuWindow = "0x1"
	s := &shellUI{bars: map[uintptr]*bar{1: b}, snap: compositor.Snapshot{Outputs: []compositor.Output{{Name: output, ActiveWorkspace: "1"}}, Windows: []compositor.Window{{ID: "0x1", Workspace: "1", Output: output}}, FocusedWindow: "0x1", FocusedOutput: output}}
	s.bindAppMenuHandlers()
	entry := func(label string) appmenu.Entry {
		return appmenu.Entry{Label: label, Enabled: true, Children: []appmenu.Entry{{Label: "Action", Enabled: true}}}
	}
	s.renderAppMenu(b, []appmenu.Entry{entry("File"), entry("Edit")})
	win := gtk.NewApplicationWindow(app)
	win.SetChild(&b.appMenu.Widget)
	win.Present()
	ctx := glib.MainContextDefault()
	for ctx.Iteration(false) {
	}
	first := b.appMenu.GetFirstChild()
	second := first.GetNextSibling()
	button := gtk.ButtonNewFromInternalPtr(first.GoPointer())
	s.onAppMenuClicked(*button)
	if s.appmenu.menu == nil || !s.appmenu.menu.GetVisible() {
		t.Fatal("click did not open menu")
	}
	motion := gtk.NewEventControllerMotion()
	second.AddController(&motion.EventController)
	s.onAppMenuHover(*motion, 1, 1)
	if s.appmenu.menu == nil || !s.appmenu.menu.GetVisible() || s.appmenu.menu.GetParent().GoPointer() != second.GoPointer() {
		t.Error("hover did not switch menu")
	}
	s.closeAppMenu()
	// Clicking this monitor's menu first focuses its own window.
	fake := compositortest.New()
	s.comp = fake
	s.snap.Outputs = append(s.snap.Outputs, compositor.Output{Name: "other", ActiveWorkspace: "2"})
	s.snap.Windows = append(s.snap.Windows, compositor.Window{ID: "0x2", Workspace: "2", Output: "other"})
	s.snap.FocusedWindow, s.snap.FocusedOutput = "0x2", "other"
	s.onAppMenuClicked(*button)
	deadline := time.Now().Add(time.Second)
	for len(fake.Actions()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := fake.Actions(); len(got) != 1 || got[0] != "focus-window 0x1" {
		t.Fatalf("focus requests %v", got)
	}
	s.snap.FocusedWindow, s.snap.FocusedOutput = "0x1", output
	s.openPendingAppMenu()
	if s.appmenu.menu == nil || !s.appmenu.menu.GetVisible() || s.appmenu.menu.GetParent().GoPointer() != first.GoPointer() {
		t.Error("focused monitor menu did not open")
	}
	s.closeAppMenu()
	win.Destroy()
	win.Unref()
}
