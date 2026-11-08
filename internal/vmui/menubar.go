package vmui

import (
	"codeberg.org/puregotk/puregotk/v4/gio"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/vm"
)

// menuBar publishes the VM manager's commands in GTK's application menu model.
func (v *vmWindow) menuBar() *appui.MenuBar {
	activate := func(action gio.SimpleAction, _ uintptr) {
		switch action.GetName() {
		case "new-machine":
			v.showNew()
		case "close-window":
			v.win.Close()
		case "start":
			v.menuStart()
		case "stop":
			v.menuStop()
		case "install":
			v.menuInstall()
		case "remove":
			v.menuRemove()
		case "refresh":
			v.recheck()
		default:
		}
	}
	action := func(name, label string) appui.MenuAction {
		return appui.MenuAction{Name: name, Label: label, Activate: activate}
	}
	groups := []appui.MenuGroup{
		{Title: i18n.T("File"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("new-machine", i18n.T("New Machine…")),
		}}, {Actions: []appui.MenuAction{
			action("close-window", i18n.T("Close Window")),
		}}}},
		{Title: i18n.T("Machine"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("start", i18n.T("Start")),
			action("stop", i18n.T("Stop")),
		}}, {Actions: []appui.MenuAction{
			action("install", i18n.T("Install")),
			action("remove", i18n.T("Remove…")),
		}}}},
		{Title: i18n.T("View"), Sections: []appui.MenuSection{{Actions: []appui.MenuAction{
			action("refresh", i18n.T("Refresh")),
		}}}},
	}
	return appui.NewMenuBar(v.c, groups)
}

// installMenuBar publishes the window's menu for the bar. The menu is
// served over D-Bus; the window holds the bar so it lives as long as it.
func (v *vmWindow) installMenuBar() {
	v.menu = v.menuBar()
}

func (v *vmWindow) menuStart() {
	name := v.selectedName()
	if name == "" || v.closed || v.operationBusy {
		return
	}
	view := v.viewOf(name)
	at, btn := &v.startError, &v.start
	switch view.Page {
	case vm.PageProvisioning:
		at, btn = &v.provError, &v.provStart
	}
	v.run(func() error { return v.manager.Start(name) }, at, btn, i18n.T("Starting…"))
}

func (v *vmWindow) menuStop() {
	name := v.selectedName()
	if name == "" || v.closed || v.operationBusy {
		return
	}
	view := v.viewOf(name)
	at, btn := &v.runError, &v.stop
	switch view.Page {
	case vm.PageProvisioning:
		at, btn = &v.provError, &v.provStop
	case vm.PageStarting:
		at, btn = &v.bootError, &v.bootStop
	}
	v.run(func() error { return v.manager.Stop(name) }, at, btn, i18n.T("Stopping…"))
}

func (v *vmWindow) menuInstall() {
	name := v.selectedName()
	if name == "" || v.closed || v.operationBusy {
		return
	}
	view := v.viewOf(name)
	if view.Page != vm.PageSetup {
		return
	}
	m := v.setupMachine()
	v.run(func() error { return v.manager.Install(m) }, &v.setupError, &v.install, i18n.T("Starting…"))
}

func (v *vmWindow) menuRemove() {
	name := v.selectedName()
	if name == "" || v.closed || v.operationBusy {
		return
	}
	view := v.viewOf(name)
	switch view.Page {
	case vm.PageSetup:
		if view.Leftovers {
			v.confirm(i18n.T("Delete the files from the earlier attempt?"), name,
				i18n.T("The downloaded files and the unfinished disk are deleted for good."),
				i18n.T("Delete"), true, func() {
					v.run(func() error { return v.manager.DeleteDisk(name) }, &v.setupRemoveError, &v.setupRemoveData, i18n.T("Deleting…"))
				})
		}
	case vm.PageProvisioning:
		if view.Machine.Backend == vm.BackendDockur {
			v.confirm(i18n.T("Abandon the installation?"), name,
				i18n.T("The container is removed. Downloaded files stay, so a new installation does not fetch them again."),
				i18n.T("Abandon"), false, func() {
					v.run(func() error { return v.manager.Remove(name) }, &v.provError, &v.provRemove, i18n.T("Removing…"))
				})
			return
		}
		v.confirm(i18n.T("Start the installation over?"), name,
			i18n.T("The unfinished disk is deleted for good; the machine stays, to install again."),
			i18n.T("Start over"), true, func() {
				v.run(func() error { return v.manager.DeleteDisk(name) }, &v.provError, &v.provRemove, i18n.T("Deleting…"))
			})
	case vm.PageStopped:
		v.confirm(i18n.T("Remove the container?"), name,
			i18n.T("Its container and settings go. The disk and its password stay, so installing again starts from them."),
			i18n.T("Remove"), false, func() {
				v.run(func() error { return v.manager.Remove(name) }, &v.removeError, &v.remove, i18n.T("Removing…"))
			})
	default:
	}
}
