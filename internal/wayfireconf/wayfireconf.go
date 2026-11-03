// Package wayfireconf applies Inua settings to Wayfire's INI configuration.
package wayfireconf

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/hyprconf"
	ini "gopkg.in/ini.v1"
)

const header = "; Wayfire settings managed by Inua; edit config.toml instead.\n"

// Path is Wayfire's user configuration next to Inua's config.toml.
func Path(configPath string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(configPath)), "wayfire.ini")
}

// Render applies the settings Inua owns to an existing Wayfire config while
// preserving every unrelated key and section.
func Render(content string, c config.Config) (string, error) {
	cfg, err := ini.LoadSources(ini.LoadOptions{AllowShadows: true, PreserveSurroundedQuote: true}, []byte(content))
	if err != nil {
		return "", fmt.Errorf("parse Wayfire config: %w", err)
	}
	input := cfg.Section("input")
	clearManaged(input)
	setManaged(input, "xkb_options", stringValue(c.Input.KbOptions))
	setManaged(input, "touchpad_accel", floatValue(c.Input.TouchpadSpeed))
	setManaged(input, "touchpad_accel_profile", c.Input.TouchpadAccel)

	for _, section := range cfg.Sections() {
		if strings.HasPrefix(section.Name(), "output:") {
			clearManaged(section)
		}
	}
	for _, m := range c.Displays.Monitors {
		if strings.HasPrefix(m.Output, "desc:") || m.Disabled {
			continue
		}
		section := cfg.Section("output:" + m.Output)
		setManaged(section, "mode", wayfireMode(m.Mode))
		position := m.Position
		if strings.Contains(position, "auto") {
			position = ""
		}
		if position != "" {
			x, y, ok := strings.Cut(position, "x")
			if !ok {
				return "", fmt.Errorf("invalid Wayfire output position %q", position)
			}
			position = x + "," + y
		}
		setManaged(section, "position", position)
		if m.Scale != 0 {
			setManaged(section, "scale", strconv.FormatFloat(m.Scale, 'f', 6, 64))
		} else {
			setManaged(section, "scale", "")
		}
	}

	command := cfg.Section("command")
	clearManaged(command)
	for i, sc := range c.Shortcuts {
		name := "inua_" + sc.Action
		if sc.Action == "exec" {
			name = "inua_exec_" + strconv.Itoa(i)
		}
		commandLine := hyprconf.ShortcutCommand(c, sc)
		bindingKey, commandKey := "binding_"+name, "command_"+name
		releaseBinding, releaseCommand := "release_binding_"+name+"_release", "release_command_"+name+"_release"
		if sc.Action == "voice" && c.Voice.Enable {
			commandLine = "inua call voice start"
			setManaged(command, releaseBinding, wayfireBinding(sc.Keys))
			setManaged(command, releaseCommand, "inua call voice stop")
		} else {
			setManaged(command, releaseBinding, "")
			setManaged(command, releaseCommand, "")
		}
		if commandLine == "" || sc.Keys == "" {
			setManaged(command, bindingKey, "")
			setManaged(command, commandKey, "")
			setManaged(command, releaseBinding, "")
			setManaged(command, releaseCommand, "")
			continue
		}
		setManaged(command, bindingKey, wayfireBinding(sc.Keys))
		setManaged(command, commandKey, commandLine)
	}

	if content == "" && len(cfg.Sections()) > 1 {
		cfg.Section(ini.DefaultSection).Comment = header
	}
	var out strings.Builder
	if _, err := cfg.WriteTo(&out); err != nil {
		return "", fmt.Errorf("render Wayfire config: %w", err)
	}
	return out.String(), nil
}

// Apply atomically updates the settings in Wayfire's config file.
func Apply(path string, c config.Config) (bool, error) {
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(statErr) {
		return false, statErr
	}
	content, err := Render(string(old), c)
	if err != nil {
		return false, err
	}
	if string(old) == content {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".wayfire-*.ini")
	if err != nil {
		return false, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return false, err
	}
	if _, err = f.WriteString(content); err == nil { err = f.Sync() }
	if closeErr := f.Close(); err == nil { err = closeErr }
	if err != nil { return false, err }
	if err := os.Rename(tmp, path); err != nil { return false, err }
	return true, nil
}

