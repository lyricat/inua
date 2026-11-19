// Package portalconf makes inua's backends the ones xdg-desktop-portal
// asks (docs/portals.md). The frontend reads its backends and its config
// from one directory, /usr/share/xdg-desktop-portal, or from
// XDG_DESKTOP_PORTAL_DIR when that is set. inua keeps a directory of its
// own: its backends' .portal files, links to the system's, and a
// hyprland-portals.conf naming inua's backend for the interfaces turned
// on. inua-session gives the directory to the frontend, so only inua's
// sessions use it.
package portalconf

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/portal"
)

// Backend is one of inua's backends: its .portal file's name, its bus
// name, the interfaces it serves, and the program D-Bus starts for it.
type Backend struct {
	Name       string // "inua"
	Bus        string
	Interfaces []string // without portal.Prefix
	Program    string   // "": not started by D-Bus (the shell is the session's)
	Args       string   // what D-Bus starts it with
}

// Backends are inua's, with the interfaces built so far.
var Backends = []Backend{
	{Name: "inua", Bus: portal.ShellName, Interfaces: []string{"Settings", "Notification", "Inhibit", "Screenshot", "Access", "Background", "Wallpaper"}},
	{Name: "inua-files", Bus: portal.FilesName, Interfaces: []string{"FileChooser", "AppChooser"}, Program: "inua-files", Args: " -portal"},
	{Name: "inua-keyring", Bus: portal.KeyringName, Interfaces: []string{"Secret"}, Program: "inua-keyring"},
}

// Interfaces are the interfaces inua can serve, in Backends' order, then
// SharePicker.
func Interfaces() []string {
	var out []string
	for _, b := range Backends {
		out = append(out, b.Interfaces...)
	}
	return append(out, SharePicker)
}

// BackendOf is the backend serving an interface, by its name without
// portal.Prefix, matched ignoring case; ok is false for one inua does not
// serve.
func BackendOf(iface string) (b Backend, name string, ok bool) {
	for _, b := range Backends {
		for _, i := range b.Interfaces {
			if strings.EqualFold(i, iface) {
				return b, i, true
			}
		}
	}
	return Backend{}, "", false
}

// Dirs are where the files are: inua's directory and the system's.
type Dirs struct {
	Own    string // $XDG_DATA_HOME/inua/portals
	System string // /usr/share/xdg-desktop-portal
	// Configs are where the session's config is looked for before
	// System, most important first: ~/.config/xdg-desktop-portal,
	// /etc/xdg-desktop-portal.
	Configs []string
	// Services is the user's D-Bus services directory, where the files
	// that start a backend's program go.
	Services string
	// Exec finds a backend's program.
	Exec func(program string) (string, error)
	// Xdph is xdg-desktop-portal-hyprland's config, where SharePicker is.
	Xdph string
}

// Default are the directories of this user.
func Default() Dirs {
	home, _ := os.UserHomeDir()
	data := cmp.Or(os.Getenv("XDG_DATA_HOME"), filepath.Join(home, ".local", "share"))
	conf := cmp.Or(os.Getenv("XDG_CONFIG_HOME"), filepath.Join(home, ".config"))
	return Dirs{
		Own:      filepath.Join(data, "inua", "portals"),
		System:   "/usr/share/xdg-desktop-portal",
		Configs:  []string{filepath.Join(conf, "xdg-desktop-portal"), "/etc/xdg-desktop-portal"},
		Services: filepath.Join(data, "dbus-1", "services"),
		Exec:     findProgram,
		Xdph:     filepath.Join(conf, "hypr", "xdph.conf"),
	}
}

// findProgram finds one of inua's programs on PATH, else beside this one.
func findProgram(program string) (string, error) {
	if path, err := exec.LookPath(program); err == nil {
		return filepath.Abs(path)
	}
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(self), program)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("%s is not installed", program)
	}
	return path, nil
}

// confName is the config of a Hyprland session, which inua's are.
const confName = "hyprland-portals.conf"

const header = "# Written by inua portals (docs/portals.md) from ~/.config/xdg-desktop-portal/hyprland-portals.conf,\n# else the system's: change that one, then run inua portals enable.\n"

// Enabled are the interfaces whose config line names inua's backend
// first, and SharePicker when xdph's config names inua's picker.
func (d Dirs) Enabled() []string {
	on := inuaChosen(d.chosen())
	if d.Xdph != "" && d.sharePickerOn() {
		on = append(on, SharePicker)
	}
	return on
}

// inuaChosen are the interfaces chosen gives to inua's backends, in
// Backends' order.
func inuaChosen(chosen map[string]string) []string {
	var on []string
	for _, b := range Backends {
		for _, iface := range b.Interfaces {
			if chosen[iface] == b.Name {
				on = append(on, iface)
			}
		}
	}
	return on
}

// PickerOn is whether inua's picker chooses files for apps: GTK then
// asks the portal from inua's own apps too (gtkx.ForcePortals).
func PickerOn() bool { return slices.Contains(Default().Enabled(), "FileChooser") }

// Set turns interfaces on or off (inua's backend, or the session's
// config's) and writes the directory again; with no interface left to
// inua's config, the directory goes. It reports whether any is on.
func (d Dirs) Set(on bool, ifaces ...string) (bool, error) {
	return d.change(ifaces, func(own, _ string) string {
		if on {
			return own
		}
		return ""
	})
}

