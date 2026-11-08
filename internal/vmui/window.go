package vmui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/vm"
)

// VMManager is the UI-facing boundary for the machines: the list, one
// machine's page, and the actions on it. Everything that reads a backend may
// block (a Docker call, a QMP round trip), so the window calls those off the
// GTK thread and paints what comes back.
type VMManager interface {
	// Machines is the list, from what is on disk. It reads no backend.
	Machines() []MachineView
	Faults() []vm.Fault
	DeleteFault(name string) error
	// Snapshot is one machine as it was last observed.
	Snapshot(name string) MachineView
	// Refresh reads one machine's backend and returns its view.
	Refresh(name string) MachineView
	// RefreshAll reads every machine and re-reads the host.
	RefreshAll() []MachineView
	// Templates are what a new machine can be made from.
	Templates() []vm.Template
	// Recommended is what suits this computer, for a new machine.
	Recommended() vm.Profile
	Create(templateID, name string, p vm.Profile) (vm.Machine, error)
	Save(vm.Machine) error
	Install(vm.Machine) error
	FetchISO(name string, media vm.ISOMedia) error
	SetISO(name, path string) error
	CancelISO(name string)
	SetISOFilters(vm.ISOFilters)
	Start(name string) error
	Stop(name string) error
	Connect(name string, display *rdpDisplay) error
	Console(name string) error
	TeeSerialContext(ctx context.Context, name string) error
	Remove(name string) error
	DeleteDisk(name string) error
	Delete(name string) error
	Fix(vm.Remedy) (done string, relogin bool, err error)
}

// machineRow is one tile of the library: what a repaint updates.
type machineRow struct {
	name string
	tile appui.LibraryTile
}

// templateRow is one way a system runs, in the New machine dialog.
type templateRow struct {
	id  string
	os  vm.OS
	row gtk.ListBoxRow
}

// vmWindow is the manager: the machines as tiles in the library, the chosen
// one's page in the panel beside it, and the page that makes a new one.
type vmWindow struct {
	win     *gtk.ApplicationWindow
	manager VMManager
	c       *appui.Ctx // the app, for a notification the window raises

	panel gtk.Box
	head  gtk.Box
	mark  gtk.Box // filled while the chosen machine is up
	title gtk.Label
	sub   gtk.Label

	stack gtk.Stack

	machineList    gtk.FlowBox
	machinesKicker gtk.Label
	faultList      gtk.FlowBox
	faultHeading   gtk.Label
	libraryEmpty   gtk.Box
	newButton      gtk.Button
	emptyNew       gtk.Button
	views          []MachineView
	rows           []machineRow
	faults         []vm.Fault

	// a new machine, asked in a dialog over the window
	newDialog    gtk.Window
	newSteps     gtk.Stack // "what", then "resources"
	newNext      gtk.Button
	newBack      gtk.Button
	createNote   gtk.Label
	newMemory    gtk.SpinButton
	newCPUs      gtk.SpinButton
	newDisk      gtk.SpinButton
	newMemorySub gtk.Label
	newCPUsSub   gtk.Label
	newDiskSub   gtk.Label
	newSystems   gtk.Box
	systems      []gtk.ToggleButton // one a system, named by it
	onSystem     func(gtk.ToggleButton)
	templateList gtk.ListBox
	templateRows []templateRow
	nameEntry    gtk.Entry
	createButton gtk.Button
	newCancel    gtk.Button
	createError  gtk.Label
	templateID   string
	templateName string // the template's own suggested name, to know a name was typed
	onNewClose   func(gtk.Window) bool
	onNewKey     func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool

	// the page of a machine whose backend this build does not have
	naNote gtk.Label

	// a folder that does not load as a machine
	faultPath, faultError gtk.Label
	faultDelete           gtk.Button

	// the serial console of a machine that has one, and its debugger
	consoleOpen                            bool
	consoleBack                            gtk.Button
	consoleSummary, consoleHardware        gtk.Label
	consoleGDB                             gtk.Switch
	consoleCmd                             gtk.Label
	consoleCopy, consoleStart, consoleStop gtk.Button
	serial                                 gtk.TextView
	serialBuffer                           *gtk.TextBuffer
	serialText                             string
	serialCancel                           context.CancelFunc
	onGDB                                  func(gtk.Switch, bool) bool
	onISOSearch                            func(gtk.Entry)
	onISOEntryChanged                      func(ptr, _ uintptr)
	onISODropChanged                       func(ptr, _, _ uintptr)
	onISOFilterSwitch                      func(gtk.Switch, bool) bool
	onISOSystem                            func(gtk.Button)
	filling                                bool // a repaint, not the user, is setting a control

	// the rows that show a guest: how it is seen depends on its backend and
	// display, so Go words them per machine
	provWatchRow, bootWatchRow, runWatchRow       gtk.Box
	provWatchTitle, bootWatchTitle, runWatchTitle gtk.Label
	provWatchNote, bootWatchNote, runWatchNote    gtk.Label
	runConsoleRow                                 gtk.Box

	// prereq
	checkRows                      gtk.Box
	checkCount, fixLabel           gtk.Label
	assistantAsk, assistantRecheck gtk.Button
	assistantNote                  gtk.Label
	assistantHint                  string // what the row says by default

	// setup
	accept, install              gtk.Button
	memory, cpus, disk           gtk.SpinButton
	memorySub, cpusSub, diskSub  gtk.Label
	leftoversRow                 gtk.Box
	setupRemoveData, setupDelete gtk.Button
	setupNote                    gtk.Label

	// the install image of the machine being set up: the file it installs
	// from, the transfer in flight, and the dialog that lists what osinfo-db
	// offers, with the buttons its rows are built with
	setupMedia      gtk.Box
	mediaNote       gtk.Label
	isoChoose       gtk.Button
	isoBrowse       gtk.Button
	isoFile         *gtk.FileDialog // the file chooser while it is open
	onISOFile       gio.AsyncReadyCallback
	isoDialog       gtk.Window
	isoDialogNote   gtk.Label
	isoClose        gtk.Button
	onISOClose      func(gtk.Window) bool
	onISOKey        func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	mediaSource     gtk.Label
	mediaRec        gtk.Box
	mediaRows       gtk.Box
	mediaProgress   gtk.Box
	isoProgress     gtk.ProgressBar
	isoProgressText gtk.Label
	isoCancel       gtk.Button
	isoButtons      []isoTarget
	// The download list's own controls: what it is searched for, the platform
	// and architecture it is limited to, and the two switches that offer what
	// the guide's rule otherwise leaves out. The window owns the choice; the
	// service applies it where a machine's view is built.
	mediaFilters gtk.Box
	isoSearch    gtk.Entry
	isoPlatform  gtk.DropDown
	isoArch      gtk.DropDown
	isoPre       gtk.Switch
	isoEOL       gtk.Switch
	mediaCount   gtk.Label

	isoFilter      vm.ISOFilters             // what the controls say
	isoSystems     []ISOSystemRow            // the systems as they were built
	isoRecommended []ISOSystemRow            // the recommended ones, as they were built
	isoOpen        map[string]bool           // the systems the user opened
	isoArchs       []string                  // the architecture families the catalog holds
	isoRevealer    map[string][]gtk.Revealer // a system's, in each list it is in
	isoMark        map[string][]gtk.Image

	// provisioning
	progress                        gtk.ProgressBar
	progressMeta, progressText      gtk.Label
	console, provStart, provStop    gtk.Button
	provRemove, provDelete          gtk.Button
	provStopRow, provStartRow       gtk.Box
	provRemoveRow, provDeleteRow    gtk.Box
	provRemoveTitle, provRemoveNote gtk.Label
	provDoneRow                     gtk.Box
	provDone                        gtk.Button

	// stopped
	readySummary                      gtk.Label
	readyMemory                       gtk.SpinButton
	readyCPUs                         gtk.SpinButton
	diskSize                          gtk.Label
	readySave                         gtk.Button
	start, remove, removeData, delete gtk.Button
	removeRow                         gtk.Box

	// starting
	bootSummary      gtk.Label
	bootProgress     gtk.ProgressBar
	bootProgressText gtk.Label
	bootConsole      gtk.Button
	bootStop         gtk.Button

	// running
	runSummary   gtk.Label
	connect      gtk.Button
	consoleReady gtk.Button
	stop         gtk.Button

	// Where errors show: under the section whose action failed, one label a
	// section, by page.
	setupError, mediaError, setupRemoveError gtk.Label
	provError, bootError                     gtk.Label
	startError, readyError, removeError      gtk.Label
	runError, consoleError, assistantError   gtk.Label
	faultActionError                         gtk.Label

	mu            sync.Mutex // the chosen machine, read by the window's watch
	selected      string
	closed        bool
	setupFilled   bool
	readyFilled   bool
	operationBusy bool
	checkRefs     gtkx.Scope // the rows built for the current check list
	fixButtons    []fixTarget
	fixNote       string // what the last fix did, under the checks until the next one
	fixFailed     bool
	// errs is the last action's failure on each machine and where it shows,
	// kept per machine so going to another and back still shows it; it is
	// cleared when the user tries again.
	errs         map[string]actionErr
	waitButton   *gtk.Button // the button whose action runs, saying what it waits for
	waitLabel    string
	question     *dialog
	watchCancel  context.CancelFunc
	menu         *appui.MenuBar
	lastPage     string
	lastChecks   []vm.DoctorItem
	onClose      func(gtk.Window) bool
	onClick      func(gtk.Button)
	onSpin       func(gtk.SpinButton)
	onTileSelect func(gtk.FlowBox)
	onTemplate   func(gtk.ListBox, uintptr)
	onNameSubmit func(gtk.Entry)
}

