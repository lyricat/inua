package ui

import (
	"context"
	"log"
	"slices"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hyprconf"
	"github.com/lyricat/inua/internal/i18n"
)

// The settings window's Shortcuts page. Shortcuts set here are saved in
// config.toml and written to hypr/inua.conf (hyprconf.renderShortcuts);
// the others stay with hyprland.conf, and the page shows them from
// Hyprland's own list (hyprctl binds).

// recordMS is how long the window waits for a shortcut before giving up,
// so Hyprland never stays in the empty submap.
const recordMS = 15000

// shortcutState is the page's part of the settings window.
type shortcutState struct {
	inua, shots, custom, hypr gtk.Box
	recordingNote, warn       gtk.Label
	newName, newCmd           gtk.Entry
	binds                     []hyprconf.Bind // Hyprland's shortcuts
	recording                 string          // the action or "exec:N" being recorded, "" if none
	recordGen                 uintptr
}

// hyprBinds asks Hyprland for its shortcuts.
func (s *shellUI) hyprBinds() []hyprconf.Bind {
	if s.hypr == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	reply, err := s.hypr.Client.Request(ctx, "j/binds")
	if err != nil {
		log.Printf("hyprland: binds: %v", err)
		return nil
	}
	binds, err := hyprconf.ParseBinds(reply)
	if err != nil {
		log.Printf("hyprland: binds: %v", err)
	}
	return binds
}

// shortcutIndex is the shortcut inua sets for an action, -1 if none.
func shortcutIndex(list []config.Shortcut, action string) int {
	return slices.IndexFunc(list, func(sc config.Shortcut) bool { return sc.Action == action })
}

// showShortcuts fills the page.
func (s *shellUI) showShortcuts() {
	sc := &s.settings.sc
	gtkx.Clear(&sc.inua)
	gtkx.Clear(&sc.shots)
	gtkx.Clear(&sc.custom)
	gtkx.Clear(&sc.hypr)
	c := s.cfg
	mine := map[string]bool{} // keys inua binds; Hyprland lists them too
	for _, x := range c.Shortcuts {
		if x.Keys != "" {
			mine[strings.ToLower(x.Keys)] = true
		}
	}
	commands := map[string]bool{}
	for _, a := range hyprconf.Actions {
		cmd := hyprconf.Command(c, a.ID)
		commands[strings.Join(strings.Fields(cmd), " ")] = true
		var keys []string
		source := ""
		managed := false
		if i := shortcutIndex(c.Shortcuts, a.ID); i >= 0 {
			managed = true
			if k := c.Shortcuts[i].Keys; k != "" {
				keys, source = []string{k}, i18n.T("Set in inua")
			} else {
				source = i18n.T("Turned off in inua")
			}
		} else if keys = hyprconf.KeysFor(sc.binds, cmd, a.Label); len(keys) > 0 {
			source = i18n.T("From Hyprland")
		}
		box := &sc.inua
		if a.Shot {
			box = &sc.shots
		}
		s.shortcutRow(box, a.ID, i18n.T(a.Label), source, keys, managed, false)
	}
	for i, x := range c.Shortcuts {
		if x.Action != "exec" {
			continue
		}
		var keys []string
		if x.Keys != "" {
			keys = []string{x.Keys}
		}
		commands[strings.Join(strings.Fields(x.Command), " ")] = true
		s.shortcutRow(&sc.custom, "exec:"+strconv.Itoa(i), cmpOrStr(x.Name, x.Command), x.Command, keys, true, true)
	}
	n := 0
	for _, b := range sc.binds {
		if b.Submap != "" || mine[strings.ToLower(b.Keys)] ||
			(b.Dispatcher == "exec" && commands[strings.Join(strings.Fields(b.Arg), " ")]) {
			continue
		}
		// inua.lua's binds, described as the action they run.
		if b.Description != "" && slices.ContainsFunc(hyprconf.Actions, func(a hyprconf.Action) bool { return strings.EqualFold(b.Description, a.Label) }) {
			continue
		}
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		row.AddCssClass("row")
		row.AddCssClass("compact")
		col := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		col.SetSizeRequest(190, -1)
		gtkx.Append(col, &keycap(b.Keys).Widget)
		gtkx.Append(row, &col.Widget)
		what := b.Dispatcher
		if b.Description != "" {
			what = b.Description
		} else if b.Arg != "" {
			if b.Dispatcher == "__lua" { // a Lua function: its code is not shown
				what = i18n.T("Lua function")
			} else {
				what += "  " + b.Arg
			}
		}
		l := gtk.NewLabel(what)
		l.AddCssClass("readout")
		l.SetXalign(0)
		l.SetHexpand(true)
		l.SetEllipsize(3) // end
		l.SetTooltipText(what)
		gtkx.Append(row, &l.Widget)
		gtkx.Append(&sc.hypr, &row.Widget)
		n++
	}
	if n == 0 {
		l := gtk.NewLabel(i18n.T("None"))
		l.AddCssClass("sub")
		l.AddCssClass("row")
		l.SetXalign(0)
		gtkx.Append(&sc.hypr, &l.Widget)
	}
	sc.recordingNote.SetVisible(sc.recording != "")
}