// remove takes the directory and the service files away.
func (d Dirs) remove() error { return errors.Join(d.services(nil), os.RemoveAll(d.Own)) }

// services writes the D-Bus service file of each backend with a program
// and an interface on, and removes the others'.
func (d Dirs) services(enabled []string) error {
	for _, b := range Backends {
		if b.Program == "" || d.Services == "" {
			continue
		}
		path := filepath.Join(d.Services, b.Bus+".service")
		if !slices.ContainsFunc(b.Interfaces, func(i string) bool { return slices.Contains(enabled, i) }) {
			if body, err := os.ReadFile(path); err == nil && strings.HasPrefix(string(body), serviceHeader) {
				os.Remove(path)
			}
			continue
		}
		exe, err := d.Exec(b.Program)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(d.Services, 0o755); err != nil {
			return err
		}
		body := fmt.Sprintf("%s[D-BUS Service]\nName=%s\nExec=%s%s\n", serviceHeader, b.Bus, exe, b.Args)
		if err := writeIfChanged(path, body); err != nil {
			return err
		}
	}
	return nil
}

const serviceHeader = "# Written by inua portals (docs/portals.md).\n"

// Refresh writes the directory again, with the system's backends as they
// are now, if inua's config chooses a backend for any interface, and
// reports whether it does.
func (d Dirs) Refresh() (bool, error) {
	chosen := d.chosen()
	if len(chosen) == 0 {
		return false, nil
	}
	return true, d.write(chosen)
}

func (d Dirs) write(chosen map[string]string) error {
	if err := d.services(inuaChosen(chosen)); err != nil {
		return err
	}
	if err := os.MkdirAll(d.Own, 0o755); err != nil {
		return err
	}
	keep := map[string]bool{confName: true}
	// inua's backends.
	for _, b := range Backends {
		name := b.Name + ".portal"
		keep[name] = true
		var ifs []string
		for _, i := range b.Interfaces {
			ifs = append(ifs, portal.Prefix+i)
		}
		body := fmt.Sprintf("[portal]\nDBusName=%s\nInterfaces=%s;\n", b.Bus, strings.Join(ifs, ";"))
		if err := writeIfChanged(filepath.Join(d.Own, name), body); err != nil {
			return err
		}
	}
	// The system's backends, and the configs of other desktops.
	links := map[string]string{}
	if entries, err := os.ReadDir(filepath.Join(d.System, "portals")); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".portal") && !keep[e.Name()] {
				links[e.Name()] = filepath.Join(d.System, "portals", e.Name())
			}
		}
	}
	dirs := []string{d.System}
	for i := len(d.Configs) - 1; i >= 0; i-- {
		dirs = append(dirs, d.Configs[i])
	}
	for _, dir := range dirs {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if n := e.Name(); strings.HasSuffix(n, "portals.conf") && n != confName {
				links[n] = filepath.Join(dir, n) // a later directory wins
			}
		}
	}
	for name, target := range links {
		keep[name] = true
		path := filepath.Join(d.Own, name)
		if t, err := os.Readlink(path); err == nil && t == target {
			continue
		}
		os.Remove(path)
		if err := os.Symlink(target, path); err != nil {
			return err
		}
	}
	// What the system no longer has.
	entries, err := os.ReadDir(d.Own)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !keep[e.Name()] {
			os.Remove(filepath.Join(d.Own, e.Name()))
		}
	}
	return writeIfChanged(filepath.Join(d.Own, confName), d.conf(chosen))
}

// conf is the session's config with inua's lines: the user's, else the
// system's, with each interface chosen naming its backend first and what
// the config named before after it.
func (d Dirs) conf(chosen map[string]string) string {
	base, order := d.baseConfig()
	lines := map[string]string{}
	for _, k := range order {
		lines[k] = base[k]
	}
	for _, b := range Backends {
		for _, iface := range b.Interfaces {
			first, ok := chosen[iface]
			if !ok {
				continue
			}
			key := portal.Prefix + iface
			rest := cmp.Or(base[key], base["default"])
			next := slices.DeleteFunc(strings.Split(rest, ";"), func(s string) bool { return s == "" || s == first })
			if _, ok := lines[key]; !ok {
				order = append(order, key)
			}
			lines[key] = strings.Join(append([]string{first}, next...), ";")
		}
	}
	var sb strings.Builder
	sb.WriteString(header + "[preferred]\n")
	for _, k := range order {
		fmt.Fprintf(&sb, "%s=%s\n", k, lines[k])
	}
	return sb.String()
}

// readPreferred reads a config's [preferred] section, and its keys in
// order; nil when the file cannot be read.
func readPreferred(path string) (map[string]string, []string) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	pref := map[string]string{}
	var keys []string
	in := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "["):
			in = line == "[preferred]"
		case in:
			if k, v, ok := strings.Cut(line, "="); ok {
				k, v = strings.TrimSpace(k), strings.TrimSpace(v)
				if _, seen := pref[k]; !seen {
					keys = append(keys, k)
				}
				pref[k] = v
			}
		}
	}
	return pref, keys
}

func writeIfChanged(path, body string) error {
	if b, err := os.ReadFile(path); err == nil && string(b) == body {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return errors.Join(err, os.Remove(tmp))
	}
	return nil
}
