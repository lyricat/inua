package shell

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/hypr"
)

// DockItem is one app on the dock.
type DockItem struct {
	// Key is the app's desktop ID, or the window class of an app without a
	// desktop entry.
	Key     string
	Name    string
	Icon    string         // icon name or path; the class when there is no entry
	Entry   *desktop.Entry // nil: no desktop entry, so it cannot be launched or pinned
	Pinned  bool
	Windows []hypr.Toplevel // its windows, most recently focused first
	Active  bool            // it has the focused window
}

// AppIndex finds desktop entries for window classes.
type AppIndex struct {
	byID, byClass map[string]*desktop.Entry
	byName        map[string]*desktop.Entry // loose names, the last resort (see looseNames)
}

// NewAppIndex indexes entries by ID and by the window class they declare.
func NewAppIndex(entries []*desktop.Entry) AppIndex {
	a := AppIndex{byID: map[string]*desktop.Entry{}, byClass: map[string]*desktop.Entry{}, byName: map[string]*desktop.Entry{}}
	for _, e := range entries {
		a.byID[e.ID] = e
		a.byID[strings.ToLower(e.ID)] = e
		if e.WMClass != "" {
			a.byClass[strings.ToLower(e.WMClass)] = e
		}
		for _, n := range looseNames(e) {
			if old := a.byName[n]; old == nil || e.ID < old.ID {
				a.byName[n] = e
			}
		}
	}
	return a
}

// loose folds a name so "mixin_desktop", "Mixin-Desktop" and
// "mixin desktop" are the same.
func loose(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '_' || r == ' ' {
			return '-'
		}
		return r
	}, strings.ToLower(s))
}

// launchers are programs that run another; an entry is not named after
// them.
var launchers = map[string]bool{"env": true, "sh": true, "bash": true, "flatpak": true, "snap": true,
	"python": true, "python3": true, "java": true, "electron": true, "wine": true, "gtk-launch": true}

// looseNames are what a window class might be for an entry that does not
// declare it: its ID, a snap entry's app part ("mixin-desktop" for
// "mixin-desktop_mixin-desktop"), and the program it runs.
func looseNames(e *desktop.Entry) []string {
	names := []string{loose(e.ID)}
	if _, app, ok := strings.Cut(e.ID, "_"); ok && app != "" {
		names = append(names, loose(app))
	}
	if len(e.Command) > 0 {
		if prog := filepath.Base(e.Command[0]); !launchers[prog] {
			names = append(names, loose(prog))
		}
	}
	return names
}

// ByID is the entry with a desktop ID.
func (a AppIndex) ByID(id string) *desktop.Entry { return a.byID[id] }

// ForClass is the app a window class belongs to: the entry declaring it
// (StartupWMClass), the entry named after it ("firefox", "org.gnome.Nautilus"
// for the class "org.gnome.Nautilus"), or the reverse-DNS entry ending in it
// ("org.gnome.Nautilus" for "nautilus"), else one whose ID or program
// is the class but for case, "-" and "_" (snaps: "mixin_desktop" for
// mixin-desktop_mixin-desktop, running /snap/bin/mixin-desktop).
func (a AppIndex) ForClass(class string) *desktop.Entry {
	c := strings.ToLower(class)
	if c == "" {
		return nil
	}
	if e := a.byClass[c]; e != nil {
		return e
	}
	if e := a.byID[class]; e != nil {
		return e
	}
	if e := a.byID[c]; e != nil {
		return e
	}
	var found *desktop.Entry
	for id, e := range a.byID {
		if strings.HasSuffix(id, "."+c) && (found == nil || e.ID < found.ID) {
			found = e
		}
	}
	if found == nil {
		found = a.byName[loose(class)]
	}
	return found
}

// Dock lists the dock's apps: the pinned ones in order (those not
// installed are skipped), then the others with windows, in the order their
// first window appeared. Windows skip reports are left out (nil: none).
func Dock(pinned []string, apps AppIndex, snap hypr.Snapshot, skip func(hypr.Toplevel) bool) []DockItem {
	var items []DockItem
	at := map[string]int{}
	add := func(it DockItem) {
		at[it.Key] = len(items)
		items = append(items, it)
	}
	for _, id := range pinned {
		e := apps.ByID(id)
		if e == nil {
			continue
		}
		if _, dup := at[e.ID]; dup {
			continue
		}
		add(DockItem{Key: e.ID, Name: e.Name, Icon: e.Icon, Entry: e, Pinned: true})
	}
	for _, t := range snap.Toplevels {
		if t.Class == "" {
			continue
		}
		entry := apps.ForClass(t.Class)
		if isFcitxWindow(t.Class, entry) || (skip != nil && skip(t)) {
			continue
		}
		key, item := t.Class, DockItem{Key: t.Class, Name: cmpOr(t.Title, t.Class), Icon: t.Class}
		if entry != nil {
			key, item = entry.ID, DockItem{Key: entry.ID, Name: entry.Name, Icon: entry.Icon, Entry: entry}
		}
		j, ok := at[key]
		if !ok {
			add(item)
			j = len(items) - 1
		}
		it := &items[j]
		it.Windows = append(it.Windows, t)
		if t.Address == snap.ActiveToplevel {
			it.Active = true
		}
	}
	for i := range items {
		ws := items[i].Windows
		slices.SortStableFunc(ws, func(a, b hypr.Toplevel) int {
			return focusHistory(a, len(snap.Toplevels)) - focusHistory(b, len(snap.Toplevels))
		})
	}
	return items
}

