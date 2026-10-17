package shell

import (
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/workshop"
)

func dockWin(addr, class string, history float64) hypr.Toplevel {
	return hypr.Toplevel{Address: addr, Class: class, Title: class + " window", Raw: map[string]any{"focusHistoryID": history}}
}

func TestDock(t *testing.T) {
	apps := NewAppIndex([]*desktop.Entry{
		{ID: "firefox", Name: "Firefox", Icon: "firefox"},
		{ID: "org.gnome.Nautilus", Name: "Files", Icon: "org.gnome.Nautilus"},
		{ID: "code", Name: "Code", Icon: "vscode", WMClass: "Code"},
		{ID: "kitty", Name: "kitty", Icon: "kitty"},
	})
	snap := hypr.Snapshot{
		Toplevels: []hypr.Toplevel{
			dockWin("0x1", "Code", 2),
			dockWin("0x2", "firefox", 1),
			dockWin("0x3", "nautilus", 3),
			dockWin("0x4", "firefox", 0),
			dockWin("0x5", "obscure-tool", 4),
		},
		ActiveToplevel: "0x4",
	}
	items := Dock([]string{"firefox", "missing.app", "kitty", "firefox"}, apps, snap, nil)
	var keys []string
	for _, it := range items {
		keys = append(keys, it.Key)
	}
	want := []string{"firefox", "kitty", "code", "org.gnome.Nautilus", "obscure-tool"}
	if len(keys) != len(want) {
		t.Fatalf("keys %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("keys %v, want %v", keys, want)
		}
	}
	ff := items[0]
	if !ff.Pinned || !ff.Active || len(ff.Windows) != 2 || ff.Windows[0].Address != "0x4" {
		t.Errorf("firefox: %+v", ff)
	}
	if items[1].Pinned != true || len(items[1].Windows) != 0 {
		t.Errorf("kitty: %+v", items[1])
	}
	if items[2].Entry == nil || items[2].Icon != "vscode" || items[2].Pinned {
		t.Errorf("code (by StartupWMClass): %+v", items[2])
	}
	if items[4].Entry != nil || items[4].Icon != "obscure-tool" {
		t.Errorf("no entry: %+v", items[4])
	}

	// Clicks: launch; bring forward the latest window; cycle when in front.
	if launch, _ := DockTarget(items[1]); !launch {
		t.Error("kitty should launch")
	}
	if _, addr := DockTarget(items[0]); addr != "0x2" {
		t.Errorf("firefox in front: next window %s, want 0x2", addr)
	}
	if _, addr := DockTarget(items[2]); addr != "0x1" {
		t.Errorf("code: %s", addr)
	}
	if launch, addr := DockTarget(DockItem{Key: "x"}); launch || addr != "" {
		t.Error("an app with no entry and no window does nothing")
	}
}

func TestDockSkipsFcitxWindows(t *testing.T) {
	apps := NewAppIndex([]*desktop.Entry{
		{ID: "org.fcitx.Fcitx5", Name: "Fcitx 5", Icon: "fcitx", Command: []string{"fcitx5"}},
	})
	snap := hypr.Snapshot{Toplevels: []hypr.Toplevel{
		dockWin("0x1", "fcitx", 0),
		dockWin("0x2", "fcitx5", 1),
		dockWin("0x3", "org.fcitx.Fcitx5", 2),
		dockWin("0x4", "fcitx5-wayland-launcher", 3),
		dockWin("0x5", "fcitx-rime", 4),
	}}

	if got := Dock(nil, apps, snap, nil); len(got) != 0 {
		t.Errorf("Dock() = %+v, want no input-method items", got)
	}
}

// The shell's own windows (Settings and its dialogs) show as Settings,
// with its icon, instead of an icon named after the class.
func TestDockShellWindows(t *testing.T) {
	snap := hypr.Snapshot{Toplevels: []hypr.Toplevel{dockWin("0x1", AppID, 0)}}
	got := Dock(nil, NewAppIndex(nil), snap, nil)
	if len(got) != 1 || got[0].Name != "Settings" || got[0].Icon != "io.github.lyricat.Inua.Settings" || got[0].Entry != nil {
		t.Errorf("Dock() = %+v", got)
	}
}

