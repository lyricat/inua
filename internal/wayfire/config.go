package wayfire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/lyricat/inua/internal/compositor"
)

// Wayfire takes inua's settings over its IPC while it runs: the input
// and output options by wayfire/set-config-options (the displays are in
// displays.go), the shortcuts as bindings of the
// command plugin (command/register-binding). Nothing is written to
// wayfire.ini; the shell applies them again each time it starts. The ids
// of the bindings it registered are kept in the runtime directory, so the
// next shell of the same session takes them off before adding its own.

// evdev names a key as Wayfire's bindings do ("KEY_SPACE"), by its XKB
// name in lower case. Letters, digits and function keys are made by rule.
var evdev = map[string]string{
	"space": "KEY_SPACE", "return": "KEY_ENTER", "kp_enter": "KEY_KPENTER", "tab": "KEY_TAB", "escape": "KEY_ESC",
	"backspace": "KEY_BACKSPACE", "delete": "KEY_DELETE", "insert": "KEY_INSERT", "home": "KEY_HOME", "end": "KEY_END",
	"prior": "KEY_PAGEUP", "next": "KEY_PAGEDOWN", "page_up": "KEY_PAGEUP", "page_down": "KEY_PAGEDOWN",
	"left": "KEY_LEFT", "right": "KEY_RIGHT", "up": "KEY_UP", "down": "KEY_DOWN",
	"comma": "KEY_COMMA", "period": "KEY_DOT", "slash": "KEY_SLASH", "semicolon": "KEY_SEMICOLON",
	"apostrophe": "KEY_APOSTROPHE", "grave": "KEY_GRAVE", "minus": "KEY_MINUS", "equal": "KEY_EQUAL",
	"bracketleft": "KEY_LEFTBRACE", "bracketright": "KEY_RIGHTBRACE", "backslash": "KEY_BACKSLASH",
	"print": "KEY_SYSRQ", "pause": "KEY_PAUSE", "menu": "KEY_COMPOSE", "caps_lock": "KEY_CAPSLOCK",
	"xf86audioraisevolume": "KEY_VOLUMEUP", "xf86audiolowervolume": "KEY_VOLUMEDOWN", "xf86audiomute": "KEY_MUTE",
	"xf86audiomicmute": "KEY_MICMUTE", "xf86audioplay": "KEY_PLAYPAUSE", "xf86audiopause": "KEY_PAUSECD",
	"xf86audionext": "KEY_NEXTSONG", "xf86audioprev": "KEY_PREVIOUSSONG", "xf86audiostop": "KEY_STOPCD",
	"xf86monbrightnessup": "KEY_BRIGHTNESSUP", "xf86monbrightnessdown": "KEY_BRIGHTNESSDOWN",
	"xf86search": "KEY_SEARCH", "xf86calculator": "KEY_CALC", "xf86mail": "KEY_MAIL", "xf86explorer": "KEY_FILE",
}

