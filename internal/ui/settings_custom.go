package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/uikit"
)

// Themes of the user's own, on the Appearance page: the last swatch adds
// one, copying a file beside config.toml and choosing it; a right click on
// one of yours opens Show in Folder and Move to Trash. (Sets of line
// icons of the user's are folders in icons beside it, chosen in the menu.)

// customButton handles the theme tile that adds one.
func (s *shellUI) customButton(name string) bool {
	switch name {
	case "theme_add":
		s.chooseTheme()
	default:
		return false
	}
	return true
}

// themeMenu opens the menu of a theme of the user's, at the right click
// on its swatch.
func (s *shellUI) themeMenu(btn *gtk.Widget, x, y float64) {
	st := &s.settings
	st.themeMenu = strings.TrimPrefix(btn.GetName(), "theme:")
	pop := uikit.NewMenu([]uikit.Item{
		{Label: i18n.T("Show in Folder"), Run: func() { s.themeAction("folder") }},
		{Label: i18n.T("Move to Trash"), Run: func() { s.themeAction("trash") }},
	}, uikit.AtPointer)
	pop.SetParent(btn)
	pop.SetPointingTo(&gdk.Rectangle{X: int32(x), Y: int32(y), Width: 1, Height: 1})
	pop.ConnectClosed(&s.onThemeMenuClosed)
	pop.Unref() // the swatch holds it
	pop.Popup()
}

// themeAction does what a theme's menu chose: shows its file, or moves it
// to the trash, the default taking its place where it was in use.
func (s *shellUI) themeAction(action string) {
	c := s.cfg
	name := s.settings.themeMenu
	if !c.IsCustomTheme(name) {
		return
	}
	switch action {
	case "folder":
		go revealFolder(filepath.Join(c.Dir, "themes"), c.ThemePath(name))
	case "trash":
		if !s.trash(c.ThemePath(name)) {
			return
		}
		if c.Style.AppTheme == name {
			c.Style.AppTheme = ""
		}
		if c.Style.Theme == name {
			c.Style.Theme = config.Default().Style.Theme
		}
		s.changeSettings(c)
	}
}

// revealFolder shows a folder, made if missing, in the file manager, with
// the item in use selected when there is one.
func revealFolder(dir, item string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	if item != "" {
		if _, err := os.Stat(item); err == nil {
			revealFile(item)
			return
		}
	}
	showInFileManager("ShowFolders", dir)
}

// trash moves a theme file or an icon set folder to the trash, so it can
// be brought back.
func (s *shellUI) trash(path string) bool {
	f := gio.FileNewForPath(path)
	defer unref(f)
	if _, err := f.Trash(nil); err != nil {
		s.setStatus(i18n.Tf("Not moved to the trash: %s", err.Error()), true)
		return false
	}
	return true
}

// chooseTheme asks for a theme file with the portal's (or GTK's) file
// chooser; onCustomChosen takes the answer.
func (s *shellUI) chooseTheme() {
	st := &s.settings
	if st.dialog != nil {
		return
	}
	if s.onCustomChosen == nil {
		s.onCustomChosen = func(_, result, _ uintptr) { s.themeChosen(result) }
	}
	d := gtk.NewFileDialog()
	if home, err := os.UserHomeDir(); err == nil {
		f := gio.FileNewForPath(home)
		d.SetInitialFolder(f)
		unref(f)
	}
	st.dialog = d
	d.SetTitle(i18n.T("Choose a theme"))
	filter := gtk.NewFileFilter()
	filter.SetName(i18n.T("Themes (CSS)"))
	filter.AddSuffix("css")
	d.SetDefaultFilter(filter)
	filter.Unref()
	d.Open(&st.win.Window, nil, &s.onCustomChosen, 0)
}

// themeChosen installs the theme chosen and puts it in use; its swatch
// shows it.
func (s *shellUI) themeChosen(result uintptr) {
	st := &s.settings
	d := st.dialog
	st.dialog = nil
	if d == nil {
		return
	}
	defer d.Unref()
	file, err := d.OpenFinish(&gio.AsyncResultBase{Ptr: result})
	if err != nil || file == nil || st.win == nil {
		return // cancelled, or the window closed meanwhile
	}
	path := file.GetPath()
	unref(file)
	if path == "" {
		return
	}
	c := s.cfg
	name, err := c.InstallTheme(path)
	if err != nil {
		s.setStatus(installError(name, err), true)
		return
	}
	c.Style.Theme = name
	s.changeSettings(c)
}

func installError(name string, err error) string {
	switch {
	case errors.Is(err, config.ErrExists):
		return i18n.Tf("Not added: the name %s is taken; rename the file or folder", name)
	case errors.Is(err, config.ErrNotTheme):
		return i18n.T("Not added: a theme has @define-color lines with #rrggbb colours")
	}
	return i18n.Tf("Not added: %s", err.Error())
}