// ReadInput reads Wayfire's effective configured input values for the settings page.
func ReadInput(path string) (config.Input, error) {
	file, err := ini.Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config.Input{}, nil
		}
		return config.Input{}, fmt.Errorf("read Wayfire input settings: %w", err)
	}
	section := file.Section("input")
	var input config.Input
	if section.HasKey("xkb_options") {
		value := section.Key("xkb_options").String()
		input.KbOptions = &value
	}
	if section.HasKey("touchpad_accel") {
		value, err := section.Key("touchpad_accel").Float64()
		if err != nil {
			return config.Input{}, fmt.Errorf("read Wayfire touchpad acceleration: %w", err)
		}
		input.TouchpadSpeed = &value
	}
	if section.HasKey("touchpad_accel_profile") {
		input.TouchpadAccel = section.Key("touchpad_accel_profile").String()
	}
	return input, nil
}

const managedPrefix = "inua-managed:"

func clearManaged(section *ini.Section) {
	for _, key := range section.Keys() {
		if _, ok := managedValue(key.Comment); ok {
			restoreManagedKey(section, key)
		}
	}
}

func managedValue(comment string) (string, bool) {
	comment = strings.TrimLeft(strings.TrimSpace(comment), "#; ")
	value, ok := strings.CutPrefix(comment, managedPrefix)
	return value, ok
}

func restoreManagedKey(section *ini.Section, key *ini.Key) {
	encoded, ok := managedValue(key.Comment)
	if !ok {
		return
	}
	if encoded == "none" {
		section.DeleteKey(key.Name())
		return
	}
	previous, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		section.DeleteKey(key.Name())
		return
	}
	value, comment, ok := strings.Cut(string(previous), "\x00")
	if !ok {
		section.DeleteKey(key.Name())
		return
	}
	key.SetValue(value)
	key.Comment = comment
}

func setManaged(section *ini.Section, name, value string) {
	key, err := section.GetKey(name)
	if value == "" {
		if err == nil {
			if _, ok := managedValue(key.Comment); ok {
				restoreManagedKey(section, key)
			}
		}
		return
	}
	if err != nil {
		key = section.Key(name)
		key.Comment = managedPrefix + "none"
	} else if _, ok := managedValue(key.Comment); !ok {
		previous := key.Value() + "\x00" + key.Comment
		key.Comment = managedPrefix + base64.StdEncoding.EncodeToString([]byte(previous))
	}
	key.SetValue(value)
}

func stringValue(value *string) string {
	if value == nil { return "" }
	return *value
}

func floatValue(value *float64) string {
	if value == nil { return "" }
	return strconv.FormatFloat(*value, 'f', 6, 64)
}

func wayfireMode(mode string) string {
	if mode == "" || mode == "preferred" {
		return ""
	}
	resolution, refresh, hasRefresh := strings.Cut(mode, "@")
	if !hasRefresh {
		return mode
	}
	hz, err := strconv.ParseFloat(refresh, 64)
	if err != nil {
		return mode
	}
	return fmt.Sprintf("%s@%.0f", resolution, hz*1000)
	}

func wayfireBinding(keys string) string {
	parts := strings.Split(keys, "+")
	mods := make([]string, 0, len(parts))
	key := ""
	for _, part := range parts {
		switch strings.ToUpper(part) {
		case "SUPER":
			mods = append(mods, "<super>")
		case "CTRL":
			mods = append(mods, "<ctrl>")
		case "ALT":
			mods = append(mods, "<alt>")
		case "SHIFT":
			mods = append(mods, "<shift>")
		default:
			key = wayfireKey(part)
		}
	}
	binding := strings.Join(mods, " ")
	if binding != "" {
		binding += " "
	}
	return binding + key
}

func wayfireKey(key string) string {
	key = strings.ToUpper(key)
	if strings.HasPrefix(key, "KEY_") {
		return key
	}
	switch key {
	case "ESCAPE":
		key = "ESC"
	case "RETURN":
		key = "ENTER"
	case "BACKSPACE":
		key = "BACKSPACE"
	case "SPACE":
		key = "SPACE"
	}
	return "KEY_" + key
}