// shortcutRow is one action: its name and where its shortcut comes from,
// then its keys, which are the button that records new ones, then a slot
// of fixed width for turning it off, handing it back to hyprland.conf, or
// (for a command) removing it, so the rows line up.
func (s *shellUI) shortcutRow(box *gtk.Box, id, title, sub string, keys []string, managed, custom bool) {
	sc := &s.settings.sc
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	row.AddCssClass("row")
	row.AddCssClass("shortcut")
	text := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	text.SetHexpand(true)
	text.SetValign(gtk.AlignCenterValue)
	t := gtk.NewLabel(title)
	t.AddCssClass("title")
	t.SetXalign(0)
	t.SetEllipsize(3)
	gtkx.Append(text, &t.Widget)
	d := gtk.NewLabel(sub)
	d.AddCssClass("sub")
	if custom {
		d.AddCssClass("mono-sub")
	}
	d.SetXalign(0)
	d.SetEllipsize(3)
	d.SetTooltipText(sub)
	d.SetVisible(sub != "")
	gtkx.Append(text, &d.Widget)
	gtkx.Append(row, &text.Widget)

	rec := gtk.NewButton()
	rec.AddCssClass("keys")
	rec.SetName("sc-rec:" + id)
	rec.SetValign(gtk.AlignCenterValue)
	rec.SetTooltipText(i18n.T("Click, then press the new shortcut"))
	caps := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
	caps.SetHalign(gtk.AlignEndValue)
	switch {
	case sc.recording == id:
		l := keycapText(i18n.T("Press keys…"))
		l.AddCssClass("waiting")
		gtkx.Append(caps, &l.Widget)
	case len(keys) == 0:
		l := keycapText(i18n.T("Not set"))
		l.AddCssClass("unset")
		gtkx.Append(caps, &l.Widget)
	default:
		for _, k := range keys {
			gtkx.Append(caps, &keycap(k).Widget)
		}
	}
	rec.SetChild(&caps.Widget)
	caps.Unref()
	rec.ConnectClicked(&s.onSettingsButton)
	gtkx.Append(row, &rec.Widget)

	slot := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	slot.AddCssClass("slot")
	slot.SetValign(gtk.AlignCenterValue)
	icon := func(name, iconName, tip string) {
		b := gtk.NewButtonFromIconName(iconName)
		b.AddCssClass("plain")
		b.SetName(name)
		b.SetTooltipText(tip)
		b.ConnectClicked(&s.onSettingsButton)
		gtkx.Append(slot, &b.Widget)
	}
	switch {
	case custom:
		icon("sc-del:"+id, "window-close-symbolic", i18n.T("Remove this command"))
	case managed:
		icon("sc-reset:"+id, "edit-undo-symbolic", i18n.T("Use hyprland.conf's shortcut again"))
	case len(keys) > 0:
		icon("sc-off:"+id, "window-close-symbolic", i18n.T("Turn the shortcut off"))
	}
	gtkx.Append(row, &slot.Widget)
	gtkx.Append(box, &row.Widget)
}

// keycap shows keys as a mono label in a hairline box.
func keycap(keys string) *gtk.Label { return keycapText(hyprconf.Label(keys)) }

func keycapText(text string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.AddCssClass("keycap")
	l.SetValign(gtk.AlignCenterValue)
	return l
}

func cmpOrStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// shortcutButton handles the page's buttons; it reports whether name was
// one of them.
func (s *shellUI) shortcutButton(name string) bool {
	kind, id, ok := strings.Cut(name, ":")
	c := s.cfg
	switch {
	case name == "sc_add":
		st := &s.settings.sc
		cmd := strings.TrimSpace(st.newCmd.GetText())
		if cmd == "" || strings.ContainsAny(cmd, "\n\r") {
			st.newCmd.AddCssClass("invalid")
			return true
		}
		st.newCmd.RemoveCssClass("invalid")
		c.Shortcuts = append(slices.Clone(c.Shortcuts), config.Shortcut{Action: "exec", Name: strings.TrimSpace(st.newName.GetText()), Command: cmd})
		st.newName.SetText("")
		st.newCmd.SetText("")
		s.changeSettings(c)
		s.startRecording("exec:" + strconv.Itoa(len(c.Shortcuts)-1))
	case !ok:
		return false
	case kind == "sc-rec":
		s.startRecording(id)
	case kind == "sc-off":
		s.setShortcut(id, "")
	case kind == "sc-reset":
		if i := shortcutIndex(c.Shortcuts, id); i >= 0 {
			c.Shortcuts = slices.Delete(slices.Clone(c.Shortcuts), i, i+1)
			s.changeSettings(c)
		}
	case kind == "sc-del":
		if i, err := strconv.Atoi(strings.TrimPrefix(id, "exec:")); err == nil && i < len(c.Shortcuts) {
			c.Shortcuts = slices.Delete(slices.Clone(c.Shortcuts), i, i+1)
			s.changeSettings(c)
		}
	default:
		return false
	}
	return true
}