// keysyms is evdev the other way, for reading Wayfire's own bindings.
var keysyms = func() map[string]string {
	prefer := map[string]string{"KEY_PAGEUP": "Prior", "KEY_PAGEDOWN": "Next", "KEY_ENTER": "Return", "KEY_ESC": "Escape",
		"KEY_SYSRQ": "Print", "KEY_DOT": "period", "KEY_LEFTBRACE": "bracketleft", "KEY_RIGHTBRACE": "bracketright"}
	out := map[string]string{}
	names := make([]string, 0, len(evdev))
	for k := range evdev {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, sym := range names {
		if _, ok := out[evdev[sym]]; !ok {
			out[evdev[sym]] = sym
		}
	}
	for k, v := range prefer {
		out[k] = v
	}
	return out
}()

// keyName is a key's evdev name; false for one with none known.
func keyName(key string) (string, bool) {
	k := strings.ToLower(key)
	switch {
	case len(k) == 1 && (k[0] >= 'a' && k[0] <= 'z' || k[0] >= '0' && k[0] <= '9'):
		return "KEY_" + strings.ToUpper(k), true
	case len(k) >= 2 && k[0] == 'f' && strings.Trim(k[1:], "0123456789") == "":
		return "KEY_" + strings.ToUpper(k), true
	}
	name, ok := evdev[k]
	return name, ok
}

// Keys writes a shortcut as Wayfire's bindings name it
// ("<super> <shift> KEY_S"); false for a key it has no name for.
func Keys(keys string) (string, bool) {
	c, ok := compositor.ParseKeys(keys)
	if !ok {
		return "", false
	}
	name, ok := keyName(c.Key)
	if !ok {
		return "", false
	}
	var parts []string
	for _, m := range []struct {
		on   bool
		name string
	}{{c.Super, "<super>"}, {c.Ctrl, "<ctrl>"}, {c.Alt, "<alt>"}, {c.Shift, "<shift>"}} {
		if m.on {
			parts = append(parts, m.name)
		}
	}
	return strings.Join(append(parts, name), " "), true
}

// neutralKeys reads a Wayfire binding ("<super> <shift> KEY_S") as inua
// writes keys; false for a button, a gesture or a key with no XKB name
// known here.
func neutralKeys(binding string) (string, bool) {
	var c compositor.Combo
	for _, f := range strings.Fields(binding) {
		switch strings.ToLower(f) {
		case "<super>":
			c.Super = true
		case "<ctrl>":
			c.Ctrl = true
		case "<alt>":
			c.Alt = true
		case "<shift>":
			c.Shift = true
		default:
			if c.Key != "" || !strings.HasPrefix(f, "KEY_") {
				return "", false
			}
			name := strings.TrimPrefix(f, "KEY_")
			switch sym, ok := keysyms[f]; {
			case ok:
				c.Key = sym
			case len(name) == 1, len(name) >= 2 && name[0] == 'F' && strings.Trim(name[1:], "0123456789") == "":
				c.Key = name
			default:
				return "", false
			}
		}
	}
	if c.Key == "" {
		return "", false
	}
	return c.String(), true
}

// options reads every option Wayfire has, by section.
func (b *Backend) options(ctx context.Context) (map[string]map[string]string, error) {
	body, err := b.Client.Call(ctx, "wayfire/list-config-options", nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		Options map[string]map[string]struct {
			Value json.RawMessage `json:"value"`
		} `json:"options"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("wayfire: list-config-options: %w", err)
	}
	out := map[string]map[string]string{}
	for sec, opts := range r.Options {
		out[sec] = map[string]string{}
		for name, o := range opts {
			// A compound option (the command plugin's "bindings") is a
			// list; its parts are also there as options of their own.
			var v string
			if json.Unmarshal(o.Value, &v) == nil {
				out[sec][name] = v
			}
		}
	}
	return out, nil
}

// commandBinds are the command plugin's bindings from wayfire.ini: each
// option binding_NAME (or repeatable_binding_NAME, always_binding_NAME)
// with its command_NAME.
func commandBinds(command map[string]string) (binds []compositor.Bind) {
	names := make([]string, 0, len(command))
	for name := range command {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, opt := range names {
		i := strings.Index(opt, "binding_")
		if i < 0 {
			continue
		}
		cmd := command["command_"+opt[i+len("binding_"):]]
		keys, ok := neutralKeys(command[opt])
		if !ok || cmd == "" {
			continue
		}
		binds = append(binds, compositor.Bind{Keys: keys, Command: cmd})
	}
	return binds
}

// Binds are the shortcuts in Wayfire's own config that run commands.
func (b *Backend) Binds(ctx context.Context) ([]compositor.Bind, error) {
	if !b.has("wayfire/list-config-options") {
		return nil, nil // Wayfire 0.9 cannot list its options
	}
	opts, err := b.options(ctx)
	if err != nil {
		return nil, err
	}
	return commandBinds(opts["command"]), nil
}

// bindsFile keeps the ids of the bindings this session's shell registered.
func bindsFile() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "inua", "wayfire-binds.json")
}

// ApplySettings sets the input options and replaces the shortcuts inua
// registered before with those of s. A key wayfire.ini binds itself stays
// bound there: the command plugin reads its options only when it loads,
// so a shortcut moved in inua keeps its old key too until the ini drops it.
// The animation options go apart from the input ones, so that a failure
// with them leaves the rest applied; it is returned once the rest is done.
func (b *Backend) ApplySettings(ctx context.Context, s compositor.Settings) error {
	input := map[string]any{}
	if s.KbOptions != nil {
		input["input/xkb_options"] = *s.KbOptions
	}
	if s.TouchpadSpeed != nil {
		input["input/touchpad_cursor_speed"] = *s.TouchpadSpeed
	}
	if s.TouchpadAccel != "" {
		input["input/touchpad_accel_profile"] = s.TouchpadAccel
	}
	if len(input) > 0 {
		if err := b.call(ctx, "wayfire/set-config-options", input); err != nil {
			return err
		}
	}
	motion := b.applyAnimations(ctx, s.Animations)
	if err := b.applyDisplays(ctx, s.Displays); err != nil {
		return errors.Join(err, motion)
	}
	if err := b.applyBinds(ctx, s.Binds); err != nil {
		return errors.Join(err, motion)
	}
	return motion
}

// motionOptions are the options of Wayfire's own plugins that move
// windows and workspaces (metadata/animate.xml, grid.xml and vswitch.xml
// as Wayfire 0.9 and 0.10 ship them), each with the value that stills it.
// The animation plugins have no on/off option of their own: animate picks
// an effect per window, from open/close/minimize_animation or, over them,
// from the fade/zoom/fire_enabled_for matchers ("none" matches no
// window); grid's type is how a window snaps; vswitch's duration is the
// workspace slide ("0" is the shortest, a single frame). Wayfire has every
// option its metadata names whether the plugin is loaded or not; one it
// does not have (minimize_animation before 0.10) is left out.
var motionOptions = []struct{ name, off string }{
	{"animate/open_animation", "none"},
	{"animate/close_animation", "none"},
	{"animate/minimize_animation", "none"},
	{"animate/fade_enabled_for", "none"},
	{"animate/zoom_enabled_for", "none"},
	{"animate/fire_enabled_for", "none"},
	{"grid/type", "none"},
	{"vswitch/duration", "0"},
}

// still reports whether an option's value is the one that stills it. An
// animation's length is read as Wayfire writes it ("0ms circle", "300").
func still(off, value string) bool {
	if off != "0" {
		return strings.TrimSpace(value) == off
	}
	v := strings.TrimSpace(value)
	n := strings.IndexFunc(v, func(r rune) bool { return !strings.ContainsRune("+-.0123456789", r) })
	if n < 0 {
		n = len(v)
	}
	length, err := strconv.ParseFloat(v[:n], 64)
	return err == nil && length == 0
}

// motionFile keeps what the motion options were before inua stilled them.
func motionFile() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "inua", "wayfire-motion.json")
}

// applyAnimations stills Wayfire's motion options (false) or puts back
// what they were before inua stilled them (true); nil leaves them alone.
// On is no more than that: an animation wayfire.ini itself turns off stays
// off, and one Wayfire has again from wayfire.ini (it started again) is
// left as it is. Options already as asked are not set again.
func (b *Backend) applyAnimations(ctx context.Context, on *bool) error {
	if on == nil {
		return nil
	}
	saved := map[string]string{}
	if data, err := os.ReadFile(motionFile()); err == nil {
		json.Unmarshal(data, &saved)
	}
	if *on && len(saved) == 0 {
		return nil // nothing inua stilled
	}
	if !b.has("wayfire/set-config-options") || !b.has("wayfire/get-config-option") {
		if *on {
			return nil
		}
		return fmt.Errorf("%w: turning animations off needs Wayfire's ipc-rules plugin", compositor.ErrUnavailable)
	}
	save := func() error {
		if err := os.MkdirAll(filepath.Dir(motionFile()), 0o700); err != nil {
			return err
		}
		data, _ := json.Marshal(saved)
		return os.WriteFile(motionFile(), data, 0o600)
	}

	set := map[string]any{}
	for _, o := range motionOptions {
		now, err := b.option(ctx, o.name)
		if errors.Is(err, errNoOption) {
			continue
		}
		if err != nil {
			return err
		}
		if *on {
			was, ok := saved[o.name]
			if !ok {
				continue
			}
			if still(o.off, now) && !still(o.off, was) {
				set[o.name] = was
			}
		} else if !still(o.off, now) {
			// Kept before it is set: a value set by halves is put back
			// only where it is still stilled.
			saved[o.name] = now
			set[o.name] = o.off
		}
	}
	if !*on && len(set) > 0 {
		if err := save(); err != nil {
			return fmt.Errorf("wayfire: keeping the animation options: %w", err)
		}
	}
	if len(set) > 0 {
		if err := b.call(ctx, "wayfire/set-config-options", set); err != nil {
			return err
		}
	}
	if *on {
		// Put back, or Wayfire's own again: none is kept any longer.
		if err := os.Remove(motionFile()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("wayfire: the animation options: %w", err)
		}
	}
	return nil
}

// applyBinds registers binds with the command plugin, in place of those
// the last shell registered.
func (b *Backend) applyBinds(ctx context.Context, binds []compositor.KeyBind) error {
	if !b.has("command/register-binding") {
		if len(binds) > 0 {
			return fmt.Errorf("%w: shortcuts need Wayfire's command plugin", compositor.ErrUnavailable)
		}
		return nil
	}

	// Off with what the last shell registered; an id Wayfire no longer
	// knows (it started again) is not an error.
	var old []uint64
	if data, err := os.ReadFile(bindsFile()); err == nil {
		json.Unmarshal(data, &old)
	}
	for _, id := range old {
		b.call(ctx, "command/unregister-binding", map[string]any{"binding-id": id})
	}
	var ids []uint64
	save := func() {
		os.MkdirAll(filepath.Dir(bindsFile()), 0o700)
		data, _ := json.Marshal(ids)
		os.WriteFile(bindsFile(), data, 0o600)
	}
	save()

	var skipped []string
	for _, kb := range binds {
		if kb.Keys == "" || kb.Command == "" {
			continue
		}
		binding, ok := Keys(kb.Keys)
		if !ok {
			skipped = append(skipped, kb.Keys)
			continue
		}
		body, err := b.Client.Call(ctx, "command/register-binding", map[string]any{"binding": binding, "exec-always": false, "command": kb.Command})
		if err != nil {
			save()
			return err
		}
		var r struct {
			ID uint64 `json:"binding-id"`
		}
		if json.Unmarshal(body, &r) == nil && r.ID != 0 {
			ids = append(ids, r.ID)
		}
	}
	save()
	if len(skipped) > 0 {
		return fmt.Errorf("Wayfire cannot bind %s", strings.Join(skipped, ", "))
	}
	return nil
}

// Source names where the settings go.
func (b *Backend) Source() string { return "Wayfire over its IPC, each time the shell starts" }

// Hint: a missing plugin is what keeps a setting from applying.
func (b *Backend) Hint(c compositor.Capability) string {
	switch c {
	case compositor.CapBinds:
		if !b.has("command/register-binding") {
			return "Add command to the plugins in Wayfire's wayfire.ini."
		}
	case compositor.CapKeyboard, compositor.CapTouchpad, compositor.CapDisplays:
		if !b.has("wayfire/set-config-options") {
			return "Add ipc and ipc-rules to the plugins in Wayfire's wayfire.ini."
		}
	}
	return ""
}
