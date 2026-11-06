package niri

import (
	"slices"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/compositor"
)

func TestRender(t *testing.T) {
	opts, speed := "caps:escape", 0.3
	text, skipped := Render(compositor.Settings{
		KbOptions: &opts, TouchpadSpeed: &speed, TouchpadAccel: "flat",
		Binds: []compositor.KeyBind{
			{Keys: "SUPER+SHIFT+S", Command: `inua call screenshot area`},
			{Keys: "SUPER+mouse:272", Command: "x"}, // a button: not a niri bind
			{Keys: "", Command: "off"},
			{Keys: "SUPER+comma", Command: `sh -c 'echo "hi"'`},
		},
	}, []string{`layout "us,de"`})
	if !slices.Equal(skipped, []string{"SUPER+mouse:272"}) {
		t.Fatalf("skipped %q", skipped)
	}
	for _, want := range []string{
		"layout \"us,de\"\n            options \"caps:escape\"", // niri replaces xkb as a whole: the layout rides along
		"accel-speed 0.30", `accel-profile "flat"`,
		`Super+Shift+S { spawn "sh" "-c" "inua call screenshot area"; }`,
		`Super+comma { spawn "sh" "-c" "sh -c 'echo \"hi\"'"; }`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	// Nothing set: no sections at all, so nothing of niri's is replaced.
	if text, _ := Render(compositor.Settings{}, nil); strings.Contains(text, "input") || strings.Contains(text, "binds") {
		t.Errorf("empty settings wrote sections:\n%s", text)
	}
}

func TestRenderAnimationsOff(t *testing.T) {
	off := false
	text, _ := Render(compositor.Settings{Animations: &off}, nil)
	if !strings.Contains(text, "animations {\n    off\n}") {
		t.Fatalf("animations off not rendered:\n%s", text)
	}
	if text, _ := Render(compositor.Settings{}, nil); strings.Contains(text, "animations {") {
		t.Fatalf("unset animation setting changed niri defaults:\n%s", text)
	}
}

func TestXKBAndInclude(t *testing.T) {
	config := `input {
    keyboard {
        xkb {
            layout "us,de"   // two
            variant ",nodeadkeys"
            options "ctrl:nocaps"
        }
        repeat-delay 300
    }
}
include optional=true "inua.kdl"
`
	if got := XKB(config); !slices.Equal(got, []string{`layout "us,de"   // two`, `variant ",nodeadkeys"`}) {
		t.Fatalf("xkb %q", got)
	}
	if XKB(`input { keyboard { xkb { layout "fr"; options "x"; }; }; }`)[0] != `layout "fr"` {
		t.Fatal("one-line xkb")
	}
	if XKB("binds {}") != nil {
		t.Fatal("xkb from nothing")
	}
	for text, want := range map[string]bool{
		config: true, `include "inua.kdl"`: true, `  include "sub/inua.kdl"`: true,
		`// include "inua.kdl"`: false, `include "other.kdl"`: false, "": false,
	} {
		if Includes(text) != want {
			t.Errorf("Includes(%q) = %v", text, !want)
		}
	}
}

func TestParseBinds(t *testing.T) {
	config := `binds {
    // Mod+X { spawn "no"; }
    Mod+Space { spawn "inua" "call" "launcher" "toggle"; }
    Mod+Shift+E { quit; }
    XF86AudioMute allow-when-locked=true { spawn "wpctl" "set-mute" "@DEFAULT_AUDIO_SINK@" "toggle"; }
    Mod+WheelScrollDown cooldown-ms=150 { focus-workspace-down; }
    Mod5+A { spawn "x"; }
}
binds { Super+E { spawn "sh" "-c" "touch /tmp/x"; }; Mod+Q { close-window; }; }
`
	got := ParseBinds(config)
	want := []compositor.Bind{
		{Keys: "SUPER+Space", Command: "inua call launcher toggle"},
		{Keys: "SUPER+SHIFT+E", Action: "quit"},
		{Keys: "XF86AudioMute", Command: "wpctl set-mute @DEFAULT_AUDIO_SINK@ toggle"},
		{Keys: "SUPER+WheelScrollDown", Action: "focus-workspace-down"},
		{Keys: "SUPER+E", Command: "touch /tmp/x"},
		{Keys: "SUPER+Q", Action: "close-window"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("binds\n%+v\nwant\n%+v", got, want)
	}
	if keys, ok := Keys("SUPER+SHIFT+S"); !ok || keys != "Super+Shift+S" {
		t.Fatalf("Keys: %q %v", keys, ok)
	}
}