// setShortcut gives an action (or "exec:N") new keys, "" for off. The
// first time inua sets an action, the keys it had in hyprland.conf are
// recorded to be unbound. Another inua shortcut on the same keys loses
// them.
func (s *shellUI) setShortcut(id, keys string) {
	c := s.cfg
	list := slices.Clone(c.Shortcuts)
	at := -1
	if n, ok := strings.CutPrefix(id, "exec:"); ok {
		if i, err := strconv.Atoi(n); err == nil && i < len(list) {
			at = i
		}
	} else if at = shortcutIndex(list, id); at < 0 {
		label := ""
		if i := slices.IndexFunc(hyprconf.Actions, func(a hyprconf.Action) bool { return a.ID == id }); i >= 0 {
			label = hyprconf.Actions[i].Label
		}
		list = append(list, config.Shortcut{Action: id, Replaces: hyprconf.KeysFor(s.settings.sc.binds, hyprconf.Command(c, id), label)})
		at = len(list) - 1
	}
	if at < 0 {
		return
	}
	if keys != "" {
		for i := range list {
			if i != at && hyprconf.SameKeys(list[i].Keys, keys) {
				list[i].Keys = ""
			}
		}
	}
	list[at].Keys = keys
	c.Shortcuts = list
	s.changeSettings(c)
}

// startRecording waits for the next key press in the settings window.
// Hyprland goes into an empty submap meanwhile, so shortcuts already bound
// reach the window too.
func (s *shellUI) startRecording(id string) {
	sc := &s.settings.sc
	if sc.recording != "" {
		s.stopRecording()
	}
	sc.recording = id
	s.dispatchSubmap(hyprconf.RecordSubmap)
	sc.recordGen++
	glib.TimeoutAdd(recordMS, &s.onRecordTimeout, sc.recordGen)
	s.showShortcuts()
}

func (s *shellUI) stopRecording() {
	sc := &s.settings.sc
	if sc.recording == "" {
		return
	}
	sc.recording = ""
	s.dispatchSubmap("reset")
	if s.settings.win != nil {
		s.showShortcuts()
	}
}

// recordKey takes a key press while recording; it reports whether it used
// it.
func (s *shellUI) recordKey(keyval, keycode uint32, state gdk.ModifierType) bool {
	sc := &s.settings.sc
	if sc.recording == "" {
		return false
	}
	name := gdk.KeyvalName(gdk.KeyvalToLower(keyval))
	switch {
	case isModifierKey(name):
		return true // wait for the key itself
	}
	mods := state & (gdk.ShiftMaskValue | gdk.ControlMaskValue | gdk.AltMaskValue | gdk.SuperMaskValue)
	switch {
	case mods == 0 && name == "Escape":
		s.stopRecording()
		return true
	case mods == 0 && name == "BackSpace":
		id := sc.recording
		s.stopRecording()
		s.setShortcut(id, "")
		return true
	}
	// The key as it is without modifiers: Shift+1 is SHIFT and 1, not
	// "exclam".
	var plain uint32
	var group, level int32
	var consumed gdk.ModifierType
	if gdk.DisplayGetDefault().TranslateKey(keycode, 0, 0, &plain, &group, &level, &consumed) && plain != 0 {
		name = gdk.KeyvalName(gdk.KeyvalToLower(plain))
	}
	keys := hyprconf.Keys(mods&gdk.SuperMaskValue != 0, mods&gdk.ControlMaskValue != 0,
		mods&gdk.AltMaskValue != 0, mods&gdk.ShiftMaskValue != 0, name)
	id := sc.recording
	s.stopRecording()
	if config.ValidKeys(keys) {
		s.setShortcut(id, keys)
	}
	return true
}

func isModifierKey(name string) bool {
	for _, p := range []string{"Shift_", "Control_", "Alt_", "Super_", "Meta_", "Hyper_", "ISO_Level", "Caps_Lock", "Num_Lock"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
