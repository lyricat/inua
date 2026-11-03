package hyprconf

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/config"
)

func ptr[T any](v T) *T { return &v }

func TestRenderNothing(t *testing.T) {
	got := Render(config.Config{Input: config.Input{}}, Devices{Touchpads: []string{"a-touchpad"}})
	if strings.Contains(got, "input {") || strings.Contains(got, "device {") {
		t.Errorf("unset settings were written:\n%s", got)
	}
}

func TestRender(t *testing.T) {
	in := config.Input{KbOptions: ptr("caps:escape,ctrl:swap_lalt_lctl"), TouchpadSpeed: ptr(0.3), TouchpadAccel: "adaptive"}
	got := Render(config.Config{Input: in}, Devices{Touchpads: []string{"acme-inc.-trackpad-2", "elan0000:00-04f3:0000-touchpad"}})
	for _, want := range []string{
		"input {\n    kb_options = caps:escape,ctrl:swap_lalt_lctl\n}\n",
		"device {\n    name = acme-inc.-trackpad-2\n    sensitivity = 0.30\n    accel_profile = adaptive\n}\n",
		"device {\n    name = elan0000:00-04f3:0000-touchpad\n    sensitivity = 0.30\n    accel_profile = adaptive\n}\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing\n%s\nin\n%s", want, got)
		}
	}
	// Only what is set: speed without an acceleration profile.
	got = Render(config.Config{Input: config.Input{TouchpadSpeed: ptr(-0.5)}}, Devices{Touchpads: []string{"x-touchpad"}})
	if strings.Contains(got, "accel_profile") || strings.Contains(got, "kb_options") || !strings.Contains(got, "sensitivity = -0.50") {
		t.Errorf("got\n%s", got)
	}
	// An empty kb_options is a setting: it clears hyprland.conf's.
	if got := Render(config.Config{Input: config.Input{KbOptions: ptr("")}}, Devices{}); !strings.Contains(got, "kb_options = \n") {
		t.Errorf("got\n%s", got)
	}
}

