//go:build linux

package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/dbustest"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/sockdir"
)

// aboutCycles is more About windows than purego has callbacks (2000):
// a trampoline made at each opening would end the test in a panic.
const aboutCycles = 2500

// TestAppAboutWindows opens and closes the About window many times on a
// Broadway display, by its Close button and as the compositor closes it,
// and checks that each Close takes down its own window and that nothing
// keeps a closed window or its button alive.
func TestAppAboutWindows(t *testing.T) {
	broadway, err := exec.LookPath("gtk4-broadwayd")
	if err != nil {
		t.Skip("gtk4-broadwayd not installed")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Both display sockets and all application files belong to this test.
	dir := sockdir.New(t)
	for key, value := range map[string]string{
		"HOME": dir, "XDG_RUNTIME_DIR": dir,
		"XDG_CONFIG_HOME": filepath.Join(dir, "config"),
		"XDG_DATA_HOME":   filepath.Join(dir, "data"),
		"XDG_CACHE_HOME":  filepath.Join(dir, "cache"),
		"XDG_STATE_HOME":  filepath.Join(dir, "state"),
		"GDK_BACKEND":     "broadway", "BROADWAY_DISPLAY": ":0",
		"GTK_A11Y": "none", "GTK_THEME": "", "LANGUAGE": "en",
		"DBUS_SESSION_BUS_ADDRESS": dbustest.Start(t),
		"DBUS_SYSTEM_BUS_ADDRESS":  "unix:path=" + filepath.Join(dir, "no-system-bus"),
	} {
		t.Setenv(key, value)
	}
	cmd := exec.Command(broadway, "--unixsocket="+filepath.Join(dir, "http.sock"), ":0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "broadway1.socket")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Broadway display did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	app := gtk.NewApplication("io.github.lyricat.Inua.AboutTest", gio.GApplicationNonUniqueValue)
	defer app.Unref()
	activate := func(gio.Application) {
		app.Hold()
		gtkx.Post(func() {
			defer app.Quit()
			defer app.Release()
			checkAppAbout(t)
		})
	}
	app.ConnectActivate(&activate)
	if code := app.Run(1, []string{"about-test"}); code != 0 {
		t.Fatalf("application exit code = %d", code)
	}
}

// aboutWindow is an open About window, by its title.
type aboutWindow struct {
	title string
	win   gtk.Window // borrowed: GTK owns it
}

func checkAppAbout(t *testing.T) {
	s := &shellUI{}
	s.bindAppMenuHandlers()

	finalized := 0
	gone := gobject.WeakNotify(func(uintptr, uintptr) { finalized++ })
	watch := func(p uintptr) {
		o := gobject.Object{}
		o.Ptr = p
		o.WeakRef(&gone, 0)
	}
	settle := func() {
		ctx := glib.MainContextDefault()
		for ctx.Iteration(false) {
		}
	}
	open := func() []aboutWindow {
		list := gtk.WindowGetToplevels()
		defer (&gobject.Object{Ptr: list.Ptr}).Unref()
		var out []aboutWindow
		for i := range list.GetNItems() {
			o := list.GetObject(i)
			w := gtk.Window{}
			w.Ptr = o.Ptr
			if w.HasCssClass("about") {
				out = append(out, aboutWindow{w.GetTitle(), w})
			}
			o.Unref() // GetObject's
		}
		return out
	}
	// closeButton is the window's Close button, with a reference to
	// release.
	closeButton := func(w *gtk.Window) *gtk.Widget {
		body := w.GetChild()
		if body == nil {
			return nil
		}
		defer body.Unref()
		return body.GetLastChild()
	}
	click := func(b *gtk.Widget) {
		o := gobject.Object{}
		o.Ptr = b.Ptr
		gobject.SignalEmitByName(&o, "clicked")
	}

	// Two at once are each their own: Close takes down its window only,
	// and the compositor's close the other.
	s.showAppAbout(compositor.Window{ID: "a", AppID: "alpha", Title: "first", PID: 1})
	s.showAppAbout(compositor.Window{ID: "b", AppID: "beta", Title: "second", PID: 2})
	wins := open()
	if len(wins) != 2 {
		t.Fatalf("open About windows = %d, want 2", len(wins))
	}
	for _, w := range wins {
		watch(w.win.Ptr)
		if b := closeButton(&w.win); b != nil {
			watch(b.Ptr)
			if w.title == "About beta" {
				click(b)
			}
			b.Unref()
		}
	}
	settle()
	if wins = open(); len(wins) != 1 || wins[0].title != "About alpha" {
		t.Fatalf("after beta's Close, open = %v, want About alpha alone", titles(wins))
	}
	wins[0].win.Close()
	settle()
	if wins = open(); len(wins) != 0 {
		t.Fatalf("after the compositor's close, open = %v", titles(wins))
	}
	if finalized != 4 {
		t.Fatalf("finalized %d of 2 windows and their buttons", finalized)
	}

	// Many times over, by Close and by the compositor in turn.
	finalized = 0
	for i := range aboutCycles {
		s.showAppAbout(compositor.Window{ID: "w", AppID: fmt.Sprint("app", i), Title: "window", PID: i})
		wins := open()
		if len(wins) != 1 || wins[0].title != fmt.Sprint("About app", i) {
			t.Fatalf("opening %d: open = %v", i, titles(wins))
		}
		w := wins[0].win
		watch(w.Ptr)
		b := closeButton(&w)
		if b == nil {
			t.Fatalf("opening %d: no Close button", i)
		}
		watch(b.Ptr)
		if i%2 == 0 {
			click(b)
		} else {
			w.Close()
		}
		b.Unref()
		if i%100 == 0 {
			settle()
		}
	}
	settle()
	if wins := open(); len(wins) != 0 {
		t.Fatalf("after %d openings, still open: %d", aboutCycles, len(wins))
	}
	if finalized != 2*aboutCycles {
		t.Fatalf("finalized %d of %d windows and their buttons", finalized, 2*aboutCycles)
	}
}

func titles(wins []aboutWindow) []string {
	var out []string
	for _, w := range wins {
		out = append(out, w.title)
	}
	return out
}