// openWindow builds the manager window from the app's layouts: a second start
// of inua-vm brings this one forward instead of making another (appui).
func openWindow(c *appui.Ctx, manager VMManager) *vmWindow {
	v := &vmWindow{manager: manager, c: c, win: &gtk.ApplicationWindow{}, errs: map[string]actionErr{}}
	builder := c.Load("vm.ui")
	builder.Get("vm", v.win)
	for id, w := range map[string]gobject.Ptr{
		"vm_panel": &v.panel, "vm_library_empty": &v.libraryEmpty, "vm_head": &v.head, "vm_mark": &v.mark, "vm_machines_kicker": &v.machinesKicker, "vm_title": &v.title, "vm_sub": &v.sub, "vm_stack": &v.stack,
		"vm_machines": &v.machineList, "vm_fault_heading": &v.faultHeading, "vm_faults": &v.faultList,
		"vm_new": &v.newButton, "vm_empty_new": &v.emptyNew, "vm_new_dialog": &v.newDialog, "vm_new_systems": &v.newSystems,
		"vm_new_steps": &v.newSteps, "vm_new_next": &v.newNext, "vm_new_back": &v.newBack, "vm_create_note": &v.createNote,
		"vm_new_memory": &v.newMemory, "vm_new_cpus": &v.newCPUs, "vm_new_disk": &v.newDisk,
		"vm_new_memory_sub": &v.newMemorySub, "vm_new_cpus_sub": &v.newCPUsSub, "vm_new_disk_sub": &v.newDiskSub,
		"vm_templates": &v.templateList, "vm_new_name": &v.nameEntry, "vm_create": &v.createButton,
		"vm_new_cancel": &v.newCancel, "vm_create_error": &v.createError,
		"vm_na_note":    &v.naNote,
		"vm_fault_path": &v.faultPath, "vm_fault_error": &v.faultError, "vm_fault_delete": &v.faultDelete,
		"vm_fault_action_error": &v.faultActionError,
		"vm_console_back":       &v.consoleBack, "vm_console_summary": &v.consoleSummary,
		"vm_console_hardware": &v.consoleHardware, "vm_console_gdb": &v.consoleGDB,
		"vm_console_cmd": &v.consoleCmd, "vm_console_copy": &v.consoleCopy,
		"vm_console_start": &v.consoleStart, "vm_console_stop": &v.consoleStop, "vm_serial": &v.serial,
		"vm_console_error": &v.consoleError,
		"vm_check_rows":    &v.checkRows, "vm_check_count": &v.checkCount, "vm_fix_note": &v.fixLabel,
		"vm_assistant_ask": &v.assistantAsk, "vm_assistant_recheck": &v.assistantRecheck,
		"vm_assistant_note": &v.assistantNote, "vm_assistant_error": &v.assistantError,
		"vm_accept": &v.accept, "vm_memory": &v.memory, "vm_cpus": &v.cpus, "vm_disk": &v.disk,
		"vm_memory_sub": &v.memorySub, "vm_cpus_sub": &v.cpusSub, "vm_disk_sub": &v.diskSub,
		"vm_install": &v.install, "vm_setup_note": &v.setupNote, "vm_setup_error": &v.setupError,
		"vm_setup_leftovers_row": &v.leftoversRow, "vm_setup_remove_data": &v.setupRemoveData,
		"vm_setup_delete": &v.setupDelete, "vm_setup_remove_error": &v.setupRemoveError,
		"vm_setup_media": &v.setupMedia, "vm_media_source": &v.mediaSource, "vm_media_note": &v.mediaNote,
		"vm_media_rows": &v.mediaRows, "vm_media_recommended": &v.mediaRec, "vm_media_progress": &v.mediaProgress,
		"vm_iso_progress": &v.isoProgress, "vm_iso_progress_text": &v.isoProgressText, "vm_iso_cancel": &v.isoCancel,
		"vm_media_filters": &v.mediaFilters, "vm_iso_search": &v.isoSearch, "vm_iso_platform": &v.isoPlatform,
		"vm_iso_arch": &v.isoArch, "vm_iso_prerelease": &v.isoPre, "vm_iso_eol": &v.isoEOL,
		"vm_media_count": &v.mediaCount, "vm_media_error": &v.mediaError,
		"vm_iso_choose": &v.isoChoose, "vm_iso_browse": &v.isoBrowse, "vm_iso_dialog": &v.isoDialog,
		"vm_iso_dialog_note": &v.isoDialogNote, "vm_iso_close": &v.isoClose,
		"vm_prov_done_row": &v.provDoneRow, "vm_prov_done": &v.provDone,
		"vm_progress": &v.progress, "vm_progress_meta": &v.progressMeta, "vm_progress_text": &v.progressText,
		"vm_prov_watch_row": &v.provWatchRow, "vm_prov_watch_title": &v.provWatchTitle,
		"vm_prov_watch_note": &v.provWatchNote, "vm_console": &v.console,
		"vm_prov_stop_row": &v.provStopRow, "vm_prov_stop": &v.provStop,
		"vm_prov_start_row": &v.provStartRow, "vm_prov_start": &v.provStart,
		"vm_prov_remove_row": &v.provRemoveRow, "vm_prov_remove_title": &v.provRemoveTitle,
		"vm_prov_remove_note": &v.provRemoveNote, "vm_prov_remove": &v.provRemove,
		"vm_prov_delete_row": &v.provDeleteRow, "vm_prov_delete": &v.provDelete, "vm_prov_error": &v.provError,
		"vm_boot_summary": &v.bootSummary, "vm_boot_progress": &v.bootProgress,
		"vm_boot_progress_text": &v.bootProgressText, "vm_boot_watch_row": &v.bootWatchRow,
		"vm_boot_watch_title": &v.bootWatchTitle, "vm_boot_watch_note": &v.bootWatchNote,
		"vm_boot_console": &v.bootConsole, "vm_boot_stop": &v.bootStop, "vm_boot_error": &v.bootError,
		"vm_ready_summary": &v.readySummary, "vm_start": &v.start, "vm_start_error": &v.startError,
		"vm_ready_memory": &v.readyMemory, "vm_ready_cpus": &v.readyCPUs, "vm_disk_size": &v.diskSize,
		"vm_ready_save": &v.readySave, "vm_ready_error": &v.readyError,
		"vm_remove_row": &v.removeRow, "vm_remove": &v.remove, "vm_remove_data": &v.removeData,
		"vm_delete": &v.delete, "vm_remove_error": &v.removeError,
		"vm_run_summary": &v.runSummary, "vm_run_watch_row": &v.runWatchRow,
		"vm_run_watch_title": &v.runWatchTitle, "vm_run_watch_note": &v.runWatchNote, "vm_connect": &v.connect,
		"vm_run_console_row": &v.runConsoleRow, "vm_console_ready": &v.consoleReady, "vm_stop": &v.stop,
		"vm_run_error": &v.runError,
	} {
		builder.Get(id, w)
	}
	builder.Close()

	v.isoOpen = map[string]bool{}
	v.isoRevealer = map[string][]gtk.Revealer{}
	v.isoMark = map[string][]gtk.Image{}
	// The list opens on the guide's own list: the architecture this build runs.
	v.isoFilter = vm.ISOFilters{ArchFamily: vm.HostArchFamily()}
	manager.SetISOFilters(v.isoFilter)
	v.onClick = func(b gtk.Button) { v.clicked(b) }
	for _, b := range []*gtk.Button{&v.accept, &v.install,
		&v.console, &v.provStart, &v.provStop, &v.provRemove, &v.provDelete, &v.readySave, &v.start,
		&v.connect, &v.consoleReady, &v.stop, &v.remove, &v.removeData, &v.delete, &v.setupRemoveData,
		&v.setupDelete, &v.bootConsole, &v.bootStop, &v.newButton, &v.emptyNew, &v.createButton, &v.newCancel, &v.newNext, &v.newBack,
		&v.consoleBack, &v.consoleCopy, &v.consoleStart, &v.consoleStop,
		&v.assistantAsk, &v.assistantRecheck, &v.isoCancel, &v.faultDelete,
		&v.isoChoose, &v.isoBrowse, &v.isoClose, &v.provDone} {
		b.ConnectClicked(&v.onClick)
	}
	v.onSpin = func(gtk.SpinButton) { v.edited() }
	for _, sb := range []*gtk.SpinButton{&v.memory, &v.cpus, &v.disk, &v.readyMemory, &v.readyCPUs} {
		sb.ConnectValueChanged(&v.onSpin)
	}
	v.onTileSelect = func(box gtk.FlowBox) {
		if v.filling {
			return
		}
		if name := appui.SelectedTile(&box); name != "" {
			v.choose(name)
		}
	}
	v.machineList.ConnectSelectedChildrenChanged(&v.onTileSelect)
	v.faultList.ConnectSelectedChildrenChanged(&v.onTileSelect)
	v.onTemplate = func(_ gtk.ListBox, ptr uintptr) {
		if ptr == 0 || v.filling {
			return
		}
		if row := gtk.ListBoxRowNewFromInternalPtr(ptr); row != nil {
			v.chooseTemplate(row.GetName())
		}
	}
	v.templateList.ConnectRowSelected(&v.onTemplate)
	v.newDialog.SetTransientFor(&v.win.Window)
	v.newDialog.SetDestroyWithParent(true)
	v.onNewClose = func(gtk.Window) bool { return false } // hide-on-close keeps it for the next one
	v.newDialog.ConnectCloseRequest(&v.onNewClose)
	v.onNewKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if keyval == uint32(gdk.KEY_Escape) {
			v.closeNew()
			return true
		}
		return false
	}
	newKeys := gtk.NewEventControllerKey()
	newKeys.ConnectKeyPressed(&v.onNewKey)
	v.newDialog.AddController(&newKeys.EventController)
	v.onNameSubmit = func(_ gtk.Entry) { v.newStep("resources") }
	v.nameEntry.ConnectActivate(&v.onNameSubmit)
	v.isoDialog.SetTransientFor(&v.win.Window)
	v.isoDialog.SetDestroyWithParent(true)
	v.onISOClose = func(gtk.Window) bool { return false } // hide-on-close keeps it for the next one
	v.isoDialog.ConnectCloseRequest(&v.onISOClose)
	v.onISOKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if keyval == uint32(gdk.KEY_Escape) {
			v.isoDialog.SetVisible(false)
			return true
		}
		return false
	}
	isoKeys := gtk.NewEventControllerKey()
	isoKeys.ConnectKeyPressed(&v.onISOKey)
	v.isoDialog.AddController(&isoKeys.EventController)
	v.onISOFile = func(_, result, _ uintptr) { v.isoFileChosen(result) }
	// The download list's controls: every one of them hands the window's own
	// filter to the service and paints again.
	v.onISOSearch = func(_ gtk.Entry) { v.applyISOFilters() }
	v.onISOEntryChanged = func(_, _ uintptr) { v.applyISOFilters() }
	v.onISODropChanged = func(_, _, _ uintptr) { v.applyISOFilters() }
	// A switch hands the state it is moving to: GtkSwitch's own handler has not
	// run yet, so reading the widget here would read the old one.
	v.onISOFilterSwitch = func(sw gtk.Switch, on bool) bool {
		if v.filling {
			return false
		}
		if sw.GoPointer() == v.isoPre.GoPointer() {
			v.isoFilter.IncludePrerelease = on
		} else {
			v.isoFilter.IncludeEnded = on
		}
		v.manager.SetISOFilters(v.isoFilter)
		v.paintAll(v.manager.Machines())
		return false
	}
	v.isoSearch.ConnectActivate(&v.onISOSearch)
	gtkx.ConnectChanged(&v.isoSearch, &v.onISOEntryChanged)
	gtkx.ConnectNotify(&v.isoPlatform, "selected", &v.onISODropChanged)
	gtkx.ConnectNotify(&v.isoArch, "selected", &v.onISODropChanged)
	v.isoPre.ConnectStateSet(&v.onISOFilterSwitch)
	v.isoEOL.ConnectStateSet(&v.onISOFilterSwitch)
	v.onISOSystem = func(b gtk.Button) {
		id := strings.TrimPrefix(b.GetName(), "iso_system:")
		if id == "" {
			return
		}
		v.openISOSystem(id, !v.isoOpen[id])
	}
	{
		names := make([]string, 0, len(isoPlatformChoices))
		for _, p := range isoPlatformChoices {
			names = append(names, isoPlatformText(p))
		}
		v.isoPlatform.SetModel(gtk.NewStringList(names))
	}
	v.onGDB = func(_ gtk.Switch, on bool) bool {
		// A repaint sets the switch to what the machine says; only the user's
		// own click writes it back.
		if v.filling {
			return false
		}
		m := v.viewOf(v.selectedName()).Machine
		if m.Boot.GDB == on {
			return false
		}
		m.Boot.GDB = on
		v.run(func() error { return v.manager.Save(m) }, &v.consoleError, nil, "")
		return false
	}
	v.consoleGDB.ConnectStateSet(&v.onGDB)
	v.serialBuffer = v.serial.GetBuffer()
	v.onClose = func(gtk.Window) bool {
		v.closed = true
		v.stopSerial()
		v.watchCancel()
		if v.menu != nil {
			v.menu.Close()
		}
		return false
	}
	v.win.ConnectCloseRequest(&v.onClose)

	v.assistantHint = v.assistantNote.GetText()
	v.fillTemplates()
	v.installMenuBar()
	v.paintAll(manager.Machines())
	v.startWatch()
	v.win.Present()
	return v
}

// startWatch follows the chosen machine while the window is open, and the
// whole list less often, so an install that takes minutes moves the page on
// its own and a machine started elsewhere shows up here.
func (v *vmWindow) startWatch() {
	ctx, cancel := context.WithCancel(context.Background())
	v.watchCancel = cancel
	go func() {
		fast := time.NewTicker(2 * time.Second)
		slow := time.NewTicker(10 * time.Second)
		defer fast.Stop()
		defer slow.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-fast.C:
				if name := v.selectedName(); name != "" {
					view := v.manager.Refresh(name)
					gtkx.Post(func() {
						if v.closed {
							return
						}
						for i, x := range v.views {
							if x.Name == view.Name {
								v.views[i] = view
							}
						}
						v.updateRows()
						v.paint()
					})
				}
			case <-slow.C:
				views := v.manager.RefreshAll()
				gtkx.Post(func() {
					if !v.closed {
						v.paintAll(views)
					}
				})
			}
		}
	}()
}

// selectedName is the machine the panel shows. The watch reads it from its own
// goroutine while GTK callbacks write it, so it has a lock of its own.
func (v *vmWindow) selectedName() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.selected
}

func (v *vmWindow) setSelected(name string) {
	v.mu.Lock()
	v.selected = name
	v.mu.Unlock()
}

// choose shows a machine's page, reading its backend now rather than waiting
// for the next tick: the user just asked for it.
func (v *vmWindow) choose(name string) {
	if v.selectedName() == name {
		return
	}
	v.stopSerial()
	v.consoleOpen = false
	v.isoDialog.SetVisible(false) // it lists the images of the machine it was opened for
	v.setSelected(name)
	v.lastPage = ""
	v.setupFilled, v.readyFilled = false, false
	v.selectRow()
	v.paint()
	name = v.selectedName()
	go func() {
		view := v.manager.Refresh(name)
		gtkx.Post(func() {
			if v.closed || v.selectedName() != name {
				return
			}
			for i, x := range v.views {
				if x.Name == view.Name {
					v.views[i] = view
				}
			}
			v.updateRows()
			v.paint()
		})
	}()
}

func (v *vmWindow) paintAll(views []MachineView) {
	if v.closed || v.win == nil {
		return
	}
	v.views = views
	v.faults = v.manager.Faults()
	visible := len(v.faults) > 0
	v.faultList.SetVisible(visible)
	v.faultHeading.SetVisible(visible)
	selected := v.selectedName()
	if !v.has(selected) && !v.hasFault(selected) {
		selected = ""
		if len(views) > 0 {
			selected = views[0].Name
		} else if len(v.faults) > 0 {
			selected = v.faults[0].Name
		}
		v.lastPage, v.setupFilled, v.readyFilled = "", false, false
	}
	v.setSelected(selected)
	v.rebuildRows()
	v.rebuildFaultRows()
	v.selectRow()
	v.paint()
}

func (v *vmWindow) has(name string) bool {
	if name == "" {
		return false
	}
	for _, x := range v.views {
		if x.Name == name {
			return true
		}
	}
	return false
}

func (v *vmWindow) hasFault(name string) bool {
	for _, fault := range v.faults {
		if fault.Name == name {
			return true
		}
	}
	return false
}

// rebuildFaultRows lists the folders that do not load as a machine, each a
// tile with what is wrong with it under its name.
func (v *vmWindow) rebuildFaultRows() {
	v.faultList.RemoveAll()
	for _, fault := range v.faults {
		tile := appui.NewLibraryTile(fault.Name, "dialog-warning-symbolic", fault.Name, i18n.T("Does not load"), false)
		v.faultList.Append(&tile.Child.Widget)
	}
}

func (v *vmWindow) viewOf(name string) MachineView {
	for _, x := range v.views {
		if x.Name == name {
			return x
		}
	}
	return MachineView{Name: name}
}