// A window of the input method's process is left out whatever its class.
func TestDockSkips(t *testing.T) {
	telegram := dockWin("0x1", "org.telegram.desktop", 1)
	popup := dockWin("0x2", "org.telegram.desktop", 0)
	popup.PID = 42
	snap := hypr.Snapshot{Toplevels: []hypr.Toplevel{telegram, popup}}
	got := Dock(nil, NewAppIndex(nil), snap, func(t hypr.Toplevel) bool { return t.PID == 42 })
	if len(got) != 1 || len(got[0].Windows) != 1 || got[0].Windows[0].Address != "0x1" {
		t.Errorf("Dock() = %+v, want Telegram's own window only", got)
	}
}

func TestIsInputMethodProgram(t *testing.T) {
	for name, want := range map[string]bool{
		"fcitx":           true,
		"fcitx5":          true,
		"fcitx5\n":        true,
		"fcitx5-config-q": false, // comm keeps 15 characters
		"telegram-deskto": false,
	} {
		if got := IsInputMethodProgram(name); got != want {
			t.Errorf("IsInputMethodProgram(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestPin(t *testing.T) {
	p := Pin([]string{"a", "b"}, "c", true)
	p = Pin(p, "a", false)
	p = Pin(p, "b", true) // moves to the end
	if len(p) != 2 || p[0] != "c" || p[1] != "b" {
		t.Errorf("got %v", p)
	}
}

func TestMovePinned(t *testing.T) {
	p := []string{"a", "b", "c"}
	for _, c := range []struct {
		key, before string
		want        string
	}{
		{"c", "a", "c,a,b"}, // to the front
		{"a", "c", "b,a,c"}, // forward, before c
		{"a", "", "b,c,a"},  // to the end
		{"b", "b", "a,b,c"}, // onto itself
		{"x", "b", "a,x,b,c"},
		{"x", "missing", "a,b,c,x"},
	} {
		if got := strings.Join(MovePinned(p, c.key, c.before), ","); got != c.want {
			t.Errorf("MovePinned(%s before %q) = %s, want %s", c.key, c.before, got, c.want)
		}
	}
	if strings.Join(p, ",") != "a,b,c" {
		t.Error("MovePinned changed its argument")
	}
}

func TestLightApps(t *testing.T) {
	apps := NewAppIndex([]*desktop.Entry{
		{ID: "io.github.lyricat.Inua.Calculator", Name: "Calculator"},
		{ID: "io.github.lyricat.Inua.Clock", Name: "Clock"},
		{ID: "firefox", Name: "Firefox"},
	})
	var names []string
	for _, e := range LightApps(apps) {
		names = append(names, e.Name)
	}
	if got := strings.Join(names, ","); got != "Clock,Calculator,Screenshot,Settings,Launcher" {
		t.Fatalf("LightApps = %s", got)
	}
}

func TestLightAppSections(t *testing.T) {
	apps := NewAppIndex([]*desktop.Entry{
		{ID: "io.github.lyricat.Inua.Viewer", Name: "Viewer"},
		{ID: "io.github.lyricat.Inua.Weather", Name: "Weather"},
		{ID: "io.github.lyricat.Inua.Clock", Name: "Clock"},
		{ID: "io.github.lyricat.Inua.VM", Name: "Windows VM"},
	})
	got := LightAppSections(apps)
	if len(got) != 3 || got[0].Title != "EVERYDAY" || len(got[0].Apps) != 2 || got[0].Apps[0].Name != "Clock" ||
		got[1].Title != "TOOLS" || len(got[1].Apps) != 2 || got[1].Apps[0].Name != "Screenshot" ||
		got[2].Title != "SYSTEM" || len(got[2].Apps) != 3 {
		t.Fatalf("LightAppSections = %+v", got)
	}
	// The Windows manager is an app of its own (docs/inua-vm.md): it sits
	// with the shell's tools, and only once its desktop entry is installed.
	if last := got[2].Apps[len(got[2].Apps)-1]; last.ID != "io.github.lyricat.Inua.VM" {
		t.Errorf("the VM manager is not in SYSTEM: %+v", got[2].Apps)
	}
	// The shell's tools are there with no light app installed.
	if got := LightAppSections(NewAppIndex(nil)); len(got) != 2 || got[0].Title != "TOOLS" || got[1].Title != "SYSTEM" {
		t.Errorf("none installed: %+v", got)
	}
}

// The workshop drawer: the apps installed by category, by their entries
// or by their manifests; then the Workshop app, in a group of its own.
func TestWorkshopSections(t *testing.T) {
	todo := workshop.Manifest{ID: "dev.inua.todo", Name: "Todo", Category: "everyday"}
	hosts := workshop.Manifest{ID: "dev.inua.ssh-hosts", Name: "SSH Hosts", Description: "Hosts", Category: "Network"}
	odd := workshop.Manifest{ID: "dev.x.odd", Name: "Odd", Category: "spaceships"}
	bare := workshop.Manifest{ID: "dev.x.bare", Name: "Bare"}
	apps := NewAppIndex([]*desktop.Entry{
		{ID: todo.AppID(), Name: "Todo (entry)"},
		{ID: WorkshopApp, Name: "Workshop"},
	})
	got := WorkshopSections(apps, []workshop.Manifest{odd, hosts, todo, bare})
	var titles []string
	for _, g := range got {
		titles = append(titles, g.Title)
	}
	if strings.Join(titles, " ") != "EVERYDAY NETWORK OTHER WORKSHOP" {
		t.Fatalf("groups %v", titles)
	}
	if got[0].Apps[0].Name != "Todo (entry)" {
		t.Errorf("by its entry: %+v", got[0].Apps[0])
	}
	if e := got[1].Apps[0]; e.ID != hosts.AppID() || e.Name != "SSH Hosts" || strings.Join(e.Command, " ") != "inua-apps workshop dev.inua.ssh-hosts" || e.WMClass != hosts.AppID() {
		t.Errorf("made from the manifest: %+v", e)
	}
	if other := got[2].Apps; len(other) != 2 || other[0].Name != "Odd" || other[1].Name != "Bare" {
		t.Errorf("other: %+v", other)
	}
	if w := got[3].Apps; len(w) != 1 || w[0].ID != WorkshopApp {
		t.Errorf("the Workshop app: %+v", w)
	}
	if got := WorkshopSections(NewAppIndex(nil), nil); len(got) != 0 {
		t.Errorf("nothing installed: %+v", got)
	}
	if got := WorkshopSections(apps, nil); len(got) != 1 || got[0].Apps[0].ID != WorkshopApp {
		t.Errorf("only the Workshop app: %+v", got)
	}
}

func TestForClassLoose(t *testing.T) {
	apps := NewAppIndex([]*desktop.Entry{
		{ID: "mixin-desktop_mixin-desktop", Command: []string{"/snap/bin/mixin-desktop", "%u"}},
		{ID: "tool-runner", Command: []string{"env", "FOO=1", "thing"}},
		{ID: "org.example.Viewer", Command: []string{"/opt/viewer/bin/fast_view"}},
	})
	for class, want := range map[string]string{
		"mixin_desktop": "mixin-desktop_mixin-desktop", // a snap: the app part, - for _
		"Tool_Runner":   "tool-runner",                 // the ID, loosely
		"fast-view":     "org.example.Viewer",          // the program
		"env":           "",                            // not a launcher's name
	} {
		got := ""
		if e := apps.ForClass(class); e != nil {
			got = e.ID
		}
		if got != want {
			t.Errorf("ForClass(%q) = %q, want %q", class, got, want)
		}
	}
}