// isFcitxWindow tells the input method's candidate popup by its class:
// Hyprland lists it as a window while composing, but it is not an app to
// launch or switch to.
func isFcitxWindow(class string, entry *desktop.Entry) bool {
	if entry != nil && strings.EqualFold(entry.ID, "org.fcitx.Fcitx5") {
		return true
	}
	class = strings.ToLower(strings.TrimSpace(class))
	return class == "fcitx" || class == "fcitx5" || class == "fcitx5-wayland-launcher" ||
		strings.HasPrefix(class, "fcitx-") || strings.HasPrefix(class, "org.fcitx.")
}

// IsInputMethodProgram reports whether a program, by its name in
// /proc/PID/comm (at most 15 characters), is the input method, whose
// windows the dock leaves out whatever their class.
func IsInputMethodProgram(name string) bool {
	switch strings.TrimSpace(name) {
	case "fcitx", "fcitx5":
		return true
	}
	return false
}

// DockTarget is what clicking an app does: launch it when it has no
// windows; bring its most recent window forward; or, when it is already in
// front, the window it has not used longest, so clicks cycle through them.
func DockTarget(it DockItem) (launch bool, address string) {
	switch {
	case len(it.Windows) == 0:
		return it.Entry != nil, ""
	case it.Active && len(it.Windows) > 1:
		return false, it.Windows[len(it.Windows)-1].Address
	}
	return false, it.Windows[0].Address
}

// Pin returns pinned with key added at the end, or removed.
func Pin(pinned []string, key string, on bool) []string {
	out := slices.DeleteFunc(slices.Clone(pinned), func(p string) bool { return p == key })
	if on {
		out = append(out, key)
	}
	return out
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// MovePinned moves key in pinned to just before the app before, or to the
// end when before is "" or not pinned. An app not pinned yet is pinned
// there; dropping an app next to itself changes nothing.
func MovePinned(pinned []string, key, before string) []string {
	if key == before {
		return slices.Clone(pinned)
	}
	out := slices.DeleteFunc(slices.Clone(pinned), func(p string) bool { return p == key })
	if i := slices.Index(out, before); before != "" && i >= 0 {
		return slices.Insert(out, i, key)
	}
	return append(out, key)
}

// LightAppGroups are inua's light apps' desktop IDs, in the drawer's
// groups and order: the day's things, then tools. An "inua:" ID is one of
// ShellApps.
var LightAppGroups = []struct {
	Title string
	IDs   []string
}{
	{"EVERYDAY", []string{
		"io.github.lyricat.Inua.Clock",
		"io.github.lyricat.Inua.Calendar",
		"io.github.lyricat.Inua.Weather",
		"io.github.lyricat.Inua.Stocks",
		"io.github.lyricat.Inua.Assistant",
	}},
	{"TOOLS", []string{
		"io.github.lyricat.Inua.Calculator",
		"io.github.lyricat.Inua.ColorPicker",
		"inua:screenshot",
		"io.github.lyricat.Inua.Viewer",
		"io.github.lyricat.Inua.SystemMonitor",
	}},
	{"NETWORK", []string{
		"io.github.lyricat.Inua.Tailscale",
	}},
	{"SYSTEM", []string{
		"inua:settings",
		"inua:launcher",
	}},
}

// ShellApps are the shell's own tools in the drawer. They are not desktop
// entries: the shell opens them itself, by their "inua:" IDs.
var ShellApps = []*desktop.Entry{
	{ID: "inua:settings", Name: "Settings", Comment: "inua's settings", Icon: "io.github.lyricat.Inua.Settings"},
	{ID: "inua:launcher", Name: "Launcher", Comment: "Find apps, windows and answers", Icon: "io.github.lyricat.Inua.Launcher"},
	{ID: "inua:screenshot", Name: "Screenshot", Comment: "Capture an area, a window or a screen", Icon: "io.github.lyricat.Inua.Screenshot"},
}

// AppGroup is a titled group of installed apps.
type AppGroup struct {
	Title string
	Apps  []*desktop.Entry
}

// LightAppSections are the drawer's groups of the shell's tools and the
// light apps that are installed; a group with none of them is left out.
func LightAppSections(apps AppIndex) []AppGroup {
	var out []AppGroup
	for _, g := range LightAppGroups {
		var es []*desktop.Entry
		for _, id := range g.IDs {
			e := apps.ByID(strings.ToLower(id))
			if strings.HasPrefix(id, "inua:") {
				i := slices.IndexFunc(ShellApps, func(e *desktop.Entry) bool { return e.ID == id })
				e = ShellApps[i]
			}
			if e != nil {
				es = append(es, e)
			}
		}
		if len(es) > 0 {
			out = append(out, AppGroup{g.Title, es})
		}
	}
	return out
}

// LightApps are the light apps that are installed, in the drawer's order.
func LightApps(apps AppIndex) []*desktop.Entry {
	var out []*desktop.Entry
	for _, g := range LightAppSections(apps) {
		out = append(out, g.Apps...)
	}
	return out
}