// rebuildRows makes a tile per machine, and only when the machines themselves
// changed: a plain repaint updates the labels in place, which is what the
// watch does every two seconds.
func (v *vmWindow) rebuildRows() {
	same := len(v.rows) == len(v.views)
	if same {
		for i, r := range v.rows {
			if r.name != v.views[i].Name {
				same = false
				break
			}
		}
	}
	if same {
		v.updateRows()
		return
	}
	v.machineList.RemoveAll()
	v.rows = nil
	for _, x := range v.views {
		built := v.buildTile(x)
		v.machineList.Append(&built.tile.Child.Widget)
		v.rows = append(v.rows, built)
	}
	v.selectRow()
}

// updateRows brings the tiles' labels and marks up to date.
func (v *vmWindow) updateRows() {
	for i := range v.rows {
		if i >= len(v.views) {
			break
		}
		x := v.views[i]
		v.rows[i].tile.Title.SetText(x.Name)
		v.rows[i].tile.Sub.SetText(vmListSub(x))
		v.rows[i].tile.SetOn(x.State == vm.StateRunning)
	}
}

// buildTile is one machine in the library: its system's mark with its own
// mark beside it, filled while it is up, then its name, and what it is and is
// doing under it.
func (v *vmWindow) buildTile(x MachineView) machineRow {
	tile := appui.NewLibraryTile(x.Name, vmOSIcon(x.Machine.OS), x.Name, vmListSub(x), true)
	tile.SetOn(x.State == vm.StateRunning)
	return machineRow{name: x.Name, tile: tile}
}

// selectRow marks the chosen machine in the library, in whichever group
// holds it. It runs while filling,
// so marking a row does not read as the user choosing it.
func (v *vmWindow) selectRow() {
	v.filling = true
	defer func() { v.filling = false }()
	name := v.selectedName()
	v.machineList.UnselectAll()
	v.faultList.UnselectAll()
	if name == "" {
		return
	}
	for i, r := range v.rows {
		if r.name == name {
			if tile := v.machineList.GetChildAtIndex(int32(i)); tile != nil {
				v.machineList.SelectChild(tile)
			}
			return
		}
	}
	for i, f := range v.faults {
		if f.Name == name {
			if tile := v.faultList.GetChildAtIndex(int32(i)); tile != nil {
				v.faultList.SelectChild(tile)
			}
			return
		}
	}
}

// paint picks the page and fills it. It runs on the GTK thread and reads only
// what is already in hand: no backend is touched here.
func (v *vmWindow) paint() {
	if v.closed || v.win == nil {
		return
	}
	page, view := v.pageOf()
	changed := page != v.lastPage
	if changed {
		if page == string(vm.PageSetup) {
			v.setupFilled = false
		} else {
			v.isoDialog.SetVisible(false)
		}
		if page == string(vm.PageStopped) {
			v.readyFilled = false
		}
		v.lastPage = page
	}
	if view.State == vm.StateRunning && page != "fault" {
		v.mark.AddCssClass("on")
	} else {
		v.mark.RemoveCssClass("on")
	}
	v.panel.SetVisible(page != "empty")
	if page != "empty" {
		v.stack.SetVisibleChildName(page)
	}
	v.machinesKicker.SetVisible(len(v.views) > 0)
	empty := len(v.views) == 0 && len(v.faults) == 0
	v.libraryEmpty.SetVisible(empty)
	v.newButton.SetVisible(!empty) // the empty library has its own
	switch page {
	case "fault":
		v.title.SetText(view.Name)
		v.sub.SetText(i18n.T("Does not load as a machine"))
		for _, fault := range v.faults {
			if fault.Name == view.Name {
				v.faultPath.SetText(fault.Path)
				v.faultError.SetText(fault.Error)
			}
		}
		v.faultDelete.SetSensitive(!v.operationBusy)
	case "unavailable":
		v.title.SetText(view.Name)
		v.sub.SetText(i18n.T("No backend for this machine"))
		v.naNote.SetText(i18n.Tf("This machine needs the %s backend, which this build does not have.", vmBackendText(view.Machine.Backend)))
	default:
		v.title.SetText(view.Name)
		v.sub.SetText(vmStateText(view))
		v.setWatchRows(view)
	}
	v.showError(page, view)
	switch page {
	case "console":
		v.showConsolePage(view)
	case string(vm.PagePrereq):
		v.showChecks(view)
	case string(vm.PageSetup):
		v.showSetup(view)
	case string(vm.PageProvisioning):
		v.showProvisioning(view)
	case string(vm.PageStopped):
		v.showStopped(view)
	case string(vm.PageStarting):
		v.showStarting(view)
	case string(vm.PageRunning):
		v.showRunning(view)
	}
	if changed {
		v.focusMain(page)
	}
}

// showConsole opens the console page of the machine that is chosen.
func (v *vmWindow) showConsole() {
	if v.selectedName() == "" {
		return
	}
	v.consoleOpen = true
	v.paint()
}

// showConsolePage fills the console page: what the machine will get, its
// debugger, and what it has printed.
func (v *vmWindow) showConsolePage(s MachineView) {
	v.consoleSummary.SetText(vmProfileText(s.Machine))
	v.consoleHardware.SetText(vmHardwareText(s))
	v.filling = true
	v.consoleGDB.SetActive(s.Machine.Boot.GDB)
	v.filling = false
	line := s.DebuggerCmd
	v.startSerial()
	if line == "" {
		line = i18n.T("Turn the debugger on to hold the guest for gdb.")
	}
	v.consoleCmd.SetText(line)
	idle := !s.Busy && !v.operationBusy
	up := s.State == vm.StateRunning || s.State == vm.StateStarting
	v.consoleCopy.SetSensitive(s.DebuggerCmd != "")
	v.consoleStart.SetVisible(!up)
	v.consoleStop.SetVisible(up)
	v.consoleStart.SetSensitive(idle)
	v.consoleStop.SetSensitive(idle)
	v.setSerial(s.SerialLog)
}

func (v *vmWindow) startSerial() {
	name := v.selectedName()
	view := v.viewOf(name)
	if view.Machine.Backend != vm.BackendQEMU || view.Display.Kind != vm.DisplaySerial ||
		view.State != vm.StateRunning && view.State != vm.StateStarting {
		v.stopSerial()
		return
	}
	if v.serialCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.serialCancel = cancel
	go func() {
		if err := v.manager.TeeSerialContext(ctx, name); err != nil && ctx.Err() == nil {
			log.Printf("vm serial %s: %v", name, err)
		}
	}()
}

func (v *vmWindow) stopSerial() {
	if v.serialCancel != nil {
		v.serialCancel()
		v.serialCancel = nil
	}
}

// setSerial shows what the guest has printed, and keeps the view at the end of
// it: a console is read from the bottom.
func (v *vmWindow) setSerial(text string) {
	if v.serialBuffer == nil || text == v.serialText {
		return
	}
	v.serialText = text
	v.serialBuffer.SetText(text, int32(len(text)))
	var end gtk.TextIter
	v.serialBuffer.GetEndIter(&end)
	v.serial.ScrollToIter(&end, 0, false, 0, 0)
}

// copyDebuggerLine puts the attach line on the clipboard, so it can be pasted
// into a terminal, and says so on the button for a moment.
func (v *vmWindow) copyDebuggerLine() {
	view := v.viewOf(v.selectedName())
	if view.DebuggerCmd == "" {
		return
	}
	display := gdk.DisplayGetDefault()
	if display == nil {
		return
	}
	clipboard := display.GetClipboard()
	if clipboard == nil {
		return
	}
	clipboard.SetText(view.DebuggerCmd)
	v.consoleCopy.SetLabel(i18n.T("Copied"))
	go func() {
		time.Sleep(2 * time.Second)
		gtkx.Post(func() { v.consoleCopy.SetLabel(i18n.T("Copy")) })
	}()
}

// vmHardwareText is what the guest will get on this host: whether it runs on
// the processor or is emulated, and what its graphics are.
func vmHardwareText(s MachineView) string {
	parts := []string{}
	if text := vmAccelerationText(s); text != "" {
		parts = append(parts, text)
	}
	parts = append(parts, vmGraphicsText(s.Machine))
	return strings.Join(parts, " · ")
}

// vmAccelerationText says Accelerated or Emulated, and why. The reasons are the
// backend's stable identifiers; the sentences are the window's.
func vmAccelerationText(s MachineView) string {
	if s.Machine.Backend != vm.BackendQEMU {
		return ""
	}
	if s.Accelerated {
		return i18n.T("Accelerated")
	}
	switch s.AccelerationReason {
	case vm.ReasonRequestedEmulation:
		return i18n.T("Emulated (slow): this machine asks for emulation")
	case vm.ReasonOtherArchitecture:
		return i18n.T("Emulated (slow): the guest is built for another architecture")
	case vm.ReasonNoKVM:
		return i18n.T("Emulated (slow): this host has no /dev/kvm")
	case vm.ReasonKVMDenied:
		return i18n.T("Emulated (slow): your user may not open /dev/kvm")
	}
	return i18n.T("Emulated (slow)")
}

// vmGraphicsText is the display the guest gets: the guide's table, in the
// window's words. A Windows guest has no accelerated driver, and a custom
// system has one only once it has written one.
func vmGraphicsText(m vm.Machine) string {
	if m.Backend != vm.BackendQEMU {
		return ""
	}
	if m.OS == vm.OSWindows {
		return i18n.T("2D: Windows has no accelerated driver here")
	}
	switch m.Display.GPU {
	case vm.GPUVirgl:
		return i18n.T("3D (OpenGL, through the host's GPU)")
	case vm.GPUVenus:
		return i18n.T("3D (Vulkan, through the host's GPU)")
	}
	return i18n.T("2D")
}

// fillTemplates fills the New machine dialog, once: a choice per system,
// and a row per way one runs, of which the chosen system's show. The
// templates do not change while the window is open.
func (v *vmWindow) fillTemplates() {
	templates := v.manager.Templates()
	v.templateRows, v.systems = nil, nil
	v.onSystem = func(b gtk.ToggleButton) {
		if b.GetActive() && !v.filling {
			v.chooseSystem(vm.OS(b.GetName()))
		}
	}
	for _, t := range templates {
		if !slices.ContainsFunc(v.systems, func(b gtk.ToggleButton) bool { return b.GetName() == string(t.OS) }) {
			b := gtk.NewToggleButton()
			b.AddCssClass("vm-system")
			b.SetName(string(t.OS))
			line := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
			line.SetHalign(gtk.AlignCenterValue)
			mark := gtk.NewImageFromIconName(vmOSIcon(t.OS))
			mark.AddCssClass("vm-os-mark")
			gtkx.Append(line, &mark.Widget)
			gtkx.Append(line, &gtk.NewLabel(vmOSText(t.OS)).Widget)
			b.SetChild(&line.Widget)
			if len(v.systems) > 0 {
				b.SetGroup(&v.systems[0])
			}
			b.ConnectToggled(&v.onSystem)
			gtkx.Append(&v.newSystems, &b.Widget)
			v.systems = append(v.systems, *b)
		}

		text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		text.SetValign(gtk.AlignCenterValue)
		title := gtk.NewLabel(vmWayName(t))
		title.AddCssClass("vm-row-name")
		title.SetXalign(0)
		sub := gtk.NewLabel(vmTemplateText(t))
		sub.AddCssClass("vm-template-sub")
		sub.SetXalign(0)
		sub.SetWrap(true)
		sub.SetMaxWidthChars(60) // the dialog keeps its width whichever system is chosen
		gtkx.Append(text, &title.Widget)
		gtkx.Append(text, &sub.Widget)

		row := gtk.NewListBoxRow()
		row.SetName(t.ID)
		row.SetChild(&text.Widget)
		v.templateList.Append(&row.Widget)
		v.templateRows = append(v.templateRows, templateRow{id: t.ID, os: t.OS, row: *row})
	}
	if len(templates) > 0 {
		v.chooseTemplate(templates[0].ID)
	}
}

// chooseSystem shows the ways a system runs, and picks its first unless one
// of its own is picked already.
func (v *vmWindow) chooseSystem(os vm.OS) {
	if t, ok := vm.TemplateByID(v.templateID); ok && t.OS == os {
		return
	}
	for _, r := range v.templateRows {
		if r.os == os {
			v.chooseTemplate(r.id)
			return
		}
	}
}

// showNew asks for a new machine in its dialog, from its first step, the
// name ready to type over and the resources what suits this computer.
func (v *vmWindow) showNew() {
	v.createError.SetVisible(false)
	if v.templateID == "" && len(v.templateRows) > 0 {
		v.chooseTemplate(v.templateRows[0].id)
	}
	r := v.manager.Recommended()
	v.newMemory.SetValue(float64(gibOf(r.MemoryMiB)))
	v.newCPUs.SetValue(float64(r.CPUs))
	v.newDisk.SetValue(float64(r.DiskGiB))
	v.newMemorySub.SetText(i18n.Tf("Recommended: %d GiB.", gibOf(r.MemoryMiB)))
	v.newCPUsSub.SetText(i18n.Tf("Recommended: %d.", r.CPUs))
	v.newDiskSub.SetText(i18n.Tf("Recommended: %d GiB.", r.DiskGiB))
	v.newStep("what")
	v.newDialog.Present()
	v.nameEntry.GrabFocus()
}

