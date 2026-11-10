package wayfire

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/compositor"
)

func TestKeys(t *testing.T) {
	for in, want := range map[string]string{
		"SUPER+SHIFT+S": "<super> <shift> KEY_S", "SUPER+Space": "<super> KEY_SPACE", "SUPER+comma": "<super> KEY_COMMA",
		"Print": "KEY_SYSRQ", "CTRL+ALT+F2": "<ctrl> <alt> KEY_F2", "SUPER+1": "<super> KEY_1", "XF86AudioMute": "KEY_MUTE",
	} {
		got, ok := Keys(in)
		if !ok || got != want {
			t.Errorf("Keys(%q) = %q %v, want %q", in, got, ok, want)
		}
		// And back, as the Shortcuts page reads Wayfire's own bindings.
		back, ok := neutralKeys(got)
		if !ok || !strings.EqualFold(back, in) {
			t.Errorf("neutralKeys(%q) = %q %v, want %q", got, back, ok, in)
		}
	}
	for _, bad := range []string{"SUPER+mouse:272", "SUPER+dead_acute", ""} {
		if got, ok := Keys(bad); ok {
			t.Errorf("Keys(%q) = %q", bad, got)
		}
	}
	for _, bad := range []string{"<super> BTN_LEFT", "swipe left 3", "<super>"} {
		if got, ok := neutralKeys(bad); ok {
			t.Errorf("neutralKeys(%q) = %q", bad, got)
		}
	}
}

func TestCommandBinds(t *testing.T) {
	got := commandBinds(map[string]string{
		"binding_launcher": "<super> KEY_SPACE", "command_launcher": "inua call launcher toggle",
		"repeatable_binding_up": "KEY_VOLUMEUP", "command_up": "wpctl set-volume @DEFAULT_AUDIO_SINK@ 5%+",
		"binding_mouse": "<super> BTN_LEFT", "command_mouse": "x",
		"binding_orphan": "<super> KEY_O",
	})
	want := []compositor.Bind{
		{Keys: "SUPER+space", Command: "inua call launcher toggle"},
		{Keys: "xf86audioraisevolume", Command: "wpctl set-volume @DEFAULT_AUDIO_SINK@ 5%+"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("binds %+v\nwant %+v", got, want)
	}
}

// The settings reach Wayfire as options and registered bindings; a second
// apply takes the first one's bindings off before adding its own.
func TestApplySettings(t *testing.T) {
	f := newFakeWayfire(t)
	f.mu.Lock()
	f.methods = append(slices.Clone(allMethods), "wayfire/set-config-options", "command/register-binding", "command/unregister-binding")
	f.mu.Unlock()
	f.set("command/register-binding", `{"result":"ok","binding-id":7}`)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	b := New(NewClient(f.socket))
	opts, speed := "caps:escape", 0.3
	settings := compositor.Settings{KbOptions: &opts, TouchpadSpeed: &speed, TouchpadAccel: "flat", Binds: []compositor.KeyBind{
		{Keys: "SUPER+comma", Command: "inua call settings toggle"},
		{Keys: "", Command: "off"},
	}}
	if err := b.ApplySettings(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	if got := f.did(); len(got) != 1 || !strings.HasPrefix(got[0], "wayfire/set-config-options ") ||
		!strings.Contains(got[0], `"input/xkb_options":"caps:escape"`) || !strings.Contains(got[0], `"input/touchpad_accel_profile":"flat"`) {
		t.Fatalf("calls %q", got)
	}
	ids, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "inua", "wayfire-binds.json"))
	if string(ids) != "[7]" {
		t.Fatalf("registered ids %s", ids)
	}
	if err := b.ApplySettings(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	if got := f.did(); !slices.Contains(got, `command/unregister-binding {"binding-id":7}`) {
		t.Fatalf("the first apply's binding was left: %q", got)
	}
	// A key Wayfire has no name for is said, the rest applied.
	settings.Binds = append(settings.Binds, compositor.KeyBind{Keys: "SUPER+dead_acute", Command: "x"})
	if err := b.ApplySettings(context.Background(), settings); err == nil || !strings.Contains(err.Error(), "SUPER+dead_acute") {
		t.Fatalf("an unbindable key: %v", err)
	}
	// Without the command plugin shortcuts cannot be set, and the hint says what to add.
	f.mu.Lock()
	f.methods = append(slices.Clone(allMethods), "wayfire/set-config-options")
	f.mu.Unlock()
	b2 := New(NewClient(f.socket))
	if err := b2.ApplySettings(context.Background(), settings); err == nil {
		t.Fatal("shortcuts applied without the command plugin")
	}
	if b2.Capabilities()[compositor.CapBinds] != compositor.Unavailable || !strings.Contains(b2.Hint(compositor.CapBinds), "command") {
		t.Fatalf("caps %v hint %q", b2.Capabilities(), b2.Hint(compositor.CapBinds))
	}
	if b2.Capabilities()[compositor.CapKeyboard] != compositor.Supported {
		t.Fatal("keyboard settings need only the IPC")
	}
}
