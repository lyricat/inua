package appmenu

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"

	"github.com/lyricat/inua/share/inua/menus"
)

const (
	// PackFallback uses the pack only when the application publishes no
	// menu of its own. It is the default.
	PackFallback = "fallback"
	// PackReplace shows the pack instead of a menu the application published.
	PackReplace = "replace"

	// IncludeGeneric inserts the shell's generic sections (File, Edit, View, Help).
	IncludeGeneric = "generic"
	// IncludeWindow inserts the window actions (Fullscreen, Float, Center).
	IncludeWindow = "window"

	// ActionKeys sends a shortcut to the window. Mods and Key say which.
	ActionKeys = "keys"

	maxPackBytes = 256 << 10
	maxPackDepth = 8
)

// Pack is one application's menu, read from a TOML file. See docs/appmenus.md.
type Pack struct {
	ID         string
	Match      []string
	Suffix     []string
	Contains   []string
	When       string
	SkipA11y   bool
	NeverPress bool
	Include    []string
	Sections   []PackSection
}

// PackSection is one top-level menu of a pack.
type PackSection struct {
	Label   string
	Include string
	Items   []PackItem
}

// PackItem is one line of a pack menu.
type PackItem struct {
	Label     string
	Separator bool
	// Disabled shows the line greyed out: the application's menu has it,
	// but the pack has no key to invoke it with.
	Disabled bool
	Action   string
	Keys     []string
	Sequence []KeyStroke
	Items    []PackItem
}

// Entries builds the menu the bar shows for this pack. generic is the
// shell's menu for every window; windowItems are its window actions.
// Both are already bound to one window. An empty result means the pack
// adds nothing of its own, and the caller uses generic.
func (p Pack) Entries(windowID string, pid int, generic, windowItems []Entry) []Entry {
	var out []Entry
	for _, name := range p.Include {
		if name == IncludeGeneric {
			out = append(out, generic...)
		}
	}
	for _, sec := range p.Sections {
		var items []Entry
		if sec.Include == IncludeWindow {
			items = append(items, windowItems...)
		}
		for _, it := range sec.Items {
			items = append(items, it.entry(windowID, pid))
		}
		out = append(out, Entry{Label: sec.Label, Enabled: true, Submenu: true, Children: items})
	}
	return out
}

func (it PackItem) entry(windowID string, pid int) Entry {
	if it.Separator {
		return Entry{Separator: true}
	}
	if it.Disabled {
		return Entry{Label: it.Label}
	}
	e := Entry{Label: it.Label, Enabled: true}
	if len(it.Items) > 0 {
		e.Submenu = true
		for _, child := range it.Items {
			e.Children = append(e.Children, child.entry(windowID, pid))
		}
		return e
	}
	e.Action = Action{Kind: it.Action, Bus: windowID, Index: int32(pid)}
	if len(it.Keys) > 0 {
		mods, key := splitKeys(it.Keys)
		e.Action = Action{Kind: ActionKeys, Bus: windowID, Mods: mods, Key: key, Index: int32(pid)}
	}
	if len(it.Sequence) > 0 {
		e.Action = Action{Kind: ActionKeySequence, Bus: windowID, Sequence: slices.Clone(it.Sequence), Index: int32(pid)}
	}
	return e
}

// splitKeys turns ["CTRL", "SHIFT", "T"] into mods "CTRL SHIFT" and key "T".
// A single letter key is uppercased; modifier names are uppercased; the key
// is otherwise sent as written (Return, Print).
func splitKeys(keys []string) (mods, key string) {
	norm := make([]string, len(keys))
	for i, k := range keys {
		k = strings.TrimSpace(k)
		if i == len(keys)-1 && len([]rune(k)) > 1 {
			norm[i] = k
			continue
		}
		norm[i] = strings.ToUpper(k)
	}
	key = norm[len(norm)-1]
	if len(norm) > 1 {
		mods = strings.Join(norm[:len(norm)-1], " ")
	}
	return mods, key
}