// newStep shows a step of the New machine dialog with its buttons: Cancel
// and Next on the first, Back and Create on the second.
func (v *vmWindow) newStep(step string) {
	resources := step == "resources"
	v.newSteps.SetVisibleChildName(step)
	v.newCancel.SetVisible(!resources)
	v.newNext.SetVisible(!resources)
	v.newBack.SetVisible(resources)
	v.createButton.SetVisible(resources)
	v.createNote.SetVisible(resources)
	if resources {
		v.createButton.GrabFocus()
	} else {
		v.nameEntry.GrabFocus()
	}
}

// chooseTemplate picks the template a new machine starts from, and offers the
// name that template usually takes.
func (v *vmWindow) chooseTemplate(id string) {
	t, ok := vm.TemplateByID(id)
	if !ok {
		return
	}
	v.templateID = id
	v.filling = true
	for i := range v.templateRows {
		r := &v.templateRows[i]
		r.row.SetVisible(r.os == t.OS)
		if r.id == id {
			v.templateList.SelectRow(&r.row)
		}
	}
	for i := range v.systems {
		if b := &v.systems[i]; b.GetName() == string(t.OS) {
			b.SetActive(true)
		}
	}
	v.filling = false
	v.createError.SetVisible(false)
	if want := t.Name; v.nameEntry.GetText() == "" || v.nameEntry.GetText() == v.templateName {
		v.nameEntry.SetText(want)
	}
	v.templateName = t.Name
}

// createMachine writes the new machine and shows it: nothing is installed or
// started here, so the new machine's page says what it needs next.
func (v *vmWindow) createMachine() {
	if v.operationBusy {
		return
	}
	id, name := v.templateID, v.nameEntry.GetText()
	if id == "" {
		return
	}
	p := vm.Profile{MemoryMiB: int(v.newMemory.GetValueAsInt()) * 1024, CPUs: int(v.newCPUs.GetValueAsInt()),
		DiskGiB: int(v.newDisk.GetValueAsInt())}
	v.operationBusy = true
	v.createError.SetVisible(false)
	v.createButton.SetSensitive(false)
	v.createButton.SetLabel(i18n.T("Creating…"))
	go func() {
		m, err := v.manager.Create(id, name, p)
		views := v.manager.Machines()
		gtkx.Post(func() {
			v.operationBusy = false
			v.createButton.SetSensitive(true)
			v.createButton.SetLabel(i18n.T("Create"))
			if err != nil {
				v.newStep("what") // where the name is
				v.createError.SetText(i18n.Tf("That name cannot be used: %v", err))
				v.createError.SetVisible(true)
				return
			}
			v.newDialog.SetVisible(false)
			v.nameEntry.SetText("")
			v.templateName = ""
			v.chooseTemplate(v.templateID)
			v.setSelected(m.Name)
			v.lastPage, v.setupFilled, v.readyFilled = "", false, false
			v.paintAll(views)
		})
	}()
}

// showChecks lists what the host is missing, one row each, in the user's
// language: the platform reports a reason, not a sentence.
func (v *vmWindow) showChecks(s MachineView) {
	v.fixLabel.SetVisible(v.fixNote != "")
	v.fixLabel.SetText(v.fixNote)
	if v.fixFailed {
		v.fixLabel.AddCssClass("error")
	} else {
		v.fixLabel.RemoveCssClass("error")
	}
	failed := failedChecks(s.Checks)
	v.checkCount.SetText(i18n.Nf("%d thing to fix", "%d things to fix", len(failed), len(failed)))
	idle := !s.Busy && !v.operationBusy
	v.assistantAsk.SetSensitive(idle)
	v.assistantRecheck.SetSensitive(idle)
	if doctorItemsEqual(v.lastChecks, failed) {
		for _, t := range v.fixButtons {
			t.btn.SetSensitive(idle)
		}
		return
	}
	v.lastChecks = failed
	v.checkRefs.Release()
	v.fixButtons = nil
	gtkx.Clear(&v.checkRows)
	for _, c := range failed {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		row.AddCssClass("row")

		mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		mark.AddCssClass("mark")
		mark.SetValign(gtk.AlignStartValue)
		gtkx.Append(row, &mark.Widget)

		text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		text.SetHexpand(true)
		text.SetValign(gtk.AlignCenterValue)
		name := gtk.NewLabel(vmCheckName(c.Name))
		name.AddCssClass("label")
		name.SetXalign(0)
		reason := gtk.NewLabel(vmCheckReason(c.Name, c.Reason))
		reason.AddCssClass("sub")
		reason.SetXalign(0)
		reason.SetWrap(true)
		gtkx.Append(text, &name.Widget)
		gtkx.Append(text, &reason.Widget)
		// What the user can do about it when inua cannot: the command, to
		// copy, or what to change where.
		if c.Remedy == nil {
			if hint, command := vmCheckHint(c, s.Distribution); hint != "" {
				l := gtk.NewLabel(hint)
				l.AddCssClass("sub")
				if command {
					l.AddCssClass("vm-command")
					l.SetSelectable(true)
					l.SetWrapMode(2) // word_char: a command has few spaces
				}
				l.SetXalign(0)
				l.SetWrap(true)
				gtkx.Append(text, &l.Widget)
			}
		}
		gtkx.Append(row, &text.Widget)

		// A button when inua can apply the fix itself.
		if c.Remedy != nil {
			label := i18n.T("Fix")
			if c.Remedy.Group != "" {
				label = i18n.Tf("Add me to %s", c.Remedy.Group)
			}
			btn := gtk.NewButtonWithLabel(label)
			btn.AddCssClass("outlined")
			btn.SetValign(gtk.AlignCenterValue)
			btn.SetName("fix")
			btn.SetSensitive(idle)
			v.fixButtons = append(v.fixButtons, fixTarget{btn: *btn, remedy: *c.Remedy})
			btn.ConnectClicked(&v.onClick)
			gtkx.Append(row, &btn.Widget)
		}
		gtkx.Append(&v.checkRows, &row.Widget)
	}
}

// failedChecks are the host checks that did not pass.
func failedChecks(checks []vm.DoctorItem) []vm.DoctorItem {
	var out []vm.DoctorItem
	for _, c := range checks {
		if !c.OK {
			out = append(out, c)
		}
	}
	return out
}

// vmCheckName is a check's name. The platform names them in English; these are
// the ones the window shows.
func vmCheckName(name string) string {
	switch name {
	case "Operating system":
		return i18n.T("Operating system")
	case "KVM":
		return i18n.T("Hardware virtualization (KVM)")
	case "Docker":
		return i18n.T("Docker")
	case "Docker Compose v2":
		return i18n.T("Docker Compose v2")
	case "FreeRDP":
		return i18n.T("FreeRDP")
	case "Browser opener":
		return i18n.T("Browser opener")
	case "Disk":
		return i18n.T("Disk space")
	case "TUN":
		return i18n.T("Network device (TUN)")
	case "Secret Service":
		return i18n.T("Secret Service")
	case "QEMU":
		return "QEMU"
	case "Firmware (UEFI)":
		return i18n.T("UEFI firmware")
	case "Waydroid":
		return "Waydroid"
	case "Binder":
		return i18n.T("Binder (the kernel's Android support)")
	}
	return name
}

// vmCheckReason words why a check failed. The Docker socket is the one case
// whose reason has to carry a warning: the socket is there and this user may
// not use it, and the fix — joining the docker group — would give everything
// running as this user what amounts to root on this host. inua explains it,
// shows the command under it (vmCheckHint) for whoever decides they want it,
// and points at the two ways that do not widen the account's rights; the
// QEMU backend needs no Docker at all.
func vmCheckReason(name string, r vm.DoctorReason) string {
	if name == "Docker" && r == vm.ReasonDenied {
		return i18n.T("Your user may not use Docker's socket. Joining the docker group would let anything running as you control this machine as root, so Inua will not do it for you: run the command below yourself if you accept that, or use rootless Docker or Podman instead.")
	}
	switch r {
	case vm.ReasonMissing:
		return i18n.T("Not installed on this system.")
	case vm.ReasonDenied:
		return i18n.T("Installed, but your user may not use it.")
	case vm.ReasonStopped:
		return i18n.T("Installed, but not running.")
	case vm.ReasonTooOld:
		return i18n.T("This Linux distribution is not supported.")
	}
	return i18n.T("Could not be checked.")
}

// fixTarget ties a button to the remedy it applies.
type fixTarget struct {
	btn    gtk.Button
	remedy vm.Remedy
}

// showSetup fills the page shown before a machine's guest exists: what it
// gets, the one action that means "install now", what it installs from, and
// removing it.
func (v *vmWindow) showSetup(s MachineView) {
	m := s.Machine
	if !v.setupFilled {
		recommended := s.Recommended
		if m.MemoryMiB == 0 {
			m.MemoryMiB, m.CPUs, m.DiskGiB = recommended.MemoryMiB, recommended.CPUs, recommended.DiskGiB
		}
		v.filling = true
		v.memory.SetValue(float64(gibOf(m.MemoryMiB)))
		v.cpus.SetValue(float64(m.CPUs))
		v.disk.SetValue(float64(m.DiskGiB))
		v.filling = false
		v.setupFilled = true
	}
	v.showRecommended(s)
	needsImage := s.Machine.Backend == vm.BackendQEMU && strings.TrimSpace(s.Machine.Boot.ISO) == "" &&
		strings.TrimSpace(s.Machine.Boot.Kernel) == ""
	switch {
	case needsImage:
		v.setupNote.SetText(i18n.T("Choose an install image first."))
	case s.Leftovers:
		v.setupNote.SetText(i18n.T("Installs from the files the earlier attempt left."))
	default:
		v.setupNote.SetText(vmInstallNote(s))
	}
	v.leftoversRow.SetVisible(s.Leftovers)
	idle := !s.Busy && !v.operationBusy
	v.accept.SetSensitive(idle)
	v.install.SetSensitive(idle && !needsImage)
	for _, sb := range []*gtk.SpinButton{&v.memory, &v.cpus, &v.disk} {
		sb.SetSensitive(idle)
	}
	v.setupRemoveData.SetSensitive(idle)
	v.setupDelete.SetSensitive(idle)
	v.showSetupMedia(s)
}

// isoTarget ties a row's button to the medium it downloads, as a remedy's
// button is tied to its remedy: one handler serves every button in the window.
type isoTarget struct {
	btn   gtk.Button
	media vm.ISOMedia
}

// showSetupMedia fills the install-image section of the setup page: what the
// machine installs from, the buttons that choose it, and the download in
// flight; and the download dialog while it is open. A machine whose backend
// fetches its own media has no such section.
func (v *vmWindow) showSetupMedia(s MachineView) {
	if s.ISO == nil {
		v.setupMedia.SetVisible(false)
		v.isoDialog.SetVisible(false)
		return
	}
	v.setupMedia.SetVisible(true)
	v.mediaNote.SetText(vmMediaNote(s))
	idle := !s.Busy && !v.operationBusy && !s.ISO.Downloading
	v.isoChoose.SetSensitive(idle && v.isoFile == nil)
	v.isoBrowse.SetSensitive(idle)

	if s.ISO.Downloading {
		v.mediaProgress.SetVisible(true)
		if s.ISO.Total > 0 {
			v.isoProgress.SetFraction(float64(s.ISO.Done) / float64(s.ISO.Total))
			v.isoProgressText.SetText(i18n.Tf("%s of %s downloaded", isoBytesText(s.ISO.Done), isoBytesText(s.ISO.Total)))
		} else {
			// The server has not said how large the file is.
			v.isoProgress.Pulse()
			v.isoProgressText.SetText(i18n.Tf("%s downloaded", isoBytesText(s.ISO.Done)))
		}
		v.isoCancel.SetSensitive(true)
	} else {
		v.mediaProgress.SetVisible(false)
	}
	if !idle {
		v.isoDialog.SetVisible(false)
	}
	// The list is the window's slowest thing to build: it is built for the
	// dialog that shows it, not for every repaint of the page.
	if v.isoDialog.GetVisible() {
		v.showISODialog(s.ISO)
	}
}

// showISODialog fills the download dialog: where the list comes from, the
// filters, the count and the systems.
func (v *vmWindow) showISODialog(iso *ISOView) {
	v.mediaSource.SetText(isoSourceText(iso.Source))
	v.isoDialogNote.SetText(isoDialogNote(iso.Source))
	v.showISOFilters(iso)
	v.mediaCount.SetText(isoCountText(iso.SystemsCount, iso.MediaCount))
	v.fillISOSystems(iso.Recommended, iso.Systems)
}

// openISODialog shows the download dialog over the window, filled before its
// first frame.
func (v *vmWindow) openISODialog() {
	s := v.viewOf(v.selectedName())
	if s.ISO == nil {
		return
	}
	v.isoDialog.SetVisible(true)
	v.showISODialog(s.ISO)
	v.isoDialog.Present()
	v.isoSearch.GrabFocus()
}

// chooseISO asks for an image file of the user's own.
func (v *vmWindow) chooseISO() {
	if v.isoFile != nil {
		return
	}
	d := gtk.NewFileDialog()
	d.SetTitle(i18n.T("Choose an install image"))
	filter := gtk.NewFileFilter()
	filter.SetName(i18n.T("Disc images"))
	for _, suffix := range []string{"iso", "img"} {
		filter.AddSuffix(suffix)
	}
	d.SetDefaultFilter(filter)
	filter.Unref()
	if dir := vm.ExpandPath("~/Downloads"); isDir(dir) {
		f := gio.FileNewForPath(dir)
		d.SetInitialFolder(f)
		(&gobject.Object{Ptr: f.GoPointer()}).Unref()
	}
	v.isoFile = d
	v.isoChoose.SetSensitive(false)
	d.Open(&v.win.Window, nil, &v.onISOFile, 0)
}

