package ui

import (
	"slices"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// settingsIcons are the pages' icons in the settings index, by page.
var settingsIcons = map[string]string{
	"appearance":    "preferences-desktop-appearance-symbolic",
	"bar":           "user-desktop-symbolic",
	"launcher":      "system-run-symbolic",
	"apps":          "view-app-grid-symbolic",
	"ai":            "applications-science-symbolic",
	"notifications": "preferences-system-notifications-symbolic",
	"clipboard":     "edit-paste-symbolic",
	"keyring":       "dialog-password-symbolic",
	"shortcuts":     "preferences-desktop-keyboard-shortcuts-symbolic",
	"wifi":          "network-wireless-signal-excellent-symbolic",
	"bluetooth":     "bluetooth-active-symbolic",
	"displays":      "video-display-symbolic",
	"keyboard":      "input-keyboard-symbolic",
	"mouse":         "input-mouse-symbolic",
	"power":         "battery-level-80-symbolic",
	"about":         "help-about-symbolic",
	"voice":         "audio-input-microphone-symbolic",
}

// buildSettingsIndex lists the pages beside them, each its icon and
// title under its group's kicker (settingsGroups), the page shown
// selected; choosing one shows it. Rows are known by their page's name.
func (s *shellUI) buildSettingsIndex() {
	st := &s.settings
	// The kicker over each group's first page, set by the list's header
	// function as it lays the rows out.
	heads := map[string]*gtk.Label{}
	for _, g := range settingsGroups {
		k := gtk.NewLabel(i18n.T(g.title))
		k.AddCssClass("kicker")
		k.AddCssClass("index-group")
		k.SetXalign(0)
		heads[g.pages[0]] = k
	}
	for _, name := range settingsPages {
		child := st.pages.GetChildByName(name)
		if child == nil {
			continue
		}
		box := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		img := gtk.NewImageFromIconName(settingsIcons[name])
		img.AddCssClass("index-icon")
		gtkx.Append(box, &img.Widget)
		label := gtk.NewLabel(st.pages.GetPage(child).GetTitle())
		label.SetXalign(0)
		gtkx.Append(box, &label.Widget)
		row := gtk.NewListBoxRow()
		row.SetName(name)
		row.SetChild(&box.Widget)
		st.index.Append(&row.Widget)
	}
	s.onSettingsHeader = func(ptr, _, _ uintptr) {
		row := gtk.ListBoxRowNewFromInternalPtr(ptr)
		if k, ok := heads[row.GetName()]; ok {
			row.SetHeader(&k.Widget)
		}
	}
	s.onSettingsHeaderDone = func(uintptr) {}
	st.index.SetHeaderFunc(&s.onSettingsHeader, 0, &s.onSettingsHeaderDone)
	s.onSettingsIndex = func(_ gtk.ListBox, ptr uintptr) {
		if ptr == 0 {
			return
		}
		name := gtk.ListBoxRowNewFromInternalPtr(ptr).GetName()
		if slices.Contains(settingsPages, name) && st.pages.GetVisibleChildName() != name {
			st.pages.SetVisibleChildName(name)
		}
	}
	st.index.ConnectRowSelected(&s.onSettingsIndex)
	s.selectSettingsIndex()
}

// selectSettingsIndex selects the page shown in the index.
func (s *shellUI) selectSettingsIndex() {
	st := &s.settings
	name := st.pages.GetVisibleChildName()
	for i := int32(0); ; i++ {
		row := st.index.GetRowAtIndex(i)
		if row == nil {
			return
		}
		if row.GetName() == name {
			st.index.SelectRow(row)
			return
		}
	}
}
