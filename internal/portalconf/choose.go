package portalconf

import (
	"bufio"
	"cmp"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/portal"
)

// portalFile is what a backend's .portal file says: the interfaces it
// serves, without portal.Prefix, and the desktops it names for itself.
type portalFile struct {
	Interfaces []string
	UseIn      []string
}

func (p portalFile) serves(iface string) bool { return slices.Contains(p.Interfaces, iface) }

// readPortals reads the .portal files in dir, by backend name.
func readPortals(dir string) map[string]portalFile {
	out := map[string]portalFile{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".portal")
		if !ok {
			continue
		}
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var p portalFile
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
			if !ok {
				continue
			}
			list := slices.DeleteFunc(strings.Split(strings.TrimSpace(v), ";"), func(s string) bool { return s == "" })
			switch strings.TrimSpace(k) {
			case "Interfaces":
				for _, i := range list {
					p.Interfaces = append(p.Interfaces, strings.TrimPrefix(i, portal.Prefix))
				}
			case "UseIn":
				p.UseIn = list
			}
		}
		f.Close()
		out[name] = p
	}
	return out
}

// systemPortals are the system's backends with inua's added, which serve
// whether or not the frontend is given inua's directory.
func (d Dirs) systemPortals() map[string]portalFile {
	out := readPortals(filepath.Join(d.System, "portals"))
	for _, b := range Backends {
		out[b.Name] = portalFile{Interfaces: b.Interfaces}
	}
	return out
}

// resolve is the backend xdg-desktop-portal gives apps for iface: the
// first in the config's list that serves it (the config the first of
// names found in dirs), else one whose UseIn names the desktop, else
// GTK's, as the frontend falls back; "" for none.
func resolve(dirs, names []string, portals map[string]portalFile, desktops []string, iface string) string {
	for _, b := range strings.Split(preferred(dirs, names, iface), ";") {
		switch b = strings.TrimSpace(b); b {
		case "none":
			return ""
		case "*":
			for _, n := range slices.Sorted(maps.Keys(portals)) {
				if portals[n].serves(iface) {
					return n
				}
			}
		default:
			if portals[b].serves(iface) {
				return b
			}
		}
	}
	for _, n := range slices.Sorted(maps.Keys(portals)) {
		p := portals[n]
		if p.serves(iface) && slices.ContainsFunc(p.UseIn, func(u string) bool {
			return slices.ContainsFunc(desktops, func(d string) bool { return strings.EqualFold(u, d) })
		}) {
			return n
		}
	}
	if portals["gtk"].serves(iface) {
		return "gtk"
	}
	return ""
}

// preferred is iface's list in the one config the frontend reads: the
// first of names found in dirs.
func preferred(dirs, names []string, iface string) string {
	for _, name := range names {
		for _, dir := range dirs {
			if pref, _ := readPreferred(filepath.Join(dir, name)); pref != nil {
				return cmp.Or(pref[portal.Prefix+iface], pref["default"])
			}
		}
	}
	return ""
}

// baseConfig is the session's config without inua: the user's, else
// /etc's, else the system's, with its keys in order.
func (d Dirs) baseConfig() (map[string]string, []string) {
	for _, dir := range append(slices.Clone(d.Configs), d.System) {
		if pref, keys := readPreferred(filepath.Join(dir, confName)); pref != nil {
			return pref, keys
		}
	}
	return map[string]string{"default": "hyprland;gtk"}, []string{"default"}
}

// baseBackend is the backend the session's config gives iface without
// inua's directory.
func (d Dirs) baseBackend(iface string) string {
	return resolve(append(slices.Clone(d.Configs), d.System), []string{confName}, readPortals(filepath.Join(d.System, "portals")), []string{"Hyprland"}, iface)
}

// chosen are the interfaces whose backend inua's config changes, and the
// backend it names first for each.
func (d Dirs) chosen() map[string]string {
	own, _ := readPreferred(filepath.Join(d.Own, confName))
	base, _ := d.baseConfig()
	out := map[string]string{}
	for _, b := range Backends {
		for _, iface := range b.Interfaces {
			key := portal.Prefix + iface
			first, _, _ := strings.Cut(own[key], ";")
			was, _, _ := strings.Cut(cmp.Or(base[key], base["default"]), ";")
			if first != "" && first != was {
				out[iface] = first
			}
		}
	}
	return out
}

// Choose makes backend the one apps get for each of ifaces it serves,
// and gives the others back to the session's config, then writes the
// directory again as Set does. backend is one of inua's (inua,
// inua-files, inua-keyring), "inua" for SharePicker, or a .portal of the
// system's; "" gives them all back.
func (d Dirs) Choose(backend string, ifaces ...string) (bool, error) {
	system := readPortals(filepath.Join(d.System, "portals"))
	pick := func(own, iface string) string {
		switch {
		case backend == own || (iface == SharePicker && backend == "inua"):
			return own
		case iface != SharePicker && system[backend].serves(iface):
			return backend
		}
		return ""
	}
	if backend != "" && !slices.ContainsFunc(ifaces, func(i string) bool {
		if strings.EqualFold(i, SharePicker) {
			return pick("inua", SharePicker) != ""
		}
		b, name, ok := BackendOf(i)
		return ok && pick(b.Name, name) != ""
	}) {
		return false, fmt.Errorf("%s serves none of %s", backend, strings.Join(ifaces, ", "))
	}
	return d.change(ifaces, pick)
}