// isDir says whether path is a folder that is there.
func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// isoFileChosen makes the chosen file the machine's install image.
func (v *vmWindow) isoFileChosen(result uintptr) {
	d := v.isoFile
	v.isoFile = nil
	if d == nil {
		return
	}
	defer d.Unref()
	file, err := d.OpenFinish(&gio.AsyncResultBase{Ptr: result})
	if err != nil || file == nil {
		v.paint() // cancelled: the button comes back
		return
	}
	path := file.GetPath()
	(&gobject.Object{Ptr: file.GoPointer()}).Unref()
	name := v.selectedName()
	v.run(func() error { return v.manager.SetISO(name, path) }, &v.mediaError, nil, "")
}

// ── the download list ───────────────────────────────────────────────────────

// showISOFilters puts the filter controls in the state the window's own filter
// says: what the user last chose. It runs while filling, so that setting a
// control does not read as the user changing it.
func (v *vmWindow) showISOFilters(iso *ISOView) {
	v.filling = true
	defer func() { v.filling = false }()

	if !slices.Equal(v.isoArchs, iso.ArchFamilies) {
		v.isoArchs = append(v.isoArchs[:0], iso.ArchFamilies...)
		names := make([]string, 0, len(v.isoArchs)+1)
		names = append(names, i18n.T("Every architecture"))
		for _, a := range v.isoArchs {
			names = append(names, isoArchText(a))
		}
		v.isoArch.SetModel(gtk.NewStringList(names))
	}
	if v.isoSearch.GetText() != v.isoFilter.Search {
		v.isoSearch.SetText(v.isoFilter.Search)
	}
	v.isoPlatform.SetSelected(uint32(isoPlatformChoice(v.isoFilter.Platform)))
	v.isoArch.SetSelected(uint32(isoArchChoice(v.isoArchs, v.isoFilter.ArchFamily)))
	v.isoPre.SetActive(v.isoFilter.IncludePrerelease)
	v.isoEOL.SetActive(v.isoFilter.IncludeEnded)
}

// isoPlatformChoice is the family drop-down's index for a platform: 0 is every
// platform, and the rest follow the list the control is built with.
func isoPlatformChoice(platform string) int {
	for i, p := range isoPlatformChoices {
		if p == platform {
			return i
		}
	}
	return 0
}

// isoArchChoice is the architecture drop-down's index for a family: 0 is every
// architecture and the families the catalog holds follow, so a family the
// catalog does not hold is never offered and never selected.
func isoArchChoice(families []string, family string) int {
	for i, f := range families {
		if f == family {
			return i + 1
		}
	}
	return 0
}

// isoPlatformChoices are the platform filter's values: every platform first,
// then the four the catalog places systems in.
var isoPlatformChoices = []string{
	"", vm.ISOPlatformLinux, vm.ISOPlatformBSD, vm.ISOPlatformWindows,
	vm.ISOPlatformOther,
}

// isoPlatformText names a platform for the filter.
func isoPlatformText(platform string) string {
	switch platform {
	case "":
		return i18n.T("Every platform")
	case vm.ISOPlatformLinux:
		return i18n.T("Linux")
	case vm.ISOPlatformBSD:
		return i18n.T("BSD")
	case vm.ISOPlatformWindows:
		return i18n.T("Windows")
	}
	return i18n.T("Other")
}

// isoArchText names an architecture family for the filter. The families are the
// database's own words - x86, arm64, riscv64 - and need no translation.
func isoArchText(family string) string {
	switch family {
	case vm.ISOArchFamilyX86:
		return "x86"
	case vm.ISOArchFamilyARM:
		return "arm64"
	}
	return family
}

// isoPlatformIcon is a system's mark in the list, from the platform it belongs
// to: the shell installs a mark for each of the machines' systems and they fit
// these four exactly.
func isoPlatformIcon(platform string) string {
	switch platform {
	case vm.ISOPlatformLinux:
		return "vm-os-linux-symbolic"
	case vm.ISOPlatformBSD:
		return "vm-os-freebsd-symbolic"
	case vm.ISOPlatformWindows:
		return "vm-os-windows-symbolic"
	}
	return "vm-os-custom-symbolic"
}

// isoMonthText is the month a system was released in - "2026-04" - which is
// what a row says beside its name instead of a date nobody reads.
func isoMonthText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01")
}

// isoMediaRowTitle is a medium's own line inside its system: the variant the
// database names, or the file's own name when it names none. The system's name
// is the row above, and three installers of one system are told apart by their
// files - the minimal, the dvd, the boot one - rather than by repeating it.
func isoMediaRowTitle(m vm.ISOMedia) string {
	if m.Variant != "" {
		return m.Variant
	}
	if base := path.Base(strings.TrimSpace(m.URL)); base != "" && base != "." && base != "/" {
		return base
	}
	if m.Live {
		return i18n.T("Live image")
	}
	return i18n.T("Installer")
}

// isoSystemSummary is what a system's row says under its name: what the filters
// left of it - how many images install it, how many are live images, and when
// it was released.
func isoSystemSummary(s vm.ISOSystem) string {
	var parts []string
	if n := s.Installers(); n > 0 {
		parts = append(parts, i18n.Nf("%d installer", "%d installers", n, n))
	}
	if n := s.Live(); n > 0 {
		parts = append(parts, i18n.Nf("%d live image", "%d live images", n, n))
	}
	if !s.Newest.IsZero() {
		parts = append(parts, i18n.Tf("newest %s", isoMonthText(s.Newest)))
	}
	if len(parts) == 0 {
		return i18n.Nf("%d install image", "%d install images", len(s.Media), len(s.Media))
	}
	return strings.Join(parts, " · ")
}

// isoCountText is the section's own line: what the filters left, counted the
// way the list is arranged - systems first, then the images inside them.
func isoCountText(systems, media int) string {
	return i18n.Tf("%s · %s", i18n.Nf("%d system", "%d systems", systems, systems),
		i18n.Nf("%d install image", "%d install images", media, media))
}

// fillISOSystems builds the download list: one row per system, which opens to
// its own media. The rows are the window's slowest thing to make and the list
// only changes with the filters and the files on disk, so they are rebuilt when
// that is what changed and left alone otherwise.
func (v *vmWindow) fillISOSystems(recommended, systems []ISOSystemRow) {
	if isoSystemsSame(v.isoRecommended, recommended) && isoSystemsSame(v.isoSystems, systems) {
		return
	}
	v.isoRecommended = append(v.isoRecommended[:0], recommended...)
	v.isoSystems = append(v.isoSystems[:0], systems...)
	v.isoButtons = nil
	clear(v.isoRevealer)
	clear(v.isoMark)
	fill := func(box *gtk.Box, list []ISOSystemRow) {
		gtkx.Clear(box)
		for _, s := range list {
			gtkx.Append(box, &v.buildISOSystem(s).Widget)
		}
	}
	fill(&v.mediaRec, v.isoRecommended)
	fill(&v.mediaRows, v.isoSystems)
}

// isoSystemsSame says whether two lists of systems would build the same
// widgets: the same systems in the same order, with the same media rows.
func isoSystemsSame(a, b []ISOSystemRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].System.OSID != b[i].System.OSID {
			return false
		}
		if !isoRowsSame(a[i].Rows, b[i].Rows) {
			return false
		}
	}
	return true
}

// buildISOSystem is one system's row: its mark, its name, what it holds, and a
// button that opens and closes the media inside it. The open state lives in the
// window, so a repaint does not close what the user opened.
func (v *vmWindow) buildISOSystem(s ISOSystemRow) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	box.AddCssClass("vm-iso-system")
	open := v.isoOpen[s.System.OSID]

	head := gtk.NewButton()
	head.AddCssClass("plain")
	head.AddCssClass("vm-iso-system-head")
	head.SetName("iso_system:" + s.System.OSID)
	head.ConnectClicked(&v.onISOSystem)

	headBox := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	mark := gtk.NewImageFromIconName(isoPlatformIcon(s.System.Platform))
	mark.AddCssClass("vm-os-mark")
	mark.SetValign(gtk.AlignCenterValue)
	gtkx.Append(headBox, &mark.Widget)

	text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	text.SetHexpand(true)
	text.SetValign(gtk.AlignCenterValue)
	title := gtk.NewLabel(s.System.OSName)
	title.AddCssClass("label")
	title.SetXalign(0)
	sub := gtk.NewLabel(isoSystemSummary(s.System))
	sub.AddCssClass("sub")
	sub.SetXalign(0)
	sub.SetEllipsize(pango.EllipsizeEndValue)
	gtkx.Append(text, &title.Widget)
	gtkx.Append(text, &sub.Widget)
	gtkx.Append(headBox, &text.Widget)

	chevron := gtk.NewImageFromIconName("pan-end-symbolic")
	chevron.AddCssClass("vm-iso-chevron")
	chevron.SetValign(gtk.AlignCenterValue)
	if open {
		chevron.SetFromIconName("pan-down-symbolic")
	}
	gtkx.Append(headBox, &chevron.Widget)
	head.SetChild(&headBox.Widget)
	v.isoMark[s.System.OSID] = append(v.isoMark[s.System.OSID], *chevron)
	gtkx.Append(box, &head.Widget)

	revealer := gtk.NewRevealer()
	revealer.SetTransitionType(gtk.RevealerTransitionTypeSlideDownValue)
	revealer.SetRevealChild(open)
	body := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	body.AddCssClass("vm-iso-media")
	for _, r := range s.Rows {
		gtkx.Append(body, &v.isoMediaRow(r).Widget)
	}
	revealer.SetChild(&body.Widget)
	v.isoRevealer[s.System.OSID] = append(v.isoRevealer[s.System.OSID], *revealer)
	gtkx.Append(box, &revealer.Widget)
	return box
}

// isoMediaRow is one medium inside an opened system: the variant's own name,
// what the medium is - architecture, live image or installer, its size when the
// database knows it, how the file already stands here - and the one action it
// allows.
func (v *vmWindow) isoMediaRow(r ISORow) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	row.AddCssClass("row")
	row.AddCssClass("vm-iso-media-row")

	text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	text.SetHexpand(true)
	text.SetValign(gtk.AlignCenterValue)
	title := gtk.NewLabel(isoMediaRowTitle(r.Media))
	title.AddCssClass("label")
	title.SetXalign(0)
	sub := gtk.NewLabel(isoMediaSub(r))
	sub.AddCssClass("sub")
	sub.SetXalign(0)
	sub.SetWrap(true)
	gtkx.Append(text, &title.Widget)
	gtkx.Append(text, &sub.Widget)
	// A download that failed says so in the downloader's own words: it is
	// where the explanation is - how much room is missing, which answer
	// the server gave - and a bare "failed" would throw it away.
	if r.Err != nil {
		reason := gtk.NewLabel(r.Err.Error())
		reason.AddCssClass("vm-media-error")
		reason.SetXalign(0)
		reason.SetWrap(true)
		gtkx.Append(text, &reason.Widget)
	}
	gtkx.Append(row, &text.Widget)

	btn := gtk.NewButtonWithLabel(isoActionText(r))
	btn.AddCssClass("outlined")
	btn.SetValign(gtk.AlignCenterValue)
	btn.SetName("iso_fetch")
	btn.SetSensitive(!r.Downloading)
	v.isoButtons = append(v.isoButtons, isoTarget{btn: *btn, media: r.Media})
	btn.ConnectClicked(&v.onClick)
	gtkx.Append(row, &btn.Widget)
	return row
}

// openISOSystem opens or closes one system's media. The media rows are built
// with the system, so this is only the revealers and the chevrons: a
// recommended system is in both lists, and opens in both.
func (v *vmWindow) openISOSystem(id string, open bool) {
	v.isoOpen[id] = open
	for _, r := range v.isoRevealer[id] {
		r.SetRevealChild(open)
	}
	name := "pan-end-symbolic"
	if open {
		name = "pan-down-symbolic"
	}
	for _, m := range v.isoMark[id] {
		m.SetFromIconName(name)
	}
	if !open {
		delete(v.isoOpen, id)
	}
}

// isoFiltersFromControls is what the filter controls currently say.
func (v *vmWindow) isoFiltersFromControls() vm.ISOFilters {
	f := v.isoFilter
	f.Search = v.isoSearch.GetText()
	if i := int(v.isoPlatform.GetSelected()); i >= 0 && i < len(isoPlatformChoices) {
		f.Platform = isoPlatformChoices[i]
	}
	// A fresh GtkStringList has no selection until GTK has chosen one, and its
	// invalid position must leave the filter as it is rather than reading as
	// "every architecture"; index 0 is the "every" entry and the rest are the
	// families the catalog holds.
	switch i := int(v.isoArch.GetSelected()); {
	case i > 0 && i <= len(v.isoArchs):
		f.ArchFamily = v.isoArchs[i-1]
	case i == 0:
		f.ArchFamily = ""
	}
	f.IncludePrerelease = v.isoPre.GetActive()
	f.IncludeEnded = v.isoEOL.GetActive()
	return f
}

