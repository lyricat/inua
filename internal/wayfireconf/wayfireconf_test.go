package wayfireconf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/config"
	ini "gopkg.in/ini.v1"
)

func TestRenderWritesWayfireSettingsAndPreservesUserConfig(t *testing.T) {
	cfg := config.Default()
	kb := "caps:escape,ctrl:swap_lalt_lctl"
	speed := 0.25
	cfg.Input.KbOptions = &kb
	cfg.Input.TouchpadSpeed = &speed
	cfg.Input.TouchpadAccel = "flat"
	cfg.Displays.Monitors = []config.Monitor{{Output: "DP-1", Mode: "2560x1440@144", Position: "2560x0", Scale: 1.25}}
	cfg.Shortcuts = []config.Shortcut{{Action: "overview", Keys: "SUPER+SHIFT+O"}, {Action: "exec", Keys: "ALT+F2", Command: "inua call launcher toggle"}}
	before := "[core]\nplugins = ipc command\n\n[input]\nxkb_layout = us\n\n[output:DP-1]\nmode = 1920x1080@60000\nposition = 0,0\n\n[command]\nbindings_mine = <super> M : echo mine\n"
	got, err := Render(before, cfg)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ini.Load([]byte(got))
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct{ section, key, want string }{
		{"core", "plugins", "ipc command"}, {"input", "xkb_layout", "us"},
		{"input", "xkb_options", kb}, {"input", "touchpad_accel", "0.250000"},
		{"input", "touchpad_accel_profile", "flat"}, {"output:DP-1", "mode", "2560x1440@144000"},
		{"output:DP-1", "position", "2560,0"}, {"output:DP-1", "scale", "1.250000"},
		{"command", "binding_inua_overview", "<super> <shift> KEY_O"},
		{"command", "command_inua_overview", "inua call overview toggle"},
		{"command", "binding_inua_exec_1", "<alt> KEY_F2"},
		{"command", "command_inua_exec_1", "inua call launcher toggle"},
	}
	for _, check := range checks {
		if got := parsed.Section(check.section).Key(check.key).String(); got != check.want {
			t.Errorf("[%s] %s = %q, want %q", check.section, check.key, got, check.want)
		}
	}
}

func TestRenderRestoresUserConfigWhenOverrideIsRemoved(t *testing.T) {
	cfg := config.Default()
	kb := "ctrl:nocaps"
	cfg.Input.KbOptions = &kb
	cfg.Displays.Monitors = []config.Monitor{{Output: "DP-1", Mode: "2560x1440@144", Position: "2560x0", Scale: 1.25}}
	cfg.Shortcuts = []config.Shortcut{{Action: "overview", Keys: "SUPER+O"}}
	before := "[input]\nxkb_options = caps:escape\n\n[output:DP-1]\nmode = 1920x1080@60000\nposition = 0,0\nscale = 1.000000\n\n[command]\nbinding_inua_overview = <super>\ncommand_inua_overview = inua call overview toggle\nbindings_user = <super> U : keep\n"
	overridden, err := Render(before, cfg)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Render(overridden, config.Default())
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ini.Load([]byte(restored))
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct{ section, key, want string }{
		{"input", "xkb_options", "caps:escape"},
		{"output:DP-1", "mode", "1920x1080@60000"},
		{"output:DP-1", "position", "0,0"},
		{"output:DP-1", "scale", "1.000000"},
		{"command", "binding_inua_overview", "<super>"},
		{"command", "command_inua_overview", "inua call overview toggle"},
	} {
		if got := parsed.Section(check.section).Key(check.key).String(); got != check.want {
			t.Errorf("[%s] %s = %q, want %q", check.section, check.key, got, check.want)
		}
	}
	if !strings.Contains(restored, "bindings_user") {
		t.Fatalf("unmanaged binding was removed:\n%s", restored)
	}
}

func TestRenderWritesWayfirePushToTalkBindings(t *testing.T) {
	cfg := config.Default()
	cfg.Voice.Enable = true
	cfg.Shortcuts = []config.Shortcut{{Action: "voice", Keys: "SUPER+SHIFT+V"}}
	got, err := Render("[command]\n", cfg)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ini.Load([]byte(got))
	if err != nil {
		t.Fatal(err)
	}
	command := parsed.Section("command")
	for key, want := range map[string]string{
		"binding_inua_voice": "<super> <shift> KEY_V",
		"command_inua_voice": "inua call voice start",
		"release_binding_inua_voice_release": "<super> <shift> KEY_V",
		"release_command_inua_voice_release": "inua call voice stop",
	} {
		if got := command.Key(key).String(); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestReadInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wayfire.ini")
	if err := os.WriteFile(path, []byte("[input]\nxkb_options = caps:escape\ntouchpad_accel = -0.25\ntouchpad_accel_profile = flat\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadInput(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.KbOptions == nil || *got.KbOptions != "caps:escape" || got.TouchpadSpeed == nil || *got.TouchpadSpeed != -0.25 || got.TouchpadAccel != "flat" {
		t.Fatalf("ReadInput = %+v", got)
	}
}

func TestApplyWritesOnlyOnChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wayfire.ini")
	if err := os.WriteFile(path, []byte("[core]\nplugins = ipc\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	changed, err := Apply(path, cfg)
	if err != nil || !changed {
		t.Fatalf("Apply = %v, %v; want changed", changed, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("file mode after apply = %v; want 0640", info.Mode().Perm())
	}
	if changed, err = Apply(path, cfg); err != nil || changed {
		t.Fatalf("second Apply = %v, %v; want unchanged", changed, err)
	}
}