// change gives each interface the backend pick names (own is inua's
// backend for it; "" the session's config) and writes the directory
// again; with no interface left to inua's config, the directory goes.
// It reports whether the directory or the share picker is on.
func (d Dirs) change(ifaces []string, pick func(own, iface string) string) (bool, error) {
	chosen := d.chosen()
	picker := d.Xdph != "" && d.sharePickerOn()
	type step struct{ iface, backend string }
	var steps []step
	for _, i := range ifaces {
		if strings.EqualFold(i, SharePicker) {
			steps = append(steps, step{SharePicker, pick("inua", SharePicker)})
			continue
		}
		b, name, ok := BackendOf(i)
		if !ok {
			return false, fmt.Errorf("inua serves no portal interface %q (%s)", i, strings.Join(Interfaces(), ", "))
		}
		steps = append(steps, step{name, pick(b.Name, name)})
	}
	for _, s := range steps {
		if s.iface == SharePicker {
			on := s.backend == "inua"
			if err := d.setSharePicker(on); err != nil {
				return false, err
			}
			picker = on
			continue
		}
		b, _, _ := BackendOf(s.iface)
		if s.backend == "" || (s.backend != b.Name && s.backend == d.baseBackend(s.iface)) {
			delete(chosen, s.iface) // what the session's config gives already
		} else {
			chosen[s.iface] = s.backend
		}
	}
	if len(chosen) == 0 {
		return picker, d.remove()
	}
	return true, d.write(chosen)
}

// Options are the backends that can serve iface: inua's first, then the
// system's that list it, by name. SharePicker's are inua's and
// xdg-desktop-portal-hyprland's own.
func (d Dirs) Options(iface string) []string {
	if iface == SharePicker {
		return []string{"inua", "hyprland"}
	}
	var out []string
	if b, _, ok := BackendOf(iface); ok {
		out = append(out, b.Name)
	}
	system := readPortals(filepath.Join(d.System, "portals"))
	for _, n := range slices.Sorted(maps.Keys(system)) {
		if system[n].serves(iface) && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// Frontend is how the running xdg-desktop-portal read its backends: the
// directory it was given ("" for the system's) and its desktop. One not
// running is the one the user's systemd would start.
type Frontend struct {
	Running bool
	Dir     string
	Desktop string
}

// ReadFrontend reads the running frontend's environment.
func ReadFrontend() Frontend {
	out, _ := exec.Command("systemctl", "--user", "show", "-p", "MainPID", "--value", "xdg-desktop-portal.service").Output()
	if pid := strings.TrimSpace(string(out)); pid != "" && pid != "0" {
		if env, err := os.ReadFile(filepath.Join("/proc", pid, "environ")); err == nil {
			f := Frontend{Running: true}
			for _, kv := range strings.Split(string(env), "\x00") {
				if v, ok := strings.CutPrefix(kv, DirEnv+"="); ok {
					f.Dir = v
				} else if v, ok := strings.CutPrefix(kv, "XDG_CURRENT_DESKTOP="); ok {
					f.Desktop = v
				}
			}
			return f
		}
	}
	return Frontend{Dir: SessionDir(), Desktop: os.Getenv("XDG_CURRENT_DESKTOP")}
}

// Wanted is the frontend that reads what inua's config says now.
func (d Dirs) Wanted() Frontend {
	f := Frontend{Desktop: cmp.Or(os.Getenv("XDG_CURRENT_DESKTOP"), "Hyprland")}
	if len(d.chosen()) > 0 {
		f.Dir = d.Own
	}
	return f
}

// Serving is the backend f gives apps for iface; "" for none. For
// SharePicker it is the picker xdph's config names.
func (d Dirs) Serving(f Frontend, iface string) string {
	if iface == SharePicker {
		if d.sharePickerOn() {
			return "inua"
		}
		return "hyprland"
	}
	desktops := strings.Split(cmp.Or(f.Desktop, "Hyprland"), ":")
	var names []string
	for _, desk := range desktops {
		names = append(names, strings.ToLower(desk)+"-portals.conf")
	}
	names = append(names, "portals.conf")
	if f.Dir != "" {
		return resolve([]string{f.Dir}, names, readPortals(f.Dir), desktops, iface)
	}
	return resolve(append(slices.Clone(d.Configs), d.System), names, readPortals(filepath.Join(d.System, "portals")), desktops, iface)
}

// Label is a backend's name as people know it.
func Label(backend string) string {
	switch backend {
	case "inua", "inua-files", "inua-keyring":
		return "inua"
	case "gtk":
		return "GTK"
	case "gnome":
		return "GNOME"
	case "gnome-keyring":
		return "GNOME Keyring"
	case "hyprland":
		return "Hyprland"
	case "kde":
		return "KDE"
	case "wlr":
		return "wlroots"
	case "lxqt":
		return "LXQt"
	case "xapp":
		return "XApp"
	case "cosmic":
		return "COSMIC"
	}
	return backend
}