func parseSequenceStroke(raw string) (KeyStroke, error) {
	parts := strings.Split(strings.TrimSpace(raw), "+")
	if len(parts) == 0 {
		return KeyStroke{}, fmt.Errorf("empty stroke")
	}
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
		if parts[i] == "" || strings.ContainsAny(parts[i], " \t") {
			return KeyStroke{}, fmt.Errorf("invalid stroke")
		}
	}
	key := parts[len(parts)-1]
	var mods []string
	for _, mod := range parts[:len(parts)-1] {
		switch strings.ToUpper(mod) {
		case "CTRL", "ALT", "SHIFT", "SUPER":
			mods = append(mods, strings.ToUpper(mod))
		default:
			return KeyStroke{}, fmt.Errorf("unknown modifier %q", mod)
		}
	}
	return KeyStroke{Mods: strings.Join(mods, " "), Key: key}, nil
}

// Packs is the menu packs from the binary and the directories, re-read
// when a file changes. Later directories override a pack with the same id.
type Packs struct {
	mu       sync.Mutex
	devDir   string
	gen      uint64
	stamp    string
	list     []Pack
	problems []string
}

// NewPacks reads the shipped packs and devDir (a checkout's
// share/inua/menus, empty when there is none).
func NewPacks(devDir string) *Packs {
	p := &Packs{devDir: devDir}
	p.refresh()
	return p
}

// Generation is the current reading. It changes when a pack file changes.
func (p *Packs) Generation() uint64 {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refresh()
	return p.gen
}

// Match is the pack for an application, by its window class and its
// desktop id. The later pack wins when several match.
func (p *Packs) Match(appID, desktopID string) (Pack, bool) {
	if p == nil {
		return Pack{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refresh()
	var found Pack
	ok := false
	for _, pack := range p.list {
		if pack.matches(appID, desktopID) {
			found = pack
			ok = true
		}
	}
	return found, ok
}

func (p *Packs) refresh() {
	dirs := packDirs(p.devDir)
	stamp := packStamp(dirs)
	if stamp == p.stamp {
		return
	}
	list, problems := loadPacks(dirs)
	p.list = list
	p.problems = problems
	p.stamp = stamp
	p.gen++
	for _, problem := range problems {
		log.Printf("appmenu: %s", problem)
	}
}

// packDirs is the on-disk packs, lowest priority first. The shipped
// packs are read separately and come before these.
func packDirs(devDir string) []string {
	var dirs []string
	dataDirs := filepath.SplitList(os.Getenv("XDG_DATA_DIRS"))
	if len(dataDirs) == 0 {
		dataDirs = []string{"/usr/local/share", "/usr/share"}
	}
	for i := len(dataDirs) - 1; i >= 0; i-- {
		if dataDirs[i] != "" {
			dirs = append(dirs, filepath.Join(dataDirs[i], "inua", "menus"))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		dataHome := os.Getenv("XDG_DATA_HOME")
		if dataHome == "" {
			dataHome = filepath.Join(home, ".local", "share")
		}
		dirs = append(dirs, filepath.Join(dataHome, "inua", "menus"))
		configHome := os.Getenv("XDG_CONFIG_HOME")
		if configHome == "" {
			configHome = filepath.Join(home, ".config")
		}
		if devDir != "" {
			dirs = append(dirs, devDir)
		}
		dirs = append(dirs, filepath.Join(configHome, "inua", "menus"))
		return dirs
	}
	if devDir != "" {
		dirs = append(dirs, devDir)
	}
	return dirs
}

func packStamp(dirs []string) string {
	var b strings.Builder
	for _, dir := range dirs {
		b.WriteString(dir)
		b.WriteByte(0)
		entries, err := os.ReadDir(dir)
		if err != nil {
			b.WriteString(err.Error())
			b.WriteByte(0)
			continue
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".toml") {
				names = append(names, e.Name())
			}
		}
		slices.Sort(names)
		for _, name := range names {
			info, err := os.Stat(filepath.Join(dir, name))
			fmt.Fprintf(&b, "%s %d %d\n", name, infoSize(info, err), infoMod(info, err))
		}
	}
	return b.String()
}

func infoSize(info os.FileInfo, err error) int64 {
	if err != nil || info == nil {
		return -1
	}
	return info.Size()
}

func infoMod(info os.FileInfo, err error) int64 {
	if err != nil || info == nil {
		return 0
	}
	return info.ModTime().UnixNano()
}

func loadPacks(dirs []string) ([]Pack, []string) {
	var list []Pack
	var problems []string
	shipped, err := readFS(menufiles.Files, "shipped")
	if err != nil {
		problems = append(problems, err.Error())
	}
	for _, pack := range shipped {
		list = upsertPack(list, pack)
	}
	for _, dir := range dirs {
		packs, probs := readDir(dir)
		problems = append(problems, probs...)
		for _, pack := range packs {
			list = upsertPack(list, pack)
		}
	}
	return list, problems
}

func upsertPack(list []Pack, p Pack) []Pack {
	for i := range list {
		if list[i].ID == p.ID {
			return append(append(list[:i], list[i+1:]...), p)
		}
	}
	return append(list, p)
}

func readDir(dir string) ([]Pack, []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []string{fmt.Sprintf("%s: %v", dir, err)}
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".toml") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	var packs []Pack
	var problems []string
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		pack, err := parsePack(path, data)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		packs = append(packs, pack)
	}
	return packs, problems
}

