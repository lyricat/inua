package shell

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/workshop"
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
	if found == nil {
		// A window of an app's with a class of its own, named under the
		// app's (Mail's composer: io.github.lyricat.Inua.Mail.Compose).
		if i := strings.LastIndexByte(class, '.'); i > 0 && strings.Count(class[:i], ".") >= 2 {
			found = a.byID[class[:i]]
		}
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
		if strings.HasPrefix(id, "inua:") {
			e = ShellApp(id)
		}
		if e == nil {
			continue
		}
		if _, dup := at[e.ID]; dup {
			continue
		}
		add(DockItem{Key: e.ID, Name: AppName(e), Icon: e.Icon, Entry: e, Pinned: true})
	}
	for _, t := range snap.Toplevels {
		if t.Class == "" {
			continue
		}
		entry := apps.ForClass(t.Class)
		if isFcitxWindow(t.Class, entry) || (skip != nil && skip(t)) {
			continue
		}
		if t.Class == AppID { // the shell's own windows: Settings and its dialogs
			entry = ShellApps[0]
		}
		key, item := t.Class, DockItem{Key: t.Class, Name: cmpOr(t.Title, t.Class), Icon: t.Class}
		if entry != nil {
			key, item = entry.ID, DockItem{Key: entry.ID, Name: AppName(entry), Icon: entry.Icon, Entry: entry}
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

// AppName is an entry's name; the shell's own are translated here.
func AppName(e *desktop.Entry) string {
	if strings.HasPrefix(e.ID, "inua:") {
		return i18n.T(e.Name)
	}
	return e.Name
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
// groups and order: everyday apps, tools, network, games, then the
// shell's own. An "inua:" ID is one of ShellApps.
var LightAppGroups = []struct {
	Title string
	IDs   []string
}{
	{i18n.M("EVERYDAY"), []string{
		"io.github.lyricat.Inua.Clock",
		"io.github.lyricat.Inua.Calendar",
		"io.github.lyricat.Inua.Mail",
		"io.github.lyricat.Inua.Weather",
		"io.github.lyricat.Inua.Stocks",
		"io.github.lyricat.Inua.Music",
		"io.github.lyricat.Inua.Video",
		"io.github.lyricat.Inua.Assistant",
	}},
	{i18n.M("TOOLS"), []string{
		"io.github.lyricat.Inua.Files",
		"io.github.lyricat.Inua.Term",
		"io.github.lyricat.Inua.Calculator",
		"io.github.lyricat.Inua.ColorPicker",
		"inua:screenshot",
		"io.github.lyricat.Inua.Viewer",
		"io.github.lyricat.Inua.SystemMonitor",
		"io.github.lyricat.Inua.DiskUsage",
		"io.github.lyricat.Inua.Pass",
		"io.github.lyricat.Inua.GPG",
	}},
	{i18n.M("NETWORK"), []string{
		"io.github.lyricat.Inua.Tailscale",
		"io.github.lyricat.Inua.Remote",
		"io.github.lyricat.Inua.MobileLink",
	}},
	{i18n.M("PLAY"), []string{
		"io.github.lyricat.Inua.Minesweeper",
		"io.github.lyricat.Inua.SpacePinball",
		"io.github.lyricat.Inua.SmallFortune",
		"io.github.lyricat.Inua.ChineseCheckers",
		"io.github.lyricat.Inua.GameLobby",
	}},
	{i18n.M("SYSTEM"), []string{
		"inua:settings",
		"inua:launcher",
		"io.github.lyricat.Inua.VM",
	}},
}

// AppID is the shell's application ID, the class of its windows.
const AppID = "io.github.lyricat.Inua"

// ShellApps are the shell's own tools in the drawer. They are not desktop
// entries: the shell opens them itself, by their "inua:" IDs.
var ShellApps = []*desktop.Entry{
	{ID: "inua:settings", Name: i18n.M("Settings"), Comment: i18n.M("inua's settings"), Icon: "io.github.lyricat.Inua.Settings"},
	{ID: "inua:launcher", Name: i18n.M("Launcher"), Comment: i18n.M("Find apps, windows and answers"), Icon: "io.github.lyricat.Inua.Launcher"},
	{ID: "inua:screenshot", Name: i18n.M("Screenshot"), Comment: i18n.M("Capture an area, a window or a screen"), Icon: "io.github.lyricat.Inua.Screenshot"},
}

// ShellApp is the shell's tool with an "inua:" ID (nil: none).
func ShellApp(id string) *desktop.Entry {
	if i := slices.IndexFunc(ShellApps, func(e *desktop.Entry) bool { return e.ID == id }); i >= 0 {
		return ShellApps[i]
	}
	return nil
}

// WorkshopApp is the Workshop light app, which ends the workshop drawer.
const WorkshopApp = "io.github.lyricat.Inua.Workshop"

// workshopTitles are the workshop drawer's group titles, by category.
var workshopTitles = map[string]string{
	"everyday": i18n.M("EVERYDAY"), "tools": i18n.M("TOOLS"), "network": i18n.M("NETWORK"),
	"development": i18n.M("DEVELOPMENT"), "media": i18n.M("MEDIA"), "games": i18n.M("GAMES"),
	"system": i18n.M("SYSTEM"), "other": i18n.M("OTHER"),
}

// WorkshopSections are the dock's workshop drawer: the workshop apps
// installed, by category in workshop.Categories' order, each by its
// desktop entry (one made from the manifest while the index has not seen
// the entry yet); then the Workshop app, to manage them, in a group of its
// own.
func WorkshopSections(apps AppIndex, installed []workshop.Manifest) []AppGroup {
	by := map[string][]*desktop.Entry{}
	for _, m := range installed {
		e := apps.ByID(strings.ToLower(m.AppID()))
		if e == nil {
			e = &desktop.Entry{ID: m.AppID(), Name: m.Name, Comment: m.Description, Icon: m.IconPath(),
				Command: []string{"inua-apps", "workshop", m.ID}, WMClass: m.AppID()}
		}
		by[m.Group()] = append(by[m.Group()], e)
	}
	var out []AppGroup
	for _, c := range workshop.Categories {
		if len(by[c]) > 0 {
			out = append(out, AppGroup{workshopTitles[c], by[c]})
		}
	}
	if e := apps.ByID(strings.ToLower(WorkshopApp)); e != nil {
		out = append(out, AppGroup{i18n.M("WORKSHOP"), []*desktop.Entry{e}})
	}
	return out
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
				e = ShellApp(id)
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