// applyISOFilters hands the window's own filter to the service and paints
// again: the list is built where a machine's view is built, so the choice
// travels through the service rather than being applied to the widgets.
func (v *vmWindow) applyISOFilters() {
	if v.filling {
		return
	}
	v.isoFilter = v.isoFiltersFromControls()
	v.manager.SetISOFilters(v.isoFilter)
	v.paintAll(v.manager.Machines())
}

// isoRowsSame says whether two lists of rows would build the same widgets: the
// same media in the same order, and the same state of their files.
func isoRowsSame(a, b []ISORow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.Media.URL != y.Media.URL || x.Cached != y.Cached || x.Partial != y.Partial ||
			x.Checksum != y.Checksum || x.Downloading != y.Downloading || (x.Err == nil) != (y.Err == nil) {
			return false
		}
	}
	return true
}

// showProvisioning fills the page shown while a machine's guest is being
// installed: the image is being pulled, the system is installing, or it is
// stopped in the middle of it. While it runs, the page offers watching it and
// pausing it; while it is paused, going on, starting over or giving up.
func (v *vmWindow) showProvisioning(s MachineView) {
	showProgress(&v.progress, &v.progressMeta, s.Progress)
	switch {
	case s.ProgressText != "":
		v.progressText.SetText(s.ProgressText)
	case s.State == vm.StateStarting, s.State == vm.StateRunning:
		v.progressText.SetText(i18n.T("The system is installing; this takes a while."))
	case s.State == vm.StateStopping:
		v.progressText.SetText(i18n.T("Stopping…"))
	case s.Busy || v.operationBusy:
		v.progressText.SetText(i18n.T("Starting…"))
	default:
		v.progressText.SetText(i18n.T("The installation is paused. Continue to resume."))
	}
	up := s.State == vm.StateRunning || s.State == vm.StateStarting
	// While a stop runs, the running half stays: the page does not swap its
	// rows under the pointer.
	paused := !up && !(s.Busy && v.waitButton == &v.provStop)
	idle := !s.Busy && !v.operationBusy
	if paused {
		v.provWatchRow.SetVisible(false) // setWatchRows shows it when there is something to watch
	}
	// A paused installation measures nothing: its line says so, without a bar.
	v.progress.SetVisible(!paused)
	v.progressMeta.SetVisible(!paused)
	v.provStopRow.SetVisible(!paused)
	v.provStartRow.SetVisible(paused)
	v.provRemoveRow.SetVisible(paused)
	v.provDeleteRow.SetVisible(paused)
	v.provStart.SetSensitive(idle)
	v.provStop.SetSensitive(idle)
	v.console.SetSensitive(idle && up)
	v.provRemove.SetSensitive(idle)
	v.provDelete.SetSensitive(idle)
	v.provDoneRow.SetVisible(s.Machine.Backend == vm.BackendQEMU && strings.TrimSpace(s.Machine.Boot.ISO) != "")
	v.provDone.SetSensitive(idle)
}

// showStopped fills the page shown when the guest is installed and off: start
// it, change its memory or CPU count, or remove it.
func (v *vmWindow) showStopped(s MachineView) {
	m := s.Machine
	v.readySummary.SetText(vmProfileText(m))
	if !v.readyFilled {
		v.filling = true
		v.readyMemory.SetValue(float64(gibOf(m.MemoryMiB)))
		v.readyCPUs.SetValue(float64(m.CPUs))
		v.filling = false
		v.readyFilled = true
	}
	v.diskSize.SetText(fmt.Sprintf("%d GiB", m.DiskGiB))
	idle := !s.Busy && !v.operationBusy
	v.start.SetSensitive(idle)
	v.readyMemory.SetSensitive(idle)
	v.readyCPUs.SetSensitive(idle)
	v.showApply(s)
	v.remove.SetSensitive(idle)
	v.removeData.SetSensitive(idle)
	v.delete.SetSensitive(idle)
}

// showStarting fills the page shown while an installed guest is coming up.
func (v *vmWindow) showStarting(s MachineView) {
	v.bootSummary.SetText(vmProfileText(s.Machine))
	if s.Progress > 0 {
		v.bootProgress.SetFraction(s.Progress)
	} else {
		v.bootProgress.Pulse()
	}
	switch {
	case s.ProgressText != "":
		v.bootProgressText.SetText(s.ProgressText)
	case s.State == vm.StateStopping:
		v.bootProgressText.SetText(i18n.T("Stopping…"))
	default:
		v.bootProgressText.SetText(i18n.T("The system is starting; ready in a minute or two."))
	}
	idle := !s.Busy && !v.operationBusy
	v.bootConsole.SetSensitive(idle)
	v.bootStop.SetSensitive(idle)
}

// showRunning fills the page shown while the guest is up: see it, or stop it.
func (v *vmWindow) showRunning(s MachineView) {
	v.runSummary.SetText(vmProfileText(s.Machine))
	idle := !s.Busy && !v.operationBusy
	v.connect.SetSensitive(idle)
	v.consoleReady.SetSensitive(idle)
	v.stop.SetSensitive(idle)
}

