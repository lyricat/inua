//go:build linux

package filesui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/dbustest"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/sockdir"
)

// Build real Files windows through appui, without device watchers, folder
// scans or portal services. The test keeps the application resident.
type windowTestView struct {
	a     App
	check func(*appui.Ctx, *window)
}

func (v *windowTestView) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	v.a.c, v.a.h = c, newHub()
	v.a.settings = files.LoadSettings()
	v.a.icons = newThemeIcons("Adwaita")
	c.StartHidden = true
	c.App.Hold()
	w := v.a.newWindow()
	gtkx.Post(func() {
		defer c.App.Quit()
		defer c.App.Release()
		v.check(c, w)
	})
	return &w.win
}

func (*windowTestView) ThemeChanged() {}

func TestWindowStylesAfterReopen(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	startBroadway(t)

	cfgPath := config.DefaultPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	v := &windowTestView{}
	v.check = func(c *appui.Ctx, first *window) {
		check := func(label string, w *window, userColor string) {
			t.Helper()
			for _, class := range []string{"app", "settings", "files"} {
				if !w.win.HasCssClass(class) {
					t.Errorf("%s: missing CSS class %q", label, class)
				}
			}
			app := w.win.GetApplication()
			if app == nil {
				t.Errorf("%s: window has no application", label)
			} else {
				if app.GoPointer() != c.App.GoPointer() {
					t.Errorf("%s: window belongs to another application", label)
				}
				app.Unref()
			}
			if w.win.GetTitle() != "Files" {
				t.Errorf("%s: title = %q", label, w.win.GetTitle())
			}
			style := w.win.GetStyleContext()
			defer style.Unref()
			var got, want gdk.RGBA
			want.Parse(c.Colors["t0"])
			if !style.LookupColor("t0", &got) || got != want {
				t.Errorf("%s: built-in theme token = %v, want %v", label, got, want)
			}
			if userColor != "" {
				want.Parse(userColor)
			}
			style.GetColor(&got)
			if got != want {
				t.Errorf("%s: foreground = %v, want %v", label, got, want)
			}
		}
		closeWindow := func(w *window) {
			if w.closeRequested() {
				t.Error("idle window refused to close")
			}
			w.win.Destroy()
		}
		defer func() {
			for len(v.a.wins) > 0 {
				closeWindow(v.a.wins[0])
			}
		}()
		check("first launch", first, "")
		second := v.a.newWindow()
		check("second window", second, "")
		closeWindow(first)
		check("surviving window", second, "")
		closeWindow(second)

		for _, step := range []struct{ name, config, color string }{
			{"missing config", "", ""},
			{"broken config", "[style", ""},
			{"light theme and user CSS", "[style]\ntheme = 'washi'\n", "#123456"},
			{"dark theme and changed user CSS", "[style]\ntheme = 'yoru'\n", "#654321"},
		} {
			if step.config != "" {
				if err := os.WriteFile(cfgPath, []byte(step.config), 0o600); err != nil {
					t.Error(err)
					return
				}
			}
			if step.color != "" {
				css := "window.files { color: " + step.color + "; }"
				if err := os.WriteFile(filepath.Join(filepath.Dir(cfgPath), "style.css"), []byte(css), 0o600); err != nil {
					t.Error(err)
					return
				}
			}
			c.ReloadStyle() // also works with no windows left
			for range 3 {
				w := v.a.takeSpare()
				check(step.name+" reopen", w, step.color)
				c.ReloadStyle()
				check(step.name+" reload", w, step.color)
				closeWindow(w)
			}
		}
	}
	if code := appui.RunView("files", appui.IDBase+".FilesTest", "Files", v, true, nil, ""); code != 0 {
		t.Fatalf("application exit code = %d", code)
	}
}

// startBroadway gives the test a Broadway display, a private session bus
// and a home of their own, and returns that home.
func startBroadway(t *testing.T) string {
	t.Helper()
	broadway, err := exec.LookPath("gtk4-broadwayd")
	if err != nil {
		t.Skip("gtk4-broadwayd not installed")
	}
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
	return dir
}