func readFS(fsys fs.FS, origin string) ([]Pack, error) {
	names, err := fs.Glob(fsys, "*.toml")
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	var packs []Pack
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", origin, name, err)
		}
		pack, err := parsePack(origin+"/"+name, data)
		if err != nil {
			return nil, err
		}
		packs = append(packs, pack)
	}
	return packs, nil
}

type packFile struct {
	ID         string        `toml:"id"`
	Match      []string      `toml:"match"`
	Suffix     []string      `toml:"suffix"`
	Contains   []string      `toml:"contains"`
	When       string        `toml:"when"`
	SkipA11y   bool          `toml:"skip_a11y"`
	NeverPress bool          `toml:"never_press"`
	Include    []string      `toml:"include"`
	Sections   []sectionFile `toml:"section"`
}

type sectionFile struct {
	Label   string     `toml:"label"`
	Include string     `toml:"include"`
	Items   []itemFile `toml:"item"`
}

type itemFile struct {
	Label     string     `toml:"label"`
	Separator bool       `toml:"separator"`
	Action    string     `toml:"action"`
	Keys      []string   `toml:"keys"`
	Sequence  []string   `toml:"sequence"`
	Disabled  bool       `toml:"disabled"`
	Items     []itemFile `toml:"item"`
}

func parsePack(name string, data []byte) (Pack, error) {
	if len(data) > maxPackBytes {
		return Pack{}, fmt.Errorf("%s: menu pack is larger than %d bytes", name, maxPackBytes)
	}
	var f packFile
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return Pack{}, fmt.Errorf("%s: %w", name, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return Pack{}, fmt.Errorf("%s: unknown %s", name, undecoded[0].String())
	}
	if err := validatePack(name, f); err != nil {
		return Pack{}, err
	}
	when := f.When
	if when == "" {
		when = PackFallback
	}
	p := Pack{
		ID:         f.ID,
		Match:      f.Match,
		Suffix:     f.Suffix,
		Contains:   f.Contains,
		When:       when,
		SkipA11y:   f.SkipA11y,
		NeverPress: f.NeverPress,
		Include:    f.Include,
	}
	for _, sec := range f.Sections {
		ps := PackSection{Label: sec.Label, Include: sec.Include}
		for _, it := range sec.Items {
			ps.Items = append(ps.Items, convertItem(it))
		}
		p.Sections = append(p.Sections, ps)
	}
	return p, nil
}

func convertItem(it itemFile) PackItem {
	out := PackItem{Label: it.Label, Separator: it.Separator, Disabled: it.Disabled, Action: it.Action, Keys: it.Keys}
	for _, raw := range it.Sequence {
		step, _ := parseSequenceStroke(raw) // validatePack has already checked it
		out.Sequence = append(out.Sequence, step)
	}
	for _, child := range it.Items {
		out.Items = append(out.Items, convertItem(child))
	}
	return out
}