// clicked dispatches the window's buttons by their id, one handler for all of
// them (puregotk makes one native trampoline per function). An action names
// where its error goes and what its button says while it runs; what cannot be
// undone is asked first.
func (v *vmWindow) clicked(b gtk.Button) {
	name := v.selectedName()
	switch b.GetName() {
	case "new_machine":
		v.showNew()
	case "new_cancel":
		v.closeNew()
	case "new_next":
		v.newStep("resources")
	case "new_back":
		v.newStep("what")
	case "create_machine":
		v.createMachine()
	case "accept":
		v.acceptProfile()
	case "ready_save":
		m := v.readyMachine()
		v.run(func() error { return v.manager.Save(m) }, &v.readyError, &v.readySave, i18n.T("Applying…"))
	case "install":
		m := v.setupMachine()
		v.run(func() error { return v.manager.Install(m) }, &v.setupError, &v.install, i18n.T("Starting…"))
	case "iso_choose":
		v.chooseISO()
	case "iso_browse":
		v.openISODialog()
	case "iso_close":
		v.isoDialog.SetVisible(false)
	case "iso_fetch":
		if media, ok := v.isoMediaOf(b); ok {
			v.isoDialog.SetVisible(false)
			v.fetchISO(media)
		}
	case "prov_done":
		v.run(func() error { return v.manager.SetISO(name, "") }, &v.provError, &v.provDone, i18n.T("Finishing…"))
	case "iso_cancel":
		// Stopping is a cancellation, not an operation: it only reaches the
		// goroutine that is downloading, so the window keeps painting.
		v.manager.CancelISO(name)
	case "start":
		v.run(func() error { return v.manager.Start(name) }, &v.startError, &v.start, i18n.T("Starting…"))
	case "prov_start":
		v.run(func() error { return v.manager.Start(name) }, &v.provError, &v.provStart, i18n.T("Starting…"))
	case "connect":
		v.watch(name, &v.connect, &v.runError, false)
	case "console":
		v.watch(name, &v.console, &v.provError, true)
	case "boot_console":
		v.watch(name, &v.bootConsole, &v.bootError, true)
	case "console_ready":
		v.watch(name, &v.consoleReady, &v.runError, true)
	case "console_back":
		v.stopSerial()
		v.consoleOpen = false
		v.paint()
	case "console_copy":
		v.copyDebuggerLine()
	case "console_start":
		v.run(func() error { return v.manager.Start(name) }, &v.consoleError, &v.consoleStart, i18n.T("Starting…"))
	case "console_stop":
		v.run(func() error { return v.manager.Stop(name) }, &v.consoleError, &v.consoleStop, i18n.T("Stopping…"))
	case "prov_stop":
		v.run(func() error { return v.manager.Stop(name) }, &v.provError, &v.provStop, i18n.T("Stopping…"))
	case "boot_stop":
		v.run(func() error { return v.manager.Stop(name) }, &v.bootError, &v.bootStop, i18n.T("Stopping…"))
	case "stop":
		v.run(func() error { return v.manager.Stop(name) }, &v.runError, &v.stop, i18n.T("Stopping…"))
	case "remove":
		v.confirm(i18n.T("Remove the container?"), name,
			i18n.T("Its container and settings go. The disk and its password stay, so installing again starts from them."),
			i18n.T("Remove"), false, func() {
				v.run(func() error { return v.manager.Remove(name) }, &v.removeError, &v.remove, i18n.T("Removing…"))
			})
	case "prov_remove":
		if v.viewOf(name).Machine.Backend == vm.BackendDockur {
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
	case "remove_data":
		v.confirm(i18n.T("Delete this machine's disk?"), name,
			i18n.T("The installed system and its files are deleted for good. The machine stays, to install again."),
			i18n.T("Delete"), true, func() {
				v.run(func() error { return v.manager.DeleteDisk(name) }, &v.removeError, &v.removeData, i18n.T("Deleting…"))
			})
	case "setup_remove_data":
		v.confirm(i18n.T("Delete the files from the earlier attempt?"), name,
			i18n.T("The downloaded files and the unfinished disk are deleted for good."),
			i18n.T("Delete"), true, func() {
				v.run(func() error { return v.manager.DeleteDisk(name) }, &v.setupRemoveError, &v.setupRemoveData, i18n.T("Deleting…"))
			})
	case "setup_delete", "prov_delete", "delete":
		at, btn := &v.removeError, &v.delete
		switch b.GetName() {
		case "setup_delete":
			at, btn = &v.setupRemoveError, &v.setupDelete
		case "prov_delete":
			at, btn = &v.provError, &v.provDelete
		}
		v.confirm(i18n.T("Delete this machine?"), name,
			i18n.T("The machine, its disk and all its files are deleted for good. This cannot be undone."),
			i18n.T("Delete"), true, func() {
				v.run(func() error { return v.manager.Delete(name) }, at, btn, i18n.T("Deleting…"))
			})
	case "fault_delete":
		path := name
		for _, f := range v.faults {
			if f.Name == name {
				path = f.Path
			}
		}
		v.confirm(i18n.T("Delete this folder?"), path,
			i18n.T("It does not load as a machine. Everything inside it is deleted for good."),
			i18n.T("Delete"), true, func() {
				v.run(func() error { return v.manager.DeleteFault(name) }, &v.faultActionError, &v.faultDelete, i18n.T("Deleting…"))
			})
	case "assistant_ask":
		v.askAboutTheHost()
	case "assistant_recheck":
		v.recheck()
	case "fix":
		for i := range v.fixButtons {
			if t := &v.fixButtons[i]; t.btn.GoPointer() == b.GoPointer() {
				v.applyFix(t)
				return
			}
		}
	}
}

// askAboutTheHost hands the host's missing pieces to inua's own assistant: the
// window runs no package manager and no privileged install, and what the
// assistant changes is its own, approved, decision. The prompt is built here,
// where the machine and the checks are, and the assistant's window - running
// or started by this - is where it goes.
func (v *vmWindow) askAboutTheHost() {
	name := v.selectedName()
	if name == "" {
		return
	}
	prompt := assistantPrompt(v.viewOf(name))
	if err := askAssistant(prompt); err != nil {
		v.errs[name] = actionErr{at: &v.assistantError, err: err}
	} else {
		delete(v.errs, name)
		v.assistantNote.SetText(i18n.T("Request sent to the assistant window."))
	}
	v.paint()
}

// recheck reads the host again, so a fix made elsewhere shows up here: the
// page moves on by itself once every check this machine needs passes.
func (v *vmWindow) recheck() {
	if v.closed || v.operationBusy {
		return
	}
	v.operationBusy = true
	v.assistantRecheck.SetLabel(i18n.T("Checking…"))
	v.paint()
	go func() {
		views := v.manager.RefreshAll()
		gtkx.Post(func() {
			v.operationBusy = false
			v.assistantRecheck.SetLabel(i18n.T("Check again"))
			v.assistantNote.SetText(v.assistantHint)
			v.paintAll(views)
		})
	}()
}

// applyFix runs a remedy. The polkit dialog it may open lives on the main
// thread, so the work goes to a goroutine (as the other actions do) and the
// result comes back under the checks.
func (v *vmWindow) applyFix(t *fixTarget) {
	if v.operationBusy {
		return
	}
	v.operationBusy = true
	r := t.remedy
	t.btn.SetLabel(i18n.T("Asking…"))
	for _, other := range v.fixButtons {
		other.btn.SetSensitive(false)
	}
	go func() {
		_, relogin, err := v.manager.Fix(r)
		views := v.manager.RefreshAll()
		gtkx.Post(func() {
			v.operationBusy = false
			v.fixFailed = err != nil
			switch {
			case err != nil:
				v.fixNote = i18n.Tf("The fix did not work: %v", err)
			case relogin:
				// A process's group list is fixed when it starts, so the
				// change is real but this session cannot see it yet: say so
				// under the checks and in a notification.
				v.fixNote = i18n.T("Done. Log out and back in for it to take effect.")
				v.askRelogin()
			default:
				v.fixNote = i18n.T("Done.")
			}
			// The rows are built again, with their buttons as they were.
			v.lastChecks = nil
			v.paintAll(views)
		})
	}()
}

// run performs a machine's action off the GTK thread and repaints what it left
// behind: the state of every machine may have moved (a start, a removal). The
// button that started it says what it waits for meanwhile, and a failure
// shows at errAt, under that button's section; the failure from before is
// cleared, since the user is trying again.
func (v *vmWindow) run(f func() error, errAt *gtk.Label, button *gtk.Button, waiting string) {
	name := v.selectedName()
	if v.closed || v.operationBusy || name == "" {
		return
	}
	v.operationBusy = true
	delete(v.errs, name)
	if button != nil {
		v.waitButton, v.waitLabel = button, button.GetLabel()
		button.SetLabel(waiting)
	}
	v.paint()
	go func() {
		err := f()
		views := v.manager.RefreshAll()
		gtkx.Post(func() {
			v.operationBusy = false
			if v.waitButton != nil {
				v.waitButton.SetLabel(v.waitLabel)
				v.waitButton = nil
			}
			if err != nil {
				v.errs[name] = actionErr{at: errAt, err: err}
			}
			v.paintAll(views)
		})
	}()
}

// fetchISO downloads an install medium and hands the machine over to the
// install flow: the service does the transfer off the GTK thread, and the same
// watch that follows a machine's state follows its download, so a repaint
// every two seconds shows how far it has got. The transfer is not an action in
// the window's own sense - it is stopped by its own button, not cancelled by a
// click elsewhere - so the two are kept apart.
func (v *vmWindow) fetchISO(media vm.ISOMedia) {
	name := v.selectedName()
	if v.closed || v.operationBusy || name == "" {
		return
	}
	v.operationBusy = true
	delete(v.errs, name)
	v.paint()
	go func() {
		err := v.manager.FetchISO(name, media)
		views := v.manager.RefreshAll()
		gtkx.Post(func() {
			v.operationBusy = false
			// The user's own stop is not a failure: the part file is still
			// there, and the row offers to resume it.
			if err != nil && !errors.Is(err, context.Canceled) {
				v.errs[name] = actionErr{at: &v.mediaError, err: err}
			}
			v.paintAll(views)
		})
	}()
}

// isoMediaOf is the medium a download button stands for.
func (v *vmWindow) isoMediaOf(b gtk.Button) (vm.ISOMedia, bool) {
	for _, t := range v.isoButtons {
		if t.btn.GoPointer() == b.GoPointer() {
			return t.media, true
		}
	}
	return vm.ISOMedia{}, false
}

// display is the monitor the manager window is on: the screen on which its
// Connect opens FreeRDP, so the guest follows that screen's scale. Without a
// monitor to read, FreeRDP keeps its own resolution (rdpArgs).
func (v *vmWindow) display() *rdpDisplay {
	if v.win == nil {
		return nil
	}
	surface := v.win.GetSurface()
	if surface == nil {
		return nil
	}
	defer surface.Unref()
	gdkDisplay := surface.GetDisplay()
	if gdkDisplay == nil {
		return nil
	}
	defer gdkDisplay.Unref()
	mon := gdkDisplay.GetMonitorAtSurface(surface)
	if mon == nil {
		return nil
	}
	defer mon.Unref()
	var rect gdk.Rectangle
	mon.GetGeometry(&rect)
	return &rdpDisplay{Width: int(rect.Width), Height: int(rect.Height), Scale: float64(mon.GetScaleFactor())}
}

// askRelogin says, in a notification, that the change needs a fresh login.
// Logging out is the shell's to do — Hyprland 0.55 runs a Lua configuration and
// rejects the classic dispatcher, and the shell already knows which form its
// compositor takes — so this points at the control center's power row rather
// than ending the session from here.
func (v *vmWindow) askRelogin() {
	if v.c == nil || v.c.App == nil {
		return
	}
	n := gio.NewNotification(i18n.T("Log out to finish"))
	n.SetBody(i18n.T("The change takes effect the next time you log in."))
	v.c.App.SendNotification("vm-relogin", n)
	n.Unref()
}

// setupMachine is the chosen machine with the setup page's editors applied.
func (v *vmWindow) setupMachine() vm.Machine {
	m := v.viewOf(v.selectedName()).Machine
	if gib := int(v.memory.GetValueAsInt()); gib != gibOf(m.MemoryMiB) {
		m.MemoryMiB = gib * 1024
	}
	m.CPUs = int(v.cpus.GetValueAsInt())
	m.DiskGiB = int(v.disk.GetValueAsInt())
	return m
}

// readyMachine is the chosen machine with the stopped page's editors applied.
// The disk keeps its size: it is the one field that cannot change under an
// installed guest. A memory size the editor only rounded keeps its own.
func (v *vmWindow) readyMachine() vm.Machine {
	m := v.viewOf(v.selectedName()).Machine
	if gib := int(v.readyMemory.GetValueAsInt()); gib != gibOf(m.MemoryMiB) {
		m.MemoryMiB = gib * 1024
	}
	m.CPUs = int(v.readyCPUs.GetValueAsInt())
	return m
}

// acceptProfile sets the editors to what suits this computer.
func (v *vmWindow) acceptProfile() {
	r := v.viewOf(v.selectedName()).Recommended
	v.memory.SetValue(float64(gibOf(r.MemoryMiB)))
	v.cpus.SetValue(float64(r.CPUs))
	v.disk.SetValue(float64(r.DiskGiB))
	v.setupFilled = true
}

// vmStateText is the line under the machine's name: what it is doing. Errors
// are not said here but under the section they belong to.
func vmStateText(s MachineView) string {
	switch s.Page {
	case vm.PagePrereq:
		return i18n.T("This system is missing something the guest needs")
	case vm.PageSetup:
		if s.Leftovers {
			return i18n.T("Files from a previous attempt are still here")
		}
		return i18n.T("Not installed yet")
	case vm.PageProvisioning:
		if s.State == vm.StateStopping {
			return i18n.T("Stopping")
		}
		if s.State == vm.StateStarting || s.State == vm.StateRunning || s.Busy {
			return i18n.T("Installing")
		}
		return i18n.T("Installation paused")
	case vm.PageStarting:
		if s.State == vm.StateStopping {
			return i18n.T("Stopping")
		}
		return i18n.T("Starting")
	case vm.PageRunning:
		return i18n.T("Running")
	case vm.PageStopped:
		return i18n.T("Stopped")
	}
	return i18n.T("Virtual machines")
}

// vmListSub is a machine's line in the list: what it is, and what it is doing.
func vmListSub(s MachineView) string {
	if !s.Ready {
		return i18n.Tf("%s · no backend in this build", vmOSText(s.Machine.OS))
	}
	return vmOSText(s.Machine.OS) + " · " + vmShortState(s)
}

// vmShortState is one or two words for the list, where the headline's sentence
// would not fit.
func vmShortState(s MachineView) string {
	if s.Busy {
		return i18n.T("Working")
	}
	switch s.Page {
	case vm.PagePrereq:
		return i18n.T("Needs the host")
	case vm.PageSetup:
		return i18n.T("Not installed")
	case vm.PageProvisioning:
		switch s.State {
		case vm.StateStopping:
			return i18n.T("Stopping")
		case vm.StateStarting, vm.StateRunning:
			return i18n.T("Installing")
		}
		return i18n.T("Installation paused")
	case vm.PageStarting:
		return i18n.T("Starting")
	case vm.PageRunning:
		return i18n.T("Running")
	case vm.PageStopped:
		return i18n.T("Stopped")
	}
	return i18n.T("Unknown")
}

// vmOSText names a system the way the window says it. The systems' own names
// are proper nouns and stay as they are: the catalog already translates the
// word "windows" as the thing a program draws in, which is not this.
func vmOSText(os vm.OS) string {
	switch os {
	case vm.OSWindows:
		return "Windows"
	case vm.OSLinux:
		return "Linux"
	case vm.OSFreeBSD:
		return "FreeBSD"
	case vm.OSAndroid:
		return "Android"
	case vm.OSCustom:
		return i18n.T("Custom")
	}
	return string(os)
}

// vmBackendText names a backend. QEMU, Dockur and Waydroid are their own names
// too, and only the phrase around them is translated.
func vmBackendText(b vm.BackendKind) string { return string(b) }

// vmOSIcon is a system's mark, from the icons the shell installs.
func vmOSIcon(os vm.OS) string {
	switch os {
	case vm.OSWindows:
		return "vm-os-windows-symbolic"
	case vm.OSLinux:
		return "vm-os-linux-symbolic"
	case vm.OSFreeBSD:
		return "vm-os-freebsd-symbolic"
	case vm.OSAndroid:
		return "vm-os-android-symbolic"
	}
	return "vm-os-custom-symbolic"
}

// vmInstallNote says what installing this machine costs, which is not the same
// for a container that downloads Windows and a system booted off an ISO.
func vmInstallNote(s MachineView) string {
	switch s.Machine.Backend {
	case vm.BackendDockur:
		return i18n.T("Downloads about 8 GB and installs Windows; takes 30+ minutes. Windows needs a licence.")
	default:
		return i18n.T("The guest installs from its own media, as it would on a real machine.")
	}
}

// isoSourceText names where the download list came from. osinfo-db is a data
// package's own name and stays as it is; a list inua wrote itself is said so,
// because it is short and ages.
func isoSourceText(source string) string {
	if source == vm.ISOSourceBuiltin {
		return i18n.T("inua's own list")
	}
	return "osinfo-db"
}

// vmMediaNote is the sentence above the download list: what the machine
// installs from, or what to do while there is nothing yet.
func vmMediaNote(s MachineView) string {
	if iso := strings.TrimSpace(s.Machine.Boot.ISO); iso != "" {
		if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(iso, home+"/") {
			iso = "~" + strings.TrimPrefix(iso, home)
		}
		return i18n.Tf("Installs from %s.", iso)
	}
	return i18n.T("A file of your own, or one to download; the machine boots it as a disc.")
}

// isoDialogNote is the sentence under the download dialog's title.
func isoDialogNote(source string) string {
	if source == vm.ISOSourceBuiltin {
		return i18n.T("osinfo-db is not installed, so this is inua's short built-in list.")
	}
	return i18n.T("Pick a system, then one of its images. It is downloaded once and becomes this machine's install image.")
}

// isoMediaTitle is a medium's own line: the system, and the variant when the
// database names one that adds something to the system's name.
func isoMediaTitle(m vm.ISOMedia) string {
	if m.Variant == "" || m.Variant == m.OSName {
		return m.OSName
	}
	return m.OSName + " · " + m.Variant
}

// isoMediaSub is what a row says under the medium: the architecture, whether
// it is a live image or an installer, its size when the database knows it, and
// how the file already stands here.
func isoMediaSub(r ISORow) string {
	parts := []string{r.Media.Arch}
	switch {
	case r.Media.Live:
		parts = append(parts, i18n.T("Live image"))
	case r.Media.Installer:
		parts = append(parts, i18n.T("Installer"))
	}
	if r.Media.VolumeSize > 0 {
		parts = append(parts, isoBytesText(r.Media.VolumeSize))
	}
	switch {
	case r.Cached:
		// An image that is here says how it got here: verified against a
		// published sum, with none published, or already in the cache from
		// before, which nothing was downloaded for and nothing verified.
		text := isoChecksumText(r.Checksum)
		if text == "" {
			text = i18n.T("Already in the cache")
		}
		parts = append(parts, text)
	case r.Partial:
		parts = append(parts, i18n.T("Unfinished download"))
	}
	return strings.Join(parts, " · ")
}

// isoActionText is what a row's button does next: download the image, resume
// an unfinished one, use the one already here, or try again after a failure.
func isoActionText(r ISORow) string {
	switch {
	case r.Err != nil:
		return i18n.T("Retry")
	case r.Cached:
		return i18n.T("Use this image")
	case r.Partial:
		return i18n.T("Resume")
	}
	return i18n.T("Download")
}

// isoChecksumText words how the published checksum of a finished download
// turned out. Most media publish none, which is not a failure: the file is
// trusted because it came from the distribution's own URL over HTTPS. An image
// that was already in the cache was not downloaded here and so was not checked.
func isoChecksumText(c vm.ISOChecksum) string {
	switch c {
	case vm.ISOChecksumVerified:
		return i18n.T("Checksum verified")
	case vm.ISOChecksumNotPublished:
		return i18n.T("No published checksum")
	case vm.ISOChecksumNotChecked:
		return i18n.T("Already in the cache")
	}
	return ""
}

// isoBytesText is a byte count the way a size reads on this page.
func isoBytesText(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.0f KiB", float64(n)/(1<<10))
}

// vmWayName is how a template's system runs: the backend, said as what it
// is. The backends' names are their own.
func vmWayName(t vm.Template) string {
	switch t.Backend {
	case vm.BackendDockur:
		return i18n.Tf("In a container, with %s", "Dockur")
	case vm.BackendWaydroid:
		return i18n.Tf("In a container, with %s", "Waydroid")
	case vm.BackendQEMU:
		return i18n.Tf("In a virtual machine, with %s", "QEMU")
	}
	return vmBackendText(t.Backend)
}

// vmTemplateText says what a way of running a system gives, in one line.
func vmTemplateText(t vm.Template) string {
	switch t.ID {
	case "windows-dockur":
		return i18n.T("Downloads and installs Windows 11 for you; you reach it over Remote Desktop.")
	case "windows-qemu":
		return i18n.T("Boots your own Windows 11 ISO, with the virtio drivers.")
	case "linux", "freebsd":
		return i18n.T("Boots the install image you pick next, with 3D where the guest has a driver.")
	case "android":
		return i18n.T("Android beside your apps, with the host's display and GPU.")
	case "custom":
		return i18n.T("Your own system: an ISO, a disk or a kernel, on any architecture.")
	}
	return string(t.Backend)
}

// vmProfileText is a machine's resources, as the section meta reads them.
func vmProfileText(m vm.Machine) string {
	return fmt.Sprintf("%s · %d CPU · %d GiB", humanMiB(m.MemoryMiB), m.CPUs, m.DiskGiB)
}

// humanMiB shows a memory size the way the other windows do.
func humanMiB(mib int) string {
	if mib >= 1024 {
		return fmt.Sprintf("%d GiB", mib/1024)
	}
	return fmt.Sprintf("%d MiB", mib)
}

func doctorItemsEqual(a, b []vm.DoctorItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].OK != b[i].OK || a[i].Reason != b[i].Reason {
			return false
		}
		ar, br := a[i].Remedy, b[i].Remedy
		if (ar == nil) != (br == nil) {
			return false
		}
		if ar != nil && ar.Group != br.Group {
			return false
		}
	}
	return true
}

