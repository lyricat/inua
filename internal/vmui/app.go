// Package vmui is inua-vm's window and its command line: the machines, their
// list and the page of the machine chosen (docs/inua-vm.md). The logic, the
// host checks and the backends are internal/vm; this is the view and the thin
// layer over it. cmd/inua-vm runs the same pieces without a display for
// --list, --doctor, --status, --start and --stop.
package vmui

import (
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/i18n"
)

// App is the manager: one window, the machines kept in it.
type App struct{}

// Register makes the manager a light app of its own, as inua-files and
// inua-viewer are: inua-vm opens it, and a second start brings its window
// forward.
func Register() {
	appui.Register("vm", "VM", i18n.M("Virtual machines"), func() appui.View { return &App{} })
}

// Build makes the window and the service behind it.
func (a *App) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	service := newService()
	w := openWindow(c, service)
	return w.win
}

// ThemeChanged is empty: the window is rebuilt by appui when the theme
// changes, and everything it draws comes from the layout and the style.
func (a *App) ThemeChanged() {}

// Start brings a machine up: `inua-vm --start NAME`, for scripts and for a
// session that wants a guest running before anyone opens its window.
func Start(name string) error {
	s := newService()
	defer s.close()
	return s.Start(name)
}

// Stop shuts a machine down, keeping its disk: `inua-vm --stop NAME`.
func Stop(name string) error {
	s := newService()
	defer s.close()
	return s.Stop(name)
}

// List is the machines' names, one per line: `inua-vm --list`.
func List() ([]string, error) {
	s := newService()
	defer s.close()
	return s.Names(), nil
}

// Doctor is what this host provides, in the window's own checks: `inua-vm
// --doctor` prints the fuller report from internal/vm instead, and this is what
// the shell's thin command uses.
func Doctor() (string, error) {
	s := newService()
	defer s.close()
	return s.Doctor(), nil
}

// Status is what the machines are doing now: `inua-vm --status`.
func Status() StatusReport {
	s := newService()
	defer s.close()
	return s.Status()
}