func validatePack(name string, f packFile) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%s: "+format, append([]any{name}, args...)...)
	}
	if strings.TrimSpace(f.ID) == "" {
		return fail("id is empty")
	}
	if len(f.Match) == 0 && len(f.Suffix) == 0 && len(f.Contains) == 0 {
		return fail("match, suffix or contains is required")
	}
	for _, group := range [][]string{f.Match, f.Suffix, f.Contains} {
		for _, token := range group {
			if strings.TrimSpace(token) == "" {
				return fail("empty match token")
			}
		}
	}
	switch f.When {
	case "", PackFallback, PackReplace:
	default:
		return fail("when is %q, want fallback or replace", f.When)
	}
	for _, inc := range f.Include {
		if inc != IncludeGeneric {
			return fail("include %q is unknown", inc)
		}
	}
	for _, sec := range f.Sections {
		if strings.TrimSpace(sec.Label) == "" {
			return fail("a section has no label")
		}
		switch sec.Include {
		case "", IncludeWindow:
		default:
			return fail("section %q include %q is unknown", sec.Label, sec.Include)
		}
		if sec.Include == "" && len(sec.Items) == 0 {
			return fail("section %q has no items", sec.Label)
		}
		for _, it := range sec.Items {
			if err := validateItem(name, sec.Label, it, 1); err != nil {
				return err
			}
		}
	}
	return nil
}

var shellPackActions = map[string]bool{
	ActionAbout:       true,
	ActionCloseWindow: true,
	ActionQuitApp:     true,
	ActionFullscreen:  true,
	ActionFloat:       true,
	ActionCenter:      true,
	ActionCopyTitle:   true,
	ActionCopyClass:   true,
}

func validateItem(name, section string, it itemFile, depth int) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%s: section %q: "+format, append([]any{name, section}, args...)...)
	}
	if depth > maxPackDepth {
		return fail("menu is nested more than %d deep", maxPackDepth)
	}
	if it.Separator {
		if it.Label != "" || it.Disabled || it.Action != "" || len(it.Keys) > 0 || len(it.Sequence) > 0 || len(it.Items) > 0 {
			return fail("a separator has other fields")
		}
		return nil
	}
	if strings.TrimSpace(it.Label) == "" {
		return fail("an item has no label")
	}
	if it.Disabled {
		if it.Action != "" || len(it.Keys) > 0 || len(it.Sequence) > 0 || len(it.Items) > 0 {
			return fail("%q is disabled and also has an action", it.Label)
		}
		return nil
	}
	n := 0
	if it.Action != "" {
		n++
	}
	if len(it.Keys) > 0 {
		n++
	}
	if len(it.Sequence) > 0 {
		n++
	}
	if len(it.Items) > 0 {
		n++
	}
	if n != 1 {
		return fail("%q must have one of action, keys or a submenu", it.Label)
	}
	if it.Action != "" && !shellPackActions[it.Action] {
		return fail("%q action %q is unknown", it.Label, it.Action)
	}
	for _, key := range it.Keys {
		if strings.TrimSpace(key) == "" || strings.ContainsAny(key, " \t") {
			return fail("%q has an empty key", it.Label)
		}
	}
	for _, step := range it.Sequence {
		if _, err := parseSequenceStroke(step); err != nil {
			return fail("%q sequence stroke %q: %v", it.Label, step, err)
		}
	}
	if len(it.Sequence) == 1 {
		return fail("%q sequence needs at least two strokes", it.Label)
	}
	for _, child := range it.Items {
		if err := validateItem(name, section, child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (p Pack) matches(appID, desktopID string) bool {
	ids := []string{strings.ToLower(appID), strings.ToLower(desktopID)}
	for _, token := range p.Match {
		token = strings.ToLower(token)
		for _, id := range ids {
			if id != "" && id == token {
				return true
			}
		}
	}
	for _, token := range p.Suffix {
		token = strings.ToLower(token)
		for _, id := range ids {
			if id != "" && strings.HasSuffix(id, token) {
				return true
			}
		}
	}
	for _, token := range p.Contains {
		token = strings.ToLower(token)
		for _, id := range ids {
			if id != "" && strings.Contains(id, token) {
				return true
			}
		}
	}
	return false
}