// Hyprland accepts what Render writes.
func TestRenderVerifies(t *testing.T) {
	if _, err := exec.LookPath("Hyprland"); err != nil {
		t.Skip("Hyprland not installed")
	}
	path := filepath.Join(t.TempDir(), "hyprland.conf")
	in := config.Input{KbOptions: ptr("caps:escape"), TouchpadSpeed: ptr(0.3), TouchpadAccel: "flat"}
	if err := os.WriteFile(path, []byte(Render(config.Config{Input: in}, Devices{Touchpads: []string{"x-touchpad"}})), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("Hyprland", "--verify-config", "-c", path).CombinedOutput()
	if !strings.Contains(string(out), "config ok") {
		t.Errorf("Hyprland rejects the file:\n%s", out)
	}
}

func TestTouchpads(t *testing.T) {
	reply := `{"mice": [
		{"name": "laptop-consumer-control-1"},
		{"name": "elan0000:00-04f3:0000-mouse"},
		{"name": "elan0000:00-04f3:0000-touchpad"},
		{"name": "acme-inc.-trackpad-2"},
		{"name": "evil-touchpad\nexec = x"}
	], "keyboards": []}`
	got, err := Touchpads([]byte(reply))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "elan0000:00-04f3:0000-touchpad,acme-inc.-trackpad-2" {
		t.Errorf("got %q", got)
	}
}

func TestApplyWithoutHyprland(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hypr", Name)
	ctx := context.Background()
	in := config.Input{KbOptions: ptr("ctrl:swap_lalt_lctl")}
	if changed, err := Apply(ctx, config.Config{Input: in}, path, nil); err != nil || !changed {
		t.Fatalf("first apply: changed=%v err=%v", changed, err)
	}
	if changed, err := Apply(ctx, config.Config{Input: in}, path, nil); err != nil || changed {
		t.Errorf("same settings rewrote the file: changed=%v err=%v", changed, err)
	}
	// A touchpad the file already has keeps its block while unplugged.
	old := Render(config.Config{Input: config.Input{TouchpadSpeed: ptr(0.1)}}, Devices{Touchpads: []string{"bt-trackpad"}})
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, config.Config{Input: config.Input{TouchpadSpeed: ptr(0.4)}}, path, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "name = bt-trackpad\n    sensitivity = 0.40") {
		t.Errorf("got\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), LuaName)); !os.IsNotExist(err) {
		t.Errorf("inua.lua written for hyprland.conf: %v", err)
	}
	// The window and layer rules' names are not devices.
	if names := known(string(got)); !slices.Equal(names, []string{"bt-trackpad"}) {
		t.Errorf("known = %q, want only the touchpad", names)
	}
	// Nor are the device blocks an earlier inua wrote for them.
	if names := known(string(got) + "\ndevice {\n    name = inua-bar-still\n    sensitivity = 0.35\n}\n"); !slices.Equal(names, []string{"bt-trackpad"}) {
		t.Errorf("known = %q, want the rule's device block dropped", names)
	}
}

func TestPath(t *testing.T) {
	if got := Path("/home/u/.config/inua/config.toml"); got != "/home/u/.config/hypr/inua.conf" {
		t.Errorf("got %s", got)
	}
}

// The sample inua init installs is the file for no settings.
func TestSample(t *testing.T) {
	got, err := os.ReadFile("../../config/hypr/" + Name)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != Render(config.Config{}, Devices{}) {
		t.Errorf("config/hypr/%s is not Render(config.Config{}, Devices{})", Name)
	}
}

func TestKeys(t *testing.T) {
	if got := Keys(true, false, false, true, "s"); got != "SUPER+SHIFT+S" {
		t.Errorf("Keys: %s", got)
	}
	if got := Keys(false, true, true, false, "Return"); got != "CTRL+ALT+Return" {
		t.Errorf("Keys: %s", got)
	}
	if got := Label("SUPER+space"); got != "Super Space" {
		t.Errorf("Label: %s", got)
	}
	if !SameKeys("SUPER+space", "SUPER+Space") || SameKeys("SUPER+S", "SUPER+SHIFT+S") {
		t.Error("SameKeys")
	}
	reply := `[
		{"modmask": 64, "key": "Space", "dispatcher": "exec", "arg": "inua call launcher toggle", "submap": ""},
		{"modmask": 4, "key": "Print", "dispatcher": "exec", "arg": "inua call screenshot area", "submap": ""},
		{"modmask": 0, "key": "Print", "dispatcher": "exec", "arg": "inua call screenshot silent-area", "submap": ""},
		{"modmask": 64, "key": "mouse:272", "mouse": true, "dispatcher": "movewindow", "arg": ""},
		{"modmask": 0, "key": "x", "dispatcher": "exec", "arg": "inua call launcher toggle", "submap": "resize"},
		{"modmask": 64, "key": "N", "dispatcher": "__lua", "arg": "", "submap": "", "description": "notification center"}
	]`
	binds, err := ParseBinds([]byte(reply))
	if err != nil || len(binds) != 5 {
		t.Fatalf("binds %+v %v", binds, err)
	}
	c := config.Default()
	if k := KeysFor(binds, Command(c, "shot-area"), ""); len(k) != 1 || k[0] != "CTRL+Print" {
		t.Errorf("KeysFor shot-area: %v", k)
	}
	if k := KeysFor(binds, Command(c, "shot-area-silent"), ""); len(k) != 1 || k[0] != "Print" {
		t.Errorf("KeysFor shot-area-silent: %v", k)
	}
	if k := KeysFor(binds, Command(c, "launcher"), ""); len(k) != 1 {
		t.Errorf("KeysFor launcher (submap binds do not count): %v", k)
	}
	// A Lua bind is matched by its description, in any case.
	if k := KeysFor(binds, Command(c, "notifications"), "Notification center"); len(k) != 1 || k[0] != "SUPER+N" {
		t.Errorf("KeysFor by description: %v", k)
	}
	if k := KeysFor(binds, Command(c, "control"), "Control center"); len(k) != 0 {
		t.Errorf("KeysFor control: %v", k)
	}
	for _, a := range Actions {
		if Command(c, a.ID) == "" {
			t.Errorf("no command for %s", a.ID)
		}
	}
	if len(Actions) != len(config.ShortcutActions) {
		t.Error("Actions and config.ShortcutActions differ")
	}
}

func TestRenderShortcuts(t *testing.T) {
	c := config.Default()
	c.Shortcuts = []config.Shortcut{
		{Action: "launcher", Keys: "SUPER+D", Replaces: []string{"SUPER+Space"}},
		{Action: "shot-area", Keys: "", Replaces: []string{"Print"}},
		{Action: "exec", Keys: "SUPER+SHIFT+Return", Command: "kitty # a comment-looking arg"},
	}
	got := Render(c, Devices{})
	for _, want := range []string{
		"unbind = SUPER, Space\n",
		"unbind = SUPER, D\n",
		"unbind = , Print\n",
		"unbind = SUPER SHIFT, Return\n",
		"bind = SUPER, D, exec, inua call launcher toggle\n",
		"bind = SUPER SHIFT, Return, exec, kitty ## a comment-looking arg\n",
		"submap = " + RecordSubmap + "\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "wl-copy") {
		t.Error("an action turned off was bound")
	}
	if strings.Index(got, "unbind = SUPER, D") > strings.Index(got, "bind = SUPER, D") {
		t.Error("unbinds must come before binds")
	}
	if _, err := exec.LookPath("Hyprland"); err == nil {
		path := filepath.Join(t.TempDir(), "hyprland.conf")
		os.WriteFile(path, []byte(got), 0o644)
		if out, _ := exec.Command("Hyprland", "--verify-config", "-c", path).CombinedOutput(); !strings.Contains(string(out), "config ok") {
			t.Errorf("Hyprland rejects:\n%s", out)
		}
	}
}

// The widget page's swipe is there while the page is on, in both kinds of
// file. Three fingers take the workspace swipe's place, unsetting it only
// when the config has it (Hyprland refuses to unset one it has not got);
// four leave it. Hyprland accepts what is written.
func TestRenderWidgetGestures(t *testing.T) {
	c := config.Default()
	three := []string{
		"gesture = 3, right, dispatcher, exec, inua call widgets right\n",
		"gesture = 3, left, dispatcher, exec, inua call widgets left\n",
	}
	got := Render(c, Devices{WorkspaceSwipe: true})
	for _, want := range append(three, "gesture = 3, horizontal, unset\n") {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if got := Render(c, Devices{}); strings.Contains(got, "unset") || !strings.Contains(got, three[0]) {
		t.Errorf("without a workspace swipe:\n%s", got)
	}
	lua := RenderLua(c, Devices{WorkspaceSwipe: true})
	for _, want := range []string{
		`hl.gesture({ fingers = 3, direction = "horizontal", action = "unset" })`,
		`hl.gesture({ fingers = 3, direction = "right", action = function() hl.dispatch(hl.dsp.exec_cmd("inua call widgets right")) end })`,
	} {
		if !strings.Contains(lua, want) {
			t.Errorf("missing %q in\n%s", want, lua)
		}
	}
	c.Widgets.Swipe = 4
	got = Render(c, Devices{WorkspaceSwipe: true})
	if strings.Contains(got, "unset") || !strings.Contains(got, "gesture = 4, right, dispatcher, exec, inua call widgets open\n") {
		t.Errorf("four fingers:\n%s", got)
	}
	for _, off := range []func(*config.Config){
		func(c *config.Config) { c.Widgets.Enable = false },
		func(c *config.Config) { c.Widgets.Swipe = 0 },
	} {
		c := config.Default()
		off(&c)
		if got := Render(c, Devices{WorkspaceSwipe: true}) + RenderLua(c, Devices{WorkspaceSwipe: true}); strings.Contains(got, "gesture") {
			t.Errorf("a swipe while off:\n%s", got)
		}
	}
	// The page's blur, while it is on, swipe or not.
	c = config.Default()
	c.Widgets.Swipe = 0
	if got := Render(c, Devices{}); !strings.Contains(got, "match:namespace = ^inua-widgets$\n    blur = on") {
		t.Errorf("no blur:\n%s", got)
	}
	if got := RenderLua(c, Devices{}); !strings.Contains(got, `match = { namespace = "^inua-widgets$" },`) {
		t.Errorf("no blur in Lua:\n%s", got)
	}
	c.Widgets.Enable = false
	if got := Render(c, Devices{}); strings.Contains(got, "inua-widgets") {
		t.Errorf("a blur while the page is off:\n%s", got)
	}
	if _, err := exec.LookPath("Hyprland"); err == nil {
		path := filepath.Join(t.TempDir(), "hyprland.conf")
		for _, file := range []string{
			"gesture = 3, horizontal, workspace\n" + Render(config.Default(), Devices{WorkspaceSwipe: true}),
			Render(config.Default(), Devices{}),
		} {
			os.WriteFile(path, []byte(file), 0o644)
			if out, _ := exec.Command("Hyprland", "--verify-config", "-c", path).CombinedOutput(); !strings.Contains(string(out), "config ok") {
				t.Errorf("Hyprland rejects:\n%s", out)
			}
		}
	}
}

func TestHasWorkspaceSwipe(t *testing.T) {
	for _, c := range []struct {
		file string
		lua  bool
		want bool
	}{
		{"gesture = 3, horizontal, workspace\n", false, true},
		{"  gesture=3,horizontal,workspace # swipe\n", false, true},
		{"# gesture = 3, horizontal, workspace\n", false, false},
		{"gesture = 4, horizontal, workspace\n", false, false},
		{"gesture = 3, up, special, scratch\n", false, false},
		{`hl.gesture({ fingers = 3, direction = "horizontal", action = "workspace" })`, true, true},
		{`hl.gesture({ action = "workspace", direction = "horizontal", fingers = 3 })`, true, true},
		{`hl.gesture({ fingers = 3, direction = "horizontal", mods = "ALT", action = "workspace" })`, true, false},
		{`hl.gesture({ fingers = 3, direction = "up", action = "close" })`, true, false},
	} {
		if got := hasWorkspaceSwipe(c.file, c.lua); got != c.want {
			t.Errorf("%q: %v", c.file, got)
		}
	}
}

// Dictation's keys are bound only while it is on: held to talk, or
// pressed to start and stop.
func TestRenderDictation(t *testing.T) {
	c := config.Default()
	c.Shortcuts = []config.Shortcut{{Action: "voice", Keys: "SUPER+SHIFT+V"}}
	if got := Render(c, Devices{}); strings.Contains(got, "voice") || !strings.Contains(got, "unbind = SUPER SHIFT, V\n") {
		t.Errorf("off:\n%s", got)
	}
	c.Voice.Enable = true
	got := Render(c, Devices{})
	for _, want := range []string{"bind = SUPER SHIFT, V, exec, inua call voice start\n", "bindr = SUPER SHIFT, V, exec, inua call voice stop\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("push to talk: missing %q", want)
		}
	}
	// While it records: Escape drops it, the keys (released) end it.
	want := "submap = " + DictationSubmap + "\nbind = , Escape, exec, inua call voice cancel\nbindr = SUPER SHIFT, V, exec, inua call voice stop\nbind = SUPER CTRL ALT, Escape, submap, reset\nsubmap = reset\n"
	if !strings.Contains(got, want) {
		t.Errorf("submap: missing\n%s", want)
	}
	if lua := RenderLua(c, Devices{}); !strings.Contains(lua, `hl.define_submap("`+DictationSubmap+`"`) || !strings.Contains(lua, `hl.bind("Escape", hl.dsp.exec_cmd("inua call voice cancel"))`) {
		t.Errorf("lua submap:\n%s", lua)
	}
	c.Voice.PushToTalk = false
	got = Render(c, Devices{})
	if !strings.Contains(got, "bind = SUPER SHIFT, V, exec, inua call voice toggle\n") || !strings.Contains(got, "bind = SUPER SHIFT, V, exec, inua call voice stop\n") {
		t.Errorf("toggle:\n%s", got)
	}
	c.Voice.Enable = false
	if got := Render(c, Devices{}); strings.Contains(got, DictationSubmap) {
		t.Errorf("off: the submap is there")
	}
}

func TestRenderLockRestore(t *testing.T) {
	if got := Render(config.Default(), Devices{}); !strings.Contains(got, "allow_session_lock_restore = true") {
		t.Errorf("inua.conf does not let the lock screen take the lock back:\n%s", got)
	}
}

// The apps' menus (popups) are blurred as the shell's glass is, their
// shadows not.
func TestRenderPopupBlur(t *testing.T) {
	got := Render(config.Default(), Devices{})
	for _, want := range []string{"popups = true", "popups_ignorealpha = 0.4"} {
		if !strings.Contains(got, want) {
			t.Errorf("inua.conf lacks %q:\n%s", want, got)
		}
	}
	if got := RenderLua(config.Default(), Devices{}); !strings.Contains(got, "blur = { popups = true, popups_ignorealpha = 0.4 }") {
		t.Errorf("inua.lua does not blur popups:\n%s", got)
	}
	// The dock's and the bar's menus are their layer surfaces' popups.
	for _, out := range []string{got, RenderLua(config.Default(), Devices{})} {
		for _, want := range []string{"^inua-(bar|dock)$", "blur_popups"} {
			if !strings.Contains(out, want) {
				t.Errorf("no %q for the shell's menus:\n%s", want, out)
			}
		}
	}
	// The bar is arranged before the dock, so it spans the screen's top
	// edge whole: a side dock reserves its width first, and the bar laid
	// out after it would start at the dock's edge, not the screen's.
	for _, out := range []string{got, RenderLua(config.Default(), Devices{})} {
		if !strings.Contains(out, "order = 10") {
			t.Errorf("the bar is not arranged before the dock:\n%s", out)
		}
	}
}

// inua.lua for a Lua config sets the same as inua.conf, in Hyprland
// 0.55's calls.
func TestRenderLua(t *testing.T) {
	c := config.Default()
	c.Input = config.Input{KbOptions: ptr(`caps:escape,"odd"`), TouchpadSpeed: ptr(0.3), TouchpadAccel: "flat"}
	c.Shortcuts = []config.Shortcut{
		{Action: "launcher", Keys: "SUPER+D", Replaces: []string{"SUPER+Space"}},
		{Action: "shot-area", Keys: "", Replaces: []string{"Print"}},
		{Action: "exec", Keys: "SUPER+SHIFT+Return", Command: `kitty -e sh -c "echo \hi"`, Name: "Terminal"},
	}
	got := RenderLua(c, Devices{Touchpads: []string{"elan-touchpad"}})
	for _, want := range []string{
		"hl.config({ misc = { allow_session_lock_restore = true } })\n",
		`match = { class = "^(io\\.github\\.lyricat\\.Inua\\..+)$" },`,
		`hl.config({ input = { kb_options = "caps:escape,\"odd\"" } })`,
		"hl.device({\n    name = \"elan-touchpad\",\n    sensitivity = 0.30,\n    accel_profile = \"flat\",\n})\n",
		`pcall(hl.unbind, "SUPER + Space")`,
		`pcall(hl.unbind, "SUPER + SPACE")`,
		`pcall(hl.unbind, "SUPER + space")`,
		`pcall(hl.unbind, "Print")`,
		`hl.bind("SUPER + D", hl.dsp.exec_cmd("inua call launcher toggle"), { description = "Launcher" })`,
		`hl.bind("SUPER + SHIFT + Return", hl.dsp.exec_cmd("kitty -e sh -c \"echo \\hi\""), { description = "Terminal" })`,
		`hl.define_submap("` + RecordSubmap + `", function()`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
	if strings.Contains(got, "wl-copy") || strings.Contains(got, "Print\"), {") {
		t.Error("an action turned off was bound")
	}
	if strings.Index(got, `pcall(hl.unbind, "SUPER + D")`) > strings.Index(got, `hl.bind("SUPER + D"`) {
		t.Error("unbinds must come before binds")
	}
	if got := luaString("a\tb\nc\x7f"); got != `"a\009b\nc\127"` {
		t.Errorf("control characters: %s", got)
	}
	if got := RenderLua(config.Config{}, Devices{Touchpads: []string{"x-touchpad"}}); strings.Contains(got, "hl.device") || !strings.Contains(got, "Nothing set in inua") {
		t.Errorf("unset settings were written:\n%s", got)
	}
}

// Hyprland reads hyprland.lua when it runs Lua and the file is there, or
// when there is no hyprland.conf; inua writes the file that config loads.
func TestConfigFile(t *testing.T) {
	for _, c := range []struct {
		files   []string
		runsLua bool
		want    string
	}{
		{nil, false, "hyprland.conf"},
		{[]string{"hyprland.conf"}, true, "hyprland.conf"},
		{[]string{"hyprland.lua"}, false, "hyprland.lua"},
		{[]string{"hyprland.conf", "hyprland.lua"}, false, "hyprland.conf"}, // 0.53, or not known
		{[]string{"hyprland.conf", "hyprland.lua"}, true, "hyprland.lua"},
	} {
		dir := t.TempDir()
		for _, f := range c.files {
			os.WriteFile(filepath.Join(dir, f), nil, 0o644)
		}
		got := ConfigFile(dir, c.runsLua)
		if got != filepath.Join(dir, c.want) {
			t.Errorf("%v, Lua %v: %s", c.files, c.runsLua, got)
		}
		wantTarget := map[string]string{"hyprland.conf": Name, "hyprland.lua": LuaName}[c.want]
		if Target(got) != filepath.Join(dir, wantTarget) {
			t.Errorf("%s: writes %s", got, Target(got))
		}
	}
}

// For a Lua config only inua.lua is written, and it keeps an unplugged
// touchpad's settings as inua.conf does.
func TestApplyLua(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hypr")
	path := filepath.Join(dir, LuaName)
	ctx := context.Background()
	old := RenderLua(config.Config{Input: config.Input{TouchpadSpeed: ptr(0.1)}}, Devices{Touchpads: []string{"bt-trackpad"}})
	os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed, err := Apply(ctx, config.Config{Input: config.Input{TouchpadSpeed: ptr(0.4)}}, path, nil); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "name = \"bt-trackpad\",\n    sensitivity = 0.40") || strings.Contains(string(got), `name = "inua-light-apps",`+"\n    sensitivity") {
		t.Errorf("got\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, Name)); !os.IsNotExist(err) {
		t.Errorf("inua.conf written for a Lua config: %v", err)
	}
}