// actionErr is an action's failure and the label under the section of the
// button that started it.
type actionErr struct {
	at  *gtk.Label
	err error
}

// pageOf is the page the window shows, and the machine it is about.
func (v *vmWindow) pageOf() (string, MachineView) {
	name := v.selectedName()
	switch {
	case name == "":
		return "empty", MachineView{}
	case v.hasFault(name):
		return "fault", MachineView{Name: name}
	}
	view := v.viewOf(name)
	switch {
	case !view.Ready:
		return "unavailable", view
	case v.consoleOpen:
		return "console", view
	}
	return string(view.Page), view
}

// errorLabels are the places an error can show, by page; a page's first is
// where an error goes that none of its own buttons started (the machine's
// state could not be read, or the action moved the page on).
func (v *vmWindow) errorLabels() []struct {
	page  string
	label *gtk.Label
} {
	return []struct {
		page  string
		label *gtk.Label
	}{
		{"fault", &v.faultActionError},
		{"console", &v.consoleError},
		{string(vm.PagePrereq), &v.assistantError},
		{string(vm.PageSetup), &v.setupError}, {string(vm.PageSetup), &v.mediaError},
		{string(vm.PageSetup), &v.setupRemoveError},
		{string(vm.PageProvisioning), &v.provError},
		{string(vm.PageStarting), &v.bootError},
		{string(vm.PageStopped), &v.startError}, {string(vm.PageStopped), &v.readyError},
		{string(vm.PageStopped), &v.removeError},
		{string(vm.PageRunning), &v.runError},
	}
}

// showError puts the machine's last failure under the section whose action
// failed, or the page's first section when that one is not on this page, and
// hides the others. The window's own record of an action outranks what the
// service last saw.
func (v *vmWindow) showError(page string, view MachineView) {
	e := v.errs[view.Name]
	err, want := e.err, e.at
	if err == nil {
		err, want = view.Err, nil
	}
	var at *gtk.Label
	for _, l := range v.errorLabels() {
		if err != nil && l.page == page && (at == nil || l.label == want) {
			at = l.label
		}
	}
	for _, l := range v.errorLabels() {
		if l.label == at {
			l.label.SetText(i18n.Tf("It did not work: %s", err.Error()))
			l.label.SetVisible(true)
		} else {
			l.label.SetVisible(false)
		}
	}
}

// focusMain gives a page's main action the focus when the page comes up, so
// Enter does what the page is for. Without it GTK focuses the first thing
// that takes focus, an editor or a line to copy, with its text selected.
func (v *vmWindow) focusMain(page string) {
	var main *gtk.Button
	switch page {
	case "empty":
		main = &v.emptyNew
	case string(vm.PagePrereq):
		if len(v.fixButtons) > 0 {
			main = &v.fixButtons[0].btn
		}
	case string(vm.PageSetup):
		main = &v.install
	case string(vm.PageProvisioning):
		main = &v.provStart
		if !v.provStartRow.GetVisible() {
			main = &v.console
		}
	case string(vm.PageStopped):
		main = &v.start
	case string(vm.PageRunning):
		main = &v.connect
	}
	if main == nil || !main.GetVisible() || !main.GetSensitive() || !main.GrabFocus() {
		v.win.SetFocus(nil)
	}
}

// seeing is how a running guest is seen: Windows in a container over Remote
// Desktop, with a web console besides; a QEMU guest in a SPICE viewer, in
// QEMU's own window, or on its serial console; Waydroid draws itself.
type seeing int

const (
	seeNone   seeing = iota // the guest draws itself
	seeRDP                  // Connect opens FreeRDP; the web console works too
	seeSPICE                // Show opens remote-viewer
	seeWindow               // QEMU's own window is already on the desktop
	seeSerial               // the window's console page
)

func seeingOf(m vm.Machine) seeing {
	switch m.Backend {
	case vm.BackendDockur:
		return seeRDP
	case vm.BackendQEMU:
		switch m.Display.Kind {
		case vm.DisplaySPICE:
			return seeSPICE
		case vm.DisplayGTK:
			return seeWindow
		case vm.DisplaySerial:
			return seeSerial
		}
	}
	return seeNone
}

// setWatchRows words the rows that show the guest, on the provisioning,
// starting and running pages, for how this machine is seen. A row whose
// machine has nothing to open says where the guest is, without a button; a
// machine that draws itself has no such row.
func (v *vmWindow) setWatchRows(s MachineView) {
	see := seeingOf(s.Machine)
	type words struct{ title, note, button string }
	var prov, boot, run words
	switch see {
	case seeRDP:
		prov = words{i18n.T("Watch the installation"), i18n.T("The installer's screen, in your browser."), i18n.T("Open")}
		boot = words{i18n.T("Web console"), i18n.T("The guest's screen, in your browser, until Remote Desktop is ready."), i18n.T("Open")}
		run = words{i18n.T("Connect"), i18n.T("Windows' desktop in a window of its own, on this screen."), i18n.T("Connect")}
	case seeSPICE:
		prov = words{i18n.T("Watch the installation"), i18n.T("The installer's screen, in a window of its own."), i18n.T("Show")}
		boot = words{i18n.T("Screen"), i18n.T("The guest's screen, in a window of its own."), i18n.T("Show")}
		run = boot
	case seeWindow:
		prov = words{i18n.T("Watch the installation"), i18n.T("QEMU shows the installer in a window of its own."), ""}
		boot = words{i18n.T("Screen"), i18n.T("QEMU shows the guest in a window of its own."), ""}
		run = boot
	case seeSerial:
		prov = words{i18n.T("Serial console"), i18n.T("What the system prints, on its own serial console."), i18n.T("Open")}
		boot, run = prov, prov
	}
	set := func(row *gtk.Box, title, note *gtk.Label, b *gtk.Button, w words) {
		row.SetVisible(w.title != "")
		title.SetText(w.title)
		note.SetText(w.note)
		b.SetVisible(w.button != "")
		if w.button != "" && v.waitButton != b {
			b.SetLabel(w.button)
		}
	}
	set(&v.provWatchRow, &v.provWatchTitle, &v.provWatchNote, &v.console, prov)
	set(&v.bootWatchRow, &v.bootWatchTitle, &v.bootWatchNote, &v.bootConsole, boot)
	set(&v.runWatchRow, &v.runWatchTitle, &v.runWatchNote, &v.connect, run)
	v.runConsoleRow.SetVisible(see == seeRDP)
	v.removeRow.SetVisible(s.Machine.Backend == vm.BackendDockur)
	// Starting over is the container's removal for Windows, whose downloads
	// stay; for the others it is the unfinished disk's.
	if s.Machine.Backend == vm.BackendDockur {
		v.provRemoveTitle.SetText(i18n.T("Abandon the installation"))
		v.provRemoveNote.SetText(i18n.T("Removes the container so you can start over. Downloaded files stay."))
		if v.waitButton != &v.provRemove {
			v.provRemove.SetLabel(i18n.T("Abandon…"))
		}
	} else {
		v.provRemoveTitle.SetText(i18n.T("Start over"))
		v.provRemoveNote.SetText(i18n.T("Deletes the unfinished disk; the machine stays, to install again."))
		if v.waitButton != &v.provRemove {
			v.provRemove.SetLabel(i18n.T("Start over…"))
		}
	}
}

// watch opens what shows a guest, for the watch rows' buttons and Connect.
func (v *vmWindow) watch(name string, b *gtk.Button, errAt *gtk.Label, web bool) {
	view := v.viewOf(name)
	switch see := seeingOf(view.Machine); {
	case see == seeSerial:
		v.showConsole()
	case see == seeRDP && web:
		v.run(func() error { return v.manager.Console(name) }, errAt, b, i18n.T("Opening…"))
	case see == seeRDP:
		display := v.display()
		v.run(func() error { return v.manager.Connect(name, display) }, errAt, b, i18n.T("Connecting…"))
	case see == seeSPICE:
		display := v.display()
		v.run(func() error { return v.manager.Connect(name, display) }, errAt, b, i18n.T("Opening…"))
	}
}

// closeNew puts the new-machine dialog away, keeping it for the next one.
func (v *vmWindow) closeNew() {
	v.newDialog.SetVisible(false)
}

// vmCheckHint is what to do about a failed check that inua does not fix
// itself: a command to copy (command is true), or what to change where. The
// window never runs a package manager or joins the docker group: choosing
// packages and widening an account's rights is the user's call, not a
// button's. A tool that is there but may not be used is not installed again.
func vmCheckHint(c vm.DoctorItem, distro string) (hint string, command bool) {
	switch {
	case c.Name == "Docker" && c.Reason == vm.ReasonDenied:
		return "sudo gpasswd -a $USER docker", true
	case c.Name == "Docker" && c.Reason == vm.ReasonStopped:
		return "sudo systemctl enable --now docker", true
	case c.Name == "Secret Service":
		return i18n.T("Start inua-keyring, or install a Secret Service."), false
	case c.Name == "Operating system":
		return i18n.T("This manager needs NixOS, Ubuntu or Debian."), false
	case c.Reason != vm.ReasonMissing:
		return "", false
	}
	plan := vm.PlanPackages(distro)
	pkg := ""
	for _, name := range plan.Packages {
		lower := strings.ToLower(name)
		switch c.Name {
		case "Docker", "Docker Compose v2":
			if strings.Contains(lower, "docker") {
				pkg = name
			}
		case "FreeRDP":
			if strings.Contains(lower, "freerdp") {
				pkg = name
			}
		case "Browser opener":
			if strings.Contains(name, "xdg") {
				pkg = name
			}
		case "KVM":
			if strings.Contains(lower, "qemu") || strings.Contains(name, "kvm") {
				pkg = name
			}
		case "QEMU":
			if strings.Contains(lower, "qemu") {
				pkg = name
			}
		case "Firmware (UEFI)":
			if strings.Contains(lower, "ovmf") || strings.Contains(lower, "edk2") {
				pkg = name
			}
		case "Waydroid":
			if strings.Contains(lower, "waydroid") {
				pkg = name
			}
		}
		if pkg != "" {
			break
		}
	}
	if pkg == "" {
		if c.Name == "KVM" {
			// No package makes /dev/kvm: the processor's virtualization does.
			return i18n.T("Turn on virtualization (Intel VT-x or AMD-V) in the firmware settings."), false
		}
		return "", false
	}
	switch plan.PackageManager {
	case "nix":
		return i18n.Tf("Add %s to your NixOS configuration.", pkg), false
	case "apt":
		return "sudo apt install " + pkg, true
	}
	return pkg, true
}

// showRecommended says, under each editor, what suits this computer, and
// offers it once the editors differ from it.
func (v *vmWindow) showRecommended(s MachineView) {
	r := s.Recommended
	v.memorySub.SetText(i18n.Tf("Recommended: %d GiB.", gibOf(r.MemoryMiB)))
	v.cpusSub.SetText(i18n.Tf("Recommended: %d.", r.CPUs))
	v.diskSub.SetText(i18n.Tf("Recommended: %d GiB.", r.DiskGiB))
	m := v.setupMachine()
	v.accept.SetVisible(r.MemoryMiB > 0 &&
		(gibOf(m.MemoryMiB) != gibOf(r.MemoryMiB) || m.CPUs != r.CPUs || m.DiskGiB != r.DiskGiB))
}

// edited follows the editors: the recommendation's button on the setup page,
// Apply on the stopped one.
func (v *vmWindow) edited() {
	if v.filling {
		return
	}
	view := v.viewOf(v.selectedName())
	switch v.lastPage {
	case string(vm.PageSetup):
		v.showRecommended(view)
	case string(vm.PageStopped):
		v.showApply(view)
	}
}

// showProgress moves a bar: to the fraction when there is one, with the
// figure in the section's head, or a pulse when nothing is measured.
func showProgress(bar *gtk.ProgressBar, meta *gtk.Label, fraction float64) {
	if fraction > 0 {
		bar.SetFraction(fraction)
		meta.SetText(fmt.Sprintf("%d%%", int(fraction*100+0.5)))
		return
	}
	bar.Pulse()
	meta.SetText("")
}

// showApply offers Apply only when the editors differ from the machine.
func (v *vmWindow) showApply(s MachineView) {
	m := v.readyMachine()
	changed := m.MemoryMiB != s.Machine.MemoryMiB || m.CPUs != s.Machine.CPUs
	v.readySave.SetSensitive(changed && !s.Busy && !v.operationBusy)
}

// gibOf is a memory size in whole GiB, as the editors show it.
func gibOf(mib int) int {
	return (mib + 512) / 1024
}
